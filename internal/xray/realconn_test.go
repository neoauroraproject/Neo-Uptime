//go:build !race

// Xray-core does pointer arithmetic that the race detector's checkptr mode
// rejects, so tests that run a real connection are left out of -race runs
// and run in a plain pass instead (see .github/workflows/ci.yml).

package xray

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
	"github.com/xtls/xray-core/infra/conf/serial"

	// Server sides, for the tests only.
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/proxy/vmess/inbound"
)

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
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
}

// A speed test through a config moves data both ways and measures it.
func TestSpeedThroughConfig(t *testing.T) {
	zero := make([]byte, 64<<10)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Like Cloudflare's: nothing that does not look like a browser.
		if !strings.Contains(r.UserAgent(), "Mozilla") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/down":
			if r.URL.Query().Get("bytes") == "1" {
				_, _ = w.Write([]byte("x"))
				return
			}
			for { // as much as the client takes
				if _, err := w.Write(zero); err != nil {
					return
				}
			}
		case "/up":
			_, _ = io.Copy(io.Discard, r.Body)
		}
	}))
	defer target.Close()
	t.Setenv("NEO_UPTIME_OOKLA_LIST", "off")
	t.Setenv("NEO_UPTIME_SPEED_URL", "")
	oldServers, oldWarm, oldDown, oldUp := speedServers, speedWarm, speedDownFor, speedUpFor
	t.Cleanup(func() { speedServers, speedWarm, speedDownFor, speedUpFor = oldServers, oldWarm, oldDown, oldUp })
	speedServers = speedServers[:1]
	speedServers[0].name = "local"
	speedServers[0].down = func(n int64) string { return fmt.Sprintf("%s/down?bytes=%d", target.URL, n) }
	speedServers[0].up = target.URL + "/up"
	speedWarm, speedDownFor, speedUpFor = 300*time.Millisecond, 2*time.Second, 2*time.Second

	const id = "b831381d-6324-4d53-ad4f-8cda48b30811"
	port := freePorts(t, 1)[0]
	srv := startServer(t, []map[string]any{{"listen": "127.0.0.1", "port": port, "protocol": "vless",
		"settings": map[string]any{"clients": []any{map[string]any{"id": id}}, "decryption": "none"}}})
	defer srv.Close()

	l, err := Parse(fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp#speed", id, port))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	phases := map[string]int{}
	var peak float64
	sp, err := MeasureSpeed(ctx, l, "", func(p agentproto.SpeedProgress) {
		phases[p.Phase]++
		if p.Phase != "ping" && p.Cur > peak {
			peak = p.Cur
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// It reported while it ran: pings one by one, then the speed over time.
	if phases["ping"] < speedPings || phases["download"] < 1 || phases["upload"] < 1 || peak < 5 {
		t.Fatalf("progress: %v peak %.0f", phases, peak)
	}
	if len(sp.DownSamples) < 1 || len(sp.UpSamples) < 1 || sp.Jitter < 0 {
		t.Fatalf("samples: %d down, %d up, jitter %.2f", len(sp.DownSamples), len(sp.UpSamples), sp.Jitter)
	}
	if sp.Down < 5 || sp.Up < 5 || sp.Ping < 0 || sp.Bytes < 1<<20 || sp.Server != "local" || sp.UpError != "" {
		t.Fatalf("speed: %+v", sp)
	}
	t.Logf("down %.0f Mbps, up %.0f Mbps, ping %.1f ms, %d MB moved", sp.Down, sp.Up, sp.Ping, sp.Bytes>>20)

	// A config that does not work says so, with the reason.
	bad, _ := Parse(fmt.Sprintf("vless://00000000-0000-4000-8000-000000000000@127.0.0.1:%d?type=tcp#bad", port))
	if _, err := MeasureSpeed(ctx, bad, "", nil); err == nil {
		t.Fatal("a wrong id must not measure a speed")
	} else {
		t.Log(err)
	}
}
