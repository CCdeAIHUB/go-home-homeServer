package lan

import "net"

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
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
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
			return Info{CIDR: network.String(), Interface: iface.Name}
		}
	}
	return Info{}
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
