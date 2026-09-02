package subathon

import "time"

// TimerRecord is the persisted representation of a Timer's clock state.
// It intentionally mirrors Timer's private fields so a Repo implementation
// can round-trip state without reaching into Timer internals.
type TimerRecord struct {
	ID             string
	UserID         string
	Name           string
	Running        bool
	StartedAt      time.Time
	EndsAt         time.Time
	RemainingSecs  int
	TotalAddedSecs int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Repo persists timers and their contributor events. Implementations must
// be safe for concurrent use.
//
// Timer state is written on every control action (reset/resume/stop) and
// event, not on every tick, so this does not need to be fast enough for a
// per-second write path.
type Repo interface {
	// CreateTimer inserts a new, stopped timer record.
	CreateTimer(rec TimerRecord) error

	// ListTimers returns every known timer, newest first.
	ListTimers() ([]TimerRecord, error)

	// SaveTimerState updates a timer's clock fields in place.
	SaveTimerState(rec TimerRecord) error

	// InsertEvent records a contributor event against a timer.
	InsertEvent(e Event) error

	// RecentEvents returns up to limit of a timer's most recent events,
	// newest first.
	RecentEvents(timerID string, limit int) ([]Event, error)
}
