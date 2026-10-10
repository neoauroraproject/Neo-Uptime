#!/usr/bin/env bash
# Neo Uptime installer — installs or upgrades in place, keeping your data.
#
#   bash <(curl -fsSL https://raw.githubusercontent.com/neoauroraproject/Neo-Uptime/main/install.sh)
#
# Options:
#   --port N          listen port (default 7788, kept on upgrade)
#   --version vX.Y.Z  install a specific release (default: latest)
#   --mirror URL      prefix for GitHub downloads, for networks where GitHub is slow
#                     or filtered, e.g. --mirror https://ghproxy.example.com/
#   --uninstall       remove the service and binary (data is kept)
#   --purge           with --uninstall, also delete /var/lib/neo-uptime and /etc/neo-uptime
set -euo pipefail

REPO="neoauroraproject/Neo-Uptime"
BIN="/usr/local/bin/neo-uptime"
DATA_DIR="/var/lib/neo-uptime"
CONF_DIR="/etc/neo-uptime"
ENV_FILE="$CONF_DIR/neo-uptime.env"
UNIT="/etc/systemd/system/neo-uptime.service"

PORT=""
VERSION=""
MIRROR="${NEO_MIRROR:-}"
UNINSTALL=0
PURGE=0

c_green=$'\033[1;32m'; c_lime=$'\033[38;5;154m'; c_red=$'\033[1;31m'; c_dim=$'\033[2m'; c_off=$'\033[0m'
say()  { printf '%s\n' "${c_lime}▸${c_off} $*"; }
ok()   { printf '%s\n' "${c_green}✓${c_off} $*"; }
die()  { printf '%s\n' "${c_red}✗ $*${c_off}" >&2; exit 1; }

usage() {
  cat <<'EOF'
Neo Uptime installer — installs or upgrades in place, keeping your data.

  --port N          listen port (default 7788, kept on upgrade)
  --version vX.Y.Z  install a specific release (default: latest)
  --mirror URL      prefix for GitHub downloads when GitHub is slow or filtered
  --uninstall       remove the service and binary (data is kept)
  --purge           with --uninstall, also delete the data and settings
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --port) PORT="${2:-}"; shift 2 ;;
    --version) VERSION="${2:-}"; shift 2 ;;
    --mirror) MIRROR="${2:-}"; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
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

if [ "$UNINSTALL" -eq 1 ]; then
  banner
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
  exit 0
fi

banner

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

# --- platform ---------------------------------------------------------------
case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l|armv7*) ARCH="armv7" ;;
  *) die "unsupported CPU architecture: $(uname -m)" ;;
esac
[ "$(uname -s)" = "Linux" ] || die "only Linux is supported by this installer"

# --- version ----------------------------------------------------------------
if [ -z "$VERSION" ]; then
  say "Finding the latest release"
  VERSION="$(fetch_text "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/' || true)"
  if [ -z "$VERSION" ]; then
    # The API is rate-limited per address; the redirect of /releases/latest is not.
    VERSION="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null | sed -E 's#.*/tag/##' || true)"
  fi
  [ -n "$VERSION" ] || die "could not determine the latest version — pass --version vX.Y.Z"
fi
case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac

CURRENT=""
[ -x "$BIN" ] && CURRENT="$("$BIN" version 2>/dev/null | awk '{print $2}' || true)"
if [ -n "$CURRENT" ]; then
  say "Upgrading ${c_dim}$CURRENT${c_off} → $VERSION ($ARCH)"
else
  say "Installing Neo Uptime $VERSION ($ARCH)"
fi

# --- download & verify ------------------------------------------------------
ASSET="neo-uptime-linux-$ARCH.tar.gz"
BASE="${MIRROR}https://github.com/$REPO/releases/download/$VERSION"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fetch "$BASE/$ASSET" "$TMP/$ASSET" || die "download failed: $BASE/$ASSET"
if fetch "$BASE/checksums.txt" "$TMP/checksums.txt" 2>/dev/null; then
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$TMP" && grep " $ASSET\$" checksums.txt | sha256sum -c --status) || die "checksum mismatch — the download is corrupt or was tampered with"
    ok "Checksum verified"
  fi
fi
tar -xzf "$TMP/$ASSET" -C "$TMP"
[ -f "$TMP/neo-uptime" ] || die "archive does not contain the neo-uptime binary"

# --- install ----------------------------------------------------------------
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
# Serve HTTPS directly (renewed certificates are picked up without a restart):
#NEO_UPTIME_TLS_CERT=/etc/letsencrypt/live/uptime.example.com/fullchain.pem
#NEO_UPTIME_TLS_KEY=/etc/letsencrypt/live/uptime.example.com/privkey.pem
EOF
  chmod 640 "$ENV_FILE"
elif [ -n "$PORT" ]; then
  sed -i -E "s/^NEO_UPTIME_PORT=.*/NEO_UPTIME_PORT=$PORT/" "$ENV_FILE"
fi
PORT="$(grep -E '^NEO_UPTIME_PORT=' "$ENV_FILE" | cut -d= -f2)"
PORT="${PORT:-7788}"

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
# Root is kept for ICMP ping and the Docker socket; the rest is locked down.
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

for _ in 1 2 3 4 5 6 7 8 9 10; do
  if systemctl is-active --quiet neo-uptime; then break; fi
  sleep 1
done
systemctl is-active --quiet neo-uptime || die "the service did not start — see: journalctl -u neo-uptime -n 50"

IP="$(fetch_text https://api.ipify.org 2>/dev/null || true)"
[ -n "$IP" ] || IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
[ -n "$IP" ] || IP="your-server-ip"

echo
ok "Neo Uptime $VERSION is running"
echo
printf '  %sOpen%s        http://%s:%s\n' "$c_lime" "$c_off" "$IP" "$PORT"
printf '  %sData%s        %s\n' "$c_dim" "$c_off" "$DATA_DIR"
printf '  %sSettings%s    %s\n' "$c_dim" "$c_off" "$ENV_FILE"
printf '  %sLogs%s        journalctl -u neo-uptime -f\n' "$c_dim" "$c_off"
printf '  %sPassword%s    neo-uptime reset-password <user>\n' "$c_dim" "$c_off"
echo
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
  printf '  %sFirewall%s    ufw is active — allow the port with: ufw allow %s/tcp\n\n' "$c_red" "$c_off" "$PORT"
fi
if [ -z "$CURRENT" ]; then
  printf '  The first visit asks you to create the admin account.\n\n'
fi
