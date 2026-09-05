package server

import (
	"log"
	"net/http"
	"time"
)

type streamElementsStatusResponse struct {
	Connected   bool   `json:"connected"`
	DisplayName string `json:"displayName,omitempty"`
	// OAuthConfigured tells the frontend whether the "Connect" button
	// will work at all — this server-level app credential, unlike a
	// timer's own connection, is either configured or it isn't (see
	// Deps.StreamElementsOAuth).
	OAuthConfigured bool `json:"oauthConfigured"`
}

// handleGetStreamElementsStatus reports whether this timer has a
// StreamElements account connected, and its display name if so — never
// the tokens themselves (see Timer.StreamElementsStatus).
func handleGetStreamElementsStatus(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		connected, displayName := t.StreamElementsStatus()
		writeJSON(w, http.StatusOK, streamElementsStatusResponse{
			Connected:       connected,
			DisplayName:     displayName,
			OAuthConfigured: deps.StreamElementsOAuth != nil,
		})
	}
}

// handleDisconnectStreamElements clears this timer's connected
// StreamElements account (see Timer.SetStreamElementsAccount) and stops
// polling it. Connecting one happens via OAuth2 instead — see
// handleStreamElementsOAuthStart/handleStreamElementsOAuthCallback in
// streamelements_oauth.go.
func handleDisconnectStreamElements(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		if err := t.SetStreamElementsAccount("", "", time.Time{}, "", ""); err != nil {
			log.Printf("server: clear stream elements account for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to disconnect StreamElements", http.StatusInternalServerError)
			return
		}
		deps.StreamElementsPoller.Watch(t)
		writeJSON(w, http.StatusOK, streamElementsStatusResponse{OAuthConfigured: deps.StreamElementsOAuth != nil})
	}
}
