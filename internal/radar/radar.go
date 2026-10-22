// Package radar reads Chabokan's internet radar (radar.chabokan.net): from
// datacenters in Iran and abroad, it checks well-known services every
// minute. The panel fetches it every few minutes while it is turned on,
// keeps a small summary in memory and hands that to status pages, so
// visitors never wait on, or talk to, the radar themselves. Nothing of it is
// written to the database.
package radar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SourceURL is the radar's data; NEO_UPTIME_RADAR_URL points elsewhere (tests).
func SourceURL() string {
	if v := strings.TrimSpace(os.Getenv("NEO_UPTIME_RADAR_URL")); v != "" {
		return v
	}
	return "https://radar.chabokan.net/api/dashboard/?hours=1"
}

// Every is how often the radar is read while it is on.
var Every = 5 * time.Minute

const (
	buckets   = 12              // the last hour, in 5-minute steps
	bucketLen = 5 * time.Minute //
	staleFor  = 15 * time.Minute
)

// domestic are the radar's Iranian services (by slug). The rest are
// foreign: whether they answer from a datacenter in Iran is whether that
// datacenter has the international internet.
var domestic = map[string]bool{
	"aparat": true, "bale": true, "cafebazaar": true, "digikala": true, "divar": true,
	"snapp-taxi": true, "snapp": true, "torob": true, "varzesh3": true, "virgool": true, "zoomit": true,
	"eitaa": true, "rubika": true, "filimo": true, "namava": true, "shad": true, "soroush": true,
}

func isDomestic(slug, name string) bool {
	s := strings.ToLower(slug)
	if domestic[s] {
		return true
	}
	n := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	return domestic[n] || strings.HasSuffix(s, ".ir")
}

// Datacenter is how one of the radar's vantage points sees the internet now.
type Datacenter struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Internal bool   `json:"internal"` // in Iran
	// State is about what matters from there: from Iran the foreign
	// services, from abroad the Iranian ones. ok | partial | down | unknown.
	State   string  `json:"state"`
	Up      int     `json:"up"`    // of those services, answering now
	Total   int     `json:"total"` //
	Latency float64 `json:"latency"`
	// Down names the services not answering now.
	Down []string `json:"down,omitempty"`
	// History: the share of checks that passed in each 5 minutes of the
	// last hour, oldest first; -1 where there was no data.
	History []float64 `json:"history"`
	At      int64     `json:"at"` // last check, unix ms
}

// Snapshot is the radar as last read.
type Snapshot struct {
	At          int64        `json:"at"` // when it was read, unix ms
	Source      string       `json:"source"`
	Link        string       `json:"link"`
	Datacenters []Datacenter `json:"datacenters"`
	Error       string       `json:"error,omitempty"`
}

// Service reads the radar while enabled() says so.
type Service struct {
	enabled func() bool
	client  *http.Client
	kick    chan struct{}

	mu   sync.RWMutex
	snap *Snapshot
}

// New returns a service; Run starts it.
func New(enabled func() bool) *Service {
	return &Service{enabled: enabled, client: &http.Client{Timeout: 45 * time.Second}, kick: make(chan struct{}, 1)}
}

// Kick asks for a read now (the setting was just turned on).
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Snapshot is the last summary, or nil while there is none. A summary much
// older than the reading pace is dropped, so a stuck radar does not show
// old news as current.
func (s *Service) Snapshot() *Snapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snap == nil || time.Since(time.UnixMilli(s.snap.At)) > 3*Every+staleFor {
		return nil
	}
	return s.snap
}

// Run reads the radar every Every while it is enabled, until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(Every)
	defer t.Stop()
	read := func() {
		if !s.enabled() {
			s.mu.Lock()
			s.snap = nil // off: forget it, keep nothing
			s.mu.Unlock()
			return
		}
		rctx, cancel := context.WithTimeout(ctx, time.Minute)
		snap, err := Fetch(rctx, s.client, SourceURL(), time.Now())
		cancel()
		s.mu.Lock()
		defer s.mu.Unlock()
		if err != nil {
			log.Printf("radar: %v", err)
			if s.snap != nil {
				c := *s.snap // keep the last good summary while it is fresh enough
				c.Error = err.Error()
				s.snap = &c
			}
			return
		}
		s.snap = snap
	}
	read()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.kick:
		}
		read()
	}
}

// ---------- reading and summing up ----------

type rawAgent struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Location string `json:"location"`
	Internal bool   `json:"is_internal"`
	Online   bool   `json:"is_online"`
	Order    int    `json:"dashboard_order"`
}

type rawTarget struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type rawPoint struct {
	At      time.Time `json:"timestamp"`
	Latency float64   `json:"latency"`
	Up      bool      `json:"is_up"`
}

type raw struct {
	Agents  []rawAgent                       `json:"agents"`
	Targets []rawTarget                      `json:"targets"`
	Stats   map[string]map[string][]rawPoint `json:"stats"`
}

// Fetch reads the radar and sums it up as of now.
func Fetch(ctx context.Context, client *http.Client, url string, now time.Time) (*Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Neo-Uptime radar (+https://github.com/neoauroraproject/Neo-Uptime)")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("radar answered HTTP %d", resp.StatusCode)
	}
	var r raw
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("radar data: %w", err)
	}
	if len(r.Agents) == 0 || len(r.Targets) == 0 {
		return nil, errors.New("the radar sent no datacenters")
	}
	return Summarize(&r, now), nil
}

// Summarize turns the radar's raw checks into a summary per datacenter.
func Summarize(r *raw, now time.Time) *Snapshot {
	snap := &Snapshot{At: now.UnixMilli(), Source: "radar.chabokan.net", Link: "https://radar.chabokan.net/", Datacenters: []Datacenter{}}
	agents := append([]rawAgent(nil), r.Agents...)
	sort.SliceStable(agents, func(i, j int) bool {
		if agents[i].Internal != agents[j].Internal {
			return agents[i].Internal
		}
		return agents[i].Order < agents[j].Order
	})
	start := now.Add(-buckets * bucketLen)
	// A service no datacenter on that side reached all hour is blocked
	// (filtered), not down: it would only drag every datacenter's state.
	reached := map[bool]map[int]bool{true: {}, false: {}}
	for _, a := range agents {
		for _, t := range r.Targets {
			for _, p := range r.Stats[strconv.Itoa(a.ID)][strconv.Itoa(t.ID)] {
				if p.Up && !p.At.Before(start) {
					reached[a.Internal][t.ID] = true
					break
				}
			}
		}
	}
	for _, a := range agents {
		dc := Datacenter{Slug: a.Slug, Name: a.Name, Location: a.Location, Internal: a.Internal, History: make([]float64, buckets)}
		var passed, checked [buckets]int
		var latSum float64
		var latN int
		var last time.Time
		for _, t := range r.Targets {
			// From Iran what counts is the world; from abroad, Iran.
			if isDomestic(t.Slug, t.Name) == a.Internal || !reached[a.Internal][t.ID] {
				continue
			}
			pts := r.Stats[strconv.Itoa(a.ID)][strconv.Itoa(t.ID)]
			if len(pts) == 0 {
				continue
			}
			sort.Slice(pts, func(i, j int) bool { return pts[i].At.Before(pts[j].At) })
			for _, p := range pts {
				if p.At.Before(start) || p.At.After(now) {
					continue
				}
				b := int(p.At.Sub(start) / bucketLen)
				if b >= buckets {
					b = buckets - 1
				}
				checked[b]++
				if p.Up {
					passed[b]++
				}
			}
			lp := pts[len(pts)-1]
			if now.Sub(lp.At) > staleFor {
				continue // this service has no recent check from here
			}
			if lp.At.After(last) {
				last = lp.At
			}
			dc.Total++
			// Down only when the last two checks failed: one lost check is noise.
			down := !lp.Up && (len(pts) < 2 || !pts[len(pts)-2].Up)
			if down {
				dc.Down = append(dc.Down, t.Name)
			} else {
				dc.Up++
				if lp.Up && lp.Latency > 0 {
					latSum += lp.Latency
					latN++
				}
			}
		}
		for i := range dc.History {
			if checked[i] == 0 {
				dc.History[i] = -1
			} else {
				dc.History[i] = math.Round(float64(passed[i])/float64(checked[i])*100) / 100
			}
		}
		if latN > 0 {
			dc.Latency = math.Round(latSum / float64(latN))
		}
		if !last.IsZero() {
			dc.At = last.UnixMilli()
		}
		switch {
		case !a.Online || dc.Total == 0:
			dc.State = "unknown"
		case float64(dc.Up) >= 0.8*float64(dc.Total):
			dc.State = "ok"
		case float64(dc.Up) >= 0.3*float64(dc.Total):
			dc.State = "partial"
		default:
			dc.State = "down"
		}
		sort.Strings(dc.Down)
		snap.Datacenters = append(snap.Datacenters, dc)
	}
	return snap
}

// Pick returns the summary for the datacenters a page shows (slugs; none
// means every one), with an overall state over the ones in Iran.
func (s *Snapshot) Pick(slugs []string) *Snapshot {
	if s == nil {
		return nil
	}
	c := *s
	if len(slugs) > 0 {
		want := map[string]bool{}
		for _, v := range slugs {
			want[v] = true
		}
		c.Datacenters = nil
		for _, d := range s.Datacenters {
			if want[d.Slug] {
				c.Datacenters = append(c.Datacenters, d)
			}
		}
	}
	if len(c.Datacenters) == 0 {
		return nil
	}
	return &c
}
