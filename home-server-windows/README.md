# Go Home Windows Home Server

This directory contains the Windows desktop home-server manager.

The Windows app is a small WinUI 3 shell. It starts the Go home-server core, connects it to the public Go Home server, and shows runtime statistics such as connected clients, assigned client IP addresses, and traffic counters.

## Current Capability

The Windows app can:

- Connect the Go home-server core to the public server.
- Send the authorization code and register as a home-server device.
- Create a Wintun home-side virtual adapter.
- Enable Windows IPv4 forwarding and Windows NetNat for tunnel traffic.
- Translate the client's real home-LAN IP to an internal `100.64.78.0/24` address at the Windows TUN boundary.
- Display connection state.
- Display total uploaded/downloaded tunnel traffic.
- Display connected client count, client device ID, client IP, and per-client traffic.

Important limitation:

- Windows uses Wintun + Windows NetNat instead of Linux ProxyARP/nftables.
- Run the app as administrator; otherwise adapter, forwarding, and NAT setup will fail.

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

The app manifest requests administrator privileges because Windows network setup requires it.

## Common Errors

The app says the Go core is missing:

- Ensure `go-home-home-server.exe` is next to `GoHome.HomeServer.Windows.exe`.
- Use the GitHub Actions artifact, which already bundles both files.

Authentication fails:

- Confirm the authorization code is current.
- Confirm the public server URL ends with `/ws`; the app will add it automatically when possible.

No client statistics are shown:

- Statistics appear after clients establish UDP sessions.
- Confirm the app was started as administrator.
- Confirm Windows Defender Firewall or other security software allows the generated Wintun adapter and the Go core process.

Build fails with Windows App SDK PRI or packaging task errors:

- Build on a Windows machine with Visual Studio 2022 Build Tools installed.
- GitHub Actions uses `windows-latest`, `setup-dotnet`, and `setup-msbuild` for this reason.

## Security

Do not commit real server IP addresses, authorization codes, passwords, private keys, SSH keys, or GitHub tokens.
