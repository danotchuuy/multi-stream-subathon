// Package server wires up the HTTP API and WebSocket endpoint for the
// subathon tracker. It supports multiple independent timers, each
// addressed by an ID: /api/timers/{timerId}/... and /ws/{timerId}.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/danotchuuy/multi-stream-subathon/internal/auth"
	"github.com/danotchuuy/multi-stream-subathon/internal/oauth"
	"github.com/danotchuuy/multi-stream-subathon/internal/platform/kick"
	"github.com/danotchuuy/multi-stream-subathon/internal/platform/streamelements"
	"github.com/danotchuuy/multi-stream-subathon/internal/platform/throne"
	"github.com/danotchuuy/multi-stream-subathon/internal/platform/twitch"
	"github.com/danotchuuy/multi-stream-subathon/internal/platform/youtube"
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

	// Twitch drives live EventSub subscriptions and verifies/handles
	// their webhook notifications. Nil disables /webhooks/twitch and live
	// Twitch events entirely (TWITCH_WEBHOOK_SECRET unset).
	Twitch *twitch.Client

	// Kick is Twitch's equivalent for Kick's webhook events. Nil disables
	// /webhooks/kick and live Kick events (KICK_CLIENT_ID unset).
	Kick *kick.Client

	// StreamElements resolves an OAuth2 access token to the channel it
	// belongs to (see handleStreamElementsOAuthCallback). Always non-nil,
	// even if StreamElementsOAuth is nil (unconfigured) — the REST
	// wrapper itself needs no app credential.
	StreamElements *streamelements.Client

	// StreamElementsOAuth drives the "Connect StreamElements" flow (see
	// streamelements_oauth.go) using this server's own registered app
	// credential. Nil disables it (STREAMELEMENTS_CLIENT_ID unset) — same
	// gating as Twitch/Kick above, unlike the old JWT-paste flow this
	// replaced, which had no server-level credential to gate on.
	StreamElementsOAuth *oauth.Provider

	// StreamElementsPoller is told to start/stop polling a timer whenever
	// its connected StreamElements account changes (see
	// handleStreamElementsOAuthCallback/handleDisconnectStreamElements).
	StreamElementsPoller *streamelements.Poller

	// YouTube resolves an access token to the channel it belongs to and
	// checks whether it's currently live. Always non-nil — the REST/gRPC
	// wrapper itself needs no app credential (see YouTubePoller, which
	// does).
	YouTube *youtube.Client

	// YouTubePoller is told to (re)start/stop watching a timer's YouTube
	// channel whenever it changes (see handleSetYouTubeChannel). Always
	// non-nil; a nil provider inside it (YOUTUBE_CLIENT_ID unset) makes
	// Watch a no-op, same effective gating as Twitch/Kick above.
	YouTubePoller *youtube.Poller

	// Throne verifies and parses Throne webhook notifications (see
	// internal/platform/throne). Unlike every other integration above,
	// this needs no server-level app credential — Throne's signing key is
	// fixed and published — so it's always non-nil and
	// /webhooks/throne/{timerID} is always mounted.
	Throne *throne.Client

	// AllowedOrigin is the frontend's origin, used both for CORS and as
	// the base URL to redirect back to after an OAuth flow completes.
	AllowedOrigin string
	CookieSecure  bool

	// UIDistDir is the path to the built frontend assets (frontend/dist
	// after `npm run build`). Empty skips static file serving entirely,
	// so the Go API can still run standalone behind the Vite dev server
	// during local development.
	UIDistDir string
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
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
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

			// Viewing a timer's state (and its full contribution history)
			// is public: the ID itself is the token an overlay/dashboard
			// viewer needs, no login required — same as Snapshot's capped
			// RecentEvents already being part of the public state.
			r.Get("/state", handleGetState())
			r.Get("/events", handleListEvents())

			// Also public, same reasoning: the overlay needs to know which
			// dollar checkpoints exist (and, by comparing against the
			// snapshot it already gets over /state or /ws, which are
			// reached) without a login.
			r.Get("/money-milestones", handleGetMoneyMilestones())

			// Public too, same token — backs the top-10 leaderboard panel
			// (meant for a Twitch "panel" under a channel's About section,
			// not an authenticated view), same reasoning as /events, which
			// this re-scans and aggregates.
			r.Get("/leaderboard", handleGetLeaderboard())

			r.Group(func(r chi.Router) {
				r.Use(requireAuth(deps.Auth))
				r.Use(requireOwnerOrModerator())
				r.Post("/control/reset", handleReset(deps.Hub))
				r.Post("/control/resume", handleResume(deps.Hub))
				r.Post("/control/stop", handleStop(deps.Hub))
				r.Post("/control/lock", handleTimerAction(deps.Hub, "lock", func(t *subathon.Timer) error { return t.SetLocked(true) }))
				r.Post("/control/unlock", handleTimerAction(deps.Hub, "unlock", func(t *subathon.Timer) error { return t.SetLocked(false) }))
				r.Post("/control/hide", handleTimerAction(deps.Hub, "hide", func(t *subathon.Timer) error { return t.SetHidden(true) }))
				r.Post("/control/unhide", handleTimerAction(deps.Hub, "unhide", func(t *subathon.Timer) error { return t.SetHidden(false) }))
				// Ending/reopening a timer — unlike lock/hide above, this is
				// dashboard-only (no "!timer end" chat command; see
				// Timer.SetEnded) and also stops/resumes its StreamElements
				// poller goroutine, the one listener Manager's channel-keyed
				// queries don't already cover.
				r.Post("/control/end", handleTimerAction(deps.Hub, "end", func(t *subathon.Timer) error {
					if err := t.SetEnded(true); err != nil {
						return err
					}
					deps.StreamElementsPoller.Stop(t.ID())
					return nil
				}))
				r.Post("/control/unend", handleTimerAction(deps.Hub, "unend", func(t *subathon.Timer) error {
					if err := t.SetEnded(false); err != nil {
						return err
					}
					deps.StreamElementsPoller.Watch(t)
					return nil
				}))
				r.Post("/events", handleAddEvent(deps.Hub))
				r.Delete("/events/{eventID}", handleRemoveEvent(deps.Hub))
				r.Get("/reward-rules", handleGetRewardRules())
				r.Put("/reward-rules", handleSaveRewardRules())
				r.Get("/money-rules", handleGetMoneyRules())
				r.Put("/money-rules", handleSaveMoneyRules())
				r.Put("/money-goal", handleSetMoneyGoal(deps.Hub))
				r.Put("/money-raised", handleSetMoneyRaised(deps.Hub))
				r.Put("/money-milestones", handleSaveMoneyMilestones())
				r.Put("/overlay-colors", handleSetOverlayColors(deps.Hub))
				r.Put("/stats-rotation", handleSetStatsRotation(deps.Hub))
				r.Put("/contribution-counts", handleSetContributionCounts(deps.Hub))
				r.Put("/stat-icons", handleSetStatIcons(deps.Hub))
				r.Put("/panel-colors", handleSetPanelColors(deps.Hub))
				r.Put("/twitch-channel", handleSetTwitchChannel(deps))
				r.Put("/kick-channel", handleSetKickChannel(deps))
				r.Put("/youtube-channel", handleSetYouTubeChannel(deps))
				r.Get("/stream-elements-token", handleGetStreamElementsStatus(deps))
				r.Delete("/stream-elements-token", handleDisconnectStreamElements(deps))
				r.Get("/stream-elements/oauth/start", handleStreamElementsOAuthStart(deps))
				r.Get("/moderators", handleListModerators(deps))
			})

			// Managing *who* moderates a timer is owner-only — unlike the
			// group above, moderators can't grant or revoke access
			// themselves.
			r.Group(func(r chi.Router) {
				r.Use(requireAuth(deps.Auth))
				r.Use(requireOwner())
				r.Post("/moderators", handleAddModerator(deps))
				r.Delete("/moderators/{userID}", handleRemoveModerator(deps))
			})
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(requireAuth(deps.Auth))
		r.Get("/api/twitch/channels", handleListTwitchChannels(deps))
		r.Get("/api/youtube/channels", handleListYouTubeChannels(deps))
	})

	// Public: Twitch/Kick post their webhook notifications here;
	// authenticated by signature, not a session.
	if deps.Twitch != nil {
		r.Post("/webhooks/twitch", handleTwitchWebhook(deps))
	}
	if deps.Kick != nil {
		r.Post("/webhooks/kick", handleKickWebhook(deps))
	}
	// Public, and per-timer rather than a single shared endpoint like
	// Twitch/Kick above: Throne has no OAuth flow or broadcaster-ID
	// subscription to route by, so the timer ID is embedded directly in
	// the URL a creator pastes into Throne's own dashboard — same token
	// model as /ws/{timerID} below. Authenticated by Ed25519 signature
	// (see throne.Client.VerifyMessage), not a session.
	r.Post("/webhooks/throne/{timerID}", handleThroneWebhook(deps))

	// Public: the timer ID is the overlay's token, no login required.
	r.Get("/ws/{timerID}", handleWS(deps.Manager, deps.Hub))

	// Serve the built frontend (dashboard + overlay) for everything that
	// didn't match an API route above, so a single container can serve
	// the whole app behind one origin. Skipped entirely if UIDistDir is
	// unset/missing, so the API still runs standalone behind the Vite
	// dev server during local development.
	if info, err := os.Stat(deps.UIDistDir); deps.UIDistDir != "" && err == nil && info.IsDir() {
		r.NotFound(spaHandler(deps.UIDistDir))
	}

	return r
}

// spaHandler serves static files out of dir, falling back to
// dir/index.html for any request that doesn't match a real file. That
// fallback is what lets client-side routes (react-router) resolve
// correctly on a hard refresh or deep link, since the browser is
// requesting a path the server itself never mounted.
func spaHandler(dir string) http.HandlerFunc {
	fileServer := http.FileServer(http.Dir(dir))
	return func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		fileServer.ServeHTTP(w, r)
	}
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

// handleListEvents returns this timer's full contribution history (every
// event ever recorded, not just Snapshot's capped recent list) — backs
// the /history page: sorting by column and filtering to one contributor
// are done client-side against this one list.
func handleListEvents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		events, err := t.AllEvents()
		if err != nil {
			log.Printf("server: load event history for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to load event history", http.StatusInternalServerError)
			return
		}
		if events == nil {
			// A nil slice would otherwise encode as JSON null instead of
			// [] — the frontend uses null to mean "hasn't loaded yet", so
			// a genuinely empty history would look stuck loading forever.
			events = []subathon.Event{}
		}

		writeJSON(w, http.StatusOK, events)
	}
}

// handleGetLeaderboard returns the top 10 contributors in each "gift
// category" (subs, bits/Kicks, tips/donations — see
// subathon.Timer.Leaderboard), re-scanning the timer's full event
// history each call. Backs the leaderboard panel page, meant to be
// added as a Twitch "panel" or similar static embed rather than
// refreshed every second like the live overlays, so this cost is fine
// at whatever interval that page polls on.
func handleGetLeaderboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		board, err := t.Leaderboard()
		if err != nil {
			log.Printf("server: compute leaderboard for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to compute leaderboard", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, board)
	}
}

type resetRequest struct {
	// InitialSeconds is how long the clock should be (re)initialized to.
	// Optional; server default is used when omitted.
	InitialSeconds int `json:"initialSeconds"`
}

// handleReset (re)initializes the clock to the given duration, discarding
// any time left over from a previous run. Leaves the timer stopped (see
// Timer.Reset) — a separate Resume/play call starts it.
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

// handleTimerAction runs action against the timer and broadcasts/returns
// its snapshot — the shared shape behind the lock/unlock/hide/unhide
// control endpoints, which only differ in which Timer method they call.
// Also what a moderator's "!timer lock"/"!timer unlock"/"!timer hide"/
// "!timer show" chat command runs (see twitch_webhook.go's
// applyChatCommand), just reached over HTTP instead of chat, for a
// manual toggle from the dashboard.
func handleTimerAction(hub *ws.Hub, verb string, action func(*subathon.Timer) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		if err := action(t); err != nil {
			log.Printf("server: %s timer %s: %v", verb, t.ID(), err)
			http.Error(w, fmt.Sprintf("failed to %s timer", verb), http.StatusInternalServerError)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

type addEventRequest struct {
	Platform subathon.Platform  `json:"platform"`
	Type     subathon.EventType `json:"type"`
	Username string             `json:"username"`
	// SecondsAdded is a float64, not an int, even though
	// subathon.Event.SecondsAdded itself is a whole number of seconds:
	// a caller computing it as count * secondsPerUnit for a dollar-
	// denominated contribution (e.g. the dashboard's "Add donation" form,
	// for a non-whole-dollar StreamElements/Throne tip like $4.33) can
	// easily land on a JS/JSON float like 259.79999999999995 — decoding
	// that straight into an int field fails outright ("invalid request
	// body") rather than just losing sub-second precision nobody needed
	// anyway, so this rounds it instead (see below).
	SecondsAdded float64 `json:"secondsAdded"`
	MoneyAdded   float64 `json:"moneyAdded,omitempty"`
	Amount       float64 `json:"amount,omitempty"`
}

// handleAddEvent lets you manually add a contributor event: either a raw
// test event (arbitrary seconds/dollars, useful for testing the overlay
// before real platform integrations are wired up) or a real donation
// recorded from the dashboard's "Add donation" form, whose seconds/
// dollars the frontend computes from the timer's donation_unit reward/
// money rules before submitting here — this endpoint itself is agnostic
// to which.
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
			SecondsAdded: int(math.Round(req.SecondsAdded)),
			MoneyAdded:   req.MoneyAdded,
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

// handleRemoveEvent deletes a previously recorded event (e.g. a mistaken or
// fraudulent contribution caught after the fact) from the dashboard's
// recent-contributors list, reversing its effect on the clock and money
// goal — see Timer.RemoveEvent. 404s if eventID isn't among this timer's
// recent events.
func handleRemoveEvent(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)
		eventID := chi.URLParam(r, "eventID")

		if err := t.RemoveEvent(eventID); err != nil {
			log.Printf("server: remove event %s from timer %s: %v", eventID, t.ID(), err)
			http.Error(w, "event not found", http.StatusNotFound)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

// handleGetRewardRules returns this timer's reward rules (seconds awarded
// per contribution, by item and platform), with defaults filled in for
// anything not yet explicitly configured.
func handleGetRewardRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		rules, err := t.RewardRules()
		if err != nil {
			log.Printf("server: load reward rules for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to load reward rules", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, rules)
	}
}

// handleSaveRewardRules replaces this timer's reward rules wholesale with
// the submitted grid.
func handleSaveRewardRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var rules subathon.RewardRules
		if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		for _, row := range rules {
			for _, secs := range row {
				if secs < 0 {
					http.Error(w, "seconds must be non-negative", http.StatusBadRequest)
					return
				}
			}
		}

		if err := t.SaveRewardRules(rules); err != nil {
			log.Printf("server: save reward rules for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to save reward rules", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, rules)
	}
}

// handleGetMoneyRules returns this timer's money rules (dollars awarded
// per contribution, by item and platform), with $0 defaults filled in for
// anything not yet explicitly configured.
func handleGetMoneyRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		rules, err := t.MoneyRules()
		if err != nil {
			log.Printf("server: load money rules for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to load money rules", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, rules)
	}
}

// handleSaveMoneyRules replaces this timer's money rules wholesale with
// the submitted grid.
func handleSaveMoneyRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var rules subathon.MoneyRules
		if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		for _, row := range rules {
			for _, dollars := range row {
				if dollars < 0 {
					http.Error(w, "dollars must be non-negative", http.StatusBadRequest)
					return
				}
			}
		}

		if err := t.SaveMoneyRules(rules); err != nil {
			log.Printf("server: save money rules for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to save money rules", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, rules)
	}
}

type setMoneyGoalRequest struct {
	Goal float64 `json:"goal"`
}

// handleSetMoneyGoal changes this timer's dollar goal. A goal of 0 clears
// it.
func handleSetMoneyGoal(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req setMoneyGoalRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Goal < 0 {
			http.Error(w, "goal must be non-negative", http.StatusBadRequest)
			return
		}

		if err := t.SetMoneyGoal(req.Goal); err != nil {
			log.Printf("server: set money goal for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to set money goal", http.StatusInternalServerError)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

type setMoneyRaisedRequest struct {
	Amount float64 `json:"amount"`
}

// handleSetMoneyRaised directly overrides this timer's running dollar
// total, e.g. to reconcile against an external donation tracker rather
// than relying solely on recorded events. Unlike /events, this doesn't
// add a history entry.
func handleSetMoneyRaised(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req setMoneyRaisedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Amount < 0 {
			http.Error(w, "amount must be non-negative", http.StatusBadRequest)
			return
		}

		if err := t.SetMoneyRaised(req.Amount); err != nil {
			log.Printf("server: set money raised for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to set money raised", http.StatusInternalServerError)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

// handleGetMoneyMilestones returns this timer's configured dollar-amount
// milestones, ordered by amount ascending. Public, same as /state and
// /events: the overlay needs these without a login.
func handleGetMoneyMilestones() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		milestones, err := t.MoneyMilestones()
		if err != nil {
			log.Printf("server: load money milestones for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to load money milestones", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, milestones)
	}
}

// handleSaveMoneyMilestones replaces this timer's milestone list wholesale
// with the submitted list.
func handleSaveMoneyMilestones() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var milestones []subathon.MoneyMilestone
		if err := json.NewDecoder(r.Body).Decode(&milestones); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		for _, m := range milestones {
			if m.Amount <= 0 {
				http.Error(w, "milestone amount must be positive", http.StatusBadRequest)
				return
			}
			if m.Label == "" {
				http.Error(w, "milestone label must not be empty", http.StatusBadRequest)
				return
			}
		}

		if err := t.SaveMoneyMilestones(milestones); err != nil {
			log.Printf("server: save money milestones for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to save money milestones", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, milestones)
	}
}

var hexColorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// handleSetOverlayColors changes the public overlays' pill colors (both
// /overlay's timer/money-goal pills and /goals-overlay's goal/goal-amount
// pills). An empty field falls back to subathon.DefaultOverlayColors for
// that field; a non-empty one must be a 6-digit hex color (what
// <input type="color"> produces).
func handleSetOverlayColors(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var colors subathon.OverlayColors
		if err := json.NewDecoder(r.Body).Decode(&colors); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		fields := []string{
			colors.TimerBg, colors.TimerText, colors.MoneyBg, colors.MoneyText,
			colors.GoalBg, colors.GoalText, colors.GoalAmountBg, colors.GoalAmountText,
		}
		for _, c := range fields {
			if c != "" && !hexColorRE.MatchString(c) {
				http.Error(w, "colors must be 6-digit hex, e.g. #111111", http.StatusBadRequest)
				return
			}
		}

		if err := t.SetOverlayColors(colors); err != nil {
			log.Printf("server: set overlay colors for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to set overlay colors", http.StatusInternalServerError)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

// handleSetPanelColors changes the top-10 leaderboard panel's colors
// (see the styling page's leaderboard-panel section). An empty field
// falls back to subathon.DefaultPanelColors for that field; a non-empty
// one must be a 6-digit hex color.
func handleSetPanelColors(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var colors subathon.PanelColors
		if err := json.NewDecoder(r.Body).Decode(&colors); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		fields := []string{colors.Bg, colors.Text, colors.AccentBg, colors.AccentText}
		for _, c := range fields {
			if c != "" && !hexColorRE.MatchString(c) {
				http.Error(w, "colors must be 6-digit hex, e.g. #111111", http.StatusBadRequest)
				return
			}
		}

		if err := t.SetPanelColors(colors); err != nil {
			log.Printf("server: set panel colors for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to set panel colors", http.StatusInternalServerError)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

type setStatsRotationRequest struct {
	Enabled bool `json:"enabled"`
}

// handleSetStatsRotation turns the main overlay's rotating stat list
// (subs/bits-Kicks/donations, far left of the timer/money pills) on or
// off — see Timer.SetStatsRotationEnabled.
func handleSetStatsRotation(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req setStatsRotationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if err := t.SetStatsRotationEnabled(req.Enabled); err != nil {
			log.Printf("server: set stats rotation for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to set stats rotation", http.StatusInternalServerError)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

type setContributionCountsRequest struct {
	SubsGiven      int `json:"subsGiven"`
	BitsGiven      int `json:"bitsGiven"`
	DonationsGiven int `json:"donationsGiven"`
}

// handleSetContributionCounts directly overrides the rotating stat
// list's three running totals — see Timer.SetContributionCounts. Like
// /money-raised, this doesn't add a history entry.
func handleSetContributionCounts(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req setContributionCountsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.SubsGiven < 0 || req.BitsGiven < 0 || req.DonationsGiven < 0 {
			http.Error(w, "counts must be non-negative", http.StatusBadRequest)
			return
		}

		if err := t.SetContributionCounts(req.SubsGiven, req.BitsGiven, req.DonationsGiven); err != nil {
			log.Printf("server: set contribution counts for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to set contribution counts", http.StatusInternalServerError)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

// maxStatIconRunes bounds each StatIcons emoji field — generous enough
// for a multi-codepoint emoji (a skin-tone modifier or ZWJ sequence can
// run to several runes) while still rejecting arbitrary pasted-in text.
const maxStatIconRunes = 8

// statSVGIconKeys are the only valid Subs/Bits/Donations values when
// StatIcons.Style is "svg" — must match the frontend's SVG_ICON_OPTIONS
// (see frontend/src/lib/statRotation.tsx) exactly, since these keys pick
// which <svg> that page renders.
var statSVGIconKeys = map[string]bool{
	"heart":     true,
	"star":      true,
	"gem":       true,
	"dollar":    true,
	"gift":      true,
	"crown":     true,
	"bolt":      true,
	"fire":      true,
	"check":     true,
	"diamond":   true,
	"snowflake": true,
	"sparkle":   true,
	"note":      true,
	"flag":      true,
	"skull":     true,
	"wallet":    true,
}

// handleSetStatIcons changes each rotating stat list category's icon —
// either an emoji or, in "svg" style, a monochrome SVG icon key plus its
// own color (see the styling page's icon-style toggle). An empty field
// falls back to subathon.DefaultStatIcons for that field.
func handleSetStatIcons(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var icons subathon.StatIcons
		if err := json.NewDecoder(r.Body).Decode(&icons); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if icons.Style != "" && icons.Style != subathon.StatIconStyleEmoji && icons.Style != subathon.StatIconStyleSVG {
			http.Error(w, `style must be "emoji" or "svg"`, http.StatusBadRequest)
			return
		}
		for _, icon := range []string{icons.Subs, icons.Bits, icons.Donations} {
			if icon == "" {
				continue
			}
			if icons.Style == subathon.StatIconStyleSVG {
				if !statSVGIconKeys[icon] {
					http.Error(w, "unrecognized svg icon key", http.StatusBadRequest)
					return
				}
			} else if utf8.RuneCountInString(icon) > maxStatIconRunes {
				http.Error(w, "each icon must be a single emoji", http.StatusBadRequest)
				return
			}
		}
		for _, c := range []string{icons.SubsColor, icons.BitsColor, icons.DonationsColor} {
			if c != "" && !hexColorRE.MatchString(c) {
				http.Error(w, "colors must be 6-digit hex, e.g. #111111", http.StatusBadRequest)
				return
			}
		}

		if err := t.SetStatIcons(icons); err != nil {
			log.Printf("server: set stat icons for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to set stat icons", http.StatusInternalServerError)
			return
		}

		hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
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
