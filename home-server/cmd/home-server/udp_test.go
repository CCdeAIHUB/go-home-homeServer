package main

import (
	"testing"
	"time"

	"gohome/shared/protocol"
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
