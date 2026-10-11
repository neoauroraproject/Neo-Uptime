<div align="center">

<img src="web/public/icon.svg" width="72" alt="" />

# Neo Uptime

**Self-hosted uptime monitoring with first-class [BackPack](https://github.com/AminMGMT/BackPack) and [Kariz](https://github.com/Erfan-XRay/Kariz) tunnel support.**
Uptime Kuma-style monitors, a full admin panel in English and فارسی, five status-page themes on your own domains with automatic HTTPS, and a private status API — in one small binary.

[Install](#install) · [Servers & agent](#servers-backpack-and-kariz-on-other-machines) · [Status pages](#status-pages) · [Own domain & HTTPS](#a-status-page-on-its-own-domain-with-https) · [Status API](#show-your-status-on-your-own-website) · [فارسی](#فارسی) · [Telegram channel](https://t.me/neoaurora)

<img src="docs/screenshots/tunnels-dark.jpg" alt="BackPack tunnels in Neo Uptime" />

</div>

## Install

On a Linux server (Ubuntu, Debian, CentOS, Alma, …), as root:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/neoauroraproject/Neo-Uptime/main/install.sh)
```

It asks for a port (Enter keeps `7788`). Then open `http://SERVER-IP:7788` and create the owner account.

Run the same command again later for a menu:

```
  1) Update to the latest version
  2) Change the port
  3) Restart
  4) Status and recent logs
  5) Reset a user's password
  6) Uninstall (keep the data)
  7) Uninstall and delete all data
```

Every action also has a flag, for scripts:

| Option | |
|---|---|
| `--port 8080` | port for a new install (default `7788`) |
| `--update` | update to the latest release, keeping all data |
| `--set-port 8080` | change the port of an installed Neo Uptime |
| `--restart` / `--status` | restart, or show status and recent logs |
| `--version v1.2.0` | install or update to a specific release |
| `--mirror https://your-gh-proxy/` | download through a GitHub mirror when GitHub is slow or filtered |
| `--uninstall` / `--uninstall --purge` | remove the service (and, with `--purge`, the data) |

Supported CPUs: `amd64`, `arm64`, `armv7`. The binary is static: no Node, no database server, no Docker needed.

<details>
<summary>Docker instead</summary>

```bash
docker run -d --name neo-uptime --restart=always -p 7788:7788 -p 80:80 -p 443:443 \
  -v neo-uptime:/data --cap-add NET_RAW ghcr.io/neoauroraproject/neo-uptime:latest
```

Ports 80 and 443 are only needed for [automatic HTTPS](#a-status-page-on-its-own-domain-with-https).
</details>

## Features

- **28 monitor types**, grouped like Uptime Kuma's:
  - *General:* HTTP(s), HTTP keyword, HTTP JSON query, TCP port, ping (ICMP), DNS, WebSocket, SSL certificate.
  - *Servers & tunnels:* server (agent), tunnel via agent (BackPack or Kariz), BackPack tunnel and BackPack panel through the panel API.
  - *Passive:* push (heartbeat, Kuma compatible).
  - *Network services:* UDP port, SMTP, MQTT, gRPC health, RADIUS, SNMP.
  - *Databases & queues:* PostgreSQL, MySQL/MariaDB, SQL Server, MongoDB, Redis, Kafka.
  - *Other:* Docker container, Steam game server, group.
- **Kuma-style checks:** per-monitor intervals, retries with their own interval, timeouts, upside-down mode, re-sent alerts while down, and TLS certificate expiry warnings.
- **Slow alerts:** get told when a tunnel's ping (or any response time) stays above a limit for a while — say 300 ms for 2 minutes — and again when it is fast again.
- **Simple to set up:** adding a monitor is a four-step wizard (type, details, alerts, finish) with the common choices first and everything else folded away.
- **Alerts where you are:** Telegram (finds your chat ID for you, and works through a custom API server or proxy when it is filtered), Bale, Discord, Slack/Mattermost, Microsoft Teams, Google Chat, email (SMTP), webhook, ntfy, Gotify and Pushover. Each channel sends in English or Persian.
- **A server agent** that reads BackPack and Kariz on the machine they run on and pushes everything to Neo Uptime with a token — so Neo Uptime can live anywhere.
- **Live dashboard:** a two-pane monitor list with heartbeat bars, groups, tags, search, filters and bulk pause/resume/delete, updated over a server-sent event stream.
- **Monitor pages:** response-time charts (live, 3h, 24h, 7d, 30d), 24h/30d/1-year uptime, important events and full check history.
- **Status pages:** as many as you like, in five themes (including the minimal **Zen**), in English or Persian, with country flags, an uploaded logo, incidents, scheduled maintenance and a live preview of unsaved changes.
- **Own domains with automatic HTTPS:** give a status page (and the panel) its own domain; Neo Uptime gets and renews the Let's Encrypt certificate.
- **Private status API** and an embeddable widget for your own website. Only the owner can create its keys.
- **Country flags** for every server and tunnel. They are bundled SVGs, not emoji, so they look right on Windows too.
- **Persian UI:** right-to-left layout, Persian digits and Solar Hijri dates, switchable per browser.
- **Admin:** an owner account plus admin, editor and viewer roles, two-factor sign-in (TOTP), per-device sessions, scoped and expiring API keys, and JSON backup/restore.
- **Integrations:** a Prometheus `/metrics` endpoint (with Kuma's metric names) and SVG badges for status, uptime, response time and certificate expiry.
- **Light and dark themes.** Fonts and flags are bundled, so nothing loads from Google or any CDN.

<table>
<tr><td><img src="docs/screenshots/dashboard-dark.jpg" alt="Dashboard" /></td><td><img src="docs/screenshots/monitor-dark.jpg" alt="Monitor detail" /></td></tr>
<tr><td><img src="docs/screenshots/dashboard-fa.jpg" alt="Dashboard in Persian" /></td><td><img src="docs/screenshots/wizard-alerts.jpg" alt="Add-monitor wizard with a slow alert" /></td></tr>
</table>

## Servers: BackPack and Kariz on other machines

Neo Uptime does not have to run next to your tunnels. Install the small agent on each server instead:

1. In Neo Uptime go to **Servers → Add a server**, give it a name and a flag, and click **Create & show install command**.
2. Run the command it shows on that server, as root. It looks like this:

   ```bash
   bash <(curl -fsSL https://raw.githubusercontent.com/neoauroraproject/Neo-Uptime/main/agent.sh) \
     --server https://uptime.example.com --token na_xxxxxxxx
   ```

3. Within seconds the server shows up as online. Its BackPack and Kariz tunnels are added as monitors in a group named after the server.

The agent only sends: it opens no port and nothing connects to it. Every 20 seconds it reports:

- **The server:** CPU, load, memory, swap, disk, network rate and TCP connections.
- **BackPack:** each tunnel in `/etc/backpack/*.toml`, its live `*.metrics.json` (connection, traffic, peer, forwarded service) and its systemd unit. No panel token is needed.
- **Kariz:** each tunnel in `/etc/kariz/*.toml`, its control socket (`status` → peer, sessions, RTT, traffic, forwards) and its `kariz@<name>` unit.
- **Latency:** an ICMP ping to each tunnel's peer.

| Agent option | |
|---|---|
| `--interval 30` | seconds between reports (default 20) |
| `--insecure` | accept a self-signed Neo Uptime certificate |
| `--backpack-api URL --backpack-token T` | read BackPack through its panel API instead of its files |
| `--mirror https://your-gh-proxy/` | download through a GitHub mirror |
| `--uninstall` | remove the agent |

`neo-uptime-agent test` (run it on the server) prints what the agent found and whether Neo Uptime accepted it. Run the agent one-liner again without `--server` and `--token` for a menu: update, change the address or token, send a test report, status, uninstall.

## Monitor BackPack tunnels through the panel API

If Neo Uptime can reach a BackPack panel, you can also connect the panel directly. Neo Uptime reads the same read-only API that BackPack documents for Prometheus, so it never touches a tunnel's config.

1. In the **BackPack web panel** go to **Settings → Security → API tokens**, create a token with scope **read** and name it `neo-uptime`.
2. In Neo Uptime go to **Tunnels → Panels → Connect panel**. Paste the panel address exactly as you open it, including the port and any secret path (for example `https://1.2.3.4:7777`), then paste the token and click **Test connection**.
3. Save. Every tunnel becomes a monitor inside a group named after the panel, together with a panel-health monitor (host CPU, RAM and disk). With **auto-import** on, tunnels you add in BackPack later show up on their own.

Each tunnel monitor records its online state, real latency, traffic in and out, uptime, transport, preset, port, peer location, KCP loss and connection pool. A tunnel that is up while the service behind it refuses connections can be counted as down: turn this on per monitor.

## Telegram alerts

1. Create a bot with [@BotFather](https://t.me/BotFather) and copy its token.
2. Send any message to the bot, or add it to your group or channel.
3. In Neo Uptime open **Settings → Notifications → Add channel → Telegram**, paste the token and press **Find chats**. Pick the chat, choose the message language and send a test.

Turn on **On by default** and every new monitor, server and imported tunnel uses the channel. When `api.telegram.org` is filtered, set **API server** to a relay (for example a Cloudflare Worker) or set a **Proxy** (`socks5://…`).

**Slow tunnels:** in a monitor's **Alerts** step, turn on **Alert when it gets slow** and set the limit, for example *slower than 300 ms for at least 2 minutes*. You get one alert when it starts and one when it is fast again.

## Push monitors (Uptime Kuma compatible)

A push monitor gives you a URL in the same format as Kuma's:

```bash
curl -fsS "https://uptime.example.com/api/push/<token>?status=up&msg=OK&ping="
```

Scripts written for Kuma keep working after you change the host. If the URL is not called within the interval, the monitor goes down.

## Status pages

| Zen | Zen, Persian and dark |
|---|---|
| <img src="docs/screenshots/status-zen.jpg" alt="Zen theme" /> | <img src="docs/screenshots/status-zen-fa-dark.jpg" alt="Zen theme in Persian, dark" /> |
| **Aurora** | **Clarity** |
| <img src="docs/screenshots/status-aurora.jpg" alt="Aurora theme" /> | <img src="docs/screenshots/status-clarity.jpg" alt="Clarity theme" /> |
| **Lime** | **Noir** |
| <img src="docs/screenshots/status-lime.jpg" alt="Lime theme" /> | <img src="docs/screenshots/status-noir.jpg" alt="Noir theme" /> |

**Zen** is the default for new pages. It shows only the status: a headline, then one quiet row per server with its flag, its bars and its uptime. It follows the visitor's light or dark setting. Each page can be in English, Persian, or follow the visitor's browser, and can hide the banner, uptime, latency and charts.

Bars show either the last 100 checks (the default, the same history as the dashboard) or one bar per day for the last 7, 14, 30, 60 or 90 days. Upload a logo in the page's **General** tab; it is also the browser-tab icon.

Each page is served at `/status/<slug>`. Any one of them can also be shown at the site root (**Settings → General → Root address opens**) or on its own domain.

## A status page on its own domain, with HTTPS

1. At your DNS provider, add an **A record** for the name (for example `status.example.com`) pointing to this server's IP. With Cloudflare, use *DNS only* (grey cloud).
2. In the status page editor, enter it under **Own domain** and save. The editor shows whether DNS points here yet.
3. Click **Get SSL now** right there. It turns on automatic HTTPS if it is off: Neo Uptime then also listens on ports 80 and 443, gets a free Let's Encrypt certificate and renews it on its own. Later domains get theirs as soon as their DNS points here.

The page then opens at `https://status.example.com` — no port. Opening the domain over plain HTTP, or on the panel's port (`:7788`), redirects there. Visitors to the status domain see only that page — the admin panel, the sign-in page and the admin API are not reachable through it.

To drop the port from the admin panel too, set a **Panel domain** under **Settings → Domains & HTTPS** (for example `uptime.example.com`), then click **Use https://uptime.example.com** to make it the public address used in alert links, push URLs and agent commands.

<img src="docs/screenshots/editor-domain.jpg" alt="Status page editor: logo upload, own domain and HTTPS status" />

If another web server already uses ports 80 and 443 (nginx, Caddy, a panel), leave automatic HTTPS off and point that server at Neo Uptime instead (see [Configuration](#configuration)); the domain still shows only its page. The ports can be changed with `NEO_UPTIME_HTTP_PORT` and `NEO_UPTIME_HTTPS_PORT`, for example when a router forwards 443 to another port.

## Show your status on your own website

The owner account (the first account created) can hand out private keys to a status page under **Settings → Status API**. A key reads one page only, and can be limited to your site's address.

**Widget:** paste this into any page. It renders the servers with their flags and refreshes every minute.

```html
<div id="neo-status"></div>
<script src="https://uptime.example.com/embed.js" data-target="#neo-status"
  data-server="https://uptime.example.com" data-slug="main" data-key="sk_…"
  data-lang="fa" data-theme="auto" async></script>
```

`data-lang` is `fa` or `en`. `data-theme` is `auto`, `light` or `dark`.

<img src="docs/screenshots/widget.jpg" width="560" alt="The status widget on another website" />

**JSON:** call the API from your own code. The key can also be passed as `?key=`.

```bash
curl -H "Authorization: Bearer sk_…" https://uptime.example.com/api/v1/status/main
```

```json
{
  "overall": "operational",
  "groups": [{
    "name": "Tunnels",
    "monitors": [{ "name": "DE2", "country": "de", "status": "up", "latencyMs": 92,
                   "uptime24h": 0.9995, "uptime30d": 0.9981, "history": ["u","u","d","u"] }]
  }],
  "incidents": [], "maintenance": []
}
```

`overall` is `operational`, `partial_outage`, `major_outage`, `maintenance` or `unknown`. A monitor's `status` is `up`, `down`, `pending`, `maintenance`, `paused` or `unknown`.

## Configuration

`/etc/neo-uptime/neo-uptime.env` (restart after editing: `systemctl restart neo-uptime`):

```ini
NEO_UPTIME_HOST=0.0.0.0
NEO_UPTIME_PORT=7788
NEO_UPTIME_DATA=/var/lib/neo-uptime
# Ports for automatic HTTPS (Settings → Domains & HTTPS):
#NEO_UPTIME_HTTP_PORT=80
#NEO_UPTIME_HTTPS_PORT=443
# Or serve the panel with your own certificate; renewals are picked up without a restart.
#NEO_UPTIME_TLS_CERT=/etc/letsencrypt/live/uptime.example.com/fullchain.pem
#NEO_UPTIME_TLS_KEY=/etc/letsencrypt/live/uptime.example.com/privkey.pem
```

Set **Settings → General → Public address** so that alert links, push URLs and agent install commands use your real address.

Behind nginx, turn on **Settings → General → Behind a reverse proxy** and keep the event stream unbuffered:

```nginx
location / {
    proxy_pass http://127.0.0.1:7788;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
}
```

### Command line

```bash
neo-uptime reset-password <user> [new-password]   # prints a new password if none is given
neo-uptime disable-2fa <user>                     # lost phone
neo-uptime set-owner <user>                       # hand the owner role to another admin
neo-uptime list-users
neo-uptime version
journalctl -u neo-uptime -f                       # logs
journalctl -u neo-uptime-agent -f                 # agent logs, on a monitored server
```

### API

Create a key under **Settings → API keys**. Read keys can call `GET /api/*` and `/metrics`. Write keys can also create and change monitors, status pages and maintenance. Keys can never manage users, keys or backups.

```bash
curl -H "Authorization: Bearer nu_…" https://uptime.example.com/api/monitors
```

## Development

```bash
make dev     # mock BackPack panel on :7777 (token: test-token), API on :7788, UI with hot reload on :5173
make test    # go vet + go test
make linux   # release archives in dist/ (server and agent)
```

The backend is Go, using SQLite through a pure-Go driver (no CGO). The UI is React, TypeScript, Vite and Tailwind CSS, and it is embedded in the binary. The agent is a separate ~7 MB binary with no database.

## License

[MIT](LICENSE). Flags from [flag-icons](https://github.com/lipis/flag-icons) (MIT).

News and releases: [t.me/neoaurora](https://t.me/neoaurora)

---

<div dir="rtl">

## فارسی

**Neo Uptime** یک سیستم مانیتورینگ آپتایم خودمیزبان شبیه Uptime Kuma است که از تانل‌های **BackPack** و **Kariz** به‌صورت مستقیم پشتیبانی می‌کند. پنل مدیریت کاملاً فارسی است: راست‌به‌چپ، با اعداد فارسی و تاریخ شمسی.

### نصب با یک دستور

روی سرور لینوکس و با کاربر root این دستور را اجرا کنید:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/neoauroraproject/Neo-Uptime/main/install.sh)
```

هنگام نصب پورت را می‌پرسد (Enter یعنی ۷۷۸۸). بعد آدرس `http://IP-سرور:7788` را باز کنید و حساب مالک را بسازید. زبان پنل را از پایین منو (EN / فا) یا **تنظیمات ← ظاهر و زبان** عوض کنید.

اجرای دوباره‌ی همین دستور یک منو نشان می‌دهد: **آپدیت**، **تغییر پورت**، ری‌استارت، وضعیت و لاگ، بازنشانی رمز، **حذف نصب** (با حفظ داده‌ها یا همراه با حذف همه‌چیز).

- اگر GitHub کند یا فیلتر است، از `--mirror https://آدرس-میرور/` استفاده کنید.
- برای اسکریپت‌ها: `--update`، `--set-port 8080`، `--uninstall`.

### سرورهای دیگر: ایجنت برای BackPack و Kariz

لازم نیست Neo Uptime روی همان سروری باشد که تانل‌ها روی آن هستند. روی هر سرور ایجنت کوچک را نصب کنید:

۱. در Neo Uptime به **سرورها ← افزودن سرور** بروید، نام و پرچم کشور را انتخاب کنید و **ساختن و نمایش دستور نصب** را بزنید.

۲. دستوری را که نشان می‌دهد روی همان سرور با کاربر root اجرا کنید:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/neoauroraproject/Neo-Uptime/main/agent.sh) \
  --server https://uptime.example.com --token na_xxxxxxxx
```

۳. ظرف چند ثانیه سرور آنلاین می‌شود و تانل‌های BackPack و Kariz آن خودکار به مانیتور تبدیل می‌شوند.

ایجنت فقط داده ارسال می‌کند و هیچ پورتی باز نمی‌کند. هر ۲۰ ثانیه این‌ها را با توکن به Neo Uptime می‌فرستد: پردازنده، رم، دیسک و ترافیک سرور، تانل‌های BackPack (از فایل‌های `/etc/backpack`، بدون نیاز به توکن پنل)، تانل‌های Kariz (از سوکت کنترلی در `/etc/kariz`) و پینگ هر تانل.

### اتصال مستقیم به پنل BackPack

اگر Neo Uptime به پنل BackPack دسترسی دارد، می‌توانید پنل را مستقیم هم وصل کنید:

۱. در پنل وب BackPack به **Settings → Security → API tokens** بروید و یک توکن با سطح دسترسی **read** بسازید.

۲. در Neo Uptime به **تانل‌ها ← پنل‌ها ← اتصال پنل** بروید. آدرس پنل را دقیقاً همان‌طور که در مرورگر باز می‌کنید وارد کنید، با پورت و مسیر مخفی اگر دارد. توکن را هم وارد کنید و **آزمایش اتصال** را بزنید.

۳. ذخیره کنید. همه‌ی تانل‌ها خودکار به مانیتور تبدیل می‌شوند. Neo Uptime فقط API خواندنی BackPack را می‌خواند و هیچ تنظیمی از تانل‌ها را تغییر نمی‌دهد.

### هشدار تلگرام

۱. با [@BotFather](https://t.me/BotFather) یک ربات بسازید و توکنش را کپی کنید.

۲. یک پیام به ربات بفرستید (یا آن را به گروه یا کانال خود اضافه کنید).

۳. در **تنظیمات ← اعلان‌ها ← افزودن کانال ← تلگرام** توکن را وارد کنید و **یافتن چت‌ها** را بزنید. چت را انتخاب کنید، زبان پیام را **فارسی** بگذارید و پیام آزمایشی بفرستید.

اگر api.telegram.org فیلتر است، در فیلد **سرور API** آدرس یک رله (مثلاً Cloudflare Worker) یا در فیلد **پراکسی** یک `socks5://` وارد کنید.

**هشدار کندی تانل:** در مرحله‌ی **هشدارها** هنگام افزودن مانیتور، **هشدار هنگام کند شدن** را روشن کنید، مثلاً «کندتر از ۳۰۰ میلی‌ثانیه به مدت دست‌کم ۲ دقیقه». یک پیام هنگام شروع کندی و یکی هنگام برگشت به حالت عادی می‌گیرید.

### دامنه‌ی اختصاصی و SSL خودکار برای صفحه‌ی وضعیت

۱. در پنل DNS دامنه یک رکورد **A** (مثلاً `status.example.com`) به IP همین سرور بسازید. اگر از Cloudflare استفاده می‌کنید، حالت *DNS only* (ابر خاکستری) باشد.

۲. در ویرایشگر صفحه‌ی وضعیت، دامنه را در **دامنه‌ی اختصاصی** وارد کنید. همانجا لوگو را هم آپلود کنید.

۳. همانجا روی **دریافت فوری SSL** بزنید. اگر HTTPS خاموش باشد روشنش می‌کند، Neo Uptime روی پورت‌های ۸۰ و ۴۴۳ هم گوش می‌دهد، گواهی رایگان Let’s Encrypt می‌گیرد و خودش تمدیدش می‌کند.

صفحه از آن به بعد روی `https://status.example.com` و **بدون پورت** باز می‌شود؛ اگر کسی دامنه را با HTTP یا با پورت پنل (`:7788`) باز کند، به همین آدرس منتقل می‌شود. روی دامنه‌ی صفحه‌ی وضعیت فقط همان صفحه دیده می‌شود؛ پنل مدیریت و صفحه‌ی ورود از آن دامنه در دسترس نیستند.

برای اینکه خود پنل مدیریت هم بدون پورت باز شود، در **تنظیمات ← دامنه‌ها و HTTPS** یک **دامنه‌ی پنل** (مثلاً `uptime.example.com`) بگذارید و بعد روی دکمه‌ی «استفاده از https://uptime.example.com» بزنید تا آدرس عمومی پنل شود.

### صفحه‌ی وضعیت و API خصوصی

- پوسته‌ی **Zen** (پیش‌فرض): فقط وضعیت، بدون هیچ چیز اضافه؛ روشن یا تیره مطابق دستگاه بازدیدکننده، با پرچم هر سرور.
- نوارها یا ۱۰۰ بررسی آخر را نشان می‌دهند (پیش‌فرض، همان تاریخچه‌ی واقعی داشبورد) یا هر روز یک نوار برای ۷، ۱۴، ۳۰، ۶۰ یا ۹۰ روز اخیر.
- هر صفحه می‌تواند فارسی، انگلیسی یا خودکار (مطابق مرورگر) باشد.
- **API وضعیت:** فقط حساب مالک (اولین حسابی که ساخته شده) می‌تواند در **تنظیمات ← API وضعیت** کلید خصوصی بسازد. هر کلید فقط یک صفحه را می‌خواند و می‌شود آن را به آدرس سایت خودتان محدود کرد. با ویجت آماده (`embed.js`) یا با فراخوانی `GET /api/v1/status/<slug>` وضعیت را در سایت خودتان نشان دهید. نمونه‌ی کد در بخش انگلیسی بالا هست.

### امکانات

- **۲۸ نوع مانیتور:** HTTP، Keyword، JSON، TCP، Ping، DNS، WebSocket، گواهی SSL، سرور (ایجنت)، تانل از طریق ایجنت، تانل و پنل BackPack، Push، UDP، SMTP، MQTT، gRPC، RADIUS، SNMP، PostgreSQL، MySQL، SQL Server، MongoDB، Redis، Kafka، Docker، سرور بازی Steam و گروه.
- **۱۱ کانال اعلان:** تلگرام، بله، دیسکورد، اسلک، Teams، ایمیل، وب‌هوک و چند کانال دیگر، با پیام فارسی یا انگلیسی.
- **پرچم کشور** برای هر سرور و تانل، به‌صورت تصویر SVG (نه ایموجی) که در ویندوز هم درست دیده می‌شود.
- **پنل ادمین کامل:**
  - حساب مالک و نقش‌های admin، editor و viewer
  - ورود دومرحله‌ای (2FA)
  - مدیریت دستگاه‌هایی که وارد حساب شده‌اند
  - کلید API
  - بکاپ و بازگردانی
  - پنجره‌ی تعمیرات (maintenance)
- **سازگار با Push در Uptime Kuma:** اسکریپت‌های فعلی‌تان فقط با عوض کردن دامنه کار می‌کنند.
- **فونت‌ها و پرچم‌ها داخل خود برنامه هستند:** هیچ چیزی از Google یا CDN بارگذاری نمی‌شود، پس برای سرورهای ایران مشکلی ندارد.

### کانال تلگرام

اخبار، نسخه‌های جدید و راهنما: [t.me/neoaurora](https://t.me/neoaurora)

### بازیابی رمز

اگر رمز را فراموش کردید، این دستور را روی سرور اجرا کنید:

```bash
neo-uptime reset-password admin
```

</div>
