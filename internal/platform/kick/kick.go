// Package kick will implement the Kick platform Listener. Kick's public
// events/webhooks API integration is not wired up yet.
package kick

import (
	"context"
	"log"

	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

// Config holds the credentials/settings needed to connect to Kick.
type Config struct {
	ChannelID string
	APIToken  string
}

// Client is a stub Kick Listener. Connect it up to Kick's webhooks/events
// API once credentials and the subathon reward rules are defined.
type Client struct {
	cfg Config
}

// New creates a Kick client from the given config.
func New(cfg Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Name() string { return "kick" }

// Listen currently just blocks until ctx is cancelled. TODO: subscribe to
// Kick sub/gift events and translate them into subathon.Event.
func (c *Client) Listen(ctx context.Context, onEvent func(subathon.Event)) error {
	log.Println("kick: listener not yet implemented, idling")
	<-ctx.Done()
	return ctx.Err()
}
