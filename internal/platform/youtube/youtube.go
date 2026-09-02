// Package youtube will implement the YouTube platform Listener via the
// YouTube Live Streaming / Membership APIs. Not wired up yet.
package youtube

import (
	"context"
	"log"

	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

// Config holds the credentials/settings needed to connect to YouTube.
type Config struct {
	ChannelID string
	APIKey    string
}

// Client is a stub YouTube Listener. Connect it up to the memberships/
// Super Chat APIs once credentials are defined.
type Client struct {
	cfg Config
}

// New creates a YouTube client from the given config.
func New(cfg Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Name() string { return "youtube" }

// Listen currently just blocks until ctx is cancelled. TODO: poll/subscribe
// to YouTube membership and Super Chat events and translate them into
// subathon.Event.
func (c *Client) Listen(ctx context.Context, onEvent func(subathon.Event)) error {
	log.Println("youtube: listener not yet implemented, idling")
	<-ctx.Done()
	return ctx.Err()
}
