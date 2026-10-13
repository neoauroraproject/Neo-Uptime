package monitor

import (
	"context"
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
	Down    float64 `json:"down"` // Mbps
	Up      float64 `json:"up"`
	Ping    float64 `json:"ping"` // ms
	Server  string  `json:"server"`
	From    string  `json:"from,omitempty"` // the server that ran it; empty = this panel
	Bytes   int64   `json:"bytes"`
	UpError string  `json:"upError,omitempty"`
	At      int64   `json:"at"` // unix ms
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

// SpeedTest measures a config's speed, here or on a server, and keeps the
// result with the monitor (id 0: nothing is kept). One test per config runs
// at a time.
func (mg *Manager) SpeedTest(ctx context.Context, id int64, link string, agentID int64) (*SpeedResult, error) {
	l, err := xray.Parse(link)
	if err != nil {
		return nil, err
	}
	if id != 0 {
		mg.speedMu.Lock()
		if mg.speeding[id] {
			mg.speedMu.Unlock()
			return nil, errors.New("a speed test of this config is already running")
		}
		mg.speeding[id] = true
		mg.speedMu.Unlock()
		defer func() {
			mg.speedMu.Lock()
			delete(mg.speeding, id)
			mg.speedMu.Unlock()
		}()
	}
	r := &SpeedResult{}
	if agentID == 0 {
		sp, err := xray.MeasureSpeed(ctx, l)
		if err != nil {
			return nil, err
		}
		r.Down, r.Up, r.Ping, r.Server, r.Bytes, r.UpError = sp.Down, sp.Up, sp.Ping, sp.Server, sp.Bytes, sp.UpError
	} else {
		a, err := mg.db.GetAgent(ctx, agentID)
		if err != nil {
			return nil, errors.New("that server does not exist")
		}
		jr, err := mg.RunJob(ctx, agentID, agentproto.Job{Kind: "speed", Link: link, Timeout: 75})
		if err != nil {
			return nil, err
		}
		if jr.Error != "" {
			if strings.Contains(strings.ToLower(jr.Error), "does not know job") {
				return nil, fmt.Errorf("neo-uptime-agent on %s is too old for speed tests: %w", a.Name, ErrAgentTooOld)
			}
			return nil, errors.New(jr.Error)
		}
		r.Down, r.Up, r.Ping, r.Server, r.Bytes, r.UpError, r.From = jr.Down, jr.Up, jr.Ping, jr.Server, jr.Bytes, jr.UpError, a.Name
	}
	r.At = time.Now().UnixMilli()
	if id != 0 {
		b, _ := json.Marshal(r)
		_ = mg.db.SetSettings(ctx, map[string]string{speedKey(id): string(b)})
		mg.pub.Publish("monitors", map[string]any{"changed": id})
	}
	return r, nil
}
