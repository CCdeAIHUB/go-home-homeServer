# Go Home

Go Home 是一个坚持纯 P2P UDP 打洞的虚拟局域网组网工具。公网服务器只负责认证、家庭管理、设备状态和 P2P 信令协调，绝不提供中继转发。

## 当前仓库状态

本仓库已经生成第一版工程骨架：

- `server`：公网服务器，Go + SQLite + WebSocket JSON-RPC。
- `web-console`：服务器 Web 控制台，Vue3。
- `home-server`：家庭服务器，Go 后端服务与 CLI。
- `client-ui`：客户端共用内嵌 UI，Vue3 + JSAPI。
- `client-pc`：PC 客户端壳规划，C# Chromium/WebView。
- `client-ios`、`client-android`、`client-harmony`：移动端平台壳规划。
- `shared/go`：Go 侧共享协议、安全校验和类型定义。
- `docs`：工程说明与协议说明。

## 已定死的核心规则

- 不做中继，不采用 IPv6，不提供 fallback relay。
- 家庭只能由公网服务器 Web 控制台创建和管理。
- 客户端只能查看可见家庭并选择连接，不能创建、编辑、绑定或解绑家庭。
- 一个家庭只能绑定一台家庭服务器，但允许多个客户端同时连接。
- 默认使用家庭真实网段；本地网段冲突时启用备用虚拟网段映射。
- 全链路采用国密体系设计，跨端可使用不同实现，但协议结果必须兼容。

## 快速启动

```powershell
go work sync
go run ./server/cmd/server
```

服务器默认监听 `:8080`，WebSocket 入口为 `/ws`，SQLite 数据库为 `data/go-home.db`。

默认管理员密码为 `admin`，默认授权码为 `GOHOME-CHANGE-ME`。生产环境必须通过环境变量或控制台修改。

## 工程说明

先阅读 [docs/engineering-spec.md](./docs/engineering-spec.md)，它是当前实现的主约束文档。

## 发布到 GitHub

三个远程仓库：

- 公网服务器：`https://github.com/CCdeAIHUB/go-home-server.git`
- 家庭服务器：`https://github.com/CCdeAIHUB/go-home-homeServer.git`
- 客户端：`https://github.com/CCdeAIHUB/go-home-client.git`

同步并推送：

```powershell
.\scripts\publish-repos.ps1 -Message "update go home project" -Push
```

脚本会在 `.publish` 目录中克隆三个仓库，按项目边界同步文件，提交并推送。远程仓库的 GitHub Actions 会在 push 后自动编译。

客户端仓库当前优先发布 Android 与 Windows：

- Android：Actions Artifact `go-home-android-debug-apk`
- Windows：Actions Artifact `go-home-windows-win-x64`

其他客户端平台目录保留，但暂不进入 Actions 编译产物链路。
