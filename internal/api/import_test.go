package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The add-monitor wizard creates a server without auto-import, waits for its
// report, then imports the chosen tunnels with its own alert settings.
func TestWizardImportAndSlowAlerts(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)
	_, nt := h.do("POST", "/api/notifications", `{"name":"tg","type":"webhook","active":true,"isDefault":true,"config":{"url":"http://127.0.0.1:1/x"}}`, true)
	ntID := int(nt["id"].(float64))

	_, out := h.do("POST", "/api/agents", `{"name":"Edge","country":"DE","interval":10,"autoImport":false}`, true)
	aid := int(out["agent"].(map[string]any)["id"].(float64))
	token := out["token"].(string)
	report := `{"format":1,"agentVersion":"test","interval":10,"system":{"hostname":"edge"},
"backpack":{"installed":true,"mode":"files","tunnels":[{"name":"de2","source":"backpack","state":"online","ping":90},{"name":"nl","source":"backpack","state":"online","ping":80}]}}`
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/agent/report", strings.NewReader(report))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("report: %v %v", err, resp)
	}
	resp.Body.Close()
	time.Sleep(300 * time.Millisecond)
	if _, out := h.do("GET", "/api/dashboard", "", false); len(out["monitors"].([]any)) != 0 {
		t.Fatalf("auto-import ran although it is off: %v", out["monitors"])
	}

	// Import one tunnel with the wizard's settings: no channels, 45 s, slow above 250 ms for 1 min.
	code, out := h.do("POST", fmt.Sprintf("/api/agents/%d/import", aid),
		`{"keys":["backpack/de2"],"notificationIds":[],"interval":45,"latencyAlertMs":250,"latencyAlertFor":60}`, true)
	if code != 200 || out["imported"] != float64(1) || out["serverMonitorId"] == nil || out["groupId"] == nil {
		t.Fatalf("import: %d %v", code, out)
	}
	ids := out["monitorIds"].([]any)
	if len(ids) != 2 { // the server and de2
		t.Fatalf("created: %v", ids)
	}
	var tunnelID int
	for _, raw := range ids {
		id := int(raw.(float64))
		_, m := h.do("GET", fmt.Sprintf("/api/monitors/%d", id), "", false)
		if m["interval"] != float64(45) || len(m["notificationIds"].([]any)) != 0 {
			t.Fatalf("monitor %d did not get the wizard's settings: %v", id, m)
		}
		if m["type"] == "agent-tunnel" {
			tunnelID = id
			cfg := m["config"].(map[string]any)
			if cfg["latencyAlertMs"] != float64(250) || cfg["latencyAlertFor"] != float64(60) {
				t.Fatalf("slow alert not set: %v", cfg)
			}
		}
	}

	// Bulk: change the slow alert, then turn it off.
	if code, _ := h.do("POST", "/api/monitors/bulk", fmt.Sprintf(`{"action":"latency","ids":[%d],"latencyAlertMs":400,"latencyAlertFor":300}`, tunnelID), true); code != 200 {
		t.Fatalf("bulk latency: %d", code)
	}
	_, m := h.do("GET", fmt.Sprintf("/api/monitors/%d", tunnelID), "", false)
	if cfg := m["config"].(map[string]any); cfg["latencyAlertMs"] != float64(400) || cfg["latencyAlertFor"] != float64(300) {
		t.Fatalf("bulk latency not applied: %v", cfg)
	}
	h.do("POST", "/api/monitors/bulk", fmt.Sprintf(`{"action":"latency","ids":[%d],"latencyAlertMs":0}`, tunnelID), true)
	_, m = h.do("GET", fmt.Sprintf("/api/monitors/%d", tunnelID), "", false)
	if cfg := m["config"].(map[string]any); cfg["latencyAlertMs"] != nil && cfg["latencyAlertMs"] != float64(0) {
		t.Fatalf("slow alert not cleared: %v", cfg)
	}

	// The default for new tunnels applies to the next import, with default channels.
	if code, _ := h.do("PUT", "/api/settings", `{"tunnelLatencyAlertMs":"500","tunnelLatencyAlertFor":"120"}`, true); code != 200 {
		t.Fatal("setting the default slow alert failed")
	}
	_, out = h.do("POST", fmt.Sprintf("/api/agents/%d/import", aid), `{"keys":["backpack/nl"]}`, true)
	nl := int(out["monitorIds"].([]any)[0].(float64))
	_, m = h.do("GET", fmt.Sprintf("/api/monitors/%d", nl), "", false)
	if cfg := m["config"].(map[string]any); cfg["latencyAlertMs"] != float64(500) || cfg["latencyAlertFor"] != float64(120) {
		t.Fatalf("default slow alert not used: %v", cfg)
	}
	if ns := m["notificationIds"].([]any); len(ns) != 1 || int(ns[0].(float64)) != ntID {
		t.Fatalf("default channels not used: %v", ns)
	}
}

// Tunnels left unticked in the wizard stay out when auto-import is on; new
// tunnels that appear later are added.
func TestSkippedTunnelsStayOut(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)
	_, out := h.do("POST", "/api/agents", `{"name":"Edge","interval":10,"autoImport":false}`, true)
	aid := int(out["agent"].(map[string]any)["id"].(float64))
	token := out["token"].(string)
	send := func(names ...string) {
		var ts []string
		for _, n := range names {
			ts = append(ts, fmt.Sprintf(`{"name":%q,"source":"backpack","state":"online","ping":50}`, n))
		}
		body := `{"format":1,"interval":10,"system":{"hostname":"e"},"backpack":{"installed":true,"mode":"files","tunnels":[` + strings.Join(ts, ",") + `]}}`
		req, _ := http.NewRequest("POST", h.srv.URL+"/api/agent/report", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	tunnels := func() map[string]bool {
		_, out := h.do("GET", "/api/dashboard", "", false)
		got := map[string]bool{}
		for _, m := range out["monitors"].([]any) {
			if mm := m.(map[string]any); mm["type"] == "agent-tunnel" {
				got[mm["name"].(string)] = true
			}
		}
		return got
	}

	send("a", "b")
	time.Sleep(200 * time.Millisecond)
	if code, out := h.do("POST", fmt.Sprintf("/api/agents/%d/import", aid), `{"keys":["backpack/a"],"skip":["backpack/b"]}`, true); code != 200 {
		t.Fatalf("import: %d %v", code, out)
	}
	h.do("PUT", fmt.Sprintf("/api/agents/%d", aid), `{"name":"Edge","interval":10,"autoImport":true}`, true)
	send("a", "b", "c")
	var got map[string]bool
	for i := 0; i < 40; i++ {
		if got = tunnels(); got["c"] {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !got["a"] || !got["c"] || got["b"] {
		t.Fatalf("want a and c watched, b skipped; got %v", got)
	}
}
