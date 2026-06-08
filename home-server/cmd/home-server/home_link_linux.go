//go:build linux

package main

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/tun"
)

const homeTunPacketOffset = 10

type homeLink struct {
	device tun.Device
	name   string
	cancel context.CancelFunc
	once   sync.Once
}

func newHomeLink(sessionID, clientIP, lanIface string, send func([]byte) error) (packetLink, error) {
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
	if err := allowHomeLinkFirewall(actualName, clientIP, lanIface); err != nil {
		_ = device.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	link := &homeLink{device: device, name: actualName, cancel: cancel}
	go link.readLoop(ctx, send)
	return link, nil
}

func (l *homeLink) WritePacket(packet []byte) error {
	buf := make([]byte, homeTunPacketOffset+len(packet))
	copy(buf[homeTunPacketOffset:], packet)
	_, err := l.device.Write([][]byte{buf}, homeTunPacketOffset)
	return err
}

func (l *homeLink) Close() error {
	var err error
	l.once.Do(func() {
		l.cancel()
		cleanupHomeLinkFirewall(l.name)
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

func allowHomeLinkFirewall(name, clientIP, lanIface string) error {
	if name == "" {
		return nil
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return nil
	}
	if err := exec.Command("nft", "list", "table", "inet", "fw4").Run(); err != nil {
		return nil
	}
	cleanupHomeLinkFirewall(name)
	rules := []string{
		"insert rule inet fw4 input iifname " + strconv.Quote(name) + " accept comment " + nftComment(name, "input"),
		"insert rule inet fw4 forward iifname " + strconv.Quote(name) + " accept comment " + nftComment(name, "forward-in"),
		"insert rule inet fw4 forward oifname " + strconv.Quote(name) + " accept comment " + nftComment(name, "forward-out"),
	}
	if lanIface != "" {
		rules = append(rules,
			"insert rule inet fw4 forward iifname "+strconv.Quote(name)+" oifname "+strconv.Quote(lanIface)+" accept comment "+nftComment(name, "forward-lan-in"),
			"insert rule inet fw4 forward iifname "+strconv.Quote(lanIface)+" oifname "+strconv.Quote(name)+" accept comment "+nftComment(name, "forward-lan-out"),
			"insert rule inet fw4 srcnat ip saddr "+clientIP+"/32 oifname != "+strconv.Quote(lanIface)+" masquerade comment "+nftComment(name, "srcnat"),
		)
	} else {
		rules = append(rules,
			"insert rule inet fw4 srcnat ip saddr "+clientIP+"/32 oifname != "+strconv.Quote(name)+" masquerade comment "+nftComment(name, "srcnat"),
		)
	}
	for _, rule := range rules {
		if err := applyNFTRule(rule); err != nil {
			return err
		}
	}
	if err := allowPasswallDNSBypass(name); err != nil {
		return err
	}
	return nil
}

func allowPasswallDNSBypass(name string) error {
	if name == "" {
		return nil
	}
	if err := exec.Command("nft", "list", "chain", "inet", "passwall", "PSW_DNS").Run(); err != nil {
		return nil
	}
	rule := "insert rule inet passwall PSW_DNS iifname " + strconv.Quote(name) + " return comment " + nftComment(name, "passwall-dns-bypass")
	if err := exec.Command("nft", "list", "set", "inet", "passwall", "passwall_lan").Run(); err == nil {
		rule = "insert rule inet passwall PSW_DNS iifname " + strconv.Quote(name) + " ip daddr != @passwall_lan return comment " + nftComment(name, "passwall-dns-bypass")
	}
	return applyNFTRule(rule)
}

func applyNFTRule(rule string) error {
	command := exec.Command("nft", "-f", "-")
	command.Stdin = strings.NewReader(rule + "\n")
	out, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft apply %q: %w (%s)", rule, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func cleanupHomeLinkFirewall(name string) {
	if name == "" {
		return
	}
	deleteCommentedNFTRules("input", name)
	deleteCommentedNFTRules("forward", name)
	deleteCommentedNFTRules("srcnat", name)
	deleteCommentedNFTRulesFromTable("passwall", "PSW_DNS", name)
}

func deleteCommentedNFTRules(chain, name string) {
	deleteCommentedNFTRulesFromTable("fw4", chain, name)
}

func deleteCommentedNFTRulesFromTable(table, chain, name string) {
	out, err := exec.Command("nft", "-a", "list", "chain", "inet", table, chain).CombinedOutput()
	if err != nil {
		return
	}
	commentPrefix := firewallComment(name, "")
	handleRe := regexp.MustCompile(`\s# handle ([0-9]+)$`)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, commentPrefix) {
			continue
		}
		match := handleRe.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		_ = exec.Command("nft", "delete", "rule", "inet", table, chain, "handle", match[1]).Run()
	}
}

func firewallComment(name, suffix string) string {
	if suffix == "" {
		return "go-home:" + name + ":"
	}
	return "go-home:" + name + ":" + suffix
}

func nftComment(name, suffix string) string {
	return strconv.Quote(firewallComment(name, suffix))
}
