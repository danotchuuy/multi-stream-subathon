package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/danotchuuy/multi-stream-subathon/internal/auth"
	"github.com/danotchuuy/multi-stream-subathon/internal/platform/twitch"
	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

const maxTwitchBodyBytes = 1 << 20 // 1MB: generous for an EventSub payload

// handleTwitchWebhook receives Twitch EventSub notifications for every
// broadcaster some timer has been configured to watch (see
// Timer.SetTwitchChannel and twitch.Client.EnsureBroadcasterSubscriptions).
// It verifies the request's HMAC signature, answers subscription-
// verification challenges, and otherwise translates each notification into
// a subathon.Event added to every currently-running timer watching that
// broadcaster.
func handleTwitchWebhook(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxTwitchBodyBytes))
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if !deps.Twitch.VerifyMessage(r.Header, body) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}

		switch r.Header.Get("Twitch-Eventsub-Message-Type") {
		case "webhook_callback_verification":
			var challenge struct {
				Challenge string `json:"challenge"`
			}
			if err := json.Unmarshal(body, &challenge); err != nil {
				http.Error(w, "invalid challenge body", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(challenge.Challenge))
			return

		case "revocation":
			log.Printf("twitch: eventsub subscription revoked: %s", body)
			w.WriteHeader(http.StatusOK)
			return

		case "notification":
			handleTwitchNotification(deps, r.Header.Get("Twitch-Eventsub-Message-Id"), body)
			w.WriteHeader(http.StatusOK)
			return

		default:
			w.WriteHeader(http.StatusOK)
		}
	}
}

type setTwitchChannelRequest struct {
	// Username is the Twitch login/display name to watch, e.g. "shroud".
	// Empty stops watching any channel.
	Username string `json:"username"`
}

// handleSetTwitchChannel changes which Twitch channel's subs/gift-subs/
// cheers add time to this timer, and which channel's chat this timer
// listens to for "!timer pause"/"!timer unpause"/"!timer lock"/
// "!timer unlock"/"!timer hide"/"!timer unhide" commands from that
// channel's moderators/broadcaster. Resolves the given login
// name to a broadcaster ID via Twitch, then (best-effort) ensures EventSub
// subscriptions for it: the sub/gift/cheer ones only succeed once that
// channel's own broadcaster has authorized this app (see oauth.NewTwitch's
// scopes) — if they haven't yet, it's retried automatically the next time
// they complete the Twitch OAuth flow (see handleOAuthCallback) — while
// the chat-command one is read as the *timer owner's* linked Twitch
// identity, which only needs the owner themselves to be that channel's
// broadcaster or a moderator there (see
// twitch.Client.EnsureChatCommandSubscription), so it can activate
// immediately even for a channel that's never signed into this app.
func handleSetTwitchChannel(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req setTwitchChannelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		username := strings.TrimSpace(strings.TrimPrefix(req.Username, "@"))

		if username == "" {
			if err := t.SetTwitchChannel("", ""); err != nil {
				log.Printf("server: clear twitch channel for timer %s: %v", t.ID(), err)
				http.Error(w, "failed to clear twitch channel", http.StatusInternalServerError)
				return
			}
			deps.Hub.Broadcast(t.ID(), t.Snapshot())
			writeJSON(w, http.StatusOK, t.Snapshot())
			return
		}

		if deps.Twitch == nil {
			http.Error(w, "Twitch integration is not configured on this server", http.StatusServiceUnavailable)
			return
		}

		broadcasterID, canonicalLogin, err := deps.Twitch.LookupBroadcasterID(username)
		if err != nil {
			log.Printf("server: look up twitch channel %q: %v", username, err)
			http.Error(w, "could not find that Twitch channel", http.StatusBadRequest)
			return
		}

		if err := t.SetTwitchChannel(broadcasterID, canonicalLogin); err != nil {
			log.Printf("server: save twitch channel for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to save twitch channel", http.StatusInternalServerError)
			return
		}

		if err := deps.Twitch.EnsureBroadcasterSubscriptions(broadcasterID); err != nil {
			log.Printf("twitch: ensure eventsub subscriptions for %s (%s): %v", username, broadcasterID, err)
		}

		if identities, err := deps.Auth.Identities(t.UserID()); err != nil {
			log.Printf("server: list identities for timer %s owner: %v", t.ID(), err)
		} else {
			for _, ident := range identities {
				if ident.Platform != auth.PlatformTwitch {
					continue
				}
				if err := deps.Twitch.EnsureChatCommandSubscription(broadcasterID, ident.PlatformUserID); err != nil {
					log.Printf("twitch: ensure chat command subscription for %s (%s), read as %s: %v", username, broadcasterID, ident.PlatformUserID, err)
				}
				break
			}
		}

		deps.Hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

type twitchChannelOption struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	// Mine is true for the signed-in user's own channel, false for one
	// they only moderate.
	Mine bool `json:"mine"`
}

// handleListTwitchChannels returns the Twitch channels the signed-in user
// could plausibly pick as a timer's watched channel: their own linked
// channel (if any) and every channel Twitch says they moderate (if their
// Twitch identity has granted user:read:moderated_channels — see
// oauth.NewTwitch). Backs the dashboard's channel picker, restricting it
// to channels a pick will actually work for rather than free text nobody
// has authorized this app for (see handleSetTwitchChannel).
func handleListTwitchChannels(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r)
		options := []twitchChannelOption{}

		if deps.Twitch == nil {
			writeJSON(w, http.StatusOK, options)
			return
		}

		identities, err := deps.Auth.Identities(u.ID)
		if err != nil {
			log.Printf("server: list identities for user %s: %v", u.ID, err)
			http.Error(w, "failed to load twitch channels", http.StatusInternalServerError)
			return
		}

		var accessToken, ownID string
		for _, ident := range identities {
			if ident.Platform == auth.PlatformTwitch {
				accessToken = ident.AccessToken
				ownID = ident.PlatformUserID
				options = append(options, twitchChannelOption{ID: ident.PlatformUserID, Username: ident.PlatformUsername, Mine: true})
				break
			}
		}

		if accessToken != "" {
			modChannels, err := deps.Twitch.ModeratedChannels(accessToken, ownID)
			if err != nil {
				// Most commonly: this identity linked before
				// user:read:moderated_channels was added and hasn't
				// reconnected since. Not fatal — they still get their own
				// channel above.
				log.Printf("server: list moderated twitch channels for user %s: %v", u.ID, err)
			} else {
				for _, ch := range modChannels {
					options = append(options, twitchChannelOption{ID: ch.BroadcasterID, Username: ch.BroadcasterLogin, Mine: false})
				}
			}
		}

		writeJSON(w, http.StatusOK, options)
	}
}

func handleTwitchNotification(deps Deps, messageID string, body []byte) {
	if messageID != "" && deps.Twitch.SeenBefore(messageID) {
		return // Twitch redelivery of a notification we already applied
	}

	var envelope struct {
		Subscription struct {
			Type string `json:"type"`
		} `json:"subscription"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		log.Printf("twitch: parse notification envelope: %v", err)
		return
	}

	if envelope.Subscription.Type == "channel.chat.message" {
		handleTwitchChatCommand(deps, body)
		return
	}

	parsed, ok, err := twitch.ParseNotification(envelope.Subscription.Type, body)
	if err != nil {
		log.Printf("twitch: parse %s notification: %v", envelope.Subscription.Type, err)
		return
	}
	if !ok {
		return
	}

	for _, t := range deps.Manager.RunningByTwitchChannel(parsed.BroadcasterUserID) {
		rules, err := t.RewardRules()
		if err != nil {
			log.Printf("twitch: load reward rules for timer %s: %v", t.ID(), err)
			continue
		}
		moneyRules, err := t.MoneyRules()
		if err != nil {
			log.Printf("twitch: load money rules for timer %s: %v", t.ID(), err)
			continue
		}

		secondsAdded := parsed.Seconds(rules[parsed.Item][subathon.PlatformTwitch])
		moneyAdded := parsed.Money(moneyRules[parsed.Item][subathon.PlatformTwitch])
		if secondsAdded <= 0 && moneyAdded <= 0 {
			continue
		}

		err = t.AddEvent(subathon.Event{
			Platform:     subathon.PlatformTwitch,
			Type:         parsed.Type,
			Username:     parsed.Username,
			SecondsAdded: secondsAdded,
			MoneyAdded:   moneyAdded,
			Amount:       float64(parsed.Count),
		})
		if err != nil {
			log.Printf("twitch: add event to timer %s: %v", t.ID(), err)
			continue
		}
		deps.Hub.Broadcast(t.ID(), t.Snapshot())
	}
}

// handleTwitchChatCommand parses a channel.chat.message notification for a
// recognized "!timer pause"/"!timer unpause"/"!timer lock"/
// "!timer unlock"/"!timer hide"/"!timer unhide" command from that
// channel's own moderator/broadcaster (see twitch.ParseChatCommand — the
// permission check happens there, off the message's own badge data) and
// applies it to every timer watching that channel, running or not (unlike
// contribution events, "!timer unpause" specifically needs to reach a
// *stopped* timer).
func handleTwitchChatCommand(deps Deps, body []byte) {
	cmd, ok, err := twitch.ParseChatCommand(body)
	if err != nil {
		log.Printf("twitch: parse chat command: %v", err)
		return
	}
	if !ok {
		return
	}

	for _, t := range deps.Manager.TimersByTwitchChannel(cmd.BroadcasterUserID) {
		if err := applyChatCommand(t, cmd.Name); err != nil {
			log.Printf("twitch: run !%s (from %s) on timer %s: %v", cmd.Name, cmd.Username, t.ID(), err)
			continue
		}
		deps.Hub.Broadcast(t.ID(), t.Snapshot())
	}
}

// applyChatCommand runs one of chatCommandNames against t. Unknown names
// (shouldn't happen — ParseChatCommand already filters them) are a no-op.
func applyChatCommand(t *subathon.Timer, name string) error {
	switch name {
	case "pause":
		return t.Stop()
	case "unpause":
		return t.Resume()
	case "lock":
		return t.SetLocked(true)
	case "unlock":
		return t.SetLocked(false)
	case "hide":
		return t.SetHidden(true)
	case "unhide":
		return t.SetHidden(false)
	default:
		return nil
	}
}
