//go:build linux

package lan

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
)

type Lease struct {
	IP        string
	Netmask   string
	Gateway   string
	DNS       []string
	ExpiresAt time.Time
	release   *nclient4.Lease
}

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
