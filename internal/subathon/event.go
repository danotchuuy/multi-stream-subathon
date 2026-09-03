package subathon

import (
	"strings"
	"time"
)

// Platform identifies which streaming service an event originated from.
type Platform string

const (
	PlatformKick           Platform = "kick"
	PlatformYouTube        Platform = "youtube"
	PlatformTwitch         Platform = "twitch"
	PlatformStreamElements Platform = "streamelements"
	PlatformManual         Platform = "manual"
)

// EventType identifies what kind of action added time to the clock.
type EventType string

const (
	EventSub       EventType = "sub"
	EventGiftedSub EventType = "gifted_sub"
	EventDonation  EventType = "donation"
	EventBits      EventType = "bits"
	EventManual    EventType = "manual"
)

// Event is a single time-adding occurrence, e.g. a sub or donation,
// attributed to whoever triggered it.
type Event struct {
	ID           string    `json:"id"`
	TimerID      string    `json:"timerId"`
	Platform     Platform  `json:"platform"`
	Type         EventType `json:"type"`
	Username     string    `json:"username"`
	SecondsAdded int       `json:"secondsAdded"`
	// MoneyAdded is how many dollars this event counted toward the
	// timer's money goal, per MoneyRules — independent of SecondsAdded,
	// since a timer may track time, money, both, or (with every rate set
	// to 0) neither.
	MoneyAdded float64   `json:"moneyAdded,omitempty"`
	Amount     float64   `json:"amount,omitempty"` // e.g. donation amount or bits count
	Occurred   time.Time `json:"occurred"`
}

// NormalizeUsername canonicalizes a contributor's username so the same
// person is recognized as one contributor regardless of how a given
// platform (or a manual test event) capitalizes or formats it: trims
// whitespace, drops a leading "@" (common for handles), and lowercases.
func NormalizeUsername(username string) string {
	username = strings.TrimSpace(username)
	username = strings.TrimPrefix(username, "@")
	return strings.ToLower(username)
}
