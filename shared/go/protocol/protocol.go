// Package protocol 定义了 Go Home 项目中所有组件之间的通信协议。
//
// 通信基于 WebSocket JSON-RPC 2.0 协议，分为三类消息：
//   - 请求（Request）：客户端发送，携带唯一 ID，期望收到对应响应
//   - 响应（Result/Error）：服务端返回，与请求 ID 一一对应
//   - 事件（Event）：服务端主动推送，无 ID，无需客户端响应
//
// 参与通信的角色：
//   - web-console：Web 管理控制台，使用 front.* 前缀的 Action
//   - home-server：家庭服务器，使用 device.* 前缀的 Action
//   - client：客户端核心，使用 client.* 和 p2p.* 前缀的 Action
//   - server：公网服务器，负责路由和转发上述所有消息
package protocol

import (
	"encoding/json"
	"time"
)

// JSONRPCVersion 是 JSON-RPC 2.0 协议的固定版本号。
const JSONRPCVersion = "2.0"

// 设备类型常量，用于标识连接到服务器的设备角色。
const (
	// DeviceTypeClient 表示客户端（PC/Android/iOS 等）。
	DeviceTypeClient = "client"
	// DeviceTypeHomeServer 表示家庭服务器。
	DeviceTypeHomeServer = "home-server"
	// DeviceTypeConsole 表示 Web 管理控制台。
	DeviceTypeConsole = "web-console"
)

// 家庭可见性常量，控制客户端是否可以自由加入家庭。
const (
	// FamilyVisibilityPublic 表示公开家庭，所有已认证客户端可见可连接。
	FamilyVisibilityPublic = "public"
	// FamilyVisibilityPrivate 表示私密家庭，仅被授权的客户端可见可连接。
	FamilyVisibilityPrivate = "private"
)

// ============================================================
// Action 常量 — 客户端发送的请求动作
// ============================================================

// front.* — Web 控制台管理操作，需要管理员登录后才能调用。
const (
	// ActionFrontLogin 管理员登录，参数 LoginParams，返回 LoginResult。
	ActionFrontLogin = "front.login"
	// ActionFrontDashboard 获取仪表盘数据（在线设备数、总流量、运行时长），返回 Dashboard。
	ActionFrontDashboard = "front.dashboard"
	// ActionFrontFamilyList 获取所有家庭列表（含家庭服务器在线状态），返回 []Family。
	ActionFrontFamilyList = "front.family.list"
	// ActionFrontFamilyCreate 创建家庭，参数 CreateFamilyParams，返回 {id}。
	ActionFrontFamilyCreate = "front.family.create"
	// ActionFrontFamilySetVisible 设置家庭可见性，参数 SetFamilyVisibilityParams，返回 {ok}。
	ActionFrontFamilySetVisible = "front.family.set_visibility"
	// ActionFrontFamilyBindServer 绑定家庭服务器到家庭，参数 BindHomeServerParams，返回 {ok}。
	ActionFrontFamilyBindServer = "front.family.bind_home_server"
	// ActionFrontFamilyUnbindServer 解绑家庭服务器，参数 UnbindHomeServerParams，返回 {ok}。
	ActionFrontFamilyUnbindServer = "front.family.unbind_home_server"
	// ActionFrontFamilyGrantDevice 授权客户端访问私密家庭，参数 FamilyGrantParams，返回 {ok}。
	ActionFrontFamilyGrantDevice = "front.family.grant_device"
	// ActionFrontFamilyRevokeDevice 撤销客户端对私密家庭的访问权限，参数 FamilyRevokeParams，返回 {ok}。
	ActionFrontFamilyRevokeDevice = "front.family.revoke_device"
	// ActionFrontDeviceList 获取所有设备列表（含在线状态和延迟），返回 []Device。
	ActionFrontDeviceList = "front.device.list"
	// ActionFrontDeviceBlacklist 设置设备黑名单状态，参数 DeviceTargetParams，返回 {ok}。
	ActionFrontDeviceBlacklist = "front.device.blacklist"
	// ActionFrontDeviceForceOffline 强制设备下线，参数 DeviceTargetParams，返回 {ok}。
	ActionFrontDeviceForceOffline = "front.device.force_offline"
	// ActionFrontConfigGet 获取系统配置（含授权码），返回 {auth_code}。
	ActionFrontConfigGet = "front.config.get"
	// ActionFrontConfigUpdateAuth 更新授权码，参数 ConfigUpdateAuthParams，返回 {ok}。
	ActionFrontConfigUpdateAuth = "front.config.update_auth_code"
	// ActionFrontConfigUpdatePass 更新管理员密码，参数 ConfigUpdatePassParams，返回 {ok}。
	ActionFrontConfigUpdatePass = "front.config.update_password"
	// ActionFrontLogList 获取服务器日志，参数 LogListParams，返回 []LogEntry。
	ActionFrontLogList = "front.log.list"
	// ActionFrontFamilyDetail 获取家庭详情（含授权设备列表和流量统计），参数 FamilyDetailParams，返回 FamilyDetail。
	ActionFrontFamilyDetail = "front.family.detail"
	// ActionFrontFamilyTraffic 获取家庭流量统计，参数 FamilyDetailParams，返回 TrafficStats。
	ActionFrontFamilyTraffic = "front.family.traffic"
	// ActionFrontFamilyBlacklist 将设备加入家庭黑名单（永久禁止加入），参数 FamilyBlacklistParams，返回 {ok}。
	ActionFrontFamilyBlacklist = "front.family.blacklist"
	// ActionFrontFamilyUnblacklist 将设备移出家庭黑名单，参数 FamilyBlacklistParams，返回 {ok}。
	ActionFrontFamilyUnblacklist = "front.family.unblacklist"
	// ActionFrontDeviceDetail 获取设备详情（含流量统计和所属家庭），参数 DeviceDetailParams，返回 DeviceDetail。
	ActionFrontDeviceDetail = "front.device.detail"
	// ActionFrontDeviceTraffic 获取设备流量统计，参数 DeviceDetailParams，返回 TrafficStats。
	ActionFrontDeviceTraffic = "front.device.traffic"
	// ActionFrontDeviceSetNote 设置设备备注，参数 DeviceNoteParams，返回 {ok}。
	ActionFrontDeviceSetNote = "front.device.set_note"
	// ActionFrontDeviceGrantFamily 将客户端加入指定私密家庭，参数 FamilyGrantParams，返回 {ok}。
	ActionFrontDeviceGrantFamily = "front.device.grant_family"
)

// device.* / client.* / p2p.* — 设备端操作。
const (
	// ActionDeviceAuth 设备认证，参数 DeviceAuthParams，返回 DeviceAuthResult。
	// 所有设备（家庭服务器和客户端）连接后必须首先调用此动作完成认证。
	ActionDeviceAuth = "device.auth"
	// ActionDeviceLANReport 家庭服务器上报局域网网段变化，参数 LANReportParams，返回 {ok}。
	// 仅家庭服务器可调用，服务器检测到网段变化后推送给相关客户端。
	ActionDeviceLANReport = "device.lan.report"
	// ActionClientFamilyList 客户端获取可访问的家庭列表，返回 []Family。
	// 仅已认证的客户端可调用，返回公开家庭 + 已授权的私密家庭。
	ActionClientFamilyList = "client.family.list"
	// ActionP2PHolePunchReq 客户端请求 P2P 打洞，参数 HolePunchRequestParams，返回 HolePunchOffer。
	// 服务器会将打洞信息转发给目标家庭服务器。
	ActionP2PHolePunchReq = "p2p.hole_punch_req"
	// ActionP2PCandidate 设备转发候选地址给对端，参数 CandidateRelayParams，返回 {ok}。
	// 用于 NAT 穿越中交换新的候选地址。
	ActionP2PCandidate = "p2p.candidate"
	// ActionStatsTraffic 设备上报流量统计，参数 TrafficReportParams，返回 {ok}。
	ActionStatsTraffic = "stats.traffic_report"
	// ActionStatsLatencyPong 设备回复延迟探测，参数 LatencyPongParams，返回 {latency_ms}。
	ActionStatsLatencyPong = "stats.latency_pong"
	// ActionPing 心跳保活，参数 HeartbeatParams，返回 {pong, server_now, latency_ms}。
	// 设备需定期发送 ping 以保持 WebSocket 连接活跃，并校验 time_key。
	ActionPing = "ping"
)

// ============================================================
// Event 常量 — 服务器主动推送的事件
// ============================================================

const (
	// EventFrontSessionRevoked 管理员会话被顶替（单点登录），参数 {reason}。
	// 当新的管理员登录时，旧会话收到此事件后被强制断开。
	EventFrontSessionRevoked = "front.session.revoked"
	// EventFrontDataChanged 数据变更通知，参数 {reason, at}。
	// 管理员执行任何写操作后，服务器推送此事件通知前端刷新数据。
	EventFrontDataChanged = "front.data_changed"
	// EventDeviceForceOffline 设备被强制下线，参数 {reason}。
	// 原因可能是"blacklisted"（被拉黑）或"forced"（管理员手动踢出）。
	EventDeviceForceOffline = "device.force_offline"
	// EventDeviceLatencyProbe 服务器发起的延迟探测，参数 {probe_id, sent_at}。
	// 设备收到后应回复 stats.latency_pong。
	EventDeviceLatencyProbe = "device.latency_probe"
	// EventP2PHolePunchOffer P2P 打洞邀请，参数 HolePunchOffer。
	// 服务器将客户端的打洞请求转发给家庭服务器，包含双方的连接信息。
	EventP2PHolePunchOffer = "p2p.hole_punch_offer"
	// EventP2PCandidate 候选地址转发，参数 {from_device_id, candidate, session_id}。
	// 一端发现新的候选地址后，服务器转发给对端。
	EventP2PCandidate = "p2p.candidate"
	// EventFamilyLANChanged 家庭局域网网段变化通知，参数 {family_id, lan_cidr}。
	// 家庭服务器上报网段变化后，服务器通知所有有权限的客户端。
	EventFamilyLANChanged = "family.lan_changed"
	// EventFamilyHomeServerChanged 家庭服务器状态变更通知，参数 {family_id, home_server_id, online}。
	// 家庭服务器上线或离线时，服务器通知所有有权限的客户端刷新家庭列表。
	EventFamilyHomeServerChanged = "family.home_server_changed"
)

// ============================================================
// 信封与错误类型
// ============================================================

// Envelope 是 JSON-RPC 2.0 消息的信封结构。
//   - 请求：JSONRPC + ID + Action + Params
//   - 成功响应：JSONRPC + ID + Result
//   - 错误响应：JSONRPC + ID + Error
//   - 事件：JSONRPC + Action + Params（无 ID）
type Envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Action  string          `json:"action,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	Token   string          `json:"token,omitempty"`
}

// RPCError 是 JSON-RPC 2.0 错误响应的结构。
// Code 为字符串类型的错误码，Message 为人类可读的错误描述。
type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Request 构造一个 JSON-RPC 2.0 请求消息。
// id 为请求唯一标识，action 为动作名称，params 为请求参数（将被 JSON 序列化）。
func Request(id, action string, params any) (Envelope, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{JSONRPC: JSONRPCVersion, ID: id, Action: action, Params: raw}, nil
}

// Event 构造一个 JSON-RPC 2.0 事件消息（无 ID，无需响应）。
// action 为事件名称，params 为事件参数（将被 JSON 序列化）。
func Event(action string, params any) (Envelope, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{JSONRPC: JSONRPCVersion, Action: action, Params: raw}, nil
}

// Result 构造一个 JSON-RPC 2.0 成功响应。
// id 为对应请求的 ID，result 为返回数据。
func Result(id string, result any) Envelope {
	return Envelope{JSONRPC: JSONRPCVersion, ID: id, Result: result}
}

// Error 构造一个 JSON-RPC 2.0 错误响应。
// id 为对应请求的 ID，code 为错误码字符串，message 为错误描述。
func Error(id, code, message string) Envelope {
	return Envelope{JSONRPC: JSONRPCVersion, ID: id, Error: &RPCError{Code: code, Message: message}}
}

// DecodeParams 将 json.RawMessage 反序列化为指定类型 T。
// 用于从 Envelope.Params 中解析请求参数。
func DecodeParams[T any](raw json.RawMessage) (T, error) {
	var out T
	err := json.Unmarshal(raw, &out)
	return out, err
}

// ============================================================
// 请求参数类型
// ============================================================

// LoginParams 是 front.login 的请求参数。
type LoginParams struct {
	// Password 管理员密码。
	Password string `json:"password"`
}

// LoginResult 是 front.login 的成功响应。
type LoginResult struct {
	// Token 登录成功后分配的会话令牌，后续管理请求需携带此令牌。
	Token string `json:"token"`
}

// DeviceAuthParams 是 device.auth 的请求参数。
// 所有设备（家庭服务器和客户端）连接后必须首先发送此请求完成认证。
type DeviceAuthParams struct {
	// DeviceID 设备唯一标识，由 SM3 哈希派生（前 32 个 hex 字符）。
	DeviceID string `json:"device_id"`
	// DeviceType 设备类型，必须为 "client" 或 "home-server"。
	DeviceType string `json:"device_type"`
	// AuthCode 服务器授权码，用于验证设备合法性。
	AuthCode string `json:"auth_code"`
	// PublicKey 设备的 SM2 公钥（PEM 格式），用于 P2P 隧道密钥交换。
	PublicKey string `json:"public_key,omitempty"`
	// TimeKey 基于 SM3-HMAC 的时间窗口密钥，用于防重放攻击。
	TimeKey string `json:"time_key"`
	// Timestamp 发送请求时的客户端时间戳（秒），与 TimeKey 配合校验。
	Timestamp int64 `json:"timestamp"`
	// UDPPort 设备监听的 UDP 端口，用于 P2P 打洞时交换连接信息。
	UDPPort int `json:"udp_port,omitempty"`
}

// DeviceAuthResult 是 device.auth 的成功响应。
type DeviceAuthResult struct {
	// Token 认证成功后分配的设备令牌。
	Token string `json:"token"`
	// ServerNow 服务器当前时间，客户端可用于校准时钟偏差。
	ServerNow time.Time `json:"server_now"`
	// DeviceID 回显设备 ID。
	DeviceID string `json:"device_id"`
	// DeviceType 回显设备类型。
	DeviceType string `json:"device_type"`
	// ServerUDPPort 公网服务器的 UDP 监听端口，设备应向此端口发送 UDP 注册探测包
	// 以便服务器发现 NAT 映射后的公网端点。0 表示服务器未启用 UDP 监听。
	ServerUDPPort int `json:"server_udp_port,omitempty"`
	// ServerUDPPorts 公网服务器的 UDP 观测端口列表。
	// 设备应使用同一个本地 UDP socket 向这些端口发送注册探测包，以收集多组 NAT 映射端点。
	ServerUDPPorts []int `json:"server_udp_ports,omitempty"`
}

// LANReportParams 是 device.lan.report 的请求参数。
// 仅家庭服务器可调用，定期上报局域网网段信息。
type LANReportParams struct {
	// LANCIDR 家庭局域网的 CIDR 地址（如 "192.168.1.0/24"）。
	LANCIDR string `json:"lan_cidr"`
	// Gateway 局域网网关地址（可选）。
	Gateway string `json:"gateway,omitempty"`
	// Interface 局域网网卡名称（可选）。
	Interface string `json:"interface,omitempty"`
	// Fingerprint 网络指纹，用于检测网络环境变化（可选）。
	Fingerprint string `json:"fingerprint,omitempty"`
}

// Family 表示一个虚拟局域网家庭。
type Family struct {
	// ID 家庭唯一标识（数据库自增）。
	ID int64 `json:"id"`
	// Name 家庭名称。
	Name string `json:"name"`
	// Visibility 可见性，"public" 或 "private"。
	Visibility string `json:"visibility"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `json:"created_at"`
	// HomeServerID 已绑定的家庭服务器设备 ID，空字符串表示未绑定。
	HomeServerID string `json:"home_server_id,omitempty"`
	// HomeServerOnline 家庭服务器是否在线。
	HomeServerOnline bool `json:"home_server_online"`
	// LANCIDR 家庭局域网的 CIDR 地址。
	LANCIDR string `json:"lan_cidr,omitempty"`
	// LANUpdatedAt 网段信息最后更新时间。
	LANUpdatedAt *time.Time `json:"lan_updated_at,omitempty"`
}

// Device 表示一个已认证的设备。
type Device struct {
	// DeviceID 设备唯一标识（SM3 哈希派生）。
	DeviceID string `json:"device_id"`
	// DeviceType 设备类型："client"、"home-server" 或 "web-console"。
	DeviceType string `json:"device_type"`
	// FamilyID 设备所属的家庭 ID（仅家庭服务器有值）。
	FamilyID *int64 `json:"family_id,omitempty"`
	// Token 认证令牌（仅在内部使用，不暴露给前端列表）。
	Token string `json:"token,omitempty"`
	// IsBlacklisted 是否被拉黑。
	IsBlacklisted bool `json:"is_blacklisted"`
	// LastOnline 最后在线时间。
	LastOnline *time.Time `json:"last_online,omitempty"`
	// LANCIDR 设备所在局域网的 CIDR（仅家庭服务器有值）。
	LANCIDR string `json:"lan_cidr,omitempty"`
	// UDPPort 设备监听的 UDP 端口。
	UDPPort int `json:"udp_port,omitempty"`
	// Online 是否当前在线（运行时计算，不持久化）。
	Online bool `json:"online"`
	// LatencyMS 设备延迟（毫秒，运行时计算）。
	LatencyMS int64 `json:"latency_ms,omitempty"`
	// Note 设备注备（管理员可设置）。
	Note string `json:"note,omitempty"`
	// WSEndpoint WebSocket 连接端点地址。
	WSEndpoint string `json:"ws_endpoint,omitempty"`
}

// CreateFamilyParams 是 front.family.create 的请求参数。
type CreateFamilyParams struct {
	// Name 家庭名称。
	Name string `json:"name"`
	// Visibility 可见性，"public" 或 "private"，默认 "private"。
	Visibility string `json:"visibility"`
}

// SetFamilyVisibilityParams 是 front.family.set_visibility 的请求参数。
type SetFamilyVisibilityParams struct {
	// FamilyID 家庭 ID。
	FamilyID int64 `json:"family_id"`
	// Visibility 目标可见性，"public" 或 "private"。
	Visibility string `json:"visibility"`
}

// BindHomeServerParams 是 front.family.bind_home_server 的请求参数。
type BindHomeServerParams struct {
	// FamilyID 家庭 ID。
	FamilyID int64 `json:"family_id"`
	// HomeServerID 要绑定的家庭服务器设备 ID。
	HomeServerID string `json:"home_server_id"`
}

// UnbindHomeServerParams 是 front.family.unbind_home_server 的请求参数。
type UnbindHomeServerParams struct {
	// FamilyID 家庭 ID。
	FamilyID int64 `json:"family_id"`
}

// FamilyGrantParams 是 front.family.grant_device 的请求参数。
type FamilyGrantParams struct {
	// FamilyID 家庭 ID。
	FamilyID int64 `json:"family_id"`
	// DeviceID 要授权的客户端设备 ID。
	DeviceID string `json:"device_id"`
}

// FamilyRevokeParams 是 front.family.revoke_device 的请求参数。
type FamilyRevokeParams struct {
	// FamilyID 家庭 ID。
	FamilyID int64 `json:"family_id"`
	// DeviceID 要撤销授权的客户端设备 ID。
	DeviceID string `json:"device_id"`
}

// DeviceTargetParams 是 front.device.blacklist 和 front.device.force_offline 的请求参数。
type DeviceTargetParams struct {
	// DeviceID 目标设备 ID。
	DeviceID string `json:"device_id"`
	// Value 操作值（仅 blacklist 使用：true=拉黑，false=解除拉黑）。
	Value bool `json:"value,omitempty"`
}

// HolePunchRequestParams 是 p2p.hole_punch_req 的请求参数。
type HolePunchRequestParams struct {
	// FamilyID 目标家庭 ID。
	FamilyID int64 `json:"family_id"`
	// ClientUDPPort 客户端监听的 UDP 端口。
	ClientUDPPort int `json:"client_udp_port"`
	// PreferredMode 首选网络模式："real"（真实同网段）或 "virtual"（虚拟映射），默认 "real"。
	PreferredMode string `json:"preferred_mode,omitempty"`
	// VirtualCIDR 客户端期望的虚拟 CIDR（当选择虚拟模式时使用）。
	VirtualCIDR string `json:"virtual_cidr,omitempty"`
	// RoutePolicy controls client-side VPN routing: "lan" or "full".
	RoutePolicy string `json:"route_policy,omitempty"`
	// ClientVirtualMAC 客户端虚拟网卡 MAC 地址（当选择虚拟模式时使用）。
	ClientVirtualMAC string `json:"client_virtual_mac,omitempty"`
	// FallbackSweep requests a bounded full-port fallback after the normal
	// prediction stage misses. It is only enabled for a later retry.
	FallbackSweep bool `json:"fallback_sweep,omitempty"`
}

// PeerCandidate 表示 P2P 连接中的一端信息。
type PeerCandidate struct {
	// DeviceID 对端设备 ID。
	DeviceID string `json:"device_id"`
	// Endpoint 对端的公网地址（host:port 格式）。
	Endpoint string `json:"endpoint"`
	// ObservedEndpoint 服务器观察到的 NAT 映射后公网端点（host:port 格式）。
	// 通过 UDP 注册探测发现，比 Endpoint 更准确。优先使用此字段。
	ObservedEndpoint string `json:"observed_endpoint,omitempty"`
	// Candidates is the ordered IPv4 endpoint list the peer should punch.
	// The first item is the preferred observed endpoint; later items are
	// conservative fallbacks derived from WebSocket and reported UDP ports.
	Candidates []string `json:"candidates,omitempty"`
	// UDPPort 对端监听的 UDP 端口。
	UDPPort int `json:"udp_port"`
	// RemoteAddr WebSocket 连接的源地址（host:port），用于多路径打洞候选。
	RemoteAddr string `json:"remote_addr,omitempty"`
	// LANCIDR 对端所在局域网的 CIDR（仅家庭服务器有值）。
	LANCIDR string `json:"lan_cidr,omitempty"`
	// PublicKey 对端的 SM2 公钥（PEM 格式），用于加密会话密钥交换。
	PublicKey string `json:"public_key,omitempty"`
}

// HolePunchOffer 是 p2p.hole_punch_offer 事件的参数，包含完整的打洞信息。
type HolePunchOffer struct {
	// SessionID 打洞会话 ID，用于后续 UDP 隧道标识和密钥关联。
	SessionID string `json:"session_id"`
	// FamilyID 目标家庭 ID。
	FamilyID int64 `json:"family_id"`
	// Request 原始打洞请求参数。
	Request HolePunchRequestParams `json:"request"`
	// Client 客户端的连接信息。
	Client PeerCandidate `json:"client"`
	// Server 家庭服务器的连接信息。
	Server PeerCandidate `json:"server"`
}

// CandidateRelayParams 是 p2p.candidate 的请求参数。
type CandidateRelayParams struct {
	// TargetDeviceID 目标设备 ID。
	TargetDeviceID string `json:"target_device_id"`
	// Candidate 候选地址（host:port 格式）。
	Candidate string `json:"candidate"`
	// SessionID 关联的打洞会话 ID。
	SessionID string `json:"session_id,omitempty"`
}

// TrafficReportParams 是 stats.traffic_report 的请求参数。
type TrafficReportParams struct {
	// Direction 流量方向："in"（接收）或 "out"（发送）。
	Direction string `json:"direction"`
	// Bytes 本周期流量字节数。
	Bytes int64 `json:"bytes"`
}

// LatencyPongParams 是 stats.latency_pong 的请求参数。
type LatencyPongParams struct {
	// ProbeID 对应的延迟探测 ID（来自 device.latency_probe 事件）。
	ProbeID string `json:"probe_id"`
}

// HeartbeatParams 是 ping 的请求参数。
type HeartbeatParams struct {
	// TimeKey 基于 SM3-HMAC 的时间窗口密钥，用于防重放攻击。
	TimeKey string `json:"time_key"`
	// Timestamp 发送请求时的客户端时间戳（秒）。
	Timestamp int64 `json:"timestamp"`
}

// LogListParams 是 front.log.list 的请求参数。
type LogListParams struct {
	// Limit 返回日志条数上限，默认 100，最大 500。
	Limit int `json:"limit"`
}

// ConfigUpdateAuthParams 是 front.config.update_auth_code 的请求参数。
type ConfigUpdateAuthParams struct {
	// AuthCode 新的授权码。
	AuthCode string `json:"auth_code"`
}

// ConfigUpdatePassParams 是 front.config.update_password 的请求参数。
type ConfigUpdatePassParams struct {
	// OldPassword 当前管理员密码。
	OldPassword string `json:"old_password"`
	// NewPassword 新的管理员密码。
	NewPassword string `json:"new_password"`
}

// ============================================================
// 响应类型
// ============================================================

// LogEntry 表示一条服务器日志记录。
type LogEntry struct {
	// ID 日志记录 ID。
	ID int64 `json:"id"`
	// Level 日志级别："info"、"warn" 或 "error"。
	Level string `json:"level"`
	// Source 日志来源模块（如 "auth"、"family"、"device"、"p2p"、"config"）。
	Source string `json:"source"`
	// Message 日志内容。
	Message string `json:"message"`
	// CreatedAt 日志创建时间。
	CreatedAt time.Time `json:"created_at"`
}

// Dashboard 表示仪表盘统计数据。
type Dashboard struct {
	// OnlineDevices 当前在线设备数。
	OnlineDevices int `json:"online_devices"`
	// TotalBytes 累计流量字节数。
	TotalBytes int64 `json:"total_bytes"`
	// UptimeSeconds 服务器运行时长（秒）。
	UptimeSeconds int64 `json:"uptime_seconds"`
}

// FamilyDetailParams 是 front.family.detail 和 front.family.traffic 的请求参数。
type FamilyDetailParams struct {
	// FamilyID 家庭 ID。
	FamilyID int64 `json:"family_id"`
}

// FamilyBlacklistParams 是 front.family.blacklist 和 front.family.unblacklist 的请求参数。
type FamilyBlacklistParams struct {
	// FamilyID 家庭 ID。
	FamilyID int64 `json:"family_id"`
	// DeviceID 要加入黑名单的设备 ID。
	DeviceID string `json:"device_id"`
}

// DeviceDetailParams 是 front.device.detail 和 front.device.traffic 的请求参数。
type DeviceDetailParams struct {
	// DeviceID 设备 ID。
	DeviceID string `json:"device_id"`
}

// DeviceNoteParams 是 front.device.set_note 的请求参数。
type DeviceNoteParams struct {
	// DeviceID 设备 ID。
	DeviceID string `json:"device_id"`
	// Note 备注内容。
	Note string `json:"note"`
}

// FamilyDetail 是 front.family.detail 的成功响应。
type FamilyDetail struct {
	// Family 家庭基本信息。
	Family Family `json:"family"`
	// Devices 已授权设备列表（含家庭服务器和客户端）。
	Devices []Device `json:"devices"`
	// Traffic 流量统计。
	Traffic TrafficStats `json:"traffic"`
	// BlacklistedDevices 被加入黑名单的设备 ID 列表。
	BlacklistedDevices []string `json:"blacklisted_devices"`
}

// DeviceDetail 是 front.device.detail 的成功响应。
type DeviceDetail struct {
	// Device 设备基本信息。
	Device Device `json:"device"`
	// Families 设备所属/被授权的家庭列表。
	Families []Family `json:"families"`
	// Traffic 流量统计。
	Traffic TrafficStats `json:"traffic"`
	// Note 设备注备。
	Note string `json:"note"`
}

// TrafficStats 表示流量统计。
type TrafficStats struct {
	// UpBytes 上行流量字节数。
	UpBytes int64 `json:"up_bytes"`
	// DownBytes 下行流量字节数。
	DownBytes int64 `json:"down_bytes"`
}
