//go:build !race

// Runs real Xray connections, which the race detector's checkptr mode
// rejects inside Xray-core; CI runs it in a plain pass.

package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"

	"github.com/neoauroraproject/Neo-Uptime/internal/agent"
)

const testUUID = "b831381d-6324-4d53-ad4f-8cda48b30811"

// vlessServer runs a real Xray VLESS server on localhost.
func vlessServer(t *testing.T) int {
	t.Helper()
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	body, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "vless",
			"settings": map[string]any{"clients": []any{map[string]any{"id": testUUID}}, "decryption": "none"}}},
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
	t.Cleanup(func() { inst.Close() })
	return port
}

func (h *harness) long(method, path, body string) (int, map[string]any) {
	h.t.Helper()
	c := &http.Client{Jar: h.client.Jar, Timeout: 90 * time.Second}
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Neo-Request", "1")
	resp, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return resp.StatusCode, out
}

// waitStatus waits for a monitor's latest heartbeat to have the status.
func (h *harness) waitStatus(id int, status int, within time.Duration) map[string]any {
	h.t.Helper()
	deadline := time.Now().Add(within)
	var last map[string]any
	for time.Now().Before(deadline) {
		_, m := h.do("GET", fmt.Sprintf("/api/monitors/%d", id), "", false)
		if hb, ok := m["last"].(map[string]any); ok {
			last = hb
			if hb["status"] == float64(status) {
				return m
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	h.t.Fatalf("monitor %d: no status %d within %s, last %v", id, status, within, last)
	return nil
}

// A subscription: its configs become monitors tested through a real Xray
// core, first on the panel, then on a server running neo-uptime-agent.
func TestSubscriptionsAndAgentTests(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer target.Close()
	port := vlessServer(t)

	good := fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp&security=none#%%F0%%9F%%87%%A9%%F0%%9F%%87%%AA%%20Frankfurt", testUUID, port)
	bad := fmt.Sprintf("vless://00000000-0000-4000-8000-000000000000@127.0.0.1:%d?type=tcp#Wrong%%20id", port)
	var mu sync.Mutex
	content := good + "\n" + bad + "\ntuic://x@h:1#Unusable\n"
	sub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Subscription-Userinfo", "upload=1000; download=2000; total=10737418240; expire=1893456000")
		_, _ = io.WriteString(w, base64.StdEncoding.EncodeToString([]byte(content)))
	}))
	defer sub.Close()

	// Preview lists the configs, with the flag read from the name.
	code, prev := h.do("POST", "/api/subscriptions/preview", fmt.Sprintf(`{"url":%q}`, sub.URL), true)
	if code != 200 || len(prev["configs"].([]any)) != 2 || len(prev["unusable"].([]any)) != 1 {
		t.Fatalf("preview: %d %v", code, prev)
	}
	first := prev["configs"].([]any)[0].(map[string]any)
	if first["key"] != "🇩🇪 Frankfurt" || first["country"] != "de" || first["summary"] != "VLESS" {
		t.Fatalf("preview item: %v", first)
	}
	if prev["usage"].(map[string]any)["total"] != float64(10737418240) {
		t.Fatalf("usage: %v", prev["usage"])
	}

	// Watch both, tested here on the panel.
	code, out := h.long("POST", "/api/subscriptions", fmt.Sprintf(`{"name":"Shop","url":%q,"interval":20,"testUrl":%q,"keys":["🇩🇪 Frankfurt","Wrong id"]}`,
		sub.URL, target.URL+"/generate_204"))
	if code != 200 || out["result"] == nil {
		t.Fatalf("create: %d %v", code, out)
	}
	res := out["result"].(map[string]any)
	ids := res["monitorIds"].([]any)
	if res["added"] != float64(2) || len(ids) != 2 {
		t.Fatalf("sync: %v", res)
	}
	subID := int(out["subscription"].(map[string]any)["id"].(float64))
	info := out["subscription"].(map[string]any)["info"].(map[string]any)
	if info["configs"] != float64(2) || info["unusable"] != float64(1) || info["total"] != float64(10737418240) {
		t.Fatalf("info: %v", info)
	}
	goodID, badID := int(ids[0].(float64)), int(ids[1].(float64))
	m := h.waitStatus(goodID, 1, 15*time.Second)
	if m["country"] != "de" || m["name"] != "Frankfurt" || m["xray"].(map[string]any)["summary"] != "VLESS" || !strings.Contains(m["last"].(map[string]any)["msg"].(string), "connected in") {
		t.Fatalf("good config: %v", m)
	}
	m = h.waitStatus(badID, 2, 15*time.Second) // pending: one retry before down
	if !strings.Contains(m["last"].(map[string]any)["msg"].(string), "closed") {
		t.Errorf("bad config message: %v", m["last"])
	}

	// One-off test from the panel.
	if _, r := h.long("POST", "/api/xray/test", fmt.Sprintf(`{"link":%q,"testUrl":%q}`, good, target.URL)); r["ok"] != true {
		t.Fatalf("test now: %v", r)
	}

	// A server with neo-uptime-agent takes over the tests.
	_, ag := h.do("POST", "/api/agents", `{"name":"Iran","interval":5,"autoImport":false}`, true)
	agentID := int(ag["agent"].(map[string]any)["id"].(float64))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go agent.New(agent.Config{Server: h.srv.URL, Token: ag["token"].(string), Interval: 5 * time.Second,
		BackpackDir: t.TempDir(), KarizDir: t.TempDir(), NoPing: true}).Run(ctx)

	code, out = h.long("PUT", fmt.Sprintf("/api/subscriptions/%d", subID), fmt.Sprintf(`{"name":"Shop","url":%q,"agentId":%d,"interval":20,"testUrl":%q}`,
		sub.URL, agentID, target.URL+"/generate_204"))
	if code != 200 {
		t.Fatalf("move tests to the agent: %d %v", code, out)
	}
	deadline := time.Now().Add(40 * time.Second)
	for {
		_, m = h.do("GET", fmt.Sprintf("/api/monitors/%d", goodID), "", false)
		if x, _ := m["xray"].(map[string]any); x["from"] == "Iran" && m["last"].(map[string]any)["status"] == float64(1) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no result from the agent: %v", m)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !strings.Contains(m["last"].(map[string]any)["msg"].(string), "from Iran") {
		t.Errorf("agent result message: %v", m["last"])
	}
	_, m = h.do("GET", fmt.Sprintf("/api/monitors/%d", badID), "", false)
	if m["config"].(map[string]any)["agentId"] != float64(agentID) {
		t.Fatalf("subscription settings did not reach its monitors: %v", m["config"])
	}

	// One-off test and fetch through the agent.
	if _, r := h.long("POST", "/api/xray/test", fmt.Sprintf(`{"link":%q,"testUrl":%q,"agentId":%d}`, good, target.URL, agentID)); r["ok"] != true {
		t.Fatalf("test through the agent: %v", r)
	}
	if _, r := h.long("POST", "/api/xray/test", fmt.Sprintf(`{"link":%q,"testUrl":%q,"agentId":%d}`, bad, target.URL, agentID)); r["ok"] != false || r["error"] == "" {
		t.Fatalf("bad config through the agent: %v", r)
	}

	// The subscription changes: the bad config leaves, the good one moves.
	mu.Lock()
	content = strings.Replace(good, "127.0.0.1", "localhost", 1) + "\n"
	mu.Unlock()
	code, out = h.long("POST", fmt.Sprintf("/api/subscriptions/%d/refresh", subID), `{}`)
	if r := out["result"].(map[string]any); code != 200 || r["updated"] != float64(1) || r["removed"] != float64(1) || r["added"] != float64(0) {
		t.Fatalf("refresh: %d %v", code, out)
	}
	_, m = h.do("GET", fmt.Sprintf("/api/monitors/%d", badID), "", false)
	if m["active"] != false || m["config"].(map[string]any)["gone"] != true {
		t.Fatalf("a config that left should be paused: %v", m)
	}
	_, m = h.do("GET", fmt.Sprintf("/api/monitors/%d", goodID), "", false)
	if m["xray"].(map[string]any)["endpoint"] != fmt.Sprintf("localhost:%d", port) {
		t.Fatalf("changed config not updated: %v", m["xray"])
	}

	// Deleting the subscription takes its monitors and group with it.
	if code, _ := h.do("DELETE", fmt.Sprintf("/api/subscriptions/%d", subID), "", true); code != 200 {
		t.Fatal("delete failed")
	}
	if _, d := h.do("GET", "/api/dashboard", "", false); len(d["monitors"].([]any)) != 0 {
		t.Fatalf("monitors left behind: %v", d["monitors"])
	}
}

// A speed test of a config, from the panel and from a server, is kept with
// the monitor.
func TestSpeedTestOfConfig(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)
	zero := make([]byte, 64<<10)
	speed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.UserAgent(), "Mozilla") { // as Cloudflare's does
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/__down":
			if r.URL.Query().Get("bytes") == "1" {
				_, _ = w.Write([]byte("x"))
				return
			}
			for {
				if _, err := w.Write(zero); err != nil {
					return
				}
			}
		case "/__up":
			_, _ = io.Copy(io.Discard, r.Body)
		}
	}))
	defer speed.Close()
	t.Setenv("NEO_UPTIME_SPEED_URL", speed.URL)
	t.Setenv("NEO_UPTIME_OOKLA_LIST", "off") // no real Speedtest.net in tests
	port := vlessServer(t)
	link := fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp&security=none#Speedy", testUUID, port)
	_, mon := h.do("POST", "/api/monitors", fmt.Sprintf(`{"name":"Speedy","type":"xray","interval":300,"config":{"link":%q}}`, link), true)
	id := int(mon["id"].(float64))
	view := func() map[string]any {
		_, m := h.do("GET", fmt.Sprintf("/api/monitors/%d", id), "", false)
		return m
	}
	if view()["speed"] != nil {
		t.Fatal("no speed before a test")
	}

	// start answers at once; the test is followed by polling it.
	runSpeed := func(body string) (map[string]any, []string) {
		code, out := h.do("POST", "/api/xray/speed", body, true)
		if code != 200 || out["ok"] != true || out["id"] == nil {
			t.Fatalf("start: %d %v", code, out)
		}
		var phases []string
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			_, st := h.do("GET", "/api/xray/speed/"+out["id"].(string), "", false)
			if p, _ := st["progress"].(map[string]any); p != nil {
				ph, _ := p["phase"].(string)
				if len(phases) == 0 || phases[len(phases)-1] != ph {
					phases = append(phases, ph)
				}
			}
			switch st["state"] {
			case "done":
				return st, phases
			case "error":
				t.Fatalf("speed test failed: %v", st["error"])
			}
			time.Sleep(150 * time.Millisecond)
		}
		t.Fatal("speed test did not finish")
		return nil, nil
	}
	st, phases := runSpeed(fmt.Sprintf(`{"monitorId":%d}`, id))
	sp, _ := st["result"].(map[string]any)
	if sp["down"].(float64) < 5 || sp["up"].(float64) < 5 || sp["server"] == nil || sp["from"] != nil || sp["jitter"] == nil || len(sp["downSamples"].([]any)) < 1 {
		t.Fatalf("panel speed test: %v", st)
	}
	if len(phases) < 2 || (phases[0] != "ping" && phases[0] != "server") {
		t.Fatalf("the page should see the phases go by: %v", phases)
	}
	if v, _ := view()["speed"].(map[string]any); v == nil || v["down"] != sp["down"] {
		t.Fatalf("result not kept: %v", view()["speed"])
	}

	// From a server running neo-uptime-agent.
	_, ag := h.do("POST", "/api/agents", `{"name":"Iran","interval":5,"autoImport":false}`, true)
	agentID := int(ag["agent"].(map[string]any)["id"].(float64))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go agent.New(agent.Config{Server: h.srv.URL, Token: ag["token"].(string), Interval: 5 * time.Second,
		BackpackDir: t.TempDir(), KarizDir: t.TempDir(), NoPing: true}).Run(ctx)
	time.Sleep(1500 * time.Millisecond)
	st, _ = runSpeed(fmt.Sprintf(`{"monitorId":%d,"agentId":%d}`, id, agentID))
	sp, _ = st["result"].(map[string]any)
	if sp["from"] != "Iran" || sp["down"].(float64) < 5 || len(sp["upSamples"].([]any)) < 1 {
		t.Fatalf("speed test from the agent: %v", st)
	}
	if v, _ := view()["speed"].(map[string]any); v == nil || v["from"] != "Iran" {
		t.Fatalf("agent result not kept: %v", view()["speed"])
	}

	// The server's own connection, without any config: on the panel and on
	// the agent, kept with the server.
	st, _ = runSpeed(`{"direct":true,"agentId":0}`)
	if sp, _ := st["result"].(map[string]any); sp["down"].(float64) < 5 || sp["from"] != nil {
		t.Fatalf("direct test on the panel: %v", st)
	}
	st, _ = runSpeed(fmt.Sprintf(`{"direct":true,"agentId":%d,"provider":"cloudflare"}`, agentID))
	if sp, _ := st["result"].(map[string]any); sp["down"].(float64) < 5 || sp["from"] != "Iran" {
		t.Fatalf("direct test on the agent: %v", st)
	}
	_, list := h.do("GET", fmt.Sprintf("/api/agents/%d", agentID), "", false)
	if v, _ := list["agent"].(map[string]any); v == nil {
		t.Fatalf("agent view: %v", list)
	}
	code, agents := h.doList("GET", "/api/agents")
	if code != 200 || len(agents) != 1 || agents[0]["speed"] == nil {
		t.Fatalf("the server's last speed test should come with the server: %d %v", code, agents)
	}
	if _, bad := h.do("POST", "/api/xray/speed", `{"direct":true,"agentId":0,"provider":"nope"}`, true); bad["ok"] != false {
		t.Fatalf("unknown provider: %v", bad)
	}

	// One test per config at a time.
	_, first := h.do("POST", "/api/xray/speed", fmt.Sprintf(`{"monitorId":%d}`, id), true)
	if _, second := h.do("POST", "/api/xray/speed", fmt.Sprintf(`{"monitorId":%d}`, id), true); second["ok"] != false {
		t.Fatalf("a second test of the same config should wait: %v", second)
	}
	for i := 0; i < 300; i++ {
		if _, s := h.do("GET", "/api/xray/speed/"+first["id"].(string), "", false); s["state"] == "done" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// A broken config reports why, and keeps the last good result.
	bad := fmt.Sprintf("vless://00000000-0000-4000-8000-000000000000@127.0.0.1:%d?type=tcp", port)
	_, started := h.do("POST", "/api/xray/speed", fmt.Sprintf(`{"link":%q}`, bad), true)
	for i := 0; ; i++ {
		_, st := h.do("GET", "/api/xray/speed/"+started["id"].(string), "", false)
		if st["state"] == "error" && st["error"] != nil {
			break
		}
		if i > 300 || st["state"] == "done" {
			t.Fatalf("a broken config must end in an error: %v", st)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if code, _ := h.do("POST", "/api/xray/speed", `{"link":"nonsense"}`, true); code != 400 {
		t.Fatalf("nonsense link: %d", code)
	}
}

// doList is do for answers that are a JSON array.
func (h *harness) doList(method, path string) (int, []map[string]any) {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, nil)
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var out []map[string]any
	_ = json.Unmarshal(data, &out)
	return resp.StatusCode, out
}
