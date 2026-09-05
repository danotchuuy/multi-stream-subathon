package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/danotchuuy/multi-stream-subathon/internal/auth"
)

type setYouTubeChannelRequest struct {
	// ChannelID is the YouTube channel ID to watch, e.g. from
	// GET /api/youtube/channels. Empty stops watching any channel.
	ChannelID string `json:"channelId"`
}

// handleSetYouTubeChannel changes which YouTube channel's Super Chats/
// Super Stickers/new members/gifted memberships add time to this timer.
// Unlike handleSetTwitchChannel/handleSetKickChannel, there's no app-
// level lookup for an arbitrary channel — YouTube only lets a channel's
// own linked owner read its live chat (see internal/platform/youtube's
// package doc) — so channelID must be one the *acting* user (owner or
// moderator, whichever is calling this) has themselves linked via
// "Sign in with Google"/"Link YouTube" (see GET /api/youtube/channels,
// which only ever offers channels this same check would accept).
func handleSetYouTubeChannel(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)
		u := userFromContext(r)

		var req setYouTubeChannelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		channelID := strings.TrimSpace(req.ChannelID)

		if channelID == "" {
			if err := t.SetYouTubeChannel("", ""); err != nil {
				log.Printf("server: clear youtube channel for timer %s: %v", t.ID(), err)
				http.Error(w, "failed to clear youtube channel", http.StatusInternalServerError)
				return
			}
			deps.YouTubePoller.Watch(t)
			deps.Hub.Broadcast(t.ID(), t.Snapshot())
			writeJSON(w, http.StatusOK, t.Snapshot())
			return
		}

		identities, err := deps.Auth.Identities(u.ID)
		if err != nil {
			log.Printf("server: list identities for user %s: %v", u.ID, err)
			http.Error(w, "failed to save youtube channel", http.StatusInternalServerError)
			return
		}
		var title string
		found := false
		for _, ident := range identities {
			if ident.Platform == auth.PlatformYouTube && ident.PlatformUserID == channelID {
				title = ident.PlatformUsername
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "you haven't linked that YouTube channel — sign in with it first", http.StatusBadRequest)
			return
		}

		if err := t.SetYouTubeChannel(channelID, title); err != nil {
			log.Printf("server: save youtube channel for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to save youtube channel", http.StatusInternalServerError)
			return
		}

		deps.YouTubePoller.Watch(t)
		deps.Hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

type youtubeChannelOption struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// handleListYouTubeChannels returns the YouTube channel(s) the signed-in
// user could pick as a timer's watched channel: whichever of their own
// linked identities are on YouTube. Unlike Twitch/Kick, there's no
// "channels I moderate" concept here (see handleSetYouTubeChannel) — just
// the account(s) they've themselves signed into this app with.
func handleListYouTubeChannels(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r)

		identities, err := deps.Auth.Identities(u.ID)
		if err != nil {
			log.Printf("server: list identities for user %s: %v", u.ID, err)
			http.Error(w, "failed to load youtube channels", http.StatusInternalServerError)
			return
		}

		options := []youtubeChannelOption{}
		for _, ident := range identities {
			if ident.Platform == auth.PlatformYouTube {
				options = append(options, youtubeChannelOption{ID: ident.PlatformUserID, Title: ident.PlatformUsername})
			}
		}

		writeJSON(w, http.StatusOK, options)
	}
}
