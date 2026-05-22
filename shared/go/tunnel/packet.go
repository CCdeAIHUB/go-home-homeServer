package tunnel

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tjfoc/gmsm/sm4"
)

const (
	Version byte = 1

	PacketProbe byte = 1
	PacketHello byte = 2
	PacketFrame byte = 3

	FrameReady     byte = 1
	FrameKeepalive byte = 2
	FramePing      byte = 3
	FramePong      byte = 4
	FrameIPv4      byte = 5
	FrameEthernet  byte = 6
)

var magic = []byte{'G', 'H', 'U', '1'}

type Probe struct {
	SessionID string `json:"session_id"`
	DeviceID  string `json:"device_id"`
	Role      string `json:"role"`
}

type Hello struct {
	SessionID           string `json:"session_id"`
	ClientDeviceID      string `json:"client_device_id"`
	EncryptedSessionKey []byte `json:"encrypted_session_key"`
}

type Ready struct {
	HomeDeviceID string      `json:"home_device_id"`
	LANCIDR      string      `json:"lan_cidr,omitempty"`
	ClientHomeIP string      `json:"client_home_ip,omitempty"`
	Devices      []DeviceMap `json:"devices,omitempty"`
}

type DeviceMap struct {
	Name      string `json:"name,omitempty"`
	RealIP    string `json:"real_ip"`
	VirtualIP string `json:"virtual_ip"`
	MAC       string `json:"mac,omitempty"`
}

type Frame struct {
	SessionID string
	Sequence  uint64
	Type      byte
	Payload   []byte
}

func MarshalProbe(value Probe) ([]byte, error) {
	return marshalControl(PacketProbe, value)
}

func MarshalHello(value Hello) ([]byte, error) {
	return marshalControl(PacketHello, value)
}

func PacketKind(packet []byte) (byte, error) {
	if len(packet) < len(magic)+2 {
		return 0, errors.New("packet is too short")
	}
	if string(packet[:len(magic)]) != string(magic) {
		return 0, errors.New("packet magic is invalid")
	}
	if packet[len(magic)] != Version {
		return 0, fmt.Errorf("packet version %d is unsupported", packet[len(magic)])
	}
	return packet[len(magic)+1], nil
}

func UnmarshalControl(packet []byte, value any) error {
	kind, err := PacketKind(packet)
	if err != nil {
		return err
	}
	if kind == PacketFrame {
		return errors.New("secure frame is not a control packet")
	}
	return json.Unmarshal(packet[len(magic)+2:], value)
}

func NewSessionKey() ([]byte, error) {
	key := make([]byte, sm4.BlockSize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

func Seal(key []byte, sessionID string, sequence uint64, frameType byte, payload []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if sessionID == "" || len(sessionID) > 255 {
		return nil, errors.New("session id length is invalid")
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	headerLen := len(magic) + 2 + 1 + len(sessionID) + 8
	header := make([]byte, headerLen)
	copy(header, magic)
	header[len(magic)] = Version
	header[len(magic)+1] = PacketFrame
	header[len(magic)+2] = byte(len(sessionID))
	copy(header[len(magic)+3:], sessionID)
	binary.BigEndian.PutUint64(header[headerLen-8:], sequence)

	plaintext := make([]byte, 1+len(payload))
	plaintext[0] = frameType
	copy(plaintext[1:], payload)
	ciphertext := aead.Seal(nil, nonce, plaintext, header)

	packet := append(header, nonce...)
	packet = append(packet, ciphertext...)
	return packet, nil
}

func Open(key, packet []byte) (Frame, error) {
	kind, err := PacketKind(packet)
	if err != nil {
		return Frame{}, err
	}
	if kind != PacketFrame {
		return Frame{}, errors.New("packet is not a secure frame")
	}
	if len(packet) < len(magic)+2+1+8 {
		return Frame{}, errors.New("frame header is incomplete")
	}

	sessionLen := int(packet[len(magic)+2])
	headerLen := len(magic) + 2 + 1 + sessionLen + 8
	aead, err := newAEAD(key)
	if err != nil {
		return Frame{}, err
	}
	if len(packet) < headerLen+aead.NonceSize()+aead.Overhead()+1 {
		return Frame{}, errors.New("frame payload is incomplete")
	}

	header := packet[:headerLen]
	sessionID := string(packet[len(magic)+3 : len(magic)+3+sessionLen])
	sequence := binary.BigEndian.Uint64(header[headerLen-8:])
	nonce := packet[headerLen : headerLen+aead.NonceSize()]
	plaintext, err := aead.Open(nil, nonce, packet[headerLen+aead.NonceSize():], header)
	if err != nil {
		return Frame{}, err
	}
	return Frame{
		SessionID: sessionID,
		Sequence:  sequence,
		Type:      plaintext[0],
		Payload:   append([]byte(nil), plaintext[1:]...),
	}, nil
}

func marshalControl(kind byte, value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	packet := append([]byte(nil), magic...)
	packet = append(packet, Version, kind)
	packet = append(packet, body...)
	return packet, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != sm4.BlockSize {
		return nil, fmt.Errorf("sm4 session key must be %d bytes", sm4.BlockSize)
	}
	block, err := sm4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
