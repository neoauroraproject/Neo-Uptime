# Neo Uptime — technical guide

The [README](../README.md) covers everyday use. This page is for scripting, reverse proxies, the APIs and development.

- [Install options](#install-options)
- [Agent](#agent)
- [V2Ray / Xray configs](#v2ray--xray-configs)
- [BackPack panel API](#backpack-panel-api)
- [Push monitors](#push-monitors-uptime-kuma-compatible)
- [Own domains and HTTPS](#own-domains-and-https)
- [Status API and widget](#status-api-and-widget)
- [Announcements feed](#announcements-feed)
- [Configuration](#configuration)
- [Command line](#command-line)
- [Admin API](#admin-api)
- [Development](#development)

## Install options

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/neoauroraproject/Neo-Uptime/main/install.sh) [options]
```

| Option | |
|---|---|
| `--port 8080` | port for a new install (default `7788`) |
| `--update` | update to the latest release, keeping all data |
| `--set-port 8080` | change the port of an installed Neo Uptime |
| `--restart` / `--status` | restart, or show status and recent logs |
| `--version v1.3.0` | install or update to a specific release |
| `--mirror https://your-gh-proxy/` | download through a GitHub mirror when GitHub is slow or filtered |
| `--uninstall` / `--uninstall --purge` | remove the service (and, with `--purge`, the data) |

Supported CPUs: `amd64`, `arm64`, `armv7`. The binary is static: no Node, no database server, no Docker needed.

Docker instead:

```bash
docker run -d --name neo-uptime --restart=always -p 7788:7788 -p 80:80 -p 443:443 \
  -v neo-uptime:/data --cap-add NET_RAW ghcr.io/neoauroraproject/neo-uptime:latest
```

Ports 80 and 443 are only needed for automatic HTTPS.

## Agent

`neo-uptime-agent` runs on the servers that host tunnels or should test configs. It only sends: it opens no port and nothing connects to it. Every 20 seconds it reports:

- **The server:** CPU, load, memory, swap, disk, network rate and TCP connections.
- **BackPack:** each tunnel in `/etc/backpack/*.toml`, its live `*.metrics.json` and its systemd unit. No panel token is needed.
- **Kariz:** each tunnel in `/etc/kariz/*.toml`, its control socket and its `kariz@<name>` unit.
- **Latency:** an ICMP ping to each tunnel's peer.
- **V2Ray configs** it was asked to test (see below), and subscriptions it was asked to fetch.

| Agent option | |
|---|---|
| `--server URL[,URL…]` | Neo Uptime addresses, tried in turn; the one that answers is remembered |
| `--token na_…` | the agent's token (Servers → New token) |
| `--interval 30` | seconds between reports (default 20) |
| `--insecure` | accept a self-signed Neo Uptime certificate |
| `--backpack-api URL --backpack-token T` | read BackPack through its panel API instead of its files |
| `--mirror https://your-gh-proxy/` | download through a GitHub mirror |
| `--uninstall` | remove the agent |

How reports reach Neo Uptime (chosen above the install command):

- **Server IP** (default): `http://IP:7788` of the Neo Uptime server, with the domain as a fallback.
- **Domain**: the public address — fine when it is not behind a CDN.
- **Through a tunnel**: forward a port of that server's BackPack or Kariz tunnel to Neo Uptime's port; the agent reports to `http://127.0.0.1:PORT` and falls back to the IP. With BackPack in reverse mode and Neo Uptime on the foreign end, add `7788` to the tunnel's ports on the Iran side.

**Updates from the panel.** *Update agent* on a server's card (or *Update agents* for all) moves an agent to the panel's own release. The panel downloads that release's agent from GitHub once (`NEO_UPTIME_RELEASES_URL` points it at a mirror), checks it against the release's `checksums.txt` and hands it out at `/api/agent/binary` to agents with a valid token — so servers that cannot reach GitHub update too; GitHub is the agent's fallback. The agent checks the SHA-256, runs the new binary once (`version`), swaps it in and exits for systemd to start it. Agents before v1.3.1 cannot do this and need the install command once.

On the server: `neo-uptime-agent test` sends one report and says whether it was accepted; `neo-uptime-agent once` prints a report as JSON. Run the agent one-liner again without `--server` and `--token` for a menu.

## V2Ray / Xray configs

Monitors of type **V2Ray config** connect through a real [Xray-core](https://github.com/XTLS/Xray-core) built into Neo Uptime and the agent — nothing to install. Each test starts an Xray instance with the config as its only outbound and fetches the test address through it (default `https://www.gstatic.com/generate_204`):

- the first request is the **connect time** (handshake with the config's server and the site — what clients call real delay);
- a second request on the open connection is the **latency** shown in charts and used by slow alerts.

Supported links: `vless://` (TCP, WS, gRPC, HTTPUpgrade, XHTTP, mKCP; TLS and REALITY, Vision flow), `vmess://`, `trojan://`, `ss://` (including Shadowsocks 2022), `hysteria2://` / `hy2://` (with salamander obfuscation), and full Xray JSON configs. Not supported: TUIC, WireGuard, SSR, Shadowsocks plugins, and the HTTP/2 and QUIC transports that Xray removed. Links with `allowInsecure=1` keep working.

**Subscriptions** are read in the usual formats (base64 or plain link lists, Xray JSON). The `subscription-userinfo` header gives the traffic used and the expiry shown on the Configs page. Each config becomes a monitor in a group named after the subscription; the subscription is read again every hour (configurable): changed links are updated, new configs are added when *Add new configs automatically* is on, and configs that left the subscription are paused.

**Testing from another server:** choose the server under *Test from*. The panel sends that server's agent the configs to test (with the ack of its report); the agent tests each on schedule and sends the results back right away. If the panel cannot download a subscription itself, that server downloads it. The agent must be v1.3.0 or newer.

## BackPack panel API

1. In the BackPack web panel go to **Settings → Security → API tokens**, create a token with scope **read**.
2. In Neo Uptime go to **Tunnels → Panels → Connect panel**, paste the panel address exactly as you open it (port and secret path included) and the token, and click **Test connection**.

Neo Uptime only reads BackPack's read-only API and never changes a tunnel.

## Push monitors (Uptime Kuma compatible)

```bash
curl -fsS "https://uptime.example.com/api/push/<token>?status=up&msg=OK&ping="
```

Scripts written for Kuma keep working after you change the host. If the URL is not called within the interval, the monitor goes down.

## Own domains and HTTPS

1. Add an **A record** for the name (for example `status.example.com`) pointing to the server's IP. With Cloudflare, use *DNS only*.
2. Enter it under **Own domain** in the status page editor and save.
3. Click **Get SSL now**. Neo Uptime listens on ports 80 and 443, gets a Let's Encrypt certificate and renews it.

Visitors to the status domain see only that page. If the panel itself listens on 443 (or 80), automatic HTTPS shares that port. If another web server already uses 80 and 443, leave automatic HTTPS off and proxy to Neo Uptime instead (see below). `NEO_UPTIME_HTTP_PORT` and `NEO_UPTIME_HTTPS_PORT` change the ports.

## Status API and widget

The owner account can create private keys under **Settings → Status API**. A key reads one status page and can be limited to your site's address. Details hidden on the page are left out here too.

```html
<div id="neo-status"></div>
<script src="https://uptime.example.com/embed.js" data-target="#neo-status"
  data-server="https://uptime.example.com" data-slug="main" data-key="sk_…"
  data-lang="fa" data-theme="auto" async></script>
```

```bash
curl -H "Authorization: Bearer sk_…" https://uptime.example.com/api/v1/status/main
```

`overall` is `operational`, `partial_outage`, `major_outage`, `maintenance` or `unknown`; a monitor's `status` is `up`, `down`, `pending`, `maintenance`, `paused` or `unknown`.

## Announcements feed

The panel shows a short text announcement on the dashboard and in the side menu, and the install scripts print one. They come from [`ads.json`](../ads.json) in this repository, read every few hours from GitHub (with a jsDelivr copy as a fallback). Nothing is sent along: it is a plain download of a public file. `NEO_UPTIME_ADS_URL` points it at another address (comma-separated list). How to write the file: [docs/ads.md](ads.md).

## Configuration

`/etc/neo-uptime/neo-uptime.env` (then `systemctl restart neo-uptime`):

```ini
NEO_UPTIME_HOST=0.0.0.0
NEO_UPTIME_PORT=7788
NEO_UPTIME_DATA=/var/lib/neo-uptime
#NEO_UPTIME_HTTP_PORT=80
#NEO_UPTIME_HTTPS_PORT=443
#NEO_UPTIME_TLS_CERT=/etc/letsencrypt/live/uptime.example.com/fullchain.pem
#NEO_UPTIME_TLS_KEY=/etc/letsencrypt/live/uptime.example.com/privkey.pem
```

Set **Settings → General → Public address** so that alert links, push URLs and agent commands use your real address. Behind nginx, turn on **Behind a reverse proxy** and keep the event stream unbuffered:

```nginx
location / {
    proxy_pass http://127.0.0.1:7788;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
}
```

## Command line

```bash
neo-uptime reset-password <user> [new-password]   # prints a new password if none is given
neo-uptime disable-2fa <user>                     # lost phone
neo-uptime set-owner <user>                       # hand the owner role to another admin
neo-uptime list-users
neo-uptime version
journalctl -u neo-uptime -f                       # logs
journalctl -u neo-uptime-agent -f                 # agent logs, on a monitored server
```

## Admin API

Create a key under **Settings → API keys**. Read keys can call `GET /api/*` and `/metrics` (Prometheus, with Kuma's metric names). Write keys can also create and change monitors, status pages and maintenance.

```bash
curl -H "Authorization: Bearer nu_…" https://uptime.example.com/api/monitors
```

## Development

```bash
make dev     # mock BackPack panel on :7777 (token: test-token), API on :7788, UI with hot reload on :5173
make test    # go vet + go test
make linux   # release archives in dist/ (server and agent)
```

The backend is Go with SQLite through a pure-Go driver (no CGO) and Xray-core built in. The UI is React, TypeScript, Vite and Tailwind CSS, embedded in the binary.
