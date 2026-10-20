package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A NeoTunnel tunnel is named after its display name, follows it when it is
// renamed in NeoTunnel, and keeps a name someone typed.
func TestTunnelFollowsNeoTunnelDisplayName(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)
	_, out := h.do("POST", "/api/agents", `{"name":"Iran","interval":10,"autoImport":true}`, true)
	token := out["token"].(string)
	report := func(deLabel, nlLabel string) {
		t.Helper()
		body := fmt.Sprintf(`{"format":1,"agentVersion":"test","interval":10,"system":{"hostname":"ir"},
"neotunnel":{"installed":true,"mode":"files","tunnels":[
 {"name":"de","label":%q,"source":"neotunnel","state":"online","ping":40,"carrier":"gre","direction":"direct","node":"fsn","nodeLabel":"Hetzner FSN"},
 {"name":"nl","label":%q,"source":"neotunnel","state":"online","ping":50,"carrier":"gre","direction":"direct","node":"ams"}]}}`, deLabel, nlLabel)
		req, _ := http.NewRequest("POST", h.srv.URL+"/api/agent/report", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("report: %v %v", err, resp)
		}
		resp.Body.Close()
	}
	// name of the monitor watching each tunnel, once it is what want says
	names := func(want map[string]string) map[string]string {
		t.Helper()
		got := map[string]string{}
		for i := 0; i < 60; i++ {
			_, d := h.do("GET", "/api/dashboard", "", false)
			got = map[string]string{}
			for _, m := range d["monitors"].([]any) {
				mm := m.(map[string]any)
				if mm["type"] == "agent-tunnel" {
					cfg := mm["config"].(map[string]any)
					got[cfg["tunnelName"].(string)] = mm["name"].(string)
				}
			}
			ok := len(got) == len(want)
			for k, v := range want {
				ok = ok && got[k] == v
			}
			if ok {
				return got
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("monitor names: got %v, want %v", got, want)
		return nil
	}

	report("آلمان VIP", "")
	names(map[string]string{"de": "آلمان VIP", "nl": "nl"})

	// Renamed in NeoTunnel: the monitor follows.
	report("آلمان ۲", "هلند")
	got := names(map[string]string{"de": "آلمان ۲", "nl": "هلند"})

	// Someone names the de monitor themselves: NeoTunnel no longer renames it.
	_, d := h.do("GET", "/api/dashboard", "", false)
	var deID any
	for _, m := range d["monitors"].([]any) {
		mm := m.(map[string]any)
		if mm["name"] == got["de"] {
			deID = mm["id"]
		}
	}
	code, m := h.do("GET", fmt.Sprintf("/api/monitors/%v", deID), "", false)
	if code != 200 {
		t.Fatalf("get monitor: %d %v", code, m)
	}
	mon := m
	if inner, ok := m["monitor"].(map[string]any); ok {
		mon = inner
	}
	mon["name"] = "My Germany"
	raw, _ := json.Marshal(mon)
	b := string(raw)
	if code, out := h.do("PUT", fmt.Sprintf("/api/monitors/%v", deID), b, true); code != 200 {
		t.Fatalf("rename: %d %v", code, out)
	}
	report("آلمان ۳", "")
	names(map[string]string{"de": "My Germany", "nl": "nl"})
}
