//go:build linux

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/tun"
)

const homeTunPacketOffset = 10
const homeDNSServiceIP = "100.64.77.1"

type homeLink struct {
	device     tun.Device
	name       string
	cancel     context.CancelFunc
	dnsRelease func()
	once       sync.Once
}

func newHomeLink(sessionID, clientIP, lanIface string, send func([]byte) error) (packetLink, error) {
	name := "gh" + sessionID[:min(10, len(sessionID))]
	device, err := tun.CreateTUN(name, tunnelMTU)
	if err != nil {
		return nil, fmt.Errorf("create home TUN: %w", err)
	}
	actualName, err := device.Name()
	if err != nil {
		_ = device.Close()
		return nil, fmt.Errorf("read home TUN name: %w", err)
	}
	if err := configureHomeLink(actualName, clientIP); err != nil {
		_ = device.Close()
		return nil, err
	}
	if err := allowHomeLinkFirewall(actualName, clientIP, lanIface); err != nil {
		_ = device.Close()
		return nil, err
	}
	dnsRelease, err := startHomeDNSProxy()
	if err != nil {
		_ = device.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	link := &homeLink{device: device, name: actualName, cancel: cancel, dnsRelease: dnsRelease}
	go link.readLoop(ctx, send)
	return link, nil
}

func (l *homeLink) WritePacket(packet []byte) error {
	buf := make([]byte, homeTunPacketOffset+len(packet))
	copy(buf[homeTunPacketOffset:], packet)
	_, err := l.device.Write([][]byte{buf}, homeTunPacketOffset)
	return err
}

func (l *homeLink) Close() error {
	var err error
	l.once.Do(func() {
		l.cancel()
		cleanupHomeLinkFirewall(l.name)
		if l.dnsRelease != nil {
			l.dnsRelease()
		}
		err = l.device.Close()
	})
	return err
}

func (l *homeLink) readLoop(ctx context.Context, send func([]byte) error) {
	batch := l.device.BatchSize()
	bufs := make([][]byte, batch)
	sizes := make([]int, batch)
	for i := range bufs {
		bufs[i] = make([]byte, 64*1024)
	}
	for {
		n, err := l.device.Read(bufs, sizes, 0)
		if err != nil {
			return
		}
		for i := 0; i < n; i++ {
			if sizes[i] <= 0 {
				continue
			}
			select {
			case <-ctx.Done():
				return
			default:
				_ = send(bufs[i][:sizes[i]])
			}
		}
	}
}

func configureHomeLink(name, clientIP string) error {
	commands := [][]string{
		{"ip", "link", "set", "dev", name, "mtu", fmt.Sprintf("%d", tunnelMTU), "up"},
		{"ip", "addr", "replace", homeDNSServiceIP + "/32", "dev", "lo"},
		{"ip", "route", "replace", clientIP + "/32", "dev", name},
		{"sysctl", "-w", "net.ipv4.ip_forward=1"},
	}
	for _, command := range commands {
		if out, err := exec.Command(command[0], command[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w (%s)", strings.Join(command, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func allowHomeLinkFirewall(name, clientIP, lanIface string) error {
	if name == "" {
		return nil
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return nil
	}
	if err := exec.Command("nft", "list", "table", "inet", "fw4").Run(); err != nil {
		return nil
	}
	cleanupHomeLinkFirewall(name)
	rules := []string{
		"insert rule inet fw4 input iifname " + strconv.Quote(name) + " accept comment " + nftComment(name, "input"),
		"insert rule inet fw4 forward iifname " + strconv.Quote(name) + " accept comment " + nftComment(name, "forward-in"),
		"insert rule inet fw4 forward oifname " + strconv.Quote(name) + " accept comment " + nftComment(name, "forward-out"),
	}
	if lanIface != "" {
		rules = append(rules,
			"insert rule inet fw4 forward iifname "+strconv.Quote(name)+" oifname "+strconv.Quote(lanIface)+" accept comment "+nftComment(name, "forward-lan-in"),
			"insert rule inet fw4 forward iifname "+strconv.Quote(lanIface)+" oifname "+strconv.Quote(name)+" accept comment "+nftComment(name, "forward-lan-out"),
			"insert rule inet fw4 srcnat ip saddr "+clientIP+"/32 oifname != "+strconv.Quote(lanIface)+" masquerade comment "+nftComment(name, "srcnat"),
		)
	} else {
		rules = append(rules,
			"insert rule inet fw4 srcnat ip saddr "+clientIP+"/32 oifname != "+strconv.Quote(name)+" masquerade comment "+nftComment(name, "srcnat"),
		)
	}
	for _, rule := range rules {
		if err := applyNFTRule(rule); err != nil {
			return err
		}
	}
	if err := allowPasswallDNSBypass(name); err != nil {
		return err
	}
	return nil
}

func allowPasswallDNSBypass(name string) error {
	if name == "" {
		return nil
	}
	if err := exec.Command("nft", "list", "chain", "inet", "passwall", "PSW_DNS").Run(); err != nil {
		return nil
	}
	rule := "insert rule inet passwall PSW_DNS iifname " + strconv.Quote(name) + " return comment " + nftComment(name, "passwall-dns-bypass")
	return applyNFTRule(rule)
}

func applyNFTRule(rule string) error {
	command := exec.Command("nft", "-f", "-")
	command.Stdin = strings.NewReader(rule + "\n")
	out, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft apply %q: %w (%s)", rule, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func cleanupHomeLinkFirewall(name string) {
	if name == "" {
		return
	}
	deleteCommentedNFTRules("input", name)
	deleteCommentedNFTRules("forward", name)
	deleteCommentedNFTRules("srcnat", name)
	deleteCommentedNFTRulesFromTable("passwall", "PSW_DNS", name)
}

func deleteCommentedNFTRules(chain, name string) {
	deleteCommentedNFTRulesFromTable("fw4", chain, name)
}

func deleteCommentedNFTRulesFromTable(table, chain, name string) {
	out, err := exec.Command("nft", "-a", "list", "chain", "inet", table, chain).CombinedOutput()
	if err != nil {
		return
	}
	commentPrefix := firewallComment(name, "")
	handleRe := regexp.MustCompile(`\s# handle ([0-9]+)$`)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, commentPrefix) {
			continue
		}
		match := handleRe.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		_ = exec.Command("nft", "delete", "rule", "inet", table, chain, "handle", match[1]).Run()
	}
}

func firewallComment(name, suffix string) string {
	if suffix == "" {
		return "go-home:" + name + ":"
	}
	return "go-home:" + name + ":" + suffix
}

func nftComment(name, suffix string) string {
	return strconv.Quote(firewallComment(name, suffix))
}

var homeDNSProxy = &dnsProxyManager{}

type dnsProxyManager struct {
	mu     sync.Mutex
	refs   int
	cancel context.CancelFunc
}

func startHomeDNSProxy() (func(), error) {
	homeDNSProxy.mu.Lock()
	defer homeDNSProxy.mu.Unlock()
	if homeDNSProxy.refs > 0 {
		homeDNSProxy.refs++
		return releaseHomeDNSProxy, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	udpConn, err := net.ListenPacket("udp", net.JoinHostPort(homeDNSServiceIP, "53"))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start full-home DNS UDP proxy: %w", err)
	}
	tcpLn, err := net.Listen("tcp", net.JoinHostPort(homeDNSServiceIP, "53"))
	if err != nil {
		_ = udpConn.Close()
		cancel()
		return nil, fmt.Errorf("start full-home DNS TCP proxy: %w", err)
	}
	homeDNSProxy.refs = 1
	homeDNSProxy.cancel = func() {
		cancel()
		_ = udpConn.Close()
		_ = tcpLn.Close()
	}
	go serveHomeDNSUDP(ctx, udpConn)
	go serveHomeDNSTCP(ctx, tcpLn)
	log.Printf("full-home DNS proxy listening on %s:53", homeDNSServiceIP)
	return releaseHomeDNSProxy, nil
}

func releaseHomeDNSProxy() {
	homeDNSProxy.mu.Lock()
	defer homeDNSProxy.mu.Unlock()
	if homeDNSProxy.refs <= 0 {
		return
	}
	homeDNSProxy.refs--
	if homeDNSProxy.refs == 0 && homeDNSProxy.cancel != nil {
		homeDNSProxy.cancel()
		homeDNSProxy.cancel = nil
	}
}

func serveHomeDNSUDP(ctx context.Context, conn net.PacketConn) {
	buf := make([]byte, 4096)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("full-home DNS UDP read failed: %v", err)
			}
			return
		}
		query := append([]byte(nil), buf[:n]...)
		go func() {
			resp, err := forwardHomeDNSUDP(ctx, query)
			if err != nil {
				log.Printf("full-home DNS UDP forward failed: %v", err)
				return
			}
			_, _ = conn.WriteTo(resp, addr)
		}()
	}
}

func forwardHomeDNSUDP(ctx context.Context, query []byte) ([]byte, error) {
	var lastErr error
	for _, upstream := range homeDNSUpstreams() {
		dialer := net.Dialer{Timeout: 2 * time.Second}
		conn, err := dialer.DialContext(ctx, "udp", upstream)
		if err != nil {
			lastErr = err
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
		if _, err = conn.Write(query); err != nil {
			lastErr = err
			_ = conn.Close()
			continue
		}
		resp := make([]byte, 4096)
		n, err := conn.Read(resp)
		_ = conn.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return resp[:n], nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no DNS upstreams configured")
	}
	return nil, lastErr
}

func serveHomeDNSTCP(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("full-home DNS TCP accept failed: %v", err)
			}
			return
		}
		go handleHomeDNSTCP(ctx, conn)
	}
}

func handleHomeDNSTCP(ctx context.Context, client net.Conn) {
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(8 * time.Second))
	var lenBuf [2]byte
	if _, err := io.ReadFull(client, lenBuf[:]); err != nil {
		return
	}
	size := binary.BigEndian.Uint16(lenBuf[:])
	if size == 0 || size > 4096 {
		return
	}
	query := make([]byte, int(size))
	if _, err := io.ReadFull(client, query); err != nil {
		return
	}
	resp, err := forwardHomeDNSTCP(ctx, query)
	if err != nil {
		log.Printf("full-home DNS TCP forward failed: %v", err)
		return
	}
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(resp)))
	_, _ = client.Write(lenBuf[:])
	_, _ = client.Write(resp)
}

func forwardHomeDNSTCP(ctx context.Context, query []byte) ([]byte, error) {
	var lastErr error
	for _, upstream := range homeDNSUpstreams() {
		dialer := net.Dialer{Timeout: 2 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", upstream)
		if err != nil {
			lastErr = err
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
		var lenBuf [2]byte
		binary.BigEndian.PutUint16(lenBuf[:], uint16(len(query)))
		if _, err = conn.Write(append(lenBuf[:], query...)); err != nil {
			lastErr = err
			_ = conn.Close()
			continue
		}
		if _, err = io.ReadFull(conn, lenBuf[:]); err != nil {
			lastErr = err
			_ = conn.Close()
			continue
		}
		size := binary.BigEndian.Uint16(lenBuf[:])
		if size == 0 || size > 4096 {
			lastErr = fmt.Errorf("invalid DNS TCP response size %d", size)
			_ = conn.Close()
			continue
		}
		resp := make([]byte, int(size))
		if _, err = io.ReadFull(conn, resp); err != nil {
			lastErr = err
			_ = conn.Close()
			continue
		}
		_ = conn.Close()
		return resp, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no DNS upstreams configured")
	}
	return nil, lastErr
}

func homeDNSUpstreams() []string {
	return []string{
		"127.0.0.1:11400",
		"127.0.0.1:53",
	}
}
