// Command server runs the multi-stream subathon tracker API: it serves the
// REST + WebSocket backend that the frontend dashboard and OBS overlay
// connect to.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danotchuuy/multi-stream-subathon/internal/auth"
	"github.com/danotchuuy/multi-stream-subathon/internal/config"
	"github.com/danotchuuy/multi-stream-subathon/internal/oauth"
	"github.com/danotchuuy/multi-stream-subathon/internal/platform/kick"
	"github.com/danotchuuy/multi-stream-subathon/internal/platform/streamelements"
	"github.com/danotchuuy/multi-stream-subathon/internal/platform/throne"
	"github.com/danotchuuy/multi-stream-subathon/internal/platform/twitch"
	"github.com/danotchuuy/multi-stream-subathon/internal/platform/youtube"
	"github.com/danotchuuy/multi-stream-subathon/internal/server"
	"github.com/danotchuuy/multi-stream-subathon/internal/sqlite"
	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
	"github.com/danotchuuy/multi-stream-subathon/internal/ws"
)

func main() {
	cfg := config.Load()

	repo, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database at %s: %v", cfg.DBPath, err)
	}
	defer repo.Close()

	manager, err := subathon.NewManager(repo)
	if err != nil {
		log.Fatalf("load timers: %v", err)
	}

	authService := auth.NewService(repo)

	providers := map[string]*oauth.Provider{}
	if cfg.Twitch.ClientID != "" {
		providers["twitch"] = oauth.NewTwitch(cfg.Twitch.ClientID, cfg.Twitch.ClientSecret, cfg.Twitch.RedirectURL)
	} else {
		log.Println("TWITCH_CLIENT_ID not set; sign up/log in with Twitch is disabled")
	}
	if cfg.Kick.ClientID != "" {
		providers["kick"] = oauth.NewKick(cfg.Kick.ClientID, cfg.Kick.ClientSecret, cfg.Kick.RedirectURL)
	} else {
		log.Println("KICK_CLIENT_ID not set; sign up/log in with Kick is disabled")
	}
	var youtubeOAuth *oauth.Provider
	if cfg.YouTube.ClientID != "" {
		youtubeOAuth = oauth.NewYouTube(cfg.YouTube.ClientID, cfg.YouTube.ClientSecret, cfg.YouTube.RedirectURL)
		providers["youtube"] = youtubeOAuth
	} else {
		log.Println("YOUTUBE_CLIENT_ID not set; sign up/log in with YouTube (and watching a YouTube channel) is disabled")
	}
	states := oauth.NewStateStore()

	var twitchClient *twitch.Client
	if cfg.Twitch.ClientID != "" && cfg.TwitchWebhookSecret != "" {
		twitchClient = twitch.New(twitch.Config{
			ClientID:      cfg.Twitch.ClientID,
			ClientSecret:  cfg.Twitch.ClientSecret,
			WebhookSecret: cfg.TwitchWebhookSecret,
			CallbackURL:   cfg.TwitchWebhookCallbackURL,
		})
	} else {
		log.Println("TWITCH_WEBHOOK_SECRET not set; live Twitch sub/bits events are disabled (reward rules still work for manual events)")
	}
	if twitchClient != nil {
		reconcileTwitchSubscriptions(twitchClient, manager)
	}

	var kickClient *kick.Client
	if cfg.Kick.ClientID != "" {
		kickClient = kick.New(kick.Config{
			ClientID:     cfg.Kick.ClientID,
			ClientSecret: cfg.Kick.ClientSecret,
		})
	} else {
		log.Println("KICK_CLIENT_ID not set; live Kick sub events are disabled (reward rules still work for manual events)")
	}

	hub := ws.NewHub()

	// The REST wrapper needs no app credential itself and is always
	// constructed; the OAuth provider that lets a timer owner connect
	// their account (replacing the old JWT-paste flow) does need one, so
	// it's only built when configured — same gating as Twitch/Kick above.
	streamElementsClient := streamelements.NewClient()
	var streamElementsOAuth *oauth.Provider
	if cfg.StreamElements.ClientID != "" {
		streamElementsOAuth = oauth.NewStreamElements(cfg.StreamElements.ClientID, cfg.StreamElements.ClientSecret, cfg.StreamElements.RedirectURL)
	} else {
		log.Println("STREAMELEMENTS_CLIENT_ID not set; connecting StreamElements tips is disabled (reward rules still work for manual events)")
	}
	streamElementsPoller := streamelements.NewPoller(streamElementsClient, streamElementsOAuth, hub)
	streamElementsPoller.StartAll(manager)

	// Same split as StreamElements above: the REST/gRPC wrapper needs no
	// app credential and is always constructed; youtubeOAuth (nil unless
	// YOUTUBE_CLIENT_ID is set) is what lets youtubePoller refresh a
	// watched channel owner's access token past its ~1 hour lifetime.
	youtubeClient := youtube.NewClient()
	youtubePoller := youtube.NewPoller(youtubeClient, youtubeOAuth, authService, hub)
	youtubePoller.StartAll(manager)

	// Needs no app credential at all — Throne's webhook-signing key is
	// fixed and published, not per-account — so this is always
	// constructed, same as the StreamElements/YouTube REST wrappers above.
	throneClient, err := throne.New(throne.Config{PublicKeyPEM: cfg.ThroneWebhookPublicKey})
	if err != nil {
		log.Fatalf("build throne client: %v", err)
	}

	router := server.New(server.Deps{
		Manager:              manager,
		Hub:                  hub,
		Auth:                 authService,
		OAuthProviders:       providers,
		OAuthStates:          states,
		Twitch:               twitchClient,
		Kick:                 kickClient,
		StreamElements:       streamElementsClient,
		StreamElementsOAuth:  streamElementsOAuth,
		StreamElementsPoller: streamElementsPoller,
		YouTube:              youtubeClient,
		YouTubePoller:        youtubePoller,
		Throne:               throneClient,
		AllowedOrigin:        cfg.AllowedOrigin,
		CookieSecure:         cfg.CookieSecure,
		UIDistDir:            cfg.UIDistDir,
	})
	httpServer := &http.Server{
		Addr:    cfg.Addr,
		Handler: router,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Push a snapshot to connected clients on a steady tick so the
	// countdown updates smoothly even between events.
	go broadcastLoop(ctx, manager, hub)

	// Bound memory from abandoned OAuth flows, expired sessions, and (if
	// configured) each platform's webhook notification dedupe set.
	go cleanupLoop(ctx, states, repo, twitchClient, kickClient, throneClient)

	go func() {
		log.Printf("subathon server listening on %s (db: %s)", cfg.Addr, cfg.DBPath)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("server: shutdown: %v", err)
	}
}

// reconcileTwitchSubscriptions ensures every already-configured Twitch
// channel (across every timer) has the full, current set of EventSub
// subscriptions — not just whatever subset existed when its channel was
// first picked or its owner last completed the Twitch OAuth flow (the
// only two places that otherwise call EnsureBroadcasterSubscriptions; see
// handleSetTwitchChannel/handleOAuthCallback). Without this, adding a new
// subscription type to twitch.subscriptionTypes (e.g. resubs via
// channel.subscription.message) would silently never take effect for any
// channel configured before the change shipped, since nothing would ever
// prompt Twitch to (re-)subscribe it — exactly what happened here. Runs
// once at startup; a broadcaster watched by more than one timer is only
// reconciled once.
func reconcileTwitchSubscriptions(client *twitch.Client, manager *subathon.Manager) {
	seen := make(map[string]bool)
	for _, t := range manager.All() {
		id, username := t.TwitchChannel()
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if err := client.EnsureBroadcasterSubscriptions(id); err != nil {
			log.Printf("twitch: reconcile eventsub subscriptions for %s (%s): %v", username, id, err)
		}
	}
}

func broadcastLoop(ctx context.Context, manager *subathon.Manager, hub *ws.Hub) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, t := range manager.All() {
				hub.Broadcast(t.ID(), t.Snapshot())
			}
		}
	}
}

func cleanupLoop(ctx context.Context, states *oauth.StateStore, repo *sqlite.Repo, twitchClient *twitch.Client, kickClient *kick.Client, throneClient *throne.Client) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			states.Sweep()
			if err := repo.DeleteExpiredSessions(); err != nil {
				log.Printf("cleanup: delete expired sessions: %v", err)
			}
			if twitchClient != nil {
				twitchClient.Sweep()
			}
			if kickClient != nil {
				kickClient.Sweep()
			}
			throneClient.Sweep()
		}
	}
}
