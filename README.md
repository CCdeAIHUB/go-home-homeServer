# Go Home Home Server

Go Home Home Server runs inside the home LAN. It connects to the public Go Home server for signaling, performs pure UDP hole punching, creates the home-side tunnel path, and forwards encrypted traffic between the remote client and LAN devices.

It does not relay traffic through the public server.

## What You Need

- A public Go Home Server already running.
- The public server WebSocket URL, for example `ws://YOUR_PUBLIC_SERVER:8080/ws`.
- The public server authorization code.
- A Linux/OpenWrt device inside the home LAN.
- Permission to run as root, because the home server needs network interface and routing access.

One family should bind only one home server.

## Windows Desktop Home Server

This repository also contains a Windows desktop manager in `home-server-windows`.

The Windows app uses WinUI 3 for the interface and starts the Go home-server core in the background. It provides:

- Public server connection page.
- Authorization-code login flow.
- Loading state while connecting.
- Wintun home-side virtual adapter setup.
- Windows IPv4 forwarding and Windows NetNat setup.
- Statistics page with total traffic, connected client count, client IP, and per-client traffic.

Windows uses Wintun + Windows NetNat instead of Linux ProxyARP/nftables. The app requests administrator privileges because adapter, forwarding, and NAT setup require elevated permissions.

GitHub Actions uploads the Windows artifact as `go-home-home-server-windows-x64`.

## Recommended OpenWrt Install

Download the latest `go-home-server-linux-amd64` or `go-home-server-linux-arm64` artifact from this repository Actions page. Put the binary on the router, then run:

```sh
chmod +x go-home-server

GO_HOME_SERVER_WS='ws://YOUR_PUBLIC_SERVER:8080/ws' \
GO_HOME_AUTH_CODE='replace-with-your-auth-code' \
GO_HOME_LAN_CIDR='192.168.1.0/24' \
GO_HOME_LAN_INTERFACE='br-lan' \
sh scripts/install-openwrt.sh
```

If the binary is hosted at a URL:

```sh
GO_HOME_HOME_SERVER_BINARY_URL='https://example.com/go-home-server' \
GO_HOME_SERVER_WS='ws://YOUR_PUBLIC_SERVER:8080/ws' \
GO_HOME_AUTH_CODE='replace-with-your-auth-code' \
GO_HOME_LAN_CIDR='192.168.1.0/24' \
GO_HOME_LAN_INTERFACE='br-lan' \
sh scripts/install-openwrt.sh
```

The script writes the auth code to `/etc/go-home/auth-code` and starts `/etc/init.d/go-home-homeserver`.

## Linux systemd Install

```sh
chmod +x go-home-server

sudo GO_HOME_SERVER_WS='ws://YOUR_PUBLIC_SERVER:8080/ws' \
  GO_HOME_AUTH_CODE='replace-with-your-auth-code' \
  GO_HOME_LAN_CIDR='192.168.1.0/24' \
  GO_HOME_LAN_INTERFACE='eth0' \
  sh scripts/install-systemd.sh
```

## Important Settings

| Setting | Purpose |
| --- | --- |
| `GO_HOME_SERVER_WS` | Public server WebSocket URL |
| `GO_HOME_AUTH_CODE` | Public server authorization code; written to a protected file |
| `GO_HOME_AUTH_CODE_FILE` | Existing auth-code file path |
| `GO_HOME_LAN_CIDR` | Home LAN CIDR, for example `192.168.1.0/24` |
| `GO_HOME_LAN_INTERFACE` | LAN interface, often `br-lan` on OpenWrt |
| `GO_HOME_UDP_PORT` | Local UDP port, default `47777` |
| `GO_HOME_UDP_SOCKETS` | Number of UDP sockets used during hole punching, default `8` |

## Upgrade

OpenWrt:

```sh
/etc/init.d/go-home-homeserver stop
GO_HOME_SERVER_WS='ws://YOUR_PUBLIC_SERVER:8080/ws' \
GO_HOME_AUTH_CODE='replace-with-your-auth-code' \
GO_HOME_LAN_CIDR='192.168.1.0/24' \
GO_HOME_LAN_INTERFACE='br-lan' \
sh scripts/install-openwrt.sh
```

Linux systemd:

```sh
sudo systemctl stop go-home-homeserver
sudo GO_HOME_SERVER_WS='ws://YOUR_PUBLIC_SERVER:8080/ws' \
  GO_HOME_AUTH_CODE='replace-with-your-auth-code' \
  GO_HOME_LAN_CIDR='192.168.1.0/24' \
  GO_HOME_LAN_INTERFACE='eth0' \
  sh scripts/install-systemd.sh
```

## Logs

OpenWrt:

```sh
logread -f | grep go-home
/etc/init.d/go-home-homeserver status
```

Linux systemd:

```sh
journalctl -u go-home-homeserver -f
systemctl status go-home-homeserver
```

## Troubleshooting

Home server does not appear online:

- Confirm `GO_HOME_SERVER_WS` points to `/ws`.
- Confirm the authorization code is correct.
- Confirm the public server TCP port is reachable from the home network.
- Check the logs above.

UDP direct tunnel times out:

- Confirm the public server UDP discovery port is open.
- Confirm the home server process is running.
- Confirm the home router/firewall allows outbound UDP.
- Keep only one home server bound to a family.

LAN devices cannot be reached:

- Confirm `GO_HOME_LAN_CIDR` matches the real home LAN.
- Confirm `GO_HOME_LAN_INTERFACE` is the LAN bridge/interface.
- On OpenWrt this is usually `br-lan`; on normal Linux it may be `eth0`, `enp*`, or a bridge name.

Full-home mode does not use router proxy rules:

- Make sure the home router itself is the LAN gateway and DNS policy point.
- If using OpenWrt/Passwall, keep router-side transparent proxy rules enabled for LAN-originated traffic.
- Go Home assigns internal `100.64.78.0/24` implementation addresses on the home side so router-local proxy systems can see tunnel traffic consistently.

Permission errors:

- Run the install script as root.
- Make sure the binary is executable.
- Make sure `/etc/go-home/auth-code` exists and is readable by the service.

## Local Development

```bash
go work sync
go test ./...
cd home-server
go run ./cmd/home-server \
  -server ws://YOUR_PUBLIC_SERVER:8080/ws \
  -auth-code-file /path/to/auth-code \
  -lan-cidr 192.168.1.0/24 \
  -lan-interface br-lan
```

## CI Artifacts

Every push to `main` runs GitHub Actions. The workflow:

- runs Go tests on Linux and macOS;
- builds Linux amd64 and arm64 binaries;
- builds the Windows WinUI 3 home-server manager for x64;
- publishes tagged releases for `v*` tags.

## Security Notes

- Do not commit real authorization codes, SSH credentials, private keys, or personal server addresses.
- Prefer `GO_HOME_AUTH_CODE_FILE` over command-line `-auth-code` in production.
- Rotate the public server authorization code if it was exposed.
