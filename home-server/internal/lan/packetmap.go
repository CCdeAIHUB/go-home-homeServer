package lan

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

// TranslateIPv4Subnet maps both packet endpoints between equal-size /24 CIDRs.
func TranslateIPv4Subnet(packet []byte, fromCIDR, toCIDR string) ([]byte, error) {
	from, to, err := mappedPrefixes(fromCIDR, toCIDR)
	if err != nil {
		return nil, err
	}
	if len(packet) < 20 || packet[0]>>4 != 4 {
		return nil, fmt.Errorf("packet is not IPv4")
	}
	headerLen := int(packet[0]&0x0f) * 4
	if headerLen < 20 || len(packet) < headerLen {
		return nil, fmt.Errorf("IPv4 header is incomplete")
	}

	totalLen := int(binary.BigEndian.Uint16(packet[2:4]))
	if totalLen < headerLen || totalLen > len(packet) {
		totalLen = len(packet)
	}
	translated := append([]byte(nil), packet...)
	changed := mapPacketAddr(translated[12:16], from, to)
	changed = mapPacketAddr(translated[16:20], from, to) || changed
	if !changed {
		return translated, nil
	}

	writeChecksum(translated[10:12], translated[:headerLen])
	if !isWholeTransportPacket(translated) {
		return translated, nil
	}
	rewriteTransportChecksum(translated[:totalLen], headerLen)
	return translated, nil
}

func mappedPrefixes(fromCIDR, toCIDR string) (netip.Prefix, netip.Prefix, error) {
	from, err := netip.ParsePrefix(fromCIDR)
	if err != nil {
		return netip.Prefix{}, netip.Prefix{}, fmt.Errorf("invalid source CIDR: %w", err)
	}
	to, err := netip.ParsePrefix(toCIDR)
	if err != nil {
		return netip.Prefix{}, netip.Prefix{}, fmt.Errorf("invalid target CIDR: %w", err)
	}
	if !from.Addr().Is4() || !to.Addr().Is4() || from.Bits() != 24 || to.Bits() != 24 {
		return netip.Prefix{}, netip.Prefix{}, fmt.Errorf("IPv4 packet mapping requires /24 CIDRs")
	}
	return from.Masked(), to.Masked(), nil
}

func mapPacketAddr(raw []byte, from, to netip.Prefix) bool {
	if len(raw) != 4 {
		return false
	}
	addr := netip.AddrFrom4([4]byte{raw[0], raw[1], raw[2], raw[3]})
	if !from.Contains(addr) {
		return false
	}
	mapped := to.Addr().As4()
	mapped[3] = raw[3]
	copy(raw, mapped[:])
	return true
}

func isWholeTransportPacket(packet []byte) bool {
	flagsOffset := binary.BigEndian.Uint16(packet[6:8])
	return flagsOffset&0x3fff == 0
}

func rewriteTransportChecksum(packet []byte, headerLen int) {
	payload := packet[headerLen:]
	switch packet[9] {
	case 6:
		if len(payload) < 20 {
			return
		}
		writeChecksum(payload[16:18], pseudoPacket(packet, payload, 16))
	case 17:
		if len(payload) < 8 || binary.BigEndian.Uint16(payload[6:8]) == 0 {
			return
		}
		writeChecksum(payload[6:8], pseudoPacket(packet, payload, 6))
		if binary.BigEndian.Uint16(payload[6:8]) == 0 {
			binary.BigEndian.PutUint16(payload[6:8], 0xffff)
		}
	}
}

func pseudoPacket(packet, payload []byte, checksumOffset int) []byte {
	pseudo := make([]byte, 12+len(payload))
	copy(pseudo[0:4], packet[12:16])
	copy(pseudo[4:8], packet[16:20])
	pseudo[9] = packet[9]
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(payload)))
	copy(pseudo[12:], payload)
	if checksumOffset >= 0 && 12+checksumOffset+1 < len(pseudo) {
		pseudo[12+checksumOffset] = 0
		pseudo[12+checksumOffset+1] = 0
	}
	return pseudo
}

func writeChecksum(field []byte, body []byte) {
	field[0], field[1] = 0, 0
	binary.BigEndian.PutUint16(field, checksum(body))
}

func checksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + sum>>16
	}
	return ^uint16(sum)
}
