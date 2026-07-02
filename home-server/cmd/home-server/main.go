// Package main 是 Go Home 家庭服务器的入口程序。
//
// 家庭服务器运行在家庭局域网中，职责包括：
//   - 连接公网服务器，上报家庭局域网网段信息
//   - 响应 P2P 打洞请求，与客户端建立 UDP 加密隧道
//   - 通过 DHCP 代理为客户端分配局域网 IP
//   - 通过代理 ARP 让客户端 IP 在局域网中可达
//   - 在虚拟网段和真实网段之间转换 IPv4 数据包
//   - 通过 UPnP/NAT-PMP 自动映射 UDP 端口
//
// 启动方式：
//
//	go run ./cmd/home-server -server ws://YOUR_SERVER:8080/ws -auth-code YOUR_CODE
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	"gohome/home-server/internal/lan"
	"gohome/home-server/internal/portmap"
	"gohome/shared/protocol"
	"gohome/shared/security"
	"gohome/shared/tunnel"
)

func main() {
	serverURL := flag.String("server", "ws://127.0.0.1:8080/ws", "public server websocket URL")
	authCode := flag.String("auth-code", "GOHOME-CHANGE-ME", "server authorization code")
	authCodeFile := flag.String("auth-code-file", "", "file containing server authorization code")
	udpPort := flag.Int("udp-port", 47777, "local UDP port for P2P hole punching")
	udpSockets := flag.Int("udp-sockets", 8, "number of local UDP sockets used for hole punching")
	enableUPnP := flag.Bool("upnp", true, "attempt same-port UPnP UDP mapping for the direct tunnel")
	enableNATPMP := flag.Bool("nat-pmp", true, "attempt same-port NAT-PMP UDP mapping for the direct tunnel")
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

	udpConns, err := openUDPSockets(*udpPort, *udpSockets)
	if err != nil {
		log.Fatalf("udp listen: %v", err)
	}
	for _, conn := range udpConns {
		defer conn.Close()
	}
	udp := newUDPService(udpConns, identity, *lanInterface)
	udp.readLoops()

	// 监听信号，优雅关闭
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *enableUPnP {
		go portmap.MaintainUPnP(ctx, uint16(*udpPort), *lanInterface)
	}
	if *enableNATPMP {
		go portmap.MaintainNATPMP(ctx, uint16(*udpPort))
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, shutting down...", sig)
		// 清理所有隧道会话（释放 DHCP 租约、移除 ProxyARP）
		udp.closeAll()
		cancel()
		os.Exit(0)
	}()

	for {
		if err := run(ctx, *serverURL, loadedAuthCode, deviceID, identity.PublicPEM, *udpPort, *lanCIDR, *lanInterface, udp, udpConns); err != nil {
			log.Printf("connection ended: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func openUDPSockets(primaryPort, socketCount int) ([]net.PacketConn, error) {
	if socketCount < 1 {
		socketCount = 1
	}
	if socketCount > 16 {
		socketCount = 16
	}
	conns := make([]net.PacketConn, 0, socketCount)
	primary, err := net.ListenPacket("udp", fmt.Sprintf(":%d", primaryPort))
	if err != nil {
		return nil, err
	}
	conns = append(conns, primary)
	for len(conns) < socketCount {
		conn, err := net.ListenPacket("udp", ":0")
		if err != nil {
			log.Printf("auxiliary UDP socket unavailable: %v", err)
			break
		}
		conns = append(conns, conn)
	}
	for index, conn := range conns {
		log.Printf("UDP punch socket[%d] listening on %s", index, conn.LocalAddr())
	}
	return conns, nil
}

// run 执行一次到公网服务器的连接生命周期：认证 → 心跳 → 断线。
// 断线后由 main 循环重连。
func run(ctx context.Context, serverURL, authCode, deviceID, publicKey string, udpPort int, lanCIDR, lanInterface string, udp *udpService, udpConns []net.PacketConn) error {
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

	// 发送设备认证请求
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

	// 读取认证响应，获取 server_udp_port
	var authResp protocol.Envelope
	if err := conn.ReadJSON(&authResp); err != nil {
		return fmt.Errorf("read auth response: %w", err)
	}
	var authResult protocol.DeviceAuthResult
	if raw, err := json.Marshal(authResp.Result); err == nil {
		_ = json.Unmarshal(raw, &authResult)
	}
	if authResp.Error != nil {
		return fmt.Errorf("auth failed: %s", authResp.Error.Message)
	}

	// 上报初始 LAN 网段信息
	if err := reportLAN(writeJSON, lanCIDR, lanInterface); err != nil {
		log.Printf("initial lan report failed: %v", err)
	}

	// 启动 UDP 注册探测（NAT 端点发现）
	if ports := serverUDPPorts(authResult); len(ports) > 0 {
		go registerUDPLoop(ctx, serverURL, ports, deviceID, authResult.Token, udpConns)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	errs := make(chan error, 1)
	// 读取服务器推送的事件
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

	// 主循环：心跳 + 流量上报 + LAN 上报
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
			delta := udp.trafficDelta()
			if err := reportTraffic(writeJSON, delta); err != nil {
				return err
			}
			logUIStats(udp.statsSnapshot())
			_ = reportLAN(writeJSON, lanCIDR, lanInterface)
		}
	}
}

func logUIStats(stats serviceStats) {
	payload, err := json.Marshal(stats)
	if err != nil {
		return
	}
	log.Printf("ui_stats %s", payload)
}

// reportLAN 向服务器上报家庭局域网网段信息。
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

// reportTraffic 向服务器上报本周期流量统计。
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

// handleServerEvent 处理服务器推送的事件。
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
	case protocol.EventP2PCandidate:
		var params struct {
			SessionID string `json:"session_id"`
			Candidate string `json:"candidate"`
		}
		if err := json.Unmarshal(env.Params, &params); err != nil {
			log.Printf("bad P2P candidate: %v", err)
			return
		}
		udp.addPunchCandidate(params.SessionID, params.Candidate)
	case protocol.EventDeviceForceOffline:
		log.Printf("force offline requested, shutting down")
		udp.closeAll()
		os.Exit(0)
	default:
		if env.Error != nil {
			log.Printf("server error: %s %s", env.Error.Code, env.Error.Message)
		}
	}
}

// loadOrCreateDeviceID 加载或创建设备 ID 持久化文件。
func loadOrCreateDeviceID(path, generated string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return string(b), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return generated, os.WriteFile(path, []byte(generated), 0o600)
}

// loadAuthCode 从命令行参数或文件加载授权码。
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

// defaultDeviceIDFile 返回设备 ID 持久化文件的默认路径。
func defaultDeviceIDFile() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ".go-home-home-server-id"
	}
	return filepath.Join(dir, "go-home", "home-server-id")
}

// defaultIdentityFile 返回 SM2 身份持久化文件的默认路径。
func defaultIdentityFile() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ".go-home-home-server-sm2.pem"
	}
	return filepath.Join(dir, "go-home", "home-server-sm2.pem")
}

// registerUDPLoop 定期向公网服务器发送 UDP 注册探测包，
// 让服务器发现本设备 NAT 映射后的公网端点。
func serverUDPPorts(authResult protocol.DeviceAuthResult) []int {
	seen := map[int]bool{}
	var ports []int
	for _, port := range authResult.ServerUDPPorts {
		if port < 1 || port > 65535 || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	if authResult.ServerUDPPort > 0 && !seen[authResult.ServerUDPPort] {
		ports = append([]int{authResult.ServerUDPPort}, ports...)
	}
	return ports
}

func registerUDPLoop(ctx context.Context, serverURL string, serverUDPPorts []int, deviceID, token string, udpConns []net.PacketConn) {
	// 从 WebSocket URL 解析服务器主机名
	wsURL := serverURL
	wsURL = strings.Replace(wsURL, "wss://", "https://", 1)
	wsURL = strings.Replace(wsURL, "ws://", "http://", 1)
	parsed, err := url.Parse(wsURL)
	if err != nil {
		log.Printf("parse server URL for UDP: %v", err)
		return
	}
	host := parsed.Hostname()
	var serverAddrs []*net.UDPAddr
	for _, port := range serverUDPPorts {
		serverAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
		if err != nil {
			log.Printf("resolve server UDP address %d: %v", port, err)
			continue
		}
		serverAddrs = append(serverAddrs, serverAddr)
	}
	if len(serverAddrs) == 0 {
		log.Printf("no usable server UDP discovery address")
		return
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	sendUDPRegisters := func() {
		for _, udpConn := range udpConns {
			localUDPPort := 0
			if addr, ok := udpConn.LocalAddr().(*net.UDPAddr); ok {
				localUDPPort = addr.Port
			}
			packet, err := tunnel.MarshalRegister(tunnel.Register{
				DeviceID: deviceID,
				Token:    token,
				UDPPort:  localUDPPort,
			})
			if err != nil {
				log.Printf("marshal register packet: %v", err)
				continue
			}
			for _, serverAddr := range serverAddrs {
				if _, err := udpConn.WriteTo(packet, serverAddr); err != nil {
					log.Printf("send UDP register from %s to %s: %v", udpConn.LocalAddr(), serverAddr, err)
				}
			}
		}
	}

	// 立即连续发送几轮，让公网服务器尽快收集多组 NAT 映射。
	for i := 0; i < 3; i++ {
		sendUDPRegisters()
		time.Sleep(120 * time.Millisecond)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendUDPRegisters()
		}
	}
}
