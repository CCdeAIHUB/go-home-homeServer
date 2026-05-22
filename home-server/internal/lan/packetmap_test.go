package lan

import (
	"encoding/binary"
	"testing"
)

func TestTranslateIPv4SubnetRewritesUDPChecksums(t *testing.T) {
	packet := udpPacket([4]byte{192, 168, 6, 100}, [4]byte{192, 168, 6, 5})
	beforeUDP := binary.BigEndian.Uint16(packet[26:28])

	translated, err := TranslateIPv4Subnet(packet, "192.168.6.0/24", "192.168.3.0/24")
	if err != nil {
		t.Fatalf("translate packet: %v", err)
	}
	if got := translated[12:20]; string(got) != string([]byte{192, 168, 3, 100, 192, 168, 3, 5}) {
		t.Fatalf("translated endpoints got %v", got)
	}
	if got := checksum(translated[:20]); got != 0 {
		t.Fatalf("IPv4 checksum verification got %#x want 0", got)
	}
	if afterUDP := binary.BigEndian.Uint16(translated[26:28]); afterUDP == 0 || afterUDP == beforeUDP {
		t.Fatalf("UDP checksum was not rewritten: before=%#x after=%#x", beforeUDP, afterUDP)
	}
}

func TestTranslateIPv4SubnetPreservesUnmappedEndpoints(t *testing.T) {
	packet := udpPacket([4]byte{10, 0, 0, 2}, [4]byte{172, 16, 0, 8})
	translated, err := TranslateIPv4Subnet(packet, "192.168.6.0/24", "192.168.3.0/24")
	if err != nil {
		t.Fatalf("translate packet: %v", err)
	}
	if string(translated) != string(packet) {
		t.Fatal("unmapped packet changed")
	}
}

func udpPacket(src, dst [4]byte) []byte {
	packet := make([]byte, 32)
	packet[0] = 0x45
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	packet[8] = 64
	packet[9] = 17
	copy(packet[12:16], src[:])
	copy(packet[16:20], dst[:])
	binary.BigEndian.PutUint16(packet[20:22], 40000)
	binary.BigEndian.PutUint16(packet[22:24], 443)
	binary.BigEndian.PutUint16(packet[24:26], 12)
	copy(packet[28:], []byte{1, 2, 3, 4})
	writeChecksum(packet[10:12], packet[:20])
	writeChecksum(packet[26:28], pseudoPacket(packet, packet[20:]))
	return packet
}
