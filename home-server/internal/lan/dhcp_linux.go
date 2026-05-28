//go:build linux

package lan

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
)

var fallbackLeaseReservations sync.Map

// Lease 表示 DHCP 代理获取的 IP 租约。
type Lease struct {
	// IP 分配给客户端的局域网 IP 地址。
	IP string
	// Netmask 子网掩码。
	Netmask string
	// Gateway 默认网关地址。
	Gateway string
	// DNS DNS 服务器地址列表。
	DNS []string
	// ExpiresAt 租约过期时间。
	ExpiresAt time.Time
	// client 内部 DHCP 客户端，用于释放租约。
	client *nclient4.Client
	// lease 内部 DHCP 租约对象，用于释放。
	lease *nclient4.Lease
	// local marks an in-process fallback lease that must be released from memory only.
	local bool
}

// Release 释放 DHCP 租约，将 IP 归还给路由器。
// 应在隧道会话结束时调用，避免 IP 地址在路由器上一直占用到租期自然过期。
// 注意：必须在 Linux 平台上调用，需要通过 nclient4.Client.Release() 方法释放。
func (l *Lease) Release() {
	if l.local {
		fallbackLeaseReservations.Delete(l.IP)
		return
	}
	if l.client == nil || l.lease == nil {
		return
	}
	if err := l.client.Release(l.lease); err != nil {
		// 释放失败不影响主流程，仅记录日志
		fmt.Printf("DHCP lease release failed for %s: %v\n", l.IP, err)
	}
	l.client.Close()
	l.client = nil
	l.lease = nil
}

// RequestLease 通过 DHCP 协议向局域网路由器申请 IP 租约。
// iface 为局域网网卡名称，mac 为客户端虚拟 MAC 地址。
// 超时 5 秒，最多重试 2 次。
// 返回的 Lease 可通过 Release() 方法释放，将 IP 归还给路由器。
func RequestLease(ctx context.Context, iface, mac string) (*Lease, error) {
	if iface == "" {
		return nil, fmt.Errorf("LAN interface is required for DHCP proxy lease")
	}
	hardwareAddr, err := net.ParseMAC(mac)
	if err != nil {
		return nil, fmt.Errorf("invalid client virtual MAC: %w", err)
	}
	client, err := nclient4.New(
		iface,
		nclient4.WithHWAddr(hardwareAddr),
		nclient4.WithTimeout(5*time.Second),
		nclient4.WithRetry(2),
	)
	if err != nil {
		return nil, err
	}

	lease, err := client.Request(ctx)
	if err != nil {
		client.Close()
		return nil, err
	}
	ack := lease.ACK
	result := &Lease{
		IP:        ack.YourIPAddr.String(),
		Netmask:   net.IP(ack.SubnetMask()).String(),
		ExpiresAt: lease.CreationTime.Add(ack.IPAddressLeaseTime(time.Hour)),
		client:    client,
		lease:     lease,
	}
	if routers := ack.Router(); len(routers) > 0 {
		result.Gateway = routers[0].String()
	}
	for _, server := range ack.DNS() {
		result.DNS = append(result.DNS, server.String())
	}
	return result, nil
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
		return nil, fmt.Errorf("local lease fallback currently requires an IPv4 /24 LAN CIDR")
	}

	gateway := interfaceIPv4(iface, prefix)
	used := usedIPv4Set(cidr)
	if gateway.IsValid() {
		used[gateway.String()] = true
	}

	for _, candidate := range fallbackCandidates(prefix) {
		ip := candidate.String()
		if used[ip] {
			continue
		}
		if _, loaded := fallbackLeaseReservations.LoadOrStore(ip, mac); loaded {
			continue
		}
		if !isIPv4Available(ctx, iface, ip) {
			fallbackLeaseReservations.Delete(ip)
			continue
		}
		return &Lease{
			IP:        ip,
			Netmask:   net.IPv4(255, 255, 255, 0).String(),
			Gateway:   gateway.String(),
			DNS:       dnsServers(gateway),
			ExpiresAt: time.Now().Add(12 * time.Hour),
			local:     true,
		}, nil
	}
	return nil, fmt.Errorf("no available local IPv4 address in %s", cidr)
}

func fallbackCandidates(prefix netip.Prefix) []netip.Addr {
	base := prefix.Masked().Addr().As4()
	var out []netip.Addr
	addRange := func(start, end, step int) {
		for host := start; ; host += step {
			if host <= 0 || host >= 255 {
				if host == end {
					break
				}
			} else {
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

func usedIPv4Set(cidr string) map[string]bool {
	used := make(map[string]bool)
	primeARP(cidr)
	if neighbors, err := ReadARPTable("/proc/net/arp", cidr); err == nil {
		for _, neighbor := range neighbors {
			used[neighbor.IP.String()] = true
		}
	}
	for _, path := range []string{"/tmp/dhcp.leases", "/var/lib/misc/dnsmasq.leases"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 3 && net.ParseIP(fields[2]).To4() != nil {
				used[fields[2]] = true
			}
		}
	}
	return used
}

func interfaceIPv4(iface string, prefix netip.Prefix) netip.Addr {
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

func dnsServers(gateway netip.Addr) []string {
	if gateway.IsValid() {
		return []string{gateway.String()}
	}
	return nil
}

func isIPv4Available(ctx context.Context, iface, ip string) bool {
	if _, err := exec.LookPath("arping"); err != nil {
		return true
	}
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, "arping", "-D", "-I", iface, "-c", "2", "-w", "2", ip)
	return cmd.Run() == nil
}
