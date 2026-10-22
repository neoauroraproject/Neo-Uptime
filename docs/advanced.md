# Neo Uptime — technical guide

The [README](../README.md) covers everyday use. This page is for scripting, reverse proxies, the APIs and development.

- [Install options](#install-options)
- [Agent](#agent)
- [V2Ray / Xray configs](#v2ray--xray-configs)
- [BackPack panel API](#backpack-panel-api)
- [Push monitors](#push-monitors-uptime-kuma-compatible)
- [Own domains and HTTPS](#own-domains-and-https)
- [Status API and widget](#status-api-and-widget)
- [Ads](#ads)
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
- **NeoTunnel:** each tunnel in `/etc/neotunnel/state.json`, on the main server and on destination servers. Its daemon pings every tunnel's far inner address (`health.json`), so a tunnel is online only when packets really pass, and the ping is the round trip through the tunnel. Traffic comes from `traffic.json`, the monthly quota counts this month, and a tunnel that is turned off or over its quota shows as stopped. The layers under a stack (the SIT under a 6to4) are not tunnels of their own. Nothing else is needed: no panel password, any NeoTunnel release. Tunnels pushed by every main server are read (`managed` and `more_masters`). From NeoTunnel 0.8.3 on, display names come too: a tunnel's monitor is named after its display name and follows it when it is renamed in NeoTunnel, until you name the monitor yourself; the card shows the far server by its display name (`peers[].label`), with the NeoTunnel ID kept as the tunnel's key.
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
- **Through a tunnel**: forward a port of that server's BackPack, Kariz or NeoTunnel tunnel to Neo Uptime's port; the agent reports to `http://127.0.0.1:PORT` and falls back to the IP. With BackPack in reverse mode and Neo Uptime on the foreign end, add `7788` to the tunnel's ports on the Iran side.

**Updates from the panel.** *Update agent* on a server's card (or *Update agents* for all) moves an agent to the panel's own release. The panel downloads that release's agent from GitHub once (`NEO_UPTIME_RELEASES_URL` points it at a mirror), checks it against the release's `checksums.txt` and hands it out at `/api/agent/binary` to agents with a valid token — so servers that cannot reach GitHub update too; GitHub is the agent's fallback. The agent checks the SHA-256, runs the new binary once (`version`), swaps it in and exits for systemd to start it. Agents before v1.3.1 cannot do this and need the install command once.

On the server: `neo-uptime-agent test` sends one report and says whether it was accepted; `neo-uptime-agent once` prints a report as JSON. Run the agent one-liner again without `--server` and `--token` for a menu.

## V2Ray / Xray configs

Monitors of type **V2Ray config** connect through a real [Xray-core](https://github.com/XTLS/Xray-core) built into Neo Uptime and the agent — nothing to install. Each test starts an Xray instance with the config as its only outbound and fetches the test address through it (default `https://www.gstatic.com/generate_204`):

- the first request is the **connect time** (handshake with the config's server and the site — what clients call real delay);
- a second request on the open connection is the **latency** shown in charts and used by slow alerts.

Supported links: `vless://` (TCP, WS, gRPC, HTTPUpgrade, XHTTP, mKCP; TLS and REALITY, Vision flow), `vmess://`, `trojan://`, `ss://` (including Shadowsocks 2022), `hysteria2://` / `hy2://` (with salamander obfuscation), and full Xray JSON configs. Not supported: TUIC, WireGuard, SSR, Shadowsocks plugins, and the HTTP/2 and QUIC transports that Xray removed. Links with `allowInsecure=1` keep working.

**Speed test.** *Speed test* on a config's card (or its page) measures real throughput through the config, by hand, with an app-style dial that moves live: it finds a test server, then eight small requests for ping and jitter, then about 8 s of download (four parallel streams, the first 1.5 s of ramp-up not counted) and 6 s of upload. The page starts it with `POST /api/xray/speed` (answers at once with an id) and follows it with `GET /api/xray/speed/<id>` every 350 ms: phase, the speed right now and the samples so far. Run on a server, the agent sends the same progress back in early reports, about every 1.5 s.

*Test servers.* By default **Speedtest by Ookla**: the panel (or the agent, or the config's exit when run through a config) asks `www.speedtest.net` for the servers near its address, probes the nearest four and uses the one that answers fastest, over Speedtest.net's plain HTTP test protocol (`latency.txt`, `random4000x4000.jpg`, `upload.php`) — implemented in Go, no extra program. If Speedtest.net cannot be reached, or **Cloudflare** is chosen in the dialog, `speed.cloudflare.com` is used, with `proof.ovh.net` as a download-only fallback. Cloudflare's server needs browser-like request headers, which are sent. A test ends at 400 MB down and 150 MB up, however fast the line is, and counts against the account's traffic limit. `NEO_UPTIME_SPEED_URL` (on the panel, or in the agent's env file) puts a server of your own first: it must answer like Cloudflare's, `GET <url>/__down?bytes=N` and `POST <url>/__up`. `NEO_UPTIME_OOKLA_LIST` points the Speedtest.net server list elsewhere (`off` turns Speedtest.net off).

*A server's own line.* *Speed test* on a server's card (Servers page) runs the same test without a config, from that server's agent (v1.4.0 or newer), and keeps the last result with the server. It measures what that machine's connection can do.

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

## Ads

The panel shows a text ad at the top of its pages, and the install scripts print one. They come from Neo Aurora's ad service — `GET https://license.hossein-m466.workers.dev/v1/ads?product=neo-uptime` — read every half hour; the last answer is kept for when the service is out of reach. Only the product name is sent. `NEO_UPTIME_ADS_URL` points it elsewhere (comma-separated list). See [docs/ads.md](ads.md).

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

---
News and help: [t.me/neoaurora](https://t.me/neoaurora) · Support the project: USDT (BEP20) `0xacA935a5955a756BedaE4738304274EdeE0223D5`


## Internet radar on status pages

Settings → General → *Internet radar* turns on reading [Chabokan's radar](https://radar.chabokan.net/): from datacenters in Iran (Asiatech, Hiweb, Sefroyek, Irancell, Amin, Shatel) and abroad (Hetzner, OVH), it checks about 30 well-known services every minute. The panel reads it every 5 minutes (about 100 KB, gzipped) and keeps only a summary per datacenter in memory: its state, how many services answer, the latency, and the last hour in twelve 5-minute steps. Nothing is written to the database, and with the switch off nothing is read or kept.

From a datacenter in Iran, the state is about foreign services (does it have the international internet?); from abroad, about Iranian ones. A service is down only after two failed checks in a row, and a service no datacenter reached all hour (WhatsApp, which is filtered) does not count. 80 % of the services answering is *normal*, 30 % *disrupted*, less *cut off*.

A status page shows it under Monitors → *Internet radar*: a small strip in the page's theme that says in one line whether there is a disruption, and opens to each datacenter. It comes with the page's own data, so visitors never wait on the radar or talk to it. The data is Chabokan's; the strip links to it. `NEO_UPTIME_RADAR_URL` points the reader elsewhere.
