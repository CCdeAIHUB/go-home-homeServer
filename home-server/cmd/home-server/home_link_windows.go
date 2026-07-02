//go:build windows

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/tun"
)

const windowsHomeNatPrefix = "100.64.78.0/24"
const windowsHomeNatGateway = "100.64.78.1"

type windowsHomeLink struct {
	device   tun.Device
	name     string
	clientIP net.IP
	natIP    net.IP
	cancel   context.CancelFunc
	once     sync.Once
}

func newHomeLink(sessionID, clientIP, lanIface string, send func([]byte) error) (packetLink, error) {
	client := net.ParseIP(clientIP).To4()
	if client == nil {
		return nil, fmt.Errorf("invalid client IPv4 address %q", clientIP)
	}
	natIP, err := windowsClientNATIP(clientIP)
	if err != nil {
		return nil, err
	}
	name := "GoHome-" + sessionID[:min(10, len(sessionID))]
	device, err := tun.CreateTUN(name, tunnelMTU)
	if err != nil {
		return nil, fmt.Errorf("create Wintun adapter: %w", err)
	}
	actualName, err := device.Name()
	if err != nil {
		_ = device.Close()
		return nil, fmt.Errorf("read Wintun adapter name: %w", err)
	}
	if err := configureWindowsHomeLink(actualName, lanIface); err != nil {
		_ = device.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	link := &windowsHomeLink{device: device, name: actualName, clientIP: client, natIP: natIP, cancel: cancel}
	go link.readLoop(ctx, send)
	log.Printf("Windows home link ready: adapter=%s client=%s nat=%s", actualName, clientIP, natIP)
	return link, nil
}

func (l *windowsHomeLink) WritePacket(packet []byte) error {
	translated, _, err := rewriteIPv4Endpoints(packet, l.clientIP, l.natIP)
	if err != nil {
		return err
	}
	_, err = l.device.Write([][]byte{translated}, 0)
	return err
}

func (l *windowsHomeLink) Close() error {
	var err error
	l.once.Do(func() {
		l.cancel()
		cleanupWindowsHomeNAT()
		err = l.device.Close()
	})
	return err
}

func (l *windowsHomeLink) readLoop(ctx context.Context, send func([]byte) error) {
	batch := l.device.BatchSize()
	bufs := make([][]byte, batch)
	sizes := make([]int, batch)
	for i := range bufs {
		bufs[i] = make([]byte, 64*1024)
	}
	for {
		n, err := l.device.Read(bufs, sizes, 0)
		if err != nil {
			return
		}
		for i := 0; i < n; i++ {
			if sizes[i] <= 0 {
				continue
			}
			packet := append([]byte(nil), bufs[i][:sizes[i]]...)
			translated, _, err := rewriteIPv4Endpoints(packet, l.natIP, l.clientIP)
			if err != nil {
				log.Printf("Windows home link packet rewrite failed: %v", err)
				continue
			}
			select {
			case <-ctx.Done():
				return
			default:
				_ = send(translated)
			}
		}
	}
}

func windowsClientNATIP(clientIP string) (net.IP, error) {
	parsed := net.ParseIP(clientIP).To4()
	if parsed == nil {
		return nil, fmt.Errorf("invalid client IPv4 address %q", clientIP)
	}
	host := int(parsed[3])
	if host == 0 || host == 1 || host == 255 {
		host = 200
	}
	return net.IPv4(100, 64, 78, byte(host)).To4(), nil
}

func configureWindowsHomeLink(name, lanIface string) error {
	commands := [][]string{
		{"netsh", "interface", "ipv4", "set", "interface", name, "forwarding=enabled", "weakhostreceive=enabled", "weakhostsend=enabled"},
		{"netsh", "interface", "ipv4", "set", "address", "name=" + name, "static", windowsHomeNatGateway, "255.255.255.0"},
	}
	if lanIface != "" {
		commands = append(commands, []string{"netsh", "interface", "ipv4", "set", "interface", lanIface, "forwarding=enabled", "weakhostreceive=enabled", "weakhostsend=enabled"})
	}
	for _, command := range commands {
		if out, err := exec.Command(command[0], command[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w (%s)", strings.Join(command, " "), err, strings.TrimSpace(string(out)))
		}
	}
	if err := ensureWindowsHomeNAT(); err != nil {
		return err
	}
	return nil
}

func ensureWindowsHomeNAT() error {
	cleanup := `Get-NetNat -Name GoHomeHomeServer -ErrorAction SilentlyContinue | Remove-NetNat -Confirm:$false`
	create := `New-NetNat -Name GoHomeHomeServer -InternalIPInterfaceAddressPrefix ` + strconv.Quote(windowsHomeNatPrefix)
	if out, err := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", cleanup+"; "+create).CombinedOutput(); err != nil {
		return fmt.Errorf("configure Windows NAT: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func cleanupWindowsHomeNAT() {
	_ = exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", `Get-NetNat -Name GoHomeHomeServer -ErrorAction SilentlyContinue | Remove-NetNat -Confirm:$false`).Run()
}
