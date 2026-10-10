package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const hbCols = `id, monitor_id, status, time, ping, msg, important, duration, bytes_in, bytes_out`

func scanHeartbeat(s scanner) (*Heartbeat, error) {
	var (
		h        Heartbeat
		ping     sql.NullFloat64
		imp      int
		bin, bou sql.NullInt64
	)
	if err := s.Scan(&h.ID, &h.MonitorID, &h.Status, &h.Time, &ping, &h.Msg, &imp, &h.Duration, &bin, &bou); err != nil {
		return nil, err
	}
	if ping.Valid {
		p := ping.Float64
		h.Ping = &p
	}
	if bin.Valid {
		v := bin.Int64
		h.BytesIn = &v
	}
	if bou.Valid {
		v := bou.Int64
		h.BytesOut = &v
	}
	h.Important = imp == 1
	return &h, nil
}

func nullFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// InsertHeartbeat stores a heartbeat and folds it into the hourly and daily
// aggregates in the same transaction, so uptime figures never disagree with
// the raw history they come from.
func (db *DB) InsertHeartbeat(ctx context.Context, h *Heartbeat) error {
	up, down := 1, 0
	if h.Status == StatusDown {
		up, down = 0, 1
	}
	sec := h.Time / 1000
	hour := sec - sec%3600
	day := sec - sec%86400
	var pingSum float64
	var pingCnt int
	if h.Ping != nil {
		pingSum, pingCnt = *h.Ping, 1
	}
	return db.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO heartbeats (monitor_id, status, time, ping, msg, important,
			duration, bytes_in, bytes_out) VALUES (?,?,?,?,?,?,?,?,?)`,
			h.MonitorID, h.Status, h.Time, nullFloat(h.Ping), h.Msg, b2i(h.Important), h.Duration,
			nullInt(h.BytesIn), nullInt(h.BytesOut))
		if err != nil {
			return err
		}
		h.ID, _ = res.LastInsertId()
		for _, table := range []struct {
			name string
			ts   int64
		}{{"stat_hourly", hour}, {"stat_daily", day}} {
			_, err := tx.ExecContext(ctx, `INSERT INTO `+table.name+` (monitor_id, ts, up, down, ping_sum, ping_cnt, ping_min, ping_max)
				VALUES (?,?,?,?,?,?,?,?)
				ON CONFLICT(monitor_id, ts) DO UPDATE SET
				  up = up + excluded.up,
				  down = down + excluded.down,
				  ping_sum = ping_sum + excluded.ping_sum,
				  ping_cnt = ping_cnt + excluded.ping_cnt,
				  ping_min = CASE WHEN excluded.ping_min IS NULL THEN ping_min WHEN ping_min IS NULL OR excluded.ping_min < ping_min THEN excluded.ping_min ELSE ping_min END,
				  ping_max = CASE WHEN excluded.ping_max IS NULL THEN ping_max WHEN ping_max IS NULL OR excluded.ping_max > ping_max THEN excluded.ping_max ELSE ping_max END`,
				h.MonitorID, table.ts, up, down, pingSum, pingCnt, nullFloat(h.Ping), nullFloat(h.Ping))
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// LastHeartbeat returns the most recent heartbeat of a monitor.
func (db *DB) LastHeartbeat(ctx context.Context, monitorID int64) (*Heartbeat, error) {
	h, err := scanHeartbeat(db.r.QueryRowContext(ctx, `SELECT `+hbCols+` FROM heartbeats
		WHERE monitor_id = ? ORDER BY time DESC LIMIT 1`, monitorID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return h, err
}

// LastImportant returns the most recent important heartbeat of a monitor.
func (db *DB) LastImportant(ctx context.Context, monitorID int64) (*Heartbeat, error) {
	h, err := scanHeartbeat(db.r.QueryRowContext(ctx, `SELECT `+hbCols+` FROM heartbeats
		WHERE monitor_id = ? AND important = 1 ORDER BY time DESC LIMIT 1`, monitorID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return h, err
}

// RecentHeartbeats returns the last n heartbeats of every monitor in ids (or
// of every monitor when ids is empty), oldest first within each monitor.
func (db *DB) RecentHeartbeats(ctx context.Context, ids []int64, n int) (map[int64][]*Heartbeat, error) {
	where := ""
	args := []any{}
	if len(ids) > 0 {
		where = "WHERE monitor_id IN (" + placeholders(len(ids)) + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	args = append(args, n)
	rows, err := db.r.QueryContext(ctx, `SELECT `+hbCols+` FROM (
		SELECT `+hbCols+`, ROW_NUMBER() OVER (PARTITION BY monitor_id ORDER BY time DESC) AS rn
		FROM heartbeats `+where+`) WHERE rn <= ? ORDER BY monitor_id, time`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]*Heartbeat{}
	for rows.Next() {
		h, err := scanHeartbeat(rows)
		if err != nil {
			return nil, err
		}
		out[h.MonitorID] = append(out[h.MonitorID], h)
	}
	return out, rows.Err()
}

// HeartbeatsSince returns heartbeats of a monitor newer than sinceMs, oldest
// first, capped at limit.
func (db *DB) HeartbeatsSince(ctx context.Context, monitorID, sinceMs int64, limit int) ([]*Heartbeat, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+hbCols+` FROM (SELECT `+hbCols+` FROM heartbeats
		WHERE monitor_id = ? AND time >= ? ORDER BY time DESC LIMIT ?) ORDER BY time`, monitorID, sinceMs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Heartbeat
	for rows.Next() {
		h, err := scanHeartbeat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// HeartbeatPage returns one page of a monitor's heartbeats, newest first.
func (db *DB) HeartbeatPage(ctx context.Context, monitorID int64, importantOnly bool, limit, offset int) ([]*Heartbeat, int, error) {
	cond := "monitor_id = ?"
	if importantOnly {
		cond += " AND important = 1"
	}
	var total int
	if err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM heartbeats WHERE `+cond, monitorID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.r.QueryContext(ctx, `SELECT `+hbCols+` FROM heartbeats WHERE `+cond+`
		ORDER BY time DESC LIMIT ? OFFSET ?`, monitorID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Heartbeat
	for rows.Next() {
		h, err := scanHeartbeat(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, h)
	}
	return out, total, rows.Err()
}

// ImportantEvents returns the important heartbeats of all monitors, newest
// first — the dashboard's event feed.
func (db *DB) ImportantEvents(ctx context.Context, limit, offset int) ([]*Heartbeat, int, error) {
	var total int
	if err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM heartbeats WHERE important = 1`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.r.QueryContext(ctx, `SELECT `+hbCols+` FROM heartbeats WHERE important = 1
		ORDER BY time DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Heartbeat
	for rows.Next() {
		h, err := scanHeartbeat(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, h)
	}
	return out, total, rows.Err()
}

// Uptime is an uptime ratio together with how many checks it rests on.
type Uptime struct {
	Ratio   float64 `json:"ratio"` // 0..1, -1 when nothing was measured
	Checks  int     `json:"checks"`
	AvgPing float64 `json:"avgPing"` // -1 when unknown
}

// UptimeSince computes uptime and average ping over a window, from the
// aggregates: hourly buckets for windows up to a week, daily beyond.
func (db *DB) UptimeSince(ctx context.Context, ids []int64, window time.Duration) (map[int64]Uptime, error) {
	table := "stat_hourly"
	bucket := int64(3600)
	if window > 7*24*time.Hour {
		table, bucket = "stat_daily", 86400
	}
	since := time.Now().Add(-window).Unix()
	since -= since % bucket
	where := "ts >= ?"
	args := []any{since}
	if len(ids) > 0 {
		where += " AND monitor_id IN (" + placeholders(len(ids)) + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := db.r.QueryContext(ctx, `SELECT monitor_id, SUM(up), SUM(down), SUM(ping_sum), SUM(ping_cnt)
		FROM `+table+` WHERE `+where+` GROUP BY monitor_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Uptime{}
	for rows.Next() {
		var id int64
		var up, down, pcnt int
		var psum float64
		if err := rows.Scan(&id, &up, &down, &psum, &pcnt); err != nil {
			return nil, err
		}
		u := Uptime{Ratio: -1, AvgPing: -1, Checks: up + down}
		if up+down > 0 {
			u.Ratio = float64(up) / float64(up+down)
		}
		if pcnt > 0 {
			u.AvgPing = psum / float64(pcnt)
		}
		out[id] = u
	}
	return out, rows.Err()
}

// Bucket is one aggregated time slot.
type Bucket struct {
	TS      int64    `json:"t"` // unix seconds, start of the slot
	Up      int      `json:"up"`
	Down    int      `json:"down"`
	AvgPing *float64 `json:"avg"`
	MinPing *float64 `json:"min"`
	MaxPing *float64 `json:"max"`
}

// Buckets returns the hourly or daily aggregates of the monitors in ids since
// the given time, oldest first.
func (db *DB) Buckets(ctx context.Context, ids []int64, daily bool, since time.Time) (map[int64][]Bucket, error) {
	table := "stat_hourly"
	if daily {
		table = "stat_daily"
	}
	args := []any{since.Unix()}
	where := "ts >= ?"
	if len(ids) > 0 {
		where += " AND monitor_id IN (" + placeholders(len(ids)) + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := db.r.QueryContext(ctx, `SELECT monitor_id, ts, up, down, ping_sum, ping_cnt, ping_min, ping_max
		FROM `+table+` WHERE `+where+` ORDER BY monitor_id, ts`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Bucket{}
	for rows.Next() {
		var (
			id         int64
			b          Bucket
			psum       float64
			pcnt       int
			pmin, pmax sql.NullFloat64
		)
		if err := rows.Scan(&id, &b.TS, &b.Up, &b.Down, &psum, &pcnt, &pmin, &pmax); err != nil {
			return nil, err
		}
		if pcnt > 0 {
			avg := psum / float64(pcnt)
			b.AvgPing = &avg
		}
		if pmin.Valid {
			v := pmin.Float64
			b.MinPing = &v
		}
		if pmax.Valid {
			v := pmax.Float64
			b.MaxPing = &v
		}
		out[id] = append(out[id], b)
	}
	return out, rows.Err()
}

// ClearHeartbeats deletes a monitor's raw heartbeats (keeping aggregates).
func (db *DB) ClearHeartbeats(ctx context.Context, monitorID int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM heartbeats WHERE monitor_id = ?`, monitorID)
	return err
}

// ClearEvents un-marks a monitor's important heartbeats.
func (db *DB) ClearEvents(ctx context.Context, monitorID int64) error {
	_, err := db.w.ExecContext(ctx, `UPDATE heartbeats SET important = 0 WHERE monitor_id = ? AND important = 1`, monitorID)
	return err
}

// ClearAllStatistics wipes every heartbeat and aggregate.
func (db *DB) ClearAllStatistics(ctx context.Context) error {
	return db.tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{`DELETE FROM heartbeats`, `DELETE FROM stat_hourly`, `DELETE FROM stat_daily`} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	})
}

// Prune removes raw heartbeats older than keepDays and hourly aggregates
// older than a year. Daily aggregates are kept: they are tiny and they are
// what the one-year uptime is made of.
func (db *DB) Prune(ctx context.Context, keepDays int) (int64, error) {
	if keepDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().AddDate(0, 0, -keepDays).UnixMilli()
	res, err := db.w.ExecContext(ctx, `DELETE FROM heartbeats WHERE time < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	_, err = db.w.ExecContext(ctx, `DELETE FROM stat_hourly WHERE ts < ?`, time.Now().AddDate(-1, 0, 0).Unix())
	return n, err
}

// DatabaseSize returns the size of the database in bytes.
func (db *DB) DatabaseSize(ctx context.Context) (int64, error) {
	var pages, size int64
	if err := db.r.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
		return 0, err
	}
	if err := db.r.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&size); err != nil {
		return 0, err
	}
	return pages * size, nil
}

// Vacuum compacts the database file.
func (db *DB) Vacuum(ctx context.Context) error {
	_, err := db.w.ExecContext(ctx, `VACUUM`)
	return err
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
