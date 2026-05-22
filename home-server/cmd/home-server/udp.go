package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"gohome/home-server/internal/lan"
	"gohome/shared/protocol"
	"gohome/shared/security"
	"gohome/shared/tunnel"
)

// udpService 管理 UDP 隧道的所有会话，负责：
//   - 接受服务器的打洞邀请，向客户端发送探测包
//   - 读取并处理 UDP 数据包（探测、握手、加密帧）
//   - 维护活跃隧道会话（SM4 密钥、对端地址、流量统计）
//   - 定期回收过期会话，释放 DHCP 租约和 ProxyARP 条目
type udpService struct {
	// conn UDP 监听连接。
	conn net.PacketConn
	// identity SM2 身份，用于解密客户端发送的会话密钥。
	identity *security.Identity
	// iface 局域网网卡名称，用于 DHCP 代理和 ProxyARP。
	iface string

	// mu 保护 offers 和 sessions 的并发访问。
	mu sync.RWMutex
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
	lease lan.Lease
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
func newUDPService(conn net.PacketConn, identity *security.Identity, iface string) *udpService {
	service := &udpService{
		conn:     conn,
		identity: identity,
		iface:    iface,
		offers:   map[string]protocol.HolePunchOffer{},
		sessions: map[string]*udpSession{},
	}
	go service.reapLoop()
	return service
}

// acceptOffer 接受服务器的打洞邀请，向客户端发送 UDP 探测包。
// 探测包发送 5 次，递增延迟（120ms × attempt），用于 P2P 打洞。
func (s *udpService) acceptOffer(offer protocol.HolePunchOffer) {
	s.mu.Lock()
	s.offers[offer.SessionID] = offer
	s.mu.Unlock()

	addr, err := net.ResolveUDPAddr("udp", offer.Client.Endpoint)
	if err != nil {
		log.Printf("resolve client endpoint for session %s: %v", offer.SessionID, err)
		return
	}
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
		for attempt := 0; attempt < 5; attempt++ {
			if _, err := s.conn.WriteTo(packet, addr); err != nil {
				log.Printf("send UDP probe for session %s: %v", offer.SessionID, err)
				return
			}
			time.Sleep(time.Duration(attempt+1) * 120 * time.Millisecond)
		}
	}()
}

// readLoop 持续读取 UDP 数据包并分发给处理函数。
// 遇到临时性错误时记录日志并继续，遇到致命错误时退出。
func (s *udpService) readLoop() {
	buf := make([]byte, 64*1024)
	for {
		n, addr, err := s.conn.ReadFrom(buf)
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
		if err := s.handlePacket(packet, addr); err != nil {
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
func (s *udpService) handlePacket(packet []byte, addr net.Addr) error {
	kind, err := tunnel.PacketKind(packet)
	if err != nil {
		return err
	}
	switch kind {
	case tunnel.PacketProbe:
		// 探测包静默丢弃（服务器端行为，客户端会处理）
		return nil
	case tunnel.PacketHello:
		var hello tunnel.Hello
		if err := tunnel.UnmarshalControl(packet, &hello); err != nil {
			return err
		}
		return s.handleHello(hello, addr)
	case tunnel.PacketFrame:
		return s.handleFrame(packet, addr)
	default:
		return fmt.Errorf("unknown UDP packet kind %d", kind)
	}
}

// handleHello 处理客户端的握手请求。
// 流程：1) 验证 sessionID 和客户端身份；2) 解密 SM4 会话密钥；
// 3) 通过 DHCP 代理为客户端分配 IP；4) 发现局域网设备；
// 5) 构建并发送 Ready 帧；6) 创建 TUN 虚拟网卡。
func (s *udpService) handleHello(hello tunnel.Hello, addr net.Addr) error {
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
	// 如果会话已存在（客户端重发 Hello），更新对端地址并重发 Ready
	if existing != nil {
		existing.mu.Lock()
		existing.peer = addr
		existing.seenAt = time.Now()
		ready := append([]byte(nil), existing.ready...)
		existing.mu.Unlock()
		return s.sendFrame(existing, tunnel.FrameReady, ready)
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
	session := &udpSession{offer: offer, key: key, peer: addr, ready: ready, seenAt: time.Now(), lease: homeLease}
	s.mu.Lock()
	if current := s.sessions[hello.SessionID]; current != nil {
		s.mu.Unlock()
		current.mu.Lock()
		current.peer = addr
		current.seenAt = time.Now()
		replayedReady := append([]byte(nil), current.ready...)
		current.mu.Unlock()
		return s.sendFrame(current, tunnel.FrameReady, replayedReady)
	}
	s.sessions[hello.SessionID] = session
	s.mu.Unlock()

	if err := s.sendFrame(session, tunnel.FrameReady, ready); err != nil {
		return err
	}
	// 创建 TUN 虚拟网卡，将隧道数据桥接到局域网
	if homeLease.IP != "" {
		link, err := newHomeLink(hello.SessionID, homeLease.IP, func(packet []byte) error {
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
	log.Printf("UDP tunnel ready: session=%s client=%s peer=%s", hello.SessionID, hello.ClientDeviceID, addr.String())
	return nil
}

// leaseClientIP 通过 DHCP 代理为客户端分配局域网 IP，并启用代理 ARP。
func (s *udpService) leaseClientIP(offer protocol.HolePunchOffer) lan.Lease {
	if offer.Request.ClientVirtualMAC == "" {
		return lan.Lease{}
	}
	iface := s.iface
	if iface == "" {
		iface = lan.Detect().Interface
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	lease, err := lan.RequestLease(ctx, iface, offer.Request.ClientVirtualMAC)
	if err != nil {
		log.Printf("DHCP proxy lease unavailable for session %s: %v", offer.SessionID, err)
		return lan.Lease{}
	}
	if err := lan.EnableProxyARP(ctx, iface, lease.IP); err != nil {
		log.Printf("proxy ARP unavailable for session %s IP %s: %v", offer.SessionID, lease.IP, err)
	}
	return lease
}

// handleFrame 处理加密帧数据。
func (s *udpService) handleFrame(packet []byte, addr net.Addr) error {
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
	session.mu.Unlock()

	packet, err := tunnel.Seal(session.key, session.offer.SessionID, sequence, frameType, payload)
	if err != nil {
		return err
	}
	_, err = s.conn.WriteTo(packet, peer)
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
	if lease.IP != "" {
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
	if lease.IP != "" {
		lease.Release()
	}
	log.Printf("session expired and cleaned up: session=%s client=%s", offer.SessionID, offer.Client.DeviceID)
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
