package protocol

import (
	"encoding/json"
	"time"
)

const (
	JSONRPCVersion = "2.0"

	DeviceTypeClient     = "client"
	DeviceTypeHomeServer = "home-server"
	DeviceTypeConsole    = "web-console"

	FamilyVisibilityPublic  = "public"
	FamilyVisibilityPrivate = "private"
)

const (
	ActionFrontLogin              = "front.login"
	ActionFrontDashboard          = "front.dashboard"
	ActionFrontFamilyList         = "front.family.list"
	ActionFrontFamilyCreate       = "front.family.create"
	ActionFrontFamilySetVisible   = "front.family.set_visibility"
	ActionFrontFamilyBindServer   = "front.family.bind_home_server"
	ActionFrontFamilyUnbindServer = "front.family.unbind_home_server"
	ActionFrontFamilyGrantDevice  = "front.family.grant_device"
	ActionFrontFamilyRevokeDevice = "front.family.revoke_device"
	ActionFrontDeviceList         = "front.device.list"
	ActionFrontDeviceBlacklist    = "front.device.blacklist"
	ActionFrontDeviceForceOffline = "front.device.force_offline"
	ActionFrontConfigGet          = "front.config.get"
	ActionFrontConfigUpdateAuth   = "front.config.update_auth_code"
	ActionFrontConfigUpdatePass   = "front.config.update_password"
	ActionFrontLogList            = "front.log.list"

	ActionDeviceAuth       = "device.auth"
	ActionDeviceLANReport  = "device.lan.report"
	ActionClientFamilyList = "client.family.list"
	ActionP2PHolePunchReq  = "p2p.hole_punch_req"
	ActionP2PCandidate     = "p2p.candidate"
	ActionStatsTraffic     = "stats.traffic_report"
	ActionStatsLatencyPong = "stats.latency_pong"
	ActionPing             = "ping"

	EventFrontSessionRevoked = "front.session.revoked"
	EventFrontDataChanged    = "front.data_changed"
	EventDeviceForceOffline  = "device.force_offline"
	EventDeviceLatencyProbe  = "device.latency_probe"
	EventP2PHolePunchOffer   = "p2p.hole_punch_offer"
	EventP2PCandidate        = "p2p.candidate"
	EventFamilyLANChanged    = "family.lan_changed"
)

type Envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Action  string          `json:"action,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Request(id, action string, params any) (Envelope, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{JSONRPC: JSONRPCVersion, ID: id, Action: action, Params: raw}, nil
}

func Event(action string, params any) (Envelope, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{JSONRPC: JSONRPCVersion, Action: action, Params: raw}, nil
}

func Result(id string, result any) Envelope {
	return Envelope{JSONRPC: JSONRPCVersion, ID: id, Result: result}
}

func Error(id, code, message string) Envelope {
	return Envelope{JSONRPC: JSONRPCVersion, ID: id, Error: &RPCError{Code: code, Message: message}}
}

func DecodeParams[T any](raw json.RawMessage) (T, error) {
	var out T
	err := json.Unmarshal(raw, &out)
	return out, err
}

type LoginParams struct {
	Password string `json:"password"`
}

type LoginResult struct {
	Token string `json:"token"`
}

type DeviceAuthParams struct {
	DeviceID   string `json:"device_id"`
	DeviceType string `json:"device_type"`
	AuthCode   string `json:"auth_code"`
	PublicKey  string `json:"public_key,omitempty"`
	TimeKey    string `json:"time_key"`
	Timestamp  int64  `json:"timestamp"`
	UDPPort    int    `json:"udp_port,omitempty"`
}

type DeviceAuthResult struct {
	Token      string    `json:"token"`
	ServerNow  time.Time `json:"server_now"`
	DeviceID   string    `json:"device_id"`
	DeviceType string    `json:"device_type"`
}

type LANReportParams struct {
	LANCIDR     string `json:"lan_cidr"`
	Gateway     string `json:"gateway,omitempty"`
	Interface   string `json:"interface,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type Family struct {
	ID               int64      `json:"id"`
	Name             string     `json:"name"`
	Visibility       string     `json:"visibility"`
	CreatedAt        time.Time  `json:"created_at"`
	HomeServerID     string     `json:"home_server_id,omitempty"`
	HomeServerOnline bool       `json:"home_server_online"`
	LANCIDR          string     `json:"lan_cidr,omitempty"`
	LANUpdatedAt     *time.Time `json:"lan_updated_at,omitempty"`
}

type Device struct {
	DeviceID      string     `json:"device_id"`
	DeviceType    string     `json:"device_type"`
	FamilyID      *int64     `json:"family_id,omitempty"`
	Token         string     `json:"token,omitempty"`
	IsBlacklisted bool       `json:"is_blacklisted"`
	LastOnline    *time.Time `json:"last_online,omitempty"`
	LANCIDR       string     `json:"lan_cidr,omitempty"`
	UDPPort       int        `json:"udp_port,omitempty"`
	Online        bool       `json:"online"`
	LatencyMS     int64      `json:"latency_ms,omitempty"`
}

type CreateFamilyParams struct {
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

type SetFamilyVisibilityParams struct {
	FamilyID   int64  `json:"family_id"`
	Visibility string `json:"visibility"`
}

type BindHomeServerParams struct {
	FamilyID     int64  `json:"family_id"`
	HomeServerID string `json:"home_server_id"`
}

type FamilyGrantParams struct {
	FamilyID int64  `json:"family_id"`
	DeviceID string `json:"device_id"`
}

type DeviceTargetParams struct {
	DeviceID string `json:"device_id"`
	Value    bool   `json:"value,omitempty"`
}

type HolePunchRequestParams struct {
	FamilyID         int64  `json:"family_id"`
	ClientUDPPort    int    `json:"client_udp_port"`
	PreferredMode    string `json:"preferred_mode,omitempty"`
	VirtualCIDR      string `json:"virtual_cidr,omitempty"`
	ClientVirtualMAC string `json:"client_virtual_mac,omitempty"`
}

type PeerCandidate struct {
	DeviceID  string `json:"device_id"`
	Endpoint  string `json:"endpoint"`
	UDPPort   int    `json:"udp_port"`
	LANCIDR   string `json:"lan_cidr,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
}

type HolePunchOffer struct {
	SessionID string                 `json:"session_id"`
	FamilyID  int64                  `json:"family_id"`
	Request   HolePunchRequestParams `json:"request"`
	Client    PeerCandidate          `json:"client"`
	Server    PeerCandidate          `json:"server"`
}

type CandidateRelayParams struct {
	TargetDeviceID string `json:"target_device_id"`
	Candidate      string `json:"candidate"`
	SessionID      string `json:"session_id,omitempty"`
}

type TrafficReportParams struct {
	Direction string `json:"direction"`
	Bytes     int64  `json:"bytes"`
}

type LatencyPongParams struct {
	ProbeID string `json:"probe_id"`
}

type LogEntry struct {
	ID        int64     `json:"id"`
	Level     string    `json:"level"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type Dashboard struct {
	OnlineDevices int   `json:"online_devices"`
	TotalBytes    int64 `json:"total_bytes"`
	UptimeSeconds int64 `json:"uptime_seconds"`
}
