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

在路由器类系统上，若自动探测会先命中 WAN 或上游接口，可以显式指定家庭 LAN：

```powershell
go run ./cmd/home-server -server ws://49.232.155.3:8080/ws -auth-code GOHOME-CHANGE-ME -lan-cidr 192.168.3.0/24 -lan-interface br-lan
```

在服务脚本中建议将授权码写入权限受限的文件，并使用 `-auth-code-file` 读取，避免把授权码暴露在进程参数里。

后续需要在 `internal` 中继续实现：

- DHCP 代申请。
- ARP 代理。
- LAN 扫描。
- UDP 打洞策略。
- SM4 隧道加密数据面。
