//go:build linux

package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/tun"
)

type homeLink struct {
	device tun.Device
	cancel context.CancelFunc
	once   sync.Once
}

func newHomeLink(sessionID, clientIP string, send func([]byte) error) (packetLink, error) {
	name := "gh" + sessionID[:min(10, len(sessionID))]
	device, err := tun.CreateTUN(name, tunnelMTU)
	if err != nil {
		return nil, fmt.Errorf("create home TUN: %w", err)
	}
	actualName, err := device.Name()
	if err != nil {
		_ = device.Close()
		return nil, fmt.Errorf("read home TUN name: %w", err)
	}
	if err := configureHomeLink(actualName, clientIP); err != nil {
		_ = device.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	link := &homeLink{device: device, cancel: cancel}
	go link.readLoop(ctx, send)
	return link, nil
}

func (l *homeLink) WritePacket(packet []byte) error {
	_, err := l.device.Write([][]byte{packet}, 0)
	return err
}

func (l *homeLink) Close() error {
	var err error
	l.once.Do(func() {
		l.cancel()
		err = l.device.Close()
	})
	return err
}

func (l *homeLink) readLoop(ctx context.Context, send func([]byte) error) {
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
			select {
			case <-ctx.Done():
				return
			default:
				_ = send(bufs[i][:sizes[i]])
			}
		}
	}
}

func configureHomeLink(name, clientIP string) error {
	commands := [][]string{
		{"ip", "link", "set", "dev", name, "mtu", fmt.Sprintf("%d", tunnelMTU), "up"},
		{"ip", "route", "replace", clientIP + "/32", "dev", name},
		{"sysctl", "-w", "net.ipv4.ip_forward=1"},
	}
	for _, command := range commands {
		if out, err := exec.Command(command[0], command[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w (%s)", strings.Join(command, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
