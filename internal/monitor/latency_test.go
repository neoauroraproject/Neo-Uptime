package monitor

import (
	"testing"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

func TestLatencyEvent(t *testing.T) {
	mg := &Manager{}
	m := &store.Monitor{Config: store.MonitorConfig{LatencyAlertMs: 200, LatencyAlertFor: 120}}
	st := &State{}
	start := time.Unix(1_800_000_000, 0)
	beat := func(status int, ping float64) *store.Heartbeat {
		return &store.Heartbeat{Status: status, Ping: &ping}
	}
	step := func(sec int, hb *store.Heartbeat) *latencyChange {
		return mg.latencyEvent(m, st, hb, start.Add(time.Duration(sec)*time.Second))
	}

	if ev := step(0, beat(store.StatusUp, 90)); ev != nil {
		t.Fatalf("fast check raised %+v", ev)
	}
	if ev := step(20, beat(store.StatusUp, 350)); ev != nil {
		t.Fatalf("alert before the hold time: %+v", ev)
	}
	if ev := step(100, beat(store.StatusUp, 400)); ev != nil {
		t.Fatalf("alert before the hold time: %+v", ev)
	}
	ev := step(140, beat(store.StatusUp, 380))
	if ev == nil || ev.kind != "latency" || ev.slowFor != 120*time.Second {
		t.Fatalf("want a latency alert after 2 minutes, got %+v", ev)
	}
	if ev := step(160, beat(store.StatusUp, 500)); ev != nil {
		t.Fatalf("alert sent twice: %+v", ev)
	}
	ev = step(180, beat(store.StatusUp, 120))
	if ev == nil || ev.kind != "latency-ok" {
		t.Fatalf("want a recovery, got %+v", ev)
	}
	if ev := step(200, beat(store.StatusUp, 130)); ev != nil {
		t.Fatalf("recovery sent twice: %+v", ev)
	}

	// A short spike, or slowness broken by a down check, starts the clock again.
	step(220, beat(store.StatusUp, 300))
	step(300, beat(store.StatusDown, 0))
	if ev := step(330, beat(store.StatusUp, 300)); ev != nil {
		t.Fatalf("clock should restart after a down check: %+v", ev)
	}
	if ev := step(460, beat(store.StatusUp, 300)); ev == nil || ev.kind != "latency" {
		t.Fatalf("want an alert 2 minutes after the restart, got %+v", ev)
	}

	// Off: nothing.
	off := &store.Monitor{}
	if ev := mg.latencyEvent(off, &State{}, beat(store.StatusUp, 9999), start); ev != nil {
		t.Fatalf("alert with the limit off: %+v", ev)
	}
	// Right away: the first slow check alerts.
	now := &store.Monitor{Config: store.MonitorConfig{LatencyAlertMs: 100}}
	if ev := mg.latencyEvent(now, &State{}, beat(store.StatusUp, 150), start); ev == nil || ev.kind != "latency" {
		t.Fatalf("want an immediate alert, got %+v", ev)
	}
}
