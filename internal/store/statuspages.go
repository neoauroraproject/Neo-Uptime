package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const statusPageCols = `id, slug, title, description, theme, layout, bar_mode, accent_color, logo, footer,
  custom_css, show_tags, show_cert_expiry, show_powered_by, show_charts, published, refresh_interval,
  created_at, updated_at, language, show_uptime, show_latency, show_banner`

func scanStatusPage(s scanner) (*StatusPage, error) {
	var p StatusPage
	var tags, cert, powered, charts, pub, upt, lat, ban int
	if err := s.Scan(&p.ID, &p.Slug, &p.Title, &p.Description, &p.Theme, &p.Layout, &p.BarMode, &p.AccentColor,
		&p.Logo, &p.Footer, &p.CustomCSS, &tags, &cert, &powered, &charts, &pub, &p.RefreshInterval,
		&p.CreatedAt, &p.UpdatedAt, &p.Language, &upt, &lat, &ban); err != nil {
		return nil, err
	}
	p.ShowUptime, p.ShowLatency, p.ShowBanner = upt == 1, lat == 1, ban == 1
	p.ShowTags, p.ShowCertExpiry, p.ShowPoweredBy, p.ShowCharts, p.Published = tags == 1, cert == 1, powered == 1, charts == 1, pub == 1
	p.Groups = []StatusPageGroup{}
	return &p, nil
}

// ListStatusPages returns every status page without groups.
func (db *DB) ListStatusPages(ctx context.Context) ([]*StatusPage, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+statusPageCols+` FROM status_pages ORDER BY title COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*StatusPage{}
	for rows.Next() {
		p, err := scanStatusPage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetStatusPage returns a status page with its groups, by id.
func (db *DB) GetStatusPage(ctx context.Context, id int64) (*StatusPage, error) {
	p, err := scanStatusPage(db.r.QueryRowContext(ctx, `SELECT `+statusPageCols+` FROM status_pages WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, db.loadGroups(ctx, p)
}

// GetStatusPageBySlug returns a status page with its groups, by slug.
func (db *DB) GetStatusPageBySlug(ctx context.Context, slug string) (*StatusPage, error) {
	p, err := scanStatusPage(db.r.QueryRowContext(ctx, `SELECT `+statusPageCols+` FROM status_pages WHERE slug=?`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, db.loadGroups(ctx, p)
}

func (db *DB) loadGroups(ctx context.Context, p *StatusPage) error {
	rows, err := db.r.QueryContext(ctx, `SELECT g.id, g.name, m.monitor_id, m.show_url
		FROM status_page_groups g LEFT JOIN status_page_monitors m ON m.group_id = g.id
		WHERE g.status_page_id = ? ORDER BY g.sort_order, g.id, m.sort_order`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	idx := map[int64]int{}
	for rows.Next() {
		var gid int64
		var name string
		var mid sql.NullInt64
		var showURL sql.NullInt64
		if err := rows.Scan(&gid, &name, &mid, &showURL); err != nil {
			return err
		}
		i, ok := idx[gid]
		if !ok {
			p.Groups = append(p.Groups, StatusPageGroup{ID: gid, Name: name, Monitors: []StatusPageMonitor{}})
			i = len(p.Groups) - 1
			idx[gid] = i
		}
		if mid.Valid {
			p.Groups[i].Monitors = append(p.Groups[i].Monitors, StatusPageMonitor{MonitorID: mid.Int64, ShowURL: showURL.Int64 == 1})
		}
	}
	return rows.Err()
}

// SaveStatusPage inserts or updates p, replacing its groups.
func (db *DB) SaveStatusPage(ctx context.Context, p *StatusPage) error {
	t := now()
	p.UpdatedAt = t
	return db.tx(ctx, func(tx *sql.Tx) error {
		if p.ID == 0 {
			p.CreatedAt = t
			res, err := tx.ExecContext(ctx, `INSERT INTO status_pages (slug, title, description, theme, layout, bar_mode,
				accent_color, logo, footer, custom_css, show_tags, show_cert_expiry, show_powered_by, show_charts,
				published, refresh_interval, created_at, updated_at, language, show_uptime, show_latency, show_banner)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				p.Slug, p.Title, p.Description, p.Theme, p.Layout, p.BarMode, p.AccentColor, p.Logo, p.Footer, p.CustomCSS,
				b2i(p.ShowTags), b2i(p.ShowCertExpiry), b2i(p.ShowPoweredBy), b2i(p.ShowCharts), b2i(p.Published),
				p.RefreshInterval, t, t, p.Language, b2i(p.ShowUptime), b2i(p.ShowLatency), b2i(p.ShowBanner))
			if err != nil {
				return err
			}
			p.ID, _ = res.LastInsertId()
		} else {
			_, err := tx.ExecContext(ctx, `UPDATE status_pages SET slug=?, title=?, description=?, theme=?, layout=?,
				bar_mode=?, accent_color=?, logo=?, footer=?, custom_css=?, show_tags=?, show_cert_expiry=?,
				show_powered_by=?, show_charts=?, published=?, refresh_interval=?, updated_at=?, language=?,
				show_uptime=?, show_latency=?, show_banner=? WHERE id=?`,
				p.Slug, p.Title, p.Description, p.Theme, p.Layout, p.BarMode, p.AccentColor, p.Logo, p.Footer, p.CustomCSS,
				b2i(p.ShowTags), b2i(p.ShowCertExpiry), b2i(p.ShowPoweredBy), b2i(p.ShowCharts), b2i(p.Published),
				p.RefreshInterval, t, p.Language, b2i(p.ShowUptime), b2i(p.ShowLatency), b2i(p.ShowBanner), p.ID)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM status_page_groups WHERE status_page_id=?`, p.ID); err != nil {
				return err
			}
		}
		for gi := range p.Groups {
			g := &p.Groups[gi]
			res, err := tx.ExecContext(ctx, `INSERT INTO status_page_groups (status_page_id, name, sort_order) VALUES (?,?,?)`,
				p.ID, g.Name, gi)
			if err != nil {
				return err
			}
			g.ID, _ = res.LastInsertId()
			for mi, m := range g.Monitors {
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO status_page_monitors (group_id, monitor_id, sort_order, show_url)
					VALUES (?,?,?,?)`, g.ID, m.MonitorID, mi, b2i(m.ShowURL)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// DeleteStatusPage removes a page.
func (db *DB) DeleteStatusPage(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM status_pages WHERE id=?`, id)
	return err
}

// PublicMonitorIDs returns the ids of monitors shown on any published page.
func (db *DB) PublicMonitorIDs(ctx context.Context) (map[int64]bool, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT DISTINCT m.monitor_id FROM status_page_monitors m
		JOIN status_page_groups g ON g.id = m.group_id JOIN status_pages p ON p.id = g.status_page_id
		WHERE p.published = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ---------- incidents ----------

// ListIncidents returns a page's incidents, newest first.
func (db *DB) ListIncidents(ctx context.Context, pageID int64, pinnedOnly bool) ([]*Incident, error) {
	q := `SELECT id, status_page_id, title, content, style, pinned, created_at, updated_at FROM incidents WHERE status_page_id=?`
	if pinnedOnly {
		q += ` AND pinned = 1`
	}
	rows, err := db.r.QueryContext(ctx, q+` ORDER BY created_at DESC LIMIT 50`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Incident{}
	for rows.Next() {
		var i Incident
		var pinned int
		if err := rows.Scan(&i.ID, &i.StatusPageID, &i.Title, &i.Content, &i.Style, &pinned, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		i.Pinned = pinned == 1
		out = append(out, &i)
	}
	return out, rows.Err()
}

// SaveIncident inserts or updates i.
func (db *DB) SaveIncident(ctx context.Context, i *Incident) error {
	t := now()
	i.UpdatedAt = t
	if i.ID == 0 {
		i.CreatedAt = t
		res, err := db.w.ExecContext(ctx, `INSERT INTO incidents (status_page_id, title, content, style, pinned, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?)`, i.StatusPageID, i.Title, i.Content, i.Style, b2i(i.Pinned), t, t)
		if err != nil {
			return err
		}
		i.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := db.w.ExecContext(ctx, `UPDATE incidents SET title=?, content=?, style=?, pinned=?, updated_at=? WHERE id=? AND status_page_id=?`,
		i.Title, i.Content, i.Style, b2i(i.Pinned), t, i.ID, i.StatusPageID)
	return err
}

// DeleteIncident removes an incident.
func (db *DB) DeleteIncident(ctx context.Context, pageID, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM incidents WHERE id=? AND status_page_id=?`, id, pageID)
	return err
}

// ---------- maintenance ----------

const maintCols = `id, title, description, strategy, active, start_at, end_at, weekdays, days_of_month,
  time_from, time_to, timezone, created_at, updated_at`

func scanMaint(s scanner) (*Maintenance, error) {
	var m Maintenance
	var active int
	var wd, dom string
	if err := s.Scan(&m.ID, &m.Title, &m.Description, &m.Strategy, &active, &m.StartAt, &m.EndAt, &wd, &dom,
		&m.TimeFrom, &m.TimeTo, &m.Timezone, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	m.Active = active == 1
	m.Weekdays, m.DaysOfMonth = []int{}, []int{}
	_ = json.Unmarshal([]byte(wd), &m.Weekdays)
	_ = json.Unmarshal([]byte(dom), &m.DaysOfMonth)
	m.MonitorIDs, m.StatusPageIDs = []int64{}, []int64{}
	return &m, nil
}

// ListMaintenances returns every maintenance with its links.
func (db *DB) ListMaintenances(ctx context.Context) ([]*Maintenance, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+maintCols+` FROM maintenances ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	var out []*Maintenance
	byID := map[int64]*Maintenance{}
	for rows.Next() {
		m, err := scanMaint(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
		byID[m.ID] = m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	links, err := db.r.QueryContext(ctx, `SELECT maintenance_id, monitor_id FROM maintenance_monitors`)
	if err != nil {
		return nil, err
	}
	for links.Next() {
		var a, b int64
		if err := links.Scan(&a, &b); err == nil {
			if m := byID[a]; m != nil {
				m.MonitorIDs = append(m.MonitorIDs, b)
			}
		}
	}
	links.Close()
	pages, err := db.r.QueryContext(ctx, `SELECT maintenance_id, status_page_id FROM maintenance_status_pages`)
	if err != nil {
		return nil, err
	}
	for pages.Next() {
		var a, b int64
		if err := pages.Scan(&a, &b); err == nil {
			if m := byID[a]; m != nil {
				m.StatusPageIDs = append(m.StatusPageIDs, b)
			}
		}
	}
	pages.Close()
	if out == nil {
		out = []*Maintenance{}
	}
	return out, nil
}

// SaveMaintenance inserts or updates m with its links.
func (db *DB) SaveMaintenance(ctx context.Context, m *Maintenance) error {
	wd, _ := json.Marshal(m.Weekdays)
	dom, _ := json.Marshal(m.DaysOfMonth)
	t := now()
	m.UpdatedAt = t
	return db.tx(ctx, func(tx *sql.Tx) error {
		if m.ID == 0 {
			m.CreatedAt = t
			res, err := tx.ExecContext(ctx, `INSERT INTO maintenances (title, description, strategy, active, start_at, end_at,
				weekdays, days_of_month, time_from, time_to, timezone, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				m.Title, m.Description, m.Strategy, b2i(m.Active), m.StartAt, m.EndAt, string(wd), string(dom),
				m.TimeFrom, m.TimeTo, m.Timezone, t, t)
			if err != nil {
				return err
			}
			m.ID, _ = res.LastInsertId()
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE maintenances SET title=?, description=?, strategy=?, active=?, start_at=?,
				end_at=?, weekdays=?, days_of_month=?, time_from=?, time_to=?, timezone=?, updated_at=? WHERE id=?`,
				m.Title, m.Description, m.Strategy, b2i(m.Active), m.StartAt, m.EndAt, string(wd), string(dom),
				m.TimeFrom, m.TimeTo, m.Timezone, t, m.ID); err != nil {
				return err
			}
			for _, q := range []string{`DELETE FROM maintenance_monitors WHERE maintenance_id=?`, `DELETE FROM maintenance_status_pages WHERE maintenance_id=?`} {
				if _, err := tx.ExecContext(ctx, q, m.ID); err != nil {
					return err
				}
			}
		}
		for _, id := range m.MonitorIDs {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO maintenance_monitors (maintenance_id, monitor_id) VALUES (?,?)`, m.ID, id); err != nil {
				return err
			}
		}
		for _, id := range m.StatusPageIDs {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO maintenance_status_pages (maintenance_id, status_page_id) VALUES (?,?)`, m.ID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetMaintenanceActive pauses or resumes a maintenance.
func (db *DB) SetMaintenanceActive(ctx context.Context, id int64, active bool) error {
	_, err := db.w.ExecContext(ctx, `UPDATE maintenances SET active=?, updated_at=? WHERE id=?`, b2i(active), now(), id)
	return err
}

// DeleteMaintenance removes a maintenance.
func (db *DB) DeleteMaintenance(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM maintenances WHERE id=?`, id)
	return err
}
