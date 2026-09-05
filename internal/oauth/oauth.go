// Package oauth implements a minimal OAuth 2.0 authorization-code client
// (with optional PKCE) used to let users sign up/log in and link
// additional streaming platform accounts. It deliberately avoids a
// generic third-party OAuth library so PKCE and per-platform user-info
// parsing stay simple to read and adjust in one place.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Provider describes one OAuth2 identity provider (Twitch, Kick, ...).
type Provider struct {
	Name         string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	Scopes       []string
	UsePKCE      bool

	// ExtraUserInfoHeaders are added to the user-info request, e.g.
	// Twitch's Helix API requires a Client-Id header alongside the bearer
	// token.
	ExtraUserInfoHeaders map[string]string

	// ExtraAuthParams are added to the authorize URL as-is, e.g. Google
	// requires access_type=offline plus prompt=consent for
	// oauth2.googleapis.com to ever issue a refresh token (see
	// NewYouTube) — without them it only does on an account's very first
	// consent, so a later reauth (e.g. after this app loses its stored
	// tokens) would silently come back with no way to refresh.
	ExtraAuthParams map[string]string

	// ParseUser extracts the platform's stable user ID and username from
	// the user-info response body.
	ParseUser func(body []byte) (platformUserID, username string, err error)
}

// AuthorizeURL builds the URL to send the user's browser to. codeChallenge
// is ignored unless UsePKCE is set.
func (p *Provider) AuthorizeURL(state, codeChallenge string) string {
	q := url.Values{
		"client_id":     {p.ClientID},
		"redirect_uri":  {p.RedirectURL},
		"response_type": {"code"},
		"state":         {state},
	}
	if len(p.Scopes) > 0 {
		q.Set("scope", strings.Join(p.Scopes, " "))
	}
	if p.UsePKCE {
		q.Set("code_challenge", codeChallenge)
		q.Set("code_challenge_method", "S256")
	}
	for k, v := range p.ExtraAuthParams {
		q.Set(k, v)
	}
	return p.AuthURL + "?" + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// Exchange trades an authorization code for tokens. codeVerifier is
// ignored unless UsePKCE is set.
func (p *Provider) Exchange(ctx context.Context, code, codeVerifier string) (accessToken, refreshToken string, expiresAt time.Time, err error) {
	form := url.Values{
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {p.RedirectURL},
	}
	if p.UsePKCE {
		form.Set("code_verifier", codeVerifier)
	}
	return p.doTokenRequest(ctx, form)
}

// Refresh trades a still-valid refresh token for a new access token
// (and, depending on the provider, a new refresh token — some rotate it
// on every use, so callers must persist whatever comes back rather than
// assuming the original refresh token stays valid), without a fresh
// authorization-code flow. Used to keep a long-lived background
// integration (e.g. StreamElements' tip poller — see
// streamelements.Poller) working past its access token's expiry.
func (p *Provider) Refresh(ctx context.Context, refreshToken string) (accessToken, newRefreshToken string, expiresAt time.Time, err error) {
	form := url.Values{
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	return p.doTokenRequest(ctx, form)
}

// doTokenRequest POSTs form to p.TokenURL and parses the resulting
// token response — the mechanics shared by Exchange (grant_type=
// authorization_code) and Refresh (grant_type=refresh_token).
func (p *Provider) doTokenRequest(ctx context.Context, form url.Values) (accessToken, refreshToken string, expiresAt time.Time, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("%s: build token request: %w", p.Name, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("%s: token request: %w", p.Name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("%s: read token response: %w", p.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", time.Time{}, fmt.Errorf("%s: token request failed (%d): %s", p.Name, resp.StatusCode, body)
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", "", time.Time{}, fmt.Errorf("%s: parse token response: %w", p.Name, err)
	}

	if tok.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	return tok.AccessToken, tok.RefreshToken, expiresAt, nil
}

// FetchUser fetches and parses the authenticated user's profile.
func (p *Provider) FetchUser(ctx context.Context, accessToken string) (platformUserID, username string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.UserInfoURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("%s: build user info request: %w", p.Name, err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	for k, v := range p.ExtraUserInfoHeaders {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("%s: user info request: %w", p.Name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("%s: read user info response: %w", p.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("%s: user info request failed (%d): %s", p.Name, resp.StatusCode, body)
	}

	return p.ParseUser(body)
}

// NewState returns a random, URL-safe token suitable for both the OAuth
// "state" CSRF parameter and, reused, as a PKCE code_verifier.
func NewState() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// PKCEChallenge derives the S256 code_challenge for a code_verifier.
func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// FlowState is what's remembered server-side between redirecting the user
// to a provider and handling its callback.
type FlowState struct {
	Provider string
	// Purpose is "login" (sign up or log in as whoever this identity
	// belongs to), "link" (attach this identity to the already
	// logged-in UserID), or "streamelements" (connect TimerID's tip
	// polling to whichever StreamElements account authorizes — see
	// internal/server/streamelements_oauth.go).
	Purpose string
	UserID  string
	// TimerID is set only for Purpose == "streamelements": which timer
	// this connection is for. Unlike "login"/"link", this flow isn't
	// about the app's own account system, so it has no UserID of its
	// own — the timer's owner-or-moderator check already happened when
	// the flow started (see handleStreamElementsOAuthStart).
	TimerID      string
	CodeVerifier string
	ExpiresAt    time.Time
}

// StateStore holds in-flight OAuth flow state, keyed by the random "state"
// value. It's deliberately in-memory, not persisted: entries live for
// minutes, not across restarts, and losing one just means the user retries
// the login.
type StateStore struct {
	mu      sync.Mutex
	entries map[string]FlowState
}

// NewStateStore creates an empty StateStore.
func NewStateStore() *StateStore {
	return &StateStore{entries: make(map[string]FlowState)}
}

// Put remembers fs under state.
func (s *StateStore) Put(state string, fs FlowState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[state] = fs
}

// Take retrieves and removes the entry for state (single use), returning
// false if it doesn't exist or has expired.
func (s *StateStore) Take(state string) (FlowState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fs, ok := s.entries[state]
	delete(s.entries, state)
	if !ok || time.Now().After(fs.ExpiresAt) {
		return FlowState{}, false
	}
	return fs, true
}

// Sweep removes expired entries. Call periodically to bound memory use
// from abandoned flows.
func (s *StateStore) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for k, fs := range s.entries {
		if now.After(fs.ExpiresAt) {
			delete(s.entries, k)
		}
	}
}
