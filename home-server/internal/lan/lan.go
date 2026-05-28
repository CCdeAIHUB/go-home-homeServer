package lan

import (
	"net"
	"strings"
)

type Info struct {
	CIDR      string
	Gateway   string
	Interface string
}

func Detect() Info {
	ifaces, err := net.Interfaces()
	if err != nil {
		return Info{}
	}
	var candidates []Info
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualNonLANInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip, network, ok := parseIPv4Net(addr)
			if !ok || !isPrivate(ip) {
				continue
			}
			info := Info{CIDR: network.String(), Interface: iface.Name}
			if isPreferredLANInterface(iface.Name) {
				return info
			}
			candidates = append(candidates, info)
		}
	}
	for _, candidate := range candidates {
		if interfaceLooksLikeGateway(candidate.Interface, candidate.CIDR) {
			return candidate
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return Info{}
}

func isPreferredLANInterface(name string) bool {
	name = strings.ToLower(name)
	return name == "br-lan" || name == "lan" || name == "bridge0"
}

func isVirtualNonLANInterface(name string) bool {
	name = strings.ToLower(name)
	if name == "br-lan" {
		return false
	}
	return strings.HasPrefix(name, "docker") ||
		strings.HasPrefix(name, "veth") ||
		strings.HasPrefix(name, "tun") ||
		strings.HasPrefix(name, "tap") ||
		strings.HasPrefix(name, "wg") ||
		strings.HasPrefix(name, "zt") ||
		(strings.HasPrefix(name, "br-") && name != "br-lan")
}

func interfaceLooksLikeGateway(_, cidr string) bool {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	ip4 := ip.To4()
	return ip4 != nil && ip4[3] == 1
}

func parseIPv4Net(addr net.Addr) (net.IP, *net.IPNet, bool) {
	ipNet, ok := addr.(*net.IPNet)
	if !ok {
		return nil, nil, false
	}
	ip := ipNet.IP.To4()
	if ip == nil {
		return nil, nil, false
	}
	network := &net.IPNet{IP: ip.Mask(ipNet.Mask), Mask: ipNet.Mask}
	return ip, network, true
}

func isPrivate(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 10 ||
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
			(ip4[0] == 192 && ip4[1] == 168)
	}
	return false
}
