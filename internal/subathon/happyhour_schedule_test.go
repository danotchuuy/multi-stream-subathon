package subathon

import (
	"testing"
	"time"
)

// scheduleRepo is a Repo that only implements what scheduled happy
// hours touch; anything else panics via the nil embedded interface.
type scheduleRepo struct {
	Repo
	announced map[string]bool
}

func (r *scheduleRepo) SaveTimerState(TimerRecord) error                          { return nil }
func (r *scheduleRepo) InsertScheduledHappyHour(string, ScheduledHappyHour) error { return nil }
func (r *scheduleRepo) DeleteScheduledHappyHour(string, string) error             { return nil }
func (r *scheduleRepo) MarkScheduledHappyHourAnnounced(_, id string) error {
	r.announced[id] = true
	return nil
}

// newScheduleTimer returns a running timer with an hour left as of now,
// a Discord webhook, and a 10 minute heads-up lead time.
func newScheduleTimer(t *testing.T) (*Timer, *scheduleRepo, time.Time) {
	t.Helper()
	repo := &scheduleRepo{announced: map[string]bool{}}
	tm := newTimer(repo, TimerRecord{
		ID:                "timer",
		RemainingSecs:     3600,
		DiscordWebhookURL: "https://discord.com/api/webhooks/1/token",
		DiscordHHLeadMins: 10,
	}, nil, nil, nil)
	if err := tm.Resume(); err != nil {
		t.Fatal(err)
	}
	return tm, repo, time.Now()
}

func take(t *testing.T, tm *Timer, at time.Time) DueHappyHours {
	t.Helper()
	found, err := tm.TakeDueHappyHours(at)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestRemainingHeadsUpFiresOnceWhenTimeIsAddedBack(t *testing.T) {
	tm, repo, start := newScheduleTimer(t)
	s, err := tm.ScheduleHappyHour(ScheduledHappyHour{Trigger: HappyHourTriggerRemaining, RemainingSecs: 1800, DurationSecs: 600})
	if err != nil {
		t.Fatal(err)
	}

	// 3600s left, 1800s above the mark: outside the 10 minute window.
	if got := take(t, tm, start); len(got.Upcoming) != 0 || len(got.Start) != 0 {
		t.Fatalf("at 1h left: got %+v, want nothing", got)
	}

	// 2350s left, 550s above the mark: inside the window.
	got := take(t, tm, start.Add(1250*time.Second))
	if len(got.Upcoming) != 1 || got.Upcoming[0].ID != s.ID {
		t.Fatalf("entering window: got %+v, want one heads-up", got)
	}
	if d := got.Upcoming[0].StartsIn; d < 549*time.Second || d > 551*time.Second {
		t.Errorf("StartsIn = %s, want ~550s", d)
	}
	if !repo.announced[s.ID] {
		t.Error("heads-up wasn't persisted")
	}

	// Time added back above the window, then counting down into it again.
	tm.mu.Lock()
	tm.endsAt = tm.endsAt.Add(1200 * time.Second)
	tm.mu.Unlock()
	for _, after := range []time.Duration{1300 * time.Second, 2500 * time.Second, 2900 * time.Second} {
		if got := take(t, tm, start.Add(after)); len(got.Upcoming) != 0 || len(got.Start) != 0 {
			t.Fatalf("after time added back (+%s): got %+v, want nothing", after, got)
		}
	}

	// Reaching the mark still starts it, exactly once.
	got = take(t, tm, start.Add(3000*time.Second))
	if len(got.Start) != 1 || len(got.Upcoming) != 0 {
		t.Fatalf("at the mark: got %+v, want it to start", got)
	}
	if got := take(t, tm, start.Add(3001*time.Second)); len(got.Start) != 0 {
		t.Fatalf("after starting: got %+v, want nothing", got)
	}
}

func TestAtHeadsUpFiresOnceInsideLeadWindow(t *testing.T) {
	tm, _, start := newScheduleTimer(t)
	s, err := tm.ScheduleHappyHour(ScheduledHappyHour{Trigger: HappyHourTriggerAt, StartAt: start.Add(30 * time.Minute), DurationSecs: 600})
	if err != nil {
		t.Fatal(err)
	}

	if got := take(t, tm, start.Add(19*time.Minute)); len(got.Upcoming) != 0 {
		t.Fatalf("11m before: got %+v, want nothing", got)
	}
	if got := take(t, tm, start.Add(21*time.Minute)); len(got.Upcoming) != 1 || got.Upcoming[0].ID != s.ID {
		t.Fatalf("9m before: got %+v, want one heads-up", got)
	}
	if got := take(t, tm, start.Add(25*time.Minute)); len(got.Upcoming) != 0 {
		t.Fatalf("5m before: got %+v, want no repeat", got)
	}
	if got := take(t, tm, start.Add(30*time.Minute)); len(got.Start) != 1 {
		t.Fatalf("at start time: got %+v, want it to start", got)
	}
}

func TestHeadsUpWaitsWhilePausedOrLockedAndNeedsWebhook(t *testing.T) {
	tm, repo, start := newScheduleTimer(t)
	s, err := tm.ScheduleHappyHour(ScheduledHappyHour{Trigger: HappyHourTriggerAt, StartAt: start.Add(5 * time.Minute), DurationSecs: 600})
	if err != nil {
		t.Fatal(err)
	}

	if err := tm.SetLocked(true); err != nil {
		t.Fatal(err)
	}
	if got := take(t, tm, start); len(got.Upcoming) != 0 {
		t.Fatalf("locked: got %+v, want no heads-up", got)
	}
	if err := tm.SetLocked(false); err != nil {
		t.Fatal(err)
	}

	if err := tm.Stop(); err != nil {
		t.Fatal(err)
	}
	if got := take(t, tm, start); len(got.Upcoming) != 0 {
		t.Fatalf("paused: got %+v, want no heads-up", got)
	}
	if err := tm.Resume(); err != nil {
		t.Fatal(err)
	}

	if err := tm.SetDiscordSettings(DiscordSettings{HHLead: 10 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	if got := take(t, tm, start); len(got.Upcoming) != 0 || repo.announced[s.ID] {
		t.Fatalf("no webhook: got %+v (announced=%v), want no heads-up and not marked", got, repo.announced[s.ID])
	}

	// Once possible, it goes out (late, but still before the start).
	if err := tm.SetDiscordSettings(DiscordSettings{
		WebhookURL:    "https://discord.com/api/webhooks/1/token",
		HHLead:        10 * time.Minute,
		Notifications: DiscordNotifications{HeadsUp: DiscordNotification{Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	if got := take(t, tm, start); len(got.Upcoming) != 1 {
		t.Fatalf("running, unlocked, webhook set: got %+v, want one heads-up", got)
	}
}

func TestEndTimeBoostEndsItEarlyAndInvalidatesTheOriginalExpiry(t *testing.T) {
	tm, _, _ := newScheduleTimer(t)
	if err := tm.EndTimeBoost(); err != ErrNoTimeBoost {
		t.Fatalf("ending with none active: err = %v, want ErrNoTimeBoost", err)
	}

	if err := tm.StartTimeBoost(30*time.Minute, 3); err != nil {
		t.Fatal(err)
	}
	originalEnd := tm.TimeBoostEndsAt()

	if err := tm.EndTimeBoost(); err != nil {
		t.Fatal(err)
	}
	if active, _ := tm.TimeBoostActive(); active {
		t.Error("still active after EndTimeBoost")
	}
	if snap := tm.Snapshot(); snap.BoostActive {
		t.Error("snapshot still shows the boost as active")
	}
	// The pending end announcement for the original expiry compares
	// against this, so it must have moved for that announcement to skip.
	if tm.TimeBoostEndsAt().Equal(originalEnd) {
		t.Error("TimeBoostEndsAt unchanged, so the original end announcement would still fire")
	}
	if err := tm.EndTimeBoost(); err != ErrNoTimeBoost {
		t.Errorf("ending twice: err = %v, want ErrNoTimeBoost", err)
	}
}

func TestHappyHourTallyCountsTimeAddedDuringIt(t *testing.T) {
	tm, _, _ := newScheduleTimer(t)
	add := func(typ EventType, secs int) Event {
		t.Helper()
		e := Event{Type: typ, Username: "viewer", SecondsAdded: secs}
		if err := tm.AddEvent(e); err != nil {
			t.Fatal(err)
		}
		return tm.Snapshot().RecentEvents[len(tm.Snapshot().RecentEvents)-1]
	}
	tally := func() (int, int) {
		added, bonus := tm.HappyHourTally()
		return int(added / time.Second), int(bonus / time.Second)
	}

	add(EventSub, 60) // before any happy hour: not counted
	if err := tm.StartTimeBoost(30*time.Minute, 2); err != nil {
		t.Fatal(err)
	}
	sub := add(EventSub, 60) // boosted to 120: 60 of it bonus
	add(EventManual, 30)     // added time, never boosted
	add(EventDonation, 45)   // boosted to 90
	if added, bonus := tally(); added != 240 || bonus != 105 {
		t.Fatalf("after 3 contributions: added=%d bonus=%d, want 240/105", added, bonus)
	}

	// While locked nothing reaches the clock, so nothing is tallied.
	if err := tm.SetLocked(true); err != nil {
		t.Fatal(err)
	}
	add(EventSub, 60)
	if err := tm.SetLocked(false); err != nil {
		t.Fatal(err)
	}
	if added, bonus := tally(); added != 240 || bonus != 105 {
		t.Fatalf("after a locked contribution: added=%d bonus=%d, want unchanged 240/105", added, bonus)
	}

	// Removing a happy hour contribution takes it back out of the tally.
	if err := tm.RemoveEvent(sub.ID); err != nil {
		t.Fatal(err)
	}
	if added, bonus := tally(); added != 120 || bonus != 45 {
		t.Fatalf("after removing the sub: added=%d bonus=%d, want 120/45", added, bonus)
	}

	// After it ends, the tally stays for the end announcement but stops growing.
	if err := tm.EndTimeBoost(); err != nil {
		t.Fatal(err)
	}
	add(EventSub, 60)
	if added, bonus := tally(); added != 120 || bonus != 45 {
		t.Fatalf("after ending: added=%d bonus=%d, want unchanged 120/45", added, bonus)
	}

	// A new happy hour starts from zero.
	if err := tm.StartTimeBoost(time.Minute, 3); err != nil {
		t.Fatal(err)
	}
	if added, bonus := tally(); added != 0 || bonus != 0 {
		t.Fatalf("new happy hour: added=%d bonus=%d, want 0/0", added, bonus)
	}
}

func (r *scheduleRepo) InsertEvent(Event) error          { return nil }
func (r *scheduleRepo) DeleteEvent(string, string) error { return nil }
