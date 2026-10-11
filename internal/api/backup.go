package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/store"
	"github.com/neoauroraproject/Neo-Uptime/internal/version"
)

// backupFile is the export format. Ids are the exporting instance's; import
// renumbers everything and rewrites every reference.
type backupFile struct {
	Format        string                 `json:"format"`
	Version       string                 `json:"version"`
	ExportedAt    int64                  `json:"exportedAt"`
	Settings      map[string]string      `json:"settings"`
	Tags          []*store.Tag           `json:"tags"`
	Notifications []*store.Notification  `json:"notifications"`
	Panels        []*store.BackpackPanel `json:"backpackPanels"`
	Monitors      []*store.Monitor       `json:"monitors"`
	StatusPages   []*store.StatusPage    `json:"statusPages"`
	Incidents     []*store.Incident      `json:"incidents"`
	Maintenances  []*store.Maintenance   `json:"maintenances"`
	Uploads       []*store.Upload        `json:"uploads,omitempty"`
}

const backupFormat = "neo-uptime-backup/1"

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b := backupFile{Format: backupFormat, Version: version.Version, ExportedAt: time.Now().Unix()}
	var err error
	b.Settings = s.settings(ctx)
	if b.Tags, err = s.db.ListTags(ctx); err != nil {
		writeStoreErr(w, err)
		return
	}
	if b.Notifications, err = s.db.ListNotifications(ctx); err != nil {
		writeStoreErr(w, err)
		return
	}
	if b.Panels, err = s.db.ListPanels(ctx); err != nil {
		writeStoreErr(w, err)
		return
	}
	if b.Monitors, err = s.db.ListMonitors(ctx); err != nil {
		writeStoreErr(w, err)
		return
	}
	pages, err := s.db.ListStatusPages(ctx)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	for _, p := range pages {
		full, err := s.db.GetStatusPage(ctx, p.ID)
		if err != nil {
			continue
		}
		b.StatusPages = append(b.StatusPages, full)
		inc, _ := s.db.ListIncidents(ctx, p.ID, false)
		b.Incidents = append(b.Incidents, inc...)
	}
	if b.Maintenances, err = s.db.ListMaintenances(ctx); err != nil {
		writeStoreErr(w, err)
		return
	}
	if b.Uploads, err = s.db.UsedUploads(ctx); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="neo-uptime-backup-%s.json"`, time.Now().Format("2006-01-02")))
	writeJSON(w, b)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode string     `json:"mode"` // merge | replace
		Data backupFile `json:"data"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Data.Format != backupFormat {
		writeErr(w, http.StatusBadRequest, "this is not a Neo Uptime backup file")
		return
	}
	ctx := r.Context()
	if in.Mode == "replace" {
		if err := s.wipe(ctx); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	counts, err := s.restore(ctx, &in.Data)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "import stopped part-way: "+err.Error())
		return
	}
	pageCache.mu.Lock()
	pageCache.m = map[string]cachedPage{}
	pageCache.mu.Unlock()
	_ = s.mgr.ReloadMaintenance()
	s.applyHTTPS(ctx)
	s.hub.Publish("monitors", map[string]any{"imported": true})
	writeJSON(w, map[string]any{"ok": true, "imported": counts})
}

func (s *Server) wipe(ctx context.Context) error {
	list, err := s.db.ListMonitors(ctx)
	if err != nil {
		return err
	}
	for _, m := range list {
		if err := s.db.DeleteMonitor(ctx, m.ID); err != nil {
			return err
		}
		s.mgr.Remove(m.ID)
	}
	pages, _ := s.db.ListStatusPages(ctx)
	for _, p := range pages {
		_ = s.db.DeleteStatusPage(ctx, p.ID)
	}
	notifs, _ := s.db.ListNotifications(ctx)
	for _, n := range notifs {
		_ = s.db.DeleteNotification(ctx, n.ID)
	}
	tags, _ := s.db.ListTags(ctx)
	for _, t := range tags {
		_ = s.db.DeleteTag(ctx, t.ID)
	}
	maints, _ := s.db.ListMaintenances(ctx)
	for _, m := range maints {
		_ = s.db.DeleteMaintenance(ctx, m.ID)
	}
	panels, _ := s.db.ListPanels(ctx)
	for _, p := range panels {
		_ = s.db.DeletePanel(ctx, p.ID)
		s.bp.Forget(p.ID)
	}
	return nil
}

func (s *Server) restore(ctx context.Context, b *backupFile) (map[string]int, error) {
	counts := map[string]int{}
	if len(b.Settings) > 0 {
		clean := map[string]string{}
		for k, v := range b.Settings {
			if _, ok := settingDefaults[k]; ok {
				clean[k] = v
			}
		}
		if err := s.db.SetSettings(ctx, clean); err != nil {
			return counts, err
		}
	}

	tagIDs := map[int64]int64{}
	existingTags, _ := s.db.ListTags(ctx)
	byName := map[string]int64{}
	for _, t := range existingTags {
		byName[t.Name] = t.ID
	}
	for _, t := range b.Tags {
		if id, ok := byName[t.Name]; ok {
			tagIDs[t.ID] = id
			continue
		}
		old := t.ID
		t.ID = 0
		if err := s.db.CreateTag(ctx, t); err != nil {
			return counts, fmt.Errorf("tag %s: %w", t.Name, err)
		}
		tagIDs[old] = t.ID
		counts["tags"]++
	}

	notifIDs := map[int64]int64{}
	for _, n := range b.Notifications {
		old := n.ID
		n.ID = 0
		if err := s.db.SaveNotification(ctx, n); err != nil {
			return counts, fmt.Errorf("notification %s: %w", n.Name, err)
		}
		notifIDs[old] = n.ID
		counts["notifications"]++
	}

	panelIDs := map[int64]int64{}
	panelGroups := map[int64]int64{} // new panel id -> old group monitor id
	for _, p := range b.Panels {
		old := p.ID
		if p.GroupMonitorID != nil {
			panelGroups[old] = *p.GroupMonitorID
		}
		p.ID, p.GroupMonitorID = 0, nil
		if err := s.db.SavePanel(ctx, p); err != nil {
			return counts, fmt.Errorf("panel %s: %w", p.Name, err)
		}
		panelIDs[old] = p.ID
		counts["backpackPanels"]++
	}

	// Monitors in two passes: create them all, then fix parents, since a child
	// may come before its group in the file.
	monIDs := map[int64]int64{}
	parents := map[int64]int64{}
	var created []*store.Monitor
	for _, m := range b.Monitors {
		old := m.ID
		if m.ParentID != nil {
			parents[old] = *m.ParentID
		}
		m.ID, m.ParentID = 0, nil
		if m.Config.PanelID != 0 {
			m.Config.PanelID = panelIDs[m.Config.PanelID]
		}
		var tags []store.MonitorTag
		for _, t := range m.Tags {
			if nid, ok := tagIDs[t.TagID]; ok {
				t.TagID = nid
				tags = append(tags, t)
			}
		}
		m.Tags = tags
		var notifs []int64
		for _, id := range m.NotificationIDs {
			if nid, ok := notifIDs[id]; ok {
				notifs = append(notifs, nid)
			}
		}
		m.NotificationIDs = notifs
		if m.PushToken != "" {
			if _, err := s.db.GetMonitorByPushToken(ctx, m.PushToken); err == nil {
				m.PushToken = "" // keep the existing monitor's token unique
			}
		}
		if err := s.db.CreateMonitor(ctx, m); err != nil {
			return counts, fmt.Errorf("monitor %s: %w", m.Name, err)
		}
		monIDs[old] = m.ID
		created = append(created, m)
		counts["monitors"]++
	}
	for old, parent := range parents {
		nid, ok1 := monIDs[old]
		np, ok2 := monIDs[parent]
		if !ok1 || !ok2 {
			continue
		}
		for _, m := range created {
			if m.ID == nid {
				m.ParentID = &np
				if err := s.db.UpdateMonitor(ctx, m); err != nil {
					return counts, err
				}
			}
		}
	}
	for oldPanel, oldGroup := range panelGroups {
		if np, ok := panelIDs[oldPanel]; ok {
			if ng, ok := monIDs[oldGroup]; ok {
				if p, err := s.db.GetPanel(ctx, np); err == nil {
					p.GroupMonitorID = &ng
					_ = s.db.SavePanel(ctx, p)
				}
			}
		}
	}
	for _, m := range created {
		s.mgr.Upsert(m)
	}

	for _, u := range b.Uploads {
		if store.UploadName(store.UploadPrefix+u.Name) != "" && sniffImage(u.Data) != "" {
			_ = s.db.SaveUpload(ctx, u)
		}
	}
	pageIDs := map[int64]int64{}
	for _, p := range b.StatusPages {
		old := p.ID
		p.ID = 0
		if existing, err := s.db.GetStatusPageBySlug(ctx, p.Slug); err == nil && existing != nil {
			p.Slug = p.Slug + "-imported"
		}
		if d, err := normDomain(p.Domain); err != nil || d == "" {
			p.Domain = ""
		} else if _, taken := s.hosts.page(d); taken {
			p.Domain = "" // the page already here keeps it
		}
		for gi := range p.Groups {
			var mons []store.StatusPageMonitor
			for _, sm := range p.Groups[gi].Monitors {
				if nid, ok := monIDs[sm.MonitorID]; ok {
					sm.MonitorID = nid
					mons = append(mons, sm)
				}
			}
			if mons == nil {
				mons = []store.StatusPageMonitor{}
			}
			p.Groups[gi].Monitors = mons
		}
		if err := s.db.SaveStatusPage(ctx, p); err != nil {
			return counts, fmt.Errorf("status page %s: %w", p.Slug, err)
		}
		s.reloadHosts(ctx)
		pageIDs[old] = p.ID
		counts["statusPages"]++
	}
	for _, i := range b.Incidents {
		np, ok := pageIDs[i.StatusPageID]
		if !ok {
			continue
		}
		i.ID, i.StatusPageID = 0, np
		_ = s.db.SaveIncident(ctx, i)
	}
	for _, m := range b.Maintenances {
		m.ID = 0
		var mons, pages []int64
		for _, id := range m.MonitorIDs {
			if nid, ok := monIDs[id]; ok {
				mons = append(mons, nid)
			}
		}
		for _, id := range m.StatusPageIDs {
			if nid, ok := pageIDs[id]; ok {
				pages = append(pages, nid)
			}
		}
		m.MonitorIDs, m.StatusPageIDs = mons, pages
		if err := s.db.SaveMaintenance(ctx, m); err != nil {
			return counts, fmt.Errorf("maintenance %s: %w", m.Title, err)
		}
		counts["maintenances"]++
	}
	return counts, nil
}
