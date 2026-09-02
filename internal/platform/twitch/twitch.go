// Package twitch will implement the Twitch platform Listener via EventSub.
// Not wired up yet.
package twitch

import (
	"context"
	"log"

	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

// Config holds the credentials/settings needed to connect to Twitch.
type Config struct {
	ChannelID    string
	ClientID     string
	ClientSecret string
}

// Client is a stub Twitch Listener. Connect it up to EventSub (subs, gift
// subs, bits, follows) once credentials are defined.
type Client struct {
	cfg Config
}

// New creates a Twitch client from the given config.
func New(cfg Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Name() string { return "twitch" }

// Listen currently just blocks until ctx is cancelled. TODO: subscribe to
// Twitch EventSub subscription/bits events and translate them into
// subathon.Event.
func (c *Client) Listen(ctx context.Context, onEvent func(subathon.Event)) error {
	log.Println("twitch: listener not yet implemented, idling")
	<-ctx.Done()
	return ctx.Err()
}
