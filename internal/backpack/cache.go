package backpack

import (
	"context"
	"sync"
	"time"
)

// PanelRef is what is needed to reach a panel.
type PanelRef struct {
	ID        int64
	URL       string
	Token     string
	IgnoreTLS bool
}

// Snapshot is what a panel last said.
type Snapshot struct {
	PanelID   int64     `json:"panelId"`
	Tunnels   []Tunnel  `json:"tunnels"`
	System    *System   `json:"system"`
	FetchedAt time.Time `json:"fetchedAt"`
	LatencyMs float64   `json:"latencyMs"`
	Err       string    `json:"error,omitempty"`
}

// Find returns the named tunnel.
func (s *Snapshot) Find(name string) (*Tunnel, bool) {
	if s == nil {
		return nil, false
	}
	for i := range s.Tunnels {
		if s.Tunnels[i].Name == name {
			return &s.Tunnels[i], true
		}
	}
	return nil, false
}

// Cache holds one snapshot per panel and fetches at most once per panel at a
// time, so twenty tunnel monitors on one panel cost one request, not twenty.
type Cache struct {
	mu    sync.Mutex
	snaps map[int64]*Snapshot
	locks map[int64]*sync.Mutex
}

// NewCache returns an empty cache.
func NewCache() *Cache {
	return &Cache{snaps: map[int64]*Snapshot{}, locks: map[int64]*sync.Mutex{}}
}

func (c *Cache) lockFor(id int64) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.locks[id]
	if !ok {
		l = &sync.Mutex{}
		c.locks[id] = l
	}
	return l
}

// Peek returns the cached snapshot without fetching.
func (c *Cache) Peek(id int64) *Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snaps[id]
}

// Forget drops a panel's snapshot.
func (c *Cache) Forget(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.snaps, id)
}

// Get returns a snapshot no older than maxAge, fetching when needed.
func (c *Cache) Get(ctx context.Context, p PanelRef, maxAge time.Duration) *Snapshot {
	l := c.lockFor(p.ID)
	l.Lock()
	defer l.Unlock()
	if s := c.Peek(p.ID); s != nil && time.Since(s.FetchedAt) < maxAge {
		return s
	}
	s := Fetch(ctx, p)
	c.mu.Lock()
	c.snaps[p.ID] = s
	c.mu.Unlock()
	return s
}

// Fetch reads a panel now, without the cache.
func Fetch(ctx context.Context, p PanelRef) *Snapshot {
	cl := &Client{BaseURL: p.URL, Token: p.Token, IgnoreTLS: p.IgnoreTLS, Timeout: 20 * time.Second}
	s := &Snapshot{PanelID: p.ID, FetchedAt: time.Now()}

	var wg sync.WaitGroup
	var sys *System
	wg.Add(1)
	go func() {
		defer wg.Done()
		sys, _, _ = cl.System(ctx)
	}()
	tunnels, latency, err := cl.Tunnels(ctx)
	wg.Wait()

	s.LatencyMs = float64(latency.Microseconds()) / 1000
	if err != nil {
		s.Err = err.Error()
		return s
	}
	if tunnels == nil {
		tunnels = []Tunnel{}
	}
	s.Tunnels = tunnels
	s.System = sys
	return s
}
