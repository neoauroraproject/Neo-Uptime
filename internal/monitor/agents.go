package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

// AgentState is the latest report of one agent.
type AgentState struct {
	Report     *agentproto.Report
	ReceivedAt time.Time
	IP         string
}

// AgentHub keeps the latest report of every agent in memory. Reports arrive
// every few seconds, so persisting them would only grind the disk; after a
// restart the next report refills it.
type AgentHub struct {
	mu sync.RWMutex
	m  map[int64]*AgentState
	x  *xrayHub
}

// NewAgentHub returns an empty hub.
func NewAgentHub() *AgentHub { return &AgentHub{m: map[int64]*AgentState{}, x: newXrayHub()} }

// Put stores a report.
func (h *AgentHub) Put(id int64, st *AgentState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.m[id] = st
}

// Get returns an agent's latest report, or nil.
func (h *AgentHub) Get(id int64) *AgentState {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.m[id]
}

// Forget drops an agent.
func (h *AgentHub) Forget(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.m, id)
}

// SourceSummary is a short account of one tunnel program on an agent.
type SourceSummary struct {
	Installed bool   `json:"installed"`
	Mode      string `json:"mode,omitempty"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
	Tunnels   int    `json:"tunnels"`
	Online    int    `json:"online"`
}

// AgentInfo is what the UI shows about an agent's host.
type AgentInfo struct {
	System     agentproto.System `json:"system"`
	ReceivedAt int64             `json:"receivedAt"`
	Version    string            `json:"version"`
	IP         string            `json:"ip"`
	Backpack   *SourceSummary    `json:"backpack,omitempty"`
	Kariz      *SourceSummary    `json:"kariz,omitempty"`
	NeoTunnel  *SourceSummary    `json:"neotunnel,omitempty"`
}

// TunnelSources are the tunnel programs an agent reads, in the order they
// are shown.
var TunnelSources = []string{"backpack", "kariz", "neotunnel"}

// SourceLabel is a tunnel program's name as people write it.
func SourceLabel(source string) string {
	switch source {
	case "kariz":
		return "Kariz"
	case "neotunnel":
		return "NeoTunnel"
	}
	return "BackPack"
}

// KnownSource reports whether an agent can read the named tunnel program.
func KnownSource(source string) bool { return slices.Contains(TunnelSources, source) }

func summarize(s *agentproto.Source) *SourceSummary {
	if s == nil {
		return nil
	}
	out := &SourceSummary{Installed: s.Installed, Mode: s.Mode, Version: s.Version, Error: s.Error, Tunnels: len(s.Tunnels)}
	for _, t := range s.Tunnels {
		if t.State == "online" {
			out.Online++
		}
	}
	return out
}

// Info builds the UI view of a stored state.
func (st *AgentState) Info() *AgentInfo {
	if st == nil || st.Report == nil {
		return nil
	}
	return &AgentInfo{
		System: st.Report.System, ReceivedAt: st.ReceivedAt.UnixMilli(), Version: st.Report.AgentVersion, IP: st.IP,
		Backpack: summarize(st.Report.Backpack), Kariz: summarize(st.Report.Kariz), NeoTunnel: summarize(st.Report.NeoTunnel),
	}
}

// Source returns the named tunnel program's report.
func (st *AgentState) Source(name string) *agentproto.Source {
	if st == nil || st.Report == nil {
		return nil
	}
	switch name {
	case "kariz":
		return st.Report.Kariz
	case "neotunnel":
		return st.Report.NeoTunnel
	}
	return st.Report.Backpack
}

func agentStale(a *store.Agent, st *AgentState) (time.Duration, bool) {
	interval := time.Duration(max(a.Interval, 5)) * time.Second
	if st != nil && st.Report != nil && st.Report.Interval > 0 {
		interval = time.Duration(st.Report.Interval) * time.Second
	}
	limit := max(3*interval, 60*time.Second)
	if st == nil {
		return 0, true
	}
	age := time.Since(st.ReceivedAt)
	return age, age > limit
}

// processStart is when this process began. Agents report within their
// interval of it; until then "no report yet" says nothing about them.
var processStart = time.Now()

// awaitingFirstReport is true while a freshly started panel has not heard
// from an agent yet: no check records anything until the agent reports.
func awaitingFirstReport(st *AgentState) bool {
	return st == nil && time.Since(processStart) < 90*time.Second
}

func agentGone(a *store.Agent, st *AgentState, age time.Duration) string {
	if st == nil {
		if a.LastSeen > 0 {
			return fmt.Sprintf("agent %s has not reported since %s", a.Name, time.Unix(a.LastSeen, 0).UTC().Format("2006-01-02 15:04 UTC"))
		}
		return fmt.Sprintf("agent %s has not reported yet — install it on the server", a.Name)
	}
	return fmt.Sprintf("agent %s silent for %s", a.Name, age.Round(time.Second))
}

func checkAgent(ctx context.Context, m *store.Monitor, env *Env) Result {
	a, err := env.DB.GetAgent(ctx, m.Config.AgentID)
	if err != nil {
		return down("this agent no longer exists")
	}
	st := env.Agents.Get(a.ID)
	if awaitingFirstReport(st) {
		return Result{Skip: true}
	}
	if age, stale := agentStale(a, st); stale {
		return down(agentGone(a, st, age))
	}
	s := st.Report.System
	r := Result{Up: true, Agent: st.Info()}
	if st.Report.RTTMs > 0 {
		v := st.Report.RTTMs
		r.Ping = &v
	}
	parts := []string{fmt.Sprintf("CPU %.0f%%", s.CPUPercent)}
	if s.MemTotal > 0 {
		parts = append(parts, fmt.Sprintf("RAM %.0f%%", 100*float64(s.MemUsed)/float64(s.MemTotal)))
	}
	if s.DiskTotal > 0 {
		parts = append(parts, fmt.Sprintf("disk %.0f%%", 100*float64(s.DiskUsed)/float64(s.DiskTotal)))
	}
	for _, name := range TunnelSources {
		if sum := summarize(st.Source(name)); sum != nil && sum.Tunnels > 0 {
			parts = append(parts, fmt.Sprintf("%s %d/%d", SourceLabel(name), sum.Online, sum.Tunnels))
		}
	}
	r.Msg = strings.Join(parts, " · ")
	return r
}

func checkAgentTunnel(ctx context.Context, m *store.Monitor, env *Env) Result {
	a, err := env.DB.GetAgent(ctx, m.Config.AgentID)
	if err != nil {
		return down("this agent no longer exists")
	}
	st := env.Agents.Get(a.ID)
	if awaitingFirstReport(st) {
		return Result{Skip: true}
	}
	if age, stale := agentStale(a, st); stale {
		return down(agentGone(a, st, age))
	}
	label := SourceLabel(m.Config.Source)
	src := st.Source(m.Config.Source)
	if src == nil {
		return down(label + " is not installed on " + a.Name)
	}
	if src.Error != "" && len(src.Tunnels) == 0 {
		return down(label + ": " + src.Error)
	}
	for i := range src.Tunnels {
		if src.Tunnels[i].Name == m.Config.TunnelName {
			return tunnelResult(&src.Tunnels[i], m.Config.ServiceDownIsDown)
		}
	}
	return down(fmt.Sprintf("%s tunnel %q is not on %s any more", label, m.Config.TunnelName, a.Name))
}

// tunnelResult turns a reported tunnel into a check result. Shared by the
// panel and agent paths so a tunnel reads the same however it was reached.
func tunnelResult(t *backpack.Tunnel, serviceDownIsDown bool) Result {
	tc := *t
	r := Result{Tunnel: &tc}
	in, out := int64(t.InBytes), int64(t.OutBytes)
	r.BytesIn, r.BytesOut = &in, &out
	if t.Ping >= 0 {
		v := float64(t.Ping)
		r.Ping = &v
	}
	label := strings.ToUpper(firstNonEmpty(t.Carrier, t.Transport))
	switch {
	case t.QuotaHit:
		r.Msg = "traffic quota reached — tunnel stopped"
	case t.State == "online" && t.ServiceDown != "" && serviceDownIsDown:
		r.Msg = "tunnel up but the forwarded service is refusing: " + t.ServiceDown
	case t.State == "online":
		r.Up = true
		r.Msg = label + " online"
		if t.Uptime != "" {
			r.Msg += " · up " + t.Uptime
		}
		if t.ServiceDown != "" {
			r.Msg += " · service down: " + t.ServiceDown
		}
	case t.State == "stopped":
		r.Msg = "tunnel service is stopped"
		if t.LastError != "" {
			r.Msg = "tunnel stopped · " + t.LastError
		}
	default:
		r.Msg = fmt.Sprintf("%s %s — peer not connected", label, firstNonEmpty(t.State, "offline"))
		if t.LastError != "" {
			r.Msg += " · " + t.LastError
		}
	}
	return r
}

// ---------- ingest & import ----------

// IngestAgentReport stores a report and re-checks the agent's monitors at
// once, so a tunnel that dropped shows within a report, not an interval. It
// returns the tests the agent should run.
func (mg *Manager) IngestAgentReport(ctx context.Context, a *store.Agent, r *agentproto.Report, ip string) AgentReply {
	if r.Early {
		// Only test results: the full report stays as it was.
		return mg.agentReply(a, r)
	}
	mg.env.Agents.Put(a.ID, &AgentState{Report: r, ReceivedAt: time.Now(), IP: ip})
	mg.db.TouchAgent(ctx, a.ID, ip, r.AgentVersion, r.System.Hostname)

	mg.mu.Lock()
	var ids []int64
	for id, aid := range mg.agentOf {
		if aid == a.ID {
			ids = append(ids, id)
		}
	}
	mg.mu.Unlock()
	for _, id := range ids {
		mg.Kick(id)
	}
	go mg.syncTunnelNames(a, r)
	if a.AutoImport {
		go func() {
			ictx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := mg.ImportAgentTunnels(ictx, a.ID, nil, nil); err != nil {
				log.Printf("agent %s: auto-import: %v", a.Name, err)
			}
		}()
	}
	mg.pub.Publish("agent", map[string]any{"agentId": a.ID})
	return mg.agentReply(a, r)
}

func agentKey(source, name string) string { return source + "\x00" + name }

// TunnelDisplayName is what a tunnel is called: the display name its program
// gives it, or its ID.
func TunnelDisplayName(t *backpack.Tunnel) string {
	if l := strings.TrimSpace(t.Label); l != "" {
		return l
	}
	return t.Name
}

// syncTunnelNames renames the monitors of tunnels whose display name changed
// in their program, as long as the monitor still has the name it was given
// from it: a name someone typed is never overwritten.
func (mg *Manager) syncTunnelNames(a *store.Agent, r *agentproto.Report) {
	labels := map[string]string{}
	st := &AgentState{Report: r}
	for _, src := range TunnelSources {
		s := st.Source(src)
		if s == nil {
			continue
		}
		for _, t := range s.Tunnels {
			labels[agentKey(src, t.Name)] = strings.TrimSpace(t.Label)
		}
	}
	mg.mu.Lock()
	var todo []*store.Monitor
	for _, rn := range mg.runners {
		m := rn.m
		if m.Type != TypeAgentTunnel || m.Config.AgentID != a.ID {
			continue
		}
		label, ok := labels[agentKey(m.Config.Source, m.Config.TunnelName)]
		if !ok || label == m.Config.TunnelLabel {
			continue
		}
		todo = append(todo, m)
	}
	mg.mu.Unlock()
	if len(todo) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, old := range todo {
		m, err := mg.db.GetMonitor(ctx, old.ID)
		if err != nil {
			continue
		}
		label := labels[agentKey(m.Config.Source, m.Config.TunnelName)]
		given := firstNonEmpty(m.Config.TunnelLabel, m.Config.TunnelName)
		if m.Name == given {
			m.Name = firstNonEmpty(label, m.Config.TunnelName)
		}
		m.Config.TunnelLabel = label
		if err := mg.db.UpdateMonitor(ctx, m); err != nil {
			log.Printf("agent %s: rename tunnel %s: %v", a.Name, m.Config.TunnelName, err)
			continue
		}
		mg.Upsert(m)
	}
}

// ImportAgentTunnels makes sure an agent has its group and server monitor,
// and creates a monitor for each reported tunnel that has none. keys, when
// given, limits it to "source/name" entries.
func (mg *Manager) ImportAgentTunnels(ctx context.Context, agentID int64, keys []string, opt *ImportOptions) (*ImportResult, error) {
	mg.importMu.Lock()
	defer mg.importMu.Unlock()
	res := &ImportResult{MonitorIDs: []int64{}}
	a, err := mg.db.GetAgent(ctx, agentID)
	if err != nil {
		return res, err
	}
	st := mg.env.Agents.Get(a.ID)
	if st == nil || st.Report == nil {
		return res, fmt.Errorf("agent %s has not reported yet", a.Name)
	}
	existing, err := mg.db.ListMonitors(ctx)
	if err != nil {
		return res, err
	}
	have := map[string]bool{}
	hasServer := false
	for _, m := range existing {
		if m.Config.AgentID != a.ID {
			continue
		}
		switch m.Type {
		case TypeAgent:
			hasServer = true
			res.ServerMonitorID = m.ID
		case TypeAgentTunnel:
			have[agentKey(m.Config.Source, m.Config.TunnelName)] = true
		}
	}
	want := map[string]bool{}
	for _, k := range keys {
		src, name, _ := strings.Cut(k, "/")
		want[agentKey(src, name)] = true
	}
	// Tunnels someone chose not to watch are remembered, so auto-import only
	// adds tunnels that are really new.
	skipped := mg.skippedTunnels(ctx, a.ID)
	if opt != nil && len(opt.Skip) > 0 {
		for _, k := range opt.Skip {
			skipped[k] = true
		}
	}
	if keys != nil || (opt != nil && len(opt.Skip) > 0) {
		for _, k := range keys {
			delete(skipped, k)
		}
		mg.saveSkipped(ctx, a.ID, skipped)
	}
	type item struct {
		source string
		t      backpack.Tunnel
	}
	var todo []item
	for _, src := range TunnelSources {
		s := st.Source(src)
		if s == nil {
			continue
		}
		for _, t := range s.Tunnels {
			k := agentKey(src, t.Name)
			if have[k] || (keys != nil && !want[k]) || (keys == nil && skipped[src+"/"+t.Name]) {
				continue
			}
			todo = append(todo, item{src, t})
		}
	}
	if a.GroupMonitorID != nil {
		res.GroupID = *a.GroupMonitorID
	}
	if hasServer && len(todo) == 0 {
		return res, nil
	}

	gid, err := mg.ensureAgentGroup(ctx, a)
	if err != nil {
		return res, err
	}
	res.GroupID = gid
	o := mg.importOptions(ctx, opt, max(a.Interval*2, 30))
	if !hasServer {
		m := &store.Monitor{
			Name: a.Name, Type: TypeAgent, ParentID: &gid, Active: true, Interval: o.Interval, RetryInterval: o.Interval,
			MaxRetries: 1, Timeout: 10, Config: store.MonitorConfig{AgentID: a.ID}, Country: a.Country,
			NotificationIDs: nonNil(o.NotificationIDs), Description: "Server health reported by neo-uptime-agent.",
		}
		if err := mg.db.CreateMonitor(ctx, m); err != nil {
			return res, err
		}
		mg.Upsert(m)
		res.ServerMonitorID = m.ID
		res.MonitorIDs = append(res.MonitorIDs, m.ID)
	}
	n := 0
	for _, it := range todo {
		label := SourceLabel(it.source)
		country := it.t.PeerCountry
		if country == "" {
			country = a.Country
		}
		m := &store.Monitor{
			Name: TunnelDisplayName(&it.t), Type: TypeAgentTunnel, ParentID: &gid, Active: true, Interval: o.Interval, RetryInterval: o.Interval,
			MaxRetries: 1, Timeout: 10, Country: strings.ToLower(country),
			Config: store.MonitorConfig{AgentID: a.ID, Source: it.source, TunnelName: it.t.Name, TunnelLabel: it.t.Label,
				LatencyAlertMs: o.LatencyAlertMs, LatencyAlertFor: o.LatencyAlertFor},
			NotificationIDs: nonNil(o.NotificationIDs),
			Description:     strings.TrimSpace(fmt.Sprintf("%s · %s %s %s", label, strings.ToUpper(it.t.Direction), strings.ToUpper(it.t.Carrier), it.t.Preset)),
		}
		if err := mg.db.CreateMonitor(ctx, m); err != nil {
			return res, err
		}
		mg.Upsert(m)
		res.MonitorIDs = append(res.MonitorIDs, m.ID)
		n++
	}
	if n > 0 {
		log.Printf("agent %s: imported %d tunnel(s)", a.Name, n)
	}
	res.Tunnels = n
	return res, nil
}

// ImportOptions sets up the monitors an import creates. Nil fields mean the
// defaults: the default channels, the source's interval and the slow alert
// set for new tunnels under Settings.
type ImportOptions struct {
	// Skip lists tunnels ("source/name") not to watch; auto-import leaves them out.
	Skip            []string `json:"skip"`
	NotificationIDs []int64  `json:"notificationIds"`
	Interval        *int     `json:"interval"`
	LatencyAlertMs  *int     `json:"latencyAlertMs"`
	LatencyAlertFor *int     `json:"latencyAlertFor"`
}

// ImportResult says what an import created.
type ImportResult struct {
	Tunnels         int     `json:"imported"`
	MonitorIDs      []int64 `json:"monitorIds"`
	GroupID         int64   `json:"groupId,omitempty"`
	ServerMonitorID int64   `json:"serverMonitorId,omitempty"`
}

type importSettings struct {
	NotificationIDs []int64
	Interval        int
	LatencyAlertMs  int
	LatencyAlertFor int
}

func (mg *Manager) importOptions(ctx context.Context, o *ImportOptions, interval int) importSettings {
	s := importSettings{Interval: interval}
	if o != nil && o.NotificationIDs != nil {
		s.NotificationIDs = o.NotificationIDs
	} else {
		s.NotificationIDs, _ = mg.db.DefaultNotificationIDs(ctx)
	}
	if o != nil && o.Interval != nil && *o.Interval >= 10 {
		s.Interval = *o.Interval
	}
	if o != nil && o.LatencyAlertMs != nil {
		s.LatencyAlertMs = max(0, *o.LatencyAlertMs)
		if o.LatencyAlertFor != nil {
			s.LatencyAlertFor = max(0, *o.LatencyAlertFor)
		}
	} else {
		s.LatencyAlertMs, s.LatencyAlertFor = mg.TunnelLatencyDefault(ctx)
	}
	if s.LatencyAlertMs == 0 {
		s.LatencyAlertFor = 0
	}
	return s
}

func skipKey(agentID int64) string { return fmt.Sprintf("agentSkip.%d", agentID) }

func (mg *Manager) skippedTunnels(ctx context.Context, agentID int64) map[string]bool {
	out := map[string]bool{}
	var list []string
	if raw := mg.db.GetSetting(ctx, skipKey(agentID), ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &list)
	}
	for _, k := range list {
		out[k] = true
	}
	return out
}

func (mg *Manager) saveSkipped(ctx context.Context, agentID int64, m map[string]bool) {
	list := make([]string, 0, len(m))
	for k := range m {
		list = append(list, k)
	}
	slices.Sort(list)
	b, _ := json.Marshal(list)
	_ = mg.db.SetSettings(ctx, map[string]string{skipKey(agentID): string(b)})
}

// TunnelLatencyDefault is the slow alert new tunnels get (0 = none).
func (mg *Manager) TunnelLatencyDefault(ctx context.Context) (ms, forSec int) {
	ms, _ = strconv.Atoi(mg.db.GetSetting(ctx, "tunnelLatencyAlertMs", "0"))
	forSec, _ = strconv.Atoi(mg.db.GetSetting(ctx, "tunnelLatencyAlertFor", "120"))
	if ms <= 0 {
		return 0, 0
	}
	return ms, max(0, forSec)
}

func (mg *Manager) ensureAgentGroup(ctx context.Context, a *store.Agent) (int64, error) {
	if a.GroupMonitorID != nil {
		if g, err := mg.db.GetMonitor(ctx, *a.GroupMonitorID); err == nil && g.Type == TypeGroup {
			return g.ID, nil
		}
	}
	g := &store.Monitor{
		Name: a.Name, Type: TypeGroup, Active: true, Interval: 60, RetryInterval: 60, Country: a.Country,
		Description: "Server reporting through neo-uptime-agent", NotificationIDs: []int64{},
	}
	if err := mg.db.CreateMonitor(ctx, g); err != nil {
		return 0, err
	}
	gid := g.ID
	a.GroupMonitorID = &gid
	if err := mg.db.SaveAgent(ctx, a); err != nil {
		return 0, err
	}
	mg.Upsert(g)
	return gid, nil
}
