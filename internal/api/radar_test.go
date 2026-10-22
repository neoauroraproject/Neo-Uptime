package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// The radar shows on a status page only when it is on under Settings and on
// the page; it is read by the panel, not on each page load.
func TestRadarOnStatusPage(t *testing.T) {
	var hits atomic.Int32
	now := time.Now().UTC()
	ts := func(m int) string { return now.Add(-time.Duration(m) * time.Minute).Format(time.RFC3339) }
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprintf(w, `{"agents":[{"id":1,"name":"Asiatech Tehran","slug":"asiatech-tehran","location":"Tehran","is_internal":true,"is_online":true},
 {"id":2,"name":"Irancell Tehran","slug":"irancell-tehran","location":"Tehran","is_internal":true,"is_online":true}],
"targets":[{"id":10,"name":"Google","slug":"google"},{"id":11,"name":"Github","slug":"github"}],
"stats":{"1":{"10":[{"timestamp":%q,"latency":300,"is_up":true},{"timestamp":%q,"latency":310,"is_up":true}],
              "11":[{"timestamp":%q,"latency":320,"is_up":true},{"timestamp":%q,"latency":330,"is_up":true}]},
         "2":{"10":[{"timestamp":%q,"latency":0,"is_up":false},{"timestamp":%q,"latency":0,"is_up":false}],
              "11":[{"timestamp":%q,"latency":0,"is_up":true},{"timestamp":%q,"latency":0,"is_up":false},{"timestamp":%q,"latency":0,"is_up":false}]}}}`,
			ts(2), ts(1), ts(2), ts(1), ts(2), ts(1), ts(30), ts(2), ts(1))
	}))
	defer fake.Close()
	t.Setenv("NEO_UPTIME_RADAR_URL", fake.URL)

	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)
	h.app.StartRadar(h.ctx)
	_, out := h.do("POST", "/api/status-pages", `{"title":"Net","slug":"net","published":true,"radar":{"show":true,"datacenters":["asiatech-tehran","irancell-tehran","asiatech-tehran",""]},"groups":[]}`, true)
	id := out["id"]
	if r, _ := out["radar"].(map[string]any); r == nil || len(r["datacenters"].([]any)) != 2 {
		t.Fatalf("the page's radar choice should be kept, cleaned: %v", out["radar"])
	}
	public := func() map[string]any {
		t.Helper()
		resp, err := http.Get(h.srv.URL + "/api/status-page/net")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var d map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&d)
		return d
	}
	// Off under Settings: nothing is read, nothing shows.
	time.Sleep(200 * time.Millisecond)
	if hits.Load() != 0 {
		t.Fatalf("the radar must not be read while it is off (%d reads)", hits.Load())
	}
	if d := public(); d["radar"] != nil {
		t.Fatalf("radar off but shown: %v", d["radar"])
	}

	if code, _ := h.do("PUT", "/api/settings", `{"radarEnabled":"true"}`, true); code != 200 {
		t.Fatal("turn the radar on")
	}
	var radar map[string]any
	for i := 0; i < 50 && radar == nil; i++ {
		time.Sleep(100 * time.Millisecond)
		radar, _ = public()["radar"].(map[string]any)
	}
	if radar == nil {
		t.Fatal("the radar should show once it is on")
	}
	dcs := radar["datacenters"].([]any)
	state := map[string]string{}
	for _, d := range dcs {
		dm := d.(map[string]any)
		state[dm["slug"].(string)] = dm["state"].(string)
	}
	if state["asiatech-tehran"] != "ok" || state["irancell-tehran"] != "down" {
		t.Fatalf("states: %v", state)
	}
	// Page loads are answered from memory.
	before := hits.Load()
	for i := 0; i < 20; i++ {
		pageCache.mu.Lock()
		pageCache.m = map[string]cachedPage{}
		pageCache.mu.Unlock()
		public()
	}
	if hits.Load() != before {
		t.Fatalf("page loads read the radar: %d → %d", before, hits.Load())
	}
	if code, r := h.do("GET", "/api/radar", "", false); code != 200 || r["enabled"] != true || r["snapshot"] == nil {
		t.Fatalf("/api/radar: %d %v", code, r)
	}

	// The page can leave the radar out.
	h.do("PUT", fmt.Sprintf("/api/status-pages/%v", id), `{"title":"Net","slug":"net","published":true,"radar":{"show":false},"groups":[]}`, true)
	if d := public(); d["radar"] != nil {
		t.Fatalf("page radar off but shown: %v", d["radar"])
	}
	// Off again: the summary is dropped.
	h.do("PUT", fmt.Sprintf("/api/status-pages/%v", id), `{"title":"Net","slug":"net","published":true,"radar":{"show":true},"groups":[]}`, true)
	h.do("PUT", "/api/settings", `{"radarEnabled":"false"}`, true)
	time.Sleep(300 * time.Millisecond)
	if d := public(); d["radar"] != nil {
		t.Fatalf("radar turned off but still shown: %v", d["radar"])
	}
}
