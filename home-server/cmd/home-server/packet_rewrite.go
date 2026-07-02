package main

import (
	"encoding/binary"
	"fmt"
	"net"
)

func rewriteIPv4Endpoints(packet []byte, from, to net.IP) ([]byte, bool, error) {
	from4 := from.To4()
	to4 := to.To4()
	if from4 == nil || to4 == nil {
		return nil, false, fmt.Errorf("invalid IPv4 rewrite endpoint")
	}
	if len(packet) < 20 || packet[0]>>4 != 4 {
		return nil, false, fmt.Errorf("packet is not IPv4")
	}
	headerLen := int(packet[0]&0x0f) * 4
	if headerLen < 20 || len(packet) < headerLen {
		return nil, false, fmt.Errorf("IPv4 header is incomplete")
	}
	totalLen := int(binary.BigEndian.Uint16(packet[2:4]))
	if totalLen < headerLen || totalLen > len(packet) {
		totalLen = len(packet)
	}
	out := append([]byte(nil), packet...)
	changed := rewriteIPv4Addr(out[12:16], from4, to4)
	changed = rewriteIPv4Addr(out[16:20], from4, to4) || changed
	if !changed {
		return out, false, nil
	}
	writeIPv4Checksum(out[10:12], out[:headerLen])
	if ipv4IsWholeTransportPacket(out) {
		rewriteIPv4TransportChecksum(out[:totalLen], headerLen)
	}
	return out, true, nil
}

func rewriteIPv4Addr(raw []byte, from, to net.IP) bool {
	if len(raw) != 4 || !rawIPv4Equal(raw, from) {
		return false
	}
	copy(raw, to.To4())
	return true
}

func rawIPv4Equal(raw []byte, ip net.IP) bool {
	ip4 := ip.To4()
	if len(raw) != 4 || ip4 == nil {
		return false
	}
	return raw[0] == ip4[0] && raw[1] == ip4[1] && raw[2] == ip4[2] && raw[3] == ip4[3]
}

func ipv4IsWholeTransportPacket(packet []byte) bool {
	flagsOffset := binary.BigEndian.Uint16(packet[6:8])
	return flagsOffset&0x3fff == 0
}

func rewriteIPv4TransportChecksum(packet []byte, headerLen int) {
	payload := packet[headerLen:]
	switch packet[9] {
	case 6:
		if len(payload) < 20 {
			return
		}
		writeIPv4Checksum(payload[16:18], ipv4PseudoPacket(packet, payload, 16))
	case 17:
		if len(payload) < 8 || binary.BigEndian.Uint16(payload[6:8]) == 0 {
			return
		}
		writeIPv4Checksum(payload[6:8], ipv4PseudoPacket(packet, payload, 6))
		if binary.BigEndian.Uint16(payload[6:8]) == 0 {
			binary.BigEndian.PutUint16(payload[6:8], 0xffff)
		}
	}
}

func ipv4PseudoPacket(packet, payload []byte, checksumOffset int) []byte {
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

func writeIPv4Checksum(field []byte, body []byte) {
	field[0], field[1] = 0, 0
	binary.BigEndian.PutUint16(field, ipv4Checksum(body))
}

func ipv4Checksum(data []byte) uint16 {
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
