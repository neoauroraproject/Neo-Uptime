// Package agent is neo-uptime-agent: it runs on a server that hosts BackPack
// or Kariz tunnels, reads their state locally — no panel, no token on that
// side — and pushes it, with the host's own vitals, to a Neo Uptime server.
package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
	"github.com/neoauroraproject/Neo-Uptime/internal/version"
)

// Config is how the agent is set up.
type Config struct {
	Server   string        // Neo Uptime base URL
	Token    string        // agent token from Neo Uptime
	Interval time.Duration // between reports
	Insecure bool          // accept a self-signed Neo Uptime certificate

	BackpackDir      string // /etc/backpack
	BackpackAPI      string // optional: read BackPack's panel API instead of its files
	BackpackToken    string
	BackpackInsecure bool

	KarizDir string // /etc/kariz

	NoPing bool // do not ping tunnel peers
}

// Agent collects and reports.
type Agent struct {
	cfg    Config
	sys    *systemSampler
	client *http.Client

	mu      sync.Mutex
	lastRTT time.Duration
}

// New returns an agent for cfg.
func New(cfg Config) *Agent {
	if cfg.Interval < 5*time.Second {
		cfg.Interval = 20 * time.Second
	}
	if cfg.BackpackDir == "" {
		cfg.BackpackDir = "/etc/backpack"
	}
	if cfg.KarizDir == "" {
		cfg.KarizDir = "/etc/kariz"
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.Insecure} //nolint:gosec // operator's choice
	return &Agent{
		cfg:    cfg,
		sys:    newSystemSampler(),
		client: &http.Client{Transport: tr, Timeout: 25 * time.Second},
	}
}

// Collect builds one report.
func (a *Agent) Collect(ctx context.Context) *agentproto.Report {
	r := &agentproto.Report{
		Format:       agentproto.Version,
		AgentVersion: version.Version,
		Time:         time.Now().UnixMilli(),
		Interval:     int(a.cfg.Interval / time.Second),
	}
	a.mu.Lock()
	r.RTTMs = float64(a.lastRTT.Microseconds()) / 1000
	a.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); r.System = a.sys.sample() }()
	go func() { defer wg.Done(); r.Backpack = collectBackpack(ctx, a.cfg) }()
	go func() { defer wg.Done(); r.Kariz = collectKariz(ctx, a.cfg) }()
	wg.Wait()
	return r
}

// Send pushes a report and returns the server's answer.
func (a *Agent) Send(ctx context.Context, r *agentproto.Report) (*agentproto.Ack, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(a.cfg.Server, "/") + "/api/agent/report"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	req.Header.Set("User-Agent", "neo-uptime-agent/"+version.Version)
	start := time.Now()
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rtt := time.Since(start)
	a.mu.Lock()
	a.lastRTT = rtt
	a.mu.Unlock()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var ack agentproto.Ack
	_ = json.Unmarshal(data, &ack)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return &ack, errors.New("the server refused the token — copy it again from Neo Uptime → Servers")
	case resp.StatusCode != http.StatusOK:
		msg := ack.Error
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		return &ack, fmt.Errorf("server answered HTTP %d: %s", resp.StatusCode, msg)
	}
	return &ack, nil
}

// Run reports until ctx ends.
func (a *Agent) Run(ctx context.Context) {
	log.Printf("neo-uptime-agent %s reporting to %s every %s", version.Version, a.cfg.Server, a.cfg.Interval)
	failures := 0
	interval := a.cfg.Interval
	for {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		r := a.Collect(cctx)
		ack, err := a.Send(cctx, r)
		cancel()
		switch {
		case err != nil:
			failures++
			if failures == 1 || failures%30 == 0 {
				log.Printf("report failed (%d in a row): %v", failures, err)
			}
		default:
			if failures > 0 {
				log.Printf("reporting again after %d failed attempts", failures)
			}
			failures = 0
			if ack.Interval >= 5 && ack.Interval <= 600 {
				interval = time.Duration(ack.Interval) * time.Second
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
