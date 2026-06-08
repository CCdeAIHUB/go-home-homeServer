#!/usr/bin/env sh
set -eu

INSTALL_DIR="${GO_HOME_INSTALL_DIR:-/opt/go-home}"
CONFIG_DIR="${GO_HOME_CONFIG_DIR:-/etc/go-home}"
DATA_DIR="${GO_HOME_DATA_DIR:-/var/lib/go-home-homeserver}"
BIN_URL="${GO_HOME_HOME_SERVER_BINARY_URL:-}"
SERVER_WS="${GO_HOME_SERVER_WS:?Set GO_HOME_SERVER_WS, for example ws://example.com:8080/ws}"
AUTH_CODE_FILE="${GO_HOME_AUTH_CODE_FILE:-$CONFIG_DIR/auth-code}"
LAN_CIDR="${GO_HOME_LAN_CIDR:-}"
LAN_INTERFACE="${GO_HOME_LAN_INTERFACE:-}"
UDP_PORT="${GO_HOME_UDP_PORT:-47777}"
UDP_SOCKETS="${GO_HOME_UDP_SOCKETS:-8}"
SERVICE_USER="${GO_HOME_SERVICE_USER:-go-home}"

if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root, for example: sudo sh scripts/install-systemd.sh" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR" "$CONFIG_DIR" "$DATA_DIR"

if ! id "$SERVICE_USER" >/dev/null 2>&1; then
  useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
fi

TMP_BIN="$(mktemp)"
if [ -n "$BIN_URL" ]; then
  curl -fsSL "$BIN_URL" -o "$TMP_BIN"
elif [ -f ./go-home-homeserver ]; then
  cp ./go-home-homeserver "$TMP_BIN"
else
  cp ./go-home-server "$TMP_BIN"
fi
install -m 0755 "$TMP_BIN" "$INSTALL_DIR/go-home-homeserver"
rm -f "$TMP_BIN"

if [ -n "${GO_HOME_AUTH_CODE:-}" ]; then
  printf '%s\n' "$GO_HOME_AUTH_CODE" > "$AUTH_CODE_FILE"
fi
if [ ! -s "$AUTH_CODE_FILE" ]; then
  echo "Set GO_HOME_AUTH_CODE or create $AUTH_CODE_FILE before starting the service." >&2
  exit 1
fi
chmod 600 "$AUTH_CODE_FILE"
chown -R "$SERVICE_USER:$SERVICE_USER" "$DATA_DIR"

cat > "$CONFIG_DIR/home-server.env" <<EOF
SERVER_WS=$SERVER_WS
AUTH_CODE_FILE=$AUTH_CODE_FILE
LAN_CIDR=$LAN_CIDR
LAN_INTERFACE=$LAN_INTERFACE
UDP_PORT=$UDP_PORT
UDP_SOCKETS=$UDP_SOCKETS
EOF
chmod 600 "$CONFIG_DIR/home-server.env"

cat > /etc/systemd/system/go-home-homeserver.service <<EOF
[Unit]
Description=Go Home home server
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=$CONFIG_DIR/home-server.env
WorkingDirectory=$DATA_DIR
ExecStart=$INSTALL_DIR/go-home-homeserver -server \${SERVER_WS} -auth-code-file \${AUTH_CODE_FILE} -udp-port \${UDP_PORT} -udp-sockets \${UDP_SOCKETS} -lan-cidr \${LAN_CIDR} -lan-interface \${LAN_INTERFACE}
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now go-home-homeserver
systemctl --no-pager status go-home-homeserver
