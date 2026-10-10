package monitor

import (
	"testing"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

func TestStatusAccepted(t *testing.T) {
	cases := []struct {
		code     int
		accepted []string
		want     bool
	}{
		{200, nil, true},
		{204, []string{"200-299"}, true},
		{301, []string{"200-299"}, false},
		{301, []string{"200-299", "300-399"}, true},
		{401, []string{"401"}, true},
		{503, []string{"5xx"}, true},
		{404, []string{"5xx"}, false},
	}
	for _, c := range cases {
		if got := statusAccepted(c.code, c.accepted); got != c.want {
			t.Errorf("statusAccepted(%d, %v) = %v, want %v", c.code, c.accepted, got, c.want)
		}
	}
}

func TestJSONQuery(t *testing.T) {
	doc := []byte(`{"status":"ok","data":{"items":[{"n":3},{"n":7}],"healthy":true},"latency":12.5}`)
	cases := []struct {
		path, op, exp string
		want          bool
	}{
		{"status", "==", "ok", true},
		{"$.status", "!=", "down", true},
		{"data.items[1].n", ">", "5", true},
		{"data.items[-1].n", "==", "7", true},
		{"data.items.length", "==", "2", true},
		{"data.healthy", "==", "true", true},
		{"latency", "<=", "10", false},
		{"missing.key", "exists", "", false},
		{"data", "exists", "", true},
		{"status", "contains", "o", true},
	}
	for _, c := range cases {
		got, _, err := evalJSONQuery(doc, c.path, c.op, c.exp)
		if err != nil {
			t.Errorf("%s %s %s: %v", c.path, c.op, c.exp, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s %s %s = %v, want %v", c.path, c.op, c.exp, got, c.want)
		}
	}
	if _, _, err := evalJSONQuery([]byte("not json"), "a", "==", "b"); err == nil {
		t.Error("expected an error for a non-JSON body")
	}
}

func TestIsImportant(t *testing.T) {
	hb := func(s int) *store.Heartbeat { return &store.Heartbeat{Status: s} }
	cases := []struct {
		prev *store.Heartbeat
		now  int
		want bool
	}{
		{nil, store.StatusUp, true},
		{hb(store.StatusUp), store.StatusUp, false},
		{hb(store.StatusUp), store.StatusDown, true},
		{hb(store.StatusDown), store.StatusUp, true},
		{hb(store.StatusUp), store.StatusPending, false},
		{hb(store.StatusPending), store.StatusUp, false},
		{hb(store.StatusPending), store.StatusDown, true},
		{hb(store.StatusUp), store.StatusMaintenance, true},
		{hb(store.StatusMaintenance), store.StatusUp, true},
	}
	for _, c := range cases {
		if got := isImportant(c.prev, c.now); got != c.want {
			t.Errorf("isImportant(%v -> %d) = %v, want %v", c.prev, c.now, got, c.want)
		}
	}
	if shouldNotify(hb(store.StatusMaintenance), store.StatusUp) {
		t.Error("coming back from maintenance should not page anyone")
	}
	if !shouldNotify(nil, store.StatusDown) || shouldNotify(nil, store.StatusUp) {
		t.Error("a brand-new monitor should only notify when it starts down")
	}
}

func TestMaintenanceWindows(t *testing.T) {
	utc := time.UTC
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, utc) }

	single := &store.Maintenance{Strategy: "single", Active: true, StartAt: at(2026, 1, 10, 2, 0).Unix(), EndAt: at(2026, 1, 10, 4, 0).Unix()}
	if s, _, _ := MaintenanceStatus(single, at(2026, 1, 10, 1, 0)); s != MaintScheduled {
		t.Errorf("before window: %s", s)
	}
	if s, _, _ := MaintenanceStatus(single, at(2026, 1, 10, 3, 0)); s != MaintUnder {
		t.Errorf("inside window: %s", s)
	}
	if s, _, _ := MaintenanceStatus(single, at(2026, 1, 10, 5, 0)); s != MaintEnded {
		t.Errorf("after window: %s", s)
	}

	// Saturdays 23:00 → 01:00, crossing midnight. 2026-01-10 is a Saturday.
	weekly := &store.Maintenance{Strategy: "weekly", Active: true, Weekdays: []int{6}, TimeFrom: "23:00", TimeTo: "01:00", Timezone: "UTC"}
	if s, _, _ := MaintenanceStatus(weekly, at(2026, 1, 10, 23, 30)); s != MaintUnder {
		t.Errorf("Saturday 23:30: %s", s)
	}
	if s, _, _ := MaintenanceStatus(weekly, at(2026, 1, 11, 0, 30)); s != MaintUnder {
		t.Errorf("Sunday 00:30 (window opened Saturday): %s", s)
	}
	if s, next, _ := MaintenanceStatus(weekly, at(2026, 1, 11, 2, 0)); s != MaintScheduled || !next.Equal(at(2026, 1, 17, 23, 0)) {
		t.Errorf("Sunday 02:00: %s next %v", s, next)
	}

	monthly := &store.Maintenance{Strategy: "monthly", Active: true, DaysOfMonth: []int{-1}, TimeFrom: "03:00", TimeTo: "04:00", Timezone: "UTC"}
	if s, _, _ := MaintenanceStatus(monthly, at(2026, 2, 28, 3, 30)); s != MaintUnder {
		t.Errorf("last day of February: %s", s)
	}

	paused := &store.Maintenance{Strategy: "manual", Active: false}
	if s, _, _ := MaintenanceStatus(paused, time.Now()); s != MaintInactive {
		t.Errorf("paused manual: %s", s)
	}
}
