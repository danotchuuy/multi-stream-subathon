package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/danotchuuy/multi-stream-subathon/internal/auth"
)

func (r *Repo) CreateUser(u auth.User) error {
	_, err := r.db.Exec(
		`INSERT INTO users (id, display_name, created_at) VALUES (?, ?, ?)`,
		u.ID, u.DisplayName, formatTime(u.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *Repo) GetUser(id string) (auth.User, bool, error) {
	var u auth.User
	var createdAt string
	err := r.db.QueryRow(`SELECT id, display_name, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.DisplayName, &createdAt)
	if err == sql.ErrNoRows {
		return auth.User{}, false, nil
	}
	if err != nil {
		return auth.User{}, false, fmt.Errorf("query user: %w", err)
	}
	u.CreatedAt = parseTime(createdAt)
	return u, true, nil
}

func (r *Repo) FindIdentity(platform auth.Platform, platformUserID string) (auth.Identity, bool, error) {
	row := r.db.QueryRow(`
		SELECT id, user_id, platform, platform_user_id, platform_username, access_token, refresh_token, token_expires_at, created_at, updated_at
		FROM identities
		WHERE platform = ? AND platform_user_id = ?`,
		string(platform), platformUserID)

	ident, err := scanIdentity(row)
	if err == sql.ErrNoRows {
		return auth.Identity{}, false, nil
	}
	if err != nil {
		return auth.Identity{}, false, fmt.Errorf("query identity: %w", err)
	}
	return ident, true, nil
}

func (r *Repo) CreateIdentity(i auth.Identity) error {
	_, err := r.db.Exec(
		`INSERT INTO identities (id, user_id, platform, platform_user_id, platform_username, access_token, refresh_token, token_expires_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		i.ID, i.UserID, string(i.Platform), i.PlatformUserID, i.PlatformUsername,
		i.AccessToken, nullableString(i.RefreshToken), nullableTime(i.TokenExpiresAt),
		formatTime(i.CreatedAt), formatTime(i.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert identity: %w", err)
	}
	return nil
}

func (r *Repo) UpdateIdentityTokens(identityID, accessToken, refreshToken string, expiresAt time.Time) error {
	_, err := r.db.Exec(
		`UPDATE identities SET access_token = ?, refresh_token = ?, token_expires_at = ?, updated_at = ? WHERE id = ?`,
		accessToken, nullableString(refreshToken), nullableTime(expiresAt), formatTime(time.Now()), identityID,
	)
	if err != nil {
		return fmt.Errorf("update identity %s tokens: %w", identityID, err)
	}
	return nil
}

func (r *Repo) ListIdentities(userID string) ([]auth.Identity, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, platform, platform_user_id, platform_username, access_token, refresh_token, token_expires_at, created_at, updated_at
		FROM identities
		WHERE user_id = ?
		ORDER BY created_at`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("query identities for user %s: %w", userID, err)
	}
	defer rows.Close()

	var out []auth.Identity
	for rows.Next() {
		ident, err := scanIdentity(rows)
		if err != nil {
			return nil, fmt.Errorf("scan identity row: %w", err)
		}
		out = append(out, ident)
	}
	return out, rows.Err()
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanIdentity(row rowScanner) (auth.Identity, error) {
	var i auth.Identity
	var platform string
	var refreshToken, tokenExpiresAt sql.NullString
	var createdAt, updatedAt string

	err := row.Scan(&i.ID, &i.UserID, &platform, &i.PlatformUserID, &i.PlatformUsername,
		&i.AccessToken, &refreshToken, &tokenExpiresAt, &createdAt, &updatedAt)
	if err != nil {
		return auth.Identity{}, err
	}

	i.Platform = auth.Platform(platform)
	i.RefreshToken = refreshToken.String
	i.TokenExpiresAt = parseTime(tokenExpiresAt.String)
	i.CreatedAt = parseTime(createdAt)
	i.UpdatedAt = parseTime(updatedAt)
	return i, nil
}

func (r *Repo) CreateSession(s auth.Session) error {
	_, err := r.db.Exec(
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		s.TokenHash, s.UserID, formatTime(s.CreatedAt), formatTime(s.ExpiresAt),
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *Repo) GetSession(tokenHash string) (auth.Session, bool, error) {
	var s auth.Session
	var createdAt, expiresAt string
	err := r.db.QueryRow(`SELECT token_hash, user_id, created_at, expires_at FROM sessions WHERE token_hash = ?`, tokenHash).
		Scan(&s.TokenHash, &s.UserID, &createdAt, &expiresAt)
	if err == sql.ErrNoRows {
		return auth.Session{}, false, nil
	}
	if err != nil {
		return auth.Session{}, false, fmt.Errorf("query session: %w", err)
	}
	s.CreatedAt = parseTime(createdAt)
	s.ExpiresAt = parseTime(expiresAt)
	return s, true, nil
}

func (r *Repo) DeleteSession(tokenHash string) error {
	if _, err := r.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *Repo) DeleteExpiredSessions() error {
	if _, err := r.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, formatTime(time.Now())); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}
