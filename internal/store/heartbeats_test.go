package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRecentHeartbeatsAndPrune(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	var ids []int64
	for i := 0; i < 3; i++ {
		m := &Monitor{Name: "m", Type: "http", Active: true, Interval: 60, NotificationIDs: []int64{}}
		if err := db.CreateMonitor(ctx, m); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
	}
	now := time.Now()
	for i, id := range ids[:2] {
		for k := 0; k < 50+i; k++ {
			at := now.Add(-time.Duration(k) * time.Hour)
			if k >= 40 {
				at = now.AddDate(0, 0, -(10 + k))
			}
			if err := db.InsertHeartbeat(ctx, &Heartbeat{MonitorID: id, Status: StatusUp, Time: at.UnixMilli()}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, sel := range [][]int64{nil, ids} {
		got, err := db.RecentHeartbeats(ctx, sel, 5)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || len(got[ids[0]]) != 5 || len(got[ids[1]]) != 5 || got[ids[2]] != nil {
			t.Fatalf("recent: %v", got)
		}
		l := got[ids[0]]
		for i := 1; i < len(l); i++ {
			if l[i].Time <= l[i-1].Time {
				t.Fatal("not oldest first")
			}
		}
		if l[len(l)-1].Time != now.UnixMilli() {
			t.Fatal("newest beat missing")
		}
	}

	// An aggregate of a deleted monitor is dropped by Prune.
	if _, err := db.w.Exec(`INSERT INTO stat_daily (monitor_id, ts, up) VALUES (9999, 0, 1)`); err != nil {
		t.Fatal(err)
	}
	n, err := db.Prune(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if n != 10+11 {
		t.Fatalf("pruned %d", n)
	}
	var left int
	db.r.QueryRow(`SELECT COUNT(*) FROM stat_daily WHERE monitor_id = 9999`).Scan(&left)
	if left != 0 {
		t.Fatal("orphan aggregate kept")
	}
}
