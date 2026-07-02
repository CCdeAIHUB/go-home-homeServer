package main

import (
	"net"
	"testing"
)

func TestRewriteIPv4EndpointsUpdatesAddresses(t *testing.T) {
	packet := []byte{
		0x45, 0x00, 0x00, 0x1c,
		0x00, 0x00, 0x00, 0x00,
		0x40, 0x11, 0x00, 0x00,
		192, 168, 3, 50,
		192, 168, 3, 1,
		0x30, 0x39, 0x00, 0x35,
		0x00, 0x08, 0x00, 0x00,
	}
	writeIPv4Checksum(packet[10:12], packet[:20])

	translated, changed, err := rewriteIPv4Endpoints(packet, net.IPv4(192, 168, 3, 50), net.IPv4(100, 64, 78, 50))
	if err != nil {
		t.Fatalf("rewriteIPv4Endpoints: %v", err)
	}
	if !changed {
		t.Fatalf("rewriteIPv4Endpoints reported unchanged")
	}
	if got := net.IP(translated[12:16]).String(); got != "100.64.78.50" {
		t.Fatalf("source IP = %s", got)
	}
	if got := ipv4Checksum(translated[:20]); got != 0 {
		t.Fatalf("IPv4 checksum verification got %#x want 0", got)
	}
}

func TestRewriteIPv4EndpointsNoMatch(t *testing.T) {
	packet := []byte{
		0x45, 0x00, 0x00, 0x14,
		0x00, 0x00, 0x00, 0x00,
		0x40, 0x01, 0x00, 0x00,
		192, 168, 3, 20,
		192, 168, 3, 1,
	}
	writeIPv4Checksum(packet[10:12], packet[:20])

	translated, changed, err := rewriteIPv4Endpoints(packet, net.IPv4(192, 168, 3, 50), net.IPv4(100, 64, 78, 50))
	if err != nil {
		t.Fatalf("rewriteIPv4Endpoints: %v", err)
	}
	if changed {
		t.Fatalf("rewriteIPv4Endpoints reported changed")
	}
	if got := net.IP(translated[12:16]).String(); got != "192.168.3.20" {
		t.Fatalf("source IP = %s", got)
	}
}
