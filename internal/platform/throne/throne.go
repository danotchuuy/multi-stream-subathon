// Package throne verifies and parses Throne (throne.com) webhook
// notifications — a wishlist/gifting platform for creators — translating a
// gift/contribution into a subathon.Event via a timer's donation reward/
// money rules (see subathon.RewardDonation), the same per-dollar rate
// StreamElements tips and the dashboard's manual "Add donation" form use.
//
// Unlike Twitch/Kick, Throne needs no OAuth app credential or per-
// broadcaster subscription: a creator pastes this server's own per-timer
// webhook URL (see internal/server/throne_webhook.go,
// POST /webhooks/throne/{timerID}) directly into Throne's own dashboard
// (Profile -> Integrations -> Webhook — see
// https://help.throne.com/en/articles/15935990-how-do-i-set-up-webhook-integration),
// and Throne POSTs every gift/contribution to it from then on. Requests are
// authenticated by an Ed25519 signature against Throne's own published
// public key (the same scheme Discord uses for interaction webhooks) —
// there is no per-account secret to configure, unlike Twitch's
// TWITCH_WEBHOOK_SECRET.
package throne

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// defaultPublicKeyPEM is Throne's published webhook-signing public key, as
// of
// https://help.throne.com/en/articles/15935990-how-do-i-set-up-webhook-integration.
// Overridable via Config.PublicKeyPEM in case Throne ever rotates it ahead
// of this constant being updated, or to point at a test key in tests.
const defaultPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAPXbUfxh7XL4SYUVcfhmYMIbxvtR9E9LDd8gPJ1PwSD8=
-----END PUBLIC KEY-----`

// Config configures a Client. Every field is optional.
type Config struct {
	// PublicKeyPEM overrides defaultPublicKeyPEM. Leave empty in
	// production.
	PublicKeyPEM string
}

// Client verifies and parses Throne webhook notifications. Unlike
// twitch.Client/kick.Client, it holds no HTTP client and makes no outbound
// calls of its own — Throne's signing key is fixed and published, not
// fetched from an API, and there's no subscription to create.
type Client struct {
	pubKey ed25519.PublicKey

	seenMu sync.Mutex
	seen   map[string]time.Time // event_id -> when first seen
}

// New builds a Client, parsing cfg.PublicKeyPEM (or defaultPublicKeyPEM if
// empty). Returns an error if the key isn't a valid Ed25519 PEM-encoded
// SubjectPublicKeyInfo block.
func New(cfg Config) (*Client, error) {
	pemBlock := cfg.PublicKeyPEM
	if pemBlock == "" {
		pemBlock = defaultPublicKeyPEM
	}

	block, _ := pem.Decode([]byte(pemBlock))
	if block == nil {
		return nil, fmt.Errorf("throne: public key is not valid PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("throne: parse public key: %w", err)
	}
	pubKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("throne: public key was not Ed25519")
	}

	return &Client{pubKey: pubKey, seen: make(map[string]time.Time)}, nil
}

// VerifyMessage checks an incoming webhook request's signature against
// this Client's Ed25519 public key, per
// https://help.throne.com/en/articles/15935990-how-do-i-set-up-webhook-integration:
// the signed message is "{timestamp}.{rawBody}" (dot-joined).
func (c *Client) VerifyMessage(header http.Header, body []byte) bool {
	timestamp := header.Get("X-Signature-Timestamp")
	sigHex := header.Get("X-Signature-Ed25519")
	if timestamp == "" || sigHex == "" {
		return false
	}

	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}

	message := append([]byte(timestamp+"."), body...)
	return ed25519.Verify(c.pubKey, message, sig)
}

// SeenBefore reports whether eventID has already been processed (Throne
// may redeliver notifications), remembering it if not. Call Sweep
// periodically to bound memory from an ever-growing set.
func (c *Client) SeenBefore(eventID string) bool {
	c.seenMu.Lock()
	defer c.seenMu.Unlock()

	if _, ok := c.seen[eventID]; ok {
		return true
	}
	c.seen[eventID] = time.Now()
	return false
}

// Sweep discards remembered event IDs older than 15 minutes.
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

// ParsedEvent is one Throne webhook notification translated into subathon
// terms, before reward/money rules (which vary per timer) turn Amount into
// seconds/dollars.
type ParsedEvent struct {
	EventID  string
	Username string
	// Amount is in dollars (Throne's price/amount fields are integer cents
	// — see priceToDollars).
	Amount float64
	// Item is Throne's item_name, purely for logging.
	Item string
}

// ParseNotification decodes a Throne webhook notification body into a
// ParsedEvent. ok is false for an event type this package doesn't
// translate (Throne may add new ones in the future).
func ParseNotification(body []byte) (ParsedEvent, bool, error) {
	var envelope struct {
		EventID   string          `json:"event_id"`
		EventType string          `json:"event_type"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ParsedEvent{}, false, fmt.Errorf("parse notification envelope: %w", err)
	}

	switch envelope.EventType {
	case "gift_purchased":
		var d struct {
			GifterUsername string `json:"gifter_username"`
			ItemName       string `json:"item_name"`
			Price          int64  `json:"price"`
		}
		if err := json.Unmarshal(envelope.Data, &d); err != nil {
			return ParsedEvent{}, false, fmt.Errorf("parse gift_purchased data: %w", err)
		}
		return ParsedEvent{
			EventID:  envelope.EventID,
			Username: usernameOrAnonymous(d.GifterUsername),
			Amount:   priceToDollars(d.Price),
			Item:     d.ItemName,
		}, true, nil

	case "contribution_purchased":
		var d struct {
			GifterUsername string `json:"gifter_username"`
			ItemName       string `json:"item_name"`
			Amount         int64  `json:"amount"`
		}
		if err := json.Unmarshal(envelope.Data, &d); err != nil {
			return ParsedEvent{}, false, fmt.Errorf("parse contribution_purchased data: %w", err)
		}
		return ParsedEvent{
			EventID:  envelope.EventID,
			Username: usernameOrAnonymous(d.GifterUsername),
			Amount:   priceToDollars(d.Amount),
			Item:     d.ItemName,
		}, true, nil

	case "gift_crowdfunded":
		// Fires once a crowdfunded gift's goal is met by contributions from
		// potentially many people, not for any single person's
		// contribution — those are already counted individually via their
		// own contribution_purchased events as they come in, so counting
		// this too would double-count the same money. Deliberately
		// unhandled (ok=false) rather than recorded as a $0 event.
		return ParsedEvent{}, false, nil

	default:
		return ParsedEvent{}, false, nil
	}
}

func usernameOrAnonymous(username string) string {
	if username == "" {
		return "anonymous"
	}
	return username
}

// priceToDollars converts Throne's integer-cents price/amount fields to
// dollars, e.g. 1099 -> 10.99.
func priceToDollars(cents int64) float64 {
	return float64(cents) / 100
}
