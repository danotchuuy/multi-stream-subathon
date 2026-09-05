// Package twitch subscribes to Twitch EventSub (via webhook transport) for
// a broadcaster's subs, resubs, gift subs, cheers, and moderator chat
// commands, translating their webhook notifications into subathon.Event
// (or, for chat commands, a direct timer action) using a timer's reward
// rules.
//
// NOTE on an alternative considered and deliberately not taken for subs/
// gift subs specifically: they could instead be read off
// channel.chat.notification (Twitch's catch-all chat-announcement event),
// which only needs the user:read:chat scope and works for any channel a
// signed-in user reads chat on, no broadcaster/moderator consent required
// — see ../../multi-stream-moderation's internal/twitch package, which
// does exactly this. It wasn't adopted here because *that* project reads
// it over a persistent WebSocket connection (open only while a browser
// tab is watching), whereas this server would need to hold one open
// indefinitely per watched channel, using some account's access token
// refreshed on an ongoing basis — token refresh doesn't exist anywhere in
// this codebase today. channel.chat.message (used here for chat
// commands, see EnsureChatCommandSubscription) sidesteps this: unlike
// channel.subscribe/channel.subscription.gift/channel.cheer, Twitch
// accepts it via webhook transport with an app access token as long as
// the "reading" identity is the channel's broadcaster or one of its
// moderators (confirmed against Twitch's docs) — no persistent
// connection needed. Revisit chat.notification for subs/gifts if the
// broadcaster-only consent requirement on those becomes a real blocker;
// cheers stay on channel.cheer either way, since bits:read has no such
// workaround.
package twitch

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

const (
	authURL                   = "https://id.twitch.tv/oauth2/token"
	helixURL                  = "https://api.twitch.tv/helix/eventsub/subscriptions"
	helixUsersURL             = "https://api.twitch.tv/helix/users"
	helixModeratedChannelsURL = "https://api.twitch.tv/helix/moderation/channels"
	transport                 = "webhook"
)

// subscriptionTypes is every EventSub subscription this package keeps
// active per broadcaster. Bumping a version here also needs the payload
// parsing in ParseNotification to match.
var subscriptionTypes = []struct{ typ, version string }{
	{"channel.subscribe", "1"},
	{"channel.subscription.message", "1"},
	{"channel.subscription.gift", "1"},
	{"channel.cheer", "1"},
}

// Config holds the credentials/settings needed to run EventSub
// subscriptions for connected broadcasters.
type Config struct {
	ClientID     string
	ClientSecret string

	// WebhookSecret signs/verifies notification payloads (10-100 chars,
	// per Twitch's requirements).
	WebhookSecret string

	// CallbackURL is the public https:// URL Twitch will POST
	// notifications to, e.g. "https://sub.example.com/webhooks/twitch".
	CallbackURL string
}

// Client manages EventSub subscriptions and verifies/parses their webhook
// notifications.
type Client struct {
	cfg Config
	hc  *http.Client

	tokenMu     sync.Mutex
	appToken    string
	tokenExpiry time.Time

	seenMu sync.Mutex
	seen   map[string]time.Time // EventSub message ID -> when first seen
}

// New creates a Client from cfg. cfg.WebhookSecret must be set; callers
// should treat an empty secret as "Twitch integration disabled" and not
// construct a Client at all.
func New(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		hc:   &http.Client{Timeout: 10 * time.Second},
		seen: make(map[string]time.Time),
	}
}

// appAccessToken returns a cached app access token (client_credentials
// grant), fetching a new one if missing or near expiry.
func (c *Client) appAccessToken() (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.appToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.appToken, nil
	}

	form := strings.NewReader(fmt.Sprintf(
		"client_id=%s&client_secret=%s&grant_type=client_credentials",
		c.cfg.ClientID, c.cfg.ClientSecret,
	))
	req, err := http.NewRequest(http.MethodPost, authURL, form)
	if err != nil {
		return "", fmt.Errorf("build app token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("app token request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("app token request failed (%d): %s", resp.StatusCode, body)
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("parse app token response: %w", err)
	}

	c.appToken = tok.AccessToken
	// Refresh a bit early rather than racing the exact expiry.
	c.tokenExpiry = time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second - time.Minute)
	return c.appToken, nil
}

// EnsureBroadcasterSubscriptions creates (or confirms already-existing)
// EventSub subscriptions for broadcasterUserID's subs, gift subs, and
// cheers. Safe to call repeatedly, e.g. every time that broadcaster
// completes the Twitch OAuth flow. Attempts every subscription type even
// if an earlier one fails — e.g. bits:read being ungranted shouldn't stop
// channel:read:subscriptions-only subs/gifts from getting subscribed, or
// vice versa — and returns a combined error naming every type that
// failed, if any.
func (c *Client) EnsureBroadcasterSubscriptions(broadcasterUserID string) error {
	token, err := c.appAccessToken()
	if err != nil {
		return fmt.Errorf("get app access token: %w", err)
	}

	var errs []error
	for _, st := range subscriptionTypes {
		condition := map[string]string{"broadcaster_user_id": broadcasterUserID}
		if err := c.createSubscription(token, st.typ, st.version, condition); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// EnsureChatCommandSubscription creates (or confirms) a
// channel.chat.message webhook subscription for broadcasterUserID, read
// as readingUserID. Unlike EnsureBroadcasterSubscriptions' subscription
// types, Twitch accepts this via app access token as long as readingUserID
// is broadcasterUserID's own broadcaster or one of their moderators (see
// the package doc comment) and has granted user:read:chat/user:bot to
// this app — readingUserID doesn't need to be broadcasterUserID itself,
// so this is what lets a timer watch chat on a channel its owner
// moderates rather than broadcasts.
func (c *Client) EnsureChatCommandSubscription(broadcasterUserID, readingUserID string) error {
	token, err := c.appAccessToken()
	if err != nil {
		return fmt.Errorf("get app access token: %w", err)
	}

	condition := map[string]string{
		"broadcaster_user_id": broadcasterUserID,
		"user_id":             readingUserID,
	}
	return c.createSubscription(token, "channel.chat.message", "1", condition)
}

// createSubscription POSTs a single EventSub webhook subscription
// request. 202 Accepted (created) and 409 Conflict (an equivalent
// subscription already exists) both count as success.
func (c *Client) createSubscription(token, subType, version string, condition map[string]string) error {
	body, err := json.Marshal(map[string]any{
		"type":      subType,
		"version":   version,
		"condition": condition,
		"transport": map[string]string{
			"method":   transport,
			"callback": c.cfg.CallbackURL,
			"secret":   c.cfg.WebhookSecret,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal %s subscription: %w", subType, err)
	}

	req, err := http.NewRequest(http.MethodPost, helixURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s subscription request: %w", subType, err)
	}
	req.Header.Set("Client-Id", c.cfg.ClientID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s subscription request: %w", subType, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusConflict {
		return fmt.Errorf("%s subscription failed (%d): %s", subType, resp.StatusCode, respBody)
	}
	return nil
}

// LookupBroadcasterID resolves a Twitch login name (e.g. from a "which
// channel should this timer watch" form field) to its stable user ID and
// canonical login. Returns the lowercase login, not Twitch's cased
// display_name, so the value a timer ends up storing matches — byte for
// byte — the same login string ModeratedChannels/the account's own
// identity use elsewhere (Twitch's display_name can differ in case from
// login, e.g. "DanotChuuy" vs "danotchuuy"); comparing those two
// differently-cased forms is what made the channel picker wrongly claim
// an already-selected channel was "no longer available". This only needs
// an app access token — the channel doesn't need to have authorized this
// app to be looked up, only for its events to actually be subscribable
// afterward.
func (c *Client) LookupBroadcasterID(login string) (id, canonicalLogin string, err error) {
	token, err := c.appAccessToken()
	if err != nil {
		return "", "", fmt.Errorf("get app access token: %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, helixUsersURL+"?login="+url.QueryEscape(login), nil)
	if err != nil {
		return "", "", fmt.Errorf("build user lookup request: %w", err)
	}
	req.Header.Set("Client-Id", c.cfg.ClientID)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("user lookup request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("user lookup failed (%d): %s", resp.StatusCode, body)
	}

	var parsed struct {
		Data []struct {
			ID    string `json:"id"`
			Login string `json:"login"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", fmt.Errorf("parse user lookup response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return "", "", fmt.Errorf("no Twitch channel found for %q", login)
	}
	return parsed.Data[0].ID, parsed.Data[0].Login, nil
}

// ModeratedChannel is one channel a user moderates, as returned by
// ModeratedChannels.
type ModeratedChannel struct {
	BroadcasterID    string
	BroadcasterLogin string
}

// ModeratedChannels lists every channel userID moderates, using their own
// user access token (requires the user:read:moderated_channels scope —
// see oauth.NewTwitch). Twitch's channel:read:subscriptions/bits:read
// authorization requirement (see EnsureBroadcasterSubscriptions) is
// satisfied by either the broadcaster or one of their moderators granting
// it, so retrying subscription creation for each of these after a
// moderator (not just a broadcaster) completes the Twitch OAuth flow can
// unlock a channel whose own broadcaster has never signed into this app
// at all — and listing them lets a UI offer only channels a pick is
// actually going to work for, rather than free text. Only the first 100
// moderated channels are considered — Twitch paginates beyond that, but
// this app doesn't chase further pages.
func (c *Client) ModeratedChannels(userAccessToken, userID string) ([]ModeratedChannel, error) {
	q := url.Values{"user_id": {userID}, "first": {"100"}}
	req, err := http.NewRequest(http.MethodGet, helixModeratedChannelsURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build moderated-channels request: %w", err)
	}
	req.Header.Set("Client-Id", c.cfg.ClientID)
	req.Header.Set("Authorization", "Bearer "+userAccessToken)

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("moderated-channels request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("moderated-channels request failed (%d): %s", resp.StatusCode, body)
	}

	var parsed struct {
		Data []struct {
			BroadcasterID    string `json:"broadcaster_id"`
			BroadcasterLogin string `json:"broadcaster_login"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse moderated-channels response: %w", err)
	}

	channels := make([]ModeratedChannel, len(parsed.Data))
	for i, d := range parsed.Data {
		channels[i] = ModeratedChannel{BroadcasterID: d.BroadcasterID, BroadcasterLogin: d.BroadcasterLogin}
	}
	return channels, nil
}

// VerifyMessage checks an incoming webhook request's HMAC signature against
// WebhookSecret, per Twitch's EventSub signing scheme.
func (c *Client) VerifyMessage(header http.Header, body []byte) bool {
	id := header.Get("Twitch-Eventsub-Message-Id")
	timestamp := header.Get("Twitch-Eventsub-Message-Timestamp")
	sig := header.Get("Twitch-Eventsub-Message-Signature")
	if id == "" || timestamp == "" || sig == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(c.cfg.WebhookSecret))
	mac.Write([]byte(id))
	mac.Write([]byte(timestamp))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(sig))
}

// SeenBefore reports whether messageID has already been processed
// (Twitch may redeliver notifications), remembering it if not. Call Sweep
// periodically to bound memory from an ever-growing set.
func (c *Client) SeenBefore(messageID string) bool {
	c.seenMu.Lock()
	defer c.seenMu.Unlock()

	if _, ok := c.seen[messageID]; ok {
		return true
	}
	c.seen[messageID] = time.Now()
	return false
}

// Sweep discards remembered message IDs older than 15 minutes, comfortably
// past Twitch's redelivery window.
func (c *Client) Sweep() {
	c.seenMu.Lock()
	defer c.seenMu.Unlock()

	cutoff := time.Now().Add(-15 * time.Minute)
	for id, seenAt := range c.seen {
		if seenAt.Before(cutoff) {
			delete(c.seen, id)
		}
	}
}

// ParsedEvent is one EventSub notification translated into subathon terms,
// before reward rules (which vary per timer) turn it into seconds.
type ParsedEvent struct {
	BroadcasterUserID string
	Item              subathon.RewardItem
	Type              subathon.EventType
	Username          string
	// Count is how many of Item this notification represents: 1 for a
	// single sub, the number of subs gifted, or the number of bits
	// cheered. Seconds owed is Count * rules[Item][twitch] for
	// everything except bits, which are priced per 100.
	Count int
}

// Seconds computes how much time this event is worth given ratePerUnit —
// rules[Item][PlatformTwitch]. RewardBits100 is priced per 100 bits and
// rounded up (not truncated) so any nonzero cheer is worth at least one
// second rather than silently vanishing — with a rate of e.g. 60s/100
// bits, plain truncating division would floor anything under 2 bits to
// 0, and the webhook handler drops zero-second events entirely rather
// than recording a no-op contribution.
func (p ParsedEvent) Seconds(ratePerUnit int) int {
	if p.Item == subathon.RewardBits100 {
		return (p.Count*ratePerUnit + 99) / 100
	}
	return p.Count * ratePerUnit
}

// Money computes how many dollars this event is worth given ratePerUnit —
// moneyRules[Item][PlatformTwitch]. Unlike Seconds, this isn't rounded up
// to a whole-unit minimum: dollars are naturally continuous, so a small
// fractional amount (e.g. $0.006 for 1 bit at $0.01/100 bits) is a
// meaningful value in its own right, not an artifact of integer
// truncation to guard against.
func (p ParsedEvent) Money(ratePerUnit float64) float64 {
	if p.Item == subathon.RewardBits100 {
		return float64(p.Count) * ratePerUnit / 100
	}
	return float64(p.Count) * ratePerUnit
}

// ParseNotification decodes an EventSub "notification" message body into a
// ParsedEvent. ok is false for a subscription type we don't translate, or
// one that shouldn't add time on its own (e.g. a gifted sub's matching
// channel.subscribe, whose time is credited via channel.subscription.gift
// instead).
func ParseNotification(subscriptionType string, body []byte) (ParsedEvent, bool, error) {
	var envelope struct {
		Event json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ParsedEvent{}, false, fmt.Errorf("parse notification envelope: %w", err)
	}

	switch subscriptionType {
	case "channel.subscribe":
		var e struct {
			UserName          string `json:"user_name"`
			BroadcasterUserID string `json:"broadcaster_user_id"`
			Tier              string `json:"tier"`
			IsGift            bool   `json:"is_gift"`
		}
		if err := json.Unmarshal(envelope.Event, &e); err != nil {
			return ParsedEvent{}, false, fmt.Errorf("parse channel.subscribe event: %w", err)
		}
		if e.IsGift {
			// Credited to the gifter via channel.subscription.gift instead.
			return ParsedEvent{}, false, nil
		}
		return ParsedEvent{
			BroadcasterUserID: e.BroadcasterUserID,
			Item:              tierToItem(e.Tier),
			Type:              subathon.EventSub,
			Username:          e.UserName,
			Count:             1,
		}, true, nil

	case "channel.subscription.message":
		var e struct {
			UserName          string `json:"user_name"`
			BroadcasterUserID string `json:"broadcaster_user_id"`
			Tier              string `json:"tier"`
		}
		if err := json.Unmarshal(envelope.Event, &e); err != nil {
			return ParsedEvent{}, false, fmt.Errorf("parse channel.subscription.message event: %w", err)
		}
		return ParsedEvent{
			BroadcasterUserID: e.BroadcasterUserID,
			Item:              tierToItem(e.Tier),
			Type:              subathon.EventResub,
			Username:          e.UserName,
			Count:             1,
		}, true, nil

	case "channel.subscription.gift":
		var e struct {
			UserName          string `json:"user_name"`
			BroadcasterUserID string `json:"broadcaster_user_id"`
			Total             int    `json:"total"`
			Tier              string `json:"tier"`
			IsAnonymous       bool   `json:"is_anonymous"`
		}
		if err := json.Unmarshal(envelope.Event, &e); err != nil {
			return ParsedEvent{}, false, fmt.Errorf("parse channel.subscription.gift event: %w", err)
		}
		username := e.UserName
		if e.IsAnonymous || username == "" {
			username = "anonymous"
		}
		return ParsedEvent{
			BroadcasterUserID: e.BroadcasterUserID,
			Item:              giftedTierToItem(e.Tier),
			Type:              subathon.EventGiftedSub,
			Username:          username,
			Count:             e.Total,
		}, true, nil

	case "channel.cheer":
		var e struct {
			UserName          string `json:"user_name"`
			BroadcasterUserID string `json:"broadcaster_user_id"`
			IsAnonymous       bool   `json:"is_anonymous"`
			Bits              int    `json:"bits"`
		}
		if err := json.Unmarshal(envelope.Event, &e); err != nil {
			return ParsedEvent{}, false, fmt.Errorf("parse channel.cheer event: %w", err)
		}
		username := e.UserName
		if e.IsAnonymous || username == "" {
			username = "anonymous"
		}
		return ParsedEvent{
			BroadcasterUserID: e.BroadcasterUserID,
			Item:              subathon.RewardBits100,
			Type:              subathon.EventBits,
			Username:          username,
			Count:             e.Bits,
		}, true, nil

	default:
		return ParsedEvent{}, false, nil
	}
}

func tierToItem(tier string) subathon.RewardItem {
	switch tier {
	case "2000":
		return subathon.RewardTier2Sub
	case "3000":
		return subathon.RewardTier3Sub
	default: // "1000", including Prime subs
		return subathon.RewardTier1Sub
	}
}

// giftedTierToItem is tierToItem's equivalent for a channel.subscription.gift
// event's own "tier" field — gifted subs are tiered the same way regular
// subs are on Twitch.
func giftedTierToItem(tier string) subathon.RewardItem {
	switch tier {
	case "2000":
		return subathon.RewardGiftedTier2Sub
	case "3000":
		return subathon.RewardGiftedTier3Sub
	default: // "1000"
		return subathon.RewardGiftedTier1Sub
	}
}

// chatCommandNames is every word ParseChatCommand recognizes as a timer
// command when it follows a "!timer " prefix, e.g. "!timer play".
// Deliberately has no "end"/"unend" entry — ending a timer (see
// subathon.Timer.SetEnded) is dashboard-only, precisely so it can't be
// triggered (or, worse, undone) by anyone with chat access.
var chatCommandNames = map[string]bool{
	"pause":  true,
	"play":   true,
	"lock":   true,
	"unlock": true,
	"hide":   true,
	"show":   true,
}

// chatCommandPrefix is the first word a chat message must start with
// (case-insensitively) for ParseChatCommand to look at it at all, e.g.
// "!timer pause" — chosen so timer commands don't collide with any other
// bot's single-word "!pause"-style commands in the same chat.
const chatCommandPrefix = "!timer"

// ChatCommand is a recognized "!timer <command>" chat command from a
// channel's own broadcaster or one of their moderators, as decoded by
// ParseChatCommand.
type ChatCommand struct {
	BroadcasterUserID string
	Username          string
	// Name is one of chatCommandNames' keys, e.g. "pause" (no "!timer").
	Name string
}

// ParseChatCommand decodes a channel.chat.message notification body and,
// if it's a recognized "!timer <command>" command (see chatCommandPrefix
// and chatCommandNames) from a chatter with the channel's "moderator" or
// "broadcaster" badge, returns it. ok is false for an ordinary message,
// a message missing the "!timer" prefix, an unrecognized command after
// it, or a command from a chatter without that badge — authority comes
// from Twitch's own badge data on the message itself (issued per-channel,
// per-message), not a separate API call this package would otherwise
// need to make.
func ParseChatCommand(body []byte) (cmd ChatCommand, ok bool, err error) {
	var envelope struct {
		Event struct {
			BroadcasterUserID string `json:"broadcaster_user_id"`
			ChatterUserName   string `json:"chatter_user_name"`
			Message           struct {
				Text string `json:"text"`
			} `json:"message"`
			Badges []struct {
				SetID string `json:"set_id"`
			} `json:"badges"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ChatCommand{}, false, fmt.Errorf("parse channel.chat.message event: %w", err)
	}
	e := envelope.Event

	isModOrBroadcaster := false
	for _, b := range e.Badges {
		if b.SetID == "moderator" || b.SetID == "broadcaster" {
			isModOrBroadcaster = true
			break
		}
	}
	if !isModOrBroadcaster {
		return ChatCommand{}, false, nil
	}

	fields := strings.Fields(e.Message.Text)
	if len(fields) < 2 || !strings.EqualFold(fields[0], chatCommandPrefix) {
		return ChatCommand{}, false, nil
	}
	name := strings.ToLower(fields[1])
	if !chatCommandNames[name] {
		return ChatCommand{}, false, nil
	}

	return ChatCommand{
		BroadcasterUserID: e.BroadcasterUserID,
		Username:          e.ChatterUserName,
		Name:              name,
	}, true, nil
}
