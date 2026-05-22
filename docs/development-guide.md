# Go Home 开发文档

## 目录

1. [项目概述](#1-项目概述)
2. [系统架构](#2-系统架构)
3. [WebSocket JSON-RPC API](#3-websocket-json-rpc-api)
   - [协议基础](#31-协议基础)
   - [管理控制台 API](#32-管理控制台-api)
   - [设备端 API](#33-设备端-api)
   - [服务器推送事件](#34-服务器推送事件)
4. [UDP 隧道协议](#4-udp-隧道协议)
5. [加密体系](#5-加密体系)
6. [错误码参考](#6-错误码参考)
7. [HTTP 控制接口（客户端本地）](#7-http-控制接口客户端本地)
8. [部署指南](#8-部署指南)
9. [开发指南](#9-开发指南)

---

## 1. 项目概述

Go Home 是一个坚持纯 P2P UDP 打洞的虚拟局域网组网工具。公网服务器只负责认证、家庭管理、设备状态和 P2P 信令协调，**绝不提供中继转发**。全链路采用国密体系（SM2/SM3/SM4）加密。

### 核心角色

| 角色 | 说明 |
|------|------|
| **公网服务器 (server)** | 提供 WebSocket JSON-RPC 端点，管理家庭、设备、配置，转发 P2P 信令 |
| **家庭服务器 (home-server)** | 运行在家庭局域网中，负责 DHCP 代理、代理 ARP、UPnP/NAT-PMP、TUN 设备桥接 |
| **客户端 (client)** | PC/Android 等终端设备，通过 P2P UDP 隧道连接家庭服务器，访问局域网 |

### 技术栈

| 组件 | 技术 |
|------|------|
| server / home-server / client-core / shared | Go 1.25.1 |
| web-console / client-ui | Vue 3.5 + Vite 7.2 |
| client-pc | C# .NET 10.0 WinForms + WebView2 |
| client-android | Kotlin + Gradle 8.9 + Android SDK 35 |
| 加密库 | tjfoc/gmsm (Go) / BouncyCastle (Android) |

---

## 2. 系统架构

```
┌─────────────────────────────────────────────┐
│         公网服务器 (server)                    │
│  Web 控制台(Vue3) + WebSocket JSON-RPC Hub   │
│  + SQLite 数据库                              │
└──────────────┬──────────────────────────────┘
               │ WebSocket 信令
     ┌─────────┼─────────┐
     ▼                   ▼
┌──────────┐      ┌──────────────┐
│ 家庭服务器 │◄────►│ 客户端(多平台) │
│home-server│ P2P  │ client-core  │
│ DHCP/ARP  │ UDP  │ + 各平台壳    │
│ UPnP/NAT  │ 隧道 │ PC/Android   │
└──────────┘      └──────────────┘
```

### 三仓库拆分

| 仓库 | 包含模块 | CI 产物 |
|------|----------|---------|
| `go-home-server` | server + web-console + shared + docs | Go 二进制 + Docker 镜像 |
| `go-home-homeServer` | home-server + shared + docs | Go 二进制 |
| `go-home-client` | client-core + client-ui + client-pc + client-android + shared | Android APK + Windows 发布包 |

---

## 3. WebSocket JSON-RPC API

### 3.1 协议基础

所有通信基于 WebSocket JSON-RPC 2.0 协议，端点为 `/ws`。

#### 消息格式

**请求（Request）：**
```json
{
  "jsonrpc": "2.0",
  "id": "auth-1",
  "action": "device.auth",
  "params": { ... }
}
```

**成功响应（Result）：**
```json
{
  "jsonrpc": "2.0",
  "id": "auth-1",
  "result": { ... }
}
```

**错误响应（Error）：**
```json
{
  "jsonrpc": "2.0",
  "id": "auth-1",
  "error": {
    "code": "unauthorized",
    "message": "authorization code is incorrect"
  }
}
```

**服务器推送事件（Event，无 ID）：**
```json
{
  "jsonrpc": "2.0",
  "action": "front.data_changed",
  "params": { "reason": "device.auth", "at": "2026-05-22T15:30:00Z" }
}
```

#### 字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `jsonrpc` | string | 固定为 `"2.0"` |
| `id` | string | 请求唯一标识，响应必须回传相同的 ID；事件无此字段 |
| `action` | string | 动作名称或事件名称 |
| `params` | object | 请求参数或事件参数 |
| `result` | any | 成功响应数据 |
| `error` | object | 错误信息，包含 `code` 和 `message` |

---

### 3.2 管理控制台 API

管理控制台 API 需要先通过 `front.login` 登录获取 token，后续请求需校验 token。

#### front.login — 管理员登录

单点登录，新登录会踢掉旧会话。

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "login-1",
  "action": "front.login",
  "params": {
    "password": "admin"
  }
}
```

**成功响应：**
```json
{
  "jsonrpc": "2.0",
  "id": "login-1",
  "result": {
    "token": "a1b2c3d4e5f6a1b2"
  }
}
```

**失败响应：**
```json
{
  "jsonrpc": "2.0",
  "id": "login-1",
  "error": {
    "code": "unauthorized",
    "message": "password is incorrect"
  }
}
```

---

#### front.dashboard — 获取仪表盘数据

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "dash-1",
  "action": "front.dashboard",
  "params": {}
}
```

**成功响应：**
```json
{
  "jsonrpc": "2.0",
  "id": "dash-1",
  "result": {
    "online_devices": 3,
    "total_bytes": 1073741824,
    "uptime_seconds": 86400
  }
}
```

---

#### front.family.list — 获取家庭列表

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "fl-1",
  "action": "front.family.list",
  "params": {}
}
```

**成功响应：**
```json
{
  "jsonrpc": "2.0",
  "id": "fl-1",
  "result": [
    {
      "id": 1,
      "name": "我的家庭",
      "visibility": "private",
      "created_at": "2026-01-15T10:30:00Z",
      "home_server_id": "home-a1b2c3d4",
      "home_server_online": true,
      "lan_cidr": "192.168.1.0/24",
      "lan_updated_at": "2026-05-22T15:00:00Z"
    }
  ]
}
```

---

#### front.family.create — 创建家庭

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "fc-1",
  "action": "front.family.create",
  "params": {
    "name": "新家庭",
    "visibility": "public"
  }
}
```

**成功响应：**
```json
{
  "jsonrpc": "2.0",
  "id": "fc-1",
  "result": { "id": 2 }
}
```

**失败响应（无效可见性）：**
```json
{
  "jsonrpc": "2.0",
  "id": "fc-1",
  "error": {
    "code": "internal_error",
    "message": "invalid visibility: unknown"
  }
}
```

---

#### front.family.set_visibility — 设置家庭可见性

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "fsv-1",
  "action": "front.family.set_visibility",
  "params": {
    "family_id": 1,
    "visibility": "public"
  }
}
```

**成功响应：** `{ "ok": true }`

---

#### front.family.bind_home_server — 绑定家庭服务器

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "fbs-1",
  "action": "front.family.bind_home_server",
  "params": {
    "family_id": 1,
    "home_server_id": "home-a1b2c3d4"
  }
}
```

**成功响应：** `{ "ok": true }`

**失败响应（设备不是家庭服务器）：**
```json
{
  "jsonrpc": "2.0",
  "id": "fbs-1",
  "error": {
    "code": "internal_error",
    "message": "device is not a home server"
  }
}
```

---

#### front.family.unbind_home_server — 解绑家庭服务器

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "fus-1",
  "action": "front.family.unbind_home_server",
  "params": {
    "family_id": 1
  }
}
```

**成功响应：** `{ "ok": true }`

---

#### front.family.grant_device — 授权客户端访问私密家庭

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "fgd-1",
  "action": "front.family.grant_device",
  "params": {
    "family_id": 1,
    "device_id": "client-e5f6a1b2"
  }
}
```

**成功响应：** `{ "ok": true }`

---

#### front.family.revoke_device — 撤销客户端访问权限

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "frd-1",
  "action": "front.family.revoke_device",
  "params": {
    "family_id": 1,
    "device_id": "client-e5f6a1b2"
  }
}
```

**成功响应：** `{ "ok": true }`

---

#### front.device.list — 获取设备列表

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "dl-1",
  "action": "front.device.list",
  "params": {}
}
```

**成功响应：**
```json
{
  "jsonrpc": "2.0",
  "id": "dl-1",
  "result": [
    {
      "device_id": "home-a1b2c3d4",
      "device_type": "home-server",
      "family_id": 1,
      "is_blacklisted": false,
      "last_online": "2026-05-22T15:30:00Z",
      "lan_cidr": "192.168.1.0/24",
      "udp_port": 47777,
      "online": true,
      "latency_ms": 15
    },
    {
      "device_id": "client-e5f6a1b2",
      "device_type": "client",
      "family_id": null,
      "is_blacklisted": false,
      "last_online": "2026-05-22T15:28:00Z",
      "online": true,
      "latency_ms": 32
    }
  ]
}
```

---

#### front.device.blacklist — 设置设备黑名单

**请求（拉黑）：**
```json
{
  "jsonrpc": "2.0",
  "id": "fb-1",
  "action": "front.device.blacklist",
  "params": {
    "device_id": "client-e5f6a1b2",
    "value": true
  }
}
```

**请求（解除拉黑）：**
```json
{
  "params": {
    "device_id": "client-e5f6a1b2",
    "value": false
  }
}
```

**成功响应：** `{ "ok": true }`

> 注意：拉黑设备后会立即强制其下线。

---

#### front.device.force_offline — 强制设备下线

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "ffo-1",
  "action": "front.device.force_offline",
  "params": {
    "device_id": "client-e5f6a1b2",
    "value": true
  }
}
```

**成功响应：** `{ "ok": true }`

---

#### front.config.get — 获取系统配置

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "cg-1",
  "action": "front.config.get",
  "params": {}
}
```

**成功响应：**
```json
{
  "jsonrpc": "2.0",
  "id": "cg-1",
  "result": {
    "auth_code": "MY-SECRET-CODE"
  }
}
```

---

#### front.config.update_auth_code — 更新授权码

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "cua-1",
  "action": "front.config.update_auth_code",
  "params": {
    "auth_code": "NEW-SECRET-CODE"
  }
}
```

**成功响应：** `{ "ok": true }`

**失败响应（空授权码）：**
```json
{
  "error": {
    "code": "internal_error",
    "message": "auth_code cannot be empty"
  }
}
```

---

#### front.config.update_password — 更新管理员密码

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "cup-1",
  "action": "front.config.update_password",
  "params": {
    "old_password": "admin",
    "new_password": "new-secure-password"
  }
}
```

**成功响应：** `{ "ok": true }`

**失败响应（旧密码错误）：**
```json
{
  "error": {
    "code": "internal_error",
    "message": "old password is incorrect"
  }
}
```

---

#### front.log.list — 获取服务器日志

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "ll-1",
  "action": "front.log.list",
  "params": {
    "limit": 50
  }
}
```

**成功响应：**
```json
{
  "jsonrpc": "2.0",
  "id": "ll-1",
  "result": [
    {
      "id": 100,
      "level": "info",
      "source": "device",
      "message": "设备上线: client-e5f6a1b2 (client)",
      "created_at": "2026-05-22T15:30:00Z"
    }
  ]
}
```

> `limit` 默认 100，最大 500。

---

### 3.3 设备端 API

#### device.auth — 设备认证

所有设备连接后**必须首先**发送此请求完成认证。

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "auth-1",
  "action": "device.auth",
  "params": {
    "device_id": "home-a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
    "device_type": "home-server",
    "auth_code": "MY-SECRET-CODE",
    "public_key": "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoEcz1UBgi0DQgAE...\n-----END PUBLIC KEY-----",
    "time_key": "a1b2c3d4e5f6",
    "timestamp": 1747926600,
    "udp_port": 47777
  }
}
```

**成功响应：**
```json
{
  "jsonrpc": "2.0",
  "id": "auth-1",
  "result": {
    "token": "f6e5d4c3b2a1f6e5d4c3b2a1f6e5d4c3",
    "server_now": "2026-05-22T15:30:00Z",
    "device_id": "home-a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
    "device_type": "home-server"
  }
}
```

**失败响应（授权码错误）：**
```json
{
  "error": {
    "code": "unauthorized",
    "message": "authorization code is incorrect"
  }
}
```

**失败响应（设备被拉黑）：**
```json
{
  "error": {
    "code": "blacklisted",
    "message": "device is blacklisted"
  }
}
```

**失败响应（time_key 无效）：**
```json
{
  "error": {
    "code": "time_key_invalid",
    "message": "time key invalid"
  }
}
```

**失败响应（时钟偏差过大）：**
```json
{
  "error": {
    "code": "clock_skew",
    "message": "time check failed, please check system time"
  }
}
```

> 连续 3 次 time_key 校验失败，连接将被强制断开。

---

#### device.lan.report — 上报局域网网段

仅家庭服务器可调用。

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "lan-1",
  "action": "device.lan.report",
  "params": {
    "lan_cidr": "192.168.1.0/24",
    "gateway": "192.168.1.1",
    "interface": "eth0"
  }
}
```

**成功响应：** `{ "ok": true }`

---

#### client.family.list — 客户端获取可访问家庭

仅已认证客户端可调用。

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "cfl-1",
  "action": "client.family.list",
  "params": {}
}
```

**成功响应：** 返回公开家庭 + 已授权的私密家庭（格式同 `front.family.list`）。

---

#### p2p.hole_punch_req — 请求 P2P 打洞

仅已认证客户端可调用。

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "hpr-1",
  "action": "p2p.hole_punch_req",
  "params": {
    "family_id": 1,
    "client_udp_port": 47778,
    "preferred_mode": "real",
    "virtual_cidr": "",
    "client_virtual_mac": ""
  }
}
```

**成功响应（HolePunchOffer）：**
```json
{
  "jsonrpc": "2.0",
  "id": "hpr-1",
  "result": {
    "session_id": "s1a2b3c4d5e6f7a8",
    "family_id": 1,
    "request": { ... },
    "client": {
      "device_id": "client-e5f6a1b2",
      "endpoint": "203.0.113.5:47778",
      "udp_port": 47778,
      "public_key": "-----BEGIN PUBLIC KEY-----\n..."
    },
    "server": {
      "device_id": "home-a1b2c3d4",
      "endpoint": "198.51.100.10:47777",
      "udp_port": 47777,
      "lan_cidr": "192.168.1.0/24",
      "public_key": "-----BEGIN PUBLIC KEY-----\n..."
    }
  }
}
```

**失败响应（家庭服务器离线）：**
```json
{
  "error": {
    "code": "not_available",
    "message": "home server is offline"
  }
}
```

---

#### p2p.candidate — 转发候选地址

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "pc-1",
  "action": "p2p.candidate",
  "params": {
    "target_device_id": "home-a1b2c3d4",
    "candidate": "203.0.113.5:47778",
    "session_id": "s1a2b3c4d5e6f7a8"
  }
}
```

**成功响应：** `{ "ok": true }`

---

#### stats.traffic_report — 上报流量统计

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "tr-1",
  "action": "stats.traffic_report",
  "params": {
    "direction": "up",
    "bytes": 1048576
  }
}
```

**成功响应：** `{ "ok": true }`

> `direction` 可选值：`"up"`（上行）、`"down"`（下行）。

---

#### stats.latency_pong — 回复延迟探测

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "lp-1",
  "action": "stats.latency_pong",
  "params": {
    "probe_id": "p1a2b3c4"
  }
}
```

**成功响应：**
```json
{
  "result": { "latency_ms": 15 }
}
```

---

#### ping — 心跳保活

已认证设备必须定期发送心跳（建议 30 秒间隔），携带 time_key 进行校验。

**请求：**
```json
{
  "jsonrpc": "2.0",
  "id": "ping-1",
  "action": "ping",
  "params": {
    "time_key": "d4e5f6a1b2c3",
    "timestamp": 1747926600
  }
}
```

**成功响应：**
```json
{
  "result": {
    "pong": true,
    "server_now": "2026-05-22T15:30:00Z",
    "latency_ms": 15
  }
}
```

---

### 3.4 服务器推送事件

事件无 `id` 字段，客户端无需回复。

#### front.session.revoked — 管理员会话被顶替

```json
{
  "jsonrpc": "2.0",
  "action": "front.session.revoked",
  "params": {
    "reason": "账号在别处登录"
  }
}
```

---

#### front.data_changed — 数据变更通知

管理员执行任何写操作后推送，前端应刷新对应数据面板。

```json
{
  "jsonrpc": "2.0",
  "action": "front.data_changed",
  "params": {
    "reason": "device.auth",
    "at": "2026-05-22T15:30:00Z"
  }
}
```

**reason 取值：**
`front.login`、`family.create`、`family.visibility`、`family.bind_home_server`、`family.unbind_home_server`、`family.grant`、`family.revoke`、`device.auth`、`device.offline`、`device.blacklist`、`device.force_offline`、`device.lan_report`、`p2p.hole_punch`、`config.auth_code`、`config.password`、`stats.traffic`、`stats.latency`

---

#### device.force_offline — 设备被强制下线

```json
{
  "jsonrpc": "2.0",
  "action": "device.force_offline",
  "params": {
    "reason": "blacklisted"
  }
}
```

**reason 取值：** `"blacklisted"`（被拉黑）、`"forced"`（管理员手动踢出）

---

#### device.latency_probe — 延迟探测

服务器每 10 秒向所有在线设备发送，设备应回复 `stats.latency_pong`。

```json
{
  "jsonrpc": "2.0",
  "action": "device.latency_probe",
  "params": {
    "probe_id": "p1a2b3c4",
    "sent_at": "2026-05-22T15:30:00Z"
  }
}
```

---

#### p2p.hole_punch_offer — P2P 打洞邀请

服务器将客户端的打洞请求转发给家庭服务器。

```json
{
  "jsonrpc": "2.0",
  "action": "p2p.hole_punch_offer",
  "params": {
    "session_id": "s1a2b3c4d5e6f7a8",
    "family_id": 1,
    "request": { ... },
    "client": { ... },
    "server": { ... }
  }
}
```

---

#### p2p.candidate — 候选地址转发

```json
{
  "jsonrpc": "2.0",
  "action": "p2p.candidate",
  "params": {
    "from_device_id": "client-e5f6a1b2",
    "candidate": "203.0.113.5:47778",
    "session_id": "s1a2b3c4d5e6f7a8"
  }
}
```

---

#### family.lan_changed — 家庭网段变化

家庭服务器上报网段变化后，服务器通知所有有权限的客户端。

```json
{
  "jsonrpc": "2.0",
  "action": "family.lan_changed",
  "params": {
    "family_id": 1,
    "lan_cidr": "192.168.2.0/24"
  }
}
```

---

## 4. UDP 隧道协议

### 数据包格式

#### 控制包（Probe / Hello）

```
[magic(4)] [version(1)] [kind(1)] [json_body(N)]
```

- magic: `GHU1` (Go Home UDP v1)
- version: `1`
- kind: `1`(Probe) / `2`(Hello)

#### 加密帧（Frame）

```
[magic(4)] [version(1)] [kind=3(1)] [sessionLen(1)] [sessionID(N)] [sequence(8)] [nonce(12)] [SM4-GCM_ciphertext+tag(M)]
```

- header（magic~sequence）作为 SM4-GCM 的 AAD（Additional Authenticated Data）
- nonce: 12 字节随机值
- ciphertext: SM4-GCM 加密后的 `[frameType(1)] [payload(N)]`

### 帧类型

| 值 | 名称 | 说明 |
|----|------|------|
| 1 | FrameReady | 会话就绪，包含 LAN 信息和设备映射 |
| 2 | FrameKeepalive | 保活帧，维持 NAT 映射 |
| 3 | FramePing | 隧道 ping，需回复 FramePong |
| 4 | FramePong | 隧道 pong |
| 5 | FrameIPv4 | IPv4 数据帧 |
| 6 | FrameEthernet | 以太网帧（预留） |

### 打洞流程

```
客户端                     服务器                    家庭服务器
  │                          │                          │
  │── p2p.hole_punch_req ──►│                          │
  │◄── HolePunchOffer ──────│── p2p.hole_punch_offer ─►│
  │                          │                          │
  │──── UDP Probe ─────────────────────────────────────►│
  │◄─── UDP Probe ──────────────────────────────────────│
  │                          │                          │
  │──── UDP Hello ────────────────────────────────────►│
  │◄─── UDP FrameReady ───────────────────────────────│
  │                          │                          │
  │──── FramePing ────────────────────────────────────►│
  │◄─── FramePong ─────────────────────────────────────│
  │                          │                          │
  │═════════ P2P UDP 加密隧道建立 ═════════════════════│
```

---

## 5. 加密体系

### 概览

| 层级 | 算法 | 用途 |
|------|------|------|
| 设备身份 | SM2 | 密钥对生成、Device ID 派生（SM3 摘要） |
| WebSocket 认证 | TimeKey (SM3-HMAC) | 时间窗口校验，连续 3 次失败强制下线 |
| UDP 隧道握手 | SM2 非对称加密 | 加密 SM4 会话密钥交换材料 |
| UDP 隧道数据 | SM4-GCM 对称加密 + SM3-HMAC 完整性 | 隧道数据加密，含 AAD 认证 |
| 防重放 | 64 位滑动窗口 | UDP 帧序列号防重放 |
| 密码存储 | SM3-HMAC + salt | 管理员密码哈希 |

### TimeKey 生成算法

```
window = timestamp / 30
time_key = HMAC-SM3(secret, strconv.FormatInt(window))
```

校验时在 `[clientWindow - 2, clientWindow + 2]` 范围内匹配，容忍 ±60 秒偏差。

### Device ID 派生算法

```
digest = SM3(public_key_pem)
device_id = prefix + "-" + hex(digest[:16])
```

### 密码哈希算法

```
salt = random_hex(16)
hash = HMAC-SM3(salt, password)
stored = "sm3:" + salt + "$" + hash
```

---

## 6. 错误码参考

| 错误码 | 说明 | 触发场景 |
|--------|------|----------|
| `bad_request` | 请求参数错误 | 参数缺失、格式错误、JSON 解析失败 |
| `unauthorized` | 认证失败 | 密码错误、授权码错误、未登录、token 无效 |
| `forbidden` | 权限不足 | 客户端无权访问指定家庭 |
| `blacklisted` | 设备被拉黑 | 设备在黑名单中尝试认证 |
| `not_available` | 资源不可用 | 家庭服务器离线、家庭未绑定服务器 |
| `time_key_invalid` | TimeKey 校验失败 | time_key 与预期不匹配 |
| `clock_skew` | 时钟偏差过大 | 客户端与服务器时间差超过容忍范围 |
| `internal_error` | 服务器内部错误 | 数据库错误、其他运行时异常 |
| `unknown_action` | 未知动作 | 请求的 action 不存在 |

---

## 7. HTTP 控制接口（客户端本地）

client-core 在控制模式下提供本地 HTTP API，供 UI 壳（WebView）调用。

基础地址：`http://127.0.0.1:47779`

### API 端点

#### POST /api/connect — 连接公网服务器

**请求体：**
```json
{
  "server": "ws://example.com:8080/ws",
  "auth_code": "MY-SECRET-CODE"
}
```

**成功响应：** `{ "ok": true }`

**失败响应（400）：**
```json
{ "error": "public server is not connected" }
```

---

#### GET /api/families — 列出可访问家庭

**成功响应：**
```json
[
  {
    "id": 1,
    "name": "我的家庭",
    "visibility": "public",
    "home_server_online": true,
    "lan_cidr": "192.168.1.0/24"
  }
]
```

---

#### POST /api/conflict — 检查网络冲突

**请求体：**
```json
{ "lan_cidr": "192.168.1.0/24" }
```

**成功响应：**
```json
{
  "conflict": false,
  "overlaps": []
}
```

**冲突响应：**
```json
{
  "conflict": true,
  "overlaps": ["192.168.1.0/24 (Wi-Fi)"]
}
```

---

#### POST /api/tunnel/connect — 建立隧道

**请求体：**
```json
{
  "family_id": 1,
  "mode": "real",
  "virtual_cidr": "",
  "client_virtual_mac": ""
}
```

**成功响应：**
```json
{
  "session_id": "s1a2b3c4d5e6f7a8",
  "family_id": 1,
  "mode": "real",
  "client_home_ip": "192.168.1.100",
  "lan_cidr": "192.168.1.0/24",
  "devices": [
    { "real_ip": "192.168.1.1", "virtual_ip": "192.168.1.1", "mac": "aa:bb:cc:dd:ee:ff" }
  ]
}
```

---

#### GET /api/tunnel — 获取活跃隧道信息

**成功响应：** 同 `/api/tunnel/connect` 的响应格式

**失败响应（409）：**
```json
{ "error": "tunnel is not connected" }
```

---

#### GET /api/status — 获取连接状态

**响应：**
```json
{
  "websocket": "connected",
  "udp": "connected",
  "grace_seconds": 0,
  "last_error": ""
}
```

**websocket 状态值：** `"idle"` / `"connected"` / `"reconnecting"` / `"grace"`
**udp 状态值：** `"idle"` / `"connected"`

---

#### GET /api/stats — 获取流量统计

**响应：**
```json
{
  "up": 1048576,
  "down": 2097152,
  "loss": 0.001,
  "latency_ms": 15,
  "tunnel_rtt_ms": 8
}
```

---

#### POST /api/disconnect — 断开连接

**成功响应：** `{ "ok": true }`

---

#### GET /api/update — 检查更新

**响应：**
```json
{
  "has_update": true,
  "current_version": "0.2.0",
  "latest_version": "0.3.0",
  "download_url": "https://cdn.example.com/client-pc-0.3.0.zip",
  "sha256": "abc123..."
}
```

---

## 8. 部署指南

### Docker 部署公网服务器

```bash
# 使用 docker-compose
docker compose up -d

# 或直接构建
docker build -f server/Dockerfile -t go-home-server .
docker run -d \
  -p 8080:8080 \
  -e GO_HOME_ADDR=:8080 \
  -e GO_HOME_DEFAULT_ADMIN_PASSWORD=your-password \
  -e GO_HOME_DEFAULT_AUTH_CODE=YOUR-SECRET-CODE \
  -v go-home-data:/data \
  go-home-server
```

### 手动部署公网服务器

```bash
cd server
go build -o go-home-server ./cmd/server
./go-home-server
```

环境变量：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `GO_HOME_ADDR` | `:8080` | 监听地址 |
| `GO_HOME_DB` | `data/go-home.db` | SQLite 数据库路径 |
| `GO_HOME_WEB_DIST` | `""` | Web 控制台静态文件目录 |
| `GO_HOME_DEFAULT_ADMIN_PASSWORD` | `admin` | 初始管理员密码 |
| `GO_HOME_DEFAULT_AUTH_CODE` | `GOHOME-CHANGE-ME` | 初始授权码 |

### 部署家庭服务器

```bash
cd home-server
go build -o go-home-server ./cmd/home-server
./go-home-server \
  -server ws://YOUR_SERVER:8080/ws \
  -auth-code YOUR-SECRET-CODE \
  -udp-port 47777 \
  -upnp
```

命令行参数：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-server` | `ws://127.0.0.1:8080/ws` | 公网服务器 WebSocket URL |
| `-auth-code` | `GOHOME-CHANGE-ME` | 授权码 |
| `-auth-code-file` | `""` | 授权码文件路径 |
| `-udp-port` | `47777` | UDP 端口 |
| `-upnp` | `true` | 启用 UPnP |
| `-lan-cidr` | `""` | 强制指定 LAN CIDR |
| `-lan-interface` | `""` | 强制指定 LAN 网卡 |

### 发布到 GitHub

```powershell
.\scripts\publish-repos.ps1 -Message "update go home project" -Push
```

---

## 9. 开发指南

### 本地开发

```bash
# 初始化 Go workspace
go work sync

# 运行公网服务器
go run ./server/cmd/server

# 运行家庭服务器
go run ./home-server/cmd/home-server -server ws://127.0.0.1:8080/ws -auth-code GOHOME-CHANGE-ME

# 运行客户端（控制模式）
go run ./client-core/cmd/client-core -control-addr 127.0.0.1:47779 -ui-dir client-ui/dist

# 构建 Web 控制台
cd web-console && npm install && npm run build

# 构建客户端 UI
cd client-ui && npm install && npm run build
```

### 项目目录结构

```
go home/
├── server/           # 公网服务器
│   ├── cmd/server/   # 入口
│   └── internal/     # config, store, ws
├── home-server/      # 家庭服务器
│   ├── cmd/home-server/
│   └── internal/     # lan, portmap
├── client-core/      # 客户端核心
│   └── cmd/client-core/
├── client-ui/        # 客户端 UI (Vue3)
├── client-pc/        # PC 客户端壳 (C#)
├── client-android/   # Android 客户端壳 (Kotlin)
├── client-ios/       # iOS 客户端壳 (规划中)
├── client-harmony/   # 鸿蒙客户端壳 (规划中)
├── web-console/      # Web 管理控制台 (Vue3)
├── shared/go/        # Go 共享库
│   ├── protocol/     # 协议定义
│   ├── security/     # SM2/SM3 密码操作
│   └── tunnel/       # UDP 隧道封包
├── scripts/          # 发布脚本
├── docs/             # 工程文档
└── cdn/              # 版本清单
```

### Go Workspace 依赖

```
go.work
├── client-core/  ──┐
├── home-server/  ──┤── 依赖 ──► shared/go
├── server/       ──┤
└── shared/go     ◄─┘
```

所有 Go 模块通过 `replace gohome/shared => ../shared/go` 指向本地共享库。

### 编码规范

1. 所有导出函数/类型必须有 GoDoc 注释
2. 错误处理不得使用 `_ =` 忽略（除非明确安全，如日志写入）
3. 协议变更需同步更新 `shared/go/protocol/` 和本文档
4. 国密算法的跨端兼容性必须验证（Go / Android BouncyCastle）
5. 新增 API 需添加到本文档的 API 参考中
