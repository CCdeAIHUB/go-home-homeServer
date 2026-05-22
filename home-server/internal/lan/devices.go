package lan

import (
	"bufio"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"gohome/shared/tunnel"
)

type Neighbor struct {
	IP  netip.Addr
	MAC string
}

func Discover(cidr, virtualCIDR string) []tunnel.DeviceMap {
	primeARP(cidr)
	neighbors, err := ReadARPTable("/proc/net/arp", cidr)
	if err != nil {
		return nil
	}
	return MapNeighbors(neighbors, cidr, virtualCIDR)
}

func ReadARPTable(path, cidr string) ([]Neighbor, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var neighbors []Neighbor
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || fields[0] == "IP" {
			continue
		}
		ip := net.ParseIP(fields[0]).To4()
		if ip == nil || !network.Contains(ip) || fields[3] == "00:00:00:00:00:00" {
			continue
		}
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		neighbors = append(neighbors, Neighbor{IP: addr, MAC: fields[3]})
	}
	return neighbors, scanner.Err()
}

func MapNeighbors(neighbors []Neighbor, realCIDR, virtualCIDR string) []tunnel.DeviceMap {
	var devices []tunnel.DeviceMap
	for _, neighbor := range neighbors {
		devices = append(devices, tunnel.DeviceMap{
			RealIP:    neighbor.IP.String(),
			VirtualIP: mapIP(neighbor.IP, realCIDR, virtualCIDR),
			MAC:       neighbor.MAC,
		})
	}
	return devices
}

func mapIP(ip netip.Addr, realCIDR, virtualCIDR string) string {
	if virtualCIDR == "" {
		return ip.String()
	}
	realPrefix, err := netip.ParsePrefix(realCIDR)
	if err != nil || realPrefix.Bits() != 24 || !realPrefix.Contains(ip) {
		return ip.String()
	}
	virtualPrefix, err := netip.ParsePrefix(virtualCIDR)
	if err != nil || virtualPrefix.Bits() != 24 || !virtualPrefix.Addr().Is4() {
		return ip.String()
	}
	virtual := virtualPrefix.Masked().Addr().As4()
	virtual[3] = ip.As4()[3]
	return netip.AddrFrom4(virtual).String()
}

func primeARP(cidr string) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || prefix.Bits() != 24 || !prefix.Addr().Is4() {
		return
	}
	base := prefix.Masked().Addr().As4()
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(700 * time.Millisecond))
	for host := 1; host < 255; host++ {
		addr := base
		addr[3] = byte(host)
		_, _ = conn.WriteTo([]byte{0}, &net.UDPAddr{IP: net.IP(addr[:]), Port: 9})
	}
}

func ValidateMappedCIDR(realCIDR, virtualCIDR string) error {
	real, err := netip.ParsePrefix(realCIDR)
	if err != nil {
		return fmt.Errorf("invalid real CIDR: %w", err)
	}
	virtual, err := netip.ParsePrefix(virtualCIDR)
	if err != nil {
		return fmt.Errorf("invalid virtual CIDR: %w", err)
	}
	if real.Bits() != 24 || virtual.Bits() != 24 || !real.Addr().Is4() || !virtual.Addr().Is4() {
		return fmt.Errorf("mapped mode currently requires IPv4 /24 CIDRs")
	}
	return nil
}
