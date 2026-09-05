package subathon

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Summary is the lightweight view of a timer used for listing/picking
// between multiple timers.
type Summary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Running       bool   `json:"running"`
	RemainingSecs int    `json:"remainingSecs"`
	// Owner is true if the user this Summary was built for owns the
	// timer, false if they only moderate it (see Timer.AddModerator).
	Owner bool `json:"owner"`
}

// Manager owns every Timer this server knows about, keyed by ID, and keeps
// them in sync with the Repo.
type Manager struct {
	mu     sync.RWMutex
	repo   Repo
	timers map[string]*Timer
}

// NewManager loads every timer (and each one's recent events) from repo
// into memory.
func NewManager(repo Repo) (*Manager, error) {
	records, err := repo.ListTimers()
	if err != nil {
		return nil, fmt.Errorf("list timers: %w", err)
	}

	m := &Manager{repo: repo, timers: make(map[string]*Timer, len(records))}
	for _, rec := range records {
		events, err := repo.RecentEvents(rec.ID, maxRecentEvents)
		if err != nil {
			return nil, fmt.Errorf("load events for timer %s: %w", rec.ID, err)
		}
		reverse(events) // RecentEvents is newest-first; Timer keeps oldest-first
		moderatorIDs, err := repo.ListModerators(rec.ID)
		if err != nil {
			return nil, fmt.Errorf("load moderators for timer %s: %w", rec.ID, err)
		}
		m.timers[rec.ID] = newTimer(repo, rec, events, moderatorIDs)
	}
	return m, nil
}

// List returns every timer userID owns or moderates, as Summaries sorted
// by name.
func (m *Manager) List(userID string) []Summary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summaries := make([]Summary, 0, len(m.timers))
	for _, t := range m.timers {
		owner := t.UserID() == userID
		if !owner && !t.IsModerator(userID) {
			continue
		}
		snap := t.Snapshot()
		summaries = append(summaries, Summary{
			ID:            snap.ID,
			Name:          snap.Name,
			Running:       snap.Running,
			RemainingSecs: snap.RemainingSecs,
			Owner:         owner,
		})
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Name < summaries[j].Name })
	return summaries
}

// All returns every Timer this manager holds, for things like a broadcast
// loop that needs to tick each one.
func (m *Manager) All() []*Timer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	timers := make([]*Timer, 0, len(m.timers))
	for _, t := range m.timers {
		timers = append(timers, t)
	}
	return timers
}

// Get looks up a timer by ID.
func (m *Manager) Get(id string) (*Timer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.timers[id]
	return t, ok
}

// Create makes a new, stopped timer owned by userID and registers it.
func (m *Manager) Create(userID, name string) (*Timer, error) {
	if name == "" {
		name = "Subathon"
	}

	now := time.Now()
	rec := TimerRecord{ID: uuid.NewString(), UserID: userID, Name: name, CreatedAt: now, UpdatedAt: now}
	if err := m.repo.CreateTimer(rec); err != nil {
		return nil, fmt.Errorf("create timer: %w", err)
	}

	t := newTimer(m.repo, rec, nil, nil)

	m.mu.Lock()
	m.timers[rec.ID] = t
	m.mu.Unlock()

	return t, nil
}

// RunningByTwitchChannel returns every currently-running, not-ended timer
// configured to watch the given Twitch broadcaster ID (see
// Timer.SetTwitchChannel), for translating an incoming EventSub
// notification into the timer(s) it should add time to. An ended timer
// (see Timer.SetEnded) is excluded regardless of its Running state —
// this is the "de-register the listeners" half of ending a timer: it
// simply stops being found here, same as if nothing were watching that
// channel on its behalf (other timers still watching it are unaffected).
func (m *Manager) RunningByTwitchChannel(broadcasterID string) []*Timer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var timers []*Timer
	for _, t := range m.timers {
		id, _ := t.TwitchChannel()
		if id == broadcasterID && t.Snapshot().Running && !t.Ended() {
			timers = append(timers, t)
		}
	}
	return timers
}

// RunningByKickChannel is RunningByTwitchChannel's Kick equivalent (see
// Timer.SetKickChannel).
func (m *Manager) RunningByKickChannel(broadcasterID string) []*Timer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var timers []*Timer
	for _, t := range m.timers {
		id, _ := t.KickChannel()
		if id == broadcasterID && t.Snapshot().Running && !t.Ended() {
			timers = append(timers, t)
		}
	}
	return timers
}

// TimersByTwitchChannel returns every not-ended timer — running or not —
// configured to watch the given Twitch broadcaster ID. Unlike
// RunningByTwitchChannel (used for contribution events, which shouldn't
// extend a timer nobody's running), chat commands like "!timer play"
// specifically need to reach a *stopped* timer too — but, like
// RunningByTwitchChannel, never an ended one: this is what makes "!timer
// ..." commands stop working once a timer's ended (see
// handleTwitchChatCommand, the only other caller besides
// isTwitchChannelEditor).
func (m *Manager) TimersByTwitchChannel(broadcasterID string) []*Timer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var timers []*Timer
	for _, t := range m.timers {
		id, _ := t.TwitchChannel()
		if id == broadcasterID && !t.Ended() {
			timers = append(timers, t)
		}
	}
	return timers
}

func reverse(events []Event) {
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
}
