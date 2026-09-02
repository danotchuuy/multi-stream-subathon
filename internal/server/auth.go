package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/danotchuuy/multi-stream-subathon/internal/auth"
	"github.com/danotchuuy/multi-stream-subathon/internal/oauth"
)

const (
	sessionCookieName = "session"
	sessionMaxAge     = 30 * 24 * time.Hour
	flowStateTTL      = 10 * time.Minute
)

// mountAuthRoutes wires up OAuth login/link, logout, and /api/me.
func mountAuthRoutes(r chi.Router, deps Deps) {
	r.Get("/auth/{provider}/start", handleOAuthStart(deps))
	r.Get("/auth/{provider}/callback", handleOAuthCallback(deps))
	r.Post("/auth/logout", handleLogout(deps))
	r.Get("/api/auth/providers", handleAuthProviders(deps))

	r.Group(func(r chi.Router) {
		r.Use(requireAuth(deps.Auth))
		r.Get("/api/me", handleMe(deps.Auth))
	})
}

// handleAuthProviders lists which OAuth providers are actually configured
// (have a client ID), so the frontend can hide/disable login or link
// buttons for ones that aren't set up rather than linking to a 404.
func handleAuthProviders(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		names := make([]string, 0, len(deps.OAuthProviders))
		for name := range deps.OAuthProviders {
			names = append(names, name)
		}
		writeJSON(w, http.StatusOK, names)
	}
}

// handleOAuthStart begins a login (mode=login, the default) or, for an
// already logged-in user, a link (mode=link) flow: it redirects the
// browser to the provider's authorize page.
func handleOAuthStart(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerName := chi.URLParam(r, "provider")
		p, ok := deps.OAuthProviders[providerName]
		if !ok {
			http.Error(w, "unknown or unconfigured provider", http.StatusNotFound)
			return
		}

		mode := r.URL.Query().Get("mode")
		if mode != "link" {
			mode = "login"
		}

		var userID string
		if mode == "link" {
			user, ok := userFromRequest(deps.Auth, r)
			if !ok {
				http.Error(w, "must be logged in to link an account", http.StatusUnauthorized)
				return
			}
			userID = user.ID
		}

		state := oauth.NewState()
		var codeVerifier, codeChallenge string
		if p.UsePKCE {
			codeVerifier = oauth.NewState()
			codeChallenge = oauth.PKCEChallenge(codeVerifier)
		}

		deps.OAuthStates.Put(state, oauth.FlowState{
			Provider:     providerName,
			Purpose:      mode,
			UserID:       userID,
			CodeVerifier: codeVerifier,
			ExpiresAt:    time.Now().Add(flowStateTTL),
		})

		http.Redirect(w, r, p.AuthorizeURL(state, codeChallenge), http.StatusFound)
	}
}

// handleOAuthCallback exchanges the authorization code, fetches the
// platform's user info, and either logs the user in (creating an account
// on first login) or links the identity to the already-logged-in user,
// then redirects back to the frontend.
func handleOAuthCallback(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerName := chi.URLParam(r, "provider")
		p, ok := deps.OAuthProviders[providerName]
		if !ok {
			http.Error(w, "unknown or unconfigured provider", http.StatusNotFound)
			return
		}

		if msg := r.URL.Query().Get("error"); msg != "" {
			redirectWithError(w, r, deps.AllowedOrigin+"/login", msg)
			return
		}

		state := r.URL.Query().Get("state")
		fs, ok := deps.OAuthStates.Take(state)
		if !ok || fs.Provider != providerName {
			http.Error(w, "invalid or expired oauth state", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		accessToken, refreshToken, expiresAt, err := p.Exchange(r.Context(), code, fs.CodeVerifier)
		if err != nil {
			log.Printf("oauth: %s token exchange: %v", providerName, err)
			redirectWithError(w, r, deps.AllowedOrigin+"/login", "oauth_failed")
			return
		}

		platformUserID, platformUsername, err := p.FetchUser(r.Context(), accessToken)
		if err != nil {
			log.Printf("oauth: %s fetch user: %v", providerName, err)
			redirectWithError(w, r, deps.AllowedOrigin+"/login", "oauth_failed")
			return
		}

		platform := auth.Platform(providerName)

		if fs.Purpose == "link" {
			err := deps.Auth.LinkIdentity(fs.UserID, platform, platformUserID, platformUsername, accessToken, refreshToken, expiresAt)
			if err != nil {
				log.Printf("oauth: link %s: %v", providerName, err)
				msg := "link_failed"
				if errors.Is(err, auth.ErrIdentityLinkedElsewhere) {
					msg = "already_linked"
				}
				redirectWithError(w, r, deps.AllowedOrigin+"/account", msg)
				return
			}
			http.Redirect(w, r, deps.AllowedOrigin+"/account", http.StatusFound)
			return
		}

		user, err := deps.Auth.LoginOrSignup(platform, platformUserID, platformUsername, accessToken, refreshToken, expiresAt)
		if err != nil {
			log.Printf("oauth: login/signup %s: %v", providerName, err)
			redirectWithError(w, r, deps.AllowedOrigin+"/login", "oauth_failed")
			return
		}

		raw, err := deps.Auth.CreateSession(user.ID)
		if err != nil {
			log.Printf("oauth: create session: %v", err)
			redirectWithError(w, r, deps.AllowedOrigin+"/login", "oauth_failed")
			return
		}

		setSessionCookie(w, raw, deps.CookieSecure)
		http.Redirect(w, r, deps.AllowedOrigin+"/", http.StatusFound)
	}
}

func redirectWithError(w http.ResponseWriter, r *http.Request, base, msg string) {
	http.Redirect(w, r, base+"?error="+url.QueryEscape(msg), http.StatusFound)
}

func handleLogout(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookieName); err == nil {
			if err := deps.Auth.Logout(c.Value); err != nil {
				log.Printf("server: logout: %v", err)
			}
		}
		clearSessionCookie(w, deps.CookieSecure)
		w.WriteHeader(http.StatusNoContent)
	}
}

type identityView struct {
	Platform string `json:"platform"`
	Username string `json:"username"`
}

type meResponse struct {
	ID          string         `json:"id"`
	DisplayName string         `json:"displayName"`
	Identities  []identityView `json:"identities"`
}

func handleMe(svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r)

		identities, err := svc.Identities(u.ID)
		if err != nil {
			log.Printf("server: list identities for user %s: %v", u.ID, err)
			http.Error(w, "failed to load linked accounts", http.StatusInternalServerError)
			return
		}

		views := make([]identityView, len(identities))
		for i, ident := range identities {
			views[i] = identityView{Platform: string(ident.Platform), Username: ident.PlatformUsername}
		}

		writeJSON(w, http.StatusOK, meResponse{ID: u.ID, DisplayName: u.DisplayName, Identities: views})
	}
}

// --- session cookie + auth context plumbing ---

type userCtxKey struct{}

// requireAuth 401s unless a valid session cookie is present, stashing the
// authenticated *auth.User on the request context.
func requireAuth(svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := userFromRequest(svc, r)
			if !ok {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), userCtxKey{}, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requireOwner 403s unless the authenticated user (from requireAuth) owns
// the timer resolved by timerContext. Mount both, requireAuth first.
func requireOwner() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t := timerFromContext(r)
			u := userFromContext(r)
			if t.UserID() == "" || t.UserID() != u.ID {
				http.Error(w, "you do not own this timer", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func userFromRequest(svc *auth.Service, r *http.Request) (auth.User, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return auth.User{}, false
	}
	user, ok, err := svc.Authenticate(c.Value)
	if err != nil {
		log.Printf("server: authenticate session: %v", err)
		return auth.User{}, false
	}
	return user, ok
}

func userFromContext(r *http.Request) auth.User {
	u, _ := r.Context().Value(userCtxKey{}).(auth.User)
	return u
}

func setSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionMaxAge.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
