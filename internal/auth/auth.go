// Package auth holds the domain state for user accounts: a User can sign
// up or log in via a platform identity (Twitch or Kick today), then link
// additional platform identities to the same account, each carrying the
// OAuth tokens needed to later pull events from that platform.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Platform identifies which streaming service an identity belongs to.
// Kept separate from subathon.Platform (which also has "manual") since
// account identities and contributor events are different concerns.
type Platform string

const (
	PlatformTwitch  Platform = "twitch"
	PlatformKick    Platform = "kick"
	PlatformYouTube Platform = "youtube"
)

// User is an account, created the first time someone logs in via any
// supported platform.
type User struct {
	ID          string
	DisplayName string
	CreatedAt   time.Time
}

// Identity links a User to one platform account, with the OAuth tokens
// needed to act on their behalf there later (e.g. subscribing to events).
type Identity struct {
	ID               string
	UserID           string
	Platform         Platform
	PlatformUserID   string
	PlatformUsername string
	AccessToken      string
	RefreshToken     string
	TokenExpiresAt   time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Session is a logged-in browser session. Only TokenHash is persisted;
// the raw token lives solely in the client's cookie.
type Session struct {
	TokenHash string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Repo persists users, their linked identities, and sessions.
// Implementations must be safe for concurrent use.
type Repo interface {
	CreateUser(u User) error
	GetUser(id string) (User, bool, error)

	FindIdentity(platform Platform, platformUserID string) (Identity, bool, error)
	CreateIdentity(i Identity) error
	UpdateIdentityTokens(identityID, accessToken, refreshToken string, expiresAt time.Time) error
	ListIdentities(userID string) ([]Identity, error)

	CreateSession(s Session) error
	GetSession(tokenHash string) (Session, bool, error)
	DeleteSession(tokenHash string) error
	DeleteExpiredSessions() error
}

// ErrIdentityLinkedElsewhere is returned by LinkIdentity when the platform
// account is already linked to a different user.
var ErrIdentityLinkedElsewhere = errors.New("this platform account is already linked to a different user")

const sessionTTL = 30 * 24 * time.Hour

// Service implements the account lifecycle on top of a Repo.
type Service struct {
	repo Repo
}

// NewService wraps repo with the account/session business logic.
func NewService(repo Repo) *Service {
	return &Service{repo: repo}
}

// LoginOrSignup finds the user for an existing (platform, platformUserID)
// identity, refreshing its tokens; if no such identity exists yet, it
// creates a brand-new account (named after the platform username) with
// this as its first identity. Either way, it returns the resulting user.
func (s *Service) LoginOrSignup(platform Platform, platformUserID, platformUsername, accessToken, refreshToken string, expiresAt time.Time) (User, error) {
	ident, ok, err := s.repo.FindIdentity(platform, platformUserID)
	if err != nil {
		return User{}, fmt.Errorf("find identity: %w", err)
	}

	if ok {
		if err := s.repo.UpdateIdentityTokens(ident.ID, accessToken, refreshToken, expiresAt); err != nil {
			return User{}, fmt.Errorf("update identity tokens: %w", err)
		}
		user, ok, err := s.repo.GetUser(ident.UserID)
		if err != nil {
			return User{}, fmt.Errorf("get user: %w", err)
		}
		if !ok {
			return User{}, fmt.Errorf("identity %s references missing user %s", ident.ID, ident.UserID)
		}
		return user, nil
	}

	now := time.Now()
	user := User{ID: uuid.NewString(), DisplayName: platformUsername, CreatedAt: now}
	if err := s.repo.CreateUser(user); err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}

	if err := s.repo.CreateIdentity(Identity{
		ID:               uuid.NewString(),
		UserID:           user.ID,
		Platform:         platform,
		PlatformUserID:   platformUserID,
		PlatformUsername: platformUsername,
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		TokenExpiresAt:   expiresAt,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		return User{}, fmt.Errorf("create identity: %w", err)
	}

	return user, nil
}

// LinkIdentity attaches an additional platform identity to an already
// logged-in user. If that platform account is already linked to a
// *different* user, it returns ErrIdentityLinkedElsewhere. If it's already
// linked to this same user, it just refreshes the tokens.
func (s *Service) LinkIdentity(userID string, platform Platform, platformUserID, platformUsername, accessToken, refreshToken string, expiresAt time.Time) error {
	existing, ok, err := s.repo.FindIdentity(platform, platformUserID)
	if err != nil {
		return fmt.Errorf("find identity: %w", err)
	}
	if ok {
		if existing.UserID != userID {
			return ErrIdentityLinkedElsewhere
		}
		return s.repo.UpdateIdentityTokens(existing.ID, accessToken, refreshToken, expiresAt)
	}

	now := time.Now()
	return s.repo.CreateIdentity(Identity{
		ID:               uuid.NewString(),
		UserID:           userID,
		Platform:         platform,
		PlatformUserID:   platformUserID,
		PlatformUsername: platformUsername,
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		TokenExpiresAt:   expiresAt,
		CreatedAt:        now,
		UpdatedAt:        now,
	})
}

// Identities lists every platform linked to userID.
func (s *Service) Identities(userID string) ([]Identity, error) {
	return s.repo.ListIdentities(userID)
}

// GetUser looks up a user by ID.
func (s *Service) GetUser(id string) (User, bool, error) {
	return s.repo.GetUser(id)
}

// CreateSession issues a new session for userID and returns the raw token
// to hand back as a cookie. Only its hash is ever persisted.
func (s *Service) CreateSession(userID string) (string, error) {
	raw := randomToken()
	now := time.Now()
	err := s.repo.CreateSession(Session{
		TokenHash: hashToken(raw),
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(sessionTTL),
	})
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return raw, nil
}

// Authenticate resolves a raw session token (as sent in a cookie) to its
// user. ok is false for a missing, expired, or unknown token — never an
// error on its own.
func (s *Service) Authenticate(rawToken string) (user User, ok bool, err error) {
	if rawToken == "" {
		return User{}, false, nil
	}

	sess, ok, err := s.repo.GetSession(hashToken(rawToken))
	if err != nil {
		return User{}, false, fmt.Errorf("get session: %w", err)
	}
	if !ok {
		return User{}, false, nil
	}
	if time.Now().After(sess.ExpiresAt) {
		_ = s.repo.DeleteSession(sess.TokenHash)
		return User{}, false, nil
	}

	return s.repo.GetUser(sess.UserID)
}

// Logout invalidates a raw session token.
func (s *Service) Logout(rawToken string) error {
	if rawToken == "" {
		return nil
	}
	return s.repo.DeleteSession(hashToken(rawToken))
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
