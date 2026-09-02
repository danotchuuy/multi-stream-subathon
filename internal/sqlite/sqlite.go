// Package sqlite implements subathon.Repo on top of an embedded SQLite
// database, so timer state and contributor history survive restarts. It
// uses a pure-Go driver (no cgo) to keep cross-compiling and Docker builds
// simple.
package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id           TEXT PRIMARY KEY,
	display_name TEXT NOT NULL,
	created_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS identities (
	id                 TEXT PRIMARY KEY,
	user_id            TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	platform           TEXT NOT NULL,
	platform_user_id   TEXT NOT NULL,
	platform_username  TEXT NOT NULL,
	access_token       TEXT NOT NULL,
	refresh_token      TEXT,
	token_expires_at   TEXT,
	created_at         TEXT NOT NULL,
	updated_at         TEXT NOT NULL,
	UNIQUE(platform, platform_user_id)
);

CREATE INDEX IF NOT EXISTS idx_identities_user ON identities(user_id);

CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL,
	expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS timers (
	id                   TEXT PRIMARY KEY,
	user_id              TEXT REFERENCES users(id) ON DELETE SET NULL,
	name                 TEXT NOT NULL,
	running              INTEGER NOT NULL DEFAULT 0,
	started_at           TEXT,
	ends_at              TEXT,
	remaining_seconds    INTEGER NOT NULL DEFAULT 0,
	total_added_seconds  INTEGER NOT NULL DEFAULT 0,
	created_at           TEXT NOT NULL,
	updated_at           TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_timers_user ON timers(user_id);

CREATE TABLE IF NOT EXISTS events (
	id             TEXT PRIMARY KEY,
	timer_id       TEXT NOT NULL REFERENCES timers(id) ON DELETE CASCADE,
	platform       TEXT NOT NULL,
	type           TEXT NOT NULL,
	username       TEXT NOT NULL,
	seconds_added  INTEGER NOT NULL,
	amount         REAL,
	occurred_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_timer_occurred ON events(timer_id, occurred_at);
`

// Repo is a subathon.Repo backed by a SQLite database file.
type Repo struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path, creating
// its parent directory too so a fresh volume mount works out of the box,
// and applies the schema. Callers must Close it on shutdown.
func Open(path string) (*Repo, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db directory %q: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %q: %w", path, err)
	}

	// A single connection avoids SQLITE_BUSY errors from this driver's
	// lack of built-in write serialization; this server's write volume
	// (control actions + events, not per-tick broadcasts) doesn't need
	// concurrent writers.
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("set %q: %w", pragma, err)
		}
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	// Additive migration for databases created before accounts existed:
	// CREATE TABLE IF NOT EXISTS above won't add a column to a
	// pre-existing timers table.
	if err := addColumnIfMissing(db, "timers", "user_id", "TEXT REFERENCES users(id) ON DELETE SET NULL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.user_id: %w", err)
	}

	return &Repo{db: db}, nil
}

// addColumnIfMissing runs `ALTER TABLE table ADD COLUMN column def` only if
// that column doesn't already exist, so it's safe to call on every start.
func addColumnIfMissing(db *sql.DB, table, column, def string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("scan %s column info: %w", table, err)
		}
		if name == column {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, def)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

// Close closes the underlying database connection.
func (r *Repo) Close() error {
	return r.db.Close()
}

func (r *Repo) CreateTimer(rec subathon.TimerRecord) error {
	_, err := r.db.Exec(
		`INSERT INTO timers (id, user_id, name, running, started_at, ends_at, remaining_seconds, total_added_seconds, created_at, updated_at)
		 VALUES (?, ?, ?, 0, NULL, NULL, 0, 0, ?, ?)`,
		rec.ID, nullableString(rec.UserID), rec.Name, formatTime(rec.CreatedAt), formatTime(rec.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert timer: %w", err)
	}
	return nil
}

func (r *Repo) ListTimers() ([]subathon.TimerRecord, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, name, running, started_at, ends_at, remaining_seconds, total_added_seconds, created_at, updated_at
		FROM timers
		ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("query timers: %w", err)
	}
	defer rows.Close()

	var out []subathon.TimerRecord
	for rows.Next() {
		var rec subathon.TimerRecord
		var running int
		var userID, startedAt, endsAt sql.NullString
		var createdAt, updatedAt string

		if err := rows.Scan(&rec.ID, &userID, &rec.Name, &running, &startedAt, &endsAt,
			&rec.RemainingSecs, &rec.TotalAddedSecs, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan timer row: %w", err)
		}

		rec.UserID = userID.String
		rec.Running = running != 0
		rec.StartedAt = parseTime(startedAt.String)
		rec.EndsAt = parseTime(endsAt.String)
		rec.CreatedAt = parseTime(createdAt)
		rec.UpdatedAt = parseTime(updatedAt)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *Repo) SaveTimerState(rec subathon.TimerRecord) error {
	_, err := r.db.Exec(
		`UPDATE timers
		 SET running = ?, started_at = ?, ends_at = ?, remaining_seconds = ?, total_added_seconds = ?, updated_at = ?
		 WHERE id = ?`,
		boolToInt(rec.Running), nullableTime(rec.StartedAt), nullableTime(rec.EndsAt),
		rec.RemainingSecs, rec.TotalAddedSecs, formatTime(rec.UpdatedAt), rec.ID,
	)
	if err != nil {
		return fmt.Errorf("update timer %s: %w", rec.ID, err)
	}
	return nil
}

func (r *Repo) InsertEvent(e subathon.Event) error {
	_, err := r.db.Exec(
		`INSERT INTO events (id, timer_id, platform, type, username, seconds_added, amount, occurred_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.TimerID, string(e.Platform), string(e.Type), e.Username, e.SecondsAdded, e.Amount, formatTime(e.Occurred),
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

func (r *Repo) RecentEvents(timerID string, limit int) ([]subathon.Event, error) {
	rows, err := r.db.Query(
		`SELECT id, timer_id, platform, type, username, seconds_added, amount, occurred_at
		 FROM events
		 WHERE timer_id = ?
		 ORDER BY occurred_at DESC
		 LIMIT ?`,
		timerID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query events for timer %s: %w", timerID, err)
	}
	defer rows.Close()

	var out []subathon.Event
	for rows.Next() {
		var e subathon.Event
		var platform, typ, occurredAt string
		var amount sql.NullFloat64

		if err := rows.Scan(&e.ID, &e.TimerID, &platform, &typ, &e.Username, &e.SecondsAdded, &amount, &occurredAt); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}

		e.Platform = subathon.Platform(platform)
		e.Type = subathon.EventType(typ)
		e.Amount = amount.Float64
		e.Occurred = parseTime(occurredAt)
		out = append(out, e)
	}
	return out, rows.Err()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// nullableTime returns nil (SQL NULL) for a zero time rather than an
// empty string, matching how CreateTimer seeds started_at/ends_at as NULL.
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return formatTime(t)
}

// nullableString returns nil (SQL NULL) for an empty string.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
