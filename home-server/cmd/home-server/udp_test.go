package main

import (
	"net"
	"testing"
	"time"

	"gohome/shared/protocol"
	"gohome/shared/tunnel"
)

type closeRecorder struct {
	closed bool
}

func (r *closeRecorder) WritePacket([]byte) error { return nil }

func (r *closeRecorder) Close() error {
	r.closed = true
	return nil
}

func TestReapExpiredClosesIdleSessionLink(t *testing.T) {
	link := &closeRecorder{}
	service := &udpService{
		offers: map[string]protocol.HolePunchOffer{"session": {}},
		sessions: map[string]*udpSession{
			"session": {link: link, seenAt: time.Unix(1, 0)},
		},
	}
	service.reapExpired(time.Unix(60, 0))
	if len(service.sessions) != 0 || len(service.offers) != 0 {
		t.Fatalf("idle session was not removed: sessions=%d offers=%d", len(service.sessions), len(service.offers))
	}
	if !link.closed {
		t.Fatal("idle session link was not closed")
	}
}

func TestInstallOfferReplacesExistingClientSession(t *testing.T) {
	link := &closeRecorder{}
	service := &udpService{
		offers: map[string]protocol.HolePunchOffer{
			"old": {SessionID: "old", Client: protocol.PeerCandidate{DeviceID: "client-a"}},
		},
		sessions: map[string]*udpSession{
			"old": {
				offer: protocol.HolePunchOffer{SessionID: "old", Client: protocol.PeerCandidate{DeviceID: "client-a"}},
				link:  link,
			},
		},
	}

	replaced := service.installOffer(protocol.HolePunchOffer{
		SessionID: "new",
		Client:    protocol.PeerCandidate{DeviceID: "client-a"},
	})
	if len(replaced) != 1 {
		t.Fatalf("expected one replaced session, got %d", len(replaced))
	}
	if _, ok := service.sessions["old"]; ok {
		t.Fatal("old session was not detached")
	}
	if _, ok := service.offers["old"]; ok {
		t.Fatal("old offer was not detached")
	}
	if _, ok := service.offers["new"]; !ok {
		t.Fatal("new offer was not installed")
	}

	service.cleanupSession(replaced[0])
	if !link.closed {
		t.Fatal("replaced session link was not closed")
	}
}

func TestPeerCandidateEndpointsUsesServerListFirst(t *testing.T) {
	peer := protocol.PeerCandidate{
		Candidates:       []string{"203.0.113.4:50001", "203.0.113.4:50001", "[2001:db8::1]:50001"},
		ObservedEndpoint: "203.0.113.4:50002",
		Endpoint:         "198.51.100.9:47777",
		RemoteAddr:       "198.51.100.10:44321",
		UDPPort:          47777,
	}
	got := peerCandidateEndpoints(peer)
	wantPrefix := []string{
		"203.0.113.4:50001",
		"203.0.113.4:50002",
		"198.51.100.9:47777",
		"203.0.113.4:47777",
		"198.51.100.10:47777",
	}
	if len(got) < len(wantPrefix) {
		t.Fatalf("peerCandidateEndpoints got too few endpoints: %#v", got)
	}
	for i, want := range wantPrefix {
		if got[i] != want {
			t.Fatalf("peerCandidateEndpoints[%d] got %q want %q; all=%#v", i, got[i], want, got)
		}
	}
	if !containsEndpoint(got, "203.0.113.4:50003") || !containsEndpoint(got, "203.0.113.4:50000") {
		t.Fatalf("peerCandidateEndpoints did not include predicted adjacent ports: %#v", got)
	}
}

func TestExpandUDPCandidatesSpreadsAdjacentPortsAcrossBaseEndpoints(t *testing.T) {
	got := expandUDPCandidates([]*net.UDPAddr{
		{IP: net.ParseIP("203.0.113.4"), Port: 5000},
		{IP: net.ParseIP("203.0.113.4"), Port: 6000},
	}, 2)
	want := []string{
		"203.0.113.4:5000",
		"203.0.113.4:6000",
		"203.0.113.4:5001",
		"203.0.113.4:4999",
		"203.0.113.4:6001",
		"203.0.113.4:5999",
	}
	if len(got) < len(want) {
		t.Fatalf("expandUDPCandidates got too few candidates: %#v", got)
	}
	for index, endpoint := range want {
		if got[index].String() != endpoint {
			t.Fatalf("expandUDPCandidates[%d] got %q want %q; all=%#v", index, got[index], endpoint, got)
		}
	}
}

func TestSendProbeReplyUsesReceivingSocket(t *testing.T) {
	sender, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	receiver, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()

	offer := protocol.HolePunchOffer{
		SessionID: "session",
		Server: protocol.PeerCandidate{
			DeviceID: "home-server",
		},
	}
	if err := (&udpService{}).sendProbeReply(sender, offer, receiver.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, 1024)
	n, addr, err := receiver.ReadFrom(packet)
	if err != nil {
		t.Fatal(err)
	}
	if addr.String() != sender.LocalAddr().String() {
		t.Fatalf("probe reply source got %q want %q", addr, sender.LocalAddr())
	}
	var probe tunnel.Probe
	if err := tunnel.UnmarshalControl(packet[:n], &probe); err != nil {
		t.Fatal(err)
	}
	if probe.SessionID != offer.SessionID || probe.DeviceID != offer.Server.DeviceID {
		t.Fatalf("probe reply got %#v", probe)
	}
}

func TestFullPortSweepBatchStartsAfterPredictionAndRotates(t *testing.T) {
	base := []*net.UDPAddr{
		{IP: net.ParseIP("203.0.113.4"), Port: 5000},
		{IP: net.ParseIP("203.0.113.4"), Port: 6000},
	}
	if got := fullPortSweepBatch(base, fullPortSweepStartAttempt-1, 3); len(got) != 0 {
		t.Fatalf("fullPortSweepBatch started before fallback stage: %#v", got)
	}
	first := fullPortSweepBatch(base, fullPortSweepStartAttempt, 3)
	second := fullPortSweepBatch(base, fullPortSweepStartAttempt+1, 3)
	wantFirst := []string{"203.0.113.4:1", "203.0.113.4:2", "203.0.113.4:3"}
	wantSecond := []string{"203.0.113.4:4", "203.0.113.4:5", "203.0.113.4:6"}
	for index, want := range wantFirst {
		if first[index].String() != want {
			t.Fatalf("fullPortSweepBatch first[%d] got %q want %q", index, first[index], want)
		}
	}
	for index, want := range wantSecond {
		if second[index].String() != want {
			t.Fatalf("fullPortSweepBatch second[%d] got %q want %q", index, second[index], want)
		}
	}
}

func containsEndpoint(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
