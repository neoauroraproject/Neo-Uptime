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
	Server   string        // Neo Uptime base URL; several, comma-separated, are tried in turn
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
	good    int // index of the address that answered last
}

// errRefused is a token problem: every address leads to the same Neo Uptime,
// so trying the next one would not help.
var errRefused = errors.New("the server refused the token — copy it again from Neo Uptime → Servers")

// Servers lists the addresses reports go to, in the order they are tried
// first: for example a tunnel's local port, then the IP, then a domain.
func (a *Agent) Servers() []string {
	var out []string
	for _, s := range strings.FieldsFunc(a.cfg.Server, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if s = strings.TrimRight(strings.TrimSpace(s), "/"); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Current is the address that worked last.
func (a *Agent) Current() string {
	list := a.Servers()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.good < len(list) {
		return list[a.good]
	}
	return ""
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

// Send pushes a report and returns the server's answer. With several
// addresses it starts with the one that worked last and moves on when one
// cannot be reached.
func (a *Agent) Send(ctx context.Context, r *agentproto.Report) (*agentproto.Ack, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	list := a.Servers()
	if len(list) == 0 {
		return nil, errors.New("no Neo Uptime address is set")
	}
	a.mu.Lock()
	first := a.good % len(list)
	a.mu.Unlock()
	var errs []string
	for n := 0; n < len(list); n++ {
		i := (first + n) % len(list)
		tctx, cancel := ctx, context.CancelFunc(func() {})
		if len(list) > 1 {
			tctx, cancel = context.WithTimeout(ctx, 12*time.Second)
		}
		ack, err := a.sendTo(tctx, list[i], body)
		cancel()
		if err == nil {
			a.mu.Lock()
			switched := a.good != i
			a.good = i
			a.mu.Unlock()
			if switched && len(list) > 1 {
				log.Printf("reporting through %s", list[i])
			}
			return ack, nil
		}
		if errors.Is(err, errRefused) || ctx.Err() != nil {
			return ack, err
		}
		if len(list) == 1 {
			return ack, err
		}
		errs = append(errs, list[i]+": "+err.Error())
	}
	return nil, errors.New(strings.Join(errs, "; "))
}

func (a *Agent) sendTo(ctx context.Context, server string, body []byte) (*agentproto.Ack, error) {
	url := server + "/api/agent/report"
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
		return &ack, errRefused
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
	log.Printf("neo-uptime-agent %s reporting to %s every %s", version.Version, strings.Join(a.Servers(), " or "), a.cfg.Interval)
	failures := 0
	interval := a.cfg.Interval
	for {
		cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		r := a.Collect(cctx)
		cancel()
		sctx, cancel := context.WithTimeout(ctx, time.Duration(min(len(a.Servers()), 4))*15*time.Second+5*time.Second)
		ack, err := a.Send(sctx, r)
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
