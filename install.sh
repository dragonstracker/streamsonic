#!/usr/bin/env bash
#
# streamsonic installer
#
#   curl -fsSL https://github.com/dragonstracker/streamsonic/releases/latest/download/install.sh \
#     | sudo DRMSONIC_TAG=latest bash
#
# Environment:
#   DRMSONIC_TAG      release tag to install, or "latest" (default)
#   DRMSONIC_PREFIX   install prefix (default /usr/local)
#   DRMSONIC_PORT     HTTP port written to a fresh config (default 8099)
#   DRMSONIC_NO_START set to 1 to skip enabling the systemd service
#
set -euo pipefail

REPO="dragonstracker/streamsonic"
TAG="${DRMSONIC_TAG:-${STREAMSONIC_TAG:-latest}}"
PREFIX="${DRMSONIC_PREFIX:-${STREAMSONIC_PREFIX:-/usr/local}}"
PORT="${DRMSONIC_PORT:-${STREAMSONIC_PORT:-8099}}"
NO_START="${DRMSONIC_NO_START:-${STREAMSONIC_NO_START:-0}}"

BIN_DIR="$PREFIX/bin"
CONF_DIR="/etc/streamsonic"
DATA_DIR="/var/lib/streamsonic"
UNIT="/etc/systemd/system/streamsonic.service"
BINARY="$BIN_DIR/streamsonic"
CONF="$CONF_DIR/streamsonic.conf"
WORK=""

log() { printf '  %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need_root() {
  [ "$(id -u)" -eq 0 ] || die "run as root (sudo)"
}

detect_platform() {
  case "$(uname -s)" in
    Linux) OS=linux ;;
    *) die "unsupported operating system: $(uname -s)" ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    armv7l|armv6l) ARCH=armv6 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac
  command -v curl >/dev/null 2>&1 || die "curl is required"
  command -v tar >/dev/null 2>&1 || die "tar is required"
}

fetch() {
  local url="$1" out="$2"
  if ! curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 15 -o "$out" "$url"; then
    return 1
  fi
}

resolve_asset() {
  ASSET="streamsonic-${OS}-${ARCH}.tar.gz"
  if [ "$TAG" = "latest" ] || [ -z "$TAG" ]; then
    BASE="https://github.com/${REPO}/releases/latest/download"
  else
    BASE="https://github.com/${REPO}/releases/download/${TAG}"
  fi
}

install_binary() {
  log "downloading ${ASSET}"
  fetch "${BASE}/${ASSET}" "$WORK/pkg.tar.gz" \
    || die "cannot download ${BASE}/${ASSET}"

  if fetch "${BASE}/checksums.txt" "$WORK/checksums.txt" 2>/dev/null; then
    local want got
    want="$(awk -v a="$ASSET" '$2==a {print $1}' "$WORK/checksums.txt" | head -n1)"
    if [ -n "$want" ]; then
      got="$(sha256sum "$WORK/pkg.tar.gz" | awk '{print $1}')"
      [ "$want" = "$got" ] || die "checksum mismatch for ${ASSET}"
      log "checksum verified"
    fi
  fi

  tar -xzf "$WORK/pkg.tar.gz" -C "$WORK"
  [ -f "$WORK/streamsonic" ] || die "archive does not contain the streamsonic binary"

  install -d -m 0755 "$BIN_DIR"
  install -m 0755 "$WORK/streamsonic" "$BINARY"
  log "installed $BINARY"
}

write_config() {
  install -d -m 0755 "$CONF_DIR" "$DATA_DIR"
  if [ -f "$CONF" ]; then
    log "keeping existing $CONF"
    return
  fi
  cat > "$CONF" <<EOF
http {
  port ${PORT};
}

dvr main {
  root ${DATA_DIR}/dvr;
  expiration 3600;
}
EOF
  chmod 0644 "$CONF"
  log "wrote $CONF"
}

write_unit() {
  cat > "$UNIT" <<EOF
[Unit]
Description=streamsonic media server
Documentation=https://github.com/${REPO}
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${BINARY} -config ${CONF}
Restart=always
RestartSec=2
LimitNOFILE=1048576
NoNewPrivileges=yes
ProtectSystem=full
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=${DATA_DIR} ${CONF_DIR}

[Install]
WantedBy=multi-user.target
EOF
  log "wrote $UNIT"
}

start_service() {
  if ! command -v systemctl >/dev/null 2>&1; then
    log "systemd not found, start manually: ${BINARY} -config ${CONF}"
    return
  fi
  systemctl daemon-reload
  systemctl enable streamsonic >/dev/null 2>&1 || true
  if [ "$NO_START" = "1" ]; then
    log "service enabled but not started (DRMSONIC_NO_START=1)"
    return
  fi
  systemctl restart streamsonic
  sleep 1
  if systemctl is-active --quiet streamsonic; then
    log "service active"
  else
    log "service failed to start, check: journalctl -u streamsonic -n 50"
  fi
}

summary() {
  local ip
  ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  [ -n "$ip" ] || ip="127.0.0.1"
  local port
  port="$(config_port)"
  [ -n "$port" ] || port="$PORT"
  printf '\nstreamsonic is installed\n'
  printf '  binary   %s\n' "$BINARY"
  printf '  config   %s\n' "$CONF"
  printf '  data     %s\n' "$DATA_DIR"
  printf '  ui       http://%s:%s/admin/\n' "$ip" "$port"
  printf '  api      http://%s:%s/streamer/api/v3/\n' "$ip" "$port"
  printf '  status   systemctl status streamsonic\n\n'
}

config_port() {
  [ -f "$CONF" ] || return
  awk '
    /^[[:space:]]*http[[:space:]]+[0-9]+/ { gsub(/[^0-9]/, ""); print; exit }
    /^[[:space:]]*http[[:space:]]*\{/ { inside = 1; next }
    inside && /^[[:space:]]*port[[:space:]]+[0-9]+/ { gsub(/[^0-9]/, ""); print; exit }
  ' "$CONF"
}

open_firewall() {
  local port
  port="$(config_port)"
  [ -n "$port" ] || port="$PORT"
  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -qi active; then
    ufw allow "${port}/tcp" >/dev/null 2>&1 && log "opened ${port}/tcp in ufw"
    return
  fi
  if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
    firewall-cmd --permanent --add-port="${port}/tcp" >/dev/null 2>&1 &&
      firewall-cmd --reload >/dev/null 2>&1 && log "opened ${port}/tcp in firewalld"
  fi
}

main() {
  need_root
  detect_platform
  resolve_asset
  WORK="$(mktemp -d)"
  trap 'rm -rf "${WORK:-}"' EXIT
  install_binary
  write_config
  write_unit
  start_service
  open_firewall
  summary
}

main "$@"
