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
		m.timers[rec.ID] = newTimer(repo, rec, events)
	}
	return m, nil
}

// List returns userID's timers as Summaries, sorted by name.
func (m *Manager) List(userID string) []Summary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summaries := make([]Summary, 0, len(m.timers))
	for _, t := range m.timers {
		if t.UserID() != userID {
			continue
		}
		snap := t.Snapshot()
		summaries = append(summaries, Summary{
			ID:            snap.ID,
			Name:          snap.Name,
			Running:       snap.Running,
			RemainingSecs: snap.RemainingSecs,
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

	t := newTimer(m.repo, rec, nil)

	m.mu.Lock()
	m.timers[rec.ID] = t
	m.mu.Unlock()

	return t, nil
}

func reverse(events []Event) {
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
}
