// Package xray tests V2Ray/Xray configs for real: each share link becomes an
// outbound of an Xray-core instance running inside this process, and a web
// request goes out through it. A config passes only when the request comes
// back, so it says what a user's client would see.
package xray

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Link is one parsed share link.
type Link struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"` // vless | vmess | trojan | shadowsocks | hysteria
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Network  string `json:"network"`  // tcp | ws | grpc | httpupgrade | xhttp | kcp | hysteria
	Security string `json:"security"` // none | tls | reality
	// Insecure skips certificate checks, as the link asks (allowInsecure).
	Insecure bool `json:"insecure,omitempty"`
	// Raw is the link as given.
	Raw string `json:"-"`

	outbound map[string]any
}

// Summary is a short label such as "VLESS · WS · TLS".
func (l *Link) Summary() string {
	parts := []string{strings.ToUpper(protoLabel(l.Protocol))}
	if l.Network != "" && l.Network != "tcp" && l.Network != "hysteria" {
		parts = append(parts, strings.ToUpper(l.Network))
	}
	if l.Security != "" && l.Security != "none" {
		parts = append(parts, strings.ToUpper(l.Security))
	}
	return strings.Join(parts, " · ")
}

// Endpoint is the server's host:port.
func (l *Link) Endpoint() string { return net.JoinHostPort(l.Address, strconv.Itoa(l.Port)) }

func protoLabel(p string) string {
	switch p {
	case "shadowsocks":
		return "SS"
	case "hysteria":
		return "Hysteria2"
	}
	return p
}

// Outbound is the Xray outbound this link describes.
func (l *Link) Outbound() map[string]any { return l.outbound }

// ErrUnsupported marks links Xray cannot run (such as TUIC or HTTP/2).
var ErrUnsupported = errors.New("not supported by Xray")

// Parse reads one share link: vless://, vmess://, trojan://, ss://,
// hysteria2:// (hy2://).
func Parse(raw string) (*Link, error) {
	raw = strings.TrimSpace(raw)
	// An Xray outbound in JSON, optionally after a "#name" line.
	if name, js, ok := strings.Cut(raw, "\n"); ok && strings.HasPrefix(name, "#") || strings.HasPrefix(raw, "{") {
		if !ok || !strings.HasPrefix(name, "#") {
			name, js = "", raw
		}
		l, err := parseOutbound(strings.TrimSpace(js))
		if err != nil {
			return nil, err
		}
		l.Name, l.Raw = firstOf(strings.TrimSpace(strings.TrimPrefix(name, "#")), l.Name, l.Endpoint()), raw
		return l, nil
	}
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok || rest == "" {
		return nil, errors.New("not a share link")
	}
	var (
		l   *Link
		err error
	)
	switch strings.ToLower(scheme) {
	case "vless":
		l, err = parseURLLink("vless", raw)
	case "trojan":
		l, err = parseURLLink("trojan", raw)
	case "vmess":
		l, err = parseVMess(rest, raw)
	case "ss":
		l, err = parseSS(rest)
	case "hysteria2", "hy2":
		l, err = parseHysteria2(raw)
	case "tuic", "hysteria", "wireguard", "wg", "ssr", "juicity", "anytls", "naive+https", "socks", "http", "https":
		return nil, fmt.Errorf("%s:// links are %w", scheme, ErrUnsupported)
	default:
		return nil, fmt.Errorf("unknown link type %q", scheme)
	}
	if err != nil {
		return nil, err
	}
	l.Raw = raw
	if l.Name == "" {
		l.Name = l.Endpoint()
	}
	return l, nil
}

// ---------- vless:// and trojan:// ----------

func parseURLLink(proto, raw string) (*Link, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("bad %s link: %w", proto, err)
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, err
	}
	secret := u.User.Username()
	if p, ok := u.User.Password(); ok && proto == "trojan" {
		secret += ":" + p
	}
	if secret == "" {
		return nil, fmt.Errorf("the %s link has no user id", proto)
	}
	q := u.Query()
	l := &Link{Name: fragment(u), Protocol: proto, Address: host, Port: port}
	settings := map[string]any{"address": host, "port": port}
	if proto == "vless" {
		settings["id"] = secret
		settings["encryption"] = firstOf(q.Get("encryption"), "none")
		if f := q.Get("flow"); f != "" {
			settings["flow"] = f
		}
	} else {
		settings["password"] = secret
	}
	defSec := "none"
	if proto == "trojan" {
		defSec = "tls"
	}
	stream, err := streamFromParams(l, params{
		network:   q.Get("type"),
		security:  firstOf(q.Get("security"), defSec),
		sni:       firstOf(q.Get("sni"), q.Get("peer")),
		alpn:      q.Get("alpn"),
		fp:        q.Get("fp"),
		pbk:       q.Get("pbk"),
		sid:       q.Get("sid"),
		spx:       q.Get("spx"),
		pqv:       q.Get("pqv"),
		header:    q.Get("headerType"),
		host:      q.Get("host"),
		path:      q.Get("path"),
		service:   q.Get("serviceName"),
		mode:      q.Get("mode"),
		authority: q.Get("authority"),
		seed:      q.Get("seed"),
		extra:     q.Get("extra"),
		insecure:  truthy(q.Get("allowInsecure")) || truthy(q.Get("insecure")),
		pcs:       q.Get("pcs"),
		ech:       q.Get("ech"),
	})
	if err != nil {
		return nil, err
	}
	l.outbound = map[string]any{"protocol": proto, "settings": settings, "streamSettings": stream}
	return l, nil
}

// ---------- vmess:// ----------

func parseVMess(rest, raw string) (*Link, error) {
	body := rest
	if i := strings.IndexAny(body, "#?"); i >= 0 && !looksBase64(body) {
		body = body[:i]
	}
	data, err := decodeBase64(body)
	if err != nil || !json.Valid(data) {
		// Some clients write vmess the way vless is written.
		if strings.Contains(rest, "@") {
			l, err := parseURLLink("vless", "vless://"+rest)
			if err != nil {
				return nil, err
			}
			l.Protocol = "vmess"
			s := l.outbound["settings"].(map[string]any)
			delete(s, "encryption")
			delete(s, "flow")
			s["security"] = "auto"
			l.outbound["protocol"] = "vmess"
			return l, nil
		}
		return nil, errors.New("bad vmess link: the body is not base64 JSON")
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("bad vmess link: %w", err)
	}
	s := func(k string) string { return strings.TrimSpace(anyString(v[k])) }
	port, err := strconv.Atoi(s("port"))
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("bad vmess port %q", s("port"))
	}
	host := strings.Trim(s("add"), "[]")
	if host == "" || s("id") == "" {
		return nil, errors.New("the vmess link has no address or id")
	}
	l := &Link{Name: s("ps"), Protocol: "vmess", Address: host, Port: port}
	sec := s("tls")
	if sec == "" {
		sec = "none"
	}
	header, mode := s("type"), ""
	if strings.EqualFold(s("net"), "grpc") {
		header, mode = "", s("type")
	}
	stream, err := streamFromParams(l, params{
		network: s("net"), security: sec, sni: s("sni"), alpn: s("alpn"), fp: s("fp"),
		pbk: s("pbk"), sid: s("sid"), spx: s("spx"),
		header: header, host: s("host"), path: s("path"), service: s("path"), mode: firstOf(s("mode"), mode),
		authority: s("authority"), seed: s("path"),
		insecure: truthy(s("allowInsecure")) || truthy(s("insecure")) || truthy(s("skip-cert-verify")),
	})
	if err != nil {
		return nil, err
	}
	settings := map[string]any{"address": host, "port": port, "id": s("id"), "security": firstOf(s("scy"), "auto")}
	l.outbound = map[string]any{"protocol": "vmess", "settings": settings, "streamSettings": stream}
	return l, nil
}

// ---------- ss:// ----------

func parseSS(rest string) (*Link, error) {
	name := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		name, _ = url.PathUnescape(rest[i+1:])
		rest = rest[:i]
	}
	var plugin string
	if i := strings.Index(rest, "?"); i >= 0 {
		q, _ := url.ParseQuery(rest[i+1:])
		plugin = q.Get("plugin")
		rest = rest[:i]
	}
	rest = strings.TrimSuffix(rest, "/")
	var method, password, hostport string
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		// SIP002: userinfo@host:port, userinfo in base64 or plain.
		info := rest[:at]
		hostport = rest[at+1:]
		if dec, err := decodeBase64(info); err == nil && strings.Contains(string(dec), ":") {
			info = string(dec)
		} else if un, err := url.PathUnescape(info); err == nil {
			info = un
		}
		method, password, _ = strings.Cut(info, ":")
	} else {
		dec, err := decodeBase64(rest)
		if err != nil {
			return nil, errors.New("bad ss link")
		}
		at := strings.LastIndex(string(dec), "@")
		if at < 0 {
			return nil, errors.New("bad ss link")
		}
		method, password, _ = strings.Cut(string(dec[:at]), ":")
		hostport = string(dec[at+1:])
	}
	if plugin != "" {
		return nil, fmt.Errorf("shadowsocks plugins (%s) are %w", strings.SplitN(plugin, ";", 2)[0], ErrUnsupported)
	}
	h, p, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, fmt.Errorf("bad ss address %q", hostport)
	}
	port, err := strconv.Atoi(p)
	if err != nil || port <= 0 || port > 65535 || method == "" {
		return nil, errors.New("bad ss link")
	}
	method = strings.ToLower(method)
	if method == "chacha20-poly1305" {
		method = "chacha20-ietf-poly1305"
	}
	l := &Link{Name: name, Protocol: "shadowsocks", Address: h, Port: port, Network: "tcp", Security: "none"}
	l.outbound = map[string]any{
		"protocol": "shadowsocks",
		"settings": map[string]any{"address": h, "port": port, "method": method, "password": password},
	}
	return l, nil
}

// ---------- hysteria2:// ----------

func parseHysteria2(raw string) (*Link, error) {
	raw = "hysteria2" + raw[strings.Index(raw, "://"):]
	// Port hopping ("443,20000-30000") is not a valid URL port: keep the first.
	if u0, err := url.Parse(raw); err != nil || u0.Port() == "" {
		raw = firstPort(raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("bad hysteria2 link: %w", err)
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, err
	}
	auth := u.User.Username()
	if p, ok := u.User.Password(); ok {
		auth += ":" + p
	}
	q := u.Query()
	l := &Link{Name: fragment(u), Protocol: "hysteria", Address: host, Port: port, Network: "hysteria", Security: "tls",
		Insecure: truthy(q.Get("insecure")) || truthy(q.Get("allowInsecure"))}
	tlsSet := map[string]any{"serverName": firstOf(q.Get("sni"), q.Get("peer"), hostIfName(host))}
	if a := splitList(q.Get("alpn")); len(a) > 0 {
		tlsSet["alpn"] = a
	} else {
		tlsSet["alpn"] = []string{"h3"}
	}
	if pin := firstOf(q.Get("pinSHA256"), q.Get("pcs")); pin != "" {
		tlsSet["pinnedPeerCertSha256"] = strings.ToLower(strings.ReplaceAll(pin, ":", ""))
	}
	stream := map[string]any{
		"network":          "hysteria",
		"security":         "tls",
		"tlsSettings":      tlsSet,
		"hysteriaSettings": map[string]any{"version": 2, "auth": auth},
	}
	switch o := q.Get("obfs"); o {
	case "":
	case "salamander":
		stream["finalmask"] = map[string]any{"udp": []any{map[string]any{"type": "salamander", "settings": map[string]any{"password": q.Get("obfs-password")}}}}
	default:
		return nil, fmt.Errorf("hysteria2 obfs %q is %w", o, ErrUnsupported)
	}
	l.outbound = map[string]any{
		"protocol":       "hysteria",
		"settings":       map[string]any{"version": 2, "address": host, "port": port},
		"streamSettings": stream,
	}
	return l, nil
}

// ---------- shared transport settings ----------

type params struct {
	network, security, sni, alpn, fp, pbk, sid, spx, pqv string
	header, host, path, service, mode, authority, seed   string
	extra, pcs, ech                                      string
	insecure                                             bool
}

func streamFromParams(l *Link, p params) (map[string]any, error) {
	network := strings.ToLower(firstOf(p.network, "tcp"))
	switch network {
	case "raw":
		network = "tcp"
	case "splithttp":
		network = "xhttp"
	case "websocket":
		network = "ws"
	case "mkcp":
		network = "kcp"
	case "h2", "http", "quic", "h3":
		return nil, fmt.Errorf("the %s transport was removed from Xray and is %w", network, ErrUnsupported)
	case "tcp", "ws", "grpc", "httpupgrade", "xhttp", "kcp":
	default:
		return nil, fmt.Errorf("unknown transport %q", p.network)
	}
	security := strings.ToLower(p.security)
	switch security {
	case "", "none", "false", "0":
		security = "none"
	case "tls", "xtls", "1", "true":
		security = "tls"
	case "reality":
	default:
		return nil, fmt.Errorf("unknown security %q", p.security)
	}
	l.Network, l.Security, l.Insecure = network, security, p.insecure && security == "tls"
	s := map[string]any{"network": network, "security": security}

	host := p.host
	switch network {
	case "tcp":
		if strings.EqualFold(p.header, "http") {
			req := map[string]any{"path": []string{firstOf(p.path, "/")}}
			if host != "" {
				req["headers"] = map[string]any{"Host": splitList(host)}
			}
			s["tcpSettings"] = map[string]any{"header": map[string]any{"type": "http", "request": req}}
		}
	case "ws":
		s["wsSettings"] = map[string]any{"path": firstOf(p.path, "/"), "host": host}
	case "httpupgrade":
		s["httpupgradeSettings"] = map[string]any{"path": firstOf(p.path, "/"), "host": host}
	case "grpc":
		g := map[string]any{"serviceName": p.service, "multiMode": strings.EqualFold(p.mode, "multi")}
		if p.authority != "" {
			g["authority"] = p.authority
		}
		s["grpcSettings"] = g
	case "xhttp":
		x := map[string]any{"path": firstOf(p.path, "/"), "host": host, "mode": firstOf(p.mode, "auto")}
		if p.extra != "" {
			var extra map[string]any
			if err := json.Unmarshal([]byte(p.extra), &extra); err == nil {
				x["extra"] = extra
			}
		}
		s["xhttpSettings"] = x
	case "kcp":
		k := map[string]any{}
		if p.header != "" && p.header != "none" {
			k["header"] = map[string]any{"type": p.header}
		}
		if p.seed != "" {
			k["seed"] = p.seed
		}
		s["kcpSettings"] = k
	}

	sni := firstOf(p.sni, firstOf(splitList(host)...))
	if sni == "" {
		sni = hostIfName(l.Address)
	}
	switch security {
	case "tls":
		t := map[string]any{"serverName": sni}
		if fp := fingerprint(p.fp, ""); fp != "" {
			t["fingerprint"] = fp
		}
		if a := splitList(p.alpn); len(a) > 0 {
			t["alpn"] = a
		}
		if p.pcs != "" {
			t["pinnedPeerCertSha256"] = p.pcs
		}
		if p.ech != "" {
			t["echConfigList"] = p.ech
		}
		s["tlsSettings"] = t
	case "reality":
		if p.pbk == "" {
			return nil, errors.New("the REALITY link has no public key (pbk)")
		}
		r := map[string]any{"serverName": sni, "fingerprint": fingerprint(p.fp, "chrome"), "publicKey": p.pbk, "shortId": p.sid}
		if p.spx != "" {
			r["spiderX"] = p.spx
		}
		if p.pqv != "" {
			r["mldsa65Verify"] = p.pqv
		}
		s["realitySettings"] = r
	}
	return s, nil
}

// fingerprint keeps the uTLS fingerprints Xray knows; others fall back.
func fingerprint(fp, fallback string) string {
	switch fp = strings.ToLower(strings.TrimSpace(fp)); fp {
	case "chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized", "randomizednoalpn":
		return fp
	}
	return fallback
}

// ---------- helpers ----------

func hostPort(u *url.URL) (string, int, error) {
	host := u.Hostname()
	if host == "" {
		return "", 0, errors.New("the link has no server address")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("bad port %q", u.Port())
	}
	return host, port, nil
}

func fragment(u *url.URL) string {
	if u.Fragment != "" {
		return strings.TrimSpace(u.Fragment)
	}
	return ""
}

// firstPort turns "host:443,2000-3000/?x" (port hopping) into "host:443/?x".
func firstPort(raw string) string {
	start := strings.Index(raw, "://") + 3
	end := len(raw)
	if i := strings.IndexAny(raw[start:], "/?#"); i >= 0 {
		end = start + i
	}
	if at := strings.LastIndex(raw[start:end], "@"); at >= 0 {
		start += at + 1
	}
	hp := raw[start:end]
	colon := strings.LastIndex(hp, ":")
	if colon < 0 {
		return raw
	}
	f := strings.FieldsFunc(hp[colon+1:], func(r rune) bool { return r == ',' || r == '-' })
	if len(f) == 0 {
		return raw
	}
	return raw[:start] + hp[:colon+1] + f[0] + raw[end:]
}

func hostIfName(h string) string {
	if net.ParseIP(h) != nil {
		return ""
	}
	return h
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstOf(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func anyString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

func looksBase64(s string) bool {
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '+' || r == '/' || r == '=' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// decodeBase64 accepts standard and URL alphabets, with or without padding.
func decodeBase64(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	s = strings.TrimRight(s, "=")
	if strings.ContainsAny(s, "-_") {
		return base64.RawURLEncoding.DecodeString(s)
	}
	return base64.RawStdEncoding.DecodeString(s)
}
