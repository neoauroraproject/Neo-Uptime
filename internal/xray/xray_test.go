package xray

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestParseLinks(t *testing.T) {
	vm, _ := json.Marshal(map[string]any{"v": "2", "ps": "DE vmess", "add": "de.example.com", "port": "443", "id": "b831381d-6324-4d53-ad4f-8cda48b30811",
		"aid": "0", "scy": "auto", "net": "ws", "type": "none", "host": "cdn.example.com", "path": "/ws?ed=2048", "tls": "tls", "sni": "", "fp": "chrome"})
	cases := []struct {
		link, name, summary, endpoint string
	}{
		{"vless://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:443?type=tcp&security=reality&pbk=Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw&sid=6ba85179e30d4fc2&sni=www.speedtest.net&fp=chrome&flow=xtls-rprx-vision#NL%20reality",
			"NL reality", "VLESS · REALITY", "1.2.3.4:443"},
		{"vmess://" + base64.StdEncoding.EncodeToString(vm), "DE vmess", "VMESS · WS · TLS", "de.example.com:443"},
		{"trojan://secret@tr.example.com:8443?type=grpc&serviceName=gun&sni=tr.example.com#TR", "TR", "TROJAN · GRPC · TLS", "tr.example.com:8443"},
		{"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pass")) + "@5.6.7.8:8388#SS%20one", "SS one", "SS", "5.6.7.8:8388"},
		{"ss://" + base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pw@9.9.9.9:1234")) + "#old", "old", "SS", "9.9.9.9:1234"},
		{"hysteria2://pw@hy.example.com:443,20000-30000/?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=x#HY", "HY", "HYSTERIA2 · TLS", "hy.example.com:443"},
		{"vless://id@[2001:db8::1]:2053?type=xhttp&path=%2Fx&mode=packet-up&security=tls&sni=a.example.com&allowInsecure=1", "", "VLESS · XHTTP · TLS", "[2001:db8::1]:2053"},
	}
	for _, c := range cases {
		l, err := Parse(c.link)
		if err != nil {
			t.Errorf("%s: %v", c.link[:20], err)
			continue
		}
		name := c.name
		if name == "" {
			name = c.endpoint
		}
		if l.Name != name || l.Summary() != c.summary || l.Endpoint() != c.endpoint {
			t.Errorf("%s: got %q %q %q", c.link[:20], l.Name, l.Summary(), l.Endpoint())
		}
		if _, err := build(l); err != nil {
			t.Errorf("%s: xray rejects it: %v", l.Name, err)
		}
	}
	for _, bad := range []string{"tuic://x@h:1", "vless://h:1", "vless://id@host:443?type=h2", "ss://bm9uZQ@h:1?plugin=obfs-local", "http://example.com"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%s should fail", bad)
		}
	}
}

func TestParseList(t *testing.T) {
	plain := "vless://id@h1.example.com:443?security=tls#one\n# comment\n\ntrojan://pw@h2.example.com:443#two\ntuic://x@h:1#three\n"
	links, errs := ParseList([]byte(base64.StdEncoding.EncodeToString([]byte(plain))))
	if len(links) != 2 || links[1].Name != "two" || len(errs) != 1 || errs[0].Name != "three" {
		t.Fatalf("base64 list: %v %v", links, errs)
	}
	if links, _ := ParseList([]byte(plain)); len(links) != 2 {
		t.Fatalf("plain list: %d", len(links))
	}
	js := `[{"remarks":"JSON one","outbounds":[{"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":"j.example.com","port":443,"users":[{"id":"b831381d-6324-4d53-ad4f-8cda48b30811","encryption":"none"}]}]},
		"streamSettings":{"network":"ws","security":"tls","tlsSettings":{"serverName":"j.example.com","allowInsecure":true}}},{"protocol":"freedom","tag":"direct"}]}]`
	links, errs = ParseList([]byte(js))
	if len(links) != 1 || links[0].Name != "JSON one" || links[0].Summary() != "VLESS · WS · TLS" || !links[0].Insecure || len(errs) != 0 {
		t.Fatalf("json configs: %+v %v", links, errs)
	}
	// What is stored for a JSON config reads back the same.
	again, err := Parse(links[0].Raw)
	if err != nil || again.Name != "JSON one" || again.Endpoint() != "j.example.com:443" || !again.Insecure {
		t.Fatalf("stored JSON config: %+v %v", again, err)
	}
	if _, err := build(again); err != nil {
		t.Fatalf("json config does not build: %v", err)
	}
	if u := ParseUsage("upload=100; download=2000; total=10737418240; expire=1767225600"); u.Total != 10737418240 || u.Download != 2000 || u.Expire != 1767225600 {
		t.Fatalf("usage: %+v", u)
	}
}
