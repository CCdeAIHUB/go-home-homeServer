//go:build windows

package lan

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

var windowsLeaseReservations sync.Map

type Lease struct {
	IP        string
	Netmask   string
	Gateway   string
	DNS       []string
	ExpiresAt time.Time
	local     bool
}

func (l *Lease) Release() {
	if l.local {
		windowsLeaseReservations.Delete(l.IP)
	}
}

func RequestLease(_ context.Context, _, _ string) (*Lease, error) {
	return nil, fmt.Errorf("DHCP proxy lease is not available on Windows")
}

func ReserveLocalLease(ctx context.Context, iface, cidr, mac string) (*Lease, error) {
	if iface == "" {
		return nil, fmt.Errorf("LAN interface is required for local lease")
	}
	if _, err := net.ParseMAC(mac); err != nil {
		return nil, fmt.Errorf("invalid client virtual MAC: %w", err)
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid LAN CIDR: %w", err)
	}
	if !prefix.Addr().Is4() || prefix.Bits() != 24 {
		return nil, fmt.Errorf("Windows local lease currently requires an IPv4 /24 LAN CIDR")
	}
	gateway := windowsInterfaceIPv4(iface, prefix)
	used := windowsUsedIPv4Set(ctx, cidr)
	if gateway.IsValid() {
		used[gateway.String()] = true
	}
	for _, candidate := range windowsFallbackCandidates(prefix) {
		ip := candidate.String()
		if used[ip] {
			continue
		}
		if _, loaded := windowsLeaseReservations.LoadOrStore(ip, mac); loaded {
			continue
		}
		if !windowsIPv4Available(ctx, ip) {
			windowsLeaseReservations.Delete(ip)
			continue
		}
		return &Lease{
			IP:        ip,
			Netmask:   net.IPv4(255, 255, 255, 0).String(),
			Gateway:   gateway.String(),
			DNS:       windowsDNSServers(gateway),
			ExpiresAt: time.Now().Add(12 * time.Hour),
			local:     true,
		}, nil
	}
	return nil, fmt.Errorf("no available local IPv4 address in %s", cidr)
}

func windowsFallbackCandidates(prefix netip.Prefix) []netip.Addr {
	base := prefix.Masked().Addr().As4()
	var out []netip.Addr
	addRange := func(start, end, step int) {
		for host := start; ; host += step {
			if host > 0 && host < 255 {
				addr := base
				addr[3] = byte(host)
				out = append(out, netip.AddrFrom4(addr))
			}
			if host == end {
				break
			}
		}
	}
	addRange(254, 250, -1)
	addRange(99, 2, -1)
	addRange(249, 100, -1)
	return out
}

func windowsInterfaceIPv4(iface string, prefix netip.Prefix) netip.Addr {
	item, err := net.InterfaceByName(iface)
	if err != nil {
		return netip.Addr{}
	}
	addrs, err := item.Addrs()
	if err != nil {
		return netip.Addr{}
	}
	for _, raw := range addrs {
		ip, _, ok := parseIPv4Net(raw)
		if !ok {
			continue
		}
		addr, ok := netip.AddrFromSlice(ip)
		if ok && prefix.Contains(addr) {
			return addr
		}
	}
	return netip.Addr{}
}

func windowsUsedIPv4Set(ctx context.Context, cidr string) map[string]bool {
	used := make(map[string]bool)
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return used
	}
	out, err := exec.CommandContext(ctx, "arp", "-a").CombinedOutput()
	if err != nil {
		return used
	}
	ipRe := regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	for _, match := range ipRe.FindAllString(string(out), -1) {
		addr, err := netip.ParseAddr(strings.TrimSpace(match))
		if err == nil && prefix.Contains(addr) {
			used[addr.String()] = true
		}
	}
	return used
}

func windowsIPv4Available(ctx context.Context, ip string) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, "ping", "-n", "1", "-w", "250", ip)
	return cmd.Run() != nil
}

func windowsDNSServers(gateway netip.Addr) []string {
	if gateway.IsValid() {
		return []string{gateway.String()}
	}
	return nil
}
