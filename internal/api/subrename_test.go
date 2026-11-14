//go:build !race

// Config monitors run Xray-core, which the race detector's checkptr mode
// rejects; CI runs this in a plain pass.

package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

// Providers write the traffic or days left into a config's name, so the
// name changes on every fetch. The config must keep its monitor; monitors
// that older versions made again for each new name are merged back.
func TestSubscriptionRenamesKeepMonitors(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)
	const uuid = "b831381d-6324-4d53-ad4f-8cda48b30811"
	de := func(name string) string {
		return fmt.Sprintf("vless://%s@127.0.0.1:1?type=ws&security=tls&sni=de.example.com&path=%%2Fws#%s", uuid, name)
	}
	nl := func(name string) string {
		return fmt.Sprintf(`vmess://%s`, base64.StdEncoding.EncodeToString([]byte(
			fmt.Sprintf(`{"v":"2","ps":%q,"add":"127.0.0.2","port":"1","id":%q,"net":"tcp","tls":""}`, name, uuid))))
	}
	// The same server twice under two names stays two monitors.
	same := func(name string) string { return fmt.Sprintf("trojan://pw@127.0.0.3:1?security=none#%s", name) }

	var mu sync.Mutex
	var content string
	set := func(links ...string) {
		mu.Lock()
		defer mu.Unlock()
		content = ""
		for _, l := range links {
			content += l + "\n"
		}
	}
	sub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = io.WriteString(w, base64.StdEncoding.EncodeToString([]byte(content)))
	}))
	defer sub.Close()

	set(de("%F0%9F%87%A9%F0%9F%87%AA%20DE%20%7C%2010.5%20GB%20left"), nl("NL | 30 days"), same("Same A"), same("Same B"))
	code, out := h.long("POST", "/api/subscriptions", fmt.Sprintf(`{"name":"Shop","url":%q,"interval":3600}`, sub.URL))
	if code != 200 || out["result"] == nil {
		t.Fatalf("create: %d %v", code, out)
	}
	if r := out["result"].(map[string]any); r["added"] != float64(4) {
		t.Fatalf("first sync: %v", r)
	}
	subID := int(out["subscription"].(map[string]any)["id"].(float64))
	refresh := func() map[string]any {
		t.Helper()
		code, out := h.long("POST", fmt.Sprintf("/api/subscriptions/%d/refresh", subID), `{}`)
		if code != 200 {
			t.Fatalf("refresh: %d %v", code, out)
		}
		return out["result"].(map[string]any)
	}
	configs := func() []string {
		t.Helper()
		_, d := h.do("GET", "/api/dashboard", "", false)
		var names []string
		for _, m := range d["monitors"].([]any) {
			m := m.(map[string]any)
			if m["type"] == "xray" {
				names = append(names, fmt.Sprintf("%s/%v", m["name"], m["active"]))
			}
		}
		sort.Strings(names)
		return names
	}
	ids0 := map[string]bool{}
	_, d := h.do("GET", "/api/dashboard", "", false)
	for _, m := range d["monitors"].([]any) {
		ids0[fmt.Sprint(m.(map[string]any)["id"])] = true
	}

	// Every config is renamed.
	for i := 0; i < 3; i++ {
		set(de(fmt.Sprintf("%%F0%%9F%%87%%A9%%F0%%9F%%87%%AA%%20DE%%20%%7C%%20%d.1%%20GB%%20left", 9-i)), nl(fmt.Sprintf("NL | %d days", 29-i)), same("Same A"), same(fmt.Sprintf("Same B%d", i)))
		r := refresh()
		if r["added"] != float64(0) || r["removed"] != float64(0) || r["updated"] != float64(3) {
			t.Fatalf("rename %d: %v", i, r)
		}
	}
	got := configs()
	want := []string{"DE | 7.1 GB left/true", "NL | 27 days/true", "Same A/true", "Same B2/true"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("after renames: %v, want %v", got, want)
	}
	_, d = h.do("GET", "/api/dashboard", "", false)
	for _, m := range d["monitors"].([]any) {
		if id := fmt.Sprint(m.(map[string]any)["id"]); !ids0[id] {
			t.Fatalf("monitor %s is new", id)
		}
	}

	// A name typed by hand stays.
	var deID int64
	all, _ := h.app.db.ListMonitors(context.Background())
	for _, m := range all {
		if m.Name == "DE | 7.1 GB left" {
			deID = m.ID
			m.Name = "Germany"
			if err := h.app.db.UpdateMonitor(context.Background(), m); err != nil {
				t.Fatal(err)
			}
		}
	}
	set(de("DE%20%7C%201%20GB%20left"), nl("NL | 27 days"), same("Same A"), same("Same B2"))
	if r := refresh(); r["added"] != float64(0) {
		t.Fatalf("after hand rename: %v", r)
	}
	if m, _ := h.app.db.GetMonitor(context.Background(), deID); m.Name != "Germany" || m.Config.ConfigName != "DE | 1 GB left" {
		t.Fatalf("typed name: %q, config name %q", m.Name, m.Config.ConfigName)
	}

	// Duplicates an older version left behind (paused, with history) are
	// merged into the oldest monitor of their config.
	ctx := context.Background()
	orig, _ := h.app.db.GetMonitor(ctx, deID)
	var dupIDs []int64
	for i := 0; i < 3; i++ {
		dup := *orig
		dup.ID = 0
		dup.Name = fmt.Sprintf("DE | %d GB left", 20+i)
		dup.Config.ConfigName = dup.Name
		dup.Config.Gone, dup.Active = true, false
		dup.NotificationIDs = []int64{}
		if err := h.app.db.CreateMonitor(ctx, &dup); err != nil {
			t.Fatal(err)
		}
		dupIDs = append(dupIDs, dup.ID)
		ping := 12.0
		if err := h.app.db.InsertHeartbeat(ctx, &store.Heartbeat{MonitorID: dup.ID, Status: store.StatusUp, Time: time.Now().Add(-time.Duration(i+1) * time.Hour).UnixMilli(), Ping: &ping}); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := h.app.db.RecentHeartbeats(ctx, []int64{deID}, 100)
	r := refresh()
	if r["merged"] != float64(3) || r["added"] != float64(0) {
		t.Fatalf("merge: %v", r)
	}
	if n := len(configs()); n != 4 {
		t.Fatalf("configs after merge: %v", configs())
	}
	for _, id := range dupIDs {
		if _, err := h.app.db.GetMonitor(ctx, id); err == nil {
			t.Fatalf("duplicate %d still there", id)
		}
	}
	after, _ := h.app.db.RecentHeartbeats(ctx, []int64{deID}, 100)
	if len(after[deID]) != len(before[deID])+3 {
		t.Fatalf("history not merged: %d before, %d after", len(before[deID]), len(after[deID]))
	}

	// A config the user skipped stays skipped when it is renamed.
	code, out = h.long("POST", "/api/subscriptions", fmt.Sprintf(`{"name":"Shop2","url":%q,"interval":3600,"keys":["Same A"],"skip":["NL | 27 days"]}`, sub.URL))
	if code != 200 {
		t.Fatalf("second sub: %d %v", code, out)
	}
	sub2 := int(out["subscription"].(map[string]any)["id"].(float64))
	set(de("x"), nl("NL | 26 days"), same("Same A"), same("Same B2"))
	_, out = h.long("POST", fmt.Sprintf("/api/subscriptions/%d/refresh", sub2), `{}`)
	all, _ = h.app.db.ListMonitors(ctx)
	for _, m := range all {
		if m.Config.SubscriptionID == int64(sub2) && m.Config.ConfigName == "NL | 26 days" {
			t.Fatalf("skipped config came back under its new name: %v", out)
		}
	}
}
