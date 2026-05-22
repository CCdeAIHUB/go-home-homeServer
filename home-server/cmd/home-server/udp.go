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

type udpService struct {
	conn     net.PacketConn
	identity *security.Identity
	iface    string

	mu       sync.RWMutex
	offers   map[string]protocol.HolePunchOffer
	sessions map[string]*udpSession
}

type udpSession struct {
	mu      sync.Mutex
	offer   protocol.HolePunchOffer
	key     []byte
	peer    net.Addr
	sendSeq uint64
	link    packetLink
	ready   []byte
}

type packetLink interface {
	WritePacket([]byte) error
	Close() error
}

func newUDPService(conn net.PacketConn, identity *security.Identity, iface string) *udpService {
	return &udpService{
		conn:     conn,
		identity: identity,
		iface:    iface,
		offers:   map[string]protocol.HolePunchOffer{},
		sessions: map[string]*udpSession{},
	}
}

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

func (s *udpService) readLoop() {
	buf := make([]byte, 64*1024)
	for {
		n, addr, err := s.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		packet := append([]byte(nil), buf[:n]...)
		if err := s.handlePacket(packet, addr); err != nil {
			log.Printf("udp packet rejected from %s: %v", addr.String(), err)
		}
	}
}

func (s *udpService) handlePacket(packet []byte, addr net.Addr) error {
	kind, err := tunnel.PacketKind(packet)
	if err != nil {
		return err
	}
	switch kind {
	case tunnel.PacketProbe:
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
	if existing != nil {
		existing.mu.Lock()
		existing.peer = addr
		ready := append([]byte(nil), existing.ready...)
		existing.mu.Unlock()
		return s.sendFrame(existing, tunnel.FrameReady, ready)
	}
	key, err := s.identity.Decrypt(hello.EncryptedSessionKey)
	if err != nil {
		return fmt.Errorf("decrypt session key: %w", err)
	}
	if offer.Request.VirtualCIDR != "" {
		if err := lan.ValidateMappedCIDR(offer.Server.LANCIDR, offer.Request.VirtualCIDR); err != nil {
			return err
		}
	}
	homeLease := s.leaseClientIP(offer)

	ready, err := json.Marshal(tunnel.Ready{
		HomeDeviceID: offer.Server.DeviceID,
		LANCIDR:      offer.Server.LANCIDR,
		ClientHomeIP: homeLease.IP,
		Devices:      lan.Discover(offer.Server.LANCIDR, offer.Request.VirtualCIDR),
	})
	if err != nil {
		return err
	}
	session := &udpSession{offer: offer, key: key, peer: addr, ready: ready}
	s.mu.Lock()
	if current := s.sessions[hello.SessionID]; current != nil {
		s.mu.Unlock()
		current.mu.Lock()
		current.peer = addr
		replayedReady := append([]byte(nil), current.ready...)
		current.mu.Unlock()
		return s.sendFrame(current, tunnel.FrameReady, replayedReady)
	}
	s.sessions[hello.SessionID] = session
	s.mu.Unlock()

	if err := s.sendFrame(session, tunnel.FrameReady, ready); err != nil {
		return err
	}
	if homeLease.IP != "" {
		link, err := newHomeLink(hello.SessionID, homeLease.IP, func(packet []byte) error {
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
	session.peer = addr
	session.mu.Unlock()

	switch frame.Type {
	case tunnel.FramePing, tunnel.FrameKeepalive:
		return s.sendFrame(session, tunnel.FramePong, frame.Payload)
	case tunnel.FrameIPv4:
		if session.link == nil {
			log.Printf("received IPv4 tunnel payload without LAN path: session=%s bytes=%d", frame.SessionID, len(frame.Payload))
			return nil
		}
		return session.link.WritePacket(frame.Payload)
	case tunnel.FrameEthernet:
		log.Printf("received tunnel payload: session=%s frame=%d bytes=%d", frame.SessionID, frame.Type, len(frame.Payload))
		return nil
	default:
		return fmt.Errorf("unsupported secure frame type %d", frame.Type)
	}
}

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
	return err
}

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
