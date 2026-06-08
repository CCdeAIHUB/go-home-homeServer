#!/bin/sh
set -eu

INSTALL_DIR="${GO_HOME_INSTALL_DIR:-/opt/go-home}"
CONFIG_DIR="${GO_HOME_CONFIG_DIR:-/etc/go-home}"
BIN_URL="${GO_HOME_HOME_SERVER_BINARY_URL:-}"
SERVER_WS="${GO_HOME_SERVER_WS:?Set GO_HOME_SERVER_WS, for example ws://example.com:8080/ws}"
AUTH_CODE_FILE="${GO_HOME_AUTH_CODE_FILE:-$CONFIG_DIR/auth-code}"
LAN_CIDR="${GO_HOME_LAN_CIDR:-}"
LAN_INTERFACE="${GO_HOME_LAN_INTERFACE:-}"
UDP_PORT="${GO_HOME_UDP_PORT:-47777}"
UDP_SOCKETS="${GO_HOME_UDP_SOCKETS:-8}"

mkdir -p "$INSTALL_DIR" "$CONFIG_DIR"

TMP_BIN="$(mktemp)"
if [ -n "$BIN_URL" ]; then
  curl -fsSL "$BIN_URL" -o "$TMP_BIN"
elif [ -f ./go-home-homeserver ]; then
  cp ./go-home-homeserver "$TMP_BIN"
elif [ -f ./go-home-server ]; then
  cp ./go-home-server "$TMP_BIN"
else
  echo "Set GO_HOME_HOME_SERVER_BINARY_URL or place go-home-homeserver in the current directory." >&2
  exit 1
fi
cp "$TMP_BIN" "$INSTALL_DIR/go-home-homeserver"
chmod 0755 "$INSTALL_DIR/go-home-homeserver"
rm -f "$TMP_BIN"

if [ -n "${GO_HOME_AUTH_CODE:-}" ]; then
  printf '%s\n' "$GO_HOME_AUTH_CODE" > "$AUTH_CODE_FILE"
  chmod 600 "$AUTH_CODE_FILE"
fi
if [ ! -s "$AUTH_CODE_FILE" ]; then
  echo "Set GO_HOME_AUTH_CODE or create $AUTH_CODE_FILE before starting the service." >&2
  exit 1
fi

cat > "$CONFIG_DIR/home-server.env" <<EOF
SERVER_WS='$SERVER_WS'
AUTH_CODE_FILE='$AUTH_CODE_FILE'
LAN_CIDR='$LAN_CIDR'
LAN_INTERFACE='$LAN_INTERFACE'
UDP_PORT='$UDP_PORT'
UDP_SOCKETS='$UDP_SOCKETS'
EOF
chmod 600 "$CONFIG_DIR/home-server.env"

cat > /etc/init.d/go-home-homeserver <<'EOF'
#!/bin/sh /etc/rc.common
START=95
STOP=10
USE_PROCD=1

start_service() {
  . /etc/go-home/home-server.env
  procd_open_instance
  procd_set_param command /opt/go-home/go-home-homeserver \
    -server "$SERVER_WS" \
    -auth-code-file "$AUTH_CODE_FILE" \
    -udp-port "$UDP_PORT" \
    -udp-sockets "$UDP_SOCKETS"
  [ -n "$LAN_CIDR" ] && procd_append_param command -lan-cidr "$LAN_CIDR"
  [ -n "$LAN_INTERFACE" ] && procd_append_param command -lan-interface "$LAN_INTERFACE"
  procd_set_param respawn 5 5 0
  procd_set_param stdout 1
  procd_set_param stderr 1
  procd_close_instance
}
EOF

chmod +x /etc/init.d/go-home-homeserver
/etc/init.d/go-home-homeserver enable
/etc/init.d/go-home-homeserver restart
/etc/init.d/go-home-homeserver status || true
