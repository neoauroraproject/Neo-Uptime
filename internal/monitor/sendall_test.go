package monitor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
	"github.com/neoauroraproject/Neo-Uptime/internal/notify"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

type nopPub struct{}

func (nopPub) Publish(string, any) {}

// Alerts are sent from goroutines; they must still go out after sendAll returns.
func TestSendAllDelivers(t *testing.T) {
	got := make(chan string, 1)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- string(b)
	}))
	defer hook.Close()

	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	n := &store.Notification{Name: "hook", Type: "webhook", Active: true, Config: map[string]string{"url": hook.URL, "contentType": "json"}}
	if err := db.SaveNotification(ctx, n); err != nil {
		t.Fatal(err)
	}
	m := &store.Monitor{Name: "m", Type: TypeTCP, Active: true, Interval: 60, NotificationIDs: []int64{n.ID}, Config: store.MonitorConfig{Hostname: "h", Port: 1}}
	if err := db.CreateMonitor(ctx, m); err != nil {
		t.Fatal(err)
	}
	mg := NewManager(db, backpack.NewCache(), NewAgentHub(), nopPub{})
	mg.sendAll(m.ID, notify.Event{Kind: "status", MonitorName: "m", Status: store.StatusDown, Time: time.Now()})
	select {
	case body := <-got:
		if !strings.Contains(body, "DOWN") && !strings.Contains(body, "down") {
			t.Fatalf("unexpected body: %s", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the alert was never delivered")
	}
}
