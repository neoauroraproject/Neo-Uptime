// Package notify delivers alerts to chat apps, email and webhooks.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Status values, mirrored from store to keep this package dependency-free.
const (
	StatusDown        = 0
	StatusUp          = 1
	StatusPending     = 2
	StatusMaintenance = 3
)

// Event is one thing worth telling someone about.
type Event struct {
	Kind        string    `json:"kind"` // status | cert | test
	MonitorID   int64     `json:"monitorId"`
	MonitorName string    `json:"monitorName"`
	MonitorType string    `json:"monitorType"`
	Target      string    `json:"target"`
	Status      int       `json:"status"`
	Msg         string    `json:"msg"`
	Time        time.Time `json:"time"`
	Ping        *float64  `json:"ping"`
	Link        string    `json:"link"`
	SiteTitle   string    `json:"siteTitle"`
	// Downtime is how long the monitor was down, on recovery.
	Downtime time.Duration `json:"-"`
	// CertDays is the days left on a certificate, for cert events.
	CertDays int `json:"certDays,omitempty"`
	// Lang is the message language: "en" (default) or "fa".
	Lang string `json:"-"`
	// TZ is the zone times are written in; empty means UTC.
	TZ string `json:"-"`
}

func (e Event) fa() bool { return e.Lang == "fa" }

// faDigits writes Western digits as Persian ones.
func faDigits(s string) string {
	r := []rune(s)
	for i, c := range r {
		if c >= '0' && c <= '9' {
			r[i] = '۰' + (c - '0')
		}
	}
	return string(r)
}

func (e Event) when() string {
	t := e.Time
	label := "UTC"
	if e.TZ != "" {
		if loc, err := time.LoadLocation(e.TZ); err == nil {
			t, label = t.In(loc), e.TZ
		}
	} else {
		t = t.UTC()
	}
	s := t.Format("2006-01-02 15:04:05") + " " + label
	if e.fa() {
		return faDigits(s)
	}
	return s
}

// StatusWord is UP / DOWN / PENDING / MAINTENANCE.
func StatusWord(s int) string {
	switch s {
	case StatusUp:
		return "UP"
	case StatusDown:
		return "DOWN"
	case StatusPending:
		return "PENDING"
	case StatusMaintenance:
		return "MAINTENANCE"
	}
	return "UNKNOWN"
}

func statusDot(s int) string {
	switch s {
	case StatusUp:
		return "🟢"
	case StatusDown:
		return "🔴"
	case StatusPending:
		return "🟡"
	case StatusMaintenance:
		return "🔵"
	}
	return "⚪"
}

// Color returns the event's colour as 0xRRGGBB.
func (e Event) Color() int {
	switch {
	case e.Kind == "cert":
		return 0xf59e0b
	case e.Status == StatusUp:
		return 0x22c55e
	case e.Status == StatusDown:
		return 0xef4444
	case e.Status == StatusMaintenance:
		return 0x3b82f6
	}
	return 0xf59e0b
}

// Title is a one-line summary.
func (e Event) Title() string {
	if e.fa() {
		switch e.Kind {
		case "test":
			return "🧪 پیام آزمایشی Neo Uptime"
		case "cert":
			return fmt.Sprintf("⚠️ [%s] گواهی SSL تا %s روز دیگر منقضی می‌شود", e.MonitorName, faDigits(fmt.Sprint(e.CertDays)))
		}
		words := map[int]string{StatusUp: "دوباره در دسترس است", StatusDown: "قطع شد", StatusPending: "در انتظار بررسی", StatusMaintenance: "در حالت تعمیرات"}
		return fmt.Sprintf("%s [%s] %s", statusDot(e.Status), e.MonitorName, words[e.Status])
	}
	switch e.Kind {
	case "test":
		return "🧪 Neo Uptime test notification"
	case "cert":
		return fmt.Sprintf("⚠️ [%s] certificate expires in %d days", e.MonitorName, e.CertDays)
	}
	return fmt.Sprintf("%s [%s] is %s", statusDot(e.Status), e.MonitorName, StatusWord(e.Status))
}

// Lines is the body as label/value pairs, for providers with rich layouts.
func (e Event) Lines() [][2]string {
	lb := func(en, fa string) string {
		if e.fa() {
			return fa
		}
		return en
	}
	if e.Kind == "test" {
		return [][2]string{{lb("Message", "پیام"), lb("If you can read this, the channel works.", "اگر این پیام را می‌بینید، این کانال درست کار می‌کند.")}}
	}
	var l [][2]string
	if e.Msg != "" {
		l = append(l, [2]string{lb("Message", "پیام"), e.Msg})
	}
	if e.Target != "" {
		l = append(l, [2]string{lb("Target", "مقصد"), e.Target})
	}
	if e.Ping != nil && e.Status == StatusUp {
		v := fmt.Sprintf("%.0f ms", *e.Ping)
		if e.fa() {
			v = faDigits(fmt.Sprintf("%.0f", *e.Ping)) + " میلی‌ثانیه"
		}
		l = append(l, [2]string{lb("Response", "زمان پاسخ"), v})
	}
	if e.Downtime > 0 {
		v := humanDuration(e.Downtime)
		if e.fa() {
			v = faDuration(e.Downtime)
		}
		l = append(l, [2]string{lb("Downtime", "مدت قطعی"), v})
	}
	l = append(l, [2]string{lb("Time", "زمان"), e.when()})
	return l
}

func faDuration(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d روز", days))
	}
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%d ساعت", h))
	}
	if m > 0 && days == 0 {
		parts = append(parts, fmt.Sprintf("%d دقیقه", m))
	}
	if len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d ثانیه", s))
	}
	return faDigits(strings.Join(parts, " و "))
}

// LinkLabel is the text of the dashboard link.
func (e Event) LinkLabel() string {
	if e.fa() {
		return "باز کردن داشبورد"
	}
	return "Open dashboard"
}

// Text is the full plain-text message.
func (e Event) Text() string {
	var b strings.Builder
	b.WriteString(e.Title())
	for _, kv := range e.Lines() {
		b.WriteString("\n")
		b.WriteString(kv[0])
		b.WriteString(": ")
		b.WriteString(kv[1])
	}
	if e.Link != "" {
		b.WriteString("\n")
		b.WriteString(e.Link)
	}
	return b.String()
}

func humanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, h, m)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// Field describes one setting of a provider, for the admin form.
type Field struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // text | password | textarea | number | select | switch | url
	Required    bool     `json:"required,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
	Options     []string `json:"options,omitempty"`
	Default     string   `json:"default,omitempty"`
}

// Provider is one kind of channel.
type Provider struct {
	Type   string  `json:"type"`
	Name   string  `json:"name"`
	Group  string  `json:"group"`
	Fields []Field `json:"fields"`
	send   func(ctx context.Context, cfg map[string]string, e Event) error
}

var providers = map[string]*Provider{}

func register(p *Provider) {
	p.Fields = append(p.Fields, Field{Key: "language", Label: "Message language", Type: "select", Options: []string{"en", "fa"}, Default: "en"})
	providers[p.Type] = p
}

// Providers lists every provider, grouped and sorted for display.
func Providers() []*Provider {
	out := make([]*Provider, 0, len(providers))
	for _, p := range providers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Send delivers e through a channel of the given type.
func Send(ctx context.Context, typ string, cfg map[string]string, e Event) error {
	p, ok := providers[typ]
	if !ok {
		return fmt.Errorf("unknown notification type %q", typ)
	}
	for _, f := range p.Fields {
		if f.Required && strings.TrimSpace(cfg[f.Key]) == "" {
			return fmt.Errorf("%s is required", f.Label)
		}
	}
	if e.Lang == "" {
		e.Lang = cfg["language"]
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return p.send(ctx, cfg, e)
}

// httpClient returns a client, through a proxy when one is configured.
func httpClient(proxy string) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if proxy = strings.TrimSpace(proxy); proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Transport: tr, Timeout: 25 * time.Second}
}

func postJSON(ctx context.Context, client *http.Client, u string, body any, headers map[string]string) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return doRequest(ctx, client, http.MethodPost, u, "application/json", bytes.NewReader(data), headers)
}

func doRequest(ctx context.Context, client *http.Client, method, u, ctype string, body io.Reader, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("User-Agent", "NeoUptime")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if client == nil {
		client = httpClient("")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}
