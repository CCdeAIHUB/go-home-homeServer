// Package tunnel 定义了 Go Home UDP 隧道的数据包格式和 SM4-GCM 加密封装。
//
// UDP 隧道是客户端与家庭服务器之间的加密通信通道，所有数据通过 P2P UDP 传输。
// 数据包格式分为两类：
//   - 控制包（Probe/Hello）：明文 JSON，用于打洞握手和会话建立
//   - 加密帧（Frame）：SM4-GCM 加密，用于隧道数据传输和保活
//
// 数据包二进制格式：
//
//	控制包: [magic(4)] [version(1)] [kind(1)] [json_body(N)]
//	加密帧: [magic(4)] [version(1)] [kind(1)] [sessionLen(1)] [sessionID(N)] [sequence(8)] [nonce(12)] [ciphertext+tag(M)]
//
// magic = "GHU1" (Go Home UDP v1)
//
// 跨平台兼容性：Go 端和 Android 端（BouncyCastle）的封包/解包必须产生相同的二进制格式。
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

// 协议版本号，当前为 1。
const Version byte = 1

// 数据包类型常量。
const (
	// PacketProbe 探测包，用于 P2P 打洞时发现对端公网地址。
	// 服务器收到后静默丢弃，客户端/家庭服务器收到后更新 peer 地址。
	PacketProbe byte = 1
	// PacketHello 握手包，客户端发送给家庭服务器以建立加密会话。
	// 包含加密的 SM4 会话密钥（用家庭服务器 SM2 公钥加密）。
	PacketHello byte = 2
	// PacketFrame 加密帧，所有隧道数据（IPv4 包、保活、ping/pong）都封装在此类型中。
	PacketFrame byte = 3
	// PacketRegister 注册探测包，设备发送到公网服务器以发现 NAT 映射后的公网端点。
	// 服务器收到后记录源地址作为该设备的 observed_endpoint。
	PacketRegister byte = 4
)

// 加密帧类型常量。
const (
	// FrameReady 会话就绪帧，家庭服务器在握手完成后发送，包含 LAN 信息和设备映射。
	FrameReady byte = 1
	// FrameKeepalive 保活帧，用于维持 NAT 映射，无需回复。
	FrameKeepalive byte = 2
	// FramePing 隧道 ping，接收方应回复 FramePong。
	FramePing byte = 3
	// FramePong 隧道 pong，对 FramePing 的回复。
	FramePong byte = 4
	// FrameIPv4 IPv4 数据帧，承载虚拟网卡读写的 IP 包。
	FrameIPv4 byte = 5
	// FrameEthernet 以太网帧（预留，当前未实现 L2 桥接）。
	FrameEthernet byte = 6
)

// magic 是 UDP 数据包的魔术字，用于识别 Go Home 协议包。
// "GHU1" = Go Home UDP version 1。
var magic = []byte{'G', 'H', 'U', '1'}

// Probe 是打洞探测包的结构。
type Probe struct {
	// SessionID 打洞会话 ID，由服务器在 hole_punch_offer 中生成。
	SessionID string `json:"session_id"`
	// DeviceID 发送方的设备 ID。
	DeviceID string `json:"device_id"`
	// Role 发送方角色："client" 或 "home-server"。
	Role string `json:"role"`
}

// Register 是设备注册探测包的结构，用于 NAT 端点发现。
type Register struct {
	// DeviceID 发送方的设备 ID，用于服务器关联 WebSocket 会话。
	DeviceID string `json:"device_id"`
	// Token 设备认证令牌，用于服务器验证合法性。
	Token string `json:"token"`
}

// Hello 是打洞握手包的结构。
type Hello struct {
	// SessionID 打洞会话 ID。
	SessionID string `json:"session_id"`
	// ClientDeviceID 客户端设备 ID，家庭服务器用于验证对端身份。
	ClientDeviceID string `json:"client_device_id"`
	// EncryptedSessionKey 用家庭服务器 SM2 公钥加密的 SM4 会话密钥。
	// 家庭服务器用 SM2 私钥解密后获得明文密钥，用于后续帧的 SM4-GCM 加解密。
	EncryptedSessionKey []byte `json:"encrypted_session_key"`
}

// Ready 是隧道就绪帧的载荷，由家庭服务器在握手完成后发送。
type Ready struct {
	// HomeDeviceID 家庭服务器设备 ID。
	HomeDeviceID string `json:"home_device_id"`
	// LANCIDR 家庭局域网的 CIDR 地址（如 "192.168.1.0/24"）。
	LANCIDR string `json:"lan_cidr,omitempty"`
	// ClientHomeIP 通过 DHCP 代理为客户端分配的局域网 IP。
	ClientHomeIP string `json:"client_home_ip,omitempty"`
	// Devices 局域网设备映射表，包含真实 IP 与虚拟 IP 的对应关系。
	Devices []DeviceMap `json:"devices,omitempty"`
}

// DeviceMap 表示局域网设备在真实网段和虚拟网段之间的映射关系。
type DeviceMap struct {
	// Name 设备名称（预留）。
	Name string `json:"name,omitempty"`
	// RealIP 设备在家庭局域网中的真实 IP 地址。
	RealIP string `json:"real_ip"`
	// VirtualIP 设备在虚拟网段中的映射 IP 地址（网段冲突时使用）。
	VirtualIP string `json:"virtual_ip"`
	// MAC 设备的 MAC 地址。
	MAC string `json:"mac,omitempty"`
}

// Frame 表示一个解密后的隧道帧。
type Frame struct {
	// SessionID 隧道会话 ID。
	SessionID string
	// Sequence 帧序列号，用于防重放检测。
	Sequence uint64
	// Type 帧类型（FrameReady/FrameKeepalive/FramePing/FramePong/FrameIPv4/FrameEthernet）。
	Type byte
	// Payload 帧载荷数据。
	Payload []byte
}

// MarshalProbe 将 Probe 结构序列化为 UDP 探测包。
func MarshalProbe(value Probe) ([]byte, error) {
	return marshalControl(PacketProbe, value)
}

// MarshalRegister 将 Register 结构序列化为 UDP 注册探测包。
func MarshalRegister(value Register) ([]byte, error) {
	return marshalControl(PacketRegister, value)
}

// MarshalHello 将 Hello 结构序列化为 UDP 握手包。
func MarshalHello(value Hello) ([]byte, error) {
	return marshalControl(PacketHello, value)
}

// PacketKind 从原始 UDP 数据包中提取包类型。
// 先验证 magic 和 version，然后返回 kind 字段。
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

// UnmarshalControl 从控制包中解析 JSON 载荷。
// 仅适用于 PacketProbe 和 PacketHello，不适用于 PacketFrame。
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

// NewSessionKey 生成 16 字节随机 SM4 会话密钥。
// 用于 P2P 隧道建立时，客户端生成后用家庭服务器 SM2 公钥加密传输。
func NewSessionKey() ([]byte, error) {
	key := make([]byte, sm4.BlockSize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// Seal 使用 SM4-GCM 对帧数据进行加密并封包。
//
// 加密封包格式：
//
//	[magic(4)] [version(1)] [PacketFrame(1)] [sessionLen(1)] [sessionID(N)] [sequence(8)] [nonce(12)] [ciphertext+tag(M)]
//
// header 部分作为 AAD（Additional Authenticated Data）参与认证，
// 任何对 header 的篡改都会导致解密失败。
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

// Open 使用 SM4-GCM 对加密帧进行解密并返回 Frame 结构。
//
// 解密流程：
//  1. 验证 magic 和 version
//  2. 提取 sessionID 和 sequence
//  3. 使用 header 作为 AAD 进行 SM4-GCM 认证解密
//  4. 返回包含 sessionID、sequence、type、payload 的 Frame
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

// marshalControl 将控制包序列化为二进制格式。
// 格式：[magic(4)] [version(1)] [kind(1)] [json_body(N)]
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

// newAEAD 从 16 字节 SM4 密钥创建 GCM 模式的 AEAD 加密器。
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
