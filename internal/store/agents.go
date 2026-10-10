package store

import (
	"context"
	"database/sql"
	"errors"
)

const agentCols = `id, name, token_hash, token_prefix, country, interval, auto_import, group_monitor_id,
  created_at, last_seen, last_ip, version, hostname`

func scanAgent(s scanner) (*Agent, error) {
	var a Agent
	var auto int
	var group sql.NullInt64
	if err := s.Scan(&a.ID, &a.Name, &a.TokenHash, &a.TokenPrefix, &a.Country, &a.Interval, &auto, &group,
		&a.CreatedAt, &a.LastSeen, &a.LastIP, &a.Version, &a.Hostname); err != nil {
		return nil, err
	}
	a.AutoImport = auto == 1
	if group.Valid {
		g := group.Int64
		a.GroupMonitorID = &g
	}
	return &a, nil
}

// ListAgents returns every agent.
func (db *DB) ListAgents(ctx context.Context) ([]*Agent, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+agentCols+` FROM agents ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAgent returns one agent.
func (db *DB) GetAgent(ctx context.Context, id int64) (*Agent, error) {
	a, err := scanAgent(db.r.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// GetAgentByTokenHash finds the agent a token belongs to.
func (db *DB) GetAgentByTokenHash(ctx context.Context, hash string) (*Agent, error) {
	a, err := scanAgent(db.r.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE token_hash=?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// SaveAgent inserts or updates a's editable fields.
func (db *DB) SaveAgent(ctx context.Context, a *Agent) error {
	if a.ID == 0 {
		a.CreatedAt = now()
		res, err := db.w.ExecContext(ctx, `INSERT INTO agents (name, token_hash, token_prefix, country, interval,
			auto_import, group_monitor_id, created_at) VALUES (?,?,?,?,?,?,?,?)`,
			a.Name, a.TokenHash, a.TokenPrefix, a.Country, a.Interval, b2i(a.AutoImport), nullableID(a.GroupMonitorID), a.CreatedAt)
		if err != nil {
			return err
		}
		a.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := db.w.ExecContext(ctx, `UPDATE agents SET name=?, token_hash=?, token_prefix=?, country=?, interval=?,
		auto_import=?, group_monitor_id=? WHERE id=?`,
		a.Name, a.TokenHash, a.TokenPrefix, a.Country, a.Interval, b2i(a.AutoImport), nullableID(a.GroupMonitorID), a.ID)
	return err
}

// TouchAgent records a report, at most every 30 seconds.
func (db *DB) TouchAgent(ctx context.Context, id int64, ip, version, hostname string) {
	_, _ = db.w.ExecContext(ctx, `UPDATE agents SET last_seen=?, last_ip=?, version=?, hostname=?
		WHERE id=? AND (last_seen < ? OR last_ip <> ? OR version <> ?)`,
		now(), ip, version, hostname, id, now()-30, ip, version)
}

// DeleteAgent removes an agent.
func (db *DB) DeleteAgent(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM agents WHERE id=?`, id)
	return err
}

// ---------- private status API keys ----------

const statusKeyCols = `id, name, prefix, key_hash, status_page_id, allowed_origins, created_at, last_used, active`

func scanStatusKey(s scanner) (*StatusAPIKey, error) {
	var k StatusAPIKey
	var active int
	if err := s.Scan(&k.ID, &k.Name, &k.Prefix, &k.KeyHash, &k.StatusPageID, &k.AllowedOrigins, &k.CreatedAt, &k.LastUsed, &active); err != nil {
		return nil, err
	}
	k.Active = active == 1
	return &k, nil
}

// ListStatusAPIKeys returns every status API key.
func (db *DB) ListStatusAPIKeys(ctx context.Context) ([]*StatusAPIKey, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+statusKeyCols+` FROM status_api_keys ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*StatusAPIKey{}
	for rows.Next() {
		k, err := scanStatusKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// GetStatusAPIKeyByHash finds a key by the hash of its secret.
func (db *DB) GetStatusAPIKeyByHash(ctx context.Context, hash string) (*StatusAPIKey, error) {
	k, err := scanStatusKey(db.r.QueryRowContext(ctx, `SELECT `+statusKeyCols+` FROM status_api_keys WHERE key_hash=?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return k, err
}

// CreateStatusAPIKey stores a key.
func (db *DB) CreateStatusAPIKey(ctx context.Context, k *StatusAPIKey) error {
	k.CreatedAt = now()
	res, err := db.w.ExecContext(ctx, `INSERT INTO status_api_keys (name, prefix, key_hash, status_page_id, allowed_origins, created_at, active)
		VALUES (?,?,?,?,?,?,1)`, k.Name, k.Prefix, k.KeyHash, k.StatusPageID, k.AllowedOrigins, k.CreatedAt)
	if err != nil {
		return err
	}
	k.ID, _ = res.LastInsertId()
	k.Active = true
	return nil
}

// UpdateStatusAPIKey changes a key's name, origins and state.
func (db *DB) UpdateStatusAPIKey(ctx context.Context, k *StatusAPIKey) error {
	_, err := db.w.ExecContext(ctx, `UPDATE status_api_keys SET name=?, allowed_origins=?, active=? WHERE id=?`,
		k.Name, k.AllowedOrigins, b2i(k.Active), k.ID)
	return err
}

// DeleteStatusAPIKey removes a key.
func (db *DB) DeleteStatusAPIKey(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM status_api_keys WHERE id=?`, id)
	return err
}

// TouchStatusAPIKey records use, at most once a minute.
func (db *DB) TouchStatusAPIKey(ctx context.Context, id int64) {
	_, _ = db.w.ExecContext(ctx, `UPDATE status_api_keys SET last_used=? WHERE id=? AND last_used < ?`, now(), id, now()-60)
}

// SetOwner marks a user as the instance owner.
func (db *DB) SetOwner(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `UPDATE users SET is_owner = CASE WHEN id = ? THEN 1 ELSE 0 END`, id)
	return err
}
