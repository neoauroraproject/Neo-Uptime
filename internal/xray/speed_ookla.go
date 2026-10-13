package xray

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// Speed test providers.
const (
	ProviderOokla      = "ookla"      // Speedtest.net's servers, the nearest ones to where the test runs
	ProviderCloudflare = "cloudflare" // speed.cloudflare.com
)

// ooklaListURL is where Speedtest.net lists the servers near the requester's
// address, which — asked through a config — is the config's exit. "off" turns
// the lookup off (tests do).
func ooklaListURL() string {
	if v := strings.TrimSpace(os.Getenv("NEO_UPTIME_OOKLA_LIST")); v != "" {
		return v
	}
	return "https://www.speedtest.net/api/js/servers?engine=js&limit=10"
}

type ooklaEntry struct {
	URL     string `json:"url"`
	Host    string `json:"host"`
	Name    string `json:"name"`
	CC      string `json:"cc"`
	Sponsor string `json:"sponsor"`
}

const ooklaUpChunk = 6 << 20

// ooklaServers finds Speedtest.net servers near where the test runs and
// puts them in order of how fast they answer: the list comes by distance,
// but a near server can be a slow one. It speaks the plain HTTP protocol of
// Speedtest.net's servers: latency.txt, random4000x4000.jpg and upload.php.
func ooklaServers(ctx context.Context, client *http.Client) ([]speedServer, error) {
	list := ooklaListURL()
	if list == "off" {
		return nil, errors.New("Speedtest.net is turned off")
	}
	lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(lctx, http.MethodGet, list, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("speedtest.net answered HTTP %d", resp.StatusCode)
	}
	var entries []ooklaEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("speedtest.net server list: %w", err)
	}

	type cand struct {
		srv     speedServer
		latency float64
	}
	// Probe the nearest few at once. Slow ones are not waited for: once the
	// first has answered, the others get a moment, then the best so far wins.
	pctx, pcancel := context.WithTimeout(ctx, 6*time.Second)
	defer pcancel()
	answers := make(chan cand, 4)
	started := 0
	for _, e := range entries {
		if started == 4 {
			break
		}
		u, err := url.Parse(e.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || !strings.Contains(e.URL, "/") {
			continue
		}
		started++
		base := e.URL[:strings.LastIndex(e.URL, "/")+1] // …/speedtest/
		name := "Speedtest.net · " + strings.TrimSpace(e.Sponsor)
		if place := strings.TrimSpace(strings.Trim(e.Name+", "+e.CC, ", ")); place != "" {
			name += " — " + place
		}
		srv := speedServer{
			name:    name,
			ping:    func() string { return fmt.Sprintf("%slatency.txt?x=%d", base, rand.Int64()) },
			down:    func(int64) string { return fmt.Sprintf("%srandom4000x4000.jpg?x=%d", base, rand.Int64()) },
			up:      e.URL,
			upChunk: ooklaUpChunk,
		}
		go func() {
			best := -1.0
			for range 2 { // the first request also opens the connection
				d, err := smallRequest(pctx, client, srv.pingURL())
				if err != nil {
					answers <- cand{latency: -1}
					return
				}
				if best < 0 || d < best {
					best = d
				}
			}
			answers <- cand{srv, best}
		}()
	}
	var found []cand
	var grace <-chan time.Time
	for got := 0; got < started; {
		select {
		case c := <-answers:
			got++
			if c.latency >= 0 {
				found = append(found, c)
				if grace == nil {
					wait := time.Duration(c.latency*2) * time.Millisecond
					grace = time.After(min(max(wait, 400*time.Millisecond), 1500*time.Millisecond))
				}
			}
		case <-grace:
			got = started
		case <-pctx.Done():
			got = started
		}
	}
	if len(found) == 0 {
		return nil, errors.New("no Speedtest.net server answered")
	}
	sort.Slice(found, func(i, j int) bool { return found[i].latency < found[j].latency })
	var out []speedServer
	for i, c := range found {
		if i == 3 {
			break
		}
		out = append(out, c.srv)
	}
	return out, nil
}
