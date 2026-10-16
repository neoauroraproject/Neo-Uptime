package monitor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
	"github.com/neoauroraproject/Neo-Uptime/internal/xray"
)

// SpeedResult is the last speed test of a config.
type SpeedResult struct {
	Down        float64   `json:"down"` // Mbps
	Up          float64   `json:"up"`
	Ping        float64   `json:"ping"` // ms
	Jitter      float64   `json:"jitter"`
	Server      string    `json:"server"`
	From        string    `json:"from,omitempty"` // the server that ran it; empty = this panel
	Bytes       int64     `json:"bytes"`
	UpError     string    `json:"upError,omitempty"`
	DownSamples []float64 `json:"downSamples,omitempty"`
	UpSamples   []float64 `json:"upSamples,omitempty"`
	At          int64     `json:"at"` // unix ms
}

// thin keeps at most n of the values, evenly spread, for storing a run.
func thin(v []float64, n int) []float64 {
	if len(v) <= n {
		return v
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = v[i*len(v)/n]
	}
	return out
}

func speedKey(id int64) string { return "xraySpeed." + strconv.FormatInt(id, 10) }

// SpeedOf returns the last speed test of a config monitor, or nil.
func (mg *Manager) SpeedOf(ctx context.Context, id int64) *SpeedResult {
	raw := mg.db.GetSetting(ctx, speedKey(id), "")
	if raw == "" {
		return nil
	}
	var r SpeedResult
	if json.Unmarshal([]byte(raw), &r) != nil {
		return nil
	}
	return &r
}

// Speeds returns the last speed test of every config that has one.
func (mg *Manager) Speeds(ctx context.Context) map[int64]*SpeedResult {
	all, err := mg.db.AllSettings(ctx)
	out := map[int64]*SpeedResult{}
	if err != nil {
		return out
	}
	for k, v := range all {
		rest, ok := strings.CutPrefix(k, "xraySpeed.")
		if !ok {
			continue
		}
		id, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			continue
		}
		var r SpeedResult
		if json.Unmarshal([]byte(v), &r) == nil {
			out[id] = &r
		}
	}
	return out
}

// ServerSpeedOf returns the last speed test of a server's own connection.
func (mg *Manager) ServerSpeedOf(ctx context.Context, agentID int64) *SpeedResult {
	raw := mg.db.GetSetting(ctx, "serverSpeed."+strconv.FormatInt(agentID, 10), "")
	if raw == "" {
		return nil
	}
	var r SpeedResult
	if json.Unmarshal([]byte(raw), &r) != nil {
		return nil
	}
	return &r
}

// SpeedRequest says what to measure. With a Link it is a config; without one
// (Direct) it is the connection of the server itself. AgentID is the server
// that runs the test: 0 is the panel's own.
type SpeedRequest struct {
	MonitorID int64  // the config monitor the result belongs to, or 0
	Link      string // the config
	AgentID   int64
	Direct    bool
	Provider  string // "" or "ookla" | "cloudflare"
}

// key names what is measured, so that one test of it runs at a time.
func (r SpeedRequest) key() string {
	if r.Direct {
		return "a" + strconv.FormatInt(r.AgentID, 10)
	}
	if r.MonitorID != 0 {
		return "m" + strconv.FormatInt(r.MonitorID, 10)
	}
	return ""
}

// SpeedJob is a speed test running in the background, which the page
// follows as a stream of its changes (or by polling).
type SpeedJob struct {
	ID       string                    `json:"id"`
	State    string                    `json:"state"` // queued | running | done | error
	Progress *agentproto.SpeedProgress `json:"progress,omitempty"`
	Result   *SpeedResult              `json:"result,omitempty"`
	Error    string                    `json:"error,omitempty"`
	From     string                    `json:"from,omitempty"`
	started  time.Time
	changed  chan struct{} // closed, and replaced, at every change
}

// StartSpeedJob starts a speed test of a config in the background. id is the
// monitor it belongs to (its result is kept there), or 0.
func (mg *Manager) StartSpeedJob(req SpeedRequest) (string, error) {
	if req.Direct {
		req.Link = ""
	} else if _, err := xray.Parse(req.Link); err != nil {
		return "", err
	}
	if req.Provider != "" && req.Provider != xray.ProviderOokla && req.Provider != xray.ProviderCloudflare {
		return "", errors.New("unknown speed test provider")
	}
	var from string
	if req.AgentID != 0 {
		a, err := mg.db.GetAgent(mg.ctx, req.AgentID)
		if err != nil {
			return "", errors.New("that server does not exist")
		}
		from = a.Name
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	job := &SpeedJob{ID: hex.EncodeToString(b), State: "queued", From: from, started: time.Now(), changed: make(chan struct{})}

	mg.speedMu.Lock()
	if k := req.key(); k != "" {
		// One test of the same thing at a time.
		if mg.speeding[k] {
			mg.speedMu.Unlock()
			return "", errors.New("a speed test of this is already running")
		}
		mg.speeding[k] = true
	}
	for k, j := range mg.speedJobs {
		if time.Since(j.started) > 15*time.Minute {
			delete(mg.speedJobs, k)
		}
	}
	mg.speedJobs[job.ID] = job
	mg.speedMu.Unlock()

	set := func(f func(*SpeedJob)) {
		mg.speedMu.Lock()
		f(job)
		close(job.changed)
		job.changed = make(chan struct{})
		mg.speedMu.Unlock()
	}
	go func() {
		ctx, cancel := context.WithTimeout(mg.ctx, 3*time.Minute)
		defer cancel()
		res, err := mg.SpeedTest(ctx, req, func(p agentproto.SpeedProgress) {
			set(func(j *SpeedJob) { j.State, j.Progress = "running", &p })
		})
		set(func(j *SpeedJob) {
			if k := req.key(); k != "" {
				delete(mg.speeding, k)
			}
			if err != nil {
				j.State, j.Error = "error", err.Error()
				return
			}
			j.State, j.Result = "done", res
		})
	}()
	return job.ID, nil
}

// SpeedJobState is a copy of a running or finished speed test.
func (mg *Manager) SpeedJobState(id string) (SpeedJob, bool) {
	mg.speedMu.Lock()
	defer mg.speedMu.Unlock()
	j, ok := mg.speedJobs[id]
	if !ok {
		return SpeedJob{}, false
	}
	return *j, true
}

// WatchSpeedJob is SpeedJobState with a channel that is closed when the test
// next changes, so a stream can send each reading as it comes.
func (mg *Manager) WatchSpeedJob(id string) (SpeedJob, <-chan struct{}, bool) {
	mg.speedMu.Lock()
	defer mg.speedMu.Unlock()
	j, ok := mg.speedJobs[id]
	if !ok {
		return SpeedJob{}, nil, false
	}
	return *j, j.changed, true
}

// SpeedTest measures a config's speed or a server's own connection, here or
// on a server, and keeps the result (with the monitor, or with the server).
// onProgress, when set, hears how it goes while it runs.
func (mg *Manager) SpeedTest(ctx context.Context, req SpeedRequest, onProgress func(agentproto.SpeedProgress)) (*SpeedResult, error) {
	var l *xray.Link
	if !req.Direct {
		var err error
		if l, err = xray.Parse(req.Link); err != nil {
			return nil, err
		}
	}
	r := &SpeedResult{}
	if req.AgentID == 0 {
		sp, err := xray.MeasureSpeed(ctx, l, req.Provider, onProgress)
		if err != nil {
			return nil, err
		}
		r.Down, r.Up, r.Ping, r.Jitter, r.Server, r.Bytes, r.UpError = sp.Down, sp.Up, sp.Ping, sp.Jitter, sp.Server, sp.Bytes, sp.UpError
		r.DownSamples, r.UpSamples = sp.DownSamples, sp.UpSamples
	} else {
		a, err := mg.db.GetAgent(ctx, req.AgentID)
		if err != nil {
			return nil, errors.New("that server does not exist")
		}
		// Speedtest.net servers and tests without a config came with the
		// agent that has the speed2 capability.
		if st := mg.env.Agents.Get(a.ID); st != nil && st.Report != nil &&
			(req.Direct || req.Provider != xray.ProviderCloudflare) && !slices.Contains(st.Report.Caps, agentproto.CapSpeed2) {
			return nil, fmt.Errorf("neo-uptime-agent on %s is too old for this speed test: %w", a.Name, ErrAgentTooOld)
		}
		jr, err := mg.runJob(ctx, req.AgentID, agentproto.Job{Kind: "speed", Link: req.Link, Direct: req.Direct, Provider: req.Provider, Timeout: 90}, func(p agentproto.JobResult) {
			if p.Progress != nil && onProgress != nil {
				onProgress(*p.Progress)
			}
		})
		if err != nil {
			return nil, err
		}
		if jr.Error != "" {
			if strings.Contains(strings.ToLower(jr.Error), "does not know job") {
				return nil, fmt.Errorf("neo-uptime-agent on %s is too old for speed tests: %w", a.Name, ErrAgentTooOld)
			}
			return nil, errors.New(jr.Error)
		}
		r.Down, r.Up, r.Ping, r.Jitter, r.Server, r.Bytes, r.UpError, r.From = jr.Down, jr.Up, jr.Ping, jr.Jitter, jr.Server, jr.Bytes, jr.UpError, a.Name
		r.DownSamples, r.UpSamples = jr.DownSamples, jr.UpSamples
	}
	r.DownSamples, r.UpSamples = thin(r.DownSamples, 50), thin(r.UpSamples, 50)
	r.At = time.Now().UnixMilli()
	key := ""
	switch {
	case req.Direct:
		key = "serverSpeed." + strconv.FormatInt(req.AgentID, 10)
	case req.MonitorID != 0:
		key = speedKey(req.MonitorID)
	}
	if key != "" {
		b, _ := json.Marshal(r)
		_ = mg.db.SetSettings(ctx, map[string]string{key: string(b)})
		if req.MonitorID != 0 {
			mg.pub.Publish("monitors", map[string]any{"changed": req.MonitorID})
		} else {
			mg.pub.Publish("agent", map[string]any{"agentId": req.AgentID})
		}
	}
	return r, nil
}
