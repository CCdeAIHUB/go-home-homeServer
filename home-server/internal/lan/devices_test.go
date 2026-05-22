package lan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadARPTableAndMapNeighbors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arp")
	if err := os.WriteFile(path, []byte(`IP address       HW type     Flags       HW address            Mask     Device
192.168.3.5      0x1         0x2         00:11:22:33:44:55     *        br-lan
192.168.3.22     0x1         0x2         aa:bb:cc:dd:ee:ff     *        br-lan
10.0.0.4         0x1         0x2         00:00:00:00:00:01     *        eth0
`), 0o600); err != nil {
		t.Fatalf("write arp fixture: %v", err)
	}
	neighbors, err := ReadARPTable(path, "192.168.3.0/24")
	if err != nil {
		t.Fatalf("read arp table: %v", err)
	}
	devices := MapNeighbors(neighbors, "192.168.3.0/24", "192.168.6.0/24")
	if len(devices) != 2 {
		t.Fatalf("device count got %d want 2", len(devices))
	}
	if devices[0].RealIP != "192.168.3.5" || devices[0].VirtualIP != "192.168.6.5" {
		t.Fatalf("unexpected first mapping: %+v", devices[0])
	}
}

func TestValidateMappedCIDR(t *testing.T) {
	if err := ValidateMappedCIDR("192.168.3.0/24", "192.168.6.0/24"); err != nil {
		t.Fatalf("valid mapped CIDR: %v", err)
	}
	if err := ValidateMappedCIDR("192.168.3.0/16", "192.168.6.0/24"); err == nil {
		t.Fatal("expected non /24 real CIDR to fail")
	}
}
