package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
	"github.com/neoauroraproject/Neo-Uptime/internal/monitor"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	client *http.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := NewHub()
	bp := backpack.NewCache()
	agents := monitor.NewAgentHub()
	mgr := monitor.NewManager(db, bp, agents, hub)
	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	static := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>app</title>")}}
	srv := httptest.NewServer(New(db, mgr, bp, agents, hub, static, t.TempDir()))
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &harness{t: t, srv: srv, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}

func (h *harness) do(method, path, body string, csrf bool) (int, map[string]any) {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf {
		req.Header.Set("X-Neo-Request", "1")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return resp.StatusCode, out
}

func TestSetupLoginAndMonitorFlow(t *testing.T) {
	h := newHarness(t)

	if code, out := h.do("GET", "/api/auth/state", "", false); code != 200 || out["setupRequired"] != true {
		t.Fatalf("auth state: %d %v", code, out)
	}
	if code, _ := h.do("GET", "/api/dashboard", "", false); code != 401 {
		t.Fatalf("dashboard without a session: %d", code)
	}
	if code, _ := h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, false); code != 403 {
		t.Fatalf("setup without the CSRF header must be refused, got %d", code)
	}
	if code, out := h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true); code != 200 {
		t.Fatalf("setup: %d %v", code, out)
	}
	if code, _ := h.do("POST", "/api/auth/setup", `{"username":"other","password":"long-enough-pw"}`, true); code != 409 {
		t.Fatalf("second setup must be refused, got %d", code)
	}

	code, out := h.do("POST", "/api/monitors", `{"name":"job","type":"push","interval":60}`, true)
	if code != 200 {
		t.Fatalf("create push monitor: %d %v", code, out)
	}
	token, _ := out["pushToken"].(string)
	if token == "" || out["active"] != true {
		t.Fatalf("push monitor should be active with a token: %v", out)
	}
	if code, _ := h.do("POST", "/api/monitors", `{"name":"bad","type":"http","config":{"url":"ftp://x"}}`, true); code != 400 {
		t.Fatalf("invalid URL accepted: %d", code)
	}
	if code, _ := h.do("POST", "/api/monitors", `{"name":"x","type":"tcp","config":{"hostname":"h","port":1}}`, false); code != 403 {
		t.Fatalf("cookie request without CSRF header accepted: %d", code)
	}

	if code, out := h.do("GET", "/api/push/"+token+"?status=up&msg=OK&ping=12", "", false); code != 200 || out["ok"] != true {
		t.Fatalf("push: %d %v", code, out)
	}
	if code, _ := h.do("GET", "/api/push/nope", "", false); code != 404 {
		t.Fatalf("unknown push token: %d", code)
	}

	code, out = h.do("GET", "/api/dashboard", "", false)
	if code != 200 {
		t.Fatalf("dashboard: %d", code)
	}
	mons, _ := out["monitors"].([]any)
	if len(mons) != 1 {
		t.Fatalf("want 1 monitor, got %d", len(mons))
	}
	last, _ := mons[0].(map[string]any)["last"].(map[string]any)
	if last == nil || last["status"].(float64) != 1 {
		t.Fatalf("pushed heartbeat not recorded: %v", mons[0])
	}
}

func TestAPIKeyScopes(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)
	code, out := h.do("POST", "/api/api-keys", `{"name":"prom","scope":"read"}`, true)
	if code != 200 {
		t.Fatalf("create key: %d %v", code, out)
	}
	secret := out["secret"].(string)

	call := func(method, path string) int {
		req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := call("GET", "/metrics"); c != 200 {
		t.Fatalf("read key on /metrics: %d", c)
	}
	if c := call("POST", "/api/monitors"); c != 403 {
		t.Fatalf("read key creating a monitor: %d", c)
	}
	if c := call("GET", "/api/users"); c != 403 {
		t.Fatalf("API key on an admin endpoint: %d", c)
	}
}

func TestAgentReportAndStatusAPI(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)

	code, out := h.do("POST", "/api/agents", `{"name":"Iran edge","country":"IR","interval":10}`, true)
	if code != 200 {
		t.Fatalf("create agent: %d %v", code, out)
	}
	token := out["token"].(string)
	if !strings.Contains(out["install"].(string), token) {
		t.Fatalf("install command lacks the token: %v", out["install"])
	}

	report := `{"format":1,"agentVersion":"test","interval":10,"system":{"hostname":"edge","cpuPercent":12,"memTotal":1000,"memUsed":400},
"backpack":{"installed":true,"mode":"files","tunnels":[{"name":"de2","source":"backpack","state":"online","ping":90,"carrier":"wssmux","direction":"reverse","peerCountry":"DE"}]},
"kariz":{"installed":true,"mode":"socket","tunnels":[{"name":"main","source":"kariz","state":"offline","ping":-1,"carrier":"tcpmux","direction":"reverse","lastError":"refused"}]}}`
	post := func(tok string) int {
		req, _ := http.NewRequest("POST", h.srv.URL+"/api/agent/report", strings.NewReader(report))
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := post("na_wrong"); c != 401 {
		t.Fatalf("wrong agent token: %d", c)
	}
	if c := post(token); c != 200 {
		t.Fatalf("report: %d", c)
	}

	// Auto-import runs in the background; wait for the three monitors.
	var mons []any
	for i := 0; i < 50; i++ {
		_, out = h.do("GET", "/api/dashboard", "", false)
		mons, _ = out["monitors"].([]any)
		if len(mons) >= 4 { // group + server + 2 tunnels
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	types := map[string]int{}
	var de2 map[string]any
	for _, m := range mons {
		mm := m.(map[string]any)
		types[mm["type"].(string)]++
		if mm["name"] == "de2" {
			de2 = mm
		}
	}
	if types["group"] != 1 || types["agent"] != 1 || types["agent-tunnel"] != 2 {
		t.Fatalf("imported monitors: %v", types)
	}
	if de2 == nil || de2["country"] != "de" {
		t.Fatalf("tunnel country should come from the peer: %v", de2)
	}

	// Private status API.
	ids := []string{}
	for _, m := range mons {
		mm := m.(map[string]any)
		if mm["type"] == "agent-tunnel" {
			ids = append(ids, fmt.Sprintf(`{"monitorId":%v}`, mm["id"]))
		}
	}
	code, out = h.do("POST", "/api/status-pages", `{"title":"Net","slug":"net","groups":[{"name":"Tunnels","monitors":[`+strings.Join(ids, ",")+`]}]}`, true)
	if code != 200 {
		t.Fatalf("status page: %d %v", code, out)
	}
	pageID := out["id"]
	code, out = h.do("POST", "/api/status-api-keys", fmt.Sprintf(`{"name":"site","statusPageId":%v,"allowedOrigins":"https://example.com"}`, pageID), true)
	if code != 200 {
		t.Fatalf("status key: %d %v", code, out)
	}
	secret := out["secret"].(string)

	get := func(key, origin string) (int, map[string]any) {
		req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/status/net", nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body
	}
	if c, _ := get("", ""); c != 401 {
		t.Fatalf("status API without key: %d", c)
	}
	if c, _ := get(secret, "https://evil.example"); c != 403 {
		t.Fatalf("status API from a foreign origin: %d", c)
	}
	c, body := get(secret, "https://example.com")
	if c != 200 || body["overall"] == nil {
		t.Fatalf("status API: %d %v", c, body)
	}
	groups := body["groups"].([]any)
	if len(groups) != 1 || len(groups[0].(map[string]any)["monitors"].([]any)) != 2 {
		t.Fatalf("status API groups: %v", groups)
	}
}

func TestOwnerOnlyStatusKeys(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)
	if code, out := h.do("POST", "/api/users", `{"username":"second","password":"long-enough-pw","role":"admin"}`, true); code != 200 {
		t.Fatalf("create admin: %d %v", code, out)
	}
	h.do("POST", "/api/auth/logout", "", true)
	if code, _ := h.do("POST", "/api/auth/login", `{"username":"second","password":"long-enough-pw"}`, true); code != 200 {
		t.Fatalf("login second admin: %d", code)
	}
	if code, _ := h.do("GET", "/api/status-api-keys", "", false); code != 403 {
		t.Fatalf("a non-owner admin listed status keys: %d", code)
	}
	if code, _ := h.do("GET", "/api/users", "", false); code != 200 {
		t.Fatalf("admin endpoints should still work: %d", code)
	}
}
