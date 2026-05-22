package tunnel

import (
	"bytes"
	"testing"
)

func TestSecureFrameRoundTrip(t *testing.T) {
	key, err := NewSessionKey()
	if err != nil {
		t.Fatalf("new key: %v", err)
	}
	packet, err := Seal(key, "session-1", 7, FramePing, []byte("hello"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	frame, err := Open(key, packet)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if frame.SessionID != "session-1" || frame.Sequence != 7 || frame.Type != FramePing {
		t.Fatalf("unexpected frame metadata: %+v", frame)
	}
	if !bytes.Equal(frame.Payload, []byte("hello")) {
		t.Fatalf("unexpected payload: %q", frame.Payload)
	}
}

func TestControlPacketRoundTrip(t *testing.T) {
	packet, err := MarshalHello(Hello{
		SessionID:           "session-2",
		ClientDeviceID:      "client-a",
		EncryptedSessionKey: []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	if kind, err := PacketKind(packet); err != nil || kind != PacketHello {
		t.Fatalf("unexpected kind %d err %v", kind, err)
	}
	var hello Hello
	if err := UnmarshalControl(packet, &hello); err != nil {
		t.Fatalf("unmarshal hello: %v", err)
	}
	if hello.SessionID != "session-2" || hello.ClientDeviceID != "client-a" {
		t.Fatalf("unexpected hello: %+v", hello)
	}
}
