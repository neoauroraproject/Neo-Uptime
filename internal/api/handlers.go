package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/auth"
	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
	"github.com/neoauroraproject/Neo-Uptime/internal/monitor"
	"github.com/neoauroraproject/Neo-Uptime/internal/notify"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
	"github.com/neoauroraproject/Neo-Uptime/internal/version"
)

// ---------- tags ----------

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListTags(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, list)
}

func (s *Server) handleSaveTag(w http.ResponseWriter, r *http.Request) {
	var t store.Tag
	if err := readJSON(r, &t); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || len(t.Name) > 40 {
		writeErr(w, http.StatusBadRequest, "tag name must be 1–40 characters")
		return
	}
	if t.Color == "" {
		t.Color = "#22c55e"
	}
	var err error
	if id, ok := pathID(r, "id"); ok {
		t.ID = id
		err = s.db.UpdateTag(r.Context(), &t)
	} else {
		err = s.db.CreateTag(r.Context(), &t)
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.hub.Publish("monitors", map[string]any{"tags": true})
	writeJSON(w, t)
}

func (s *Server) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.db.DeleteTag(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.hub.Publish("monitors", map[string]any{"tags": true})
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- notifications ----------

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, notify.Providers())
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListNotifications(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if who := callerFrom(r); who.Role < roleEditor {
		for _, n := range list {
			n.Config = map[string]string{}
		}
	}
	writeJSON(w, list)
}

func (s *Server) handleSaveNotification(w http.ResponseWriter, r *http.Request) {
	var in struct {
		store.Notification
		ApplyExisting bool `json:"applyExisting"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	n := in.Notification
	n.ID = 0
	if id, ok := pathID(r, "id"); ok {
		n.ID = id
	}
	n.Name = strings.TrimSpace(n.Name)
	if n.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	valid := false
	for _, p := range notify.Providers() {
		if p.Type == n.Type {
			valid = true
		}
	}
	if !valid {
		writeErr(w, http.StatusBadRequest, "unknown notification type")
		return
	}
	if n.Config == nil {
		n.Config = map[string]string{}
	}
	if err := s.db.SaveNotification(r.Context(), &n); err != nil {
		writeStoreErr(w, err)
		return
	}
	if in.ApplyExisting {
		if err := s.db.LinkNotificationToAll(r.Context(), n.ID); err != nil {
			writeStoreErr(w, err)
			return
		}
		s.hub.Publish("monitors", map[string]any{"notifications": true})
	}
	writeJSON(w, n)
}

func (s *Server) handleDeleteNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.db.DeleteNotification(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID     int64             `json:"id"`
		Type   string            `json:"type"`
		Config map[string]string `json:"config"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ev := notify.Event{
		Kind: "test", MonitorName: "Test", Status: notify.StatusUp, Time: time.Now(),
		SiteTitle: s.db.GetSetting(r.Context(), "siteTitle", "Neo Uptime"),
	}
	if err := notify.Send(r.Context(), in.Type, in.Config, ev); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "msg": "sent"})
}

// ---------- maintenance ----------

type maintView struct {
	*store.Maintenance
	Status    string `json:"status"`
	NextStart int64  `json:"nextStart,omitempty"`
	NextEnd   int64  `json:"nextEnd,omitempty"`
}

func (s *Server) handleListMaintenance(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListMaintenances(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	now := time.Now()
	out := make([]maintView, 0, len(list))
	for _, m := range list {
		st, start, end := monitor.MaintenanceStatus(m, now)
		v := maintView{Maintenance: m, Status: st}
		if !start.IsZero() {
			v.NextStart = start.UnixMilli()
		}
		if !end.IsZero() {
			v.NextEnd = end.UnixMilli()
		}
		out = append(out, v)
	}
	writeJSON(w, out)
}

func validHM(s string) bool {
	h, m, ok := strings.Cut(s, ":")
	hh, e1 := strconv.Atoi(h)
	mm, e2 := strconv.Atoi(m)
	return ok && e1 == nil && e2 == nil && hh >= 0 && hh < 24 && mm >= 0 && mm < 60
}

func (s *Server) handleSaveMaintenance(w http.ResponseWriter, r *http.Request) {
	var m store.Maintenance
	if err := readJSON(r, &m); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	m.ID = 0
	if id, ok := pathID(r, "id"); ok {
		m.ID = id
	}
	m.Title = strings.TrimSpace(m.Title)
	if m.Title == "" {
		writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	switch m.Strategy {
	case "manual":
	case "single":
		if m.StartAt == 0 || m.EndAt == 0 || m.EndAt <= m.StartAt {
			writeErr(w, http.StatusBadRequest, "choose a start and an end after it")
			return
		}
	case "weekly", "monthly":
		if !validHM(m.TimeFrom) || !validHM(m.TimeTo) {
			writeErr(w, http.StatusBadRequest, "times must be HH:MM")
			return
		}
		if m.Strategy == "weekly" && len(m.Weekdays) == 0 {
			writeErr(w, http.StatusBadRequest, "choose at least one weekday")
			return
		}
		if m.Strategy == "monthly" && len(m.DaysOfMonth) == 0 {
			writeErr(w, http.StatusBadRequest, "choose at least one day of the month")
			return
		}
		if _, err := time.LoadLocation(m.Timezone); err != nil || m.Timezone == "" {
			m.Timezone = "UTC"
		}
	default:
		writeErr(w, http.StatusBadRequest, "strategy must be manual, single, weekly or monthly")
		return
	}
	if m.Weekdays == nil {
		m.Weekdays = []int{}
	}
	if m.DaysOfMonth == nil {
		m.DaysOfMonth = []int{}
	}
	if m.MonitorIDs == nil {
		m.MonitorIDs = []int64{}
	}
	if m.StatusPageIDs == nil {
		m.StatusPageIDs = []int64{}
	}
	if m.ID == 0 {
		m.Active = true
	}
	if err := s.db.SaveMaintenance(r.Context(), &m); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.maintenanceChanged()
	writeJSON(w, m)
}

func (s *Server) maintenanceChanged() {
	_ = s.mgr.ReloadMaintenance()
	s.mgr.RefreshMaintenance()
	pageCache.mu.Lock()
	pageCache.m = map[string]cachedPage{}
	pageCache.mu.Unlock()
	s.hub.Publish("maintenance", map[string]any{})
}

func (s *Server) handleDeleteMaintenance(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.db.DeleteMaintenance(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.maintenanceChanged()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) toggleMaintenance(w http.ResponseWriter, r *http.Request, active bool) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.db.SetMaintenanceActive(r.Context(), id, active); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.maintenanceChanged()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handlePauseMaintenance(w http.ResponseWriter, r *http.Request) {
	s.toggleMaintenance(w, r, false)
}
func (s *Server) handleResumeMaintenance(w http.ResponseWriter, r *http.Request) {
	s.toggleMaintenance(w, r, true)
}

// ---------- BackPack ----------

type panelView struct {
	*store.BackpackPanel
	HasToken  bool    `json:"hasToken"`
	Tunnels   int     `json:"tunnels"`
	Online    int     `json:"online"`
	Error     string  `json:"error,omitempty"`
	FetchedAt int64   `json:"fetchedAt,omitempty"`
	LatencyMs float64 `json:"latencyMs"`
	Hostname  string  `json:"hostname,omitempty"`
	Version   string  `json:"version,omitempty"`
}

func (s *Server) panelView(p *store.BackpackPanel) panelView {
	c := *p
	v := panelView{BackpackPanel: &c, HasToken: p.Token != ""}
	c.Token = ""
	if snap := s.bp.Peek(p.ID); snap != nil {
		v.Error = snap.Err
		v.FetchedAt = snap.FetchedAt.UnixMilli()
		v.LatencyMs = snap.LatencyMs
		v.Tunnels = len(snap.Tunnels)
		for _, t := range snap.Tunnels {
			if t.State == "online" {
				v.Online++
			}
		}
		if snap.System != nil {
			v.Hostname, v.Version = snap.System.Hostname, snap.System.Version
		}
	}
	return v
}

func (s *Server) handleListPanels(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListPanels(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]panelView, 0, len(list))
	for _, p := range list {
		out = append(out, s.panelView(p))
	}
	writeJSON(w, out)
}

func normalizePanelURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("panel URL must look like https://panel.example.com:7777")
	}
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimRight(u.String(), "/"), nil
}

func (s *Server) handleTestPanel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID        int64  `json:"id"`
		URL       string `json:"url"`
		Token     string `json:"token"`
		IgnoreTLS bool   `json:"ignoreTls"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := normalizePanelURL(in.URL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	token := in.Token
	if token == "" && in.ID > 0 {
		if p, err := s.db.GetPanel(r.Context(), in.ID); err == nil {
			token = p.Token
		}
	}
	snap := backpack.Fetch(r.Context(), backpack.PanelRef{URL: u, Token: token, IgnoreTLS: in.IgnoreTLS})
	if snap.Err != "" {
		writeErr(w, http.StatusBadGateway, snap.Err)
		return
	}
	writeJSON(w, snap)
}

func (s *Server) handleSavePanel(w http.ResponseWriter, r *http.Request) {
	var in store.BackpackPanel
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in.ID = 0
	if id, ok := pathID(r, "id"); ok {
		existing, err := s.db.GetPanel(r.Context(), id)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		in.ID = id
		in.GroupMonitorID = existing.GroupMonitorID
		in.CreatedAt = existing.CreatedAt
		if in.Token == "" {
			in.Token = existing.Token
		}
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	u, err := normalizePanelURL(in.URL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in.URL = u
	if strings.TrimSpace(in.Token) == "" {
		writeErr(w, http.StatusBadRequest, "an API token is required — create a read token in BackPack under Settings → Security → API tokens")
		return
	}
	if in.Interval == 0 {
		in.Interval = 30
	}
	in.Interval = max(20, min(3600, in.Interval))
	if err := s.db.SavePanel(r.Context(), &in); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.bp.Forget(in.ID)
	if in.AutoImport {
		p := in
		go func() {
			ctx, cancel := bgCtx()
			defer cancel()
			_, _ = s.mgr.ImportTunnels(ctx, &p, nil)
		}()
	}
	writeJSON(w, s.panelView(&in))
}

func (s *Server) handleDeletePanel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	ctx := r.Context()
	if r.URL.Query().Get("deleteMonitors") == "1" {
		list, err := s.db.ListMonitors(ctx)
		if err == nil {
			p, _ := s.db.GetPanel(ctx, id)
			for _, m := range list {
				mine := (m.Type == monitor.TypeBackpackTun || m.Type == monitor.TypeBackpackHost) && m.Config.PanelID == id
				isGroup := p != nil && p.GroupMonitorID != nil && *p.GroupMonitorID == m.ID
				if mine || isGroup {
					if err := s.db.DeleteMonitor(ctx, m.ID); err == nil {
						s.mgr.Remove(m.ID)
					}
				}
			}
		}
	}
	if err := s.db.DeletePanel(ctx, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.bp.Forget(id)
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handlePanelSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	p, err := s.db.GetPanel(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	maxAge := 15 * time.Second
	if r.URL.Query().Get("refresh") == "1" {
		maxAge = 0
	}
	snap := s.bp.Get(r.Context(), monitor.PanelRef(p), maxAge)
	imported := map[string]int64{}
	if list, err := s.db.ListMonitors(r.Context()); err == nil {
		for _, m := range list {
			if m.Type == monitor.TypeBackpackTun && m.Config.PanelID == id {
				imported[m.Config.TunnelName] = m.ID
			}
		}
	}
	writeJSON(w, map[string]any{"panel": s.panelView(p), "snapshot": snap, "imported": imported})
}

func (s *Server) handleImportTunnels(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var in struct {
		Names []string `json:"names"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := s.db.GetPanel(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	n, err := s.mgr.ImportTunnels(r.Context(), p, in.Names)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"imported": n})
}

// ---------- settings ----------

var settingDefaults = map[string]string{
	"siteTitle":           "Neo Uptime",
	"primaryBaseURL":      "",
	"entryPage":           "dashboard",
	"keepDataDays":        "180",
	"tlsExpiryNotifyDays": "7,14,21",
	"searchEngineIndex":   "false",
	"trustProxy":          "false",
	"timezone":            "auto",
}

func (s *Server) settings(ctx context.Context) map[string]string {
	stored, _ := s.db.AllSettings(ctx)
	out := map[string]string{}
	for k, def := range settingDefaults {
		if v, ok := stored[k]; ok {
			out[k] = v
		} else {
			out[k] = def
		}
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.settings(r.Context()))
}

func (s *Server) handlePublicSettings(w http.ResponseWriter, r *http.Request) {
	all := s.settings(r.Context())
	writeJSON(w, map[string]string{
		"siteTitle": all["siteTitle"], "primaryBaseURL": all["primaryBaseURL"],
		"timezone": all["timezone"], "entryPage": all["entryPage"],
	})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	clean := map[string]string{}
	for k, v := range in {
		if _, ok := settingDefaults[k]; !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "keepDataDays":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 3650 {
				writeErr(w, http.StatusBadRequest, "keep history for 1 to 3650 days")
				return
			}
		case "primaryBaseURL":
			if v != "" {
				u, err := url.Parse(v)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					writeErr(w, http.StatusBadRequest, "primary URL must start with http:// or https://")
					return
				}
				v = strings.TrimRight(v, "/")
			}
		case "siteTitle":
			if v == "" {
				v = "Neo Uptime"
			}
		}
		clean[k] = v
	}
	if err := s.db.SetSettings(r.Context(), clean); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, s.settings(r.Context()))
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	size, _ := s.db.DatabaseSize(r.Context())
	list, _ := s.db.ListMonitors(r.Context())
	writeJSON(w, map[string]any{
		"version": version.Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"startedAt": s.started.UnixMilli(), "dbSize": size, "dataDir": s.dataDir,
		"monitors": len(list), "liveClients": s.hub.Clients(), "repo": version.Repo,
	})
}

func (s *Server) handleClearStatistics(w http.ResponseWriter, r *http.Request) {
	if err := s.db.ClearAllStatistics(r.Context()); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleVacuum(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Vacuum(r.Context()); err != nil {
		writeStoreErr(w, err)
		return
	}
	size, _ := s.db.DatabaseSize(r.Context())
	writeJSON(w, map[string]any{"ok": true, "dbSize": size})
}

// ---------- users ----------

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListUsers(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, list)
}

func (s *Server) countAdmins(ctx context.Context) int {
	list, _ := s.db.ListUsers(ctx)
	n := 0
	for _, u := range list {
		if u.Role == "admin" {
			n++
		}
	}
	return n
}

func (s *Server) handleSaveUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		Role      string `json:"role"`
		ResetTOTP bool   `json:"resetTotp"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !slices.Contains([]string{"admin", "editor", "viewer"}, in.Role) {
		writeErr(w, http.StatusBadRequest, "role must be admin, editor or viewer")
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	if err := validUsername(in.Username); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	if id, ok := pathID(r, "id"); ok {
		u, err := s.db.GetUser(ctx, id)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		if u.Role == "admin" && in.Role != "admin" && s.countAdmins(ctx) <= 1 {
			writeErr(w, http.StatusBadRequest, "there must be at least one admin")
			return
		}
		if u.IsOwner && in.Role != "admin" {
			writeErr(w, http.StatusBadRequest, "the owner account is always an admin")
			return
		}
		if u.IsOwner && !callerFrom(r).IsOwner && in.Password != "" {
			writeErr(w, http.StatusForbidden, "only the owner can change the owner's password")
			return
		}
		u.Username, u.Role = in.Username, in.Role
		if in.Password != "" {
			if err := auth.ValidatePassword(in.Password); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			u.PasswordHash, _ = auth.HashPassword(in.Password)
			_ = s.db.DeleteUserSessions(ctx, u.ID, callerFrom(r).SessionHash)
		}
		if in.ResetTOTP {
			u.TOTPEnabled, u.TOTPSecret = false, ""
		}
		if err := s.db.UpdateUser(ctx, u); err != nil {
			writeStoreErr(w, err)
			return
		}
		writeJSON(w, u)
		return
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, _ := auth.HashPassword(in.Password)
	u := &store.User{Username: in.Username, PasswordHash: hash, Role: in.Role}
	if err := s.db.CreateUser(ctx, u); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, u)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if id == callerFrom(r).UserID {
		writeErr(w, http.StatusBadRequest, "you cannot delete your own account")
		return
	}
	u, err := s.db.GetUser(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if u.Role == "admin" && s.countAdmins(r.Context()) <= 1 {
		writeErr(w, http.StatusBadRequest, "there must be at least one admin")
		return
	}
	if u.IsOwner {
		writeErr(w, http.StatusBadRequest, "the owner account cannot be deleted")
		return
	}
	if err := s.db.DeleteUser(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- API keys ----------

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListAPIKeys(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if list == nil {
		list = []*store.APIKey{}
	}
	writeJSON(w, list)
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Scope       string `json:"scope"`
		ExpiresDays int    `json:"expiresDays"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeErr(w, http.StatusBadRequest, "give the key a name, e.g. \"prometheus\"")
		return
	}
	if in.Scope != "write" {
		in.Scope = "read"
	}
	secret := "nu_" + auth.RandomToken(24)
	k := &store.APIKey{Name: in.Name, Prefix: secret[:9], KeyHash: auth.HashToken(secret), Scope: in.Scope, UserID: callerFrom(r).UserID}
	if in.ExpiresDays > 0 {
		k.ExpiresAt = time.Now().AddDate(0, 0, in.ExpiresDays).Unix()
	}
	if err := s.db.CreateAPIKey(r.Context(), k); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"key": k, "secret": secret})
}

func (s *Server) handleToggleAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var active bool
	switch r.PathValue("action") {
	case "enable":
		active = true
	case "disable":
	default:
		writeErr(w, http.StatusNotFound, "unknown action")
		return
	}
	if err := s.db.SetAPIKeyActive(r.Context(), id, active); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.db.DeleteAPIKey(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- push ----------

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var ping *float64
	if v, err := strconv.ParseFloat(q.Get("ping"), 64); err == nil && v >= 0 {
		ping = &v
	}
	status := q.Get("status")
	if status == "" {
		status = "up"
	}
	msg := q.Get("msg")
	if len(msg) > 250 {
		msg = msg[:250]
	}
	if err := s.mgr.Push(r.Context(), r.PathValue("token"), status, msg, ping); err != nil {
		code := http.StatusNotFound
		text := "monitor not found or not active"
		if !strings.Contains(err.Error(), "not found") {
			text = err.Error()
		}
		writeStatus(w, code, map[string]any{"ok": false, "msg": text})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
