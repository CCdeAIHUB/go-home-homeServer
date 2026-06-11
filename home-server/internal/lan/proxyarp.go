package lan

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
)

func EnableProxyARP(ctx context.Context, iface, ip string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("proxy ARP requires Linux")
	}
	if iface == "" {
		return fmt.Errorf("LAN interface is required for proxy ARP")
	}
	if parsed := net.ParseIP(ip).To4(); parsed == nil {
		return fmt.Errorf("invalid proxy ARP IPv4 address %q", ip)
	}
	if err := exec.CommandContext(ctx, "sysctl", "-w", "net.ipv4.conf."+iface+".proxy_arp=1").Run(); err != nil {
		return fmt.Errorf("enable proxy ARP on %s: %w", iface, err)
	}
	if err := exec.CommandContext(ctx, "ip", "neigh", "replace", "proxy", ip, "dev", iface).Run(); err != nil {
		return fmt.Errorf("install proxy ARP neighbor: %w", err)
	}
	return nil
}

func DisableProxyARP(ctx context.Context, iface, ip string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("proxy ARP requires Linux")
	}
	if iface == "" {
		return fmt.Errorf("LAN interface is required for proxy ARP")
	}
	if err := exec.CommandContext(ctx, "ip", "neigh", "del", "proxy", ip, "dev", iface).Run(); err != nil {
		return fmt.Errorf("remove proxy ARP neighbor: %w", err)
	}
	return nil
}
