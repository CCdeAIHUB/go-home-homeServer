# Home Server Binary

This directory contains the Go Home home-side server.

The home server runs inside the home LAN, connects to the public server over WebSocket, performs UDP hole punching, and forwards encrypted tunnel traffic to LAN devices.

## Run From Source

```bash
go run ./cmd/home-server \
  -server ws://YOUR_PUBLIC_SERVER:8080/ws \
  -auth-code-file /path/to/auth-code \
  -lan-cidr 192.168.1.0/24 \
  -lan-interface br-lan
```

Use `-auth-code-file` instead of `-auth-code` in production so the authorization code is not visible in process arguments.

## Production Install

Use the repository-level scripts:

- OpenWrt/procd: `scripts/install-openwrt.sh`
- Linux/systemd: `scripts/install-systemd.sh`

## Common Flags

| Flag | Purpose |
| --- | --- |
| `-server` | Public server WebSocket URL |
| `-auth-code` | Authorization code, useful for local testing only |
| `-auth-code-file` | File containing the authorization code |
| `-udp-port` | Local UDP port, default `47777` |
| `-udp-sockets` | UDP socket count for hole punching |
| `-upnp` | Try UPnP same-port UDP mapping |
| `-nat-pmp` | Try NAT-PMP mapping |
| `-lan-cidr` | Home LAN CIDR |
| `-lan-interface` | Home LAN interface, often `br-lan` on OpenWrt |

## Common Errors

Home server is offline in the web console:

- Confirm the public server URL ends with `/ws`.
- Confirm the authorization code is correct.
- Check `logread -f` on OpenWrt or `journalctl -u go-home-homeserver -f` on Linux.

Tunnel handshake times out:

- Confirm the public server UDP port is open.
- Confirm outbound UDP is allowed from the home network.
- Confirm only one home server is bound to the target family.

LAN device access fails:

- Confirm `-lan-cidr` matches the real home subnet.
- Confirm `-lan-interface` is the LAN bridge/interface, not the WAN interface.

## Security

Do not commit real authorization codes, SSH credentials, private keys, or personal server IP addresses.
