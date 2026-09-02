// Package server wires up the HTTP API and WebSocket endpoint for the
// subathon tracker. It supports multiple independent timers, each
// addressed by an ID: /api/timers/{timerId}/... and /ws/{timerId}.
package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/danotchuuy/multi-stream-subathon/internal/auth"
	"github.com/danotchuuy/multi-stream-subathon/internal/oauth"
	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
	"github.com/danotchuuy/multi-stream-subathon/internal/ws"
)

// Deps bundles everything the router needs to construct handlers.
type Deps struct {
	Manager *subathon.Manager
	Hub     *ws.Hub

	Auth           *auth.Service
	OAuthProviders map[string]*oauth.Provider // keyed by provider name, e.g. "twitch"
	OAuthStates    *oauth.StateStore

	// AllowedOrigin is the frontend's origin, used both for CORS and as
	// the base URL to redirect back to after an OAuth flow completes.
	AllowedOrigin string
	CookieSecure  bool
}

// New builds the router: auth (OAuth login/link, session, /api/me), REST
// endpoints for listing/creating/controlling timers, and the per-timer
// /ws endpoint served by hub.
func New(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{deps.AllowedOrigin},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
	}))

	r.Get("/healthz", handleHealth)

	mountAuthRoutes(r, deps)

	r.Route("/api/timers", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireAuth(deps.Auth))
			r.Get("/", handleListTimers(deps.Manager))
			r.Post("/", handleCreateTimer(deps.Manager))
		})

		r.Route("/{timerID}", func(r chi.Router) {
			r.Use(timerContext(deps.Manager))

			// Viewing a timer's state is public: the ID itself is the
			// token an overlay/dashboard viewer needs, no login required.
			r.Get("/state", handleGetState())

			r.Group(func(r chi.Router) {
				r.Use(requireAuth(deps.Auth))
				r.Use(requireOwner())
				r.Post("/control/reset", handleReset(deps.Hub))
				r.Post("/control/resume", handleResume(deps.Hub))
				r.Post("/control/stop", handleStop(deps.Hub))
				r.Post("/events", handleAddEvent(deps.Hub))
			})
		})
	})

	// Public: the timer ID is the overlay's token, no login required.
	r.Get("/ws/{timerID}", handleWS(deps.Manager, deps.Hub))

	return r
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func handleListTimers(manager *subathon.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r)
		writeJSON(w, http.StatusOK, manager.List(u.ID))
	}
}

type createTimerRequest struct {
	Name string `json:"name"`
}

func handleCreateTimer(manager *subathon.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r)

		var req createTimerRequest
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}

		t, err := manager.Create(u.ID, req.Name)
		if err != nil {
			log.Printf("server: create timer: %v", err)
			http.Error(w, "failed to create timer", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusCreated, t.Snapshot())
	}
}

type timerCtxKey struct{}

// timerContext resolves {timerID} against manager and 404s if it doesn't
// exist, stashing the *subathon.Timer on the request context.
func timerContext(manager *subathon.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "timerID")
			t, ok := manager.Get(id)
			if !ok {
				http.Error(w, "timer not found", http.StatusNotFound)
				return
			}
			ctx := context.WithValue(r.Context(), timerCtxKey{}, t)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// timerFromContext retrieves the *subathon.Timer stashed by timerContext.
// Only valid for handlers mounted under it.
func timerFromContext(r *http.Request) *subathon.Timer {
	t, _ := r.Context().Value(timerCtxKey{}).(*subathon.Timer)
	return t
}

func handleGetState() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

type resetRequest struct {
	// InitialSeconds is how long the clock should be (re)initialized to.
	// Optional; server default is used when omitted.
	InitialSeconds int `json:"initialSeconds"`
}

// handleReset (re)initializes the clock to the given duration and starts it
// running, discarding any time left over from a previous run.
func handleReset(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req resetRequest
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}

		d := time.Hour
		if req.InitialSeconds > 0 {
			d = time.Duration(req.InitialSeconds) * time.Second
		}

		if err := t.Reset(d); err != nil {
			log.Printf("server: reset timer %s: %v", t.ID(), err)
			http.Error(w, "failed to reset timer", http.StatusInternalServerError)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

// handleResume continues the clock from wherever it was frozen by a
// previous stop, without resetting the remaining time.
func handleResume(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		if err := t.Resume(); err != nil {
			log.Printf("server: resume timer %s: %v", t.ID(), err)
			http.Error(w, "failed to resume timer", http.StatusInternalServerError)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

func handleStop(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		if err := t.Stop(); err != nil {
			log.Printf("server: stop timer %s: %v", t.ID(), err)
			http.Error(w, "failed to stop timer", http.StatusInternalServerError)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

type addEventRequest struct {
	Platform     subathon.Platform  `json:"platform"`
	Type         subathon.EventType `json:"type"`
	Username     string             `json:"username"`
	SecondsAdded int                `json:"secondsAdded"`
	Amount       float64            `json:"amount,omitempty"`
}

// handleAddEvent lets you manually add a contributor event, useful for
// testing the overlay before real platform integrations are wired up.
func handleAddEvent(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req addEventRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Platform == "" {
			req.Platform = subathon.PlatformManual
		}
		if req.Type == "" {
			req.Type = subathon.EventManual
		}

		err := t.AddEvent(subathon.Event{
			Platform:     req.Platform,
			Type:         req.Type,
			Username:     req.Username,
			SecondsAdded: req.SecondsAdded,
			Amount:       req.Amount,
		})
		if err != nil {
			log.Printf("server: add event to timer %s: %v", t.ID(), err)
			http.Error(w, "failed to record event", http.StatusInternalServerError)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusCreated, t.Snapshot())
	}
}

// handleWS resolves {timerID} itself (rather than going through
// timerContext) because chi's route tree keeps this outside /api/timers.
func handleWS(manager *subathon.Manager, hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "timerID")
		if _, ok := manager.Get(id); !ok {
			http.Error(w, "timer not found", http.StatusNotFound)
			return
		}
		hub.ServeHTTP(id, w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
