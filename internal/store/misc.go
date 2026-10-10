package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// ---------- settings ----------

// AllSettings returns every stored setting.
func (db *DB) AllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// GetSetting returns one setting, or def when unset.
func (db *DB) GetSetting(ctx context.Context, key, def string) string {
	var v string
	if err := db.r.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

// SetSettings upserts several settings at once.
func (db *DB) SetSettings(ctx context.Context, kv map[string]string) error {
	return db.tx(ctx, func(tx *sql.Tx) error {
		for k, v := range kv {
			if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?,?)
				ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---------- tags ----------

// ListTags returns every tag.
func (db *DB) ListTags(ctx context.Context) ([]*Tag, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT id, name, color, created_at FROM tags ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// CreateTag inserts t.
func (db *DB) CreateTag(ctx context.Context, t *Tag) error {
	t.CreatedAt = now()
	res, err := db.w.ExecContext(ctx, `INSERT INTO tags (name, color, created_at) VALUES (?,?,?)`, t.Name, t.Color, t.CreatedAt)
	if err != nil {
		return err
	}
	t.ID, _ = res.LastInsertId()
	return nil
}

// UpdateTag saves t.
func (db *DB) UpdateTag(ctx context.Context, t *Tag) error {
	_, err := db.w.ExecContext(ctx, `UPDATE tags SET name=?, color=? WHERE id=?`, t.Name, t.Color, t.ID)
	return err
}

// DeleteTag removes a tag from everything.
func (db *DB) DeleteTag(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM tags WHERE id=?`, id)
	return err
}

// ---------- notifications ----------

const notificationCols = `n.id, n.name, n.type, n.config, n.is_default, n.active, n.created_at`

func scanNotification(s scanner) (*Notification, error) {
	var n Notification
	var cfg string
	var def, active int
	if err := s.Scan(&n.ID, &n.Name, &n.Type, &cfg, &def, &active, &n.CreatedAt); err != nil {
		return nil, err
	}
	n.Config = map[string]string{}
	_ = json.Unmarshal([]byte(cfg), &n.Config)
	n.IsDefault = def == 1
	n.Active = active == 1
	return &n, nil
}

// ListNotifications returns every notification channel.
func (db *DB) ListNotifications(ctx context.Context) ([]*Notification, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+notificationCols+` FROM notifications n ORDER BY n.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Notification{}
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// GetNotification returns one channel.
func (db *DB) GetNotification(ctx context.Context, id int64) (*Notification, error) {
	n, err := scanNotification(db.r.QueryRowContext(ctx, `SELECT `+notificationCols+` FROM notifications n WHERE n.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

// SaveNotification inserts or updates n.
func (db *DB) SaveNotification(ctx context.Context, n *Notification) error {
	cfg, _ := json.Marshal(n.Config)
	if n.ID == 0 {
		n.CreatedAt = now()
		res, err := db.w.ExecContext(ctx, `INSERT INTO notifications (name, type, config, is_default, active, created_at)
			VALUES (?,?,?,?,?,?)`, n.Name, n.Type, string(cfg), b2i(n.IsDefault), b2i(n.Active), n.CreatedAt)
		if err != nil {
			return err
		}
		n.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := db.w.ExecContext(ctx, `UPDATE notifications SET name=?, type=?, config=?, is_default=?, active=? WHERE id=?`,
		n.Name, n.Type, string(cfg), b2i(n.IsDefault), b2i(n.Active), n.ID)
	return err
}

// DeleteNotification removes a channel.
func (db *DB) DeleteNotification(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM notifications WHERE id=?`, id)
	return err
}

// DefaultNotificationIDs returns the channels new monitors get by default.
func (db *DB) DefaultNotificationIDs(ctx context.Context) ([]int64, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT id FROM notifications WHERE is_default = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ---------- BackPack panels ----------

const panelCols = `id, name, url, token, ignore_tls, interval, auto_import, group_monitor_id, created_at, updated_at`

func scanPanel(s scanner) (*BackpackPanel, error) {
	var p BackpackPanel
	var ignore, auto int
	var group sql.NullInt64
	if err := s.Scan(&p.ID, &p.Name, &p.URL, &p.Token, &ignore, &p.Interval, &auto, &group, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.IgnoreTLS = ignore == 1
	p.AutoImport = auto == 1
	if group.Valid {
		g := group.Int64
		p.GroupMonitorID = &g
	}
	return &p, nil
}

// ListPanels returns every BackPack panel.
func (db *DB) ListPanels(ctx context.Context) ([]*BackpackPanel, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+panelCols+` FROM backpack_panels ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*BackpackPanel{}
	for rows.Next() {
		p, err := scanPanel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPanel returns one BackPack panel.
func (db *DB) GetPanel(ctx context.Context, id int64) (*BackpackPanel, error) {
	p, err := scanPanel(db.r.QueryRowContext(ctx, `SELECT `+panelCols+` FROM backpack_panels WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// SavePanel inserts or updates p.
func (db *DB) SavePanel(ctx context.Context, p *BackpackPanel) error {
	t := now()
	p.UpdatedAt = t
	if p.ID == 0 {
		p.CreatedAt = t
		res, err := db.w.ExecContext(ctx, `INSERT INTO backpack_panels (name, url, token, ignore_tls, interval, auto_import,
			group_monitor_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			p.Name, p.URL, p.Token, b2i(p.IgnoreTLS), p.Interval, b2i(p.AutoImport), nullableID(p.GroupMonitorID), t, t)
		if err != nil {
			return err
		}
		p.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := db.w.ExecContext(ctx, `UPDATE backpack_panels SET name=?, url=?, token=?, ignore_tls=?, interval=?,
		auto_import=?, group_monitor_id=?, updated_at=? WHERE id=?`,
		p.Name, p.URL, p.Token, b2i(p.IgnoreTLS), p.Interval, b2i(p.AutoImport), nullableID(p.GroupMonitorID), t, p.ID)
	return err
}

// DeletePanel removes a panel record.
func (db *DB) DeletePanel(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM backpack_panels WHERE id=?`, id)
	return err
}
