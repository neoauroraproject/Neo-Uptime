package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/auth"
	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
	"github.com/neoauroraproject/Neo-Uptime/internal/monitor"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

// monitorView is a monitor together with what is known about it right now.
type monitorView struct {
	*store.Monitor
	Last        *store.Heartbeat   `json:"last"`
	Uptime24    float64            `json:"uptime24"`
	Uptime30    float64            `json:"uptime30"`
	AvgPing24   float64            `json:"avgPing24"`
	Cert        *monitor.CertInfo  `json:"cert,omitempty"`
	Agent       *monitor.AgentInfo `json:"agent,omitempty"`
	Tunnel      *backpack.Tunnel   `json:"tunnel,omitempty"`
	System      *backpack.System   `json:"system,omitempty"`
	NextCheck   int64              `json:"nextCheck"`
	Maintenance bool               `json:"maintenance"`
	Target      string             `json:"target"`
}

// redact hides credentials from read-only callers.
func redact(m *store.Monitor) *store.Monitor {
	c := *m
	c.Config.BasicAuthPass = ""
	c.Config.ConnectionString = ""
	c.Config.Headers = ""
	c.Config.Body = ""
	c.Config.Password = ""
	c.Config.Secret = ""
	c.PushToken = ""
	return &c
}

func (s *Server) views(ctx context.Context, list []*store.Monitor, who *caller) ([]monitorView, error) {
	u24, err := s.db.UptimeSince(ctx, nil, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	u30, err := s.db.UptimeSince(ctx, nil, 30*24*time.Hour)
	if err != nil {
		return nil, err
	}
	states := s.mgr.States()
	out := make([]monitorView, 0, len(list))
	for _, m := range list {
		st := states[m.ID]
		mm := m
		if who != nil && who.Role < roleEditor {
			mm = redact(m)
		}
		v := monitorView{
			Monitor: mm, Last: st.Last, Cert: st.Cert, Tunnel: st.Tunnel, System: st.System, Agent: st.Agent,
			NextCheck: st.NextCheck, Target: monitor.Target(m),
			Uptime24: -1, Uptime30: -1, AvgPing24: -1,
			Maintenance: m.Active && s.mgr.InMaintenance(m.ID),
		}
		if u, ok := u24[m.ID]; ok {
			v.Uptime24, v.AvgPing24 = u.Ratio, u.AvgPing
		}
		if u, ok := u30[m.ID]; ok {
			v.Uptime30 = u.Ratio
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.db.ListMonitors(ctx)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	views, err := s.views(ctx, list, callerFrom(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	beats, err := s.db.RecentHeartbeats(ctx, nil, queryInt(r, "beats", 60, 1, 200))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"monitors": views, "heartbeats": beats, "serverTime": time.Now().UnixMilli()})
}

func (s *Server) handleListMonitors(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListMonitors(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	views, err := s.views(r.Context(), list, callerFrom(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, views)
}

func (s *Server) handleGetMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	m, err := s.db.GetMonitor(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	views, err := s.views(r.Context(), []*store.Monitor{m}, callerFrom(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, views[0])
}

// normalize fills defaults and rejects what cannot run.
func (s *Server) normalize(ctx context.Context, m *store.Monitor, existing *store.Monitor) error {
	m.Name = strings.TrimSpace(m.Name)
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(m.Name) > 150 {
		return fmt.Errorf("name is too long")
	}
	if !monitor.KnownType(m.Type) {
		return fmt.Errorf("unknown monitor type %q", m.Type)
	}
	if m.Interval == 0 {
		m.Interval = 60
	}
	if m.Interval < 10 || m.Interval > 86400 {
		return fmt.Errorf("interval must be between 10 seconds and 24 hours")
	}
	if m.RetryInterval == 0 {
		m.RetryInterval = m.Interval
	}
	m.RetryInterval = max(10, min(86400, m.RetryInterval))
	m.MaxRetries = max(0, min(20, m.MaxRetries))
	if m.Timeout <= 0 {
		m.Timeout = int(min(48, max(5, float64(m.Interval)*0.8)))
	}
	m.Timeout = max(1, min(m.Interval, m.Timeout))
	m.ResendInterval = max(0, m.ResendInterval)
	if m.Tags == nil {
		m.Tags = []store.MonitorTag{}
	}
	if m.NotificationIDs == nil {
		m.NotificationIDs = []int64{}
	}
	if m.ParentID != nil {
		if *m.ParentID == 0 {
			m.ParentID = nil
		} else {
			p, err := s.db.GetMonitor(ctx, *m.ParentID)
			if err != nil || p.Type != monitor.TypeGroup {
				return fmt.Errorf("parent must be a group monitor")
			}
			if existing != nil && *m.ParentID == existing.ID {
				return fmt.Errorf("a group cannot contain itself")
			}
		}
	}

	c := &m.Config
	switch m.Type {
	case monitor.TypeHTTP, monitor.TypeKeyword, monitor.TypeJSONQuery:
		c.URL = strings.TrimSpace(c.URL)
		u, err := url.Parse(c.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("URL must start with http:// or https://")
		}
		if m.Type == monitor.TypeKeyword && c.Keyword == "" {
			return fmt.Errorf("keyword is required")
		}
		if m.Type == monitor.TypeJSONQuery && strings.TrimSpace(c.JSONPath) == "" {
			return fmt.Errorf("JSON path is required")
		}
		if len(c.AcceptedStatusCodes) == 0 {
			c.AcceptedStatusCodes = []string{"200-299"}
		}
	case monitor.TypeTCP:
		if strings.TrimSpace(c.Hostname) == "" || c.Port < 1 || c.Port > 65535 {
			return fmt.Errorf("hostname and a port between 1 and 65535 are required")
		}
	case monitor.TypePing:
		if strings.TrimSpace(c.Hostname) == "" {
			return fmt.Errorf("hostname is required")
		}
	case monitor.TypeDNS:
		if strings.TrimSpace(c.Hostname) == "" {
			return fmt.Errorf("hostname is required")
		}
		if c.DNSRecordType == "" {
			c.DNSRecordType = "A"
		}
	case monitor.TypePush:
		if m.PushToken == "" {
			if existing != nil && existing.PushToken != "" {
				m.PushToken = existing.PushToken
			} else {
				m.PushToken = auth.RandomAlnum(24)
			}
		}
	case monitor.TypeDocker:
		if strings.TrimSpace(c.ContainerName) == "" {
			return fmt.Errorf("container name is required")
		}
	case monitor.TypePostgres, monitor.TypeMySQL, monitor.TypeRedis:
		if strings.TrimSpace(c.ConnectionString) == "" {
			return fmt.Errorf("connection string is required")
		}
	case monitor.TypeBackpackTun:
		if c.PanelID == 0 || strings.TrimSpace(c.TunnelName) == "" {
			return fmt.Errorf("choose a BackPack panel and a tunnel")
		}
		if _, err := s.db.GetPanel(ctx, c.PanelID); err != nil {
			return fmt.Errorf("that BackPack panel does not exist")
		}
	case monitor.TypeBackpackHost:
		if _, err := s.db.GetPanel(ctx, c.PanelID); err != nil {
			return fmt.Errorf("choose a BackPack panel")
		}
	case monitor.TypeAgent, monitor.TypeAgentTunnel:
		if _, err := s.db.GetAgent(ctx, c.AgentID); err != nil {
			return fmt.Errorf("choose a server (agent)")
		}
		if m.Type == monitor.TypeAgentTunnel {
			if c.Source != "kariz" {
				c.Source = "backpack"
			}
			if strings.TrimSpace(c.TunnelName) == "" {
				return fmt.Errorf("choose a tunnel")
			}
		}
	case monitor.TypeSMTP, monitor.TypeRadius, monitor.TypeSteam, monitor.TypeTLS, monitor.TypeGRPC, monitor.TypeMQTT, monitor.TypeKafka:
		if strings.TrimSpace(c.Hostname) == "" {
			return fmt.Errorf("hostname is required")
		}
		if m.Type == monitor.TypeRadius && c.Secret == "" {
			return fmt.Errorf("RADIUS shared secret is required")
		}
	case monitor.TypeUDP:
		if strings.TrimSpace(c.Hostname) == "" || c.Port < 1 || c.Port > 65535 {
			return fmt.Errorf("hostname and a port between 1 and 65535 are required")
		}
	case monitor.TypeSNMP:
		if strings.TrimSpace(c.Hostname) == "" || strings.TrimSpace(c.OID) == "" {
			return fmt.Errorf("hostname and OID are required")
		}
	case monitor.TypeWebSocket:
		c.URL = strings.TrimSpace(c.URL)
		if !strings.HasPrefix(c.URL, "ws://") && !strings.HasPrefix(c.URL, "wss://") {
			return fmt.Errorf("URL must start with ws:// or wss://")
		}
	case monitor.TypeMongo, monitor.TypeMSSQL:
		if strings.TrimSpace(c.ConnectionString) == "" {
			return fmt.Errorf("connection string is required")
		}
	}
	m.Country = normCountry(m.Country)
	if m.Type != monitor.TypePush {
		m.PushToken = ""
	}
	return nil
}

func (s *Server) handleCreateMonitor(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var m store.Monitor
	if err := json.Unmarshal(raw, &m); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid monitor: "+err.Error())
		return
	}
	// A monitor created without saying otherwise starts running.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil {
		if _, set := fields["active"]; !set {
			m.Active = true
		}
	}
	m.ID = 0
	if err := s.normalize(r.Context(), &m, nil); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.CreateMonitor(r.Context(), &m); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.mgr.Upsert(&m)
	writeJSON(w, m)
}

func (s *Server) handleUpdateMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	existing, err := s.db.GetMonitor(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var m store.Monitor
	if err := readJSON(r, &m); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	m.ID = id
	m.SortOrder = existing.SortOrder
	if err := s.normalize(r.Context(), &m, existing); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.UpdateMonitor(r.Context(), &m); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.mgr.Upsert(&m)
	writeJSON(w, m)
}

func (s *Server) handleDeleteMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.db.DeleteMonitor(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.mgr.Remove(id)
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) setActive(w http.ResponseWriter, r *http.Request, active bool) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.db.SetMonitorActive(r.Context(), id, active); err != nil {
		writeStoreErr(w, err)
		return
	}
	m, err := s.db.GetMonitor(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if active {
		s.mgr.Upsert(m)
	} else {
		s.mgr.Pause(id)
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handlePauseMonitor(w http.ResponseWriter, r *http.Request)  { s.setActive(w, r, false) }
func (s *Server) handleResumeMonitor(w http.ResponseWriter, r *http.Request) { s.setActive(w, r, true) }

func (s *Server) handleCheckNow(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	s.mgr.Kick(id)
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleBulkMonitors(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action string  `json:"action"`
		IDs    []int64 `json:"ids"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	for _, id := range in.IDs {
		switch in.Action {
		case "pause":
			if err := s.db.SetMonitorActive(ctx, id, false); err == nil {
				s.mgr.Pause(id)
			}
		case "resume":
			if err := s.db.SetMonitorActive(ctx, id, true); err == nil {
				if m, err := s.db.GetMonitor(ctx, id); err == nil {
					s.mgr.Upsert(m)
				}
			}
		case "delete":
			if err := s.db.DeleteMonitor(ctx, id); err == nil {
				s.mgr.Remove(id)
			}
		default:
			writeErr(w, http.StatusBadRequest, "action must be pause, resume or delete")
			return
		}
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleMonitorHeartbeats(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	limit := queryInt(r, "limit", 25, 1, 500)
	offset := queryInt(r, "offset", 0, 0, 1<<30)
	list, total, err := s.db.HeartbeatPage(r.Context(), id, r.URL.Query().Get("important") == "1", limit, offset)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if list == nil {
		list = []*store.Heartbeat{}
	}
	writeJSON(w, map[string]any{"items": list, "total": total})
}

func (s *Server) handleImportantEvents(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 25, 1, 200)
	offset := queryInt(r, "offset", 0, 0, 1<<30)
	list, total, err := s.db.ImportantEvents(r.Context(), limit, offset)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if list == nil {
		list = []*store.Heartbeat{}
	}
	writeJSON(w, map[string]any{"items": list, "total": total})
}

// chartPoint is one point on a response-time chart.
type chartPoint struct {
	T      int64    `json:"t"` // unix ms
	Ping   *float64 `json:"ping"`
	Min    *float64 `json:"min,omitempty"`
	Max    *float64 `json:"max,omitempty"`
	Status int      `json:"status"`
	Up     int      `json:"up,omitempty"`
	Down   int      `json:"down,omitempty"`
}

var chartRanges = map[string]time.Duration{
	"1h": time.Hour, "3h": 3 * time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour,
	"7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour, "90d": 90 * 24 * time.Hour,
	"day": 24 * time.Hour, "week": 7 * 24 * time.Hour, "month": 30 * 24 * time.Hour,
}

// chart builds a series: raw beats (down-sampled) up to a day, aggregates beyond.
func (s *Server) chart(ctx context.Context, id int64, rng string) ([]chartPoint, error) {
	d, ok := chartRanges[rng]
	if !ok {
		d = 24 * time.Hour
	}
	since := time.Now().Add(-d)
	if d <= 24*time.Hour {
		beats, err := s.db.HeartbeatsSince(ctx, id, since.UnixMilli(), 20000)
		if err != nil {
			return nil, err
		}
		return downsample(beats, 360, since, d), nil
	}
	buckets, err := s.db.Buckets(ctx, []int64{id}, d > 31*24*time.Hour, since)
	if err != nil {
		return nil, err
	}
	out := []chartPoint{}
	for _, b := range buckets[id] {
		st := store.StatusUp
		if b.Down > 0 {
			st = store.StatusDown
		}
		out = append(out, chartPoint{T: b.TS * 1000, Ping: b.AvgPing, Min: b.MinPing, Max: b.MaxPing, Status: st, Up: b.Up, Down: b.Down})
	}
	return out, nil
}

// downsample keeps every beat when there are few, and otherwise averages into
// slots, keeping the worst status in each so a short outage stays visible.
func downsample(beats []*store.Heartbeat, slots int, since time.Time, d time.Duration) []chartPoint {
	out := []chartPoint{}
	if len(beats) <= slots {
		for _, h := range beats {
			out = append(out, chartPoint{T: h.Time, Ping: h.Ping, Status: h.Status})
		}
		return out
	}
	width := d.Milliseconds() / int64(slots)
	start := since.UnixMilli()
	type acc struct {
		sum      float64
		n        int
		min, max float64
		status   int
		up, down int
		seen     bool
	}
	accs := make([]acc, slots)
	rank := map[int]int{store.StatusDown: 3, store.StatusPending: 2, store.StatusMaintenance: 1, store.StatusUp: 0}
	for _, h := range beats {
		i := int((h.Time - start) / width)
		if i < 0 || i >= slots {
			continue
		}
		a := &accs[i]
		if !a.seen || rank[h.Status] > rank[a.status] {
			a.status = h.Status
		}
		a.seen = true
		if h.Status == store.StatusDown {
			a.down++
		} else {
			a.up++
		}
		if h.Ping != nil {
			if a.n == 0 || *h.Ping < a.min {
				a.min = *h.Ping
			}
			if a.n == 0 || *h.Ping > a.max {
				a.max = *h.Ping
			}
			a.sum += *h.Ping
			a.n++
		}
	}
	for i, a := range accs {
		if !a.seen {
			continue
		}
		p := chartPoint{T: start + int64(i)*width + width/2, Status: a.status, Up: a.up, Down: a.down}
		if a.n > 0 {
			avg, mn, mx := a.sum/float64(a.n), a.min, a.max
			p.Ping, p.Min, p.Max = &avg, &mn, &mx
		}
		out = append(out, p)
	}
	return out
}

func (s *Server) handleMonitorChart(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	pts, err := s.chart(r.Context(), id, r.URL.Query().Get("range"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, pts)
}

func (s *Server) handleMonitorStats(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	ctx := r.Context()
	out := map[string]any{}
	for _, win := range []struct {
		key string
		d   time.Duration
	}{{"24h", 24 * time.Hour}, {"7d", 7 * 24 * time.Hour}, {"30d", 30 * 24 * time.Hour}, {"1y", 365 * 24 * time.Hour}} {
		u, err := s.db.UptimeSince(ctx, []int64{id}, win.d)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		if v, ok := u[id]; ok {
			out[win.key] = v
		} else {
			out[win.key] = store.Uptime{Ratio: -1, AvgPing: -1}
		}
	}
	writeJSON(w, out)
}

func (s *Server) handleClearHeartbeats(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.db.ClearHeartbeats(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleClearEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.db.ClearEvents(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
