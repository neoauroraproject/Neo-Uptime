package xray

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/app/proxyman"
	xlog "github.com/xtls/xray-core/common/log"
	xnet "github.com/xtls/xray-core/common/net"
	xserial "github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
	xtls "github.com/xtls/xray-core/transport/internet/tls"

	// What a client needs: the core, outbound protocols and transports.
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/log"
	_ "github.com/xtls/xray-core/app/policy"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/hysteria"
	_ "github.com/xtls/xray-core/proxy/shadowsocks"
	_ "github.com/xtls/xray-core/proxy/shadowsocks_2022"
	_ "github.com/xtls/xray-core/proxy/trojan"
	_ "github.com/xtls/xray-core/proxy/vless/outbound"
	_ "github.com/xtls/xray-core/proxy/vmess/outbound"
	_ "github.com/xtls/xray-core/transport/internet/grpc"
	_ "github.com/xtls/xray-core/transport/internet/headers/http"
	_ "github.com/xtls/xray-core/transport/internet/headers/noop"
	_ "github.com/xtls/xray-core/transport/internet/httpupgrade"
	_ "github.com/xtls/xray-core/transport/internet/hysteria"
	_ "github.com/xtls/xray-core/transport/internet/kcp"
	_ "github.com/xtls/xray-core/transport/internet/reality"
	_ "github.com/xtls/xray-core/transport/internet/splithttp"
	_ "github.com/xtls/xray-core/transport/internet/tagged/taggedimpl"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	_ "github.com/xtls/xray-core/transport/internet/tls"
	_ "github.com/xtls/xray-core/transport/internet/udp"
	_ "github.com/xtls/xray-core/transport/internet/websocket"
)

// DefaultURL is fetched through a config when nothing else is set: tiny,
// fast everywhere, and answers 204.
const DefaultURL = "https://www.gstatic.com/generate_204"

// Result is what one test measured.
type Result struct {
	// Connect is the first request: the handshake with the config's server
	// and the site, what clients show as "real delay".
	Connect time.Duration
	// Delay is a second request on the open connection: the round trip
	// through the config once connected.
	Delay  time.Duration
	Status int
}

// Xray reports deprecated features and the like while reading a config;
// nobody here can act on that, so its log goes nowhere. Each test instance
// also runs with logging off.
func init() { xlog.RegisterHandler(quiet{}) }

type quiet struct{}

func (quiet) Handle(xlog.Message) {}

// tests caps how many configs are tested at once in this process.
var tests = make(chan struct{}, 8)

// startMu: starting an Xray instance sets process-wide state (its system
// dialer), so instances come up one at a time; they then run side by side.
var startMu sync.Mutex

// Start builds and starts an Xray instance.
func Start(cfg *core.Config) (*core.Instance, error) {
	startMu.Lock()
	defer startMu.Unlock()
	inst, err := core.New(cfg)
	if err != nil {
		return nil, err
	}
	if err := inst.Start(); err != nil {
		inst.Close()
		return nil, err
	}
	return inst, nil
}

// Test connects through the link with a real Xray core and fetches testURL.
func Test(ctx context.Context, l *Link, testURL string) (*Result, error) {
	if testURL == "" {
		testURL = DefaultURL
	}
	select {
	case tests <- struct{}{}:
		defer func() { <-tests }()
	case <-ctx.Done():
		return nil, errors.New("timed out waiting for other tests")
	}
	cfg, err := build(l)
	if err != nil {
		return nil, fmt.Errorf("xray rejected the config: %w", err)
	}
	inst, err := Start(cfg)
	if err != nil {
		return nil, fmt.Errorf("xray could not start the config: %w", err)
	}
	defer inst.Close()

	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := xnet.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return core.Dial(ctx, inst, dest)
		},
		MaxIdleConnsPerHost: 1,
		DisableCompression:  true,
		TLSHandshakeTimeout: 0,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func() (int, time.Duration, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
		if err != nil {
			return 0, 0, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0 Safari/537.36")
		t0 := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return 0, 0, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return resp.StatusCode, time.Since(t0), nil
	}
	code, first, err := get()
	if err != nil {
		if why := diagnose(l); why != "" {
			return nil, errors.New(why)
		}
		return nil, explain(ctx, err)
	}
	r := &Result{Status: code, Connect: first, Delay: first}
	if code < 400 {
		if _, again, err := get(); err == nil {
			r.Delay = again
		}
	}
	return r, nil
}

// build turns a link into an Xray config with one outbound and no logs.
func build(l *Link) (*core.Config, error) {
	ob := map[string]any{}
	for k, v := range l.outbound {
		ob[k] = v
	}
	ob["tag"] = "proxy"
	body, err := json.Marshal(map[string]any{
		"log":       map[string]any{"loglevel": "none", "access": "none", "error": "none"},
		"outbounds": []any{ob},
	})
	if err != nil {
		return nil, err
	}
	jc, err := serial.DecodeJSONConfig(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	cfg, err := jc.Build()
	if err != nil {
		return nil, err
	}
	if l.Insecure {
		if err := allowInsecure(cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// allowInsecure skips certificate checks for links that ask for it. Xray's
// JSON loader refuses "allowInsecure" since mid-2026, but plenty of configs
// in use still rely on it, and clients still connect with them.
func allowInsecure(cfg *core.Config) error {
	for _, ob := range cfg.Outbound {
		if ob.SenderSettings == nil {
			continue
		}
		m, err := ob.SenderSettings.GetInstance()
		if err != nil {
			return err
		}
		sc, ok := m.(*proxyman.SenderConfig)
		if !ok || sc.StreamSettings == nil {
			continue
		}
		for i, ts := range sc.StreamSettings.SecuritySettings {
			inst, err := ts.GetInstance()
			if err != nil {
				return err
			}
			if tc, ok := inst.(*xtls.Config); ok {
				tc.AllowInsecure = true
				sc.StreamSettings.SecuritySettings[i] = xserial.ToTypedMessage(tc)
			}
		}
		ob.SenderSettings = xserial.ToTypedMessage(sc)
	}
	return nil
}

// explain turns transport errors into something a person can act on.
func explain(ctx context.Context, err error) error {
	msg := err.Error()
	if i := strings.Index(msg, `": `); i >= 0 && strings.HasPrefix(msg, "Get \"") {
		msg = msg[i+3:]
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "Client.Timeout"):
		return errors.New("no answer through the config before the timeout")
	case msg == "EOF" || strings.HasSuffix(msg, ": EOF") || strings.Contains(msg, "connection reset") || strings.Contains(msg, "closed pipe"):
		return errors.New("the connection through the config was closed — the server refused it, or the config is wrong or expired")
	}
	if len(msg) > 220 {
		msg = msg[:220] + "…"
	}
	return errors.New(msg)
}

// diagnose looks at a failed config from outside Xray, whose dial errors do
// not reach the caller: can the server be reached at all, and does its
// certificate hold? Empty when nothing stands out.
func diagnose(l *Link) string {
	if l.Network == "hysteria" || l.Network == "kcp" {
		return "" // UDP: nothing to try without the protocol itself
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", l.Endpoint())
	if err != nil {
		reason := "no answer"
		switch {
		case strings.Contains(err.Error(), "refused"):
			reason = "connection refused"
		case strings.Contains(err.Error(), "no such host"):
			reason = "the address does not resolve"
		case strings.Contains(err.Error(), "unreachable"):
			reason = "network unreachable"
		}
		return fmt.Sprintf("cannot reach the server %s (%s)", l.Endpoint(), reason)
	}
	defer c.Close()
	if l.Security != "tls" || l.Insecure {
		return ""
	}
	sni := l.Address
	if s, ok := l.outbound["streamSettings"].(map[string]any); ok {
		if t, ok := s["tlsSettings"].(map[string]any); ok {
			if n, _ := t["serverName"].(string); n != "" {
				sni = n
			}
		}
	}
	tc := tls.Client(c, &tls.Config{ServerName: sni})
	if err := tc.HandshakeContext(ctx); err != nil {
		var unknown x509.UnknownAuthorityError
		var host x509.HostnameError
		var invalid x509.CertificateInvalidError
		switch {
		case errors.As(err, &unknown):
			return fmt.Sprintf("the server's certificate is not trusted (self-signed or unknown issuer) — the config does not allow insecure connections")
		case errors.As(err, &host):
			return fmt.Sprintf("the server's certificate is not valid for %s", sni)
		case errors.As(err, &invalid):
			return "the server's certificate is expired or invalid"
		}
		return "TLS handshake with the server failed: " + err.Error()
	}
	return ""
}
