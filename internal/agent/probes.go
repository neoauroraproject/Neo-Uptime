package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
	"github.com/neoauroraproject/Neo-Uptime/internal/xray"
)

// prober runs the tests the server hands out, from this server's network:
// recurring probes (a V2Ray config every minute) and one-off jobs (fetch a
// subscription, test a config now). Results go back with the next report;
// wake asks for that report early so nobody waits a whole interval.
type prober struct {
	agent  *Agent
	client *http.Client
	wake   chan struct{}
	slots  chan struct{}

	mu       sync.Mutex
	probes   map[int64]agentproto.Probe
	lastRun  map[int64]time.Time
	running  map[int64]bool
	results  map[int64]agentproto.ProbeResult
	jobs     []agentproto.JobResult
	seenJobs map[string]time.Time
}

func newProber() *prober {
	return &prober{
		client:   &http.Client{Timeout: 30 * time.Second},
		wake:     make(chan struct{}, 1),
		slots:    make(chan struct{}, 4),
		probes:   map[int64]agentproto.Probe{},
		lastRun:  map[int64]time.Time{},
		running:  map[int64]bool{},
		results:  map[int64]agentproto.ProbeResult{},
		seenJobs: map[string]time.Time{},
	}
}

// set replaces the recurring tests with the server's current list.
func (p *prober) set(list []agentproto.Probe) {
	p.mu.Lock()
	defer p.mu.Unlock()
	keep := map[int64]agentproto.Probe{}
	for _, pr := range list {
		if old, ok := p.probes[pr.ID]; ok && old.Link != pr.Link {
			delete(p.lastRun, pr.ID) // a changed config is tested at once
		}
		keep[pr.ID] = pr
	}
	for id := range p.probes {
		if _, ok := keep[id]; !ok {
			delete(p.lastRun, id)
			delete(p.results, id)
		}
	}
	p.probes = keep
}

// add starts one-off jobs, each once.
func (p *prober) add(jobs []agentproto.Job) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, at := range p.seenJobs {
		if time.Since(at) > 10*time.Minute {
			delete(p.seenJobs, id)
		}
	}
	for _, j := range jobs {
		if _, seen := p.seenJobs[j.ID]; seen {
			continue
		}
		p.seenJobs[j.ID] = time.Now()
		go p.runJob(j)
	}
}

// attach puts the latest results into a report.
func (p *prober) attach(r *agentproto.Report) {
	r.Caps = []string{agentproto.CapXray}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, res := range p.results {
		r.Probes = append(r.Probes, res)
	}
	r.Jobs = append(r.Jobs, p.jobs...)
}

// delivered drops job results the server has received.
func (p *prober) delivered(sent []agentproto.JobResult) {
	if len(sent) == 0 {
		return
	}
	// A progress note and the final result of a job share its ID.
	key := func(j agentproto.JobResult) string { return fmt.Sprint(j.ID, j.Partial) }
	done := map[string]bool{}
	for _, j := range sent {
		done[key(j)] = true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	kept := p.jobs[:0]
	for _, j := range p.jobs {
		if !done[key(j)] {
			kept = append(kept, j)
		}
	}
	p.jobs = kept
}

// loop starts each probe when it is due.
func (p *prober) loop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		p.mu.Lock()
		var due []agentproto.Probe
		for id, pr := range p.probes {
			every := time.Duration(max(pr.Every, 10)) * time.Second
			if !p.running[id] && time.Since(p.lastRun[id]) >= every {
				p.running[id] = true
				p.lastRun[id] = time.Now()
				due = append(due, pr)
			}
		}
		p.mu.Unlock()
		for _, pr := range due {
			go p.runProbe(ctx, pr)
		}
	}
}

func (p *prober) runProbe(ctx context.Context, pr agentproto.Probe) {
	p.slots <- struct{}{}
	defer func() { <-p.slots }()
	res := agentproto.ProbeResult{ID: pr.ID}
	ping, connect, err := testLink(ctx, pr.Link, pr.URL, pr.Timeout)
	res.At = time.Now().UnixMilli()
	if err != nil {
		res.Msg = err.Error()
	} else {
		res.OK, res.Ping, res.Connect = true, ping, connect
	}
	p.mu.Lock()
	delete(p.running, pr.ID)
	if _, still := p.probes[pr.ID]; still {
		p.results[pr.ID] = res
	}
	p.mu.Unlock()
	p.poke()
}

func (p *prober) runJob(j agentproto.Job) {
	res := agentproto.JobResult{ID: j.ID}
	timeout := time.Duration(max(j.Timeout, 5)) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	switch j.Kind {
	case "fetch":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.URL, nil)
		if err == nil {
			req.Header.Set("User-Agent", "v2rayN/6.31")
			var resp *http.Response
			if resp, err = p.client.Do(req); err == nil {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					res.Error = "the subscription answered " + resp.Status
				}
				res.Body = string(body)
				res.Userinfo = resp.Header.Get("Subscription-Userinfo")
				res.Title = resp.Header.Get("Profile-Title")
			}
		}
		if err != nil {
			res.Error = err.Error()
		}
	case "test":
		ping, connect, err := testLink(ctx, j.Link, j.URL, j.Timeout)
		if err != nil {
			res.Error = err.Error()
		} else {
			res.Ping, res.Connect = ping, connect
		}
	case "speed":
		l, err := xray.Parse(j.Link)
		if err == nil {
			var sp *xray.Speed
			if sp, err = xray.MeasureSpeed(ctx, l, func(pr agentproto.SpeedProgress) { p.progress(j.ID, pr) }); err == nil {
				res.Down, res.Up, res.Ping, res.Jitter, res.Server, res.Bytes, res.UpError = sp.Down, sp.Up, sp.Ping, sp.Jitter, sp.Server, sp.Bytes, sp.UpError
				res.DownSamples, res.UpSamples = sp.DownSamples, sp.UpSamples
			}
		}
		if err != nil {
			res.Error = err.Error()
		}
	case "update":
		if err := p.agent.selfUpdate(ctx, j); err != nil {
			res.Error = err.Error()
		} else {
			res.Restart = true
		}
	default:
		res.Error = "this agent does not know job " + j.Kind
	}
	p.mu.Lock()
	p.dropProgress(j.ID)
	p.jobs = append(p.jobs, res)
	p.mu.Unlock()
	p.poke()
}

// progress tells the server how a running job is going. Only the latest
// note is kept: the server wants where it is now.
func (p *prober) progress(id string, pr agentproto.SpeedProgress) {
	p.mu.Lock()
	p.dropProgress(id)
	p.jobs = append(p.jobs, agentproto.JobResult{ID: id, Partial: true, Progress: &pr})
	p.mu.Unlock()
	p.poke()
}

// dropProgress forgets the unsent progress notes of a job. p.mu is held.
func (p *prober) dropProgress(id string) {
	kept := p.jobs[:0]
	for _, j := range p.jobs {
		if !(j.ID == id && j.Partial) {
			kept = append(kept, j)
		}
	}
	p.jobs = kept
}

func (p *prober) poke() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// testLink runs a config once through the built-in Xray core.
func testLink(ctx context.Context, link, url string, timeoutSecs int) (ping, connect float64, err error) {
	l, err := xray.Parse(link)
	if err != nil {
		return 0, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(max(timeoutSecs, 5))*time.Second)
	defer cancel()
	r, err := xray.Test(ctx, l, url)
	if err != nil {
		return 0, 0, err
	}
	return msOf(r.Delay), msOf(r.Connect), nil
}

func msOf(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
