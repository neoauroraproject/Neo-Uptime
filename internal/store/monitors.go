package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const monitorCols = `id, name, type, parent_id, active, interval, retry_interval, max_retries,
  timeout, resend_interval, upside_down, description, config, push_token, sort_order,
  created_at, updated_at, country`

type scanner interface{ Scan(dest ...any) error }

func scanMonitor(s scanner) (*Monitor, error) {
	var (
		m       Monitor
		parent  sql.NullInt64
		active  int
		upside  int
		config  string
		pushTok sql.NullString
	)
	err := s.Scan(&m.ID, &m.Name, &m.Type, &parent, &active, &m.Interval, &m.RetryInterval,
		&m.MaxRetries, &m.Timeout, &m.ResendInterval, &upside, &m.Description, &config,
		&pushTok, &m.SortOrder, &m.CreatedAt, &m.UpdatedAt, &m.Country)
	if err != nil {
		return nil, err
	}
	if parent.Valid {
		p := parent.Int64
		m.ParentID = &p
	}
	m.Active = active == 1
	m.UpsideDown = upside == 1
	m.PushToken = pushTok.String
	_ = json.Unmarshal([]byte(config), &m.Config)
	m.Tags = []MonitorTag{}
	m.NotificationIDs = []int64{}
	return &m, nil
}

// ListMonitors returns every monitor with its tags and notifications.
func (db *DB) ListMonitors(ctx context.Context) ([]*Monitor, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+monitorCols+` FROM monitors ORDER BY sort_order, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Monitor
	byID := map[int64]*Monitor{}
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
		byID[m.ID] = m
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := db.attachTags(ctx, byID); err != nil {
		return nil, err
	}
	if err := db.attachNotifications(ctx, byID); err != nil {
		return nil, err
	}
	return out, nil
}

// GetMonitor returns one monitor.
func (db *DB) GetMonitor(ctx context.Context, id int64) (*Monitor, error) {
	m, err := scanMonitor(db.r.QueryRowContext(ctx, `SELECT `+monitorCols+` FROM monitors WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	byID := map[int64]*Monitor{m.ID: m}
	if err := db.attachTags(ctx, byID); err != nil {
		return nil, err
	}
	if err := db.attachNotifications(ctx, byID); err != nil {
		return nil, err
	}
	return m, nil
}

// GetMonitorByPushToken finds a push monitor by its token.
func (db *DB) GetMonitorByPushToken(ctx context.Context, token string) (*Monitor, error) {
	m, err := scanMonitor(db.r.QueryRowContext(ctx, `SELECT `+monitorCols+` FROM monitors WHERE push_token = ?`, token))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (db *DB) attachTags(ctx context.Context, byID map[int64]*Monitor) error {
	rows, err := db.r.QueryContext(ctx, `SELECT mt.monitor_id, t.id, t.name, t.color, mt.value
		FROM monitor_tags mt JOIN tags t ON t.id = mt.tag_id ORDER BY t.name COLLATE NOCASE`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var mid int64
		var t MonitorTag
		if err := rows.Scan(&mid, &t.TagID, &t.Name, &t.Color, &t.Value); err != nil {
			return err
		}
		if m, ok := byID[mid]; ok {
			m.Tags = append(m.Tags, t)
		}
	}
	return rows.Err()
}

func (db *DB) attachNotifications(ctx context.Context, byID map[int64]*Monitor) error {
	rows, err := db.r.QueryContext(ctx, `SELECT monitor_id, notification_id FROM monitor_notifications`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var mid, nid int64
		if err := rows.Scan(&mid, &nid); err != nil {
			return err
		}
		if m, ok := byID[mid]; ok {
			m.NotificationIDs = append(m.NotificationIDs, nid)
		}
	}
	return rows.Err()
}

func nullablePush(tok string) any {
	if tok == "" {
		return nil
	}
	return tok
}

func nullableID(id *int64) any {
	if id == nil || *id == 0 {
		return nil
	}
	return *id
}

// CreateMonitor inserts m and sets m.ID.
func (db *DB) CreateMonitor(ctx context.Context, m *Monitor) error {
	cfg, _ := json.Marshal(m.Config)
	t := now()
	m.CreatedAt, m.UpdatedAt = t, t
	return db.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO monitors (name, type, parent_id, active, interval,
			retry_interval, max_retries, timeout, resend_interval, upside_down, description, config,
			push_token, sort_order, created_at, updated_at, country) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.Name, m.Type, nullableID(m.ParentID), b2i(m.Active), m.Interval, m.RetryInterval,
			m.MaxRetries, m.Timeout, m.ResendInterval, b2i(m.UpsideDown), m.Description, string(cfg),
			nullablePush(m.PushToken), m.SortOrder, t, t, m.Country)
		if err != nil {
			return err
		}
		m.ID, _ = res.LastInsertId()
		return writeMonitorLinks(ctx, tx, m)
	})
}

// UpdateMonitor saves every field of m.
func (db *DB) UpdateMonitor(ctx context.Context, m *Monitor) error {
	cfg, _ := json.Marshal(m.Config)
	m.UpdatedAt = now()
	return db.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE monitors SET name=?, type=?, parent_id=?, active=?,
			interval=?, retry_interval=?, max_retries=?, timeout=?, resend_interval=?, upside_down=?,
			description=?, config=?, push_token=?, sort_order=?, updated_at=?, country=? WHERE id=?`,
			m.Name, m.Type, nullableID(m.ParentID), b2i(m.Active), m.Interval, m.RetryInterval,
			m.MaxRetries, m.Timeout, m.ResendInterval, b2i(m.UpsideDown), m.Description, string(cfg),
			nullablePush(m.PushToken), m.SortOrder, m.UpdatedAt, m.Country, m.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM monitor_tags WHERE monitor_id=?`, m.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM monitor_notifications WHERE monitor_id=?`, m.ID); err != nil {
			return err
		}
		return writeMonitorLinks(ctx, tx, m)
	})
}

func writeMonitorLinks(ctx context.Context, tx *sql.Tx, m *Monitor) error {
	for _, t := range m.Tags {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO monitor_tags (monitor_id, tag_id, value) VALUES (?,?,?)`,
			m.ID, t.TagID, t.Value); err != nil {
			return err
		}
	}
	for _, nid := range m.NotificationIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO monitor_notifications (monitor_id, notification_id) VALUES (?,?)`,
			m.ID, nid); err != nil {
			return err
		}
	}
	return nil
}

// SetMonitorActive pauses or resumes a monitor.
func (db *DB) SetMonitorActive(ctx context.Context, id int64, active bool) error {
	res, err := db.w.ExecContext(ctx, `UPDATE monitors SET active=?, updated_at=? WHERE id=?`, b2i(active), now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteMonitor removes a monitor and, through cascades, its history.
func (db *DB) DeleteMonitor(ctx context.Context, id int64) error {
	return db.tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`DELETE FROM stat_hourly WHERE monitor_id=?`,
			`DELETE FROM stat_daily WHERE monitor_id=?`,
			`DELETE FROM heartbeats WHERE monitor_id=?`,
		} {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM monitors WHERE id=?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// LinkNotificationToAll attaches a notification to every monitor.
func (db *DB) LinkNotificationToAll(ctx context.Context, nid int64) error {
	_, err := db.w.ExecContext(ctx, `INSERT OR IGNORE INTO monitor_notifications (monitor_id, notification_id)
		SELECT id, ? FROM monitors`, nid)
	return err
}

// MonitorNotifications returns the active notifications attached to a monitor.
func (db *DB) MonitorNotifications(ctx context.Context, monitorID int64) ([]*Notification, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+notificationCols+` FROM notifications n
		JOIN monitor_notifications mn ON mn.notification_id = n.id
		WHERE mn.monitor_id = ? AND n.active = 1`, monitorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
