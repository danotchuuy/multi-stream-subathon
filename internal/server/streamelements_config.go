package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

type streamElementsStatusResponse struct {
	Connected   bool   `json:"connected"`
	DisplayName string `json:"displayName,omitempty"`
}

// handleGetStreamElementsStatus reports whether this timer has a
// StreamElements account connected, and its display name if so — never
// the token itself (see Timer.StreamElementsStatus).
func handleGetStreamElementsStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		connected, displayName := t.StreamElementsStatus()
		writeJSON(w, http.StatusOK, streamElementsStatusResponse{
			Connected:   connected,
			DisplayName: displayName,
		})
	}
}

type setStreamElementsTokenRequest struct {
	Token string `json:"token"`
}

// handleSetStreamElementsToken connects (or, with an empty token,
// disconnects) this timer's StreamElements account. A non-empty token is
// validated by resolving it to a channel via deps.StreamElements before
// saving — an invalid/expired one is rejected here rather than silently
// never producing any tips. On success (or on disconnect),
// deps.StreamElementsPoller is told to start/stop polling accordingly.
func handleSetStreamElementsToken(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req setStreamElementsTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		token := strings.TrimSpace(req.Token)

		if token == "" {
			if err := t.SetStreamElementsAccount("", "", ""); err != nil {
				log.Printf("server: clear stream elements account for timer %s: %v", t.ID(), err)
				http.Error(w, "failed to disconnect StreamElements", http.StatusInternalServerError)
				return
			}
			deps.StreamElementsPoller.Watch(t)
			writeJSON(w, http.StatusOK, streamElementsStatusResponse{})
			return
		}

		account, err := deps.StreamElements.ResolveChannel(token)
		if err != nil {
			log.Printf("server: resolve stream elements token: %v", err)
			http.Error(w, "invalid StreamElements token", http.StatusBadRequest)
			return
		}

		if err := t.SetStreamElementsAccount(token, account.ChannelID, account.DisplayName); err != nil {
			log.Printf("server: save stream elements account for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to save StreamElements account", http.StatusInternalServerError)
			return
		}

		deps.StreamElementsPoller.Watch(t)

		writeJSON(w, http.StatusOK, streamElementsStatusResponse{
			Connected:   true,
			DisplayName: account.DisplayName,
		})
	}
}
