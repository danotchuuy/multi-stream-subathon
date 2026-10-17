package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestValidWebhookURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://discord.com/api/webhooks/123/abc":        true,
		"https://discordapp.com/api/webhooks/123/abc":     true,
		"https://canary.discord.com/api/webhooks/123/abc": true,
		"https://discord.com/api/webhooks/123":            false, // no token
		"http://discord.com/api/webhooks/123/abc":         false,
		"https://discord.com:8443/api/webhooks/123/abc":   false,
		"https://user:pw@discord.com/api/webhooks/1/a":    false,
		"https://evil.com/api/webhooks/123/abc":           false,
		"https://discord.com.evil.com/api/webhooks/1/a":   false,
		"https://169.254.169.254/api/webhooks/1/a":        false,
		"https://discord.com/api/users/123/abc":           false,
		"":                                                false,
	} {
		if got := ValidWebhookURL(raw); got != want {
			t.Errorf("ValidWebhookURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// capturePost posts m through a fake transport and returns what was sent.
func capturePost(t *testing.T, m Message) webhookMessage {
	t.Helper()
	var got webhookMessage
	c := NewClient()
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.String() != "https://discord.com/api/webhooks/1/tok" {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if err := c.Post(context.Background(), "https://discord.com/api/webhooks/1/tok", m); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestPostWithoutPingNeverPings(t *testing.T) {
	got := capturePost(t, Message{Content: "hi @everyone"})
	if got.Content != "hi @everyone" || got.AllowedMentions.Parse == nil || len(got.AllowedMentions.Parse) != 0 {
		t.Errorf("sent %+v, want content unchanged with allowed_mentions.parse = []", got)
	}
}

func TestPostWithPingMentionsEveryone(t *testing.T) {
	got := capturePost(t, Message{Content: "Happy Hour!", PingEveryone: true})
	if got.Content != "@everyone Happy Hour!" {
		t.Errorf("content = %q, want it prefixed with @everyone", got.Content)
	}
	if len(got.AllowedMentions.Parse) != 1 || got.AllowedMentions.Parse[0] != "everyone" {
		t.Errorf("allowed_mentions.parse = %v, want [everyone]", got.AllowedMentions.Parse)
	}
}

func TestInfoBuildsAvatarURL(t *testing.T) {
	c := NewClient()
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
			`{"id": "42", "name": "Subathon Bot", "avatar": "abc123", "token": "tok"}`))}, nil
	})
	info, err := c.Info(context.Background(), "https://discord.com/api/webhooks/42/tok")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "Subathon Bot" || info.AvatarURL != "https://cdn.discordapp.com/avatars/42/abc123.png?size=128" {
		t.Errorf("info = %+v", info)
	}
}

func TestPostRejectsNonDiscordURLAndBadStatus(t *testing.T) {
	c := NewClient()
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader(`{"message": "Unknown Webhook"}`))}, nil
	})

	if err := c.Post(context.Background(), "https://example.com/api/webhooks/1/tok", Message{Content: "x"}); err == nil {
		t.Error("posting to a non-Discord URL succeeded")
	}
	if err := c.Post(context.Background(), "https://discord.com/api/webhooks/1/tok", Message{Content: "x"}); err == nil || !strings.Contains(err.Error(), "Unknown Webhook") {
		t.Errorf("404 error = %v, want it to mention Discord's message", err)
	}
}
