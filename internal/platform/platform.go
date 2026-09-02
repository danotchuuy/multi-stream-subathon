// Package platform defines the common interface each streaming platform
// integration (Kick, YouTube, Twitch) implements to feed events into the
// subathon store.
package platform

import (
	"context"

	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

// Listener connects to a streaming platform's event source (EventSub,
// webhooks, chat, etc.) and forwards subathon-relevant events to onEvent
// until ctx is cancelled.
type Listener interface {
	// Name identifies the platform, e.g. "kick", "youtube", "twitch".
	Name() string

	// Listen blocks until ctx is cancelled or an unrecoverable error
	// occurs, calling onEvent for each event it observes.
	Listen(ctx context.Context, onEvent func(subathon.Event)) error
}
