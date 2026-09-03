// Package kick subscribes to Kick's public webhook events for a
// broadcaster's subs, gift subs, and Kicks gifts, and translates their
// webhook notifications into subathon.Event using a timer's reward rules.
//
// The request/response shapes and signing scheme here are cross-checked
// against docs.kick.com and against another local project
// (../multi-stream-moderation) that exercises the same API against a live
// Kick app in production, but neither this package nor that check has run
// against a live Kick app itself — verify against current docs before
// relying on this in production, particularly if Kick's API has moved on
// since.
package kick

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

const (
	authURL         = "https://id.kick.com/oauth/token"
	channelsURL     = "https://api.kick.com/public/v1/channels"
	subscriptionURL = "https://api.kick.com/public/v1/events/subscriptions"
	publicKeyURL    = "https://api.kick.com/public/v1/public-key"
)

// subscriptionEvents is every webhook event this package keeps active per
// broadcaster. Bumping a version here also needs the payload parsing in
// ParseNotification to match.
var subscriptionEvents = []struct {
	name    string
	version int
}{
	{"channel.subscription.new", 1},
	{"channel.subscription.renewal", 1},
	{"channel.subscription.gifts", 1},
	{"kicks.gifted", 1},
}

// Config holds the credentials needed to look up channels and manage
// webhook event subscriptions.
type Config struct {
	ClientID     string
	ClientSecret string
}

// Client manages Kick webhook subscriptions and verifies/parses their
// notifications.
type Client struct {
	cfg Config
	hc  *http.Client

	tokenMu     sync.Mutex
	appToken    string
	tokenExpiry time.Time

	pubKeyMu sync.Mutex
	pubKey   *rsa.PublicKey

	seenMu sync.Mutex
	seen   map[string]time.Time // webhook message ID -> when first seen
}

// New creates a Client from cfg.
func New(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		hc:   &http.Client{Timeout: 10 * time.Second},
		seen: make(map[string]time.Time),
	}
}

// appAccessToken returns a cached app access token (client_credentials
// grant), fetching a new one if missing or near expiry. Used for every
// call in this package, including creating event subscriptions for a
// broadcaster who has never authorized this app themselves — Kick's
// events API accepts an explicit broadcaster_user_id in the subscription
// request body from an app-level token for any channel. (A signed-in
// user's own token would work too, but Kick silently ignores whatever
// broadcaster_user_id is sent and subscribes to that user's own channel
// instead — so the app token is the only way to target an arbitrary
// channel.) https://docs.kick.com/events/subscribe-to-events
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
	c.tokenExpiry = time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second - time.Minute)
	return c.appToken, nil
}

// LookupBroadcasterID resolves a Kick channel slug (e.g. from a "which
// channel should this timer watch" form field) to its stable broadcaster
// user ID and display name.
func (c *Client) LookupBroadcasterID(slug string) (id, displayName string, err error) {
	token, err := c.appAccessToken()
	if err != nil {
		return "", "", fmt.Errorf("get app access token: %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, channelsURL+"?slug="+url.QueryEscape(slug), nil)
	if err != nil {
		return "", "", fmt.Errorf("build channel lookup request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("channel lookup request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("channel lookup failed (%d): %s", resp.StatusCode, body)
	}

	var parsed struct {
		Data []struct {
			BroadcasterUserID int    `json:"broadcaster_user_id"`
			Slug              string `json:"slug"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", fmt.Errorf("parse channel lookup response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return "", "", fmt.Errorf("no Kick channel found for %q", slug)
	}
	return strconv.Itoa(parsed.Data[0].BroadcasterUserID), parsed.Data[0].Slug, nil
}

// kickSubscription is one entry from GET /events/subscriptions.
type kickSubscription struct {
	Event             string `json:"event"`
	BroadcasterUserID int    `json:"broadcaster_user_id"`
}

// listSubscriptions returns every webhook subscription currently
// registered for this app, across every broadcaster.
func (c *Client) listSubscriptions(token string) ([]kickSubscription, error) {
	req, err := http.NewRequest(http.MethodGet, subscriptionURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build list-subscriptions request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list-subscriptions request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list-subscriptions failed (%d): %s", resp.StatusCode, body)
	}

	var parsed struct {
		Data []kickSubscription `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse list-subscriptions response: %w", err)
	}
	return parsed.Data, nil
}

// EnsureBroadcasterSubscriptions creates whichever of subscriptionEvents
// broadcasterUserID doesn't already have an active webhook subscription
// for, using the app-level token (see appAccessToken) — works for any
// channel, not just ones that have authorized this app. Lists existing
// subscriptions first rather than just POSTing and treating a conflict as
// success: unlike Twitch's EventSub, Kick's subscribe endpoint doesn't
// reject a duplicate, it just creates a second one, and Kick delivers the
// event once per active subscription — so an un-deduplicated retry would
// double (or triple, ...) every event for that channel.
func (c *Client) EnsureBroadcasterSubscriptions(broadcasterUserID string) error {
	id, err := strconv.Atoi(broadcasterUserID)
	if err != nil {
		return fmt.Errorf("invalid broadcaster user id %q: %w", broadcasterUserID, err)
	}

	token, err := c.appAccessToken()
	if err != nil {
		return fmt.Errorf("get app access token: %w", err)
	}

	existing, err := c.listSubscriptions(token)
	if err != nil {
		return fmt.Errorf("list existing subscriptions: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, sub := range existing {
		if sub.BroadcasterUserID == id {
			have[sub.Event] = true
		}
	}

	var events []map[string]any
	for _, e := range subscriptionEvents {
		if !have[e.name] {
			events = append(events, map[string]any{"name": e.name, "version": e.version})
		}
	}
	if len(events) == 0 {
		return nil
	}

	body, err := json.Marshal(map[string]any{
		"broadcaster_user_id": id,
		"events":              events,
		"method":              "webhook",
	})
	if err != nil {
		return fmt.Errorf("marshal subscription request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, subscriptionURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build subscription request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("subscription request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("subscription request failed (%d): %s", resp.StatusCode, respBody)
	}
	return nil
}

// fetchPublicKey retrieves and caches Kick's webhook-signing public key.
func (c *Client) fetchPublicKey() (*rsa.PublicKey, error) {
	c.pubKeyMu.Lock()
	defer c.pubKeyMu.Unlock()

	if c.pubKey != nil {
		return c.pubKey, nil
	}

	resp, err := c.hc.Get(publicKeyURL)
	if err != nil {
		return nil, fmt.Errorf("fetch public key: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch public key failed (%d): %s", resp.StatusCode, body)
	}

	var parsed struct {
		Data struct {
			PublicKey string `json:"public_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse public key response: %w", err)
	}

	block, _ := pem.Decode([]byte(parsed.Data.PublicKey))
	if block == nil {
		return nil, fmt.Errorf("public key response was not valid PEM")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key was not RSA")
	}

	c.pubKey = rsaKey
	return c.pubKey, nil
}

// VerifyMessage checks an incoming webhook request's signature against
// Kick's published public key (asymmetric — Kick signs with its private
// key rather than a shared secret we configure). Per
// https://docs.kick.com/events/webhook-security, the signed message is
// "{messageID}.{timestamp}.{rawBody}" (dot-joined), RSA-PKCS1v15/SHA-256.
func (c *Client) VerifyMessage(header http.Header, body []byte) bool {
	id := header.Get("Kick-Event-Message-Id")
	timestamp := header.Get("Kick-Event-Message-Timestamp")
	sigB64 := header.Get("Kick-Event-Signature")
	if id == "" || timestamp == "" || sigB64 == "" {
		return false
	}

	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}

	pubKey, err := c.fetchPublicKey()
	if err != nil {
		return false
	}

	signed := id + "." + timestamp + "." + string(body)
	hash := sha256.Sum256([]byte(signed))
	if rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], sig) == nil {
		return true
	}

	// The key may have rotated since it was cached; refetch once and
	// retry before giving up.
	c.pubKeyMu.Lock()
	c.pubKey = nil
	c.pubKeyMu.Unlock()
	pubKey, err = c.fetchPublicKey()
	if err != nil {
		return false
	}
	return rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], sig) == nil
}

// SeenBefore reports whether messageID has already been processed
// (Kick may redeliver notifications), remembering it if not. Call Sweep
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

// Sweep discards remembered message IDs older than 15 minutes.
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

// ParsedEvent is one webhook notification translated into subathon terms,
// before reward rules (which vary per timer) turn it into seconds.
type ParsedEvent struct {
	BroadcasterUserID string
	Item              subathon.RewardItem
	Type              subathon.EventType
	Username          string
	// Count is how many of Item this notification represents: 1 for a
	// single (re)subscription, the number of subs gifted, or the number
	// of Kicks gifted.
	Count int
}

// Seconds computes how much time this event is worth given ratePerUnit —
// rules[Item][PlatformKick]. RewardBits100 is priced per 100 units,
// mirroring Twitch's bits and this reward item's own "per 100" naming,
// and rounded up (not truncated) so any nonzero Kicks gift is worth at
// least one second rather than silently vanishing — with a rate of e.g.
// 60s/100 Kicks, plain truncating division would floor anything under 2
// Kicks to 0, and the webhook handler drops zero-second events entirely
// rather than recording a no-op contribution.
func (p ParsedEvent) Seconds(ratePerUnit int) int {
	if p.Item == subathon.RewardBits100 {
		return (p.Count*ratePerUnit + 99) / 100
	}
	return p.Count * ratePerUnit
}

// Money computes how many dollars this event is worth given ratePerUnit —
// moneyRules[Item][PlatformKick]. Unlike Seconds, this isn't rounded up to
// a whole-unit minimum: dollars are naturally continuous, so a small
// fractional amount is a meaningful value in its own right, not an
// artifact of integer truncation to guard against.
func (p ParsedEvent) Money(ratePerUnit float64) float64 {
	if p.Item == subathon.RewardBits100 {
		return float64(p.Count) * ratePerUnit / 100
	}
	return float64(p.Count) * ratePerUnit
}

// ParseNotification decodes a webhook notification body into a
// ParsedEvent. ok is false for an event type we don't translate. Kick
// doesn't document subscription tiers the way Twitch does, so every
// (re)subscription is priced as RewardTier1Sub.
func ParseNotification(eventType string, body []byte) (ParsedEvent, bool, error) {
	switch eventType {
	case "channel.subscription.new", "channel.subscription.renewal":
		var e struct {
			Broadcaster struct {
				UserID int `json:"user_id"`
			} `json:"broadcaster"`
			Subscriber struct {
				Username string `json:"username"`
			} `json:"subscriber"`
		}
		if err := json.Unmarshal(body, &e); err != nil {
			return ParsedEvent{}, false, fmt.Errorf("parse %s event: %w", eventType, err)
		}
		typ := subathon.EventSub
		return ParsedEvent{
			BroadcasterUserID: strconv.Itoa(e.Broadcaster.UserID),
			Item:              subathon.RewardTier1Sub,
			Type:              typ,
			Username:          e.Subscriber.Username,
			Count:             1,
		}, true, nil

	case "channel.subscription.gifts":
		var e struct {
			Broadcaster struct {
				UserID int `json:"user_id"`
			} `json:"broadcaster"`
			Gifter struct {
				Username string `json:"username"`
			} `json:"gifter"`
			Giftees []struct {
				Username string `json:"username"`
			} `json:"giftees"`
		}
		if err := json.Unmarshal(body, &e); err != nil {
			return ParsedEvent{}, false, fmt.Errorf("parse channel.subscription.gifts event: %w", err)
		}
		username := e.Gifter.Username
		if username == "" {
			username = "anonymous"
		}
		return ParsedEvent{
			BroadcasterUserID: strconv.Itoa(e.Broadcaster.UserID),
			Item:              subathon.RewardGiftedSub,
			Type:              subathon.EventGiftedSub,
			Username:          username,
			Count:             len(e.Giftees),
		}, true, nil

	case "kicks.gifted":
		var e struct {
			Broadcaster struct {
				UserID int `json:"user_id"`
			} `json:"broadcaster"`
			Sender struct {
				Username string `json:"username"`
			} `json:"sender"`
			Gift struct {
				Amount int `json:"amount"`
			} `json:"gift"`
		}
		if err := json.Unmarshal(body, &e); err != nil {
			return ParsedEvent{}, false, fmt.Errorf("parse kicks.gifted event: %w", err)
		}
		username := e.Sender.Username
		if username == "" {
			username = "anonymous"
		}
		return ParsedEvent{
			BroadcasterUserID: strconv.Itoa(e.Broadcaster.UserID),
			Item:              subathon.RewardBits100,
			Type:              subathon.EventBits,
			Username:          username,
			Count:             e.Gift.Amount,
		}, true, nil

	default:
		return ParsedEvent{}, false, nil
	}
}
