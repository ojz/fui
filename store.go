package main

import (
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type PageInfo struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
	StateCount int    `json:"state_count"`
	Done       bool   `json:"done"`
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// modernc/sqlite is happiest with a single writer connection.
	db.SetMaxOpenConns(1)
	schema := `
CREATE TABLE IF NOT EXISTS pages (
	id           TEXT PRIMARY KEY,
	title        TEXT NOT NULL DEFAULT '',
	html         TEXT NOT NULL,
	created_at   INTEGER NOT NULL,
	updated_at   INTEGER NOT NULL,
	done_at      INTEGER
);
CREATE TABLE IF NOT EXISTS state (
	page_id    TEXT NOT NULL,
	key        TEXT NOT NULL,
	value      TEXT NOT NULL,
	updated_at INTEGER NOT NULL,
	PRIMARY KEY (page_id, key)
);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() int64 { return time.Now().UnixMilli() }

func (s *Store) UpsertPage(id, title, html string) error {
	t := now()
	_, err := s.db.Exec(`
INSERT INTO pages (id, title, html, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET title = excluded.title, html = excluded.html, updated_at = excluded.updated_at`,
		id, title, html, t, t)
	return err
}

func (s *Store) GetHTML(id string) (string, bool, error) {
	var html string
	err := s.db.QueryRow(`SELECT html FROM pages WHERE id = ?`, id).Scan(&html)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return html, err == nil, err
}

func (s *Store) ListPages() ([]PageInfo, error) {
	rows, err := s.db.Query(`
SELECT p.id, p.title, p.created_at, p.updated_at, p.done_at IS NOT NULL,
       (SELECT COUNT(*) FROM state st WHERE st.page_id = p.id)
FROM pages p ORDER BY p.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pages := []PageInfo{}
	for rows.Next() {
		var p PageInfo
		if err := rows.Scan(&p.ID, &p.Title, &p.CreatedAt, &p.UpdatedAt, &p.Done, &p.StateCount); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, rows.Err()
}

func (s *Store) DeletePage(id string) error {
	if _, err := s.db.Exec(`DELETE FROM state WHERE page_id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM pages WHERE id = ?`, id)
	return err
}

// SetState stores one key. A nil value deletes the key.
func (s *Store) SetState(pageID, key string, value *string) error {
	if value == nil {
		_, err := s.db.Exec(`DELETE FROM state WHERE page_id = ? AND key = ?`, pageID, key)
		return err
	}
	_, err := s.db.Exec(`
INSERT INTO state (page_id, key, value, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(page_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		pageID, key, *value, now())
	return err
}

func (s *Store) ClearState(pageID string) error {
	_, err := s.db.Exec(`DELETE FROM state WHERE page_id = ?`, pageID)
	return err
}

func (s *Store) GetState(pageID string) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM state WHERE page_id = ?`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// MarkDone records a completion signal.
func (s *Store) MarkDone(pageID string) error {
	res, err := s.db.Exec(`UPDATE pages SET done_at = ? WHERE id = ?`, now(), pageID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("page not found")
	}
	return nil
}

// ConsumeDone atomically checks and clears a pending completion signal.
func (s *Store) ConsumeDone(pageID string) (bool, error) {
	res, err := s.db.Exec(`UPDATE pages SET done_at = NULL WHERE id = ? AND done_at IS NOT NULL`, pageID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// PageUpdatedAt returns when the page's HTML was last written, and whether
// the page exists at all.
func (s *Store) PageUpdatedAt(id string) (int64, bool, error) {
	var t int64
	err := s.db.QueryRow(`SELECT updated_at FROM pages WHERE id = ?`, id).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return t, err == nil, err
}
