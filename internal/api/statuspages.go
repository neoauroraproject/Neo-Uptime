package api

import (
	"encoding/json"
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

var themes = []string{"aurora", "clarity", "lime", "noir"}
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
	if id, ok := pathID(r, "id"); ok {
		existing, err := s.db.GetStatusPage(r.Context(), id)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		p.ID = existing.ID
		p.CreatedAt = existing.CreatedAt
		if r.URL.Query().Get("meta") == "1" {
			// Only the settings changed; keep the groups as they are.
			p.Groups = existing.Groups
		}
	}
	p.Slug = strings.ToLower(strings.TrimSpace(p.Slug))
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" {
		writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	if !slugRe.MatchString(p.Slug) {
		writeErr(w, http.StatusBadRequest, "slug may only contain lowercase letters, digits and dashes")
		return
	}
	if !slices.Contains(themes, p.Theme) {
		p.Theme = "aurora"
	}
	if !slices.Contains(layouts, p.Layout) {
		p.Layout = "list"
	}
	if p.BarMode != "days" {
		p.BarMode = "heartbeat"
	}
	if p.RefreshInterval == 0 {
		p.RefreshInterval = 60
	}
	p.RefreshInterval = max(10, min(3600, p.RefreshInterval))
	if len(p.CustomCSS) > 64<<10 {
		writeErr(w, http.StatusBadRequest, "custom CSS is limited to 64 KB")
		return
	}
	if p.Groups == nil {
		p.Groups = []store.StatusPageGroup{}
	}
	if err := s.db.SaveStatusPage(r.Context(), &p); err != nil {
		writeStoreErr(w, err)
		return
	}
	pageCache.drop(p.Slug)
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
	writeJSON(w, map[string]any{"ok": true})
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
	State        string               `json:"state"`
	Direction    string               `json:"direction"`
	Carrier      string               `json:"carrier"`
	Preset       string               `json:"preset,omitempty"`
	TunnelPort   string               `json:"tunnelPort,omitempty"`
	Uptime       string               `json:"uptime,omitempty"`
	InBytes      uint64               `json:"inBytes"`
	OutBytes     uint64               `json:"outBytes"`
	TotalBytes   uint64               `json:"totalBytes"`
	PeerCountry  string               `json:"peerCountry,omitempty"`
	PeerLocation string               `json:"peerLocation,omitempty"`
	KCPLoss      float64              `json:"kcpLoss,omitempty"`
	Rates        []backpack.RatePoint `json:"rates,omitempty"`
}

type publicMonitor struct {
	ID       int64              `json:"id"`
	Name     string             `json:"name"`
	Type     string             `json:"type"`
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=10")
		_, _ = w.Write(body)
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

	var ids []int64
	for _, g := range p.Groups {
		for _, m := range g.Monitors {
			ids = append(ids, m.MonitorID)
		}
	}
	all, err := s.db.ListMonitors(ctx)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
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
			since := time.Now().UTC().AddDate(0, 0, -89)
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
			pm := publicMonitor{ID: m.ID, Name: m.Name, Type: m.Type, Status: -1, Uptime24: -1, Uptime30: -1, AvgPing: -1, Beats: []publicBeat{}}
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
				pm.Tunnel = &publicTunnel{
					State: t.State, Direction: t.Direction, Carrier: t.Carrier, Preset: t.Preset,
					TunnelPort: t.TunnelPort, Uptime: t.Uptime, InBytes: t.InBytes, OutBytes: t.OutBytes,
					TotalBytes: t.TotalBytes, PeerCountry: t.PeerCountry, PeerLocation: t.PeerLocation,
					KCPLoss: t.KCPLossPercent, Rates: t.Rates,
				}
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
		groups = append(groups, pg)
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
		"published": p.Published,
	}
	body, err := json.Marshal(map[string]any{
		"page": page, "groups": groups, "overall": overall, "incidents": incidents,
		"maintenance": maint, "updatedAt": now.UnixMilli(),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "encoding failed")
		return
	}
	if p.Published {
		pageCache.put(slug, body)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=10")
	_, _ = w.Write(body)
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
