#!/usr/bin/env bash
# Neo Uptime agent installer — run on each server that hosts BackPack or Kariz
# tunnels (or any server you want watched). The command with your token is
# shown in Neo Uptime → Servers → Add server.
#
#   bash <(curl -fsSL https://raw.githubusercontent.com/neoauroraproject/Neo-Uptime/main/agent.sh) \
#        --server https://uptime.example.com --token na_xxxxxxxx
set -euo pipefail

REPO="neoauroraproject/Neo-Uptime"
BIN="/usr/local/bin/neo-uptime-agent"
CONF_DIR="/etc/neo-uptime-agent"
ENV_FILE="$CONF_DIR/agent.env"
UNIT="/etc/systemd/system/neo-uptime-agent.service"

SERVER=""
TOKEN=""
INTERVAL=""
INSECURE=""
BP_API=""
BP_TOKEN=""
VERSION=""
MIRROR="${NEO_MIRROR:-}"
UNINSTALL=0

c_green=$'\033[1;32m'; c_lime=$'\033[38;5;154m'; c_red=$'\033[1;31m'; c_dim=$'\033[2m'; c_off=$'\033[0m'
say() { printf '%s\n' "${c_lime}▸${c_off} $*"; }
ok()  { printf '%s\n' "${c_green}✓${c_off} $*"; }
die() { printf '%s\n' "${c_red}✗ $*${c_off}" >&2; exit 1; }

usage() {
  cat <<'EOF'
Neo Uptime agent installer

  --server URL          your Neo Uptime address (required on first install)
  --token TOKEN         the agent token from Neo Uptime → Servers (required on first install)
  --interval N          seconds between reports (default 20)
  --insecure            accept a self-signed Neo Uptime certificate
  --backpack-api URL    read BackPack through its panel API instead of its files
  --backpack-token T    read-scope token for --backpack-api
  --version vX.Y.Z      install a specific release
  --mirror URL          prefix for GitHub downloads when GitHub is slow or filtered
  --uninstall           remove the agent

Run it again without --server/--token for a menu: update, change the address
or token, test, status, uninstall.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --server) SERVER="${2:-}"; shift 2 ;;
    --token) TOKEN="${2:-}"; shift 2 ;;
    --interval) INTERVAL="${2:-}"; shift 2 ;;
    --insecure) INSECURE="true"; shift ;;
    --backpack-api) BP_API="${2:-}"; shift 2 ;;
    --backpack-token) BP_TOKEN="${2:-}"; shift 2 ;;
    --version) VERSION="${2:-}"; shift 2 ;;
    --mirror) MIRROR="${2:-}"; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "run as root (sudo -i, then the command again)"
command -v systemctl >/dev/null 2>&1 || die "systemd is required"

# Run again without --server/--token on a server that has the agent: a menu.
interactive() { [ -t 1 ] && [ -r /dev/tty ]; }
ask() { # prompt default -> answer
  local ans=""
  printf '%s' "$1" >&2
  read -r ans </dev/tty || true
  printf '%s' "${ans:-$2}"
}
# The current ad from Neo Aurora's ad service.
show_ad() {
  [ -x "$BIN" ] && timeout 6 "$BIN" ad 2>/dev/null || true
}
if [ "$UNINSTALL" -eq 0 ] && [ -z "$SERVER$TOKEN" ] && [ -x "$BIN" ] && [ -f "$ENV_FILE" ] && interactive; then
  cur_server="$(grep -E '^NEO_AGENT_SERVER=' "$ENV_FILE" | cut -d= -f2-)"
  cur_version="$("$BIN" version 2>/dev/null | awk '{print $2}' || true)"
  if systemctl is-active --quiet neo-uptime-agent; then state="${c_green}running${c_off}"; else state="${c_red}stopped${c_off}"; fi
  printf '\n  neo-uptime-agent %s · reporting to %s · %s\n' "${cur_version:-?}" "$cur_server" "$state"
  show_ad
  printf '%s\n' "  1) Update the agent" "  2) Change the Neo Uptime address" "  3) Change the token" \
    "  4) Send a report now (test)" "  5) Status and recent logs" "  6) Uninstall the agent" "  0) Exit" ""
  choice="$(ask "  Choose [1]: " 1)"
  echo
  case "$choice" in
    1) ;;
    2) SERVER="$(ask "  New address (e.g. https://uptime.example.com): " "")"; [ -n "$SERVER" ] || exit 0 ;;
    3) TOKEN="$(ask "  New token (Neo Uptime -> Servers -> New token): " "")"; [ -n "$TOKEN" ] || exit 0 ;;
    4) set -a; . "$ENV_FILE"; set +a; "$BIN" test; exit $? ;;
    5) systemctl --no-pager status neo-uptime-agent | head -n 12 || true; echo; journalctl -u neo-uptime-agent -n 30 --no-pager || true; exit 0 ;;
    6) if [ "$(ask "  Remove the agent from this server? [y/N]: " n)" = "y" ]; then UNINSTALL=1; else exit 0; fi ;;
    *) exit 0 ;;
  esac
fi

if [ "$UNINSTALL" -eq 1 ]; then
  systemctl disable --now neo-uptime-agent >/dev/null 2>&1 || true
  rm -f "$UNIT" "$BIN"
  rm -rf "$CONF_DIR"
  systemctl daemon-reload
  ok "Neo Uptime agent removed"
  exit 0
fi

fetch() {
  if command -v curl >/dev/null 2>&1; then curl -fL --retry 3 --connect-timeout 15 -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then wget -q -O "$2" "$1"
  else die "curl or wget is required"; fi
}
fetch_text() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL --connect-timeout 15 "$1"; else wget -qO- "$1"; fi
}

case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l|armv7*) ARCH="armv7" ;;
  *) die "unsupported CPU architecture: $(uname -m)" ;;
esac

if [ -z "$VERSION" ]; then
  VERSION="$(fetch_text "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/' || true)"
  [ -n "$VERSION" ] || VERSION="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null | sed -E 's#.*/tag/##' || true)"
  [ -n "$VERSION" ] || die "could not determine the latest version — pass --version vX.Y.Z"
fi
case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac

# Keep the existing settings on upgrade; only flags given now change them.
if [ -f "$ENV_FILE" ]; then
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  SERVER="${SERVER:-${NEO_AGENT_SERVER:-}}"
  TOKEN="${TOKEN:-${NEO_AGENT_TOKEN:-}}"
  INTERVAL="${INTERVAL:-${NEO_AGENT_INTERVAL:-}}"
  INSECURE="${INSECURE:-${NEO_AGENT_INSECURE:-}}"
  BP_API="${BP_API:-${BACKPACK_API:-}}"
  BP_TOKEN="${BP_TOKEN:-${BACKPACK_TOKEN:-}}"
fi
[ -n "$SERVER" ] || die "--server is required (copy the command from Neo Uptime → Servers)"
[ -n "$TOKEN" ] || die "--token is required (copy the command from Neo Uptime → Servers)"

say "Installing neo-uptime-agent $VERSION ($ARCH)"
ASSET="neo-uptime-agent-linux-$ARCH.tar.gz"
BASE="${MIRROR}https://github.com/$REPO/releases/download/$VERSION"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
fetch "$BASE/$ASSET" "$TMP/$ASSET" || die "download failed: $BASE/$ASSET"
if fetch "$BASE/checksums.txt" "$TMP/checksums.txt" 2>/dev/null && command -v sha256sum >/dev/null 2>&1; then
  (cd "$TMP" && grep " $ASSET\$" checksums.txt | sha256sum -c --status) || die "checksum mismatch"
  ok "Checksum verified"
fi
tar -xzf "$TMP/$ASSET" -C "$TMP"
[ -f "$TMP/neo-uptime-agent" ] || die "archive does not contain neo-uptime-agent"
install -m 755 "$TMP/neo-uptime-agent" "$BIN.new"
mv -f "$BIN.new" "$BIN"

mkdir -p "$CONF_DIR"
chmod 700 "$CONF_DIR"
cat > "$ENV_FILE" <<EOF
# neo-uptime-agent settings — restart after editing: systemctl restart neo-uptime-agent
NEO_AGENT_SERVER=$SERVER
NEO_AGENT_TOKEN=$TOKEN
NEO_AGENT_INTERVAL=${INTERVAL:-20}
NEO_AGENT_INSECURE=${INSECURE:-false}
BACKPACK_API=$BP_API
BACKPACK_TOKEN=$BP_TOKEN
EOF
chmod 600 "$ENV_FILE"

cat > "$UNIT" <<EOF
[Unit]
Description=Neo Uptime agent
Documentation=https://github.com/$REPO
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=$ENV_FILE
ExecStart=$BIN run
Restart=always
RestartSec=5
# Root reads BackPack's files and Kariz's owner-only sockets and sends ICMP.
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable neo-uptime-agent >/dev/null 2>&1

say "Sending a first report to $SERVER"
set -a; . "$ENV_FILE"; set +a
if "$BIN" test; then
  systemctl restart neo-uptime-agent
  echo
  ok "Agent installed and reporting. It shows as online in Neo Uptime → Servers."
else
  systemctl restart neo-uptime-agent
  echo
  printf '%s\n' "${c_red}The first report was not accepted.${c_off} The agent keeps retrying; check the address and token,"
  printf '%s\n' "and that this server can reach $SERVER. Logs: journalctl -u neo-uptime-agent -f"
fi
printf '  %sLogs%s       journalctl -u neo-uptime-agent -f\n' "$c_dim" "$c_off"
printf '  %sSettings%s   %s\n' "$c_dim" "$c_off" "$ENV_FILE"
printf '  %sOne report%s neo-uptime-agent once\n\n' "$c_dim" "$c_off"
show_ad
