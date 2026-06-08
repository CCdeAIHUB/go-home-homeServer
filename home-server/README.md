# 家庭服务器

家庭服务器运行在家庭局域网中，负责连接公网服务器、参与 UDP 直连打洞、创建家庭侧虚拟链路，并把加密隧道中的 IPv4 数据转发到家庭局域网。

- WebSocket 鉴权与重连。
- LAN 网段自动检测与上报。
- UDP 监听入口。
- P2P 打洞信令事件接收、UDP probe 与直连握手。
- SM2 会话密钥解封与 SM4 加密隧道 ready/ping/pong 帧。
- 同端口 UPnP UDP 映射尝试；失败时继续纯直连打洞，不引入中继。

启动：

```powershell
go run ./cmd/home-server -server ws://127.0.0.1:8080/ws -auth-code GOHOME-CHANGE-ME
```

在路由器类系统上，若自动探测会先命中 WAN 或上游接口，可以显式指定家庭 LAN：

```powershell
go run ./cmd/home-server -server ws://your-server.example.com:8080/ws -auth-code GOHOME-CHANGE-ME -lan-cidr 192.168.3.0/24 -lan-interface br-lan
```

在服务脚本中建议将授权码写入权限受限的文件，并使用 `-auth-code-file` 读取，避免把授权码暴露在进程参数里。

家庭路由器支持 UPnP 时默认会尝试把 `-udp-port` 映射到相同外部 UDP 端口。受限网络或不希望自动映射时可用 `-upnp=false` 关闭。

## 部署方式

### OpenWrt / procd

```sh
GO_HOME_HOME_SERVER_BINARY_URL="https://example.com/go-home-homeserver" \
GO_HOME_SERVER_WS="ws://YOUR_PUBLIC_SERVER:8080/ws" \
GO_HOME_AUTH_CODE="YOUR_AUTH_CODE" \
GO_HOME_LAN_CIDR="192.168.3.0/24" \
GO_HOME_LAN_INTERFACE="br-lan" \
sh scripts/install-openwrt.sh
```

### Linux systemd

```sh
sudo GO_HOME_HOME_SERVER_BINARY_URL="https://example.com/go-home-homeserver" \
  GO_HOME_SERVER_WS="ws://YOUR_PUBLIC_SERVER:8080/ws" \
  GO_HOME_AUTH_CODE="YOUR_AUTH_CODE" \
  GO_HOME_LAN_CIDR="192.168.3.0/24" \
  GO_HOME_LAN_INTERFACE="br-lan" \
  sh scripts/install-systemd.sh
```

若未设置 `GO_HOME_HOME_SERVER_BINARY_URL`，脚本会优先使用当前目录中的 `./go-home-homeserver`，其次兼容 CI 旧产物名 `./go-home-server`。
