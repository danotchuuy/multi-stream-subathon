// Package config loads server configuration from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the settings needed to start the server.
type Config struct {
	// Addr is the address the HTTP/WS server listens on, e.g. ":8090".
	Addr string

	// PublicBaseURL is the externally reachable origin this deployment is
	// served under, e.g. "https://subathon.example.com" in production.
	// AllowedOrigin and the Twitch/Kick redirect URLs all default to it,
	// so a real deployment behind one HTTPS domain only needs this one
	// var set — individual PUBLIC_BASE_URL-derived defaults can still be
	// overridden below for setups that split origins.
	PublicBaseURL string

	// AllowedOrigin is the frontend origin allowed to make CORS/WS
	// requests, e.g. "http://localhost:5173" locally or the production
	// domain once deployed.
	AllowedOrigin string

	// InitialDuration is how much time the clock starts with when the
	// subathon is started, before any events add more.
	InitialDuration time.Duration

	// DBPath is where the SQLite database file lives. Point this at a
	// mounted volume in Docker/Kubernetes (e.g. "/data/subathon.db") so
	// timer state and contributor history survive restarts/redeploys.
	DBPath string

	// UIDistDir is the path to the built frontend assets (frontend/dist
	// after `npm run build`). Empty (the default outside Docker) skips
	// static file serving, since local development instead runs the Vite
	// dev server and its API proxy — see internal/server.New.
	UIDistDir string

	// CookieSecure marks the session cookie Secure (HTTPS-only). Leave
	// false for local/LAN http development; set true once served over
	// TLS, or the browser will silently refuse to store the cookie.
	CookieSecure bool

	Twitch         OAuthAppConfig
	Kick           OAuthAppConfig
	YouTube        OAuthAppConfig
	StreamElements OAuthAppConfig

	// TwitchWebhookSecret signs/verifies Twitch EventSub webhook
	// notifications. Empty disables live Twitch event listening (reward
	// rules can still be configured and used for manual events) — Twitch
	// requires this to be 10-100 characters.
	TwitchWebhookSecret string

	// TwitchWebhookCallbackURL is the public https:// URL Twitch posts
	// EventSub notifications to. Derived from PublicBaseURL.
	TwitchWebhookCallbackURL string

	// ThroneWebhookPublicKey overrides Throne's built-in published Ed25519
	// webhook-signing public key (see internal/platform/throne). Leave
	// unset — Throne needs no server-level app credential at all, unlike
	// every other integration above, so this only matters if Throne
	// rotates its key before this app is updated to match.
	ThroneWebhookPublicKey string
}

// OAuthAppConfig holds one platform's registered OAuth app credentials.
// ClientID empty means that provider is disabled (its login/link buttons
// won't work, but the rest of the app still runs).
type OAuthAppConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Load reads config from the environment, falling back to development
// defaults for anything unset.
func Load() Config {
	// Defaults to routing through the Vite dev server (which proxies
	// /auth to this API - see frontend/vite.config.ts) rather than
	// straight to the API port, so the session cookie the OAuth callback
	// sets ends up scoped to the frontend's origin and "just works" with
	// the SPA's same-origin fetches. For a real deployment, set
	// PUBLIC_BASE_URL to the single https:// domain the app is served
	// under (and register <that domain>/auth/<provider>/callback with
	// each OAuth app) - AllowedOrigin, CookieSecure, and both providers'
	// redirect URLs all derive from it.
	publicBaseURL := strings.TrimRight(getEnv("PUBLIC_BASE_URL", "http://localhost:5173"), "/")
	defaultCookieSecure := strings.HasPrefix(publicBaseURL, "https://")

	return Config{
		Addr:            getEnv("ADDR", ":8090"),
		PublicBaseURL:   publicBaseURL,
		AllowedOrigin:   getEnv("ALLOWED_ORIGIN", publicBaseURL),
		InitialDuration: getEnvDuration("INITIAL_DURATION", time.Hour),
		DBPath:          getEnv("DB_PATH", "data/subathon.db"),
		UIDistDir:       getEnv("UI_DIST_DIR", ""),
		CookieSecure:    getEnvBool("COOKIE_SECURE", defaultCookieSecure),
		Twitch: OAuthAppConfig{
			ClientID:     getEnv("TWITCH_CLIENT_ID", ""),
			ClientSecret: getEnv("TWITCH_CLIENT_SECRET", ""),
			RedirectURL:  getEnv("TWITCH_REDIRECT_URL", publicBaseURL+"/auth/twitch/callback"),
		},
		Kick: OAuthAppConfig{
			ClientID:     getEnv("KICK_CLIENT_ID", ""),
			ClientSecret: getEnv("KICK_CLIENT_SECRET", ""),
			RedirectURL:  getEnv("KICK_REDIRECT_URL", publicBaseURL+"/auth/kick/callback"),
		},
		YouTube: OAuthAppConfig{
			ClientID:     getEnv("YOUTUBE_CLIENT_ID", ""),
			ClientSecret: getEnv("YOUTUBE_CLIENT_SECRET", ""),
			RedirectURL:  getEnv("YOUTUBE_REDIRECT_URL", publicBaseURL+"/auth/youtube/callback"),
		},
		// Unlike Twitch/Kick (whose {provider}/start route also signs
		// into this app), StreamElements' redirect is only ever reached
		// via its own dedicated per-timer connect flow — see
		// streamelements_oauth.go — but registers with StreamElements the
		// same way, as a fixed callback URL.
		StreamElements: OAuthAppConfig{
			ClientID:     getEnv("STREAMELEMENTS_CLIENT_ID", ""),
			ClientSecret: getEnv("STREAMELEMENTS_CLIENT_SECRET", ""),
			RedirectURL:  getEnv("STREAMELEMENTS_REDIRECT_URL", publicBaseURL+"/auth/streamelements/callback"),
		},
		TwitchWebhookSecret:      getEnv("TWITCH_WEBHOOK_SECRET", ""),
		TwitchWebhookCallbackURL: publicBaseURL + "/webhooks/twitch",
		ThroneWebhookPublicKey:   getEnv("THRONE_WEBHOOK_PUBLIC_KEY", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	return fallback
}
