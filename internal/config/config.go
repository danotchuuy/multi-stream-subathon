// Package config loads server configuration from environment variables.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds the settings needed to start the server.
type Config struct {
	// Addr is the address the HTTP/WS server listens on, e.g. ":8090".
	Addr string

	// AllowedOrigin is the frontend origin allowed to make CORS/WS
	// requests during local development, e.g. "http://localhost:5173".
	AllowedOrigin string

	// InitialDuration is how much time the clock starts with when the
	// subathon is started, before any events add more.
	InitialDuration time.Duration

	// DBPath is where the SQLite database file lives. Point this at a
	// mounted volume in Docker/Kubernetes (e.g. "/data/subathon.db") so
	// timer state and contributor history survive restarts/redeploys.
	DBPath string

	// CookieSecure marks the session cookie Secure (HTTPS-only). Leave
	// false for local/LAN http development; set true once served over
	// TLS, or the browser will silently refuse to store the cookie.
	CookieSecure bool

	Twitch OAuthAppConfig
	Kick   OAuthAppConfig
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
	return Config{
		Addr:            getEnv("ADDR", ":8090"),
		AllowedOrigin:   getEnv("ALLOWED_ORIGIN", "http://localhost:5173"),
		InitialDuration: getEnvDuration("INITIAL_DURATION", time.Hour),
		DBPath:          getEnv("DB_PATH", "data/subathon.db"),
		CookieSecure:    getEnvBool("COOKIE_SECURE", false),
		// Redirect URLs default to routing through the Vite dev server
		// (which proxies /auth to this API - see frontend/vite.config.ts)
		// rather than straight to the API port, so the session cookie the
		// OAuth callback sets ends up scoped to the frontend's origin and
		// "just works" with the SPA's same-origin fetches. Override these
		// (and register the same URL with the provider) for any other
		// deployment, e.g. a real domain or LAN access from other devices.
		Twitch: OAuthAppConfig{
			ClientID:     getEnv("TWITCH_CLIENT_ID", ""),
			ClientSecret: getEnv("TWITCH_CLIENT_SECRET", ""),
			RedirectURL:  getEnv("TWITCH_REDIRECT_URL", "http://localhost:5173/auth/twitch/callback"),
		},
		Kick: OAuthAppConfig{
			ClientID:     getEnv("KICK_CLIENT_ID", ""),
			ClientSecret: getEnv("KICK_CLIENT_SECRET", ""),
			RedirectURL:  getEnv("KICK_REDIRECT_URL", "http://localhost:5173/auth/kick/callback"),
		},
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
