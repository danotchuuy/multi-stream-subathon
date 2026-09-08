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
	id                            TEXT PRIMARY KEY,
	user_id                       TEXT REFERENCES users(id) ON DELETE SET NULL,
	name                          TEXT NOT NULL,
	running                       INTEGER NOT NULL DEFAULT 0,
	started_at                    TEXT,
	ends_at                       TEXT,
	remaining_seconds             INTEGER NOT NULL DEFAULT 0,
	total_added_seconds           INTEGER NOT NULL DEFAULT 0,
	total_money_raised            REAL NOT NULL DEFAULT 0,
	money_goal                    REAL NOT NULL DEFAULT 0,
	twitch_broadcaster_id         TEXT,
	twitch_broadcaster_username   TEXT,
	kick_broadcaster_id           TEXT,
	kick_broadcaster_username     TEXT,
	youtube_channel_id            TEXT,
	youtube_channel_title         TEXT,
	stream_elements_token         TEXT NOT NULL DEFAULT '',
	stream_elements_refresh_token TEXT NOT NULL DEFAULT '',
	stream_elements_token_expires_at TEXT,
	stream_elements_channel_id    TEXT NOT NULL DEFAULT '',
	stream_elements_display_name  TEXT NOT NULL DEFAULT '',
	locked                        INTEGER NOT NULL DEFAULT 0,
	hidden                        INTEGER NOT NULL DEFAULT 0,
	ended                         INTEGER NOT NULL DEFAULT 0,
	overlay_timer_bg              TEXT NOT NULL DEFAULT '',
	overlay_timer_text            TEXT NOT NULL DEFAULT '',
	overlay_money_bg              TEXT NOT NULL DEFAULT '',
	overlay_money_text            TEXT NOT NULL DEFAULT '',
	overlay_goal_bg               TEXT NOT NULL DEFAULT '',
	overlay_goal_text             TEXT NOT NULL DEFAULT '',
	overlay_goal_amount_bg        TEXT NOT NULL DEFAULT '',
	overlay_goal_amount_text      TEXT NOT NULL DEFAULT '',
	subs_given                    INTEGER NOT NULL DEFAULT 0,
	bits_given                    INTEGER NOT NULL DEFAULT 0,
	donations_given               INTEGER NOT NULL DEFAULT 0,
	show_stats_rotation           INTEGER NOT NULL DEFAULT 0,
	stat_icon_style               TEXT NOT NULL DEFAULT '',
	stat_icon_outline             INTEGER NOT NULL DEFAULT 0,
	stat_icon_subs                TEXT NOT NULL DEFAULT '',
	stat_icon_bits                TEXT NOT NULL DEFAULT '',
	stat_icon_donations           TEXT NOT NULL DEFAULT '',
	stat_icon_subs_color          TEXT NOT NULL DEFAULT '',
	stat_icon_bits_color          TEXT NOT NULL DEFAULT '',
	stat_icon_donations_color     TEXT NOT NULL DEFAULT '',
	panel_bg                      TEXT NOT NULL DEFAULT '',
	panel_text                    TEXT NOT NULL DEFAULT '',
	panel_accent_bg               TEXT NOT NULL DEFAULT '',
	panel_accent_text             TEXT NOT NULL DEFAULT '',
	created_at                    TEXT NOT NULL,
	updated_at                    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_timers_user ON timers(user_id);

CREATE TABLE IF NOT EXISTS events (
	id             TEXT PRIMARY KEY,
	timer_id       TEXT NOT NULL REFERENCES timers(id) ON DELETE CASCADE,
	platform       TEXT NOT NULL,
	type           TEXT NOT NULL,
	username       TEXT NOT NULL,
	seconds_added  INTEGER NOT NULL,
	money_added    REAL NOT NULL DEFAULT 0,
	amount         REAL,
	occurred_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_timer_occurred ON events(timer_id, occurred_at);

CREATE TABLE IF NOT EXISTS reward_rules (
	timer_id TEXT NOT NULL REFERENCES timers(id) ON DELETE CASCADE,
	item     TEXT NOT NULL,
	platform TEXT NOT NULL,
	seconds  INTEGER NOT NULL,
	PRIMARY KEY (timer_id, item, platform)
);

CREATE TABLE IF NOT EXISTS money_rules (
	timer_id TEXT NOT NULL REFERENCES timers(id) ON DELETE CASCADE,
	item     TEXT NOT NULL,
	platform TEXT NOT NULL,
	dollars  REAL NOT NULL,
	PRIMARY KEY (timer_id, item, platform)
);

CREATE TABLE IF NOT EXISTS money_milestones (
	timer_id TEXT NOT NULL REFERENCES timers(id) ON DELETE CASCADE,
	amount   REAL NOT NULL,
	label    TEXT NOT NULL,
	hidden   INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (timer_id, amount)
);

CREATE TABLE IF NOT EXISTS timer_moderators (
	timer_id   TEXT NOT NULL REFERENCES timers(id) ON DELETE CASCADE,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL,
	PRIMARY KEY (timer_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_timer_moderators_user ON timer_moderators(user_id);
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
	if err := addColumnIfMissing(db, "timers", "twitch_broadcaster_id", "TEXT"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.twitch_broadcaster_id: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "twitch_broadcaster_username", "TEXT"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.twitch_broadcaster_username: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "kick_broadcaster_id", "TEXT"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.kick_broadcaster_id: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "kick_broadcaster_username", "TEXT"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.kick_broadcaster_username: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "youtube_channel_id", "TEXT"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.youtube_channel_id: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "youtube_channel_title", "TEXT"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.youtube_channel_title: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "total_money_raised", "REAL NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.total_money_raised: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "money_goal", "REAL NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.money_goal: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "locked", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.locked: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "hidden", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.hidden: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "ended", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.ended: %w", err)
	}
	if err := addColumnIfMissing(db, "events", "money_added", "REAL NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate events.money_added: %w", err)
	}
	for _, col := range []string{
		"overlay_timer_bg", "overlay_timer_text", "overlay_money_bg", "overlay_money_text",
		"overlay_goal_bg", "overlay_goal_text", "overlay_goal_amount_bg", "overlay_goal_amount_text",
		"stream_elements_token", "stream_elements_refresh_token",
		"stream_elements_channel_id", "stream_elements_display_name",
	} {
		if err := addColumnIfMissing(db, "timers", col, "TEXT NOT NULL DEFAULT ''"); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate timers.%s: %w", col, err)
		}
	}
	// Nullable (no DEFAULT ''): a timestamp, not a string, same treatment
	// as started_at/ends_at elsewhere in this schema.
	if err := addColumnIfMissing(db, "timers", "stream_elements_token_expires_at", "TEXT"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.stream_elements_token_expires_at: %w", err)
	}
	// A row with a token but no refresh token predates this integration's
	// switch from a pasted-in JWT to OAuth2 (every token this version
	// saves comes paired with a refresh token — see
	// Timer.SetStreamElementsAccount) — that JWT can't be used with the
	// OAuth2 "oAuth <token>" auth scheme this version sends instead of
	// "Bearer", so clear it rather than let the poller retry a doomed
	// request forever. The owner reconnects from the dashboard.
	if _, err := db.Exec(`
		UPDATE timers
		SET stream_elements_token = '', stream_elements_channel_id = '', stream_elements_display_name = ''
		WHERE stream_elements_token != '' AND stream_elements_refresh_token = ''`); err != nil {
		db.Close()
		return nil, fmt.Errorf("clear pre-oauth2 stream elements tokens: %w", err)
	}
	if err := addColumnIfMissing(db, "money_milestones", "hidden", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate money_milestones.hidden: %w", err)
	}

	// subs_given/bits_given/donations_given back the overlay's rotating
	// stat list (see subathon.Timer.contributionCounts) and, like every
	// other running total in this schema, are maintained incrementally
	// from here on rather than recomputed. That leaves a gap for timers
	// that already have contribution history predating these columns, so
	// — only the first time each column is actually added, not on every
	// startup — backfill them from that existing events history.
	statsColumnsExisted, err := hasColumn(db, "timers", "subs_given")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("check timers.subs_given: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "subs_given", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.subs_given: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "bits_given", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.bits_given: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "donations_given", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.donations_given: %w", err)
	}
	if err := addColumnIfMissing(db, "timers", "show_stats_rotation", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.show_stats_rotation: %w", err)
	}
	if !statsColumnsExisted {
		// Mirrors Timer.contributionCounts: subs/resubs/gifted subs and
		// bits/Kicks sum each event's amount (falling back to 1 for a row
		// with no amount recorded), donations count events since amount
		// there is a dollar figure, not a "how many".
		if _, err := db.Exec(`
			UPDATE timers SET subs_given = (
				SELECT COALESCE(SUM(CASE WHEN amount IS NULL OR amount <= 0 THEN 1 ELSE amount END), 0)
				FROM events WHERE events.timer_id = timers.id AND events.type IN ('sub', 'resub', 'gifted_sub')
			)`); err != nil {
			db.Close()
			return nil, fmt.Errorf("backfill timers.subs_given: %w", err)
		}
		if _, err := db.Exec(`
			UPDATE timers SET bits_given = (
				SELECT COALESCE(SUM(CASE WHEN amount IS NULL OR amount <= 0 THEN 1 ELSE amount END), 0)
				FROM events WHERE events.timer_id = timers.id AND events.type = 'bits'
			)`); err != nil {
			db.Close()
			return nil, fmt.Errorf("backfill timers.bits_given: %w", err)
		}
		if _, err := db.Exec(`
			UPDATE timers SET donations_given = (
				SELECT COUNT(*) FROM events WHERE events.timer_id = timers.id AND events.type = 'donation'
			)`); err != nil {
			db.Close()
			return nil, fmt.Errorf("backfill timers.donations_given: %w", err)
		}
	}
	for _, col := range []string{
		"stat_icon_style", "stat_icon_subs", "stat_icon_bits", "stat_icon_donations",
		"stat_icon_subs_color", "stat_icon_bits_color", "stat_icon_donations_color",
		"panel_bg", "panel_text", "panel_accent_bg", "panel_accent_text",
	} {
		if err := addColumnIfMissing(db, "timers", col, "TEXT NOT NULL DEFAULT ''"); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate timers.%s: %w", col, err)
		}
	}
	if err := addColumnIfMissing(db, "timers", "stat_icon_outline", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate timers.stat_icon_outline: %w", err)
	}

	// reward_rules moved from being keyed by user_id to timer_id (reward
	// rates are now per-timer, not account-wide). A pre-existing table
	// from before that change has no timer_id column; since reward rules
	// are just rate configuration (not event history), drop and recreate
	// rather than trying to guess which of a user's timers each rule
	// should move to.
	if err := recreateTableIfColumnMissing(db, "reward_rules", "timer_id", schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate reward_rules.timer_id: %w", err)
	}

	return &Repo{db: db}, nil
}

// hasColumn reports whether table already has column.
func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", table, err)
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
			return false, fmt.Errorf("scan %s column info: %w", table, err)
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// addColumnIfMissing runs `ALTER TABLE table ADD COLUMN column def` only if
// that column doesn't already exist, so it's safe to call on every start.
func addColumnIfMissing(db *sql.DB, table, column, def string) error {
	exists, err := hasColumn(db, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, def)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

// recreateTableIfColumnMissing drops and recreates table (by re-running
// fullSchema, which CREATE TABLE IF NOT EXISTS's it back) if table exists
// but lacks column — i.e. it predates a schema change that isn't a simple
// added column. Only appropriate for tables holding derived/configuration
// data that's fine to lose, never for tables with durable history.
func recreateTableIfColumnMissing(db *sql.DB, table, column, fullSchema string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}

	var found, sawAnyColumn bool
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
			rows.Close()
			return fmt.Errorf("scan %s column info: %w", table, err)
		}
		sawAnyColumn = true
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if !sawAnyColumn || found {
		return nil // table doesn't exist yet, or already has the column
	}

	if _, err := db.Exec(fmt.Sprintf("DROP TABLE %s", table)); err != nil {
		return fmt.Errorf("drop stale %s: %w", table, err)
	}
	if _, err := db.Exec(fullSchema); err != nil {
		return fmt.Errorf("recreate %s: %w", table, err)
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
		SELECT id, user_id, name, running, started_at, ends_at, remaining_seconds, total_added_seconds,
		       total_money_raised, money_goal,
		       twitch_broadcaster_id, twitch_broadcaster_username,
		       kick_broadcaster_id, kick_broadcaster_username,
		       youtube_channel_id, youtube_channel_title, locked, hidden, ended,
		       overlay_timer_bg, overlay_timer_text, overlay_money_bg, overlay_money_text,
		       overlay_goal_bg, overlay_goal_text, overlay_goal_amount_bg, overlay_goal_amount_text,
		       stream_elements_token, stream_elements_refresh_token, stream_elements_token_expires_at,
		       stream_elements_channel_id, stream_elements_display_name,
		       subs_given, bits_given, donations_given, show_stats_rotation,
		       stat_icon_style, stat_icon_outline, stat_icon_subs, stat_icon_bits, stat_icon_donations,
		       stat_icon_subs_color, stat_icon_bits_color, stat_icon_donations_color,
		       panel_bg, panel_text, panel_accent_bg, panel_accent_text,
		       created_at, updated_at
		FROM timers
		ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("query timers: %w", err)
	}
	defer rows.Close()

	var out []subathon.TimerRecord
	for rows.Next() {
		var rec subathon.TimerRecord
		var running, locked, hidden, ended, showStatsRotation, statIconOutline int
		var userID, startedAt, endsAt sql.NullString
		var twitchBroadcasterID, twitchBroadcasterUsername sql.NullString
		var kickBroadcasterID, kickBroadcasterUsername sql.NullString
		var youtubeChannelID, youtubeChannelTitle sql.NullString
		var streamElementsTokenExpiresAt sql.NullString
		var createdAt, updatedAt string

		if err := rows.Scan(&rec.ID, &userID, &rec.Name, &running, &startedAt, &endsAt,
			&rec.RemainingSecs, &rec.TotalAddedSecs, &rec.TotalMoneyRaised, &rec.MoneyGoal,
			&twitchBroadcasterID, &twitchBroadcasterUsername,
			&kickBroadcasterID, &kickBroadcasterUsername,
			&youtubeChannelID, &youtubeChannelTitle, &locked, &hidden, &ended,
			&rec.OverlayTimerBg, &rec.OverlayTimerText, &rec.OverlayMoneyBg, &rec.OverlayMoneyText,
			&rec.OverlayGoalBg, &rec.OverlayGoalText, &rec.OverlayGoalAmountBg, &rec.OverlayGoalAmountText,
			&rec.StreamElementsToken, &rec.StreamElementsRefreshToken, &streamElementsTokenExpiresAt,
			&rec.StreamElementsChannelID, &rec.StreamElementsDisplayName,
			&rec.SubsGiven, &rec.BitsGiven, &rec.DonationsGiven, &showStatsRotation,
			&rec.StatIconStyle, &statIconOutline, &rec.StatIconSubs, &rec.StatIconBits, &rec.StatIconDonations,
			&rec.StatIconSubsColor, &rec.StatIconBitsColor, &rec.StatIconDonationsColor,
			&rec.PanelBg, &rec.PanelText, &rec.PanelAccentBg, &rec.PanelAccentText,
			&createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan timer row: %w", err)
		}

		rec.UserID = userID.String
		rec.Running = running != 0
		rec.StartedAt = parseTime(startedAt.String)
		rec.EndsAt = parseTime(endsAt.String)
		rec.TwitchBroadcasterID = twitchBroadcasterID.String
		rec.TwitchBroadcasterUsername = twitchBroadcasterUsername.String
		rec.KickBroadcasterID = kickBroadcasterID.String
		rec.KickBroadcasterUsername = kickBroadcasterUsername.String
		rec.YouTubeChannelID = youtubeChannelID.String
		rec.YouTubeChannelTitle = youtubeChannelTitle.String
		rec.StreamElementsTokenExpiresAt = parseTime(streamElementsTokenExpiresAt.String)
		rec.Locked = locked != 0
		rec.Hidden = hidden != 0
		rec.Ended = ended != 0
		rec.StatsRotationEnabled = showStatsRotation != 0
		rec.StatIconOutline = statIconOutline != 0
		rec.CreatedAt = parseTime(createdAt)
		rec.UpdatedAt = parseTime(updatedAt)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *Repo) SaveTimerState(rec subathon.TimerRecord) error {
	_, err := r.db.Exec(
		`UPDATE timers
		 SET running = ?, started_at = ?, ends_at = ?, remaining_seconds = ?, total_added_seconds = ?,
		     total_money_raised = ?, money_goal = ?,
		     twitch_broadcaster_id = ?, twitch_broadcaster_username = ?,
		     kick_broadcaster_id = ?, kick_broadcaster_username = ?,
		     youtube_channel_id = ?, youtube_channel_title = ?, locked = ?, hidden = ?, ended = ?,
		     overlay_timer_bg = ?, overlay_timer_text = ?, overlay_money_bg = ?, overlay_money_text = ?,
		     overlay_goal_bg = ?, overlay_goal_text = ?, overlay_goal_amount_bg = ?, overlay_goal_amount_text = ?,
		     stream_elements_token = ?, stream_elements_refresh_token = ?, stream_elements_token_expires_at = ?,
		     stream_elements_channel_id = ?, stream_elements_display_name = ?,
		     subs_given = ?, bits_given = ?, donations_given = ?, show_stats_rotation = ?,
		     stat_icon_style = ?, stat_icon_outline = ?, stat_icon_subs = ?, stat_icon_bits = ?, stat_icon_donations = ?,
		     stat_icon_subs_color = ?, stat_icon_bits_color = ?, stat_icon_donations_color = ?,
		     panel_bg = ?, panel_text = ?, panel_accent_bg = ?, panel_accent_text = ?,
		     updated_at = ?
		 WHERE id = ?`,
		boolToInt(rec.Running), nullableTime(rec.StartedAt), nullableTime(rec.EndsAt),
		rec.RemainingSecs, rec.TotalAddedSecs, rec.TotalMoneyRaised, rec.MoneyGoal,
		nullableString(rec.TwitchBroadcasterID), nullableString(rec.TwitchBroadcasterUsername),
		nullableString(rec.KickBroadcasterID), nullableString(rec.KickBroadcasterUsername),
		nullableString(rec.YouTubeChannelID), nullableString(rec.YouTubeChannelTitle),
		boolToInt(rec.Locked), boolToInt(rec.Hidden), boolToInt(rec.Ended),
		rec.OverlayTimerBg, rec.OverlayTimerText, rec.OverlayMoneyBg, rec.OverlayMoneyText,
		rec.OverlayGoalBg, rec.OverlayGoalText, rec.OverlayGoalAmountBg, rec.OverlayGoalAmountText,
		rec.StreamElementsToken, rec.StreamElementsRefreshToken, nullableTime(rec.StreamElementsTokenExpiresAt),
		rec.StreamElementsChannelID, rec.StreamElementsDisplayName,
		rec.SubsGiven, rec.BitsGiven, rec.DonationsGiven, boolToInt(rec.StatsRotationEnabled),
		rec.StatIconStyle, boolToInt(rec.StatIconOutline), rec.StatIconSubs, rec.StatIconBits, rec.StatIconDonations,
		rec.StatIconSubsColor, rec.StatIconBitsColor, rec.StatIconDonationsColor,
		rec.PanelBg, rec.PanelText, rec.PanelAccentBg, rec.PanelAccentText,
		formatTime(rec.UpdatedAt), rec.ID,
	)
	if err != nil {
		return fmt.Errorf("update timer %s: %w", rec.ID, err)
	}
	return nil
}

func (r *Repo) InsertEvent(e subathon.Event) error {
	_, err := r.db.Exec(
		`INSERT INTO events (id, timer_id, platform, type, username, seconds_added, money_added, amount, occurred_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.TimerID, string(e.Platform), string(e.Type), e.Username, e.SecondsAdded, e.MoneyAdded, e.Amount, formatTime(e.Occurred),
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

func (r *Repo) DeleteEvent(timerID, eventID string) error {
	if _, err := r.db.Exec(`DELETE FROM events WHERE timer_id = ? AND id = ?`, timerID, eventID); err != nil {
		return fmt.Errorf("delete event: %w", err)
	}
	return nil
}

func (r *Repo) RecentEvents(timerID string, limit int) ([]subathon.Event, error) {
	rows, err := r.db.Query(
		`SELECT id, timer_id, platform, type, username, seconds_added, money_added, amount, occurred_at
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
	return scanEvents(rows)
}

func (r *Repo) AllEvents(timerID string) ([]subathon.Event, error) {
	rows, err := r.db.Query(
		`SELECT id, timer_id, platform, type, username, seconds_added, money_added, amount, occurred_at
		 FROM events
		 WHERE timer_id = ?
		 ORDER BY occurred_at DESC`,
		timerID,
	)
	if err != nil {
		return nil, fmt.Errorf("query all events for timer %s: %w", timerID, err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]subathon.Event, error) {
	var out []subathon.Event
	for rows.Next() {
		var e subathon.Event
		var platform, typ, occurredAt string
		var amount sql.NullFloat64

		if err := rows.Scan(&e.ID, &e.TimerID, &platform, &typ, &e.Username, &e.SecondsAdded, &e.MoneyAdded, &amount, &occurredAt); err != nil {
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

func (r *Repo) RewardRules(timerID string) (subathon.RewardRules, error) {
	rows, err := r.db.Query(
		`SELECT item, platform, seconds FROM reward_rules WHERE timer_id = ?`, timerID)
	if err != nil {
		return nil, fmt.Errorf("query reward rules: %w", err)
	}
	defer rows.Close()

	rules := subathon.RewardRules{}
	for rows.Next() {
		var item, platform string
		var seconds int
		if err := rows.Scan(&item, &platform, &seconds); err != nil {
			return nil, fmt.Errorf("scan reward rule row: %w", err)
		}
		ri := subathon.RewardItem(item)
		if rules[ri] == nil {
			rules[ri] = map[subathon.Platform]int{}
		}
		rules[ri][subathon.Platform(platform)] = seconds
	}
	return rules, rows.Err()
}

func (r *Repo) SaveRewardRules(timerID string, rules subathon.RewardRules) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM reward_rules WHERE timer_id = ?`, timerID); err != nil {
		return fmt.Errorf("clear reward rules: %w", err)
	}

	stmt, err := tx.Prepare(
		`INSERT INTO reward_rules (timer_id, item, platform, seconds) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for item, row := range rules {
		for platform, secs := range row {
			if _, err := stmt.Exec(timerID, string(item), string(platform), secs); err != nil {
				return fmt.Errorf("insert reward rule: %w", err)
			}
		}
	}

	return tx.Commit()
}

func (r *Repo) MoneyRules(timerID string) (subathon.MoneyRules, error) {
	rows, err := r.db.Query(
		`SELECT item, platform, dollars FROM money_rules WHERE timer_id = ?`, timerID)
	if err != nil {
		return nil, fmt.Errorf("query money rules: %w", err)
	}
	defer rows.Close()

	rules := subathon.MoneyRules{}
	for rows.Next() {
		var item, platform string
		var dollars float64
		if err := rows.Scan(&item, &platform, &dollars); err != nil {
			return nil, fmt.Errorf("scan money rule row: %w", err)
		}
		ri := subathon.RewardItem(item)
		if rules[ri] == nil {
			rules[ri] = map[subathon.Platform]float64{}
		}
		rules[ri][subathon.Platform(platform)] = dollars
	}
	return rules, rows.Err()
}

func (r *Repo) SaveMoneyRules(timerID string, rules subathon.MoneyRules) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM money_rules WHERE timer_id = ?`, timerID); err != nil {
		return fmt.Errorf("clear money rules: %w", err)
	}

	stmt, err := tx.Prepare(
		`INSERT INTO money_rules (timer_id, item, platform, dollars) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for item, row := range rules {
		for platform, dollars := range row {
			if _, err := stmt.Exec(timerID, string(item), string(platform), dollars); err != nil {
				return fmt.Errorf("insert money rule: %w", err)
			}
		}
	}

	return tx.Commit()
}

func (r *Repo) MoneyMilestones(timerID string) ([]subathon.MoneyMilestone, error) {
	rows, err := r.db.Query(
		`SELECT amount, label, hidden FROM money_milestones WHERE timer_id = ? ORDER BY amount ASC`, timerID)
	if err != nil {
		return nil, fmt.Errorf("query money milestones: %w", err)
	}
	defer rows.Close()

	out := []subathon.MoneyMilestone{}
	for rows.Next() {
		var m subathon.MoneyMilestone
		var hidden int
		if err := rows.Scan(&m.Amount, &m.Label, &hidden); err != nil {
			return nil, fmt.Errorf("scan money milestone row: %w", err)
		}
		m.Hidden = hidden != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repo) SaveMoneyMilestones(timerID string, milestones []subathon.MoneyMilestone) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM money_milestones WHERE timer_id = ?`, timerID); err != nil {
		return fmt.Errorf("clear money milestones: %w", err)
	}

	stmt, err := tx.Prepare(
		`INSERT INTO money_milestones (timer_id, amount, label, hidden) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, m := range milestones {
		if _, err := stmt.Exec(timerID, m.Amount, m.Label, boolToInt(m.Hidden)); err != nil {
			return fmt.Errorf("insert money milestone: %w", err)
		}
	}

	return tx.Commit()
}

func (r *Repo) ListModerators(timerID string) ([]string, error) {
	rows, err := r.db.Query(`SELECT user_id FROM timer_moderators WHERE timer_id = ?`, timerID)
	if err != nil {
		return nil, fmt.Errorf("query moderators for timer %s: %w", timerID, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, fmt.Errorf("scan moderator row: %w", err)
		}
		out = append(out, userID)
	}
	return out, rows.Err()
}

func (r *Repo) AddModerator(timerID, userID string) error {
	_, err := r.db.Exec(
		`INSERT OR IGNORE INTO timer_moderators (timer_id, user_id, created_at) VALUES (?, ?, ?)`,
		timerID, userID, formatTime(time.Now()),
	)
	if err != nil {
		return fmt.Errorf("insert moderator: %w", err)
	}
	return nil
}

func (r *Repo) RemoveModerator(timerID, userID string) error {
	if _, err := r.db.Exec(`DELETE FROM timer_moderators WHERE timer_id = ? AND user_id = ?`, timerID, userID); err != nil {
		return fmt.Errorf("delete moderator: %w", err)
	}
	return nil
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
