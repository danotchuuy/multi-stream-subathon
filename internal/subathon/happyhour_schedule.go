package subathon

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// HappyHourTrigger is what starts a ScheduledHappyHour.
type HappyHourTrigger string

const (
	// HappyHourTriggerAt starts the happy hour at a wall-clock time
	// (ScheduledHappyHour.StartAt).
	HappyHourTriggerAt HappyHourTrigger = "at"
	// HappyHourTriggerRemaining starts the happy hour once the running
	// countdown reaches ScheduledHappyHour.RemainingSecs left.
	HappyHourTriggerRemaining HappyHourTrigger = "remaining"
)

// MaxScheduledHappyHours caps how many pending happy hours one timer can
// have queued at once.
const MaxScheduledHappyHours = 50

// MissedHappyHourGrace is how late an "at" happy hour can still start —
// e.g. after a brief server restart. One found due later than this (the
// server was down for a while, or the timer was ended over its start
// time and reopened afterward) is dropped rather than starting a happy
// hour nobody planned for at that point.
const MissedHappyHourGrace = 5 * time.Minute

var (
	// ErrInvalidHappyHourSchedule is returned by Timer.ScheduleHappyHour
	// for an unknown trigger, a non-positive remaining-time mark, or a
	// start time that isn't in the future.
	ErrInvalidHappyHourSchedule = errors.New("invalid happy hour schedule")
	// ErrHappyHourMarkPassed is returned by Timer.ScheduleHappyHour for a
	// HappyHourTriggerRemaining mark the clock is already at or under —
	// it would otherwise start the moment the timer next ticks.
	ErrHappyHourMarkPassed = errors.New("the timer already has that much time or less left")
	// ErrTooManyScheduledHappyHours is returned by
	// Timer.ScheduleHappyHour once MaxScheduledHappyHours are pending.
	ErrTooManyScheduledHappyHours = fmt.Errorf("at most %d happy hours can be scheduled at once", MaxScheduledHappyHours)
	// ErrScheduledHappyHourNotFound is returned by
	// Timer.CancelScheduledHappyHour for an unknown ID (including one
	// that has already started).
	ErrScheduledHappyHourNotFound = errors.New("scheduled happy hour not found")
)

// ScheduledHappyHour is a happy hour (see Timer.StartTimeBoost) queued to
// start later, either at StartAt (Trigger "at") or once the running
// countdown reaches RemainingSecs left (Trigger "remaining"). It's
// removed once it starts or is cancelled.
type ScheduledHappyHour struct {
	ID            string           `json:"id"`
	Trigger       HappyHourTrigger `json:"trigger"`
	StartAt       time.Time        `json:"startAt,omitzero"`
	RemainingSecs int              `json:"remainingSecs,omitempty"`
	DurationSecs  int              `json:"durationSecs"`
	Multiplier    float64          `json:"multiplier"`
	// Announced is set once this happy hour's Discord "starting soon"
	// announcement has gone out (see TakeDueHappyHours), so it's never
	// sent twice.
	Announced bool      `json:"announced"`
	CreatedAt time.Time `json:"createdAt"`
}

// sortScheduledHappyHours orders "at" happy hours by start time first,
// then "remaining" ones from the most time left to the least — roughly
// the order they'll start in.
func sortScheduledHappyHours(s []ScheduledHappyHour) {
	slices.SortStableFunc(s, func(a, b ScheduledHappyHour) int {
		if a.Trigger != b.Trigger {
			if a.Trigger == HappyHourTriggerAt {
				return -1
			}
			return 1
		}
		if a.Trigger == HappyHourTriggerAt {
			return a.StartAt.Compare(b.StartAt)
		}
		return b.RemainingSecs - a.RemainingSecs
	})
}

// ScheduledHappyHours returns this timer's pending happy hours, in
// roughly the order they'll start.
func (t *Timer) ScheduledHappyHours() []ScheduledHappyHour {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return slices.Clone(t.scheduledHappyHours)
}

// ScheduleHappyHour queues s to start later (see ScheduledHappyHour),
// returning it with its ID and CreatedAt filled in. The caller validates
// DurationSecs/Multiplier, the same as for an immediate happy hour.
func (t *Timer) ScheduleHappyHour(s ScheduledHappyHour) (ScheduledHappyHour, error) {
	now := time.Now()
	switch s.Trigger {
	case HappyHourTriggerAt:
		if !s.StartAt.After(now) {
			return ScheduledHappyHour{}, fmt.Errorf("%w: start time must be in the future", ErrInvalidHappyHourSchedule)
		}
		s.RemainingSecs = 0
	case HappyHourTriggerRemaining:
		if s.RemainingSecs <= 0 {
			return ScheduledHappyHour{}, fmt.Errorf("%w: time left must be positive", ErrInvalidHappyHourSchedule)
		}
		s.StartAt = time.Time{}
	default:
		return ScheduledHappyHour{}, fmt.Errorf("%w: unknown trigger %q", ErrInvalidHappyHourSchedule, s.Trigger)
	}
	s.ID = uuid.NewString()
	s.CreatedAt = now
	s.Multiplier = normalizeBoostMultiplier(s.Multiplier)

	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.scheduledHappyHours) >= MaxScheduledHappyHours {
		return ScheduledHappyHour{}, ErrTooManyScheduledHappyHours
	}
	if s.Trigger == HappyHourTriggerRemaining && t.remainingLocked(now) <= time.Duration(s.RemainingSecs)*time.Second {
		return ScheduledHappyHour{}, ErrHappyHourMarkPassed
	}

	if err := t.repo.InsertScheduledHappyHour(t.id, s); err != nil {
		return ScheduledHappyHour{}, fmt.Errorf("insert scheduled happy hour: %w", err)
	}
	t.scheduledHappyHours = append(t.scheduledHappyHours, s)
	sortScheduledHappyHours(t.scheduledHappyHours)
	t.happyHourScheduleVersion++
	return s, nil
}

// CancelScheduledHappyHour removes a pending happy hour before it starts.
func (t *Timer) CancelScheduledHappyHour(id string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	i := slices.IndexFunc(t.scheduledHappyHours, func(s ScheduledHappyHour) bool { return s.ID == id })
	if i < 0 {
		return ErrScheduledHappyHourNotFound
	}
	if err := t.repo.DeleteScheduledHappyHour(t.id, id); err != nil {
		return fmt.Errorf("delete scheduled happy hour: %w", err)
	}
	t.scheduledHappyHours = slices.Delete(t.scheduledHappyHours, i, i+1)
	t.happyHourScheduleVersion++
	return nil
}

// UpcomingHappyHour is a pending happy hour that has just come within
// the Discord "starting soon" lead time (see DueHappyHours.Upcoming).
// StartsIn is roughly how long until it starts: real time for an "at"
// one, clock time left above the mark for a "remaining" one.
type UpcomingHappyHour struct {
	ScheduledHappyHour
	StartsIn time.Duration
}

// DueHappyHours is what TakeDueHappyHours found on one check.
type DueHappyHours struct {
	// Start should be started now.
	Start []ScheduledHappyHour
	// Missed were dropped without starting.
	Missed []ScheduledHappyHour
	// Upcoming should get their one-time "starting soon" announcement.
	Upcoming []UpcomingHappyHour
}

// TakeDueHappyHours removes and returns every pending happy hour that's
// due as of now: an "at" one whose StartAt has arrived, or a "remaining"
// one whose mark the clock has reached. Nothing comes due while the
// timer is paused or locked (see SetLocked) — a due happy hour waits for
// Play/unlock instead, so e.g. Reset-ing below a mark while paused waits
// for Play. Some are dropped without starting (Missed) instead: "at"
// ones more than MissedHappyHourGrace late (including from waiting out a
// pause or lock that long), and "remaining" ones whose mark the clock
// reaches while locked (it keeps counting down while locked, so waiting
// for unlock could start one arbitrarily late). Each is removed from
// memory even if deleting its row fails (returned as err), so a broken
// database can't make it start again every tick.
//
// With a Discord webhook set and the heads-up notification on (see
// SetDiscordSettings),
// it also returns, as Upcoming, each still-pending one that has come
// within the lead time of starting, under the same running-and-unlocked
// condition — at most once per happy hour (ScheduledHappyHour.Announced
// is set and persisted), so a "remaining" one whose clock gets time
// added back above the lead window and then counts down into it again
// isn't announced twice.
func (t *Timer) TakeDueHappyHours(now time.Time) (DueHappyHours, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var out DueHappyHours
	if len(t.scheduledHappyHours) == 0 {
		return out, nil
	}

	remaining := t.remainingLocked(now)
	canStart := t.running && !t.locked
	var errs []error
	t.scheduledHappyHours = slices.DeleteFunc(t.scheduledHappyHours, func(s ScheduledHappyHour) bool {
		switch {
		case s.Trigger == HappyHourTriggerAt && now.Sub(s.StartAt) > MissedHappyHourGrace:
			out.Missed = append(out.Missed, s)
		case s.Trigger == HappyHourTriggerRemaining && t.locked && remaining <= time.Duration(s.RemainingSecs)*time.Second:
			out.Missed = append(out.Missed, s)
		case !canStart:
			return false
		case s.Trigger == HappyHourTriggerAt && !now.Before(s.StartAt):
			out.Start = append(out.Start, s)
		case s.Trigger == HappyHourTriggerRemaining && remaining <= time.Duration(s.RemainingSecs)*time.Second:
			out.Start = append(out.Start, s)
		default:
			return false
		}
		if err := t.repo.DeleteScheduledHappyHour(t.id, s.ID); err != nil {
			errs = append(errs, fmt.Errorf("delete scheduled happy hour %s: %w", s.ID, err))
		}
		return true
	})

	if canStart && t.discord.WebhookURL != "" && t.discord.Notifications.HeadsUp.Enabled && t.discord.HHLead > 0 {
		for i := range t.scheduledHappyHours {
			s := &t.scheduledHappyHours[i]
			if s.Announced {
				continue
			}
			var startsIn time.Duration
			if s.Trigger == HappyHourTriggerAt {
				startsIn = s.StartAt.Sub(now)
			} else {
				startsIn = remaining - time.Duration(s.RemainingSecs)*time.Second
			}
			if startsIn <= 0 || startsIn > t.discord.HHLead {
				continue
			}
			// Marked in memory regardless of the write, same reasoning as
			// the deletes above: never announce it every tick.
			s.Announced = true
			if err := t.repo.MarkScheduledHappyHourAnnounced(t.id, s.ID); err != nil {
				errs = append(errs, fmt.Errorf("mark scheduled happy hour %s announced: %w", s.ID, err))
			}
			out.Upcoming = append(out.Upcoming, UpcomingHappyHour{ScheduledHappyHour: *s, StartsIn: startsIn})
		}
	}

	if len(out.Start) > 0 || len(out.Missed) > 0 || len(out.Upcoming) > 0 {
		t.happyHourScheduleVersion++
	}
	return out, errors.Join(errs...)
}
