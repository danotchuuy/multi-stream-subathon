package server

import (
	"log"
	"net/http"
	"time"

	"github.com/danotchuuy/multi-stream-subathon/internal/oauth"
)

// handleStreamElementsOAuthStart begins connecting this timer's
// StreamElements account for tip polling, replacing the old flow of
// pasting in a JWT token — redirects the browser to StreamElements' own
// authorize page. Mounted under the timer's owner-or-moderator-gated
// route group (see server.go), so only someone who can already control
// this timer can (re)connect its donations; the callback below trusts
// the "state" token minted here rather than re-checking identity itself.
func handleStreamElementsOAuthStart(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.StreamElementsOAuth == nil {
			http.Error(w, "StreamElements isn't configured on this server", http.StatusNotFound)
			return
		}
		t := timerFromContext(r)

		state := oauth.NewState()
		deps.OAuthStates.Put(state, oauth.FlowState{
			Provider:  "streamelements",
			Purpose:   "streamelements",
			TimerID:   t.ID(),
			ExpiresAt: time.Now().Add(flowStateTTL),
		})

		http.Redirect(w, r, deps.StreamElementsOAuth.AuthorizeURL(state, ""), http.StatusFound)
	}
}

// handleStreamElementsOAuthCallback completes the flow handleStreamElements
// OAuthStart began: exchanges the authorization code, resolves which
// StreamElements channel it belongs to, saves that account against
// whichever timer the "state" token recorded, (re)starts polling it, and
// redirects back to that timer's dashboard. Mounted as a fixed public
// route (see mountAuthRoutes) — like /auth/{provider}/callback, it's
// driven entirely by the one-time state token rather than a session, so
// it doesn't need requireAuth.
func handleStreamElementsOAuthCallback(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.StreamElementsOAuth == nil {
			http.Error(w, "StreamElements isn't configured on this server", http.StatusNotFound)
			return
		}

		if msg := r.URL.Query().Get("error"); msg != "" {
			redirectWithError(w, r, deps.AllowedOrigin+"/", msg)
			return
		}

		state := r.URL.Query().Get("state")
		fs, ok := deps.OAuthStates.Take(state)
		if !ok || fs.Provider != "streamelements" {
			http.Error(w, "invalid or expired oauth state", http.StatusBadRequest)
			return
		}

		t, ok := deps.Manager.Get(fs.TimerID)
		if !ok {
			http.Error(w, "timer no longer exists", http.StatusGone)
			return
		}
		dashboardURL := deps.AllowedOrigin + "/t/" + t.ID()

		code := r.URL.Query().Get("code")
		accessToken, refreshToken, expiresAt, err := deps.StreamElementsOAuth.Exchange(r.Context(), code, "")
		if err != nil {
			log.Printf("streamelements oauth: token exchange: %v", err)
			redirectWithError(w, r, dashboardURL, "oauth_failed")
			return
		}

		account, err := deps.StreamElements.ResolveChannel(accessToken)
		if err != nil {
			log.Printf("streamelements oauth: resolve channel: %v", err)
			redirectWithError(w, r, dashboardURL, "oauth_failed")
			return
		}

		if err := t.SetStreamElementsAccount(accessToken, refreshToken, expiresAt, account.ChannelID, account.DisplayName); err != nil {
			log.Printf("streamelements oauth: save account for timer %s: %v", t.ID(), err)
			redirectWithError(w, r, dashboardURL, "oauth_failed")
			return
		}

		deps.StreamElementsPoller.Watch(t)

		http.Redirect(w, r, dashboardURL, http.StatusFound)
	}
}
