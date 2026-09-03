package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/danotchuuy/multi-stream-subathon/internal/platform/kick"
	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

const maxKickBodyBytes = 1 << 20 // 1MB: generous for a webhook payload

// handleKickWebhook receives Kick webhook notifications for every
// broadcaster some timer has been configured to watch (see
// Timer.SetKickChannel and kick.Client.EnsureBroadcasterSubscriptions). It
// verifies the request's signature and translates each notification into a
// subathon.Event added to every currently-running timer watching that
// broadcaster.
//
// NOTE: see the platform/kick package doc comment for how confident this
// webhook delivery format (which header carries the event type, whether
// there's a verification handshake) actually is.
func handleKickWebhook(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxKickBodyBytes))
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if !deps.Kick.VerifyMessage(r.Header, body) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}

		messageID := r.Header.Get("Kick-Event-Message-Id")
		if messageID != "" && deps.Kick.SeenBefore(messageID) {
			w.WriteHeader(http.StatusOK)
			return // Kick redelivery of a notification we already applied
		}

		eventType := r.Header.Get("Kick-Event-Type")
		parsed, ok, err := kick.ParseNotification(eventType, body)
		if err != nil {
			log.Printf("kick: parse %s notification: %v", eventType, err)
			w.WriteHeader(http.StatusOK)
			return
		}
		if !ok {
			w.WriteHeader(http.StatusOK)
			return
		}

		for _, t := range deps.Manager.RunningByKickChannel(parsed.BroadcasterUserID) {
			rules, err := t.RewardRules()
			if err != nil {
				log.Printf("kick: load reward rules for timer %s: %v", t.ID(), err)
				continue
			}
			moneyRules, err := t.MoneyRules()
			if err != nil {
				log.Printf("kick: load money rules for timer %s: %v", t.ID(), err)
				continue
			}

			secondsAdded := parsed.Seconds(rules[parsed.Item][subathon.PlatformKick])
			moneyAdded := parsed.Money(moneyRules[parsed.Item][subathon.PlatformKick])
			if secondsAdded <= 0 && moneyAdded <= 0 {
				continue
			}

			err = t.AddEvent(subathon.Event{
				Platform:     subathon.PlatformKick,
				Type:         parsed.Type,
				Username:     parsed.Username,
				SecondsAdded: secondsAdded,
				MoneyAdded:   moneyAdded,
				Amount:       float64(parsed.Count),
			})
			if err != nil {
				log.Printf("kick: add event to timer %s: %v", t.ID(), err)
				continue
			}
			deps.Hub.Broadcast(t.ID(), t.Snapshot())
		}

		w.WriteHeader(http.StatusOK)
	}
}

type setKickChannelRequest struct {
	// Username is the Kick channel slug to watch, e.g. "xqc". Empty stops
	// watching any channel.
	Username string `json:"username"`
}

// handleSetKickChannel changes which Kick channel's subs/gift-subs/Kicks
// add time to this timer. Resolves the given slug to a broadcaster ID via
// Kick, then (best-effort) ensures webhook subscriptions for it — using
// this app's own token (see kick.Client.EnsureBroadcasterSubscriptions),
// so unlike Twitch this doesn't require the watched channel to have
// signed into this app at all.
func handleSetKickChannel(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req setKickChannelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		username := strings.TrimSpace(strings.TrimPrefix(req.Username, "@"))

		if username == "" {
			if err := t.SetKickChannel("", ""); err != nil {
				log.Printf("server: clear kick channel for timer %s: %v", t.ID(), err)
				http.Error(w, "failed to clear kick channel", http.StatusInternalServerError)
				return
			}
			deps.Hub.Broadcast(t.ID(), t.Snapshot())
			writeJSON(w, http.StatusOK, t.Snapshot())
			return
		}

		if deps.Kick == nil {
			http.Error(w, "Kick integration is not configured on this server", http.StatusServiceUnavailable)
			return
		}

		broadcasterID, displayName, err := deps.Kick.LookupBroadcasterID(username)
		if err != nil {
			log.Printf("server: look up kick channel %q: %v", username, err)
			http.Error(w, "could not find that Kick channel", http.StatusBadRequest)
			return
		}

		if err := t.SetKickChannel(broadcasterID, displayName); err != nil {
			log.Printf("server: save kick channel for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to save kick channel", http.StatusInternalServerError)
			return
		}

		if err := deps.Kick.EnsureBroadcasterSubscriptions(broadcasterID); err != nil {
			log.Printf("kick: ensure webhook subscriptions for %s (%s): %v", username, broadcasterID, err)
		}

		deps.Hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}
