# 家庭服务器

家庭服务器运行在家庭局域网中，当前骨架已经包含：

- WebSocket 鉴权与重连。
- LAN 网段自动检测与上报。
- UDP 监听入口。
- P2P 打洞信令事件接收。

启动：

```powershell
go run ./cmd/home-server -server ws://127.0.0.1:8080/ws -auth-code GOHOME-CHANGE-ME
```

后续需要在 `internal` 中继续实现：

- DHCP 代申请。
- ARP 代理。
- LAN 扫描。
- UDP 打洞策略。
- SM4 隧道加密数据面。

