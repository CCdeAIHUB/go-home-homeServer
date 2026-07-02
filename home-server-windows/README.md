# Go Home Windows Home Server

This directory contains the Windows desktop home-server manager.

The Windows app is a small WinUI 3 shell. It starts the Go home-server core, connects it to the public Go Home server, and shows runtime statistics such as connected clients, assigned client IP addresses, and traffic counters.

## Current Capability

The Windows app can:

- Connect the Go home-server core to the public server.
- Send the authorization code and register as a home-server device.
- Display connection state.
- Display total uploaded/downloaded tunnel traffic.
- Display connected client count, client device ID, client IP, and per-client traffic.

Important limitation:

- The existing full LAN forwarding path still targets Linux/OpenWrt. Windows builds currently do not provide the Linux TUN, DHCP proxy, ProxyARP, nftables, and LAN routing behavior used by the production home-server deployment.
- For production "home LAN gateway" usage, prefer OpenWrt/iStoreOS/Linux deployment until the Windows network driver path is implemented.

## Build

Requirements:

- Windows 10 19041 or newer.
- Visual Studio 2022 Build Tools or Visual Studio with Windows App SDK support.
- .NET 8 SDK.
- Go 1.25.1 or newer.

From the repository root:

```powershell
cd home-server
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -o go-home-home-server-windows-amd64.exe ./cmd/home-server

cd ..
dotnet publish .\home-server-windows\GoHome.HomeServer.Windows.csproj `
  -c Release `
  -r win-x64 `
  --self-contained true `
  -p:PublishSingleFile=false `
  -o .\artifacts\go-home-home-server-windows
```

Run:

```powershell
.\artifacts\go-home-home-server-windows\GoHome.HomeServer.Windows.exe
```

## Use

1. Open the app.
2. Enter the public server address, for example `ws://YOUR_PUBLIC_SERVER:8080/ws`.
3. Enter the public server authorization code.
4. Optionally enter the home LAN CIDR and local interface name.
5. Click **连接**.
6. After the Go core starts, the app switches to the statistics page.

## Common Errors

The app says the Go core is missing:

- Ensure `go-home-home-server.exe` is next to `GoHome.HomeServer.Windows.exe`.
- Use the GitHub Actions artifact, which already bundles both files.

Authentication fails:

- Confirm the authorization code is current.
- Confirm the public server URL ends with `/ws`; the app will add it automatically when possible.

No client statistics are shown:

- Statistics appear after clients establish UDP sessions.
- The Windows build can register and expose statistics, but full LAN forwarding remains Linux/OpenWrt-oriented.

Build fails with Windows App SDK PRI or packaging task errors:

- Build on a Windows machine with Visual Studio 2022 Build Tools installed.
- GitHub Actions uses `windows-latest`, `setup-dotnet`, and `setup-msbuild` for this reason.

## Security

Do not commit real server IP addresses, authorization codes, passwords, private keys, SSH keys, or GitHub tokens.
