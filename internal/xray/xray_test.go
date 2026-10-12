package xray

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"

	// Server sides, for the tests only.
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/proxy/vmess/inbound"
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

// Real connections: an Xray server in this process, one inbound per kind of
// link, and a web server the configs must reach.
func TestRealConnections(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer target.Close()
	testURL := target.URL + "/generate_204"

	certPEM, keyPEM := selfSigned(t)
	certs := []map[string]any{{"certificate": strings.Split(strings.TrimSpace(string(certPEM)), "\n"), "key": strings.Split(strings.TrimSpace(string(keyPEM)), "\n")}}
	tlsSrv := map[string]any{"certificates": certs}

	// REALITY borrows the handshake of a real TLS site; a local one here.
	realSite := httptest.NewUnstartedServer(http.NotFoundHandler())
	realSite.Config.ErrorLog = log.New(io.Discard, "", 0)
	realSite.StartTLS()
	defer realSite.Close()
	priv, _ := ecdh.X25519().GenerateKey(rand.Reader)
	privKey := base64.RawURLEncoding.EncodeToString(priv.Bytes())
	pubKey := base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())

	const id = "b831381d-6324-4d53-ad4f-8cda48b30811"
	ss22 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 16))
	ports := freePorts(t, 11)
	inbounds := []map[string]any{
		{"port": ports[0], "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]any{"id": id}}, "decryption": "none"}},
		{"port": ports[1], "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]any{"id": id}}, "decryption": "none"},
			"streamSettings": map[string]any{"network": "ws", "wsSettings": map[string]any{"path": "/ws"}}},
		{"port": ports[2], "protocol": "vmess", "settings": map[string]any{"clients": []any{map[string]any{"id": id}}}},
		{"port": ports[3], "protocol": "trojan", "settings": map[string]any{"clients": []any{map[string]any{"password": "tr-pass"}}},
			"streamSettings": map[string]any{"security": "tls", "tlsSettings": tlsSrv}},
		{"port": ports[4], "protocol": "shadowsocks", "settings": map[string]any{"method": "aes-256-gcm", "password": "ss-pass", "network": "tcp,udp"}},
		{"port": ports[5], "protocol": "shadowsocks", "settings": map[string]any{"method": "2022-blake3-aes-128-gcm", "password": ss22, "network": "tcp,udp"}},
		{"port": ports[6], "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]any{"id": id}}, "decryption": "none"},
			"streamSettings": map[string]any{"network": "grpc", "grpcSettings": map[string]any{"serviceName": "gun"}}},
		{"port": ports[7], "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]any{"id": id}}, "decryption": "none"},
			"streamSettings": map[string]any{"network": "xhttp", "xhttpSettings": map[string]any{"path": "/xh"}}},
		{"port": ports[8], "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]any{"id": id, "flow": "xtls-rprx-vision"}}, "decryption": "none"},
			"streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{
				"target": strings.TrimPrefix(realSite.URL, "https://"), "serverNames": []string{"example.com"}, "privateKey": privKey, "shortIds": []string{"6ba85179e30d4fc2"}}}},
		{"port": ports[9], "protocol": "hysteria", "settings": map[string]any{"version": 2, "clients": []any{map[string]any{"auth": "hy-pass"}}},
			"streamSettings": map[string]any{"network": "hysteria", "hysteriaSettings": map[string]any{"version": 2}, "security": "tls", "tlsSettings": map[string]any{"certificates": certs, "alpn": []string{"h3"}}}},
		{"port": ports[10], "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]any{"id": id}}, "decryption": "none"},
			"streamSettings": map[string]any{"network": "httpupgrade", "httpupgradeSettings": map[string]any{"path": "/up"}}},
	}
	for i := range inbounds {
		inbounds[i]["listen"] = "127.0.0.1"
	}
	srv := startServer(t, inbounds)
	defer srv.Close()

	vm, _ := json.Marshal(map[string]any{"v": "2", "ps": "vmess", "add": "127.0.0.1", "port": ports[2], "id": id, "aid": 0, "net": "tcp", "tls": ""})
	ok := []string{
		fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp&security=none#vless-tcp", id, ports[0]),
		fmt.Sprintf("vless://%s@127.0.0.1:%d?type=ws&path=%%2Fws&host=example.com#vless-ws", id, ports[1]),
		"vmess://" + base64.StdEncoding.EncodeToString(vm),
		fmt.Sprintf("trojan://tr-pass@127.0.0.1:%d?security=tls&sni=localhost&allowInsecure=1#trojan-insecure", ports[3]),
		fmt.Sprintf("ss://%s@127.0.0.1:%d#ss", base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:ss-pass")), ports[4]),
		fmt.Sprintf("ss://2022-blake3-aes-128-gcm:%s@127.0.0.1:%d#ss2022", strings.ReplaceAll(ss22, "+", "%2B"), ports[5]),
		fmt.Sprintf("vless://%s@127.0.0.1:%d?type=grpc&serviceName=gun&mode=gun#grpc", id, ports[6]),
		fmt.Sprintf("vless://%s@127.0.0.1:%d?type=xhttp&path=%%2Fxh&mode=auto#xhttp", id, ports[7]),
		fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp&security=reality&sni=example.com&fp=chrome&pbk=%s&sid=6ba85179e30d4fc2&flow=xtls-rprx-vision#reality", id, ports[8], pubKey),
		fmt.Sprintf("hysteria2://hy-pass@127.0.0.1:%d/?sni=localhost&insecure=1#hy2", ports[9]),
		fmt.Sprintf("vless://%s@127.0.0.1:%d?type=httpupgrade&path=%%2Fup#httpupgrade", id, ports[10]),
	}
	for _, link := range ok {
		l, err := Parse(link)
		if err != nil {
			t.Errorf("parse %s: %v", link, err)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		r, err := Test(ctx, l, testURL)
		cancel()
		if err != nil || r.Status != 204 {
			t.Errorf("%s: %+v %v", l.Name, r, err)
		}
	}

	bad := []string{
		fmt.Sprintf("vless://00000000-0000-4000-8000-000000000000@127.0.0.1:%d?type=tcp#wrong-id", ports[0]),
		fmt.Sprintf("trojan://tr-pass@127.0.0.1:%d?security=tls&sni=localhost#untrusted-cert", ports[3]),
		fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp#closed-port", id, freePorts(t, 1)[0]),
	}
	for _, link := range bad {
		l, _ := Parse(link)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		r, err := Test(ctx, l, testURL)
		cancel()
		if err == nil {
			t.Errorf("%s should fail, got %+v", l.Name, r)
		} else {
			t.Logf("%s: %v", l.Name, err)
		}
	}
}

func startServer(t *testing.T, inbounds []map[string]any) *core.Instance {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"log":       map[string]any{"loglevel": "none"},
		"inbounds":  inbounds,
		"outbounds": []any{map[string]any{"protocol": "freedom"}},
	})
	jc, err := serial.DecodeJSONConfig(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jc.Build()
	if err != nil {
		t.Fatal(err)
	}
	inst, err := core.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Start(); err != nil {
		t.Fatal(err)
	}
	return inst
}

func freePorts(t *testing.T, n int) []int {
	t.Helper()
	var out []int
	var ls []net.Listener
	for range n {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ls = append(ls, l)
		out = append(out, l.Addr().(*net.TCPAddr).Port)
	}
	for _, l := range ls {
		l.Close()
	}
	return out
}

func selfSigned(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	_ = tls.Certificate{}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
}
