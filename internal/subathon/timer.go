// Package subathon holds the platform-agnostic domain state for one or
// more subathon runs: each timer's running clock, contributor events, and
// snapshots, backed by a Repo for durability.
package subathon

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

const maxRecentEvents = 25

// OverlayColors customizes the public overlays' pill colors, as hex
// strings (e.g. "#111111") — set via Timer.SetOverlayColors from the
// dashboard's color pickers. TimerBg/TimerText/MoneyBg/MoneyText are the
// main overlay's (/overlay) timer and money-goal pills; GoalBg/GoalText
// are the goals overlay's (/goals-overlay) outer pill per milestone, and
// GoalAmountBg/GoalAmountText are the smaller dollar-amount pill nested
// inside each of those.
type OverlayColors struct {
	TimerBg   string `json:"timerBg"`
	TimerText string `json:"timerText"`
	MoneyBg   string `json:"moneyBg"`
	MoneyText string `json:"moneyText"`

	GoalBg         string `json:"goalBg"`
	GoalText       string `json:"goalText"`
	GoalAmountBg   string `json:"goalAmountBg"`
	GoalAmountText string `json:"goalAmountText"`
}

// DefaultOverlayColors is used for any color a timer hasn't customized.
func DefaultOverlayColors() OverlayColors {
	return OverlayColors{
		TimerBg:   "#111111",
		TimerText: "#ffffff",
		MoneyBg:   "#111111",
		MoneyText: "#ffffff",

		GoalBg:         "#111111",
		GoalText:       "#ffffff",
		GoalAmountBg:   "#ffffff",
		GoalAmountText: "#111111",
	}
}

// fillOverlayColorDefaults returns c with DefaultOverlayColors filled in
// for any field left empty.
func fillOverlayColorDefaults(c OverlayColors) OverlayColors {
	d := DefaultOverlayColors()
	if c.TimerBg == "" {
		c.TimerBg = d.TimerBg
	}
	if c.TimerText == "" {
		c.TimerText = d.TimerText
	}
	if c.MoneyBg == "" {
		c.MoneyBg = d.MoneyBg
	}
	if c.MoneyText == "" {
		c.MoneyText = d.MoneyText
	}
	if c.GoalBg == "" {
		c.GoalBg = d.GoalBg
	}
	if c.GoalText == "" {
		c.GoalText = d.GoalText
	}
	if c.GoalAmountBg == "" {
		c.GoalAmountBg = d.GoalAmountBg
	}
	if c.GoalAmountText == "" {
		c.GoalAmountText = d.GoalAmountText
	}
	return c
}

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

	// TotalMoneyRaised is the running total of every recorded event's
	// MoneyAdded. MoneyGoal is the configured target (see
	// Timer.SetMoneyGoal); 0 means no goal has been set.
	TotalMoneyRaised float64 `json:"totalMoneyRaised"`
	MoneyGoal        float64 `json:"moneyGoal,omitempty"`

	// TwitchChannel is the login name of the Twitch channel this timer is
	// configured to watch, if any (see Timer.SetTwitchChannel).
	TwitchChannel string `json:"twitchChannel,omitempty"`

	// KickChannel is the slug of the Kick channel this timer is
	// configured to watch, if any (see Timer.SetKickChannel).
	KickChannel string `json:"kickChannel,omitempty"`

	// Locked, when true, means new contributions are still recorded (and
	// still count toward TotalMoneyRaised) but no longer extend the
	// clock — see Timer.SetLocked.
	Locked bool `json:"locked"`

	// Hidden, when true, means the public overlay should render nothing
	// rather than the clock — see Timer.SetHidden. The dashboard always
	// shows the clock to the owner/moderators regardless.
	Hidden bool `json:"hidden"`

	// OverlayColors customizes the public overlay's timer/money-goal pill
	// colors — always populated (DefaultOverlayColors until customized),
	// never the zero value.
	OverlayColors OverlayColors `json:"overlayColors"`
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

	// locked, when true, means AddEvent still records contributions (and
	// still applies MoneyAdded) but stops applying SecondsAdded to the
	// clock. hidden, when true, means the public overlay should render
	// nothing. Both are toggled via the dashboard or a channel
	// moderator's "!timer lock"/"!timer unlock"/"!timer hide"/
	// "!timer unhide" chat command.
	locked bool
	hidden bool

	// totalMoneyRaised/moneyGoal mirror totalAdded/the clock, but for
	// money: totalMoneyRaised accumulates every event's MoneyAdded;
	// moneyGoal is the configured target (0 = none set).
	totalMoneyRaised float64
	moneyGoal        float64

	// twitchBroadcasterID/twitchBroadcasterUsername identify the Twitch
	// channel (if any) whose subs/gift-subs/cheers should add time to
	// this timer. Empty means none configured.
	twitchBroadcasterID       string
	twitchBroadcasterUsername string

	// kickBroadcasterID/kickBroadcasterUsername are the Kick equivalent.
	kickBroadcasterID       string
	kickBroadcasterUsername string

	// streamElementsToken/streamElementsChannelID/
	// streamElementsDisplayName identify the StreamElements account (if
	// any) this timer polls for tips — see SetStreamElementsAccount.
	// Unlike the Twitch/Kick fields above, the token is a secret and must
	// never appear in Snapshot or any other public response.
	streamElementsToken       string
	streamElementsChannelID   string
	streamElementsDisplayName string

	// overlayColors customizes the public overlay's pill colors; always
	// fully populated (see fillOverlayColorDefaults), never a zero value.
	overlayColors OverlayColors

	// moderatorIDs are the accounts (besides the owner) allowed to control
	// this timer: reset/resume/stop, add events, reward rules, watched
	// channels — everything except managing this list itself. See
	// AddModerator.
	moderatorIDs map[string]bool
}

func newTimer(repo Repo, rec TimerRecord, events []Event, moderatorIDs []string) *Timer {
	mods := make(map[string]bool, len(moderatorIDs))
	for _, id := range moderatorIDs {
		mods[id] = true
	}
	return &Timer{
		repo:                      repo,
		id:                        rec.ID,
		userID:                    rec.UserID,
		name:                      rec.Name,
		running:                   rec.Running,
		startedAt:                 rec.StartedAt,
		endsAt:                    rec.EndsAt,
		remaining:                 time.Duration(rec.RemainingSecs) * time.Second,
		totalAdded:                rec.TotalAddedSecs,
		events:                    events,
		locked:                    rec.Locked,
		hidden:                    rec.Hidden,
		totalMoneyRaised:          rec.TotalMoneyRaised,
		moneyGoal:                 rec.MoneyGoal,
		twitchBroadcasterID:       rec.TwitchBroadcasterID,
		twitchBroadcasterUsername: rec.TwitchBroadcasterUsername,
		kickBroadcasterID:         rec.KickBroadcasterID,
		kickBroadcasterUsername:   rec.KickBroadcasterUsername,
		streamElementsToken:       rec.StreamElementsToken,
		streamElementsChannelID:   rec.StreamElementsChannelID,
		streamElementsDisplayName: rec.StreamElementsDisplayName,
		overlayColors: fillOverlayColorDefaults(OverlayColors{
			TimerBg:   rec.OverlayTimerBg,
			TimerText: rec.OverlayTimerText,
			MoneyBg:   rec.OverlayMoneyBg,
			MoneyText: rec.OverlayMoneyText,

			GoalBg:         rec.OverlayGoalBg,
			GoalText:       rec.OverlayGoalText,
			GoalAmountBg:   rec.OverlayGoalAmountBg,
			GoalAmountText: rec.OverlayGoalAmountText,
		}),
		moderatorIDs: mods,
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

// IsModerator reports whether userID has been granted moderator access to
// this timer (see AddModerator). It does not consider the owner a
// moderator — callers that mean "owner or moderator" check UserID() too.
func (t *Timer) IsModerator(userID string) bool {
	if userID == "" {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.moderatorIDs[userID]
}

// ModeratorIDs returns the user IDs of every account with moderator
// access to this timer.
func (t *Timer) ModeratorIDs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ids := make([]string, 0, len(t.moderatorIDs))
	for id := range t.moderatorIDs {
		ids = append(ids, id)
	}
	return ids
}

// AddModerator grants userID the same control over this timer as its
// owner — reset/resume/stop, adding events, reward rules, watched
// channels — but not managing this moderator list itself. Idempotent.
func (t *Timer) AddModerator(userID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.repo.AddModerator(t.id, userID); err != nil {
		return fmt.Errorf("add moderator: %w", err)
	}
	if t.moderatorIDs == nil {
		t.moderatorIDs = make(map[string]bool)
	}
	t.moderatorIDs[userID] = true
	return nil
}

// RemoveModerator revokes userID's moderator access to this timer, if
// they had it.
func (t *Timer) RemoveModerator(userID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.repo.RemoveModerator(t.id, userID); err != nil {
		return fmt.Errorf("remove moderator: %w", err)
	}
	delete(t.moderatorIDs, userID)
	return nil
}

// TwitchChannel returns the Twitch broadcaster ID and login name this
// timer is configured to watch, or ("", "") if none.
func (t *Timer) TwitchChannel() (broadcasterID, username string) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.twitchBroadcasterID, t.twitchBroadcasterUsername
}

// SetTwitchChannel changes which Twitch channel's subs/gift-subs/cheers
// add time to this timer. Pass ("", "") to stop watching any channel.
func (t *Timer) SetTwitchChannel(broadcasterID, username string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.twitchBroadcasterID = broadcasterID
	t.twitchBroadcasterUsername = username

	return t.persistLocked()
}

// KickChannel returns the Kick broadcaster ID and slug this timer is
// configured to watch, or ("", "") if none.
func (t *Timer) KickChannel() (broadcasterID, username string) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.kickBroadcasterID, t.kickBroadcasterUsername
}

// SetKickChannel changes which Kick channel's subs/gift-subs add time to
// this timer. Pass ("", "") to stop watching any channel.
func (t *Timer) SetKickChannel(broadcasterID, username string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.kickBroadcasterID = broadcasterID
	t.kickBroadcasterUsername = username

	return t.persistLocked()
}

// StreamElementsAccount returns this timer's configured StreamElements
// JWT token, channel ID, and display name, or ("", "", "") if none is
// configured. The token is a secret — callers building any response sent
// to a client must not include it (see StreamElementsStatus, which
// deliberately omits it).
func (t *Timer) StreamElementsAccount() (token, channelID, displayName string) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.streamElementsToken, t.streamElementsChannelID, t.streamElementsDisplayName
}

// StreamElementsStatus reports whether a StreamElements account is
// connected and, if so, its display name — the public-safe view of
// StreamElementsAccount for API responses.
func (t *Timer) StreamElementsStatus() (connected bool, displayName string) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.streamElementsToken != "", t.streamElementsDisplayName
}

// SetStreamElementsAccount changes which StreamElements account this
// timer polls for tips. Pass ("", "", "") to disconnect. Callers are
// responsible for having already validated token via
// streamelements.Client.ResolveChannel (which is also how channelID/
// displayName get filled in) and for calling streamelements.Poller.Watch
// afterward to start/stop/restart polling — this method only persists the
// state.
func (t *Timer) SetStreamElementsAccount(token, channelID, displayName string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.streamElementsToken = token
	t.streamElementsChannelID = channelID
	t.streamElementsDisplayName = displayName

	return t.persistLocked()
}

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

// Locked reports whether this timer is currently locked (see SetLocked).
func (t *Timer) Locked() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.locked
}

// SetLocked locks or unlocks this timer. While locked, AddEvent still
// records contributions (and still counts their MoneyAdded toward the
// money goal), but stops applying their SecondsAdded to the clock — for
// e.g. freezing the countdown during a break without losing track of
// what came in during it. Doesn't itself pause the countdown; combine
// with Stop if that's also wanted.
func (t *Timer) SetLocked(locked bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.locked = locked
	return t.persistLocked()
}

// Hidden reports whether this timer is currently hidden from its public
// overlay (see SetHidden).
func (t *Timer) Hidden() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.hidden
}

// SetHidden hides or unhides this timer's public overlay. The dashboard
// (owner/moderator view) always shows the clock regardless.
func (t *Timer) SetHidden(hidden bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.hidden = hidden
	return t.persistLocked()
}

// OverlayColors returns this timer's configured overlay pill colors, with
// DefaultOverlayColors filled in for anything unset.
func (t *Timer) OverlayColors() OverlayColors {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.overlayColors
}

// SetOverlayColors changes the public overlay's pill colors. An empty
// field falls back to DefaultOverlayColors for that field.
func (t *Timer) SetOverlayColors(colors OverlayColors) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.overlayColors = fillOverlayColorDefaults(colors)
	return t.persistLocked()
}

// AddEvent records a contributor event and, unless the timer is locked
// (see SetLocked), extends the clock: the running end time if the
// countdown is live, or the frozen remaining time if it's stopped. e.ID
// is generated if empty.
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

	if !t.locked {
		added := time.Duration(e.SecondsAdded) * time.Second
		t.totalAdded += e.SecondsAdded
		if t.running {
			t.endsAt = t.endsAt.Add(added)
		} else {
			t.remaining += added
		}
	}
	t.totalMoneyRaised += e.MoneyAdded

	t.events = append(t.events, e)
	if len(t.events) > maxRecentEvents {
		t.events = t.events[len(t.events)-maxRecentEvents:]
	}

	if err := t.repo.InsertEvent(e); err != nil {
		return err
	}
	return t.persistLocked()
}

// AllEvents returns this timer's full contribution history, newest first
// — every sub/gift-sub/cheer/Kicks/manual event ever recorded against it,
// not just the recent ones kept in Snapshot.
func (t *Timer) AllEvents() ([]Event, error) {
	events, err := t.repo.AllEvents(t.id)
	if err != nil {
		return nil, fmt.Errorf("load event history: %w", err)
	}
	return events, nil
}

// RewardRules returns this timer's reward rules, filling in defaults for
// any item/platform pair that hasn't been explicitly configured.
func (t *Timer) RewardRules() (RewardRules, error) {
	saved, err := t.repo.RewardRules(t.id)
	if err != nil {
		return nil, fmt.Errorf("load reward rules: %w", err)
	}

	rules := DefaultRewardRules()
	for item, row := range saved {
		if rules[item] == nil {
			rules[item] = make(map[Platform]int, len(row))
		}
		for platform, secs := range row {
			rules[item][platform] = secs
		}
	}
	return rules, nil
}

// SaveRewardRules persists this timer's reward rules wholesale.
func (t *Timer) SaveRewardRules(rules RewardRules) error {
	if err := t.repo.SaveRewardRules(t.id, rules); err != nil {
		return fmt.Errorf("save reward rules: %w", err)
	}
	return nil
}

// MoneyRules returns this timer's money rules, filling in $0 defaults for
// any item/platform pair that hasn't been explicitly configured.
func (t *Timer) MoneyRules() (MoneyRules, error) {
	saved, err := t.repo.MoneyRules(t.id)
	if err != nil {
		return nil, fmt.Errorf("load money rules: %w", err)
	}

	rules := DefaultMoneyRules()
	for item, row := range saved {
		if rules[item] == nil {
			rules[item] = make(map[Platform]float64, len(row))
		}
		for platform, dollars := range row {
			rules[item][platform] = dollars
		}
	}
	return rules, nil
}

// SaveMoneyRules persists this timer's money rules wholesale.
func (t *Timer) SaveMoneyRules(rules MoneyRules) error {
	if err := t.repo.SaveMoneyRules(t.id, rules); err != nil {
		return fmt.Errorf("save money rules: %w", err)
	}
	return nil
}

// MoneyGoal returns this timer's configured dollar goal, or 0 if none is
// set.
func (t *Timer) MoneyGoal() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.moneyGoal
}

// SetMoneyGoal changes this timer's dollar goal. Pass 0 to clear it.
func (t *Timer) SetMoneyGoal(goal float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.moneyGoal = goal
	return t.persistLocked()
}

// SetMoneyRaised directly overrides the running dollar total, e.g. to
// reconcile against an external donation tracker instead of relying
// solely on recorded events. Unlike AddEvent, this doesn't add a history
// entry — it's a correction to the running total, not a contribution.
func (t *Timer) SetMoneyRaised(amount float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.totalMoneyRaised = amount
	return t.persistLocked()
}

// MoneyMilestones returns this timer's configured dollar-amount
// milestones, ordered by amount ascending.
func (t *Timer) MoneyMilestones() ([]MoneyMilestone, error) {
	milestones, err := t.repo.MoneyMilestones(t.id)
	if err != nil {
		return nil, fmt.Errorf("load money milestones: %w", err)
	}
	return milestones, nil
}

// SaveMoneyMilestones persists this timer's milestone list wholesale.
func (t *Timer) SaveMoneyMilestones(milestones []MoneyMilestone) error {
	if err := t.repo.SaveMoneyMilestones(t.id, milestones); err != nil {
		return fmt.Errorf("save money milestones: %w", err)
	}
	return nil
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
		ID:               t.id,
		Name:             t.name,
		Running:          t.running,
		StartedAt:        t.startedAt,
		EndsAt:           endsAt,
		RemainingSecs:    int(remaining.Seconds()),
		TotalAddedSecs:   t.totalAdded,
		RecentEvents:     events,
		TwitchChannel:    t.twitchBroadcasterUsername,
		KickChannel:      t.kickBroadcasterUsername,
		TotalMoneyRaised: t.totalMoneyRaised,
		MoneyGoal:        t.moneyGoal,
		Locked:           t.locked,
		Hidden:           t.hidden,
		OverlayColors:    t.overlayColors,
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
		ID:                        t.id,
		UserID:                    t.userID,
		Name:                      t.name,
		Running:                   t.running,
		StartedAt:                 t.startedAt,
		EndsAt:                    t.endsAt,
		RemainingSecs:             int(remaining.Seconds()),
		TotalAddedSecs:            t.totalAdded,
		TotalMoneyRaised:          t.totalMoneyRaised,
		MoneyGoal:                 t.moneyGoal,
		Locked:                    t.locked,
		Hidden:                    t.hidden,
		TwitchBroadcasterID:       t.twitchBroadcasterID,
		TwitchBroadcasterUsername: t.twitchBroadcasterUsername,
		KickBroadcasterID:         t.kickBroadcasterID,
		KickBroadcasterUsername:   t.kickBroadcasterUsername,
		StreamElementsToken:       t.streamElementsToken,
		StreamElementsChannelID:   t.streamElementsChannelID,
		StreamElementsDisplayName: t.streamElementsDisplayName,
		OverlayTimerBg:            t.overlayColors.TimerBg,
		OverlayTimerText:          t.overlayColors.TimerText,
		OverlayMoneyBg:            t.overlayColors.MoneyBg,
		OverlayMoneyText:          t.overlayColors.MoneyText,
		OverlayGoalBg:             t.overlayColors.GoalBg,
		OverlayGoalText:           t.overlayColors.GoalText,
		OverlayGoalAmountBg:       t.overlayColors.GoalAmountBg,
		OverlayGoalAmountText:     t.overlayColors.GoalAmountText,
		UpdatedAt:                 time.Now(),
	})
}
