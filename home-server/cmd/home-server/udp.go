package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"gohome/home-server/internal/lan"
	"gohome/shared/protocol"
	"gohome/shared/security"
	"gohome/shared/tunnel"
)

const (
	candidatePortPredictionWindow  = 16
	aggressivePortPredictionWindow = 512
	maxProbeCandidatesPerAttempt   = 256
	fullPortSweepStartAttempt      = 32
	fullPortSweepBatchSize         = 1024
	readyBurstDuration             = 2200 * time.Millisecond
	readyBurstInterval             = 120 * time.Millisecond
)

// udpService 管理 UDP 隧道的所有会话，负责：
//   - 接受服务器的打洞邀请，向客户端发送探测包
//   - 读取并处理 UDP 数据包（探测、握手、加密帧）
//   - 维护活跃隧道会话（SM4 密钥、对端地址、流量统计）
//   - 定期回收过期会话，释放 DHCP 租约和 ProxyARP 条目
type udpService struct {
	// conns UDP 监听连接，第一项为主连接，其余为打洞辅助连接。
	conns []net.PacketConn
	// identity SM2 身份，用于解密客户端发送的会话密钥。
	identity *security.Identity
	// iface 局域网网卡名称，用于 DHCP 代理和 ProxyARP。
	iface string

	// mu 保护 offers 和 sessions 的并发访问。
	mu sync.RWMutex
	// helloMu serializes session creation so repeated punch packets cannot
	// allocate multiple LAN leases before the first session is installed.
	helloMu sync.Mutex
	// offers 待处理的打洞邀请，key 为 sessionID。
	offers map[string]protocol.HolePunchOffer
	// sessions 活跃的 UDP 隧道会话，key 为 sessionID。
	sessions map[string]*udpSession
}

// udpSession 表示一个活跃的 UDP 隧道会话。
type udpSession struct {
	// mu 保护会话内部状态的并发访问。
	mu sync.Mutex
	// offer 原始打洞邀请，包含双方连接信息。
	offer protocol.HolePunchOffer
	// key SM4-GCM 会话密钥（16 字节）。
	key []byte
	// peer 对端（客户端）的公网地址。
	peer net.Addr
	// conn 收到该会话握手的 UDP 连接，后续加密帧固定从该连接发送。
	conn net.PacketConn
	// sendSeq 发送序列号，递增用于防重放和排序。
	sendSeq uint64
	// replay 接收侧的 64 位滑动窗口重放检测器。
	replay tunnel.ReplayWindow
	// link 虚拟网卡链路（TUN 设备），nil 表示无 L3 隧道。
	link packetLink
	// ready 预序列化的 Ready 帧载荷，用于重发。
	ready []byte
	// seenAt 最后收到数据包的时间，用于过期检测。
	seenAt time.Time
	// up 发送流量累计字节数。
	up uint64
	// down 接收流量累计字节数。
	down uint64
	// report 上次上报时的流量快照，用于计算增量。
	report trafficTotals
	// lease DHCP 租约信息，用于会话结束时释放。
	lease *lan.Lease
}

// trafficTotals 记录上行和下行流量字节数。
type trafficTotals struct {
	Up   uint64
	Down uint64
}

// packetLink 是虚拟网卡链路的接口，用于读写 IP 数据包。
type packetLink interface {
	// WritePacket 将 IP 数据包写入虚拟网卡。
	WritePacket([]byte) error
	// Close 关闭虚拟网卡并清理相关路由配置。
	Close() error
}

// newUDPService 创建 UDP 隧道服务并启动过期会话回收循环。
func newUDPService(conns []net.PacketConn, identity *security.Identity, iface string) *udpService {
	if len(conns) == 0 {
		panic("udp service requires at least one packet conn")
	}
	service := &udpService{
		conns:    conns,
		identity: identity,
		iface:    iface,
		offers:   map[string]protocol.HolePunchOffer{},
		sessions: map[string]*udpSession{},
	}
	go service.reapLoop()
	return service
}

func (s *udpService) primaryConn() net.PacketConn {
	return s.conns[0]
}

// acceptOffer 接受服务器的打洞邀请，向客户端发送 UDP 探测包。
// 探测包会向所有候选端点多轮发送，用于 P2P 打洞。
func (s *udpService) acceptOffer(offer protocol.HolePunchOffer) {
	replaced := s.installOffer(offer)
	for _, session := range replaced {
		log.Printf("replacing stale UDP session: old_session=%s client=%s new_session=%s", session.offer.SessionID, offer.Client.DeviceID, offer.SessionID)
		s.cleanupSession(session)
	}

	baseCandidates, err := resolvePeerUDPBaseCandidates(offer.Client)
	if err != nil {
		log.Printf("resolve client candidates for session %s: %v", offer.SessionID, err)
		return
	}
	log.Printf("UDP probe base candidates for session %s: %v", offer.SessionID, baseCandidates)

	packet, err := tunnel.MarshalProbe(tunnel.Probe{
		SessionID: offer.SessionID,
		DeviceID:  offer.Server.DeviceID,
		Role:      protocol.DeviceTypeHomeServer,
	})
	if err != nil {
		log.Printf("build UDP probe for session %s: %v", offer.SessionID, err)
		return
	}

	go func() {
		timeout := 45 * time.Second
		if offer.Request.FallbackSweep {
			timeout = 75 * time.Second
		}
		deadline := time.Now().Add(timeout)
		attempt := 0
		lastWindow := -1
		lastCandidateCount := 0
		sentPackets := uint64(0)
		fallbackSweep := offer.Request.FallbackSweep
		for time.Now().Before(deadline) {
			s.mu.RLock()
			active := s.sessions[offer.SessionID] != nil
			current, offered := s.offers[offer.SessionID]
			s.mu.RUnlock()
			if active || !offered || current.Client.DeviceID != offer.Client.DeviceID {
				break
			}
			if refreshed, err := resolvePeerUDPBaseCandidates(current.Client); err == nil {
				baseCandidates = refreshed
			}
			candidates := punchCandidateBatch(baseCandidates, attempt, maxProbeCandidatesPerAttempt)
			var sweepCandidates []*net.UDPAddr
			if fallbackSweep {
				sweepCandidates = fullPortSweepBatch(baseCandidates, attempt, fullPortSweepBatchSize)
			}
			window := punchPredictionWindow(attempt)
			if window != lastWindow {
				total := len(expandUDPCandidates(baseCandidates, window))
				log.Printf("UDP probe stage for session %s: attempt=%d window=+/-%d total_candidates=%d batch=%d sweep=%d sockets=%d", offer.SessionID, attempt, window, total, len(candidates), len(sweepCandidates), len(s.conns))
				lastWindow = window
			}
			lastCandidateCount = len(candidates)
			// 向所有候选端点发送探测包
			for _, conn := range s.conns {
				for _, candidate := range candidates {
					if _, err := conn.WriteTo(packet, candidate); err != nil {
						log.Printf("send UDP probe for session %s from %s to %s: %v", offer.SessionID, conn.LocalAddr(), candidate, err)
					} else {
						sentPackets++
					}
				}
			}
			for _, candidate := range sweepCandidates {
				if _, err := s.primaryConn().WriteTo(packet, candidate); err != nil {
					log.Printf("send UDP sweep probe for session %s from %s to %s: %v", offer.SessionID, s.primaryConn().LocalAddr(), candidate, err)
				} else {
					sentPackets++
				}
			}
			time.Sleep(punchInterval(attempt))
			attempt++
		}
		log.Printf("UDP probe burst finished for session %s: attempts=%d last_window=+/-%d last_batch=%d sockets=%d packets=%d", offer.SessionID, attempt, lastWindow, lastCandidateCount, len(s.conns), sentPackets)
	}()
}

// addPunchCandidate adds a signaling-assisted candidate discovered while a
// punch attempt is active. The public server only forwards endpoint metadata;
// all tunnel packets still travel directly between peers.
func (s *udpService) addPunchCandidate(sessionID, endpoint string) {
	normalized, ok := normalizeIPv4Endpoint(endpoint)
	if !ok || sessionID == "" {
		return
	}
	s.mu.Lock()
	offer, found := s.offers[sessionID]
	if !found {
		s.mu.Unlock()
		return
	}
	if containsString(offer.Client.Candidates, normalized) {
		s.mu.Unlock()
		return
	}
	offer.Client.Candidates = rememberCandidate(offer.Client.Candidates, normalized)
	offer.Client.ObservedEndpoint = normalized
	s.offers[sessionID] = offer
	s.mu.Unlock()
	log.Printf("added live UDP candidate for session %s: %s", sessionID, normalized)
}

func rememberCandidate(candidates []string, candidate string) []string {
	const maxCandidates = 64
	out := make([]string, 0, minInt(len(candidates)+1, maxCandidates))
	out = append(out, candidate)
	for _, existing := range candidates {
		if existing == candidate {
			continue
		}
		out = append(out, existing)
		if len(out) >= maxCandidates {
			break
		}
	}
	return out
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// installOffer stores a new punch offer and immediately detaches older
// sessions for the same client. This lets a restarted client reconnect
// without waiting for the idle reaper to release its previous TUN and lease.
func (s *udpService) installOffer(offer protocol.HolePunchOffer) []*udpSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	var replaced []*udpSession
	for sessionID, session := range s.sessions {
		if sessionID == offer.SessionID || session.offer.Client.DeviceID != offer.Client.DeviceID {
			continue
		}
		replaced = append(replaced, session)
		delete(s.sessions, sessionID)
		delete(s.offers, sessionID)
	}
	for sessionID, pending := range s.offers {
		if sessionID != offer.SessionID && pending.Client.DeviceID == offer.Client.DeviceID {
			delete(s.offers, sessionID)
		}
	}
	s.offers[offer.SessionID] = offer
	return replaced
}

func punchInterval(attempt int) time.Duration {
	switch {
	case attempt < 24:
		return 35 * time.Millisecond
	case attempt < 64:
		return 100 * time.Millisecond
	case attempt < 100:
		return 250 * time.Millisecond
	default:
		return 500 * time.Millisecond
	}
}

func resolvePeerUDPCandidates(peer protocol.PeerCandidate) ([]*net.UDPAddr, error) {
	return resolvePeerUDPEndpoints(peerCandidateEndpoints(peer))
}

func resolvePeerUDPBaseCandidates(peer protocol.PeerCandidate) ([]*net.UDPAddr, error) {
	return resolvePeerUDPEndpoints(peerBaseCandidateEndpoints(peer))
}

func resolvePeerUDPEndpoints(endpoints []string) ([]*net.UDPAddr, error) {
	if len(endpoints) == 0 {
		return nil, errors.New("peer has no usable IPv4 UDP candidate")
	}
	var out []*net.UDPAddr
	var lastErr error
	seen := map[string]bool{}
	for _, endpoint := range endpoints {
		addr, err := net.ResolveUDPAddr("udp4", endpoint)
		if err != nil {
			lastErr = err
			continue
		}
		key := addr.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, addr)
	}
	if len(out) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, errors.New("peer candidates could not be resolved")
	}
	return out, nil
}

func punchCandidateBatch(base []*net.UDPAddr, attempt int, maxBatch int) []*net.UDPAddr {
	window := punchPredictionWindow(attempt)
	candidates := expandUDPCandidates(base, window)
	if maxBatch <= 0 || len(candidates) <= maxBatch {
		return candidates
	}
	baseCount := len(base)
	if baseCount > maxBatch {
		baseCount = maxBatch
	}
	out := append([]*net.UDPAddr(nil), candidates[:baseCount]...)
	room := maxBatch - len(out)
	rotating := candidates[baseCount:]
	if room <= 0 || len(rotating) == 0 {
		return out
	}
	offset := (attempt * room) % len(rotating)
	for i := 0; i < room; i++ {
		out = append(out, rotating[(offset+i)%len(rotating)])
	}
	return out
}

func fullPortSweepBatch(base []*net.UDPAddr, attempt int, maxBatch int) []*net.UDPAddr {
	if attempt < fullPortSweepStartAttempt || maxBatch <= 0 {
		return nil
	}
	var hosts []net.IP
	seen := map[string]bool{}
	for _, candidate := range base {
		if candidate == nil || candidate.IP == nil || candidate.IP.To4() == nil {
			continue
		}
		host := candidate.IP.To4()
		key := host.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		hosts = append(hosts, host)
	}
	if len(hosts) == 0 {
		return nil
	}
	out := make([]*net.UDPAddr, 0, maxBatch)
	offset := ((attempt - fullPortSweepStartAttempt) * maxBatch) % 65535
	for index := 0; index < 65535 && len(out) < maxBatch; index++ {
		port := (offset+index)%65535 + 1
		for _, host := range hosts {
			out = append(out, &net.UDPAddr{IP: host, Port: port})
			if len(out) >= maxBatch {
				break
			}
		}
	}
	return out
}

func punchPredictionWindow(attempt int) int {
	switch {
	case attempt < 12:
		return candidatePortPredictionWindow
	case attempt < 32:
		return 64
	case attempt < 60:
		return 256
	default:
		return aggressivePortPredictionWindow
	}
}

func expandUDPCandidates(base []*net.UDPAddr, window int) []*net.UDPAddr {
	var out []*net.UDPAddr
	seen := map[string]bool{}
	add := func(addr *net.UDPAddr) {
		if addr == nil || addr.IP == nil || addr.IP.To4() == nil || addr.Port < 1 || addr.Port > 65535 {
			return
		}
		normalized := &net.UDPAddr{IP: addr.IP.To4(), Port: addr.Port}
		key := normalized.String()
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, normalized)
	}
	for _, addr := range base {
		add(addr)
	}
	exact := append([]*net.UDPAddr(nil), out...)
	for delta := 1; delta <= window; delta++ {
		for _, addr := range exact {
			if addr.Port+delta <= 65535 {
				add(&net.UDPAddr{IP: addr.IP, Port: addr.Port + delta})
			}
			if addr.Port-delta >= 1 {
				add(&net.UDPAddr{IP: addr.IP, Port: addr.Port - delta})
			}
		}
	}
	return out
}

func peerCandidateEndpoints(peer protocol.PeerCandidate) []string {
	return peerCandidateEndpointsWithWindow(peer, candidatePortPredictionWindow)
}

func peerCandidateEndpointsWithWindow(peer protocol.PeerCandidate, window int) []string {
	out := peerBaseCandidateEndpoints(peer)
	if window <= 0 {
		return out
	}
	seen := map[string]bool{}
	for _, endpoint := range out {
		seen[endpoint] = true
	}
	add := func(endpoint string) {
		normalized, ok := normalizeIPv4Endpoint(endpoint)
		if !ok || seen[normalized] {
			return
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	base := append([]string(nil), out...)
	for _, endpoint := range base {
		addPortPredictionWindow(add, endpoint, window)
	}
	return out
}

func peerBaseCandidateEndpoints(peer protocol.PeerCandidate) []string {
	var out []string
	seen := map[string]bool{}
	add := func(endpoint string) {
		normalized, ok := normalizeIPv4Endpoint(endpoint)
		if !ok || seen[normalized] {
			return
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	for _, endpoint := range peer.Candidates {
		add(endpoint)
	}
	add(peer.ObservedEndpoint)
	add(peer.Endpoint)
	if peer.UDPPort > 0 {
		for _, endpoint := range []string{peer.ObservedEndpoint, peer.Endpoint, peer.RemoteAddr} {
			if host, ok := endpointHost(endpoint); ok {
				add(net.JoinHostPort(host, strconv.Itoa(peer.UDPPort)))
			}
		}
	}
	return out
}

func addPortPredictionWindow(add func(string), endpoint string, window int) {
	host, port, ok := endpointParts(endpoint)
	if !ok {
		return
	}
	for delta := 1; delta <= window; delta++ {
		if port+delta <= 65535 {
			add(net.JoinHostPort(host, strconv.Itoa(port+delta)))
		}
		if port-delta >= 1 {
			add(net.JoinHostPort(host, strconv.Itoa(port-delta)))
		}
	}
}

func endpointHost(endpoint string) (string, bool) {
	host, _, ok := endpointParts(endpoint)
	return host, ok
}

func endpointParts(endpoint string) (string, int, bool) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", 0, false
	}
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", 0, false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || ip.To4() == nil {
		return "", 0, false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	return ip.To4().String(), port, true
}

func normalizeIPv4Endpoint(endpoint string) (string, bool) {
	host, port, ok := endpointParts(endpoint)
	if !ok {
		return "", false
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), true
}

func (s *udpService) readLoops() {
	for _, conn := range s.conns {
		conn := conn
		go s.readLoop(conn)
	}
}

// readLoop 持续读取 UDP 数据包并分发给处理函数。
// 遇到临时性错误时记录日志并继续，遇到致命错误时退出。
func (s *udpService) readLoop(conn net.PacketConn) {
	buf := make([]byte, 64*1024)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			// 记录错误但不退出，尝试恢复读取
			if isTemporaryError(err) {
				log.Printf("udp read temporary error: %v", err)
				continue
			}
			log.Printf("udp read fatal error, stopping read loop: %v", err)
			return
		}
		packet := append([]byte(nil), buf[:n]...)
		if err := s.handlePacket(conn, packet, addr); err != nil {
			log.Printf("udp packet rejected from %s: %v", addr.String(), err)
		}
	}
}

// isTemporaryError 判断是否为临时性网络错误（可恢复）。
func isTemporaryError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// handlePacket 根据数据包类型分发处理。
func (s *udpService) handlePacket(conn net.PacketConn, packet []byte, addr net.Addr) error {
	kind, err := tunnel.PacketKind(packet)
	if err != nil {
		return err
	}
	switch kind {
	case tunnel.PacketProbe:
		// 探测包静默丢弃（服务器端行为，客户端会处理）
		return nil
	case tunnel.PacketRegisterAck:
		// 公网服务器的 UDP 注册确认仅用于打开和验证入站路径。
		return nil
	case tunnel.PacketHello:
		var hello tunnel.Hello
		if err := tunnel.UnmarshalControl(packet, &hello); err != nil {
			return err
		}
		return s.handleHello(conn, hello, addr)
	case tunnel.PacketFrame:
		return s.handleFrame(conn, packet, addr)
	default:
		return fmt.Errorf("unknown UDP packet kind %d", kind)
	}
}

// handleHello 处理客户端的握手请求。
// 流程：1) 验证 sessionID 和客户端身份；2) 解密 SM4 会话密钥；
// 3) 通过 DHCP 代理为客户端分配 IP；4) 发现局域网设备；
// 5) 构建并发送 Ready 帧；6) 创建 TUN 虚拟网卡。
func (s *udpService) handleHello(conn net.PacketConn, hello tunnel.Hello, addr net.Addr) error {
	s.mu.RLock()
	offer, ok := s.offers[hello.SessionID]
	existing := s.sessions[hello.SessionID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown punch session %s", hello.SessionID)
	}
	if hello.ClientDeviceID != offer.Client.DeviceID {
		return fmt.Errorf("client %s is not offered for session %s", hello.ClientDeviceID, hello.SessionID)
	}
	// Reply immediately from the socket that received Hello. Cellular NATs can
	// keep this return path open only briefly; DHCP and LAN setup happen later.
	if err := s.sendProbeReply(conn, offer, addr); err != nil {
		log.Printf("send immediate UDP probe reply for session %s from %s to %s: %v", hello.SessionID, conn.LocalAddr(), addr, err)
	}
	// 如果会话已存在（客户端重发 Hello），更新对端地址并重发 Ready
	if existing != nil {
		existing.mu.Lock()
		existing.conn = conn
		existing.peer = addr
		existing.seenAt = time.Now()
		ready := append([]byte(nil), existing.ready...)
		existing.mu.Unlock()
		return s.sendReadyBurst(existing, ready, "existing")
	}
	s.helloMu.Lock()
	defer s.helloMu.Unlock()
	s.mu.RLock()
	existing = s.sessions[hello.SessionID]
	s.mu.RUnlock()
	if existing != nil {
		existing.mu.Lock()
		existing.conn = conn
		existing.peer = addr
		existing.seenAt = time.Now()
		ready := append([]byte(nil), existing.ready...)
		existing.mu.Unlock()
		return s.sendReadyBurst(existing, ready, "locked-existing")
	}
	// 用 SM2 私钥解密会话密钥
	key, err := s.identity.Decrypt(hello.EncryptedSessionKey)
	if err != nil {
		return fmt.Errorf("decrypt session key: %w", err)
	}
	// 校验虚拟网段映射合法性
	if offer.Request.VirtualCIDR != "" {
		if err := lan.ValidateMappedCIDR(offer.Server.LANCIDR, offer.Request.VirtualCIDR); err != nil {
			return err
		}
	}
	// 通过 DHCP 代理为客户端分配局域网 IP
	homeLease := s.leaseClientIP(offer)
	if homeLease == nil || homeLease.IP == "" {
		return fmt.Errorf("DHCP proxy lease unavailable for session %s on interface %q", offer.SessionID, s.lanInterface())
	}

	// 构建 Ready 帧载荷
	ready, err := json.Marshal(tunnel.Ready{
		HomeDeviceID: offer.Server.DeviceID,
		LANCIDR:      offer.Server.LANCIDR,
		ClientHomeIP: homeLease.IP,
		Devices:      lan.Discover(offer.Server.LANCIDR, offer.Request.VirtualCIDR),
	})
	if err != nil {
		return err
	}
	session := &udpSession{offer: offer, key: key, peer: addr, conn: conn, ready: ready, seenAt: time.Now(), lease: homeLease}
	s.mu.Lock()
	if current := s.sessions[hello.SessionID]; current != nil {
		s.mu.Unlock()
		s.releaseLease(offer, homeLease)
		current.mu.Lock()
		current.conn = conn
		current.peer = addr
		current.seenAt = time.Now()
		replayedReady := append([]byte(nil), current.ready...)
		current.mu.Unlock()
		return s.sendReadyBurst(current, replayedReady, "raced-existing")
	}
	s.sessions[hello.SessionID] = session
	s.mu.Unlock()

	if err := s.sendReadyBurst(session, ready, "new"); err != nil {
		return err
	}
	// 创建 TUN 虚拟网卡，将隧道数据桥接到局域网
	if homeLease.IP != "" {
		link, err := newHomeLink(hello.SessionID, homeLease.IP, s.lanInterface(), func(packet []byte) error {
			if offer.Request.VirtualCIDR != "" {
				translated, err := lan.TranslateIPv4Subnet(packet, offer.Server.LANCIDR, offer.Request.VirtualCIDR)
				if err != nil {
					return err
				}
				packet = translated
			}
			return s.sendFrame(session, tunnel.FrameIPv4, packet)
		})
		if err != nil {
			log.Printf("home TUN path unavailable for session %s: %v", hello.SessionID, err)
		} else {
			session.link = link
		}
	}
	log.Printf("UDP tunnel ready: session=%s client=%s local=%s peer=%s", hello.SessionID, hello.ClientDeviceID, conn.LocalAddr(), addr.String())
	return nil
}

func (s *udpService) sendReadyBurst(session *udpSession, ready []byte, reason string) error {
	if err := s.sendFrame(session, tunnel.FrameReady, ready); err != nil {
		return err
	}
	sessionID := session.offer.SessionID
	go func() {
		ticker := time.NewTicker(readyBurstInterval)
		defer ticker.Stop()
		deadline := time.Now().Add(readyBurstDuration)
		sent := 1
		for time.Now().Before(deadline) {
			<-ticker.C
			if err := s.sendFrame(session, tunnel.FrameReady, ready); err != nil {
				log.Printf("UDP ready replay failed: session=%s reason=%s sent=%d: %v", sessionID, reason, sent, err)
				return
			}
			sent++
		}
		log.Printf("UDP ready replay finished: session=%s reason=%s sent=%d", sessionID, reason, sent)
	}()
	return nil
}

func (s *udpService) sendProbeReply(conn net.PacketConn, offer protocol.HolePunchOffer, addr net.Addr) error {
	packet, err := tunnel.MarshalProbe(tunnel.Probe{
		SessionID: offer.SessionID,
		DeviceID:  offer.Server.DeviceID,
		Role:      protocol.DeviceTypeHomeServer,
	})
	if err != nil {
		return err
	}
	_, err = conn.WriteTo(packet, addr)
	return err
}

func (s *udpService) releaseLease(offer protocol.HolePunchOffer, lease *lan.Lease) {
	if lease == nil {
		return
	}
	if lease.IP != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := lan.DisableProxyARP(ctx, s.lanInterface(), lease.IP); err != nil {
			log.Printf("proxy ARP cleanup unavailable for session %s IP %s: %v", offer.SessionID, lease.IP, err)
		}
		cancel()
	}
	lease.Release()
}

// leaseClientIP 通过 DHCP 代理为客户端分配局域网 IP，并启用代理 ARP。
// 返回的 *Lease 可通过 Release() 方法释放租约。
func (s *udpService) leaseClientIP(offer protocol.HolePunchOffer) *lan.Lease {
	if offer.Request.ClientVirtualMAC == "" {
		return nil
	}
	iface := s.lanInterface()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	lease, err := lan.RequestLease(ctx, iface, offer.Request.ClientVirtualMAC)
	if err != nil {
		log.Printf("DHCP proxy lease unavailable for session %s: %v", offer.SessionID, err)
		fallbackCtx, fallbackCancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer fallbackCancel()
		lease, err = lan.ReserveLocalLease(fallbackCtx, iface, offer.Server.LANCIDR, offer.Request.ClientVirtualMAC)
		if err != nil {
			log.Printf("local LAN lease fallback unavailable for session %s: %v", offer.SessionID, err)
			return nil
		}
		log.Printf("using local LAN lease fallback for session %s: ip=%s iface=%s cidr=%s", offer.SessionID, lease.IP, iface, offer.Server.LANCIDR)
	}
	arpCtx, arpCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer arpCancel()
	if err := lan.EnableProxyARP(arpCtx, iface, lease.IP); err != nil {
		log.Printf("proxy ARP unavailable for session %s IP %s: %v", offer.SessionID, lease.IP, err)
	}
	return lease
}

func (s *udpService) lanInterface() string {
	if s.iface != "" {
		return s.iface
	}
	return lan.Detect().Interface
}

// handleFrame 处理加密帧数据。
func (s *udpService) handleFrame(conn net.PacketConn, packet []byte, addr net.Addr) error {
	sessionID, err := frameSessionID(packet)
	if err != nil {
		return err
	}
	s.mu.RLock()
	session := s.sessions[sessionID]
	s.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("unknown secure frame session %s", sessionID)
	}
	frame, err := tunnel.Open(session.key, packet)
	if err != nil {
		return err
	}
	session.mu.Lock()
	if !session.replay.Accept(frame.Sequence) {
		session.mu.Unlock()
		return fmt.Errorf("secure frame sequence %d was already seen", frame.Sequence)
	}
	session.conn = conn
	session.peer = addr
	session.seenAt = time.Now()
	session.down += uint64(len(packet))
	session.mu.Unlock()

	switch frame.Type {
	case tunnel.FramePing, tunnel.FrameKeepalive:
		return s.sendFrame(session, tunnel.FramePong, frame.Payload)
	case tunnel.FrameIPv4:
		if session.link == nil {
			log.Printf("received IPv4 tunnel payload without LAN path: session=%s bytes=%d", frame.SessionID, len(frame.Payload))
			return nil
		}
		packet := frame.Payload
		var translateErr error
		if session.offer.Request.VirtualCIDR != "" {
			packet, translateErr = lan.TranslateIPv4Subnet(packet, session.offer.Request.VirtualCIDR, session.offer.Server.LANCIDR)
			if translateErr != nil {
				return translateErr
			}
		}
		return session.link.WritePacket(packet)
	case tunnel.FrameEthernet:
		// L2 以太网帧暂未实现桥接，记录日志后丢弃
		log.Printf("received ethernet frame (not yet supported): session=%s bytes=%d", frame.SessionID, len(frame.Payload))
		return nil
	default:
		return fmt.Errorf("unsupported secure frame type %d", frame.Type)
	}
}

// sendFrame 使用 SM4-GCM 加密帧数据并发送给对端。
func (s *udpService) sendFrame(session *udpSession, frameType byte, payload []byte) error {
	session.mu.Lock()
	session.sendSeq++
	sequence := session.sendSeq
	peer := session.peer
	conn := session.conn
	session.mu.Unlock()
	if conn == nil {
		conn = s.primaryConn()
	}

	packet, err := tunnel.Seal(session.key, session.offer.SessionID, sequence, frameType, payload)
	if err != nil {
		return err
	}
	_, err = conn.WriteTo(packet, peer)
	if err == nil {
		session.mu.Lock()
		session.up += uint64(len(packet))
		session.mu.Unlock()
	}
	return err
}

// trafficDelta 计算自上次上报以来的流量增量。
func (s *udpService) trafficDelta() trafficTotals {
	s.mu.RLock()
	sessions := make([]*udpSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.mu.RUnlock()

	var delta trafficTotals
	for _, session := range sessions {
		session.mu.Lock()
		delta.Up += counterDelta(session.up, session.report.Up)
		delta.Down += counterDelta(session.down, session.report.Down)
		session.report = trafficTotals{Up: session.up, Down: session.down}
		session.mu.Unlock()
	}
	return delta
}

// counterDelta 计算计数器增量，处理计数器重置的情况。
func counterDelta(current, previous uint64) uint64 {
	if current < previous {
		return current
	}
	return current - previous
}

// reapLoop 定期回收过期会话。
func (s *udpService) reapLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for now := range ticker.C {
		s.reapExpired(now)
	}
}

// reapExpired 清理超过 idleTimeout 的过期会话。
// 过期清理包括：1) 移除 ProxyARP 条目；2) 释放 DHCP 租约；3) 关闭 TUN 设备。
func (s *udpService) reapExpired(now time.Time) {
	const idleTimeout = 45 * time.Second
	var expired []*udpSession
	s.mu.Lock()
	for sessionID, session := range s.sessions {
		session.mu.Lock()
		stale := now.Sub(session.seenAt) > idleTimeout
		session.mu.Unlock()
		if !stale {
			continue
		}
		expired = append(expired, session)
		delete(s.sessions, sessionID)
		delete(s.offers, sessionID)
	}
	s.mu.Unlock()
	for _, session := range expired {
		s.cleanupSession(session)
	}
}

// cleanupSession 清理单个会话的所有运行时资源。
// 包括：移除 ProxyARP 条目、释放 DHCP 租约、关闭 TUN 设备。
func (s *udpService) cleanupSession(session *udpSession) {
	session.mu.Lock()
	link := session.link
	session.link = nil
	lease := session.lease
	offer := session.offer
	session.mu.Unlock()

	// 关闭 TUN 虚拟网卡
	if link != nil {
		_ = link.Close()
	}
	// 移除 ProxyARP 条目
	if lease != nil && lease.IP != "" {
		iface := s.iface
		if iface == "" {
			iface = lan.Detect().Interface
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := lan.DisableProxyARP(ctx, iface, lease.IP); err != nil {
			log.Printf("failed to remove proxy ARP for %s: %v", lease.IP, err)
		}
		cancel()
	}
	// 释放 DHCP 租约
	if lease != nil {
		lease.Release()
	}
	log.Printf("session cleaned up: session=%s client=%s", offer.SessionID, offer.Client.DeviceID)
}

// closeAll 关闭所有活跃会话，用于优雅关闭。
func (s *udpService) closeAll() {
	s.mu.Lock()
	sessions := make([]*udpSession, 0, len(s.sessions))
	for sessionID, session := range s.sessions {
		sessions = append(sessions, session)
		delete(s.sessions, sessionID)
		delete(s.offers, sessionID)
	}
	s.mu.Unlock()
	for _, session := range sessions {
		s.cleanupSession(session)
	}
}

// frameSessionID 从加密帧中提取 session ID，无需解密。
func frameSessionID(packet []byte) (string, error) {
	if kind, err := tunnel.PacketKind(packet); err != nil {
		return "", err
	} else if kind != tunnel.PacketFrame {
		return "", errors.New("packet is not a secure frame")
	}
	if len(packet) < 7 {
		return "", errors.New("frame header is incomplete")
	}
	sessionLen := int(packet[6])
	if len(packet) < 7+sessionLen {
		return "", errors.New("frame session id is incomplete")
	}
	return string(packet[7 : 7+sessionLen]), nil
}
