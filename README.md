# Go Home Home Server

Go Home Home Server runs inside the home LAN, connects to the public Go Home server for signaling, performs pure UDP hole punching, creates the home-side TUN path, leases/proxies a LAN IP for the remote client, and forwards encrypted tunnel packets to LAN devices.

It does not provide relay service and does not send LAN traffic through the public server.

## Quick Start

```bash
go work sync
go test ./...
cd home-server
go run ./cmd/home-server \
  -server ws://YOUR_PUBLIC_SERVER:8080/ws \
  -auth-code YOUR_AUTH_CODE \
  -lan-cidr 192.168.3.0/24 \
  -lan-interface br-lan
```

Use `-auth-code-file` in production to avoid exposing the auth code in process arguments.

## Important Flags

| Flag | Purpose |
| --- | --- |
| `-server` | Public server WebSocket URL, for example `ws://example.com:8080/ws` |
| `-auth-code` | Public server authorization code |
| `-auth-code-file` | File containing the authorization code |
| `-udp-port` | Local UDP port used for hole punching, default `47777` |
| `-udp-sockets` | Number of local UDP sockets used during hole punching |
| `-upnp` | Enable same-port UPnP mapping attempt |
| `-nat-pmp` | Enable NAT-PMP mapping attempt |
| `-lan-cidr` | Home LAN CIDR override |
| `-lan-interface` | Home LAN interface override, for example `br-lan` |

## One-Click OpenWrt Install

Download a GitHub Actions artifact or Release binary, then run on the router:

```sh
GO_HOME_HOME_SERVER_BINARY_URL="https://example.com/go-home-homeserver" \
GO_HOME_SERVER_WS="ws://YOUR_PUBLIC_SERVER:8080/ws" \
GO_HOME_AUTH_CODE="YOUR_AUTH_CODE" \
GO_HOME_LAN_CIDR="192.168.3.0/24" \
GO_HOME_LAN_INTERFACE="br-lan" \
sh scripts/install-openwrt.sh
```

If `GO_HOME_HOME_SERVER_BINARY_URL` is not set, the script uses `./go-home-homeserver` or `./go-home-server` in the current directory.

## One-Click Linux systemd Install

```sh
sudo GO_HOME_HOME_SERVER_BINARY_URL="https://example.com/go-home-homeserver" \
  GO_HOME_SERVER_WS="ws://YOUR_PUBLIC_SERVER:8080/ws" \
  GO_HOME_AUTH_CODE="YOUR_AUTH_CODE" \
  GO_HOME_LAN_CIDR="192.168.3.0/24" \
  GO_HOME_LAN_INTERFACE="br-lan" \
  sh scripts/install-systemd.sh
```

## Router Proxy Integration

When the client chooses full-home mode, Android uses the home router DNS path so router-side proxy policies can apply. On OpenWrt/Passwall routers, Go Home keeps public-DNS fallback traffic usable while allowing router-local DNS to enter the router proxy decision path. This makes the tunneled device behave closer to a normal LAN client.

For transparent proxy compatibility, each home-side Go Home TUN interface receives a local `100.64.78.x/32` IPv4 address derived from the client's leased home IP. This lets router-local redirect/tproxy services such as Passwall accept traffic from the Go Home interface without requiring per-device manual rules.

The `100.64.78.0/24` addresses are local implementation addresses only. They are not assigned to the remote client and should not be used as the family LAN subnet.

## CI Artifacts

Every push to `main` runs GitHub Actions and uploads the Linux amd64 home-server binary. Tagged versions (`v*`) are published as GitHub Releases.
