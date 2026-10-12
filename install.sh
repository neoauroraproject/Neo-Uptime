#!/usr/bin/env bash
# Neo Uptime installer and manager.
#
#   bash <(curl -fsSL https://raw.githubusercontent.com/neoauroraproject/Neo-Uptime/main/install.sh)
#
# On a new server it installs Neo Uptime. Where it is already installed it shows
# a menu: update, change the port, restart, status and logs, reset a password,
# uninstall. Every action also has a flag, for scripts:
#
#   --port N          port for a new install (default 7788)
#   --update          update to the latest release (or --version vX.Y.Z)
#   --set-port N      change the port of an installed Neo Uptime
#   --restart         restart the service
#   --status          show the service status and recent logs
#   --uninstall       remove the service and binary (data is kept)
#   --purge           with --uninstall, also delete /var/lib/neo-uptime and /etc/neo-uptime
#   --version vX.Y.Z  install or update to a specific release
#   --mirror URL      prefix for GitHub downloads, for networks where GitHub is slow
#                     or filtered, e.g. --mirror https://ghproxy.example.com/
set -euo pipefail

REPO="neoauroraproject/Neo-Uptime"
BIN="/usr/local/bin/neo-uptime"
DATA_DIR="/var/lib/neo-uptime"
CONF_DIR="/etc/neo-uptime"
ENV_FILE="$CONF_DIR/neo-uptime.env"
UNIT="/etc/systemd/system/neo-uptime.service"
CHANNEL="https://t.me/neoaurora"

ACTION=""
PORT=""
NEW_PORT=""
VERSION=""
MIRROR="${NEO_MIRROR:-}"
PURGE=0

c_green=$'\033[1;32m'; c_lime=$'\033[38;5;154m'; c_red=$'\033[1;31m'; c_yellow=$'\033[1;33m'; c_dim=$'\033[2m'; c_bold=$'\033[1m'; c_off=$'\033[0m'
say()  { printf '%s\n' "${c_lime}▸${c_off} $*"; }
ok()   { printf '%s\n' "${c_green}✓${c_off} $*"; }
warn() { printf '%s\n' "${c_yellow}!${c_off} $*"; }
die()  { printf '%s\n' "${c_red}✗ $*${c_off}" >&2; exit 1; }

usage() {
  cat <<'EOF'
Neo Uptime installer and manager

  (no option)       install, or show the menu if Neo Uptime is already installed
  --port N          port for a new install (default 7788)
  --update          update to the latest release
  --set-port N      change the port
  --restart         restart the service
  --status          show status and recent logs
  --uninstall       remove the service and binary (data is kept)
  --purge           with --uninstall, also delete the data and settings
  --version vX.Y.Z  install or update to a specific release
  --mirror URL      prefix for GitHub downloads when GitHub is slow or filtered
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --port) PORT="${2:-}"; shift 2 ;;
    --set-port) ACTION="port"; NEW_PORT="${2:-}"; shift 2 ;;
    --update|--upgrade) ACTION="update"; shift ;;
    --restart) ACTION="restart"; shift ;;
    --status) ACTION="status"; shift ;;
    --version) VERSION="${2:-}"; shift 2 ;;
    --mirror) MIRROR="${2:-}"; shift 2 ;;
    --uninstall) ACTION="uninstall"; shift ;;
    --purge) PURGE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "run as root (sudo -i, then the command again)"
command -v systemctl >/dev/null 2>&1 || die "systemd is required"

banner() {
  printf '%s' "$c_lime"
  cat <<'EOF'
   _  __            __  __     __  _
  / |/ /__ ___     / / / /__  / /_(_)_ _  ___
 /    / -_) _ \   / /_/ / _ \/ __/ /  ' \/ -_)
/_/|_/\__/\___/   \____/ .__/\__/_/_/_/_/\__/
                      /_/
EOF
  printf '%s\n' "$c_off"
}

# Questions are read from the terminal, so they work under `bash <(curl …)`.
interactive() { [ -t 1 ] && [ -r /dev/tty ]; }
ask() { # prompt default → answer
  local ans=""
  printf '%s' "$1" >&2
  read -r ans </dev/tty || true
  printf '%s' "${ans:-$2}"
}

installed() { [ -x "$BIN" ] && [ -f "$UNIT" ]; }
# The current ad from Neo Aurora's ad service; quiet and quick when there
# is none or the service cannot be reached.
show_ad() {
  [ -x "$BIN" ] && timeout 6 "$BIN" ad 2>/dev/null || true
}
current_version() { "$BIN" version 2>/dev/null | awk '{print $2}' || true; }
current_port() {
  local p=""
  [ -f "$ENV_FILE" ] && p="$(grep -E '^NEO_UPTIME_PORT=' "$ENV_FILE" | tail -n1 | cut -d= -f2)"
  printf '%s' "${p:-7788}"
}
valid_port() { [[ "$1" =~ ^[0-9]+$ ]] && [ "$1" -ge 1 ] && [ "$1" -le 65535 ]; }
port_busy() { command -v ss >/dev/null 2>&1 && ss -ltnH "sport = :$1" 2>/dev/null | grep -q .; }

public_ip() {
  local ip=""
  ip="$(fetch_text https://api.ipify.org 2>/dev/null || true)"
  [ -n "$ip" ] || ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  printf '%s' "${ip:-your-server-ip}"
}

firewall_hint() {
  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
    printf '  %sFirewall%s    ufw is active — allow the port with: ufw allow %s/tcp\n\n' "$c_yellow" "$c_off" "$1"
  fi
}

wait_active() {
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if systemctl is-active --quiet neo-uptime; then return 0; fi
    sleep 1
  done
  systemctl is-active --quiet neo-uptime || die "the service did not start — see: journalctl -u neo-uptime -n 50"
}

# --- downloader -------------------------------------------------------------
fetch() { # url dest
  if command -v curl >/dev/null 2>&1; then
    curl -fL --retry 3 --connect-timeout 15 -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$2" "$1"
  else
    die "curl or wget is required"
  fi
}
fetch_text() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL --connect-timeout 15 "$1"; else wget -qO- "$1"; fi
}

latest_version() {
  local v=""
  v="$(fetch_text "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/' || true)"
  if [ -z "$v" ] && command -v curl >/dev/null 2>&1; then
    # The API is rate-limited per address; the redirect of /releases/latest is not.
    v="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null | sed -E 's#.*/tag/##' || true)"
  fi
  printf '%s' "$v"
}

# --- actions ------------------------------------------------------------------

do_install() {
  case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    armv7l|armv7*) ARCH="armv7" ;;
    *) die "unsupported CPU architecture: $(uname -m)" ;;
  esac
  [ "$(uname -s)" = "Linux" ] || die "only Linux is supported by this installer"

  if [ -z "$VERSION" ]; then
    say "Finding the latest release"
    VERSION="$(latest_version)"
    [ -n "$VERSION" ] || die "could not determine the latest version — pass --version vX.Y.Z"
  fi
  case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac

  local current=""
  installed && current="$(current_version)"
  if [ -n "$current" ]; then
    if [ "$current" = "$VERSION" ] && [ "$ACTION" = "update" ]; then
      ok "Neo Uptime $current is already the latest version"
      return 0
    fi
    say "Updating ${c_dim}$current${c_off} → $VERSION ($ARCH)"
  else
    say "Installing Neo Uptime $VERSION ($ARCH)"
    if [ -z "$PORT" ] && interactive; then
      while :; do
        PORT="$(ask "  Port for the web panel [7788]: " 7788)"
        valid_port "$PORT" || { warn "Use a number from 1 to 65535."; continue; }
        if port_busy "$PORT"; then warn "Port $PORT is already in use — choose another."; continue; fi
        break
      done
    fi
  fi

  local asset="neo-uptime-linux-$ARCH.tar.gz"
  local base="${MIRROR}https://github.com/$REPO/releases/download/$VERSION"
  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT

  fetch "$base/$asset" "$TMP/$asset" || die "download failed: $base/$asset"
  if fetch "$base/checksums.txt" "$TMP/checksums.txt" 2>/dev/null; then
    if command -v sha256sum >/dev/null 2>&1; then
      (cd "$TMP" && grep " $asset\$" checksums.txt | sha256sum -c --status) || die "checksum mismatch — the download is corrupt or was tampered with"
      ok "Checksum verified"
    fi
  fi
  tar -xzf "$TMP/$asset" -C "$TMP"
  [ -f "$TMP/neo-uptime" ] || die "archive does not contain the neo-uptime binary"

  mkdir -p "$DATA_DIR" "$CONF_DIR"
  chmod 750 "$DATA_DIR" "$CONF_DIR"
  install -m 755 "$TMP/neo-uptime" "$BIN.new"
  mv -f "$BIN.new" "$BIN"

  if [ ! -f "$ENV_FILE" ]; then
    cat > "$ENV_FILE" <<EOF
# Neo Uptime settings — restart after editing: systemctl restart neo-uptime
NEO_UPTIME_HOST=0.0.0.0
NEO_UPTIME_PORT=${PORT:-7788}
NEO_UPTIME_DATA=$DATA_DIR
# Ports used when automatic HTTPS is on (Settings → Domains & HTTPS):
#NEO_UPTIME_HTTP_PORT=80
#NEO_UPTIME_HTTPS_PORT=443
# Or serve the panel with your own certificate (renewals are picked up):
#NEO_UPTIME_TLS_CERT=/etc/letsencrypt/live/uptime.example.com/fullchain.pem
#NEO_UPTIME_TLS_KEY=/etc/letsencrypt/live/uptime.example.com/privkey.pem
EOF
    chmod 640 "$ENV_FILE"
  elif [ -n "$PORT" ]; then
    sed -i -E "s/^NEO_UPTIME_PORT=.*/NEO_UPTIME_PORT=$PORT/" "$ENV_FILE"
  fi
  PORT="$(current_port)"

  cat > "$UNIT" <<EOF
[Unit]
Description=Neo Uptime — uptime monitoring
Documentation=https://github.com/$REPO
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=$ENV_FILE
ExecStart=$BIN serve
Restart=always
RestartSec=3
LimitNOFILE=65535
# Root is kept for ICMP ping, the Docker socket and ports 80/443 (automatic
# HTTPS); the rest is locked down.
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=read-only
ReadWritePaths=$DATA_DIR

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable neo-uptime >/dev/null 2>&1
  systemctl restart neo-uptime
  wait_active

  echo
  ok "Neo Uptime $VERSION is running"
  echo
  printf '  %sOpen%s        http://%s:%s\n' "$c_lime" "$c_off" "$(public_ip)" "$PORT"
  printf '  %sManage%s      run this command again for the menu (update, port, uninstall)\n' "$c_dim" "$c_off"
  printf '  %sLogs%s        journalctl -u neo-uptime -f\n' "$c_dim" "$c_off"
  printf '  %sNews%s        %s\n' "$c_dim" "$c_off" "$CHANNEL"
  echo
  firewall_hint "$PORT"
  if [ -z "$current" ]; then
    printf '  The first visit asks you to create the owner account.\n\n'
  fi
  show_ad
}

do_port() {
  installed || die "Neo Uptime is not installed"
  local old new
  old="$(current_port)"
  new="$NEW_PORT"
  if [ -z "$new" ]; then
    interactive || die "pass the new port: --set-port N"
    new="$(ask "  New port (now $old): " "")"
  fi
  valid_port "$new" || die "use a port from 1 to 65535"
  [ "$new" = "$old" ] && { ok "Neo Uptime already listens on port $old"; return 0; }
  port_busy "$new" && die "port $new is already in use"
  if grep -qE '^NEO_UPTIME_PORT=' "$ENV_FILE"; then
    sed -i -E "s/^NEO_UPTIME_PORT=.*/NEO_UPTIME_PORT=$new/" "$ENV_FILE"
  else
    printf 'NEO_UPTIME_PORT=%s\n' "$new" >> "$ENV_FILE"
  fi
  systemctl restart neo-uptime
  wait_active
  ok "Neo Uptime now listens on port $new"
  printf '  %sOpen%s        http://%s:%s\n\n' "$c_lime" "$c_off" "$(public_ip)" "$new"
  firewall_hint "$new"
  warn "Agents keep reporting to the old address until you update it: re-run the agent installer with --server http://ADDRESS:$new"
}

do_restart() {
  installed || die "Neo Uptime is not installed"
  systemctl restart neo-uptime
  wait_active
  ok "Neo Uptime restarted"
}

do_status() {
  installed || die "Neo Uptime is not installed"
  systemctl --no-pager status neo-uptime | head -n 12 || true
  echo
  journalctl -u neo-uptime -n 30 --no-pager 2>/dev/null || true
}

do_password() {
  installed || die "Neo Uptime is not installed"
  "$BIN" list-users || true
  local user
  user="$(ask "  User to reset: " "")"
  [ -n "$user" ] || return 0
  "$BIN" reset-password "$user"
  ok "$user was signed out everywhere; sign in with the new password"
}

do_uninstall() {
  say "Stopping and removing the service"
  systemctl disable --now neo-uptime >/dev/null 2>&1 || true
  rm -f "$UNIT" "$BIN"
  systemctl daemon-reload
  if [ "$PURGE" -eq 1 ]; then
    rm -rf "$DATA_DIR" "$CONF_DIR"
    ok "Neo Uptime and all its data were removed"
  else
    ok "Neo Uptime was removed. Your data is still in $DATA_DIR (use --uninstall --purge to delete it)"
  fi
}

menu() {
  local version port state
  version="$(current_version)"
  port="$(current_port)"
  if systemctl is-active --quiet neo-uptime; then state="${c_green}running${c_off}"; else state="${c_red}stopped${c_off}"; fi
  printf '  Neo Uptime %s%s%s · port %s · %s\n' "$c_bold" "${version:-?}" "$c_off" "$port" "$state"
  show_ad
  cat <<EOF
  1) Update to the latest version
  2) Change the port
  3) Restart
  4) Status and recent logs
  5) Reset a user's password
  6) Uninstall (keep the data)
  7) Uninstall and delete all data
  0) Exit

EOF
  local choice
  choice="$(ask "  Choose [1]: " 1)"
  echo
  case "$choice" in
    1) ACTION="update"; do_install ;;
    2) do_port ;;
    3) do_restart ;;
    4) do_status ;;
    5) do_password ;;
    6) if [ "$(ask "  Remove Neo Uptime (data is kept)? [y/N]: " n)" = "y" ]; then do_uninstall; fi ;;
    7) if [ "$(ask "  Delete Neo Uptime AND all monitors, history and settings? Type DELETE: " "")" = "DELETE" ]; then PURGE=1; do_uninstall; else say "Nothing was deleted"; fi ;;
    0|q|Q) ;;
    *) warn "Unknown choice: $choice" ;;
  esac
}

banner
case "$ACTION" in
  update) do_install ;;
  port) do_port ;;
  restart) do_restart ;;
  status) do_status ;;
  uninstall) do_uninstall ;;
  *)
    if installed && interactive; then
      menu
    else
      do_install
    fi
    ;;
esac
