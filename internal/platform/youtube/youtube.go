// Package youtube feeds a timer's YouTube Super Chats/Super Stickers/new
// members/gifted memberships into subathon.Event, using a timer's
// reward/money rules — see Poller in poller.go. Unlike Twitch/Kick, there
// is no app-level credential that works for an arbitrary channel: YouTube
// only lets a channel's own linked owner (see internal/auth's
// identities) read that channel's live chat, so every call here takes
// that owner's own OAuth2 access token as a parameter, resolved and kept
// fresh by Poller via internal/oauth.Provider.Refresh rather than a
// server-wide Config the way twitch.Client's app token is.
//
// Reading live chat itself uses YouTube's liveChatMessages.streamList
// gRPC method (see internal/youtubepb), not REST polling: it's push-based
// (YouTube holds the connection open and sends new messages as they're
// posted) and, unlike the REST liveChatMessages.list method it
// supersedes, is what Google's own docs recommend
// (https://developers.google.com/youtube/v3/live/streaming-live-chat).
// This package's gRPC plumbing (stream reconnect/backoff, terminal-error
// classification) and the ported internal/youtubepb bindings themselves
// are carried over from this same author's multi-stream-moderation
// project, which exercises this service against a live YouTube account —
// unlike this project's Kick integration, this isn't a from-the-docs
// guess.
package youtube

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/danotchuuy/multi-stream-subathon/internal/youtubepb"
)

// apiBase is a var (not const) so tests can point it at a local server.
var apiBase = "https://www.googleapis.com/youtube/v3"

// grpcTarget is YouTube's live chat streaming endpoint. See
// https://developers.google.com/youtube/v3/live/streaming-live-chat
const grpcTarget = "dns:///youtube.googleapis.com:443"

// Client is a thin wrapper around the YouTube Data API's read endpoints
// this package needs, plus the shared gRPC connection used for
// liveChatMessages.streamList. It holds no per-account state itself —
// every call takes the caller's own OAuth2 access token — so one Client
// is shared across every timer.
type Client struct {
	hc *http.Client

	grpcOnce sync.Once
	grpcConn *grpc.ClientConn
	grpcErr  error
}

// NewClient builds a Client ready to use; no configuration needed since
// the server-level app credential this integration needs (see package
// doc) lives on the internal/oauth.Provider that drives sign-in, not on
// this REST/gRPC API wrapper.
func NewClient() *Client {
	return &Client{hc: &http.Client{Timeout: 15 * time.Second}}
}

// chatClient lazily dials the shared gRPC connection used for every
// liveChatMessages.streamList call. One connection is reused across every
// watched timer rather than dialing per-timer: YouTube's own guidance
// warns that HTTP/2's SETTINGS_MAX_CONCURRENT_STREAMS caps how many
// concurrent streams a single connection can carry, but this server only
// expects a handful of concurrently watched channels, nowhere near that
// limit.
func (c *Client) chatClient() (youtubepb.V3DataLiveChatMessageServiceClient, error) {
	c.grpcOnce.Do(func() {
		creds := credentials.NewTLS(&tls.Config{})
		c.grpcConn, c.grpcErr = grpc.NewClient(grpcTarget, grpc.WithTransportCredentials(creds))
	})
	if c.grpcErr != nil {
		return nil, c.grpcErr
	}
	return youtubepb.NewV3DataLiveChatMessageServiceClient(c.grpcConn), nil
}

// Channel is the identity an access token resolves to (see
// ResolveChannel) — enough to confirm to a timer owner which YouTube
// channel they just linked, and to address FindActiveLiveChat.
type Channel struct {
	ID    string
	Title string
}

// ResolveChannel returns the YouTube channel token's own account owns,
// via GET channels?part=snippet&mine=true. Also serves as the save-time
// validation for a freshly linked identity: an invalid/expired token
// fails here with a clear error.
func (c *Client) ResolveChannel(ctx context.Context, token string) (Channel, error) {
	var resp struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	q := url.Values{"part": {"snippet"}, "mine": {"true"}}
	if err := c.get(ctx, token, apiBase+"/channels?"+q.Encode(), &resp); err != nil {
		return Channel{}, err
	}
	if len(resp.Items) == 0 {
		return Channel{}, fmt.Errorf("youtube: channels?mine=true returned no channel")
	}
	return Channel{ID: resp.Items[0].ID, Title: resp.Items[0].Snippet.Title}, nil
}

// FindActiveLiveChat returns the live chat ID for token's own channel's
// currently active broadcast, via
// liveBroadcasts?mine=true&broadcastStatus=active — mine=true is what
// makes this only work with that channel owner's own token, not any
// other authenticated caller's. ok is false (not an error) if that
// channel isn't currently live, the ordinary case Poller polls through
// rather than treating as a failure.
func (c *Client) FindActiveLiveChat(ctx context.Context, token string) (liveChatID string, ok bool, err error) {
	var resp struct {
		Items []struct {
			Snippet struct {
				LiveChatID string `json:"liveChatId"`
			} `json:"snippet"`
		} `json:"items"`
	}
	q := url.Values{
		"part":            {"snippet"},
		"mine":            {"true"},
		"broadcastStatus": {"active"},
		"broadcastType":   {"all"},
	}
	if err := c.get(ctx, token, apiBase+"/liveBroadcasts?"+q.Encode(), &resp); err != nil {
		return "", false, err
	}
	for _, item := range resp.Items {
		if item.Snippet.LiveChatID != "" {
			return item.Snippet.LiveChatID, true, nil
		}
	}
	return "", false, nil
}

func (c *Client) get(ctx context.Context, token, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("youtube API %s: %s: %s", endpoint, resp.Status, string(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
