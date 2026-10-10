package notify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func init() {
	register(&Provider{
		Type: "telegram", Name: "Telegram", Group: "Chat",
		Fields: []Field{
			{Key: "botToken", Label: "Bot token", Type: "password", Required: true, Placeholder: "123456:ABC-DEF…", Help: "From @BotFather."},
			{Key: "chatId", Label: "Chat ID", Type: "text", Required: true, Placeholder: "-1001234567890", Help: "User, group or channel id. Message @userinfobot to find yours."},
			{Key: "threadId", Label: "Topic (thread) ID", Type: "text", Help: "Optional — for forum topics in supergroups."},
			{Key: "apiBase", Label: "API server", Type: "url", Default: "https://api.telegram.org", Help: "Change when api.telegram.org is filtered — e.g. a Cloudflare Worker relay."},
			{Key: "proxy", Label: "Proxy", Type: "text", Placeholder: "socks5://127.0.0.1:1080", Help: "Optional http(s):// or socks5:// proxy for reaching Telegram."},
			{Key: "silent", Label: "Send silently", Type: "switch"},
		},
		send: sendTelegram,
	})
	register(&Provider{
		Type: "bale", Name: "Bale", Group: "Chat",
		Fields: []Field{
			{Key: "botToken", Label: "Bot token", Type: "password", Required: true, Help: "From @botfather inside Bale."},
			{Key: "chatId", Label: "Chat ID", Type: "text", Required: true},
		},
		send: sendBale,
	})
	register(&Provider{
		Type: "discord", Name: "Discord", Group: "Chat",
		Fields: []Field{
			{Key: "webhookUrl", Label: "Webhook URL", Type: "url", Required: true, Placeholder: "https://discord.com/api/webhooks/…"},
			{Key: "username", Label: "Display name", Type: "text", Default: "Neo Uptime"},
			{Key: "proxy", Label: "Proxy", Type: "text", Placeholder: "socks5://127.0.0.1:1080"},
		},
		send: sendDiscord,
	})
	register(&Provider{
		Type: "slack", Name: "Slack / Mattermost", Group: "Chat",
		Fields: []Field{
			{Key: "webhookUrl", Label: "Incoming webhook URL", Type: "url", Required: true},
			{Key: "channel", Label: "Channel override", Type: "text", Placeholder: "#alerts"},
			{Key: "username", Label: "Display name", Type: "text", Default: "Neo Uptime"},
		},
		send: sendSlack,
	})
	register(&Provider{
		Type: "teams", Name: "Microsoft Teams", Group: "Chat",
		Fields: []Field{
			{Key: "webhookUrl", Label: "Workflow / webhook URL", Type: "url", Required: true},
		},
		send: sendTeams,
	})
	register(&Provider{
		Type: "googlechat", Name: "Google Chat", Group: "Chat",
		Fields: []Field{
			{Key: "webhookUrl", Label: "Webhook URL", Type: "url", Required: true},
		},
		send: sendGoogleChat,
	})
	register(&Provider{
		Type: "email", Name: "Email (SMTP)", Group: "Email",
		Fields: []Field{
			{Key: "host", Label: "SMTP host", Type: "text", Required: true, Placeholder: "smtp.gmail.com"},
			{Key: "port", Label: "Port", Type: "number", Required: true, Default: "587"},
			{Key: "security", Label: "Security", Type: "select", Options: []string{"starttls", "tls", "none"}, Default: "starttls"},
			{Key: "username", Label: "Username", Type: "text"},
			{Key: "password", Label: "Password", Type: "password"},
			{Key: "from", Label: "From", Type: "text", Required: true, Placeholder: "Neo Uptime <alerts@example.com>"},
			{Key: "to", Label: "To", Type: "text", Required: true, Placeholder: "ops@example.com, me@example.com"},
			{Key: "ignoreTls", Label: "Ignore TLS errors", Type: "switch"},
		},
		send: sendEmail,
	})
	register(&Provider{
		Type: "webhook", Name: "Webhook", Group: "Developer",
		Fields: []Field{
			{Key: "url", Label: "URL", Type: "url", Required: true},
			{Key: "contentType", Label: "Body", Type: "select", Options: []string{"json", "form"}, Default: "json"},
			{Key: "headers", Label: "Extra headers (JSON)", Type: "textarea", Placeholder: `{"Authorization": "Bearer …"}`},
		},
		send: sendWebhook,
	})
	register(&Provider{
		Type: "ntfy", Name: "ntfy", Group: "Push",
		Fields: []Field{
			{Key: "server", Label: "Server", Type: "url", Default: "https://ntfy.sh"},
			{Key: "topic", Label: "Topic", Type: "text", Required: true},
			{Key: "priority", Label: "Priority (1-5)", Type: "number", Default: "4"},
			{Key: "token", Label: "Access token", Type: "password"},
		},
		send: sendNtfy,
	})
	register(&Provider{
		Type: "gotify", Name: "Gotify", Group: "Push",
		Fields: []Field{
			{Key: "server", Label: "Server", Type: "url", Required: true},
			{Key: "appToken", Label: "Application token", Type: "password", Required: true},
			{Key: "priority", Label: "Priority", Type: "number", Default: "8"},
		},
		send: sendGotify,
	})
	register(&Provider{
		Type: "pushover", Name: "Pushover", Group: "Push",
		Fields: []Field{
			{Key: "userKey", Label: "User key", Type: "password", Required: true},
			{Key: "appToken", Label: "Application token", Type: "password", Required: true},
			{Key: "device", Label: "Device", Type: "text"},
			{Key: "priority", Label: "Priority (-2..2)", Type: "number", Default: "0"},
		},
		send: sendPushover,
	})
}

func telegramHTML(e Event) string {
	var b strings.Builder
	b.WriteString("<b>" + html.EscapeString(e.Title()) + "</b>")
	for _, kv := range e.Lines() {
		b.WriteString("\n<b>" + html.EscapeString(kv[0]) + ":</b> " + html.EscapeString(kv[1]))
	}
	if e.Link != "" {
		b.WriteString("\n<a href=\"" + html.EscapeString(e.Link) + "\">Open dashboard</a>")
	}
	return b.String()
}

func sendTelegram(ctx context.Context, cfg map[string]string, e Event) error {
	base := strings.TrimRight(firstSet(cfg["apiBase"], "https://api.telegram.org"), "/")
	body := map[string]any{
		"chat_id":                  cfg["chatId"],
		"text":                     telegramHTML(e),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
		"disable_notification":     cfg["silent"] == "true",
	}
	if t := strings.TrimSpace(cfg["threadId"]); t != "" {
		if n, err := strconv.Atoi(t); err == nil {
			body["message_thread_id"] = n
		}
	}
	return postJSON(ctx, httpClient(cfg["proxy"]), base+"/bot"+cfg["botToken"]+"/sendMessage", body, nil)
}

func sendBale(ctx context.Context, cfg map[string]string, e Event) error {
	body := map[string]any{"chat_id": cfg["chatId"], "text": e.Text()}
	return postJSON(ctx, nil, "https://tapi.bale.ai/bot"+cfg["botToken"]+"/sendMessage", body, nil)
}

func sendDiscord(ctx context.Context, cfg map[string]string, e Event) error {
	var fields []map[string]any
	for _, kv := range e.Lines() {
		fields = append(fields, map[string]any{"name": kv[0], "value": kv[1], "inline": kv[0] != "Message"})
	}
	embed := map[string]any{
		"title":     e.Title(),
		"color":     e.Color(),
		"fields":    fields,
		"timestamp": e.Time.UTC().Format(time.RFC3339),
		"footer":    map[string]any{"text": firstSet(e.SiteTitle, "Neo Uptime")},
	}
	if e.Link != "" {
		embed["url"] = e.Link
	}
	body := map[string]any{"username": firstSet(cfg["username"], "Neo Uptime"), "embeds": []any{embed}}
	return postJSON(ctx, httpClient(cfg["proxy"]), cfg["webhookUrl"], body, nil)
}

func sendSlack(ctx context.Context, cfg map[string]string, e Event) error {
	var fields []map[string]any
	for _, kv := range e.Lines() {
		fields = append(fields, map[string]any{"title": kv[0], "value": kv[1], "short": kv[0] != "Message"})
	}
	body := map[string]any{
		"username": firstSet(cfg["username"], "Neo Uptime"),
		"text":     e.Title(),
		"attachments": []any{map[string]any{
			"color":      fmt.Sprintf("#%06x", e.Color()),
			"fields":     fields,
			"title_link": e.Link,
			"footer":     firstSet(e.SiteTitle, "Neo Uptime"),
			"ts":         e.Time.Unix(),
			"mrkdwn_in":  []string{"text"},
			"fallback":   e.Text(),
		}},
	}
	if c := strings.TrimSpace(cfg["channel"]); c != "" {
		body["channel"] = c
	}
	return postJSON(ctx, nil, cfg["webhookUrl"], body, nil)
}

func sendTeams(ctx context.Context, cfg map[string]string, e Event) error {
	facts := []map[string]string{}
	for _, kv := range e.Lines() {
		facts = append(facts, map[string]string{"title": kv[0], "value": kv[1]})
	}
	card := map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":    "AdaptiveCard",
		"version": "1.4",
		"body": []any{
			map[string]any{"type": "TextBlock", "text": e.Title(), "weight": "Bolder", "size": "Medium", "wrap": true},
			map[string]any{"type": "FactSet", "facts": facts},
		},
	}
	if e.Link != "" {
		card["actions"] = []any{map[string]any{"type": "Action.OpenUrl", "title": "Open dashboard", "url": e.Link}}
	}
	body := map[string]any{
		"type": "message",
		"attachments": []any{map[string]any{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"content":     card,
		}},
	}
	return postJSON(ctx, nil, cfg["webhookUrl"], body, nil)
}

func sendGoogleChat(ctx context.Context, cfg map[string]string, e Event) error {
	return postJSON(ctx, nil, cfg["webhookUrl"], map[string]any{"text": e.Text()}, nil)
}

func sendWebhook(ctx context.Context, cfg map[string]string, e Event) error {
	headers := map[string]string{}
	if h := strings.TrimSpace(cfg["headers"]); h != "" {
		if err := json.Unmarshal([]byte(h), &headers); err != nil {
			return fmt.Errorf("headers must be a JSON object of strings: %w", err)
		}
	}
	// Shaped like Uptime Kuma's webhook body so existing receivers keep working.
	payload := map[string]any{
		"msg": e.Text(),
		"heartbeat": map[string]any{
			"status": e.Status, "msg": e.Msg, "time": e.Time.UTC().Format(time.RFC3339), "ping": e.Ping,
		},
		"monitor": map[string]any{
			"id": e.MonitorID, "name": e.MonitorName, "type": e.MonitorType, "url": e.Target,
		},
		"event": e.Kind,
	}
	if cfg["contentType"] == "form" {
		data, _ := json.Marshal(payload)
		form := url.Values{"data": {string(data)}}
		return doRequest(ctx, nil, http.MethodPost, cfg["url"], "application/x-www-form-urlencoded",
			strings.NewReader(form.Encode()), headers)
	}
	return postJSON(ctx, nil, cfg["url"], payload, headers)
}

func sendNtfy(ctx context.Context, cfg map[string]string, e Event) error {
	server := strings.TrimRight(firstSet(cfg["server"], "https://ntfy.sh"), "/")
	prio, _ := strconv.Atoi(firstSet(cfg["priority"], "4"))
	tag := "green_circle"
	switch {
	case e.Kind == "cert":
		tag = "warning"
	case e.Status == StatusDown:
		tag = "red_circle"
	case e.Status == StatusMaintenance:
		tag = "blue_circle"
	}
	body := map[string]any{
		"topic":    cfg["topic"],
		"title":    strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(e.Title(), "🟢"), "🔴"), "🔵")),
		"message":  strings.TrimPrefix(e.Text(), e.Title()+"\n"),
		"priority": prio,
		"tags":     []string{tag},
	}
	if e.Link != "" {
		body["click"] = e.Link
	}
	headers := map[string]string{}
	if t := cfg["token"]; t != "" {
		headers["Authorization"] = "Bearer " + t
	}
	return postJSON(ctx, nil, server, body, headers)
}

func sendGotify(ctx context.Context, cfg map[string]string, e Event) error {
	prio, _ := strconv.Atoi(firstSet(cfg["priority"], "8"))
	body := map[string]any{"title": e.Title(), "message": strings.TrimPrefix(e.Text(), e.Title()+"\n"), "priority": prio}
	return postJSON(ctx, nil, strings.TrimRight(cfg["server"], "/")+"/message?token="+url.QueryEscape(cfg["appToken"]), body, nil)
}

func sendPushover(ctx context.Context, cfg map[string]string, e Event) error {
	form := url.Values{
		"token":    {cfg["appToken"]},
		"user":     {cfg["userKey"]},
		"title":    {e.Title()},
		"message":  {strings.TrimPrefix(e.Text(), e.Title()+"\n")},
		"priority": {firstSet(cfg["priority"], "0")},
	}
	if d := cfg["device"]; d != "" {
		form.Set("device", d)
	}
	if e.Link != "" {
		form.Set("url", e.Link)
	}
	return doRequest(ctx, nil, http.MethodPost, "https://api.pushover.net/1/messages.json",
		"application/x-www-form-urlencoded", strings.NewReader(form.Encode()), nil)
}

func sendEmail(ctx context.Context, cfg map[string]string, e Event) error {
	host := strings.TrimSpace(cfg["host"])
	port := firstSet(cfg["port"], "587")
	addr := net.JoinHostPort(host, port)
	tlsCfg := &tls.Config{ServerName: host, InsecureSkipVerify: cfg["ignoreTls"] == "true"} //nolint:gosec

	var conn net.Conn
	var err error
	d := &net.Dialer{Timeout: 20 * time.Second}
	if cfg["security"] == "tls" {
		conn, err = tls.DialWithDialer(d, "tcp", addr, tlsCfg)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if cfg["security"] == "starttls" || cfg["security"] == "" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("STARTTLS: %w", err)
			}
		}
	}
	if cfg["username"] != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg["username"], cfg["password"], host)); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	from := cfg["from"]
	fromAddr := from
	if i := strings.LastIndex(from, "<"); i >= 0 {
		fromAddr = strings.Trim(from[i:], "<> ")
	}
	if err := c.Mail(fromAddr); err != nil {
		return err
	}
	var rcpts []string
	for _, r := range strings.Split(cfg["to"], ",") {
		if r = strings.TrimSpace(r); r != "" {
			rcpts = append(rcpts, r)
			if err := c.Rcpt(r); err != nil {
				return fmt.Errorf("recipient %s: %w", r, err)
			}
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	var htmlBody strings.Builder
	htmlBody.WriteString(`<div style="font-family:system-ui,sans-serif;max-width:560px">`)
	fmt.Fprintf(&htmlBody, `<h2 style="margin:0 0 12px;color:#%06x">%s</h2><table style="border-collapse:collapse">`, e.Color(), html.EscapeString(e.Title()))
	for _, kv := range e.Lines() {
		fmt.Fprintf(&htmlBody, `<tr><td style="padding:4px 12px 4px 0;color:#64748b">%s</td><td style="padding:4px 0">%s</td></tr>`,
			html.EscapeString(kv[0]), html.EscapeString(kv[1]))
	}
	htmlBody.WriteString(`</table>`)
	if e.Link != "" {
		fmt.Fprintf(&htmlBody, `<p><a href="%s">Open dashboard</a></p>`, html.EscapeString(e.Link))
	}
	htmlBody.WriteString(`</div>`)

	msg := "From: " + from + "\r\n" +
		"To: " + strings.Join(rcpts, ", ") + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("utf-8", e.Title()) + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		wrap76(base64.StdEncoding.EncodeToString([]byte(htmlBody.String())))
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func wrap76(s string) string {
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	b.WriteString(s)
	return b.String()
}

func firstSet(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
