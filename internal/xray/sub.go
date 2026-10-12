package xray

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Usage is what a subscription says about its account, from the
// subscription-userinfo header most panels send.
type Usage struct {
	Upload   uint64 `json:"upload"`
	Download uint64 `json:"download"`
	Total    uint64 `json:"total"`  // 0 = unlimited
	Expire   int64  `json:"expire"` // unix seconds, 0 = never
}

// Sub is a fetched subscription.
type Sub struct {
	Title  string      `json:"title,omitempty"`
	Usage  *Usage      `json:"usage,omitempty"`
	Links  []*Link     `json:"links"`
	Errors []LinkError `json:"errors,omitempty"`
}

// LinkError is a line of a subscription that could not be used.
type LinkError struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// Fetch downloads and reads a subscription.
func Fetch(ctx context.Context, client *http.Client, url string) (*Sub, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(url), nil)
	if err != nil {
		return nil, fmt.Errorf("bad subscription address: %w", err)
	}
	// Panels pick a format by client; this one gets the plain link list.
	req.Header.Set("User-Agent", "v2rayN/6.31")
	req.Header.Set("Accept", "*/*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the subscription answered HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	s := &Sub{Usage: ParseUsage(resp.Header.Get("Subscription-Userinfo")), Title: ProfileTitle(resp.Header.Get("Profile-Title"))}
	s.Links, s.Errors = ParseList(body)
	if len(s.Links) == 0 && len(s.Errors) == 0 {
		return s, errors.New("the subscription is empty")
	}
	return s, nil
}

// ParseList reads a subscription body: base64 or plain share links, one per
// line, or Xray JSON configs.
func ParseList(body []byte) ([]*Link, []LinkError) {
	body = bytes.TrimSpace(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf")))
	var links []*Link
	var errs []LinkError
	if len(body) > 0 && (body[0] == '[' || body[0] == '{') {
		return parseJSONConfigs(body)
	}
	if !bytes.Contains(body, []byte("://")) {
		if dec, err := decodeBase64(string(body)); err == nil {
			body = dec
		}
	}
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		l, err := Parse(line)
		if err != nil {
			errs = append(errs, LinkError{Name: lineName(line), Error: err.Error()})
			continue
		}
		links = append(links, l)
	}
	return links, errs
}

// parseJSONConfigs reads full Xray configs, as some panels serve them: the
// proxy outbound of each becomes a link.
func parseJSONConfigs(body []byte) ([]*Link, []LinkError) {
	var list []json.RawMessage
	if body[0] == '{' {
		list = []json.RawMessage{body}
	} else if err := json.Unmarshal(body, &list); err != nil {
		return nil, []LinkError{{Name: "subscription", Error: "invalid JSON: " + err.Error()}}
	}
	var links []*Link
	var errs []LinkError
	for i, raw := range list {
		var c struct {
			Remarks   string           `json:"remarks"`
			Outbounds []map[string]any `json:"outbounds"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			errs = append(errs, LinkError{Name: fmt.Sprintf("config %d", i+1), Error: err.Error()})
			continue
		}
		var ob map[string]any
		for _, o := range c.Outbounds {
			if o["tag"] == "proxy" {
				ob = o
				break
			}
		}
		if ob == nil {
			for _, o := range c.Outbounds {
				if isProxy(anyString(o["protocol"])) {
					ob = o
					break
				}
			}
		}
		name := firstOf(c.Remarks, fmt.Sprintf("config %d", i+1))
		if ob == nil {
			errs = append(errs, LinkError{Name: name, Error: "no proxy outbound in this config"})
			continue
		}
		b, _ := json.Marshal(ob)
		l, err := parseOutbound(string(b))
		if err != nil {
			errs = append(errs, LinkError{Name: name, Error: err.Error()})
			continue
		}
		l.Name = name
		l.Raw = "#" + name + "\n" + string(b)
		links = append(links, l)
	}
	return links, errs
}

func isProxy(p string) bool {
	switch p {
	case "vless", "vmess", "trojan", "shadowsocks", "hysteria":
		return true
	}
	return false
}

// parseOutbound reads an Xray outbound written as JSON.
func parseOutbound(raw string) (*Link, error) {
	var ob map[string]any
	if err := json.Unmarshal([]byte(raw), &ob); err != nil {
		return nil, fmt.Errorf("invalid outbound JSON: %w", err)
	}
	proto := anyString(ob["protocol"])
	if !isProxy(proto) {
		return nil, fmt.Errorf("outbound protocol %q is %w", proto, ErrUnsupported)
	}
	l := &Link{Protocol: proto, Network: "tcp", Security: "none", Name: anyString(ob["tag"])}
	settings, _ := ob["settings"].(map[string]any)
	server := settings
	for _, k := range []string{"vnext", "servers"} {
		if list, ok := settings[k].([]any); ok && len(list) > 0 {
			server, _ = list[0].(map[string]any)
		}
	}
	if server != nil {
		l.Address = anyString(server["address"])
		l.Port, _ = strconv.Atoi(anyString(server["port"]))
	}
	if stream, ok := ob["streamSettings"].(map[string]any); ok {
		l.Network = firstOf(anyString(stream["network"]), "tcp")
		l.Security = firstOf(anyString(stream["security"]), "none")
		if t, ok := stream["tlsSettings"].(map[string]any); ok && t["allowInsecure"] == true {
			delete(t, "allowInsecure")
			l.Insecure = true
		}
	}
	if l.Address == "" || l.Port <= 0 {
		return nil, errors.New("the outbound has no server address")
	}
	delete(ob, "tag")
	delete(ob, "mux")
	l.outbound = ob
	if l.Name == "proxy" {
		l.Name = ""
	}
	return l, nil
}

func lineName(line string) string {
	if i := strings.LastIndex(line, "#"); i >= 0 && i < len(line)-1 {
		return strings.TrimSpace(unescape(line[i+1:]))
	}
	if len(line) > 40 {
		return line[:40] + "…"
	}
	return line
}

func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

// ParseUsage reads a subscription-userinfo header.
func ParseUsage(h string) *Usage {
	if h == "" {
		return nil
	}
	u := &Usage{}
	for _, part := range strings.Split(h, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		switch strings.ToLower(k) {
		case "upload":
			u.Upload = uint64(n)
		case "download":
			u.Download = uint64(n)
		case "total":
			u.Total = uint64(n)
		case "expire":
			u.Expire = int64(n)
		}
	}
	return u
}

// ProfileTitle reads a profile-title header, plain or "base64:…".
func ProfileTitle(h string) string {
	if rest, ok := strings.CutPrefix(h, "base64:"); ok {
		if b, err := decodeBase64(rest); err == nil {
			return string(b)
		}
	}
	return h
}

// Expired reports whether the subscription's account has ended.
func (u *Usage) Expired(now time.Time) bool { return u != nil && u.Expire > 0 && now.Unix() > u.Expire }
