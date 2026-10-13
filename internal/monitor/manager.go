package monitor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
	"github.com/neoauroraproject/Neo-Uptime/internal/notify"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

// Publisher receives live events for connected browsers.
type Publisher interface {
	Publish(kind string, data any)
}

// State is what the manager knows about a monitor between checks.
type State struct {
	mu        sync.Mutex
	Last      *store.Heartbeat `json:"last"`
	Retries   int              `json:"retries"`
	Cert      *CertInfo        `json:"cert,omitempty"`
	Tunnel    *backpack.Tunnel `json:"tunnel,omitempty"`
	System    *backpack.System `json:"system,omitempty"`
	Agent     *AgentInfo       `json:"agent,omitempty"`
	Xray      *XrayInfo        `json:"xray,omitempty"`
	NextCheck int64            `json:"nextCheck"` // unix ms

	lastPush  time.Time
	downSince time.Time
	downCount int
	slowSince time.Time // first check above the latency limit, in this run
	slowSent  bool      // the latency alert went out and recovery is pending
	inMaint   bool
}

// Snapshot is a copy of a State safe to hand out.
type Snapshot struct {
	Last      *store.Heartbeat `json:"last"`
	Retries   int              `json:"retries"`
	Cert      *CertInfo        `json:"cert,omitempty"`
	Tunnel    *backpack.Tunnel `json:"tunnel,omitempty"`
	System    *backpack.System `json:"system,omitempty"`
	Agent     *AgentInfo       `json:"agent,omitempty"`
	Xray      *XrayInfo        `json:"xray,omitempty"`
	NextCheck int64            `json:"nextCheck"`
}

type runner struct {
	m      *store.Monitor
	cancel context.CancelFunc
	kick   chan struct{}
}

// Manager owns every running monitor.
type Manager struct {
	db  *store.DB
	env *Env
	pub Publisher

	ctx context.Context

	mu      sync.Mutex
	runners map[int64]*runner
	states  map[int64]*State
	parents map[int64]int64
	types   map[int64]string
	agentOf map[int64]int64 // monitor id -> agent id

	importMu  sync.Mutex
	subMu     sync.Mutex
	speedMu   sync.Mutex
	speeding  map[int64]bool // config monitors being speed-tested
	speedJobs map[string]*SpeedJob

	maintMu sync.RWMutex
	maints  []*store.Maintenance
}

// NewManager wires a manager. Start must be called before use.
func NewManager(db *store.DB, bp *backpack.Cache, agents *AgentHub, pub Publisher) *Manager {
	return &Manager{
		db:        db,
		env:       &Env{DB: db, Backpack: bp, Agents: agents},
		agentOf:   map[int64]int64{},
		speeding:  map[int64]bool{},
		speedJobs: map[string]*SpeedJob{},
		pub:       pub,
		runners:   map[int64]*runner{},
		states:    map[int64]*State{},
		parents:   map[int64]int64{},
		types:     map[int64]string{},
	}
}

// Start loads every monitor and begins checking the active ones.
func (mg *Manager) Start(ctx context.Context) error {
	mg.ctx = ctx
	if err := mg.ReloadMaintenance(); err != nil {
		return err
	}
	list, err := mg.db.ListMonitors(ctx)
	if err != nil {
		return err
	}
	for _, m := range list {
		mg.track(m)
	}
	for _, m := range list {
		if m.Active {
			mg.startRunner(m, time.Duration(rand.IntN(4000))*time.Millisecond)
		}
	}
	go mg.maintenanceWatcher()
	go mg.housekeeping()
	go mg.panelSync()
	go mg.subscriptionSync()
	return nil
}

func (mg *Manager) track(m *store.Monitor) {
	mg.mu.Lock()
	defer mg.mu.Unlock()
	if m.ParentID != nil {
		mg.parents[m.ID] = *m.ParentID
	} else {
		delete(mg.parents, m.ID)
	}
	mg.types[m.ID] = m.Type
	if m.Config.AgentID != 0 && (m.Type == TypeAgent || m.Type == TypeAgentTunnel) {
		mg.agentOf[m.ID] = m.Config.AgentID
	} else {
		delete(mg.agentOf, m.ID)
	}
	if _, ok := mg.states[m.ID]; !ok {
		mg.states[m.ID] = &State{}
	}
}

func (mg *Manager) state(id int64) *State {
	mg.mu.Lock()
	defer mg.mu.Unlock()
	st, ok := mg.states[id]
	if !ok {
		st = &State{}
		mg.states[id] = st
	}
	return st
}

// States returns a copy of every monitor's state.
func (mg *Manager) States() map[int64]Snapshot {
	mg.mu.Lock()
	ids := make([]int64, 0, len(mg.states))
	sts := make([]*State, 0, len(mg.states))
	for id, st := range mg.states {
		ids = append(ids, id)
		sts = append(sts, st)
	}
	mg.mu.Unlock()
	out := make(map[int64]Snapshot, len(ids))
	for i, st := range sts {
		st.mu.Lock()
		out[ids[i]] = Snapshot{Last: st.Last, Retries: st.Retries, Cert: st.Cert, Tunnel: st.Tunnel, System: st.System, Agent: st.Agent, Xray: st.Xray, NextCheck: st.NextCheck}
		st.mu.Unlock()
	}
	return out
}

// StateOf returns one monitor's state.
func (mg *Manager) StateOf(id int64) Snapshot {
	st := mg.state(id)
	st.mu.Lock()
	defer st.mu.Unlock()
	return Snapshot{Last: st.Last, Retries: st.Retries, Cert: st.Cert, Tunnel: st.Tunnel, System: st.System, Agent: st.Agent, Xray: st.Xray, NextCheck: st.NextCheck}
}

// Upsert (re)starts a monitor after it was created or changed.
func (mg *Manager) Upsert(m *store.Monitor) {
	mg.stopRunner(m.ID)
	mg.track(m)
	if m.Active {
		mg.startRunner(m, 0)
	}
	mg.pub.Publish("monitors", map[string]any{"changed": m.ID})
}

// Remove stops a deleted monitor.
func (mg *Manager) Remove(id int64) {
	mg.stopRunner(id)
	mg.mu.Lock()
	delete(mg.states, id)
	delete(mg.parents, id)
	delete(mg.types, id)
	delete(mg.agentOf, id)
	mg.mu.Unlock()
	mg.pub.Publish("monitors", map[string]any{"removed": id})
}

// Pause stops checking a monitor.
func (mg *Manager) Pause(id int64) {
	mg.stopRunner(id)
	mg.pub.Publish("monitors", map[string]any{"changed": id})
}

// Kick runs a monitor's next check now.
func (mg *Manager) Kick(id int64) {
	mg.mu.Lock()
	r := mg.runners[id]
	mg.mu.Unlock()
	if r != nil {
		select {
		case r.kick <- struct{}{}:
		default:
		}
	}
}

func (mg *Manager) stopRunner(id int64) {
	mg.mu.Lock()
	r := mg.runners[id]
	delete(mg.runners, id)
	mg.mu.Unlock()
	if r != nil {
		r.cancel()
	}
}

func (mg *Manager) startRunner(m *store.Monitor, delay time.Duration) {
	ctx, cancel := context.WithCancel(mg.ctx)
	r := &runner{m: m, cancel: cancel, kick: make(chan struct{}, 1)}
	mg.mu.Lock()
	mg.runners[m.ID] = r
	mg.mu.Unlock()

	st := mg.state(m.ID)
	st.mu.Lock()
	if st.Last == nil {
		if h, err := mg.db.LastHeartbeat(ctx, m.ID); err == nil {
			st.Last = h
			if h.Status == store.StatusDown {
				if imp, err := mg.db.LastImportant(ctx, m.ID); err == nil {
					st.downSince = time.UnixMilli(imp.Time)
				}
				st.Retries = m.MaxRetries
			}
		}
	}
	if m.Type == TypePush && st.lastPush.IsZero() {
		// Give a push monitor a full window after (re)start before calling it
		// missing, or every restart would page.
		st.lastPush = time.Now()
	}
	st.mu.Unlock()

	go mg.loop(ctx, r, delay)
}

func (mg *Manager) loop(ctx context.Context, r *runner, delay time.Duration) {
	m := r.m
	if delay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
	for {
		next := mg.tick(ctx, m)
		st := mg.state(m.ID)
		st.mu.Lock()
		st.NextCheck = time.Now().Add(next).UnixMilli()
		st.mu.Unlock()
		t := time.NewTimer(next)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		case <-r.kick:
			t.Stop()
		}
	}
}

func (mg *Manager) interval(m *store.Monitor) time.Duration {
	if m.Interval < 5 {
		return 5 * time.Second
	}
	return time.Duration(m.Interval) * time.Second
}

func (mg *Manager) retryInterval(m *store.Monitor) time.Duration {
	if m.RetryInterval < 5 {
		return mg.interval(m)
	}
	return time.Duration(m.RetryInterval) * time.Second
}

// tick runs one check and returns how long to wait before the next.
func (mg *Manager) tick(ctx context.Context, m *store.Monitor) time.Duration {
	var res Result
	switch m.Type {
	case TypePush:
		st := mg.state(m.ID)
		st.mu.Lock()
		since := time.Since(st.lastPush)
		st.mu.Unlock()
		if since < mg.interval(m) && !mg.inMaintenance(m.ID, time.Now()) {
			return mg.interval(m) - since
		}
		res = down(fmt.Sprintf("no push received in the last %s", mg.interval(m).Round(time.Second)))
	case TypeGroup:
		res = mg.groupResult(m)
	default:
		chk, ok := checkers[m.Type]
		if !ok {
			res = down("unknown monitor type " + m.Type)
			break
		}
		timeout := time.Duration(m.Timeout) * time.Second
		if timeout <= 0 {
			timeout = min(mg.interval(m)*8/10, 48*time.Second)
		}
		cctx, cancel := context.WithTimeout(ctx, timeout)
		res = safeCheck(cctx, chk, m, mg.env)
		cancel()
	}
	if ctx.Err() != nil {
		return time.Second
	}
	if res.Skip {
		return mg.interval(m)
	}
	status := mg.process(ctx, m, res)
	if status == store.StatusPending {
		return mg.retryInterval(m)
	}
	return mg.interval(m)
}

func safeCheck(ctx context.Context, chk Checker, m *store.Monitor, env *Env) (r Result) {
	defer func() {
		if p := recover(); p != nil {
			r = down(fmt.Sprintf("internal error: %v", p))
		}
	}()
	return chk(ctx, m, env)
}

// Push records a heartbeat pushed by a client (Uptime Kuma compatible).
func (mg *Manager) Push(ctx context.Context, token, status, msg string, ping *float64) error {
	m, err := mg.db.GetMonitorByPushToken(ctx, token)
	if err != nil {
		return err
	}
	if !m.Active {
		return errors.New("monitor is paused")
	}
	if m.Type != TypePush {
		return store.ErrNotFound
	}
	st := mg.state(m.ID)
	st.mu.Lock()
	st.lastPush = time.Now()
	st.mu.Unlock()
	res := Result{Up: !strings.EqualFold(status, "down"), Msg: firstNonEmpty(msg, "OK"), Ping: ping}
	mg.process(ctx, m, res)
	mg.Kick(m.ID) // restart the missing-push window from now
	return nil
}

func (mg *Manager) groupResult(m *store.Monitor) Result {
	mg.mu.Lock()
	var children []int64
	for id, p := range mg.parents {
		if p == m.ID {
			if _, running := mg.runners[id]; running {
				children = append(children, id)
			}
		}
	}
	mg.mu.Unlock()
	if len(children) == 0 {
		return Result{Up: true, Msg: "no active members"}
	}
	var downs []string
	for _, id := range children {
		st := mg.state(id)
		st.mu.Lock()
		last := st.Last
		st.mu.Unlock()
		if last != nil && last.Status == store.StatusDown {
			if cm, err := mg.db.GetMonitor(mg.ctx, id); err == nil {
				downs = append(downs, cm.Name)
			}
		}
	}
	if len(downs) > 0 {
		slices.Sort(downs)
		return down(fmt.Sprintf("%d of %d down: %s", len(downs), len(children), strings.Join(downs, ", ")))
	}
	return Result{Up: true, Msg: fmt.Sprintf("all %d members up", len(children))}
}

// isImportant follows Kuma's rules: a change between up, down and
// maintenance matters; pending in either direction does not, except that
// pending turning into down does.
func isImportant(prev *store.Heartbeat, status int) bool {
	if prev == nil {
		return true
	}
	p := prev.Status
	switch {
	case p == status:
		return false
	case p == store.StatusPending && status == store.StatusUp:
		return false
	case p == store.StatusUp && status == store.StatusPending:
		return false
	case p == store.StatusDown && status == store.StatusPending:
		return false
	}
	return true
}

func shouldNotify(prev *store.Heartbeat, status int) bool {
	if status != store.StatusUp && status != store.StatusDown {
		return false
	}
	if prev == nil {
		return status == store.StatusDown
	}
	if prev.Status == store.StatusMaintenance && status == store.StatusUp {
		return false
	}
	return true
}

func (mg *Manager) process(ctx context.Context, m *store.Monitor, res Result) int {
	st := mg.state(m.ID)
	st.mu.Lock()

	prev := st.Last
	now := time.Now()
	var status int
	msg := res.Msg
	if title, ok := mg.maintenanceTitle(m.ID, now); ok {
		status = store.StatusMaintenance
		msg = "Under maintenance: " + title
		st.Retries = 0
	} else {
		ok := res.Up
		if m.UpsideDown && m.Type != TypeGroup {
			ok = !ok
			if ok {
				msg = "upside down: " + msg
			}
		}
		switch {
		case ok:
			status = store.StatusUp
			st.Retries = 0
		case prev != nil && prev.Status == store.StatusDown:
			status = store.StatusDown
		case st.Retries < m.MaxRetries:
			st.Retries++
			status = store.StatusPending
			msg = fmt.Sprintf("%s (retry %d/%d)", msg, st.Retries, m.MaxRetries)
		default:
			status = store.StatusDown
		}
	}

	important := isImportant(prev, status)
	hb := &store.Heartbeat{
		MonitorID: m.ID,
		Status:    status,
		Time:      now.UnixMilli(),
		Msg:       truncate(msg, 500),
		Important: important,
		BytesIn:   res.BytesIn,
		BytesOut:  res.BytesOut,
	}
	if status == store.StatusUp {
		hb.Ping = res.Ping
	}
	if prev != nil {
		hb.Duration = (hb.Time - prev.Time) / 1000
	}
	if err := mg.db.InsertHeartbeat(ctx, hb); err != nil {
		log.Printf("monitor %d: saving heartbeat: %v", m.ID, err)
	}
	st.Last = hb
	if res.Cert != nil {
		st.Cert = res.Cert
	}
	if res.Tunnel != nil {
		st.Tunnel = res.Tunnel
	}
	if res.System != nil {
		st.System = res.System
	}
	if res.Agent != nil {
		st.Agent = res.Agent
	}
	if res.Xray != nil {
		st.Xray = res.Xray
	}

	notifyStatus := important && shouldNotify(prev, status)
	var downtime time.Duration
	if important {
		st.downCount = 0
		if status == store.StatusDown {
			st.downSince = now
		} else if status == store.StatusUp && !st.downSince.IsZero() {
			downtime = now.Sub(st.downSince)
			st.downSince = time.Time{}
		}
	}
	resend := false
	if !important && status == store.StatusDown && m.ResendInterval > 0 {
		st.downCount++
		if st.downCount >= m.ResendInterval {
			resend = true
			st.downCount = 0
		}
	}
	latency := mg.latencyEvent(m, st, hb, now)
	cert, tunnel, system, agentInfo, xinfo := st.Cert, st.Tunnel, st.System, st.Agent, st.Xray
	st.mu.Unlock()

	mg.pub.Publish("heartbeat", map[string]any{
		"monitorId": m.ID,
		"heartbeat": hb,
		"cert":      cert,
		"tunnel":    tunnel,
		"system":    system,
		"agent":     agentInfo,
		"xray":      xinfo,
	})

	if notifyStatus || resend {
		ev := mg.baseEvent(ctx, m)
		ev.Kind = "status"
		ev.Status = status
		ev.Msg = hb.Msg
		ev.Ping = hb.Ping
		ev.Time = now
		ev.Downtime = downtime
		go mg.sendAll(m.ID, ev)
	}
	if latency != nil {
		ev := mg.baseEvent(ctx, m)
		ev.Kind = latency.kind
		ev.Status = store.StatusUp
		ev.Ping = hb.Ping
		ev.Time = now
		ev.LatencyLimit = m.Config.LatencyAlertMs
		ev.SlowFor = latency.slowFor
		mg.pub.Publish("latency", map[string]any{"monitorId": m.ID, "kind": latency.kind, "ping": hb.Ping, "limit": m.Config.LatencyAlertMs})
		go mg.sendAll(m.ID, ev)
	}
	if res.Cert != nil && m.Config.CertExpiryNotify && status != store.StatusMaintenance {
		mg.checkCert(ctx, m, res.Cert)
	}
	if important {
		mg.mu.Lock()
		parent, hasParent := mg.parents[m.ID]
		mg.mu.Unlock()
		if hasParent {
			mg.Kick(parent)
		}
	}
	return status
}

type latencyChange struct {
	kind    string // latency | latency-ok
	slowFor time.Duration
}

// latencyEvent tracks the response time against the monitor's latency limit.
// It returns an alert once the time has stayed above the limit for the set
// duration, and a recovery once it is back under. The caller holds st.mu.
func (mg *Manager) latencyEvent(m *store.Monitor, st *State, hb *store.Heartbeat, now time.Time) *latencyChange {
	limit := m.Config.LatencyAlertMs
	if limit <= 0 || hb.Status == store.StatusMaintenance {
		st.slowSince, st.slowSent = time.Time{}, false
		return nil
	}
	if hb.Status != store.StatusUp || hb.Ping == nil {
		// Down is reported by the status alert; start the clock again later.
		st.slowSince = time.Time{}
		return nil
	}
	if *hb.Ping > float64(limit) {
		if st.slowSince.IsZero() {
			st.slowSince = now
		}
		hold := time.Duration(max(m.Config.LatencyAlertFor, 0)) * time.Second
		if !st.slowSent && now.Sub(st.slowSince) >= hold {
			st.slowSent = true
			return &latencyChange{kind: "latency", slowFor: now.Sub(st.slowSince)}
		}
		return nil
	}
	slowFor := now.Sub(st.slowSince)
	st.slowSince = time.Time{}
	if st.slowSent {
		st.slowSent = false
		return &latencyChange{kind: "latency-ok", slowFor: slowFor}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Target is the human-readable thing a monitor checks.
func Target(m *store.Monitor) string {
	c := m.Config
	switch m.Type {
	case TypeHTTP, TypeKeyword, TypeJSONQuery:
		return c.URL
	case TypeTCP:
		return fmt.Sprintf("%s:%d", c.Hostname, c.Port)
	case TypePing, TypeDNS:
		return c.Hostname
	case TypeDocker:
		return c.ContainerName
	case TypeBackpackTun, TypeAgentTunnel:
		return "tunnel " + c.TunnelName
	case TypeSMTP, TypeGRPC, TypeRadius, TypeSNMP, TypeSteam, TypeUDP, TypeTLS:
		if c.Port > 0 {
			return fmt.Sprintf("%s:%d", c.Hostname, c.Port)
		}
		return c.Hostname
	case TypeKafka, TypeMQTT:
		return c.Hostname
	case TypeWebSocket:
		return c.URL
	case TypeXray:
		return xrayTarget(c.Link)
	}
	return ""
}

func (mg *Manager) baseEvent(ctx context.Context, m *store.Monitor) notify.Event {
	base := strings.TrimRight(mg.db.GetSetting(ctx, "primaryBaseURL", ""), "/")
	ev := notify.Event{
		MonitorID:   m.ID,
		MonitorName: m.Name,
		MonitorType: m.Type,
		Target:      Target(m),
		SiteTitle:   mg.db.GetSetting(ctx, "siteTitle", "Neo Uptime"),
	}
	if tz := mg.db.GetSetting(ctx, "timezone", "auto"); tz != "auto" && tz != "" {
		ev.TZ = tz
	}
	if base != "" {
		ev.Link = base + "/dashboard/" + strconv.FormatInt(m.ID, 10)
	}
	return ev
}

func (mg *Manager) sendAll(monitorID int64, ev notify.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	list, err := mg.db.MonitorNotifications(ctx, monitorID)
	if err != nil {
		log.Printf("notifications for monitor %d: %v", monitorID, err)
		return
	}
	// Send to every channel at once, and wait: the context above ends when
	// this function returns.
	var wg sync.WaitGroup
	for _, n := range list {
		wg.Add(1)
		go func(n *store.Notification) {
			defer wg.Done()
			if err := notify.Send(ctx, n.Type, n.Config, ev); err != nil {
				log.Printf("notification %q (%s) failed: %v", n.Name, n.Type, err)
				mg.pub.Publish("notification-error", map[string]any{"notification": n.Name, "error": err.Error(), "monitorId": monitorID})
			}
		}(n)
	}
	wg.Wait()
}

// checkCert warns once per threshold per certificate.
func (mg *Manager) checkCert(ctx context.Context, m *store.Monitor, c *CertInfo) {
	days := []int{7, 14, 21}
	if raw := mg.db.GetSetting(ctx, "tlsExpiryNotifyDays", ""); raw != "" {
		days = days[:0]
		for _, p := range strings.Split(raw, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n > 0 {
				days = append(days, n)
			}
		}
	}
	slices.Sort(days)
	threshold := 0
	for _, d := range days {
		if c.DaysRemaining <= d {
			threshold = d
			break
		}
	}
	if threshold == 0 {
		return
	}
	key := fmt.Sprintf("certNotified.%d", m.ID)
	mark := fmt.Sprintf("%d:%d", c.ValidTo, threshold)
	if mg.db.GetSetting(ctx, key, "") == mark {
		return
	}
	_ = mg.db.SetSettings(ctx, map[string]string{key: mark})
	ev := mg.baseEvent(ctx, m)
	ev.Kind = "cert"
	ev.Status = store.StatusUp
	ev.CertDays = c.DaysRemaining
	ev.Time = time.Now()
	ev.Msg = fmt.Sprintf("Certificate for %s (issued by %s) expires on %s", c.Subject, c.Issuer,
		time.Unix(c.ValidTo, 0).UTC().Format("2006-01-02"))
	go mg.sendAll(m.ID, ev)
}

// ---------- maintenance ----------

// ReloadMaintenance re-reads maintenance windows and re-checks affected monitors.
func (mg *Manager) ReloadMaintenance() error {
	list, err := mg.db.ListMaintenances(context.Background())
	if err != nil {
		return err
	}
	mg.maintMu.Lock()
	mg.maints = list
	mg.maintMu.Unlock()
	return nil
}

// Maintenances returns the cached maintenance list.
func (mg *Manager) Maintenances() []*store.Maintenance {
	mg.maintMu.RLock()
	defer mg.maintMu.RUnlock()
	return mg.maints
}

func (mg *Manager) maintenanceTitle(id int64, t time.Time) (string, bool) {
	mg.mu.Lock()
	chain := []int64{id}
	for cur, depth := id, 0; depth < 10; depth++ {
		p, ok := mg.parents[cur]
		if !ok {
			break
		}
		chain = append(chain, p)
		cur = p
	}
	mg.mu.Unlock()
	mg.maintMu.RLock()
	defer mg.maintMu.RUnlock()
	for _, mt := range mg.maints {
		hit := false
		for _, c := range chain {
			if slices.Contains(mt.MonitorIDs, c) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		if s, _, _ := MaintenanceStatus(mt, t); s == MaintUnder {
			return mt.Title, true
		}
	}
	return "", false
}

func (mg *Manager) inMaintenance(id int64, t time.Time) bool {
	_, ok := mg.maintenanceTitle(id, t)
	return ok
}

// InMaintenance reports whether a monitor is in a maintenance window now.
func (mg *Manager) InMaintenance(id int64) bool { return mg.inMaintenance(id, time.Now()) }

// maintenanceWatcher re-checks a monitor the moment a window opens or closes,
// rather than at its next interval.
func (mg *Manager) maintenanceWatcher() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-mg.ctx.Done():
			return
		case <-t.C:
		}
		mg.RefreshMaintenance()
	}
}

// RefreshMaintenance kicks every monitor whose maintenance state changed.
func (mg *Manager) RefreshMaintenance() {
	mg.mu.Lock()
	ids := make([]int64, 0, len(mg.runners))
	for id := range mg.runners {
		ids = append(ids, id)
	}
	mg.mu.Unlock()
	now := time.Now()
	for _, id := range ids {
		in := mg.inMaintenance(id, now)
		st := mg.state(id)
		st.mu.Lock()
		changed := st.inMaint != in
		st.inMaint = in
		st.mu.Unlock()
		if changed {
			mg.Kick(id)
		}
	}
}

// ---------- housekeeping ----------

func (mg *Manager) housekeeping() {
	run := func() {
		ctx, cancel := context.WithTimeout(mg.ctx, 10*time.Minute)
		defer cancel()
		keep, _ := strconv.Atoi(mg.db.GetSetting(ctx, "keepDataDays", "180"))
		if n, err := mg.db.Prune(ctx, keep); err != nil {
			log.Printf("pruning history: %v", err)
		} else if n > 0 {
			log.Printf("pruned %d heartbeats older than %d days", n, keep)
		}
		mg.db.PurgeExpiredSessions(ctx)
	}
	time.Sleep(time.Minute)
	run()
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-mg.ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

// ---------- BackPack import ----------

func (mg *Manager) panelSync() {
	time.Sleep(10 * time.Second)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		panels, err := mg.db.ListPanels(mg.ctx)
		if err == nil {
			for _, p := range panels {
				if p.AutoImport {
					if _, err := mg.ImportTunnels(mg.ctx, p, nil); err != nil {
						log.Printf("backpack %s: auto-import: %v", p.Name, err)
					}
				}
			}
		}
		select {
		case <-mg.ctx.Done():
			return
		case <-t.C:
		}
	}
}

// EnsurePanelGroup makes sure a panel has its group monitor and its
// panel-health monitor, creating them on first use.
func (mg *Manager) EnsurePanelGroup(ctx context.Context, p *store.BackpackPanel) (int64, error) {
	if p.GroupMonitorID != nil {
		if g, err := mg.db.GetMonitor(ctx, *p.GroupMonitorID); err == nil && g.Type == TypeGroup {
			return g.ID, nil
		}
	}
	notifs, _ := mg.db.DefaultNotificationIDs(ctx)
	g := &store.Monitor{
		Name: p.Name, Type: TypeGroup, Active: true, Interval: 60, RetryInterval: 60,
		Description: "BackPack panel " + p.URL, NotificationIDs: []int64{},
	}
	if err := mg.db.CreateMonitor(ctx, g); err != nil {
		return 0, err
	}
	gid := g.ID
	p.GroupMonitorID = &gid
	if err := mg.db.SavePanel(ctx, p); err != nil {
		return 0, err
	}
	mg.Upsert(g)

	host := &store.Monitor{
		Name: p.Name + " · panel", Type: TypeBackpackHost, ParentID: &gid, Active: true,
		Interval: max(p.Interval, 20), RetryInterval: max(p.Interval, 20), MaxRetries: 2, Timeout: 25,
		Config: store.MonitorConfig{PanelID: p.ID}, NotificationIDs: nonNil(notifs),
		Description: "Reachability of the BackPack panel and its host's CPU and memory.",
	}
	if err := mg.db.CreateMonitor(ctx, host); err == nil {
		mg.Upsert(host)
	}
	return gid, nil
}

func nonNil(v []int64) []int64 {
	if v == nil {
		return []int64{}
	}
	return v
}

// ImportTunnels creates a monitor for each tunnel of p that has none. With
// names nil it imports every tunnel; otherwise only the named ones.
func (mg *Manager) ImportTunnels(ctx context.Context, p *store.BackpackPanel, names []string) (int, error) {
	snap := mg.env.Backpack.Get(ctx, PanelRef(p), time.Duration(max(p.Interval, 10))*time.Second)
	if snap.Err != "" {
		return 0, errors.New(snap.Err)
	}
	existing, err := mg.db.ListMonitors(ctx)
	if err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, m := range existing {
		if m.Type == TypeBackpackTun && m.Config.PanelID == p.ID {
			have[m.Config.TunnelName] = true
		}
	}
	var todo []backpack.Tunnel
	for _, t := range snap.Tunnels {
		if have[t.Name] {
			continue
		}
		if names != nil && !slices.Contains(names, t.Name) {
			continue
		}
		todo = append(todo, t)
	}
	if len(todo) == 0 {
		return 0, nil
	}
	gid, err := mg.EnsurePanelGroup(ctx, p)
	if err != nil {
		return 0, err
	}
	notifs, _ := mg.db.DefaultNotificationIDs(ctx)
	slowMs, slowFor := mg.TunnelLatencyDefault(ctx)
	n := 0
	for _, t := range todo {
		desc := strings.TrimSpace(strings.Join([]string{strings.ToUpper(t.Direction), strings.ToUpper(t.Carrier), t.Preset}, " "))
		m := &store.Monitor{
			Name: t.Name, Type: TypeBackpackTun, ParentID: &gid, Active: true, Country: strings.ToLower(t.PeerCountry),
			Interval: max(p.Interval, 20), RetryInterval: max(p.Interval, 20), MaxRetries: 1, Timeout: 25,
			Config:          store.MonitorConfig{PanelID: p.ID, TunnelName: t.Name, LatencyAlertMs: slowMs, LatencyAlertFor: slowFor},
			NotificationIDs: nonNil(notifs), Description: desc,
		}
		if err := mg.db.CreateMonitor(ctx, m); err != nil {
			return n, err
		}
		mg.Upsert(m)
		n++
	}
	log.Printf("backpack %s: imported %d tunnel(s)", p.Name, n)
	return n, nil
}
