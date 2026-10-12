package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const subscriptionCols = `id, name, url, agent_id, interval, timeout, test_url, auto_import, refresh_minutes,
  group_monitor_id, skip, info, last_fetch, last_error, created_at, updated_at`

func scanSubscription(s scanner) (*Subscription, error) {
	var x Subscription
	var auto int
	var group sql.NullInt64
	var skip, info string
	if err := s.Scan(&x.ID, &x.Name, &x.URL, &x.AgentID, &x.Interval, &x.Timeout, &x.TestURL, &auto, &x.RefreshMinutes,
		&group, &skip, &info, &x.LastFetch, &x.LastError, &x.CreatedAt, &x.UpdatedAt); err != nil {
		return nil, err
	}
	x.AutoImport = auto == 1
	if group.Valid {
		g := group.Int64
		x.GroupMonitorID = &g
	}
	_ = json.Unmarshal([]byte(skip), &x.Skip)
	if x.Skip == nil {
		x.Skip = []string{}
	}
	_ = json.Unmarshal([]byte(info), &x.Info)
	return &x, nil
}

// ListSubscriptions returns every subscription.
func (db *DB) ListSubscriptions(ctx context.Context) ([]*Subscription, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+subscriptionCols+` FROM subscriptions ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Subscription{}
	for rows.Next() {
		x, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// GetSubscription returns one subscription.
func (db *DB) GetSubscription(ctx context.Context, id int64) (*Subscription, error) {
	x, err := scanSubscription(db.r.QueryRowContext(ctx, `SELECT `+subscriptionCols+` FROM subscriptions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return x, err
}

// SaveSubscription inserts or updates x.
func (db *DB) SaveSubscription(ctx context.Context, x *Subscription) error {
	t := now()
	x.UpdatedAt = t
	if x.Skip == nil {
		x.Skip = []string{}
	}
	skip, _ := json.Marshal(x.Skip)
	info, _ := json.Marshal(x.Info)
	var group any
	if x.GroupMonitorID != nil {
		group = *x.GroupMonitorID
	}
	if x.ID == 0 {
		x.CreatedAt = t
		res, err := db.w.ExecContext(ctx, `INSERT INTO subscriptions (name, url, agent_id, interval, timeout, test_url, auto_import,
			refresh_minutes, group_monitor_id, skip, info, last_fetch, last_error, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			x.Name, x.URL, x.AgentID, x.Interval, x.Timeout, x.TestURL, b2i(x.AutoImport), x.RefreshMinutes, group,
			string(skip), string(info), x.LastFetch, x.LastError, t, t)
		if err != nil {
			return err
		}
		x.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := db.w.ExecContext(ctx, `UPDATE subscriptions SET name=?, url=?, agent_id=?, interval=?, timeout=?, test_url=?,
		auto_import=?, refresh_minutes=?, group_monitor_id=?, skip=?, info=?, last_fetch=?, last_error=?, updated_at=? WHERE id=?`,
		x.Name, x.URL, x.AgentID, x.Interval, x.Timeout, x.TestURL, b2i(x.AutoImport), x.RefreshMinutes, group,
		string(skip), string(info), x.LastFetch, x.LastError, t, x.ID)
	return err
}

// DeleteSubscription removes a subscription (not its monitors).
func (db *DB) DeleteSubscription(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM subscriptions WHERE id=?`, id)
	return err
}
