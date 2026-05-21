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
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"gohome/home-server/internal/lan"
	"gohome/shared/protocol"
	"gohome/shared/security"
)

func main() {
	serverURL := flag.String("server", "ws://127.0.0.1:8080/ws", "public server websocket URL")
	authCode := flag.String("auth-code", "GOHOME-CHANGE-ME", "server authorization code")
	udpPort := flag.Int("udp-port", 47777, "local UDP port for P2P hole punching")
	deviceIDFile := flag.String("device-id-file", defaultDeviceIDFile(), "device id persistence file")
	flag.Parse()

	deviceID, err := loadOrCreateDeviceID(*deviceIDFile)
	if err != nil {
		log.Fatalf("device id: %v", err)
	}
	log.Printf("home-server device id: %s", deviceID)

	udpConn, err := net.ListenPacket("udp", fmt.Sprintf(":%d", *udpPort))
	if err != nil {
		log.Fatalf("udp listen: %v", err)
	}
	defer udpConn.Close()
	go udpReadLoop(udpConn)

	for {
		if err := run(context.Background(), *serverURL, *authCode, deviceID, *udpPort); err != nil {
			log.Printf("connection ended: %v", err)
		}
		time.Sleep(3 * time.Second)
	}
}

func run(ctx context.Context, serverURL, authCode, deviceID string, udpPort int) error {
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

	if err := reportLAN(writeJSON); err != nil {
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
			handleServerEvent(writeJSON, env)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errs:
			return err
		case <-ticker.C:
			ping, _ := protocol.Request(fmt.Sprintf("ping-%d", time.Now().Unix()), protocol.ActionPing, map[string]any{
				"time_key":  security.GenerateTimeKey(authCode, time.Now()),
				"timestamp": time.Now().Unix(),
			})
			if err := writeJSON(ping); err != nil {
				return err
			}
			_ = reportLAN(writeJSON)
		}
	}
}

func reportLAN(writeJSON func(any) error) error {
	info := lan.Detect()
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

func handleServerEvent(writeJSON func(any) error, env protocol.Envelope) {
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

func udpReadLoop(conn net.PacketConn) {
	buf := make([]byte, 2048)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		log.Printf("udp packet from %s len=%d", addr.String(), n)
	}
}

func loadOrCreateDeviceID(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return string(b), nil
	}
	token, err := security.NewToken(16)
	if err != nil {
		return "", err
	}
	id := "home-" + token
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return id, os.WriteFile(path, []byte(id), 0o600)
}

func defaultDeviceIDFile() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ".go-home-home-server-id"
	}
	return filepath.Join(dir, "go-home", "home-server-id")
}
