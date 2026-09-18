package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

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
// listens to for "!timer pause"/"!timer play"/"!timer lock"/
// "!timer unlock"/"!timer hide"/"!timer show" commands from that
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

	// Twitch fires both channel.subscribe and channel.subscription.message
	// for a lapsed-then-resubscribed viewer who shares a message about
	// their (non-continuous) total months: channel.subscribe because it's
	// technically a new subscription period, channel.subscription.message
	// because they shared a resub message. Left alone, that credits the
	// same action twice — once as a sub, once as a resub. Since
	// channel.subscribe always arrives first, remember it and skip the
	// companion message that follows shortly after for the same viewer.
	switch parsed.Type {
	case subathon.EventSub:
		deps.Twitch.MarkSubscribed(parsed.BroadcasterUserID, parsed.Username)
	case subathon.EventResub:
		if deps.Twitch.RecentlySubscribed(parsed.BroadcasterUserID, parsed.Username) {
			return
		}
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
// recognized "!timer pause"/"!timer play"/"!timer lock"/
// "!timer unlock"/"!timer hide"/"!timer show"/"!timer hh <duration>"
// command from that channel's own moderator/broadcaster (see
// twitch.ParseChatCommand — the permission check happens there, off the
// message's own badge data) and applies it to every timer watching that
// channel, running or not (unlike contribution events, "!timer play"
// specifically needs to reach a *stopped* timer) — but never an *ended*
// one: Manager.TimersByTwitchChannel already excludes those, which is what
// makes "!timer ..." stop working once a timer's ended (see
// subathon.Timer.SetEnded).
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
		if cmd.Name == "hh" {
			handleHHCommand(deps, t, cmd)
			continue
		}
		if err := applyChatCommand(t, cmd.Name); err != nil {
			log.Printf("twitch: run !%s (from %s) on timer %s: %v", cmd.Name, cmd.Username, t.ID(), err)
			continue
		}
		deps.Hub.Broadcast(t.ID(), t.Snapshot())
	}
}

// applyChatCommand runs one of chatCommandNames' bare toggles against t —
// every command except "hh" (see handleHHCommand, which needs deps to send
// its chat announcement, not just the timer). Unknown names (shouldn't
// happen — ParseChatCommand already filters them) are a no-op.
func applyChatCommand(t *subathon.Timer, name string) error {
	switch name {
	case "pause":
		return t.Stop()
	case "play":
		return t.Resume()
	case "lock":
		return t.SetLocked(true)
	case "unlock":
		return t.SetLocked(false)
	case "hide":
		return t.SetHidden(true)
	case "show":
		return t.SetHidden(false)
	default:
		return nil
	}
}

// maxHHDuration caps how long a single "!timer hh <duration>" command can
// double contributions for, e.g. against a typo like "!timer hh 300h".
const maxHHDuration = 24 * time.Hour

// handleHHCommand implements "!timer hh <duration>" (e.g. "!timer hh
// 30m") — parses the duration and hands off to startHappyHour, the same
// entry point the dashboard's "Start Happy Hour" control uses (see
// handleStartHappyHour), just reached from chat instead of an HTTP request.
func handleHHCommand(deps Deps, t *subathon.Timer, cmd twitch.ChatCommand) {
	d, err := time.ParseDuration(cmd.Arg)
	if err != nil || d <= 0 {
		log.Printf("twitch: !timer hh (from %s) on timer %s: invalid duration %q", cmd.Username, t.ID(), cmd.Arg)
		return
	}

	if err := startHappyHour(deps, t, cmd.BroadcasterUserID, d); err != nil {
		log.Printf("twitch: start time boost (from %s) on timer %s: %v", cmd.Username, t.ID(), err)
	}
}

// startHappyHour starts a time-doubling boost on t (see subathon.Timer.
// StartTimeBoost, capped at maxHHDuration), announces it in
// broadcasterID's chat using the timer owner's own Twitch account (see
// announceAsOwner), and schedules a matching "it's over" announcement for
// when it expires (see announceTimeBoostEnd) — shared by the "!timer hh
// <duration>" chat command (handleHHCommand) and the dashboard's "Start
// Happy Hour" control (handleStartHappyHour). broadcasterID empty (e.g. the
// timer has no Twitch channel configured) skips both announcements but
// still starts the boost itself.
func startHappyHour(deps Deps, t *subathon.Timer, broadcasterID string, d time.Duration) error {
	if d > maxHHDuration {
		d = maxHHDuration
	}

	if err := t.StartTimeBoost(d); err != nil {
		return fmt.Errorf("start time boost: %w", err)
	}
	endsAt := t.TimeBoostEndsAt()
	deps.Hub.Broadcast(t.ID(), t.Snapshot())

	if broadcasterID == "" {
		return nil
	}

	minutes := int(d.Round(time.Minute) / time.Minute)
	startMessage := fmt.Sprintf("⏱️ Happy Hour! Time contributions are DOUBLED for the next %d minutes!", minutes)
	if err := announceAsOwner(deps, t, broadcasterID, startMessage); err != nil {
		log.Printf("twitch: announce time boost start on timer %s: %v", t.ID(), err)
	}

	// This server process is what's tracking the expiry, so the
	// announcement is scheduled here rather than resumed on restart — a
	// boost still active in the database when the server restarts won't
	// get its "it's over" announcement, same best-effort, in-memory-only
	// treatment as e.g. oauth.StateStore's in-flight flows.
	time.AfterFunc(d, func() { announceTimeBoostEnd(deps, t, broadcasterID, endsAt) })
	return nil
}

type startHappyHourRequest struct {
	// DurationSeconds is how long to double time contributions for, e.g.
	// 1800 for 30 minutes.
	DurationSeconds int `json:"durationSeconds"`
}

// handleStartHappyHour is the dashboard's "Start Happy Hour" control —
// startHappyHour reached over HTTP instead of a "!timer hh <duration>"
// chat command, for a streamer/moderator who'd rather click a button than
// type in chat. Announces on this timer's configured Twitch channel (see
// Timer.SetTwitchChannel), if any; a timer with no Twitch channel
// configured still gets the boost, just with no chat announcement.
func handleStartHappyHour(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req startHappyHourRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.DurationSeconds <= 0 {
			http.Error(w, "durationSeconds must be positive", http.StatusBadRequest)
			return
		}

		broadcasterID, _ := t.TwitchChannel()
		if err := startHappyHour(deps, t, broadcasterID, time.Duration(req.DurationSeconds)*time.Second); err != nil {
			log.Printf("server: start happy hour for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to start happy hour", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

// announceTimeBoostEnd announces that a "!timer hh" boost has ended, but
// only if expectedEndsAt still matches t's current boost expiry (see
// Timer.TimeBoostEndsAt) — a later "!timer hh" command before this one's
// timer fired would have moved that expiry forward, meaning *that*
// command's own scheduled call (not this stale one) owns announcing the
// real end.
func announceTimeBoostEnd(deps Deps, t *subathon.Timer, broadcasterID string, expectedEndsAt time.Time) {
	if !t.TimeBoostEndsAt().Equal(expectedEndsAt) {
		return
	}

	deps.Hub.Broadcast(t.ID(), t.Snapshot())

	message := "⏱️ Happy Hour has ended — time contributions are back to normal."
	if err := announceAsOwner(deps, t, broadcasterID, message); err != nil {
		log.Printf("twitch: announce time boost end on timer %s: %v", t.ID(), err)
	}
}

// tokenRefreshMargin refreshes an about-to-expire Twitch access token this
// early rather than racing its exact expiry — same margin youtube.Poller
// uses for the same reason.
const tokenRefreshMargin = time.Minute

// announceAsOwner posts a chat announcement to broadcasterID's channel
// (via twitch.Client.SendChatAnnouncement) using t's owner's own linked
// Twitch identity — "the streamer's account" a "!timer hh" command's
// start/end announcements should post as (see handleHHCommand/
// announceTimeBoostEnd) — refreshing that identity's access token first
// if it's expired or about to be (see oauth.Provider.Refresh, same
// pattern as youtube.Poller.run). A no-op if Twitch integration is
// disabled or the owner has no linked Twitch identity.
func announceAsOwner(deps Deps, t *subathon.Timer, broadcasterID, message string) error {
	if deps.Twitch == nil {
		return nil
	}

	identities, err := deps.Auth.Identities(t.UserID())
	if err != nil {
		return fmt.Errorf("list identities for timer %s owner: %w", t.ID(), err)
	}
	var ident *auth.Identity
	for i := range identities {
		if identities[i].Platform == auth.PlatformTwitch {
			ident = &identities[i]
			break
		}
	}
	if ident == nil {
		return fmt.Errorf("timer %s owner has no linked Twitch identity to announce as", t.ID())
	}

	token := ident.AccessToken
	if !ident.TokenExpiresAt.IsZero() && time.Now().After(ident.TokenExpiresAt.Add(-tokenRefreshMargin)) {
		provider := deps.OAuthProviders["twitch"]
		if provider == nil {
			return fmt.Errorf("twitch oauth provider not configured")
		}
		newToken, newRefreshToken, newExpiresAt, err := provider.Refresh(context.Background(), ident.RefreshToken)
		if err != nil {
			return fmt.Errorf("refresh twitch token for %s: %w", ident.PlatformUsername, err)
		}
		if err := deps.Auth.RefreshIdentityTokens(ident.ID, newToken, newRefreshToken, newExpiresAt); err != nil {
			log.Printf("twitch: save refreshed token for %s: %v", ident.PlatformUsername, err)
		}
		token = newToken
	}

	return deps.Twitch.SendChatAnnouncement(broadcasterID, ident.PlatformUserID, token, message)
}
