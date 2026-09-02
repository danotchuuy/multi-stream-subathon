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
	states := oauth.NewStateStore()

	hub := ws.NewHub()

	router := server.New(server.Deps{
		Manager:        manager,
		Hub:            hub,
		Auth:           authService,
		OAuthProviders: providers,
		OAuthStates:    states,
		AllowedOrigin:  cfg.AllowedOrigin,
		CookieSecure:   cfg.CookieSecure,
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

	// Bound memory from abandoned OAuth flows and expired sessions.
	go cleanupLoop(ctx, states, repo)

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

func cleanupLoop(ctx context.Context, states *oauth.StateStore, repo *sqlite.Repo) {
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
		}
	}
}
