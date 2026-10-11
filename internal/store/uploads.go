package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Upload is an image uploaded through the admin, such as a status page logo.
// It is served at /uploads/<Name>.
type Upload struct {
	Name      string `json:"name"`
	Mime      string `json:"mime"`
	Data      []byte `json:"data"`
	CreatedAt int64  `json:"createdAt"`
}

// UploadPrefix is the URL path uploads are served under.
const UploadPrefix = "/uploads/"

// SaveUpload stores u. A name that already exists is left as it is.
func (db *DB) SaveUpload(ctx context.Context, u *Upload) error {
	if u.CreatedAt == 0 {
		u.CreatedAt = now()
	}
	_, err := db.w.ExecContext(ctx, `INSERT OR IGNORE INTO uploads (name, mime, data, created_at) VALUES (?,?,?,?)`,
		u.Name, u.Mime, u.Data, u.CreatedAt)
	return err
}

// GetUpload returns one upload by name.
func (db *DB) GetUpload(ctx context.Context, name string) (*Upload, error) {
	var u Upload
	err := db.r.QueryRowContext(ctx, `SELECT name, mime, data, created_at FROM uploads WHERE name=?`, name).
		Scan(&u.Name, &u.Mime, &u.Data, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

// UsedUploads returns the uploads a status page refers to.
func (db *DB) UsedUploads(ctx context.Context) ([]*Upload, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT name, mime, data, created_at FROM uploads
		WHERE ? || name IN (SELECT logo FROM status_pages)`, UploadPrefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Upload{}
	for rows.Next() {
		var u Upload
		if err := rows.Scan(&u.Name, &u.Mime, &u.Data, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &u)
	}
	return out, rows.Err()
}

// PruneUploads deletes uploads older than before that no status page uses.
// Fresh ones are kept: they may belong to a page that is still being edited.
func (db *DB) PruneUploads(ctx context.Context, before int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM uploads WHERE created_at < ?
		AND ? || name NOT IN (SELECT logo FROM status_pages)`, before, UploadPrefix)
	return err
}

// UploadName returns the upload name in a /uploads/ URL, or "".
func UploadName(url string) string {
	if name, ok := strings.CutPrefix(url, UploadPrefix); ok && name != "" && !strings.ContainsAny(name, "/\\") {
		return name
	}
	return ""
}
