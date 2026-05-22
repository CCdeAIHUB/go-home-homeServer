//go:build linux

package lan

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
)

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
	// release 内部 DHCP 租约对象，用于释放。
	release *nclient4.Lease
}

// Release 释放 DHCP 租约，将 IP 归还给路由器。
// 应在隧道会话结束时调用，避免 IP 地址在路由器上一直占用到租期自然过期。
func (l Lease) Release() {
	if l.release == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.release.Release(ctx); err != nil {
		// 释放失败不影响主流程，仅记录日志
		fmt.Printf("DHCP lease release failed for %s: %v\n", l.IP, err)
	}
}

// RequestLease 通过 DHCP 协议向局域网路由器申请 IP 租约。
// iface 为局域网网卡名称，mac 为客户端虚拟 MAC 地址。
// 超时 5 秒，最多重试 2 次。
func RequestLease(ctx context.Context, iface, mac string) (Lease, error) {
	if iface == "" {
		return Lease{}, fmt.Errorf("LAN interface is required for DHCP proxy lease")
	}
	hardwareAddr, err := net.ParseMAC(mac)
	if err != nil {
		return Lease{}, fmt.Errorf("invalid client virtual MAC: %w", err)
	}
	client, err := nclient4.New(
		iface,
		nclient4.WithHWAddr(hardwareAddr),
		nclient4.WithTimeout(5*time.Second),
		nclient4.WithRetry(2),
	)
	if err != nil {
		return Lease{}, err
	}
	defer client.Close()

	lease, err := client.Request(ctx)
	if err != nil {
		return Lease{}, err
	}
	ack := lease.ACK
	result := Lease{
		IP:        ack.YourIPAddr.String(),
		Netmask:   net.IP(ack.SubnetMask()).String(),
		ExpiresAt: lease.CreationTime.Add(ack.IPAddressLeaseTime(time.Hour)),
		release:   lease,
	}
	if routers := ack.Router(); len(routers) > 0 {
		result.Gateway = routers[0].String()
	}
	for _, server := range ack.DNS() {
		result.DNS = append(result.DNS, server.String())
	}
	return result, nil
}
