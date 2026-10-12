package monitor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
	"github.com/neoauroraproject/Neo-Uptime/internal/xray"
)

// XrayInfo describes a V2Ray/Xray config monitor for the UI. It never holds
// the link itself, which carries the account's secret.
type XrayInfo struct {
	Summary  string  `json:"summary"` // "VLESS · WS · TLS"
	Protocol string  `json:"protocol"`
	Network  string  `json:"network"`
	Security string  `json:"security"`
	Endpoint string  `json:"endpoint"`
	Connect  float64 `json:"connect,omitempty"` // ms, the first request through it
	From     string  `json:"from,omitempty"`    // the server that tested it; empty = this panel
}

func xrayInfo(l *xray.Link) *XrayInfo {
	return &XrayInfo{Summary: l.Summary(), Protocol: l.Protocol, Network: l.Network, Security: l.Security, Endpoint: l.Endpoint()}
}

func xrayTarget(link string) string {
	l, err := xray.Parse(link)
	if err != nil {
		return ""
	}
	return l.Protocol + " " + l.Endpoint()
}

// XrayDetails is the UI description of a monitor's config, nil for others.
func XrayDetails(m *store.Monitor) *XrayInfo {
	if m.Type != TypeXray {
		return nil
	}
	l, err := xray.Parse(m.Config.Link)
	if err != nil {
		return nil
	}
	return xrayInfo(l)
}

func checkXray(ctx context.Context, m *store.Monitor, env *Env) Result {
	l, err := xray.Parse(m.Config.Link)
	if err != nil {
		return down("this config cannot be read: " + err.Error())
	}
	info := xrayInfo(l)
	if m.Config.AgentID == 0 {
		r, err := xray.Test(ctx, l, m.Config.TestURL)
		if err != nil {
			return Result{Msg: err.Error(), Xray: info}
		}
		return xrayUp(info, r.Delay, r.Connect)
	}

	a, err := env.DB.GetAgent(ctx, m.Config.AgentID)
	if err != nil {
		return down("the server that tested this config no longer exists — choose another one")
	}
	info.From = a.Name
	st := env.Agents.Get(a.ID)
	if awaitingFirstReport(st) {
		return Result{Skip: true}
	}
	if age, stale := agentStale(a, st); stale {
		return Result{Msg: agentGone(a, st, age), Xray: info}
	}
	if !slices.Contains(st.Report.Caps, agentproto.CapXray) {
		return Result{Msg: fmt.Sprintf("neo-uptime-agent on %s is too old to test configs — update it from Servers", a.Name), Xray: info}
	}
	e, fresh, asked := env.Agents.x.take(m.ID)
	limit := max(3*time.Duration(m.Interval)*time.Second, 2*time.Minute)
	switch {
	case e == nil && !asked.IsZero() && time.Since(asked) > limit:
		return Result{Msg: fmt.Sprintf("%s has not sent a test result for %s", a.Name, time.Since(asked).Round(time.Second)), Xray: info}
	case e == nil:
		return Result{Skip: true}
	case !fresh && time.Since(e.got) > limit:
		return Result{Msg: fmt.Sprintf("no new test result from %s for %s", a.Name, time.Since(e.got).Round(time.Second)), Xray: info}
	case !fresh:
		return Result{Skip: true}
	case !e.res.OK:
		return Result{Msg: e.res.Msg, Xray: info}
	}
	return xrayUp(info, time.Duration(e.res.Ping*float64(time.Millisecond)), time.Duration(e.res.Connect*float64(time.Millisecond)))
}

func xrayUp(info *XrayInfo, delay, connect time.Duration) Result {
	info.Connect = float64(connect.Microseconds()) / 1000
	msg := fmt.Sprintf("%s · connected in %d ms", info.Summary, connect.Milliseconds())
	if info.From != "" {
		msg += " · from " + info.From
	}
	r := up(msg, delay)
	r.Xray = info
	return r
}

// ---------- tests and jobs on agents ----------

type probeEntry struct {
	res  agentproto.ProbeResult
	got  time.Time
	used bool
}

// xrayHub holds what agents test for the panel: the latest result of each
// recurring test, and one-off jobs waiting for an agent's next report.
type xrayHub struct {
	mu      sync.Mutex
	results map[int64]*probeEntry // by monitor
	asked   map[int64]time.Time   // when the agent first got the test
	jobs    map[int64][]agentproto.Job
	waiters map[string]chan agentproto.JobResult
}

func newXrayHub() *xrayHub {
	return &xrayHub{results: map[int64]*probeEntry{}, asked: map[int64]time.Time{}, jobs: map[int64][]agentproto.Job{}, waiters: map[string]chan agentproto.JobResult{}}
}

// take returns a monitor's latest result; fresh is true the first time.
func (h *xrayHub) take(id int64) (e *probeEntry, fresh bool, asked time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, asked = h.results[id], h.asked[id]
	if e == nil {
		return nil, false, asked
	}
	cp := *e
	fresh = !e.used
	e.used = true
	return &cp, fresh, asked
}

func (h *xrayHub) forget(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.results, id)
	delete(h.asked, id)
}

// ErrAgentTooOld: the agent predates jobs and must be updated by hand once.
var ErrAgentTooOld = errors.New("update it with the install command shown under Servers")

// RunJob hands a one-off job to an agent and waits for its answer, which
// comes with the agent's next report.
func (mg *Manager) RunJob(ctx context.Context, agentID int64, job agentproto.Job) (agentproto.JobResult, error) {
	a, err := mg.db.GetAgent(ctx, agentID)
	if err != nil {
		return agentproto.JobResult{}, errors.New("that server does not exist")
	}
	st := mg.env.Agents.Get(a.ID)
	if _, stale := agentStale(a, st); stale {
		return agentproto.JobResult{}, fmt.Errorf("%s is not reporting — is neo-uptime-agent running there?", a.Name)
	}
	if !slices.Contains(st.Report.Caps, agentproto.CapXray) {
		return agentproto.JobResult{}, fmt.Errorf("neo-uptime-agent on %s is too old for this: %w", a.Name, ErrAgentTooOld)
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	job.ID = hex.EncodeToString(b)
	ch := make(chan agentproto.JobResult, 1)
	h := mg.env.Agents.x
	h.mu.Lock()
	h.jobs[a.ID] = append(h.jobs[a.ID], job)
	h.waiters[job.ID] = ch
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.waiters, job.ID)
		q := h.jobs[a.ID][:0]
		for _, j := range h.jobs[a.ID] {
			if j.ID != job.ID {
				q = append(q, j)
			}
		}
		h.jobs[a.ID] = q
		h.mu.Unlock()
	}()
	select {
	case r := <-ch:
		return r, nil
	case <-ctx.Done():
		return agentproto.JobResult{}, fmt.Errorf("%s did not answer in time", a.Name)
	}
}

// agentReply takes an agent's test results and returns what it should run.
func (mg *Manager) agentReply(a *store.Agent, r *agentproto.Report) AgentReply {
	h := mg.env.Agents.x
	mine := map[int64]*store.Monitor{}
	mg.mu.Lock()
	for id, rn := range mg.runners {
		if rn.m.Type == TypeXray && rn.m.Config.AgentID == a.ID {
			mine[id] = rn.m
		}
	}
	mg.mu.Unlock()

	var kick []int64
	h.mu.Lock()
	for _, pr := range r.Probes {
		if mine[pr.ID] == nil {
			continue
		}
		if old := h.results[pr.ID]; old != nil && old.res.At >= pr.At {
			continue
		}
		h.results[pr.ID] = &probeEntry{res: pr, got: time.Now()}
		kick = append(kick, pr.ID)
	}
	for _, jr := range r.Jobs {
		if ch, ok := h.waiters[jr.ID]; ok {
			select {
			case ch <- jr:
			default:
			}
		}
	}
	reply := AgentReply{Jobs: h.jobs[a.ID]}
	h.jobs[a.ID] = nil
	ids := make([]int64, 0, len(mine))
	for id := range mine {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		m := mine[id]
		if _, ok := h.asked[id]; !ok {
			h.asked[id] = time.Now()
		}
		reply.Probes = append(reply.Probes, agentproto.Probe{ID: id, Kind: "xray", Link: m.Config.Link, URL: m.Config.TestURL,
			Every: max(m.Interval, 10), Timeout: max(m.Timeout, 5)})
	}
	h.mu.Unlock()
	for _, id := range kick {
		mg.Kick(id)
	}
	return reply
}

// AgentReply is sent back to an agent with the ack.
type AgentReply struct {
	Probes []agentproto.Probe
	Jobs   []agentproto.Job
}

// TestConfig tests a link once, here or on an agent.
func (mg *Manager) TestConfig(ctx context.Context, link, testURL string, agentID int64) (delay, connect time.Duration, err error) {
	l, err := xray.Parse(link)
	if err != nil {
		return 0, 0, err
	}
	if agentID == 0 {
		r, err := xray.Test(ctx, l, testURL)
		if err != nil {
			return 0, 0, err
		}
		return r.Delay, r.Connect, nil
	}
	res, err := mg.RunJob(ctx, agentID, agentproto.Job{Kind: "test", Link: link, URL: testURL, Timeout: 20})
	if err != nil {
		return 0, 0, err
	}
	if res.Error != "" {
		return 0, 0, errors.New(res.Error)
	}
	return time.Duration(res.Ping * float64(time.Millisecond)), time.Duration(res.Connect * float64(time.Millisecond)), nil
}

// ---------- subscriptions ----------

var subClient = &http.Client{Timeout: 25 * time.Second}

// FetchSubscription downloads a subscription. With a server set, it falls
// back to that server when this panel cannot get it, as happens with
// subscriptions only reachable from inside a country.
func (mg *Manager) FetchSubscription(ctx context.Context, url string, agentID int64) (*xray.Sub, string, error) {
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	s, err := xray.Fetch(pctx, subClient, url)
	cancel()
	if err == nil && len(s.Links) > 0 {
		return s, "", nil
	}
	if err == nil {
		err = errors.New("no usable configs in it")
	}
	if agentID == 0 {
		return s, "", err
	}
	a, aerr := mg.db.GetAgent(ctx, agentID)
	if aerr != nil {
		return nil, "", err
	}
	res, jerr := mg.RunJob(ctx, agentID, agentproto.Job{Kind: "fetch", URL: url, Timeout: 30})
	if jerr == nil && res.Error != "" && res.Body == "" {
		jerr = errors.New(res.Error)
	}
	if jerr != nil {
		return nil, "", fmt.Errorf("this panel: %v · %s: %v", err, a.Name, jerr)
	}
	links, errs := xray.ParseList([]byte(res.Body))
	s = &xray.Sub{Links: links, Errors: errs, Usage: xray.ParseUsage(res.Userinfo), Title: xray.ProfileTitle(res.Title)}
	if len(links) == 0 {
		return s, a.Name, fmt.Errorf("no usable configs in the subscription (fetched by %s)", a.Name)
	}
	return s, a.Name, nil
}

// KeyedLink is a config with the name it is known by in its subscription.
type KeyedLink struct {
	Key  string
	Link *xray.Link
}

// KeyLinks names each config uniquely: by its own name, numbered when two
// share one.
func KeyLinks(links []*xray.Link) []KeyedLink {
	seen := map[string]int{}
	out := make([]KeyedLink, 0, len(links))
	for _, l := range links {
		k := strings.TrimSpace(l.Name)
		if k == "" {
			k = l.Endpoint()
		}
		if len(k) > 140 {
			k = k[:140]
		}
		seen[k]++
		if n := seen[k]; n > 1 {
			k = fmt.Sprintf("%s #%d", k, n)
		}
		out = append(out, KeyedLink{Key: k, Link: l})
	}
	return out
}

// SubSyncResult says what syncing a subscription changed.
type SubSyncResult struct {
	Added      int     `json:"added"`
	Updated    int     `json:"updated"`
	Removed    int     `json:"removed"`
	Configs    int     `json:"configs"`
	MonitorIDs []int64 `json:"monitorIds"`
	GroupID    int64   `json:"groupId,omitempty"`
}

// SyncSubscription fetches a subscription and brings its monitors in step:
// configs that changed are updated, new ones added (all of them with
// auto-import, or those named in keys), and configs that left the
// subscription are paused.
func (mg *Manager) SyncSubscription(ctx context.Context, id int64, keys []string, opt *ImportOptions) (*SubSyncResult, error) {
	mg.subMu.Lock()
	defer mg.subMu.Unlock()
	x, err := mg.db.GetSubscription(ctx, id)
	if err != nil {
		return nil, err
	}
	s, via, err := mg.FetchSubscription(ctx, x.URL, x.AgentID)
	x.LastFetch = time.Now().Unix()
	if err != nil {
		x.LastError = err.Error()
		_ = mg.db.SaveSubscription(ctx, x)
		mg.pub.Publish("subscriptions", map[string]any{"changed": x.ID})
		return nil, err
	}
	x.LastError = ""
	x.Info = store.SubscriptionInfo{Title: s.Title, Configs: len(s.Links), Unusable: len(s.Errors), Via: via}
	if u := s.Usage; u != nil {
		x.Info.HasUsage, x.Info.Upload, x.Info.Download, x.Info.Total, x.Info.Expire = true, u.Upload, u.Download, u.Total, u.Expire
	}

	all, err := mg.db.ListMonitors(ctx)
	if err != nil {
		return nil, err
	}
	existing := map[string]*store.Monitor{}
	for _, m := range all {
		if m.Type == TypeXray && m.Config.SubscriptionID == x.ID {
			existing[m.Config.ConfigName] = m
		}
	}
	skipped := map[string]bool{}
	for _, k := range x.Skip {
		skipped[k] = true
	}
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
		delete(skipped, k)
	}
	if opt != nil {
		for _, k := range opt.Skip {
			skipped[k] = true
		}
	}
	x.Skip = x.Skip[:0]
	for k := range skipped {
		x.Skip = append(x.Skip, k)
	}
	slices.Sort(x.Skip)

	res := &SubSyncResult{MonitorIDs: []int64{}, Configs: len(s.Links)}
	gid, err := mg.ensureSubGroup(ctx, x)
	if err != nil {
		return nil, err
	}
	res.GroupID = gid
	o := mg.importOptions(ctx, opt, x.Interval)
	present := map[string]bool{}
	for _, kl := range KeyLinks(s.Links) {
		present[kl.Key] = true
		if m, ok := existing[kl.Key]; ok {
			changed := false
			if m.Config.Link != kl.Link.Raw {
				m.Config.Link, m.Description, changed = kl.Link.Raw, kl.Link.Summary(), true
			}
			if m.Config.Gone {
				m.Config.Gone, m.Active, changed = false, true, true
			}
			if changed {
				if err := mg.db.UpdateMonitor(ctx, m); err != nil {
					return nil, err
				}
				mg.Upsert(m)
				res.Updated++
			}
			continue
		}
		if keys != nil && !want[kl.Key] || keys == nil && (!x.AutoImport || skipped[kl.Key]) {
			continue
		}
		m := &store.Monitor{
			Name: CleanName(kl.Key), Type: TypeXray, ParentID: &gid, Active: true, Interval: o.Interval, RetryInterval: o.Interval,
			MaxRetries: 1, Timeout: x.Timeout, Country: FlagCountry(kl.Key), Description: kl.Link.Summary(),
			Config: store.MonitorConfig{Link: kl.Link.Raw, TestURL: x.TestURL, AgentID: x.AgentID, SubscriptionID: x.ID,
				ConfigName: kl.Key, LatencyAlertMs: o.LatencyAlertMs, LatencyAlertFor: o.LatencyAlertFor},
			NotificationIDs: nonNil(o.NotificationIDs),
		}
		if err := mg.db.CreateMonitor(ctx, m); err != nil {
			return nil, err
		}
		mg.Upsert(m)
		res.MonitorIDs = append(res.MonitorIDs, m.ID)
		res.Added++
	}
	for key, m := range existing {
		if present[key] || m.Config.Gone {
			continue
		}
		m.Config.Gone, m.Active = true, false
		if err := mg.db.UpdateMonitor(ctx, m); err != nil {
			return nil, err
		}
		mg.Upsert(m)
		res.Removed++
	}
	if err := mg.db.SaveSubscription(ctx, x); err != nil {
		return nil, err
	}
	if res.Added+res.Updated+res.Removed > 0 {
		log.Printf("subscription %s: %d added, %d updated, %d removed", x.Name, res.Added, res.Updated, res.Removed)
	}
	mg.pub.Publish("subscriptions", map[string]any{"changed": x.ID})
	return res, nil
}

// ApplySubscription passes a subscription's settings on to its monitors.
func (mg *Manager) ApplySubscription(ctx context.Context, x *store.Subscription) error {
	all, err := mg.db.ListMonitors(ctx)
	if err != nil {
		return err
	}
	for _, m := range all {
		if m.Type != TypeXray || m.Config.SubscriptionID != x.ID {
			continue
		}
		if m.Config.AgentID == x.AgentID && m.Config.TestURL == x.TestURL && m.Interval == x.Interval && m.Timeout == x.Timeout {
			continue
		}
		if m.Config.AgentID != x.AgentID {
			mg.env.Agents.x.forget(m.ID)
		}
		m.Config.AgentID, m.Config.TestURL, m.Interval, m.RetryInterval, m.Timeout = x.AgentID, x.TestURL, x.Interval, x.Interval, x.Timeout
		if err := mg.db.UpdateMonitor(ctx, m); err != nil {
			return err
		}
		mg.Upsert(m)
	}
	if x.GroupMonitorID != nil {
		if g, err := mg.db.GetMonitor(ctx, *x.GroupMonitorID); err == nil && g.Name != x.Name {
			g.Name = x.Name
			if err := mg.db.UpdateMonitor(ctx, g); err == nil {
				mg.Upsert(g)
			}
		}
	}
	return nil
}

func (mg *Manager) ensureSubGroup(ctx context.Context, x *store.Subscription) (int64, error) {
	if x.GroupMonitorID != nil {
		if g, err := mg.db.GetMonitor(ctx, *x.GroupMonitorID); err == nil && g.Type == TypeGroup {
			return g.ID, nil
		}
	}
	g := &store.Monitor{
		Name: x.Name, Type: TypeGroup, Active: true, Interval: 60, RetryInterval: 60,
		Description: "V2Ray subscription", NotificationIDs: []int64{},
	}
	if err := mg.db.CreateMonitor(ctx, g); err != nil {
		return 0, err
	}
	gid := g.ID
	x.GroupMonitorID = &gid
	mg.Upsert(g)
	return gid, nil
}

// subscriptionSync refreshes each subscription when it is due.
func (mg *Manager) subscriptionSync() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-mg.ctx.Done():
			return
		case <-t.C:
		}
		list, err := mg.db.ListSubscriptions(mg.ctx)
		if err != nil {
			continue
		}
		for _, x := range list {
			if x.RefreshMinutes <= 0 || time.Since(time.Unix(x.LastFetch, 0)) < time.Duration(x.RefreshMinutes)*time.Minute {
				continue
			}
			ctx, cancel := context.WithTimeout(mg.ctx, 2*time.Minute)
			if _, err := mg.SyncSubscription(ctx, x.ID, nil, nil); err != nil {
				log.Printf("subscription %s: %v", x.Name, err)
			}
			cancel()
		}
	}
}

// CleanName drops flag emoji from a config's name: the flag is shown as a
// picture already, and Windows draws emoji flags as two letters.
func CleanName(name string) string {
	out := strings.Map(func(r rune) rune {
		if r >= 0x1F1E6 && r <= 0x1F1FF {
			return -1
		}
		return r
	}, name)
	if out = strings.Join(strings.Fields(out), " "); out == "" {
		return name
	}
	return out
}

// FlagCountry reads the country from a flag emoji in a name, as config
// names often start with one ("🇩🇪 Frankfurt").
func FlagCountry(name string) string {
	var prev rune
	for _, r := range name {
		if r >= 0x1F1E6 && r <= 0x1F1FF {
			if prev != 0 {
				return strings.ToLower(string([]rune{'A' + (prev - 0x1F1E6), 'A' + (r - 0x1F1E6)}))
			}
			prev = r
			continue
		}
		prev = 0
	}
	return ""
}
