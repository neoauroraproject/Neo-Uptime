package monitor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

// SpeedJob is a speed test running in the background, which the page
// follows by polling.
type SpeedJob struct {
	ID       string                    `json:"id"`
	State    string                    `json:"state"` // queued | running | done | error
	Progress *agentproto.SpeedProgress `json:"progress,omitempty"`
	Result   *SpeedResult              `json:"result,omitempty"`
	Error    string                    `json:"error,omitempty"`
	From     string                    `json:"from,omitempty"`
	started  time.Time
}

// StartSpeedJob starts a speed test of a config in the background. id is the
// monitor it belongs to (its result is kept there), or 0.
func (mg *Manager) StartSpeedJob(id int64, link string, agentID int64) (string, error) {
	if _, err := xray.Parse(link); err != nil {
		return "", err
	}
	var from string
	if agentID != 0 {
		a, err := mg.db.GetAgent(mg.ctx, agentID)
		if err != nil {
			return "", errors.New("that server does not exist")
		}
		from = a.Name
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	job := &SpeedJob{ID: hex.EncodeToString(b), State: "queued", From: from, started: time.Now()}

	mg.speedMu.Lock()
	if id != 0 {
		// One test per config at a time.
		if mg.speeding[id] {
			mg.speedMu.Unlock()
			return "", errors.New("a speed test of this config is already running")
		}
		mg.speeding[id] = true
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
		mg.speedMu.Unlock()
	}
	go func() {
		ctx, cancel := context.WithTimeout(mg.ctx, 3*time.Minute)
		defer cancel()
		res, err := mg.SpeedTest(ctx, id, link, agentID, func(p agentproto.SpeedProgress) {
			set(func(j *SpeedJob) { j.State, j.Progress = "running", &p })
		})
		set(func(j *SpeedJob) {
			if id != 0 {
				delete(mg.speeding, id)
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

// SpeedTest measures a config's speed, here or on a server, and keeps the
// result with the monitor (id 0: nothing is kept). onProgress, when set,
// hears how it goes while it runs.
func (mg *Manager) SpeedTest(ctx context.Context, id int64, link string, agentID int64, onProgress func(agentproto.SpeedProgress)) (*SpeedResult, error) {
	l, err := xray.Parse(link)
	if err != nil {
		return nil, err
	}
	r := &SpeedResult{}
	if agentID == 0 {
		sp, err := xray.MeasureSpeed(ctx, l, onProgress)
		if err != nil {
			return nil, err
		}
		r.Down, r.Up, r.Ping, r.Jitter, r.Server, r.Bytes, r.UpError = sp.Down, sp.Up, sp.Ping, sp.Jitter, sp.Server, sp.Bytes, sp.UpError
		r.DownSamples, r.UpSamples = sp.DownSamples, sp.UpSamples
	} else {
		a, err := mg.db.GetAgent(ctx, agentID)
		if err != nil {
			return nil, errors.New("that server does not exist")
		}
		jr, err := mg.runJob(ctx, agentID, agentproto.Job{Kind: "speed", Link: link, Timeout: 75}, func(p agentproto.JobResult) {
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
	if id != 0 {
		b, _ := json.Marshal(r)
		_ = mg.db.SetSettings(ctx, map[string]string{speedKey(id): string(b)})
		mg.pub.Publish("monitors", map[string]any{"changed": id})
	}
	return r, nil
}
