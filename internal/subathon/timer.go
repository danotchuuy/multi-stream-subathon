// Package subathon holds the platform-agnostic domain state for one or
// more subathon runs: each timer's running clock, contributor events, and
// snapshots, backed by a Repo for durability.
package subathon

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const maxRecentEvents = 25

// Snapshot is the point-in-time view of a timer sent to clients.
type Snapshot struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Running        bool      `json:"running"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	EndsAt         time.Time `json:"endsAt"`
	RemainingSecs  int       `json:"remainingSecs"`
	TotalAddedSecs int       `json:"totalAddedSecs"`
	RecentEvents   []Event   `json:"recentEvents"`
}

// Timer holds one subathon clock's mutable state behind a mutex. It is
// safe for concurrent use by HTTP handlers, platform listeners, and the WS
// hub. Every state-changing method persists via repo before returning.
type Timer struct {
	mu   sync.RWMutex
	repo Repo

	id     string
	userID string
	name   string

	running   bool
	startedAt time.Time
	endsAt    time.Time // meaningful only while running

	// remaining holds the frozen time left on the clock while stopped
	// (including before the clock has ever been started).
	remaining time.Duration

	totalAdded int
	events     []Event // oldest first, capped at maxRecentEvents
}

func newTimer(repo Repo, rec TimerRecord, events []Event) *Timer {
	return &Timer{
		repo:       repo,
		id:         rec.ID,
		userID:     rec.UserID,
		name:       rec.Name,
		running:    rec.Running,
		startedAt:  rec.StartedAt,
		endsAt:     rec.EndsAt,
		remaining:  time.Duration(rec.RemainingSecs) * time.Second,
		totalAdded: rec.TotalAddedSecs,
		events:     events,
	}
}

// ID is this timer's stable identifier, used in API routes, the WS
// subscription, and as the token the overlay is given to know which timer
// to display.
func (t *Timer) ID() string { return t.id }

// UserID is the owning account's ID. Only that user may control this
// timer or add events to it; anyone with the ID can view it (state/ws).
func (t *Timer) UserID() string { return t.userID }

// Name is a human-readable label, shown in the timer picker.
func (t *Timer) Name() string { return t.name }

// Reset (re)initializes the countdown to the given duration and starts it
// running, discarding whatever time was left on the clock.
func (t *Timer) Reset(initial time.Duration) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.running = true
	t.startedAt = time.Now()
	t.endsAt = t.startedAt.Add(initial)
	t.remaining = initial

	return t.persistLocked()
}

// Resume continues the countdown from wherever it was frozen by Stop,
// without changing the remaining time. It's a no-op if already running.
func (t *Timer) Resume() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return nil
	}
	t.running = true
	t.startedAt = time.Now()
	t.endsAt = t.startedAt.Add(t.remaining)

	return t.persistLocked()
}

// Stop pauses the countdown, freezing the remaining time as of the moment
// Stop was called so it stops counting down until Resume or Reset is
// called. It's a no-op if already stopped.
func (t *Timer) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}
	t.remaining = max(0, time.Until(t.endsAt))
	t.running = false

	return t.persistLocked()
}

// AddEvent records a contributor event and extends the clock: the running
// end time if the countdown is live, or the frozen remaining time if it's
// stopped. e.ID is generated if empty.
func (t *Timer) AddEvent(e Event) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	e.TimerID = t.id
	e.Username = NormalizeUsername(e.Username)
	if e.Occurred.IsZero() {
		e.Occurred = time.Now()
	}

	added := time.Duration(e.SecondsAdded) * time.Second
	t.totalAdded += e.SecondsAdded
	if t.running {
		t.endsAt = t.endsAt.Add(added)
	} else {
		t.remaining += added
	}

	t.events = append(t.events, e)
	if len(t.events) > maxRecentEvents {
		t.events = t.events[len(t.events)-maxRecentEvents:]
	}

	if err := t.repo.InsertEvent(e); err != nil {
		return err
	}
	return t.persistLocked()
}

// Snapshot returns the current state, computing remaining time as of now.
func (t *Timer) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	remaining := t.remaining
	endsAt := t.endsAt
	if t.running {
		remaining = max(0, time.Until(t.endsAt))
	} else {
		endsAt = time.Time{}
	}

	events := make([]Event, len(t.events))
	copy(events, t.events)

	return Snapshot{
		ID:             t.id,
		Name:           t.name,
		Running:        t.running,
		StartedAt:      t.startedAt,
		EndsAt:         endsAt,
		RemainingSecs:  int(remaining.Seconds()),
		TotalAddedSecs: t.totalAdded,
		RecentEvents:   events,
	}
}

// persistLocked writes the current clock state to the repo. Caller must
// hold t.mu (read or write).
func (t *Timer) persistLocked() error {
	remaining := t.remaining
	if t.running {
		remaining = max(0, time.Until(t.endsAt))
	}
	return t.repo.SaveTimerState(TimerRecord{
		ID:             t.id,
		UserID:         t.userID,
		Name:           t.name,
		Running:        t.running,
		StartedAt:      t.startedAt,
		EndsAt:         t.endsAt,
		RemainingSecs:  int(remaining.Seconds()),
		TotalAddedSecs: t.totalAdded,
		UpdatedAt:      time.Now(),
	})
}
