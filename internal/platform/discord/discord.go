// Package discord posts announcements to a Discord channel through an
// incoming webhook URL (Server Settings → Integrations → Webhooks), so
// no bot account or OAuth flow is needed — the URL itself is the
// credential, and must be treated as a secret.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// webhookHosts are the hosts Discord hands out webhook URLs on. Posting
// is restricted to these so a timer's webhook setting can't be pointed
// at an arbitrary (e.g. internal) address for this server to POST to.
var webhookHosts = map[string]bool{
	"discord.com":        true,
	"discordapp.com":     true,
	"ptb.discord.com":    true,
	"canary.discord.com": true,
}

// ValidWebhookURL reports whether raw looks like a Discord webhook URL:
// https, on a Discord host, under /api/webhooks/, with no credentials or
// custom port.
func ValidWebhookURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "https" &&
		webhookHosts[strings.ToLower(u.Host)] &&
		u.User == nil &&
		strings.HasPrefix(u.Path, "/api/webhooks/") &&
		len(strings.Split(strings.Trim(u.Path, "/"), "/")) >= 4 // api/webhooks/{id}/{token}
}

// Client posts webhook messages.
type Client struct {
	http *http.Client
}

// NewClient returns a Client with a short request timeout — an
// announcement isn't worth holding anything up for.
func NewClient() *Client {
	return &Client{http: &http.Client{
		Timeout: 10 * time.Second,
		// A real webhook never redirects; refusing to follow keeps
		// requests on the host ValidWebhookURL checked.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

type webhookMessage struct {
	Content         string          `json:"content"`
	AllowedMentions allowedMentions `json:"allowed_mentions"`
}

// allowedMentions controls which mentions in a message actually ping:
// an empty Parse pings no one, even if the text says "@everyone";
// "everyone" lets an @everyone/@here in it ping. Role and user mentions
// never ping either way.
type allowedMentions struct {
	Parse []string `json:"parse"`
}

// EveryoneMention is what a message starts with to ping @everyone (see
// Message.PingEveryone).
const EveryoneMention = "@everyone"

// Message is one webhook post.
type Message struct {
	Content string
	// PingEveryone prefixes Content with EveryoneMention and lets it
	// ping. Without it, nothing in Content pings anyone.
	PingEveryone bool
}

// MaxContentLen is Discord's limit on a message's length, in
// characters; it rejects longer ones outright.
const MaxContentLen = 2000

// Text is the message exactly as it will appear in the channel, cut to
// MaxContentLen characters (with an ellipsis) if it'd be longer.
func (m Message) Text() string {
	text := m.Content
	if m.PingEveryone {
		text = EveryoneMention + " " + text
	}
	if runes := []rune(text); len(runes) > MaxContentLen {
		text = string(runes[:MaxContentLen-1]) + "…"
	}
	return text
}

// Post sends m through webhookURL, which must pass ValidWebhookURL.
func (c *Client) Post(ctx context.Context, webhookURL string, m Message) error {
	if !ValidWebhookURL(webhookURL) {
		return fmt.Errorf("not a Discord webhook URL")
	}

	mentions := allowedMentions{Parse: []string{}}
	if m.PingEveryone {
		mentions.Parse = []string{"everyone"}
	}
	body, err := json.Marshal(webhookMessage{Content: m.Text(), AllowedMentions: mentions})
	if err != nil {
		return fmt.Errorf("marshal webhook message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	resp.Body.Close()
	return nil
}

// WebhookInfo is how a webhook's posts are labeled in the channel.
type WebhookInfo struct {
	Name string
	// AvatarURL is empty when the webhook uses Discord's default avatar.
	AvatarURL string
}

// Info looks up the name and avatar Discord shows on webhookURL's posts
// (set in the channel's Integrations settings). The webhook token
// authorizes this; no bot account is needed.
func (c *Client) Info(ctx context.Context, webhookURL string) (WebhookInfo, error) {
	if !ValidWebhookURL(webhookURL) {
		return WebhookInfo{}, fmt.Errorf("not a Discord webhook URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, webhookURL, nil)
	if err != nil {
		return WebhookInfo{}, fmt.Errorf("build webhook request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return WebhookInfo{}, fmt.Errorf("get webhook: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		ID     string  `json:"id"`
		Name   string  `json:"name"`
		Avatar *string `json:"avatar"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return WebhookInfo{}, fmt.Errorf("decode webhook: %w", err)
	}
	info := WebhookInfo{Name: body.Name}
	if body.Avatar != nil && *body.Avatar != "" {
		info.AvatarURL = fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png?size=128",
			url.PathEscape(body.ID), url.PathEscape(*body.Avatar))
	}
	return info, nil
}

// do sends req, turning a non-2xx response into an error. The caller
// closes the returned body.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		// The URL (and so its token) can appear in a *url.Error; drop it.
		if ue, ok := err.(*url.Error); ok {
			err = ue.Err
		}
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("discord returned %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
	}
	return resp, nil
}
