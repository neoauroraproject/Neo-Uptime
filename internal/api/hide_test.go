package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

func TestHideKeys(t *testing.T) {
	p := &store.StatusPage{Title: "x", Slug: "x", Hide: []string{"port", "bogus", "flag", "port"}}
	if err := normPage(p); err != nil || !slices.Equal(p.Hide, []string{"flag", "port"}) {
		t.Fatalf("normPage: %v %v", err, p.Hide)
	}
	tn := &backpack.Tunnel{Direction: "reverse", Carrier: "wssmux", Preset: "aggressive", TunnelPort: "6729", Uptime: "1h5m",
		InBytes: 5, OutBytes: 6, TotalBytes: 11, PeerCountry: "DE", PeerLocation: "Frankfurt"}
	all, _ := json.Marshal(publicTunnelOf(&store.StatusPage{}, tn))
	for _, want := range []string{"reverse", "wssmux", "aggressive", "6729", "1h5m", `"inBytes":5`, `"totalBytes":11`, "DE", "Frankfurt"} {
		if !strings.Contains(string(all), want) {
			t.Errorf("nothing hidden, but %s is missing: %s", want, all)
		}
	}
	none, _ := json.Marshal(publicTunnelOf(&store.StatusPage{Hide: hideKeys}, tn))
	for _, gone := range []string{"reverse", "wssmux", "aggressive", "6729", "1h5m", "Bytes", "DE", "Frankfurt"} {
		if strings.Contains(string(none), gone) {
			t.Errorf("everything hidden, but %s is sent: %s", gone, none)
		}
	}
}

// A tunnel on a status page shows only what the page allows, and the
// page's private status API follows the same choices.
func TestStatusPageHidesTunnelDetails(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)
	_, out := h.do("POST", "/api/agents", `{"name":"Edge","country":"de","interval":10,"autoImport":true}`, true)
	token := out["token"].(string)
	report := `{"format":1,"agentVersion":"test","interval":10,"system":{"hostname":"edge"},
"backpack":{"installed":true,"mode":"files","tunnels":[{"name":"de2","source":"backpack","state":"online","ping":77,
"direction":"reverse","carrier":"wssmux","preset":"aggressive","tunnelPort":"6729","uptime":"1h5m0s",
"inBytes":3500,"outBytes":271,"totalBytes":3771,"peerCountry":"DE","peerLocation":"Frankfurt"}]}}`
	send := func() {
		req, _ := http.NewRequest("POST", h.srv.URL+"/api/agent/report", strings.NewReader(report))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("report: %v %v", err, resp)
		}
		resp.Body.Close()
	}
	send()

	var tunnelID int
	for i := 0; i < 50 && tunnelID == 0; i++ {
		time.Sleep(100 * time.Millisecond)
		_, d := h.do("GET", "/api/dashboard", "", false)
		for _, raw := range d["monitors"].([]any) {
			if m := raw.(map[string]any); m["type"] == "agent-tunnel" && m["tunnel"] != nil {
				tunnelID = int(m["id"].(float64))
			}
		}
		if tunnelID == 0 {
			send()
		}
	}
	if tunnelID == 0 {
		t.Fatal("the tunnel was not imported")
	}

	page := fmt.Sprintf(`{"title":"Status","slug":"net","published":true,"layout":"cards",
		"hide":["port","transport","profile","direction","traffic","connected","location","flag","bars","nope"],
		"groups":[{"name":"","monitors":[{"monitorId":%d}]}]}`, tunnelID)
	code, out := h.do("POST", "/api/status-pages", page, true)
	if code != 200 || len(out["hide"].([]any)) != 9 {
		t.Fatalf("create page: %d %v", code, out["hide"])
	}
	pid := int(out["id"].(float64))
	_, _, body := h.raw(h.client, "GET", "", "/api/status-page/net", "")
	if !strings.Contains(body, `"hide":["flag","location","bars","direction","transport","profile","port","traffic","connected"]`) {
		t.Errorf("page settings: %s", body)
	}
	for _, gone := range []string{"6729", "wssmux", "aggressive", "reverse", "Bytes", "1h5m0s", "Frankfurt", `"country"`, `"peerCountry"`} {
		if strings.Contains(body, gone) {
			t.Errorf("hidden %s is sent: %s", gone, body)
		}
	}
	if !strings.Contains(body, `"ping":77`) || !strings.Contains(body, `"tunnel":{`) {
		t.Errorf("the rest should stay: %s", body)
	}

	// The status API key of this page sees the same.
	_, k := h.do("POST", "/api/status-api-keys", fmt.Sprintf(`{"name":"site","statusPageId":%d}`, pid), true)
	secret, _ := k["secret"].(string)
	if secret == "" {
		t.Fatalf("status key: %v", k)
	}
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/status/net", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(v1), `"tunnel":{`) {
		t.Fatalf("status API: %d %s", resp.StatusCode, v1)
	}
	for _, gone := range []string{"wssmux", "aggressive", "reverse", "bytesIn", "1h5m0s", `"country"`} {
		if strings.Contains(string(v1), gone) {
			t.Errorf("status API sends hidden %s: %s", gone, v1)
		}
	}

	// Showing everything again takes effect at once.
	page = strings.Replace(page, `"hide":["port","transport","profile","direction","traffic","connected","location","flag","bars","nope"]`, `"hide":[]`, 1)
	if code, _ := h.do("PUT", fmt.Sprintf("/api/status-pages/%d", pid), page, true); code != 200 {
		t.Fatalf("update page: %d", code)
	}
	_, _, body = h.raw(h.client, "GET", "", "/api/status-page/net", "")
	for _, want := range []string{"6729", "wssmux", "aggressive", "reverse", `"inBytes":3500`, "1h5m0s", "Frankfurt", `"country":"de"`} {
		if !strings.Contains(body, want) {
			t.Errorf("%s should be back: %s", want, body)
		}
	}
}
