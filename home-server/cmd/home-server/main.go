package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"gohome/home-server/internal/lan"
	"gohome/home-server/internal/portmap"
	"gohome/shared/protocol"
	"gohome/shared/security"
)

func main() {
	serverURL := flag.String("server", "ws://127.0.0.1:8080/ws", "public server websocket URL")
	authCode := flag.String("auth-code", "GOHOME-CHANGE-ME", "server authorization code")
	authCodeFile := flag.String("auth-code-file", "", "file containing server authorization code")
	udpPort := flag.Int("udp-port", 47777, "local UDP port for P2P hole punching")
	enableUPnP := flag.Bool("upnp", true, "attempt same-port UPnP UDP mapping for the direct tunnel")
	lanCIDR := flag.String("lan-cidr", "", "home LAN CIDR override")
	lanInterface := flag.String("lan-interface", "", "home LAN interface label override")
	deviceIDFile := flag.String("device-id-file", defaultDeviceIDFile(), "device id persistence file")
	identityFile := flag.String("identity-file", defaultIdentityFile(), "SM2 identity persistence file")
	flag.Parse()

	identity, err := security.LoadOrCreateIdentity(*identityFile)
	if err != nil {
		log.Fatalf("identity: %v", err)
	}
	deviceID, err := loadOrCreateDeviceID(*deviceIDFile, identity.DeviceID("home"))
	if err != nil {
		log.Fatalf("device id: %v", err)
	}
	loadedAuthCode, err := loadAuthCode(*authCode, *authCodeFile)
	if err != nil {
		log.Fatalf("auth code: %v", err)
	}
	if *udpPort < 1 || *udpPort > 65535 {
		log.Fatalf("udp port must be between 1 and 65535")
	}
	log.Printf("home-server device id: %s", deviceID)

	udpConn, err := net.ListenPacket("udp", fmt.Sprintf(":%d", *udpPort))
	if err != nil {
		log.Fatalf("udp listen: %v", err)
	}
	defer udpConn.Close()
	udp := newUDPService(udpConn, identity, *lanInterface)
	go udp.readLoop()
	if *enableUPnP {
		go portmap.MaintainUPnP(context.Background(), uint16(*udpPort), *lanInterface)
	}

	for {
		if err := run(context.Background(), *serverURL, loadedAuthCode, deviceID, identity.PublicPEM, *udpPort, *lanCIDR, *lanInterface, udp); err != nil {
			log.Printf("connection ended: %v", err)
		}
		time.Sleep(3 * time.Second)
	}
}

func run(ctx context.Context, serverURL, authCode, deviceID, publicKey string, udpPort int, lanCIDR, lanInterface string, udp *udpService) error {
	conn, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	var writeMu sync.Mutex
	writeJSON := func(value any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(value)
	}

	now := time.Now()
	auth, err := protocol.Request("auth-1", protocol.ActionDeviceAuth, protocol.DeviceAuthParams{
		DeviceID:   deviceID,
		DeviceType: protocol.DeviceTypeHomeServer,
		AuthCode:   authCode,
		PublicKey:  publicKey,
		TimeKey:    security.GenerateTimeKey(authCode, now),
		Timestamp:  now.Unix(),
		UDPPort:    udpPort,
	})
	if err != nil {
		return err
	}
	if err := writeJSON(auth); err != nil {
		return err
	}

	if err := reportLAN(writeJSON, lanCIDR, lanInterface); err != nil {
		log.Printf("initial lan report failed: %v", err)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	errs := make(chan error, 1)
	go func() {
		for {
			var env protocol.Envelope
			if err := conn.ReadJSON(&env); err != nil {
				errs <- err
				return
			}
			handleServerEvent(writeJSON, udp, env)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errs:
			return err
		case <-ticker.C:
			now := time.Now()
			ping, _ := protocol.Request(fmt.Sprintf("ping-%d", now.Unix()), protocol.ActionPing, protocol.HeartbeatParams{
				TimeKey:   security.GenerateTimeKey(authCode, now),
				Timestamp: now.Unix(),
			})
			if err := writeJSON(ping); err != nil {
				return err
			}
			if err := reportTraffic(writeJSON, udp.trafficDelta()); err != nil {
				return err
			}
			_ = reportLAN(writeJSON, lanCIDR, lanInterface)
		}
	}
}

func reportLAN(writeJSON func(any) error, lanCIDR, lanInterface string) error {
	info := lan.Detect()
	if lanCIDR != "" {
		info.CIDR = lanCIDR
	}
	if lanInterface != "" {
		info.Interface = lanInterface
	}
	env, err := protocol.Request(fmt.Sprintf("lan-%d", time.Now().UnixNano()), protocol.ActionDeviceLANReport, protocol.LANReportParams{
		LANCIDR:   info.CIDR,
		Gateway:   info.Gateway,
		Interface: info.Interface,
	})
	if err != nil {
		return err
	}
	return writeJSON(env)
}

func reportTraffic(writeJSON func(any) error, delta trafficTotals) error {
	for _, report := range []struct {
		direction string
		bytes     uint64
	}{
		{direction: "up", bytes: delta.Up},
		{direction: "down", bytes: delta.Down},
	} {
		if report.bytes == 0 {
			continue
		}
		env, err := protocol.Request(fmt.Sprintf("traffic-%s-%d", report.direction, time.Now().UnixNano()), protocol.ActionStatsTraffic, protocol.TrafficReportParams{
			Direction: report.direction,
			Bytes:     int64(report.bytes),
		})
		if err != nil {
			return err
		}
		if err := writeJSON(env); err != nil {
			return err
		}
	}
	return nil
}

func handleServerEvent(writeJSON func(any) error, udp *udpService, env protocol.Envelope) {
	switch env.Action {
	case protocol.EventDeviceLatencyProbe:
		var params struct {
			ProbeID string `json:"probe_id"`
		}
		if err := json.Unmarshal(env.Params, &params); err != nil {
			log.Printf("bad latency probe: %v", err)
			return
		}
		reply, err := protocol.Request("latency-"+params.ProbeID, protocol.ActionStatsLatencyPong, protocol.LatencyPongParams{ProbeID: params.ProbeID})
		if err != nil {
			log.Printf("latency pong build failed: %v", err)
			return
		}
		if err := writeJSON(reply); err != nil {
			log.Printf("latency pong failed: %v", err)
		}
	case protocol.EventP2PHolePunchOffer:
		var offer protocol.HolePunchOffer
		if err := json.Unmarshal(env.Params, &offer); err != nil {
			log.Printf("bad hole punch offer: %v", err)
			return
		}
		udp.acceptOffer(offer)
		log.Printf("hole punch offer: client=%s endpoint=%s family=%d", offer.Client.DeviceID, offer.Client.Endpoint, offer.FamilyID)
	case protocol.EventDeviceForceOffline:
		log.Printf("force offline requested")
		os.Exit(0)
	default:
		if env.Error != nil {
			log.Printf("server error: %s %s", env.Error.Code, env.Error.Message)
		}
	}
}

func loadOrCreateDeviceID(path, generated string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return string(b), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return generated, os.WriteFile(path, []byte(generated), 0o600)
}

func loadAuthCode(value, path string) (string, error) {
	if path == "" {
		return value, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	authCode := strings.TrimSpace(string(b))
	if authCode == "" {
		return "", fmt.Errorf("auth code file is empty")
	}
	return authCode, nil
}

func defaultDeviceIDFile() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ".go-home-home-server-id"
	}
	return filepath.Join(dir, "go-home", "home-server-id")
}

func defaultIdentityFile() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ".go-home-home-server-sm2.pem"
	}
	return filepath.Join(dir, "go-home", "home-server-sm2.pem")
}
