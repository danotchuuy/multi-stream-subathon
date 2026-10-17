package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
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

// handleHHCommand implements "!timer hh <duration> [multiplier]" (e.g.
// "!timer hh 30m" or "!timer hh 30m 3x") — parses the duration and
// optional multiplier (see parseHHArgs; defaults to
// subathon.DefaultBoostMultiplier) and hands off to startHappyHour, the
// same entry point the dashboard's "Start Happy Hour" control uses (see
// handleStartHappyHour), just reached from chat instead of an HTTP request.
func handleHHCommand(deps Deps, t *subathon.Timer, cmd twitch.ChatCommand) {
	d, multiplier, err := parseHHArgs(cmd.Arg)
	if err != nil {
		log.Printf("twitch: !timer hh (from %s) on timer %s: %v", cmd.Username, t.ID(), err)
		return
	}

	if err := startHappyHour(deps, t, cmd.BroadcasterUserID, d, multiplier); err != nil {
		log.Printf("twitch: start time boost (from %s) on timer %s: %v", cmd.Username, t.ID(), err)
	}
}

// parseHHArgs parses "!timer hh"'s arguments: a required positive
// duration (e.g. "30m") followed by an optional multiplier greater than 1,
// written with or without a trailing "x" (e.g. "3", "3x", "1.5x"). A
// missing multiplier is returned as subathon.DefaultBoostMultiplier.
func parseHHArgs(arg string) (d time.Duration, multiplier float64, err error) {
	fields := strings.Fields(arg)
	if len(fields) == 0 || len(fields) > 2 {
		return 0, 0, fmt.Errorf("invalid arguments %q", arg)
	}
	d, err = time.ParseDuration(fields[0])
	if err != nil || d <= 0 {
		return 0, 0, fmt.Errorf("invalid duration %q", fields[0])
	}

	multiplier = subathon.DefaultBoostMultiplier
	if len(fields) == 2 {
		raw := strings.TrimSuffix(strings.ToLower(fields[1]), "x")
		multiplier, err = strconv.ParseFloat(raw, 64)
		if err != nil || multiplier <= 1 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
			return 0, 0, fmt.Errorf("invalid multiplier %q", fields[1])
		}
	}
	return d, multiplier, nil
}

// formatMultiplier renders a boost multiplier for chat, e.g. "2x" or
// "1.5x".
func formatMultiplier(m float64) string {
	return strconv.FormatFloat(m, 'f', -1, 64) + "x"
}

// startHappyHour starts a time boost on t multiplying contributions by
// multiplier (see subathon.Timer.StartTimeBoost, capped at maxHHDuration
// and subathon.MaxBoostMultiplier), announces it in
// broadcasterID's chat using the timer owner's own Twitch account (see
// announceAsOwner) and on the timer's Discord webhook, if set (see
// postDiscord), and schedules matching "it's over" announcements for when
// it expires (see announceTimeBoostEnd) — shared by the "!timer hh
// <duration>" chat command (handleHHCommand), the dashboard's "Start
// Happy Hour" control (handleStartHappyHour), and scheduled happy hours
// (RunHappyHourScheduler). broadcasterID empty (e.g. the timer has no
// Twitch channel configured) skips the Twitch announcements but still
// starts the boost itself.
func startHappyHour(deps Deps, t *subathon.Timer, broadcasterID string, d time.Duration, multiplier float64) error {
	if d > maxHHDuration {
		d = maxHHDuration
	}

	if err := t.StartTimeBoost(d, multiplier); err != nil {
		return fmt.Errorf("start time boost: %w", err)
	}
	endsAt := t.TimeBoostEndsAt()
	deps.Hub.Broadcast(t.ID(), t.Snapshot())

	m := t.TimeBoostMultiplier()
	postDiscordNotification(deps, t, notifyStart, startVars(t.Name(), m, d, endsAt))

	announceTwitch(deps, t, broadcasterID, notifyStart, twitchStartText(t.TwitchAnnouncements(), t.Name(), m, d))

	// This server process is what's tracking the expiry, so the
	// announcement is scheduled here rather than resumed on restart — a
	// boost still active in the database when the server restarts won't
	// get its "it's over" announcement, same best-effort, in-memory-only
	// treatment as e.g. oauth.StateStore's in-flight flows.
	time.AfterFunc(d, func() { announceTimeBoostEnd(deps, t, broadcasterID, endsAt) })
	return nil
}

type startHappyHourRequest struct {
	// DurationSeconds is how long to boost time contributions for, e.g.
	// 1800 for 30 minutes.
	DurationSeconds int `json:"durationSeconds"`
	// Multiplier is how much to multiply time contributions by, e.g. 3
	// for triple time. Omitted (0) means subathon.DefaultBoostMultiplier.
	Multiplier float64 `json:"multiplier,omitempty"`
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
		if req.Multiplier == 0 {
			req.Multiplier = subathon.DefaultBoostMultiplier
		}
		if req.Multiplier <= 1 || req.Multiplier > subathon.MaxBoostMultiplier {
			http.Error(w, fmt.Sprintf("multiplier must be greater than 1 and at most %g", subathon.MaxBoostMultiplier), http.StatusBadRequest)
			return
		}

		broadcasterID, _ := t.TwitchChannel()
		if err := startHappyHour(deps, t, broadcasterID, time.Duration(req.DurationSeconds)*time.Second, req.Multiplier); err != nil {
			log.Printf("server: start happy hour for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to start happy hour", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

// handleEndHappyHour is the dashboard's "End Happy Hour" control: ends
// the active happy hour now — however it was started — and announces
// that it's over, exactly as if it had run its course. Its originally
// scheduled end announcement is then skipped (see announceTimeBoostEnd).
// 409 if none is active.
func handleEndHappyHour(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		if err := t.EndTimeBoost(); err != nil {
			if errors.Is(err, subathon.ErrNoTimeBoost) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			log.Printf("server: end happy hour for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to end happy hour", http.StatusInternalServerError)
			return
		}

		broadcasterID, _ := t.TwitchChannel()
		announceHappyHourEnded(deps, t, broadcasterID)
		writeJSON(w, http.StatusOK, t.Snapshot())
	}
}

// announceTimeBoostEnd announces that a happy hour has ended — in
// broadcasterID's Twitch chat (skipped if empty) and on the timer's
// Discord webhook, if set — but
// only if expectedEndsAt still matches t's current boost expiry (see
// Timer.TimeBoostEndsAt) — a later "!timer hh" command before this one's
// timer fired would have moved that expiry forward, meaning *that*
// command's own scheduled call (not this stale one) owns announcing the
// real end.
func announceTimeBoostEnd(deps Deps, t *subathon.Timer, broadcasterID string, expectedEndsAt time.Time) {
	if !t.TimeBoostEndsAt().Equal(expectedEndsAt) {
		return
	}
	announceHappyHourEnded(deps, t, broadcasterID)
}

// announceHappyHourEnded pushes the post-happy-hour snapshot and posts
// the "it's over" message to Discord and, if broadcasterID isn't empty,
// Twitch chat — whether it ran its course (announceTimeBoostEnd) or was
// ended early (handleEndHappyHour).
func announceHappyHourEnded(deps Deps, t *subathon.Timer, broadcasterID string) {
	deps.Hub.Broadcast(t.ID(), t.Snapshot())

	added, bonus := t.HappyHourTally()
	multiplier := t.TimeBoostMultiplier()
	postDiscordNotification(deps, t, notifyEnd, endVars(t.Name(), multiplier, added, bonus))
	announceTwitch(deps, t, broadcasterID, notifyEnd, twitchEndText(t.TwitchAnnouncements(), t.Name(), multiplier, added, bonus))
}

// twitchHappyHourEndMessage is the default Twitch chat "it's over"
// announcement (see twitchEndText):
// how much time the happy hour added (see Timer.HappyHourTally) and a
// thank-you. The time-added sentence is left out when it added nothing,
// rather than announcing "0 seconds".
func twitchHappyHourEndMessage(multiplier float64, added, bonus time.Duration) string {
	msg := "⏱️ Happy Hour has ended — time contributions are back to normal."
	if added > 0 {
		msg += fmt.Sprintf(" It added %s to the timer", humanDuration(added))
		if bonus > 0 {
			msg += fmt.Sprintf(" (%s of that from the %s boost)", humanDuration(bonus), formatMultiplier(multiplier))
		}
		msg += "."
	}
	return msg + " Thank you so much for all the support! 💜"
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
