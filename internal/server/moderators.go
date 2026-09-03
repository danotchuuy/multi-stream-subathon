package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type moderatorView struct {
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
}

// handleListModerators returns this timer's current moderators (not
// including its owner). Available to the owner and existing moderators
// alike — seeing who else has access doesn't need to be owner-only,
// unlike granting or revoking it.
func handleListModerators(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		ids := t.ModeratorIDs()
		views := make([]moderatorView, 0, len(ids))
		for _, id := range ids {
			u, ok, err := deps.Auth.GetUser(id)
			if err != nil {
				log.Printf("server: get user %s (moderator of timer %s): %v", id, t.ID(), err)
				continue
			}
			if !ok {
				continue // account since deleted; drop it rather than fail the whole list
			}
			views = append(views, moderatorView{UserID: u.ID, DisplayName: u.DisplayName})
		}
		writeJSON(w, http.StatusOK, views)
	}
}

type addModeratorRequest struct {
	// Username is the Twitch or Kick username of the account to grant
	// moderator access to — whichever platform they've linked here.
	Username string `json:"username"`
}

// handleAddModerator grants moderator access to this timer to whichever
// account has Username linked on Twitch or Kick (see
// auth.Service.UserByUsername). Owner only: moderators can do everything
// an owner can to run the timer, but not manage who else can.
func handleAddModerator(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req addModeratorRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		username := strings.TrimSpace(strings.TrimPrefix(req.Username, "@"))
		if username == "" {
			http.Error(w, "username is required", http.StatusBadRequest)
			return
		}

		u, ok, err := deps.Auth.UserByUsername(username)
		if err != nil {
			log.Printf("server: look up user %q: %v", username, err)
			http.Error(w, "failed to look up that user", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "no account found for that username — they need to have signed into this app at least once", http.StatusNotFound)
			return
		}
		if u.ID == t.UserID() {
			http.Error(w, "that user already owns this timer", http.StatusBadRequest)
			return
		}

		if err := t.AddModerator(u.ID); err != nil {
			log.Printf("server: add moderator %s to timer %s: %v", u.ID, t.ID(), err)
			http.Error(w, "failed to add moderator", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusCreated, moderatorView{UserID: u.ID, DisplayName: u.DisplayName})
	}
}

// handleRemoveModerator revokes a user's moderator access to this timer.
// Owner only.
func handleRemoveModerator(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)
		userID := chi.URLParam(r, "userID")

		if err := t.RemoveModerator(userID); err != nil {
			log.Printf("server: remove moderator %s from timer %s: %v", userID, t.ID(), err)
			http.Error(w, "failed to remove moderator", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
