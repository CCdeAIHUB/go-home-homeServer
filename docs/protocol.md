# WebSocket JSON-RPC 协议

所有业务 API 通过 WebSocket 传输，入口为 `/ws`。

## 基础包格式

```json
{
  "jsonrpc": "2.0",
  "id": "request-id",
  "action": "device.auth",
  "params": {}
}
```

响应格式：

```json
{
  "jsonrpc": "2.0",
  "id": "request-id",
  "result": {}
}
```

错误格式：

```json
{
  "jsonrpc": "2.0",
  "id": "request-id",
  "error": {
    "code": "unauthorized",
    "message": "unauthorized"
  }
}
```

服务端主动事件使用 `action` 且不带 `id`。

## 关键 Action

### 管理端

- `front.login`
- `front.dashboard`
- `front.family.list`
- `front.family.create`
- `front.family.set_visibility`
- `front.family.bind_home_server`
- `front.family.unbind_home_server`
- `front.device.list`
- `front.device.blacklist`
- `front.device.force_offline`
- `front.config.get`
- `front.config.update_auth_code`
- `front.config.update_password`
- `front.log.list`

### 设备端

- `device.auth`
- `device.lan.report`
- `client.family.list`
- `p2p.hole_punch_req`
- `p2p.candidate`
- `stats.traffic_report`
- `stats.latency_pong`
- `ping`

### 服务端事件

- `front.session.revoked`
- `front.data_changed`
- `device.force_offline`
- `device.latency_probe`
- `p2p.hole_punch_offer`
- `p2p.candidate`
- `family.lan_changed`

## 热数据更新

公网服务器在家庭、设备、配置、流量、延迟、LAN 网段等数据变化后，向当前 Web 控制台推送 `front.data_changed` 事件。控制台收到事件后自动重新拉取仪表盘、家庭、设备、日志和配置数据，不再依赖手动刷新按钮。

## 延迟计算

公网服务器周期性向设备发送 `device.latency_probe`：

```json
{
  "jsonrpc": "2.0",
  "action": "device.latency_probe",
  "params": {
    "probe_id": "random-id",
    "sent_at": "2026-05-21T00:00:00Z"
  }
}
```

设备收到后通过 `stats.latency_pong` 回传 `probe_id`。公网服务器按发送时间与收到时间计算 RTT，并在设备列表和客户端状态能力中暴露 `latency_ms`。

