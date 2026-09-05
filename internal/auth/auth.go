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
	FindIdentityByUsername(platform Platform, username string) (Identity, bool, error)
	CreateIdentity(i Identity) error
	UpdateIdentityTokens(identityID, accessToken, refreshToken string, expiresAt time.Time) error
	ListIdentities(userID string) ([]Identity, error)
	DeleteIdentity(identityID string) error

	CreateSession(s Session) error
	GetSession(tokenHash string) (Session, bool, error)
	DeleteSession(tokenHash string) error
	DeleteExpiredSessions() error
}

// ErrIdentityLinkedElsewhere is returned by LinkIdentity when the platform
// account is already linked to a different user.
var ErrIdentityLinkedElsewhere = errors.New("this platform account is already linked to a different user")

// ErrIdentityNotLinked is returned by UnlinkIdentity when the account has
// no identity on the given platform to unlink.
var ErrIdentityNotLinked = errors.New("that platform is not linked to this account")

// ErrLastIdentity is returned by UnlinkIdentity when it's asked to remove
// an account's only linked identity — since there's no other way to log
// back in (no password auth), that would lock the account out entirely.
var ErrLastIdentity = errors.New("cannot unlink your only linked platform account")

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

// IdentityByPlatformUser looks up the identity for a specific platform
// account — not by our own userID, by the platform's own ID for it. ok is
// false if no account has linked that platform identity. Unlike
// Identities (used to show/manage the *signed-in* user's own linked
// accounts), this is for platform integrations that need to act using
// whichever account happens to own a given platform identity's own
// delegated OAuth tokens — e.g. youtube.Poller reading a timer's watched
// YouTube channel's live chat with that channel owner's own linked
// tokens, since YouTube (unlike Twitch/Kick) has no app-level credential
// that works for an arbitrary channel.
func (s *Service) IdentityByPlatformUser(platform Platform, platformUserID string) (Identity, bool, error) {
	return s.repo.FindIdentity(platform, platformUserID)
}

// RefreshIdentityTokens persists a refreshed access/refresh token pair
// for identityID, e.g. after youtube.Poller calls oauth.Provider.Refresh
// to keep polling past an access token's expiry — exported for the same
// reason as IdentityByPlatformUser.
func (s *Service) RefreshIdentityTokens(identityID, accessToken, refreshToken string, expiresAt time.Time) error {
	return s.repo.UpdateIdentityTokens(identityID, accessToken, refreshToken, expiresAt)
}

// UnlinkIdentity removes userID's identity on platform, e.g. from an
// "unlink Twitch" button on the account page. Refuses with
// ErrLastIdentity if it's their only linked identity — there's no
// password auth to fall back on, so that would lock them out — and with
// ErrIdentityNotLinked if that platform isn't linked to begin with.
func (s *Service) UnlinkIdentity(userID string, platform Platform) error {
	identities, err := s.repo.ListIdentities(userID)
	if err != nil {
		return fmt.Errorf("list identities: %w", err)
	}

	var target *Identity
	for i := range identities {
		if identities[i].Platform == platform {
			target = &identities[i]
			break
		}
	}
	if target == nil {
		return ErrIdentityNotLinked
	}
	if len(identities) <= 1 {
		return ErrLastIdentity
	}

	if err := s.repo.DeleteIdentity(target.ID); err != nil {
		return fmt.Errorf("delete identity: %w", err)
	}
	return nil
}

// GetUser looks up a user by ID.
func (s *Service) GetUser(id string) (User, bool, error) {
	return s.repo.GetUser(id)
}

// twitchAndKick is the platforms UserByUsername searches — YouTube has no
// login provider yet (see internal/oauth), so no identity can ever exist
// for it.
var twitchAndKick = []Platform{PlatformTwitch, PlatformKick}

// UserByUsername finds the account with username linked on Twitch or Kick
// (case-insensitive), e.g. resolving what a timer owner types into "add
// moderator by username." ok is false if no linked identity matches —
// never an error on its own.
func (s *Service) UserByUsername(username string) (user User, ok bool, err error) {
	for _, platform := range twitchAndKick {
		ident, found, err := s.repo.FindIdentityByUsername(platform, username)
		if err != nil {
			return User{}, false, fmt.Errorf("find identity on %s: %w", platform, err)
		}
		if found {
			return s.repo.GetUser(ident.UserID)
		}
	}
	return User{}, false, nil
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
