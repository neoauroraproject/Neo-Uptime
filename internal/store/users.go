package store

import (
	"context"
	"database/sql"
	"errors"
)

const userCols = `id, username, password_hash, role, totp_secret, totp_enabled, created_at, last_login, is_owner`

func scanUser(s scanner) (*User, error) {
	var u User
	var totp, owner int
	if err := s.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.TOTPSecret, &totp, &u.CreatedAt, &u.LastLogin, &owner); err != nil {
		return nil, err
	}
	u.TOTPEnabled = totp == 1
	u.IsOwner = owner == 1
	return &u, nil
}

// CountUsers returns how many accounts exist.
func (db *DB) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// ListUsers returns every account.
func (db *DB) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// GetUser returns one account by id.
func (db *DB) GetUser(ctx context.Context, id int64) (*User, error) {
	u, err := scanUser(db.r.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// GetUserByName returns one account by username (case-insensitive).
func (db *DB) GetUserByName(ctx context.Context, name string) (*User, error) {
	u, err := scanUser(db.r.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// CreateUser inserts u and sets u.ID.
func (db *DB) CreateUser(ctx context.Context, u *User) error {
	u.CreatedAt = now()
	res, err := db.w.ExecContext(ctx, `INSERT INTO users (username, password_hash, role, totp_secret, totp_enabled, created_at, is_owner)
		VALUES (?,?,?,?,?,?,?)`, u.Username, u.PasswordHash, u.Role, u.TOTPSecret, b2i(u.TOTPEnabled), u.CreatedAt, b2i(u.IsOwner))
	if err != nil {
		return err
	}
	u.ID, _ = res.LastInsertId()
	return nil
}

// UpdateUser saves username, role, password hash and second factor.
func (db *DB) UpdateUser(ctx context.Context, u *User) error {
	_, err := db.w.ExecContext(ctx, `UPDATE users SET username=?, password_hash=?, role=?, totp_secret=?, totp_enabled=? WHERE id=?`,
		u.Username, u.PasswordHash, u.Role, u.TOTPSecret, b2i(u.TOTPEnabled), u.ID)
	return err
}

// TouchLogin records a successful sign-in.
func (db *DB) TouchLogin(ctx context.Context, id int64) {
	_, _ = db.w.ExecContext(ctx, `UPDATE users SET last_login=? WHERE id=?`, now(), id)
}

// DeleteUser removes an account and its sessions and keys.
func (db *DB) DeleteUser(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	return err
}

// CreateSession stores a session.
func (db *DB) CreateSession(ctx context.Context, s *Session) error {
	_, err := db.w.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, created_at, expires_at, last_seen, ip, user_agent)
		VALUES (?,?,?,?,?,?,?)`, s.TokenHash, s.UserID, s.CreatedAt, s.ExpiresAt, s.LastSeen, s.IP, s.UserAgent)
	return err
}

// GetSession returns a live session by token hash.
func (db *DB) GetSession(ctx context.Context, hash string) (*Session, error) {
	var s Session
	err := db.r.QueryRowContext(ctx, `SELECT token_hash, user_id, created_at, expires_at, last_seen, ip, user_agent
		FROM sessions WHERE token_hash = ? AND expires_at > ?`, hash, now()).
		Scan(&s.TokenHash, &s.UserID, &s.CreatedAt, &s.ExpiresAt, &s.LastSeen, &s.IP, &s.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.ID = s.TokenHash[:12]
	return &s, nil
}

// TouchSession extends a session.
func (db *DB) TouchSession(ctx context.Context, hash string, expires int64) {
	_, _ = db.w.ExecContext(ctx, `UPDATE sessions SET last_seen=?, expires_at=? WHERE token_hash=?`, now(), expires, hash)
}

// ListSessions returns a user's live sessions.
func (db *DB) ListSessions(ctx context.Context, userID int64) ([]*Session, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT token_hash, user_id, created_at, expires_at, last_seen, ip, user_agent
		FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY last_seen DESC`, userID, now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.TokenHash, &s.UserID, &s.CreatedAt, &s.ExpiresAt, &s.LastSeen, &s.IP, &s.UserAgent); err != nil {
			return nil, err
		}
		s.ID = s.TokenHash[:12]
		out = append(out, &s)
	}
	return out, rows.Err()
}

// DeleteSession signs one session out.
func (db *DB) DeleteSession(ctx context.Context, hash string) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, hash)
	return err
}

// DeleteSessionByPublicID signs out the session whose public id matches.
func (db *DB) DeleteSessionByPublicID(ctx context.Context, userID int64, id string) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND substr(token_hash,1,12)=?`, userID, id)
	return err
}

// DeleteUserSessions signs a user out everywhere, except keepHash.
func (db *DB) DeleteUserSessions(ctx context.Context, userID int64, keepHash string) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND token_hash<>?`, userID, keepHash)
	return err
}

// PurgeExpiredSessions removes dead sessions.
func (db *DB) PurgeExpiredSessions(ctx context.Context) {
	_, _ = db.w.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now())
}

const apiKeyCols = `id, name, prefix, key_hash, scope, COALESCE(user_id,0), created_at, expires_at, last_used, active`

func scanAPIKey(s scanner) (*APIKey, error) {
	var k APIKey
	var active int
	if err := s.Scan(&k.ID, &k.Name, &k.Prefix, &k.KeyHash, &k.Scope, &k.UserID, &k.CreatedAt, &k.ExpiresAt, &k.LastUsed, &active); err != nil {
		return nil, err
	}
	k.Active = active == 1
	return &k, nil
}

// ListAPIKeys returns every key.
func (db *DB) ListAPIKeys(ctx context.Context) ([]*APIKey, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+apiKeyCols+` FROM api_keys ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// GetAPIKeyByHash returns a key by the hash of its secret.
func (db *DB) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	k, err := scanAPIKey(db.r.QueryRowContext(ctx, `SELECT `+apiKeyCols+` FROM api_keys WHERE key_hash = ?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return k, err
}

// CreateAPIKey stores a key.
func (db *DB) CreateAPIKey(ctx context.Context, k *APIKey) error {
	k.CreatedAt = now()
	res, err := db.w.ExecContext(ctx, `INSERT INTO api_keys (name, prefix, key_hash, scope, user_id, created_at, expires_at, active)
		VALUES (?,?,?,?,?,?,?,1)`, k.Name, k.Prefix, k.KeyHash, k.Scope, k.UserID, k.CreatedAt, k.ExpiresAt)
	if err != nil {
		return err
	}
	k.ID, _ = res.LastInsertId()
	k.Active = true
	return nil
}

// SetAPIKeyActive enables or disables a key.
func (db *DB) SetAPIKeyActive(ctx context.Context, id int64, active bool) error {
	_, err := db.w.ExecContext(ctx, `UPDATE api_keys SET active=? WHERE id=?`, b2i(active), id)
	return err
}

// DeleteAPIKey removes a key.
func (db *DB) DeleteAPIKey(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM api_keys WHERE id=?`, id)
	return err
}

// TouchAPIKey records that a key was used, at most once a minute.
func (db *DB) TouchAPIKey(ctx context.Context, id int64) {
	_, _ = db.w.ExecContext(ctx, `UPDATE api_keys SET last_used=? WHERE id=? AND last_used < ?`, now(), id, now()-60)
}
