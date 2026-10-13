package xray

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
)

// fakeOokla is a Speedtest.net in miniature: a server list that points at
// data servers answering as theirs do. Browser-like requests only, as the
// real ones demand.
func fakeOokla(t *testing.T, delays map[string]time.Duration, order ...string) (list string, hits func(name string) int) {
	t.Helper()
	zero := make([]byte, 64<<10)
	var mu sync.Mutex
	count := map[string]int{}
	hits = func(name string) int {
		mu.Lock()
		defer mu.Unlock()
		return count[name]
	}
	var entries []map[string]any
	for _, name := range order {
		name := name
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.UserAgent(), "Mozilla") {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			mu.Lock()
			count[name]++
			mu.Unlock()
			switch {
			case strings.HasSuffix(r.URL.Path, "/latency.txt"):
				time.Sleep(delays[name])
				_, _ = io.WriteString(w, "test=test\n")
			case strings.HasSuffix(r.URL.Path, "/random4000x4000.jpg"):
				for {
					if _, err := w.Write(zero); err != nil {
						return
					}
				}
			case strings.HasSuffix(r.URL.Path, "/upload.php"):
				n, _ := io.Copy(io.Discard, r.Body)
				_, _ = fmt.Fprintf(w, "size=%d", n)
			}
		}))
		t.Cleanup(srv.Close)
		entries = append(entries, map[string]any{"url": srv.URL + "/speedtest/upload.php", "host": strings.TrimPrefix(srv.URL, "http://"),
			"name": "Naaldwijk", "cc": "NL", "sponsor": "Sponsor " + name, "id": "1", "distance": 0})
	}
	body, _ := json.Marshal(entries)
	ls := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.UserAgent(), "Mozilla") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(ls.Close)
	return ls.URL, hits
}

// A server's own connection, measured without any config, with the nearest
// of the servers Speedtest.net lists: the one that answers fastest.
func TestSpeedDirectWithOokla(t *testing.T) {
	// The list comes nearest first, but the nearest is not the fastest.
	list, hits := fakeOokla(t, map[string]time.Duration{"slow": 120 * time.Millisecond, "slower": 250 * time.Millisecond}, "slow", "fast", "slower")
	t.Setenv("NEO_UPTIME_OOKLA_LIST", list)
	t.Setenv("NEO_UPTIME_SPEED_URL", "")
	oldW, oldD, oldU := speedWarm, speedDownFor, speedUpFor
	t.Cleanup(func() { speedWarm, speedDownFor, speedUpFor = oldW, oldD, oldU })
	speedWarm, speedDownFor, speedUpFor = 200*time.Millisecond, 1500*time.Millisecond, 1500*time.Millisecond

	phases := map[string]int{}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	sp, err := MeasureSpeed(ctx, nil, "", func(p agentproto.SpeedProgress) { phases[p.Phase]++ })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sp.Server, "Speedtest.net") || !strings.Contains(sp.Server, "Sponsor fast") || !strings.Contains(sp.Server, "NL") {
		t.Fatalf("server: %q", sp.Server)
	}
	if sp.Down < 5 || sp.Up < 5 || sp.Bytes < 1<<20 || sp.UpError != "" || phases["server"] < 1 || phases["ping"] < speedPings || phases["download"] < 1 || phases["upload"] < 1 {
		t.Fatalf("speed: %+v phases %v", sp, phases)
	}
	if hits("fast") < 10 || hits("slow") > 6 || hits("slower") > 6 {
		t.Fatalf("the fast server should carry the test: fast %d, slow %d, slower %d", hits("fast"), hits("slow"), hits("slower"))
	}
	t.Logf("%s: down %.0f up %.0f Mbps, ping %.1f ms", sp.Server, sp.Down, sp.Up, sp.Ping)
}

// When Speedtest.net cannot be reached, or the user asks for Cloudflare, the
// built-in servers are used instead.
func TestSpeedFallsBackFromOokla(t *testing.T) {
	t.Setenv("NEO_UPTIME_OOKLA_LIST", "http://127.0.0.1:1/servers") // nothing listens
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__down":
			if r.URL.Query().Get("bytes") == "1" || r.Header.Get("Range") != "" {
				_, _ = w.Write([]byte("x"))
				return
			}
			zero := make([]byte, 64<<10)
			for {
				if _, err := w.Write(zero); err != nil {
					return
				}
			}
		case "/__up":
			_, _ = io.Copy(io.Discard, r.Body)
		}
	}))
	defer own.Close()
	t.Setenv("NEO_UPTIME_SPEED_URL", own.URL)
	oldW, oldD, oldU := speedWarm, speedDownFor, speedUpFor
	t.Cleanup(func() { speedWarm, speedDownFor, speedUpFor = oldW, oldD, oldU })
	speedWarm, speedDownFor, speedUpFor = 200*time.Millisecond, time.Second, time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, provider := range []string{"", ProviderCloudflare} {
		sp, err := MeasureSpeed(ctx, nil, provider, nil)
		if err != nil || sp.Down < 5 || sp.Up < 5 || strings.Contains(sp.Server, "Speedtest.net") {
			t.Fatalf("provider %q: %+v %v", provider, sp, err)
		}
	}
}
