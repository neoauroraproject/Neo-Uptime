package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
	"github.com/neoauroraproject/Neo-Uptime/internal/monitor"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

var themes = []string{"zen", "pulse", "aurora", "clarity", "lime", "noir"}
var layouts = []string{"list", "cards"}

// ---------- admin ----------

func (s *Server) handleListStatusPages(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListStatusPages(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, list)
}

func (s *Server) handleGetStatusPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	p, err := s.db.GetStatusPage(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, p)
}

func (s *Server) handleSaveStatusPage(w http.ResponseWriter, r *http.Request) {
	var p store.StatusPage
	if err := readJSON(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p.ID = 0
	oldSlug, oldDomain := "", ""
	if id, ok := pathID(r, "id"); ok {
		existing, err := s.db.GetStatusPage(r.Context(), id)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		p.ID = existing.ID
		p.CreatedAt = existing.CreatedAt
		oldSlug, oldDomain = existing.Slug, existing.Domain
		if r.URL.Query().Get("meta") == "1" {
			// Only the settings changed; keep the groups as they are.
			p.Groups = existing.Groups
		}
	}
	if err := normPage(&p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.Domain != oldDomain {
		if c := callerFrom(r); c == nil || c.Role != roleAdmin {
			writeErr(w, http.StatusForbidden, "only an admin can change a status page's domain")
			return
		}
		if p.Domain != "" {
			if p.Domain == s.db.GetSetting(r.Context(), "panelDomain", "") {
				writeErr(w, http.StatusBadRequest, "that domain is already used by the panel")
				return
			}
			if other, ok := s.hosts.page(p.Domain); ok && other.id != p.ID {
				writeErr(w, http.StatusBadRequest, "that domain is already used by the page “"+other.title+"”")
				return
			}
		}
	}
	if err := s.db.SaveStatusPage(r.Context(), &p); err != nil {
		writeStoreErr(w, err)
		return
	}
	pageCache.drop(p.Slug)
	pageCache.drop(oldSlug)
	s.reloadHosts(r.Context())
	if p.Domain != "" && p.Domain != oldDomain && s.certs != nil {
		s.certs.Issue(p.Domain, false)
	}
	_ = s.db.PruneUploads(r.Context(), time.Now().Add(-time.Hour).Unix())
	saved, err := s.db.GetStatusPage(r.Context(), p.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, saved)
}

func (s *Server) handleDeleteStatusPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if p, err := s.db.GetStatusPage(r.Context(), id); err == nil {
		pageCache.drop(p.Slug)
		if entry := s.db.GetSetting(r.Context(), "entryPage", ""); entry == "status:"+p.Slug {
			_ = s.db.SetSettings(r.Context(), map[string]string{"entryPage": "dashboard"})
		}
	}
	if err := s.db.DeleteStatusPage(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.reloadHosts(r.Context())
	_ = s.db.PruneUploads(r.Context(), time.Now().Add(-time.Hour).Unix())
	writeJSON(w, map[string]any{"ok": true})
}

// normPage checks a page from the editor and fills in defaults.
// hideKeys are the details a status page can leave out. Hidden data is not
// sent at all, so it cannot be read from the page either.
var hideKeys = []string{
	"flag", "status", "location", "graph", "bars", // every service
	"direction", "transport", "profile", "port", "traffic", "connected", // tunnels
}

func normPage(p *store.StatusPage) error {
	p.Slug = strings.ToLower(strings.TrimSpace(p.Slug))
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" {
		return errors.New("title is required")
	}
	if !slugRe.MatchString(p.Slug) {
		return errors.New("slug may only contain lowercase letters, digits and dashes")
	}
	if !slices.Contains(themes, p.Theme) {
		p.Theme = "zen"
	}
	if p.Language != "en" && p.Language != "fa" {
		p.Language = "auto"
	}
	if !slices.Contains(layouts, p.Layout) {
		p.Layout = "list"
	}
	if p.BarMode != "days" {
		p.BarMode = "heartbeat"
	}
	if p.BarDays == 0 {
		p.BarDays = 90
	}
	p.BarDays = max(1, min(365, p.BarDays))
	if p.RefreshInterval == 0 {
		p.RefreshInterval = 60
	}
	p.RefreshInterval = max(10, min(3600, p.RefreshInterval))
	if len(p.CustomCSS) > 64<<10 {
		return errors.New("custom CSS is limited to 64 KB")
	}
	if len(p.Logo) > 2048 {
		return errors.New("the logo address is too long — upload the image instead")
	}
	d, err := normDomain(p.Domain)
	if err != nil {
		return err
	}
	p.Domain = d
	hide := []string{}
	for _, k := range hideKeys {
		if slices.Contains(p.Hide, k) {
			hide = append(hide, k)
		}
	}
	p.Hide = hide
	// Radar datacenters are slugs such as "asiatech-tehran".
	dcs := []string{}
	for _, v := range p.Radar.Datacenters {
		v = strings.TrimSpace(v)
		if v != "" && len(v) <= 64 && len(dcs) < 50 && !slices.Contains(dcs, v) {
			dcs = append(dcs, v)
		}
	}
	p.Radar.Datacenters = dcs
	if p.Groups == nil {
		p.Groups = []store.StatusPageGroup{}
	}
	return nil
}

func (s *Server) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	list, err := s.db.ListIncidents(r.Context(), id, false)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, list)
}

func (s *Server) handleSaveIncident(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var in store.Incident
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in.StatusPageID = pid
	in.ID = 0
	if iid, ok := pathID(r, "iid"); ok {
		in.ID = iid
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	if !slices.Contains([]string{"info", "warning", "danger", "success", "maintenance"}, in.Style) {
		in.Style = "warning"
	}
	if err := s.db.SaveIncident(r.Context(), &in); err != nil {
		writeStoreErr(w, err)
		return
	}
	if p, err := s.db.GetStatusPage(r.Context(), pid); err == nil {
		pageCache.drop(p.Slug)
	}
	writeJSON(w, in)
}

func (s *Server) handleDeleteIncident(w http.ResponseWriter, r *http.Request) {
	pid, ok1 := pathID(r, "id")
	iid, ok2 := pathID(r, "iid")
	if !ok1 || !ok2 {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.db.DeleteIncident(r.Context(), pid, iid); err != nil {
		writeStoreErr(w, err)
		return
	}
	if p, err := s.db.GetStatusPage(r.Context(), pid); err == nil {
		pageCache.drop(p.Slug)
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- public ----------

type publicBeat struct {
	S int      `json:"s"`
	T int64    `json:"t"`
	P *float64 `json:"p,omitempty"`
}

type publicTunnel struct {
	Source       string               `json:"source,omitempty"`
	State        string               `json:"state"`
	Direction    string               `json:"direction,omitempty"`
	Carrier      string               `json:"carrier,omitempty"`
	Preset       string               `json:"preset,omitempty"`
	TunnelPort   string               `json:"tunnelPort,omitempty"`
	Uptime       string               `json:"uptime,omitempty"`
	InBytes      *uint64              `json:"inBytes,omitempty"` // nil when traffic is hidden
	OutBytes     *uint64              `json:"outBytes,omitempty"`
	TotalBytes   *uint64              `json:"totalBytes,omitempty"`
	PeerCountry  string               `json:"peerCountry,omitempty"`
	PeerLocation string               `json:"peerLocation,omitempty"`
	KCPLoss      float64              `json:"kcpLoss,omitempty"`
	Rates        []backpack.RatePoint `json:"rates,omitempty"`
}

// publicTunnelOf is what a status page shows of a tunnel, without the
// details the page hides.
func publicTunnelOf(p *store.StatusPage, t *backpack.Tunnel) *publicTunnel {
	pt := &publicTunnel{Source: t.Source, State: t.State, KCPLoss: t.KCPLossPercent}
	if !p.Hidden("flag") {
		pt.PeerCountry = t.PeerCountry
	}
	if !p.Hidden("location") {
		pt.PeerLocation = t.PeerLocation
	}
	if !p.Hidden("direction") {
		pt.Direction = t.Direction
	}
	if !p.Hidden("transport") {
		pt.Carrier = firstSet(t.Carrier, t.Transport)
	}
	if !p.Hidden("profile") {
		pt.Preset = t.Preset
	}
	if !p.Hidden("port") {
		pt.TunnelPort = t.TunnelPort
	}
	if !p.Hidden("connected") {
		pt.Uptime = t.Uptime
	}
	if !p.Hidden("traffic") {
		in, out, total := t.InBytes, t.OutBytes, t.TotalBytes
		pt.InBytes, pt.OutBytes, pt.TotalBytes, pt.Rates = &in, &out, &total, t.Rates
	}
	return pt
}

type publicMonitor struct {
	ID       int64              `json:"id"`
	Name     string             `json:"name"`
	Type     string             `json:"type"`
	Country  string             `json:"country,omitempty"`
	URL      string             `json:"url,omitempty"`
	Tags     []store.MonitorTag `json:"tags,omitempty"`
	Status   int                `json:"status"` // -1 = no data, -2 = paused
	Ping     *float64           `json:"ping"`
	Uptime24 float64            `json:"uptime24"`
	Uptime30 float64            `json:"uptime30"`
	AvgPing  float64            `json:"avgPing"`
	CertDays *int               `json:"certDays,omitempty"`
	Tunnel   *publicTunnel      `json:"tunnel,omitempty"`
	Beats    []publicBeat       `json:"beats"`
	Days     []store.Bucket     `json:"days,omitempty"`
}

type publicGroup struct {
	Name     string          `json:"name"`
	Monitors []publicMonitor `json:"monitors"`
}

type publicMaint struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Start       int64  `json:"start,omitempty"`
	End         int64  `json:"end,omitempty"`
}

type cachedPage struct {
	at   time.Time
	body []byte
}

type pageCacheT struct {
	mu sync.Mutex
	m  map[string]cachedPage
}

var pageCache = &pageCacheT{m: map[string]cachedPage{}}

func (c *pageCacheT) get(slug string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[slug]
	if !ok || time.Since(e.at) > 10*time.Second {
		return nil, false
	}
	return e.body, true
}

func (c *pageCacheT) put(slug string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[slug] = cachedPage{at: time.Now(), body: b}
}

func (c *pageCacheT) drop(slug string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, slug)
}

func (s *Server) handlePublicStatusPage(w http.ResponseWriter, r *http.Request) {
	slug := strings.ToLower(r.PathValue("slug"))
	if body, ok := pageCache.get(slug); ok {
		writePublicJSON(w, body)
		return
	}
	ctx := r.Context()
	p, err := s.db.GetStatusPageBySlug(ctx, slug)
	if err != nil {
		writeErr(w, http.StatusNotFound, "status page not found")
		return
	}
	if !p.Published {
		if who, err := s.identify(r); err != nil || who.Kind != "session" {
			writeErr(w, http.StatusNotFound, "status page not found")
			return
		}
	}
	body, err := json.Marshal(s.publicPage(ctx, p))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "encoding failed")
		return
	}
	if p.Published {
		pageCache.put(slug, body)
	}
	writePublicJSON(w, body)
}

// writePublicJSON sends a status page. Browsers must not keep it: the page
// refreshes on its own and a stale copy would hide a fresh save. The server
// cache above absorbs the load.
func writePublicJSON(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}

// handlePreviewStatusPage renders a page from the editor's unsaved state.
func (s *Server) handlePreviewStatusPage(w http.ResponseWriter, r *http.Request) {
	var p store.StatusPage
	if err := readJSON(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// A half-typed title, slug or domain must not stop the preview.
	title := strings.TrimSpace(p.Title)
	if !slugRe.MatchString(strings.ToLower(strings.TrimSpace(p.Slug))) {
		p.Slug = "preview"
	}
	p.Title, p.Domain = "-", ""
	if err := normPage(&p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p.Title = title
	writeJSON(w, s.publicPage(r.Context(), &p))
}

// publicPage builds what a status page shows.
func (s *Server) publicPage(ctx context.Context, p *store.StatusPage) map[string]any {
	var ids []int64
	for _, g := range p.Groups {
		for _, m := range g.Monitors {
			ids = append(ids, m.MonitorID)
		}
	}
	all, _ := s.db.ListMonitors(ctx)
	byID := map[int64]*store.Monitor{}
	for _, m := range all {
		byID[m.ID] = m
	}
	u24, _ := s.db.UptimeSince(ctx, ids, 24*time.Hour)
	u30, _ := s.db.UptimeSince(ctx, ids, 30*24*time.Hour)
	beats := map[int64][]*store.Heartbeat{}
	var days map[int64][]store.Bucket
	if len(ids) > 0 {
		beats, _ = s.db.RecentHeartbeats(ctx, ids, 100)
		if p.BarMode == "days" {
			since := time.Now().UTC().AddDate(0, 0, -(max(1, p.BarDays) - 1))
			since = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, time.UTC)
			days, _ = s.db.Buckets(ctx, ids, true, since)
		}
	}
	states := s.mgr.States()

	nUp, nDown, nMaint, nTotal := 0, 0, 0, 0
	groups := []publicGroup{}
	for _, g := range p.Groups {
		pg := publicGroup{Name: g.Name, Monitors: []publicMonitor{}}
		for _, ref := range g.Monitors {
			m, ok := byID[ref.MonitorID]
			if !ok {
				continue
			}
			st := states[m.ID]
			pm := publicMonitor{ID: m.ID, Name: m.Name, Type: m.Type, Country: m.Country, Status: -1, Uptime24: -1, Uptime30: -1, AvgPing: -1, Beats: []publicBeat{}}
			if p.Hidden("flag") {
				pm.Country = ""
			}
			if ref.ShowURL {
				pm.URL = monitor.Target(m)
			}
			if p.ShowTags {
				pm.Tags = m.Tags
			}
			if st.Last != nil {
				pm.Status = st.Last.Status
				pm.Ping = st.Last.Ping
			}
			if !m.Active {
				pm.Status = -2
			}
			if u, ok := u24[m.ID]; ok {
				pm.Uptime24, pm.AvgPing = u.Ratio, u.AvgPing
			}
			if u, ok := u30[m.ID]; ok {
				pm.Uptime30 = u.Ratio
			}
			if p.ShowCertExpiry && st.Cert != nil {
				d := st.Cert.DaysRemaining
				pm.CertDays = &d
			}
			if t := st.Tunnel; t != nil {
				pm.Tunnel = publicTunnelOf(p, t)
			}
			for _, h := range beats[m.ID] {
				pm.Beats = append(pm.Beats, publicBeat{S: h.Status, T: h.Time, P: h.Ping})
			}
			if days != nil {
				pm.Days = days[m.ID]
				if pm.Days == nil {
					pm.Days = []store.Bucket{}
				}
			}
			if m.Active {
				nTotal++
				switch pm.Status {
				case store.StatusUp, store.StatusPending:
					nUp++
				case store.StatusDown:
					nDown++
				case store.StatusMaintenance:
					nMaint++
				}
			}
			pg.Monitors = append(pg.Monitors, pm)
		}
		if len(pg.Monitors) > 0 {
			groups = append(groups, pg)
		}
	}

	overall := "operational"
	switch {
	case nTotal == 0:
		overall = "unknown"
	case nDown > 0 && nDown == nTotal:
		overall = "major"
	case nDown > 0:
		overall = "partial"
	case nMaint > 0:
		overall = "maintenance"
	}

	incidents, _ := s.db.ListIncidents(ctx, p.ID, true)
	maint := []publicMaint{}
	now := time.Now()
	for _, mt := range s.mgr.Maintenances() {
		linked := slices.Contains(mt.StatusPageIDs, p.ID)
		if !linked {
			for _, id := range mt.MonitorIDs {
				if slices.Contains(ids, id) {
					linked = true
					break
				}
			}
		}
		if !linked {
			continue
		}
		status, start, end := monitor.MaintenanceStatus(mt, now)
		if status != monitor.MaintUnder && status != monitor.MaintScheduled {
			continue
		}
		pm := publicMaint{Title: mt.Title, Description: mt.Description, Status: status}
		if !start.IsZero() {
			pm.Start = start.UnixMilli()
		}
		if !end.IsZero() {
			pm.End = end.UnixMilli()
		}
		maint = append(maint, pm)
	}

	page := map[string]any{
		"slug": p.Slug, "title": p.Title, "description": p.Description, "theme": p.Theme, "layout": p.Layout,
		"barMode": p.BarMode, "accentColor": p.AccentColor, "logo": p.Logo, "footer": p.Footer,
		"customCss": p.CustomCSS, "showTags": p.ShowTags, "showCertExpiry": p.ShowCertExpiry,
		"showPoweredBy": p.ShowPoweredBy, "showCharts": p.ShowCharts, "refreshInterval": p.RefreshInterval,
		"published": p.Published, "language": p.Language, "showUptime": p.ShowUptime,
		"showLatency": p.ShowLatency, "showBanner": p.ShowBanner, "barDays": p.BarDays, "domain": p.Domain,
		"hide": p.Hide,
	}
	out := map[string]any{
		"page": page, "groups": groups, "overall": overall, "incidents": incidents,
		"maintenance": maint, "updatedAt": now.UnixMilli(),
	}
	// The radar strip comes from the summary in memory: no extra request,
	// no waiting on the radar.
	if p.Radar.Show && s.radar != nil {
		if snap := s.radar.Snapshot().Pick(p.Radar.Datacenters); snap != nil {
			out["radar"] = snap
		}
	}
	return out
}

func (s *Server) handlePublicChart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.db.GetStatusPageBySlug(ctx, strings.ToLower(r.PathValue("slug")))
	if err != nil || (!p.Published && !s.signedIn(r)) {
		writeErr(w, http.StatusNotFound, "status page not found")
		return
	}
	id, ok := pathID(r, "id")
	if !ok || !p.ShowCharts {
		writeErr(w, http.StatusNotFound, "no chart")
		return
	}
	found := false
	for _, g := range p.Groups {
		for _, m := range g.Monitors {
			if m.MonitorID == id {
				found = true
			}
		}
	}
	if !found {
		writeErr(w, http.StatusNotFound, "monitor is not on this page")
		return
	}
	rng := r.URL.Query().Get("range")
	if rng != "week" && rng != "month" {
		rng = "day"
	}
	pts, err := s.chart(ctx, id, rng)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	writeJSON(w, pts)
}

func (s *Server) signedIn(r *http.Request) bool {
	who, err := s.identify(r)
	return err == nil && who.Kind == "session"
}
