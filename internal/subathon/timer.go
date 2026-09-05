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

// StatIconStyle selects which of two representations StatIcons' per-
// category fields hold: a native emoji character (whose color is fixed
// by the emoji font and can't be overridden) or a monochrome SVG icon
// key from the frontend's fixed set (see server.statSVGIconKeys), whose
// color is whatever the matching *Color field says.
type StatIconStyle string

const (
	StatIconStyleEmoji StatIconStyle = "emoji"
	StatIconStyleSVG   StatIconStyle = "svg"
)

// StatIcons customizes the overlay's rotating stat list's per-category
// icon — set via Timer.SetStatIcons from the styling page. Style picks
// which of two representations Subs/Bits/Donations hold (see
// StatIconStyle); SubsColor/BitsColor/DonationsColor and Outline only
// take effect when Style is StatIconStyleSVG, since an emoji's own color
// and fill can't be overridden. Outline swaps every SVG icon (hand-drawn
// shape or Unicode glyph alike) from a solid fill to a hollow stroke;
// unlike the colors, it's one setting shared across all three
// categories rather than per-category, since "solid vs. outline" is a
// single style choice, not a per-icon one. Always fully populated
// (DefaultStatIcons fills in anything unset, per Style), never a zero
// value.
type StatIcons struct {
	Style   StatIconStyle `json:"style"`
	Outline bool          `json:"outline"`

	Subs      string `json:"subs"`
	Bits      string `json:"bits"`
	Donations string `json:"donations"`

	SubsColor      string `json:"subsColor"`
	BitsColor      string `json:"bitsColor"`
	DonationsColor string `json:"donationsColor"`
}

// DefaultStatIcons is used for any field a timer hasn't customized.
func DefaultStatIcons() StatIcons {
	return StatIcons{
		Style:     StatIconStyleEmoji,
		Outline:   false,
		Subs:      "💜",
		Bits:      "💎",
		Donations: "💵",

		SubsColor:      "#ffffff",
		BitsColor:      "#ffffff",
		DonationsColor: "#ffffff",
	}
}

// defaultStatIconValue is Subs/Bits/Donations' default value for style —
// an emoji for StatIconStyleEmoji, an SVG icon key for StatIconStyleSVG.
// The two representations aren't interchangeable, so which default
// applies depends on which style ends up set, not just DefaultStatIcons'
// fixed emoji.
func defaultStatIconValue(style StatIconStyle, category string) string {
	if style == StatIconStyleSVG {
		switch category {
		case "subs":
			return "heart"
		case "bits":
			return "gem"
		case "donations":
			return "dollar"
		}
	}
	d := DefaultStatIcons()
	switch category {
	case "subs":
		return d.Subs
	case "bits":
		return d.Bits
	case "donations":
		return d.Donations
	}
	return ""
}

// fillStatIconDefaults returns c with defaults filled in for any field
// left empty — Style defaults to StatIconStyleEmoji, and each of
// Subs/Bits/Donations defaults per defaultStatIconValue(c.Style, ...)
// (so a field left empty gets a default matching whatever Style ended up
// set, not necessarily DefaultStatIcons' emoji).
func fillStatIconDefaults(c StatIcons) StatIcons {
	if c.Style == "" {
		c.Style = StatIconStyleEmoji
	}
	if c.Subs == "" {
		c.Subs = defaultStatIconValue(c.Style, "subs")
	}
	if c.Bits == "" {
		c.Bits = defaultStatIconValue(c.Style, "bits")
	}
	if c.Donations == "" {
		c.Donations = defaultStatIconValue(c.Style, "donations")
	}
	d := DefaultStatIcons()
	if c.SubsColor == "" {
		c.SubsColor = d.SubsColor
	}
	if c.BitsColor == "" {
		c.BitsColor = d.BitsColor
	}
	if c.DonationsColor == "" {
		c.DonationsColor = d.DonationsColor
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

	// YouTubeChannel/YouTubeChannelID are the title and ID of the
	// YouTube channel this timer is configured to watch, if any (see
	// Timer.SetYouTubeChannel) — unlike TwitchChannel/KickChannel, the ID
	// is exposed too since the frontend's picker (GET /api/youtube/
	// channels) is keyed by ID, not title, so it needs the ID (not just
	// the display title) to reselect the saved channel.
	YouTubeChannel   string `json:"youtubeChannel,omitempty"`
	YouTubeChannelID string `json:"youtubeChannelId,omitempty"`

	// Locked, when true, means new contributions are still recorded (and
	// still count toward TotalMoneyRaised) but no longer extend the
	// clock — see Timer.SetLocked.
	Locked bool `json:"locked"`

	// Hidden, when true, means the public overlay should render nothing
	// rather than the clock — see Timer.SetHidden. The dashboard always
	// shows the clock to the owner/moderators regardless.
	Hidden bool `json:"hidden"`

	// Ended, when true, means this timer no longer receives live
	// platform events or responds to "!timer ..." chat commands — see
	// Timer.SetEnded. Unlike Locked/Hidden, only reachable from the
	// dashboard, never chat.
	Ended bool `json:"ended"`

	// OverlayColors customizes the public overlay's timer/money-goal pill
	// colors — always populated (DefaultOverlayColors until customized),
	// never the zero value.
	OverlayColors OverlayColors `json:"overlayColors"`

	// SubsGiven/BitsGiven/DonationsGiven are running totals of this
	// timer's three "gift categories" (see contributionCounts), for the
	// overlay's optional rotating stat list. SubsGiven counts subs plus
	// gifted subs (a 5-sub gift counts as 5); BitsGiven counts bits/Kicks
	// contributed; DonationsGiven counts the number of tip/donation
	// events (not their dollar total, which the money pill already
	// shows).
	SubsGiven      int `json:"subsGiven"`
	BitsGiven      int `json:"bitsGiven"`
	DonationsGiven int `json:"donationsGiven"`

	// StatsRotationEnabled turns on the main overlay's rotating stat list
	// (far left of the timer/money pills), cycling through SubsGiven/
	// BitsGiven/DonationsGiven with an icon per category. Off by default
	// — see Timer.SetStatsRotationEnabled.
	StatsRotationEnabled bool `json:"statsRotationEnabled"`

	// StatIcons customizes each rotating stat list category's icon —
	// either an emoji or a colorable monochrome SVG icon, see
	// StatIconStyle — always populated (DefaultStatIcons until
	// customized), never the zero value.
	StatIcons StatIcons `json:"statIcons"`
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
	// "!timer show" chat command.
	locked bool
	hidden bool

	// ended, when true, means this timer stops receiving live platform
	// events and no longer responds to any "!timer ..." chat command —
	// see SetEnded. Dashboard-only: there is no chat command for it.
	ended bool

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

	// youtubeChannelID/youtubeChannelTitle identify the YouTube channel
	// (if any) whose Super Chats/Super Stickers/new members/gifted
	// memberships should add time to this timer. Empty means none
	// configured. Unlike twitchBroadcasterID/kickBroadcasterID, only a
	// channel whose owner has linked their YouTube identity to this app
	// can actually be watched — see Timer.SetYouTubeChannel.
	youtubeChannelID    string
	youtubeChannelTitle string

	// streamElementsToken/streamElementsRefreshToken/
	// streamElementsTokenExpiresAt/streamElementsChannelID/
	// streamElementsDisplayName identify the StreamElements account (if
	// any) this timer polls for tips — an OAuth2 access/refresh token
	// pair, see SetStreamElementsAccount. Unlike the Twitch/Kick fields
	// above, the tokens are secrets and must never appear in Snapshot or
	// any other public response.
	streamElementsToken          string
	streamElementsRefreshToken   string
	streamElementsTokenExpiresAt time.Time
	streamElementsChannelID      string
	streamElementsDisplayName    string

	// overlayColors customizes the public overlay's pill colors; always
	// fully populated (see fillOverlayColorDefaults), never a zero value.
	overlayColors OverlayColors

	// subsGiven/bitsGiven/donationsGiven mirror totalAdded/
	// totalMoneyRaised, but count contributions per "gift category"
	// instead of seconds/dollars — see Snapshot.SubsGiven and
	// contributionCounts. Maintained incrementally in AddEvent/
	// RemoveEvent, not recomputed from history.
	subsGiven      int
	bitsGiven      int
	donationsGiven int

	// statsRotationEnabled turns on the overlay's rotating stat list; see
	// SetStatsRotationEnabled.
	statsRotationEnabled bool

	// statIcons customizes each rotating stat list category's icon;
	// always fully populated (see fillStatIconDefaults), never a zero
	// value.
	statIcons StatIcons

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
		repo:                         repo,
		id:                           rec.ID,
		userID:                       rec.UserID,
		name:                         rec.Name,
		running:                      rec.Running,
		startedAt:                    rec.StartedAt,
		endsAt:                       rec.EndsAt,
		remaining:                    time.Duration(rec.RemainingSecs) * time.Second,
		totalAdded:                   rec.TotalAddedSecs,
		events:                       events,
		locked:                       rec.Locked,
		hidden:                       rec.Hidden,
		ended:                        rec.Ended,
		totalMoneyRaised:             rec.TotalMoneyRaised,
		moneyGoal:                    rec.MoneyGoal,
		twitchBroadcasterID:          rec.TwitchBroadcasterID,
		twitchBroadcasterUsername:    rec.TwitchBroadcasterUsername,
		kickBroadcasterID:            rec.KickBroadcasterID,
		kickBroadcasterUsername:      rec.KickBroadcasterUsername,
		youtubeChannelID:             rec.YouTubeChannelID,
		youtubeChannelTitle:          rec.YouTubeChannelTitle,
		streamElementsToken:          rec.StreamElementsToken,
		streamElementsRefreshToken:   rec.StreamElementsRefreshToken,
		streamElementsTokenExpiresAt: rec.StreamElementsTokenExpiresAt,
		streamElementsChannelID:      rec.StreamElementsChannelID,
		streamElementsDisplayName:    rec.StreamElementsDisplayName,
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
		subsGiven:            rec.SubsGiven,
		bitsGiven:            rec.BitsGiven,
		donationsGiven:       rec.DonationsGiven,
		statsRotationEnabled: rec.StatsRotationEnabled,
		statIcons: fillStatIconDefaults(StatIcons{
			Style:     StatIconStyle(rec.StatIconStyle),
			Outline:   rec.StatIconOutline,
			Subs:      rec.StatIconSubs,
			Bits:      rec.StatIconBits,
			Donations: rec.StatIconDonations,

			SubsColor:      rec.StatIconSubsColor,
			BitsColor:      rec.StatIconBitsColor,
			DonationsColor: rec.StatIconDonationsColor,
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

// YouTubeChannel returns the YouTube channel ID and title this timer is
// configured to watch, or ("", "") if none.
func (t *Timer) YouTubeChannel() (channelID, title string) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.youtubeChannelID, t.youtubeChannelTitle
}

// SetYouTubeChannel changes which YouTube channel's Super Chats/Super
// Stickers/new members/gifted memberships add time to this timer. Pass
// ("", "") to stop watching any channel. Unlike SetTwitchChannel/
// SetKickChannel, channelID must be a channel whose own owner has signed
// into this app (see youtube.Poller) — there's no app-level credential
// that works for an arbitrary channel, so callers (see
// handleSetYouTubeChannel) only ever offer already-linked channels
// rather than free text.
func (t *Timer) SetYouTubeChannel(channelID, title string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.youtubeChannelID = channelID
	t.youtubeChannelTitle = title

	return t.persistLocked()
}

// StreamElementsAccount returns this timer's configured StreamElements
// OAuth2 access token, refresh token, access-token expiry, channel ID,
// and display name, or the zero values if none is configured. The
// tokens are secrets — callers building any response sent to a client
// must not include them (see StreamElementsStatus, which deliberately
// omits them).
func (t *Timer) StreamElementsAccount() (token, refreshToken string, expiresAt time.Time, channelID, displayName string) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.streamElementsToken, t.streamElementsRefreshToken, t.streamElementsTokenExpiresAt,
		t.streamElementsChannelID, t.streamElementsDisplayName
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
// timer polls for tips, after completing an OAuth2 connect flow (see
// internal/server/streamelements_oauth.go). Pass zero values for every
// argument to disconnect. Callers are responsible for having already
// resolved channelID/displayName via streamelements.Client.
// ResolveChannel and for calling streamelements.Poller.Watch afterward
// to start/stop/restart polling — this method only persists the state.
func (t *Timer) SetStreamElementsAccount(token, refreshToken string, expiresAt time.Time, channelID, displayName string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.streamElementsToken = token
	t.streamElementsRefreshToken = refreshToken
	t.streamElementsTokenExpiresAt = expiresAt
	t.streamElementsChannelID = channelID
	t.streamElementsDisplayName = displayName

	return t.persistLocked()
}

// SetStreamElementsTokens updates just this timer's StreamElements
// access/refresh token pair, leaving its channel ID/display name
// unchanged — called by streamelements.Poller after refreshing an
// about-to-expire access token, as opposed to SetStreamElementsAccount's
// full (re)connect. A no-op (returns nil without persisting) if this
// timer isn't currently connected, e.g. the owner disconnected it while
// a refresh was in flight.
func (t *Timer) SetStreamElementsTokens(token, refreshToken string, expiresAt time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.streamElementsToken == "" {
		return nil
	}

	t.streamElementsToken = token
	t.streamElementsRefreshToken = refreshToken
	t.streamElementsTokenExpiresAt = expiresAt

	return t.persistLocked()
}

// Reset (re)initializes the countdown to the given duration, discarding
// whatever time was left on the clock. It leaves the timer stopped —
// call Resume (or the dashboard's Play) to start it counting down.
func (t *Timer) Reset(initial time.Duration) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.running = false
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

// Ended reports whether this timer has been ended (see SetEnded).
func (t *Timer) Ended() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.ended
}

// SetEnded ends or reopens this timer. Ending is a stronger, deliberately
// UI-only action than SetLocked — there is no "!timer end" chat command
// (see twitch.chatCommandNames), unlike every other toggle here — meant
// for shutting a subathon down for good rather than a temporary freeze:
//
//   - Manager.RunningByTwitchChannel/RunningByKickChannel/
//     TimersByTwitchChannel all exclude an ended timer, so it stops
//     receiving live contribution events and, since
//     TimersByTwitchChannel also backs chat command routing, stops
//     responding to any "!timer ..." command at all — both the
//     "de-register the listeners" and "chat commands no longer work"
//     halves of ending live entirely in those Manager queries rather
//     than here.
//   - The caller (handleSetEnded) additionally stops/resumes this
//     timer's StreamElements poller goroutine to match, since that's a
//     genuinely per-timer listener Manager's channel-keyed queries above
//     don't cover.
//
// Doesn't itself pause the countdown, change Locked/Hidden, or block
// manual events/dashboard controls — those stay independently
// controllable (e.g. to make a final correction) even once ended.
func (t *Timer) SetEnded(ended bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.ended = ended
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

// StatsRotationEnabled reports whether the overlay's rotating stat list
// (subs/bits-Kicks/donations, far left of the timer/money pills) is
// turned on for this timer.
func (t *Timer) StatsRotationEnabled() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.statsRotationEnabled
}

// SetStatsRotationEnabled turns the overlay's rotating stat list on or
// off.
func (t *Timer) SetStatsRotationEnabled(enabled bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.statsRotationEnabled = enabled
	return t.persistLocked()
}

// SetContributionCounts directly overrides the rotating stat list's three
// running totals (see Snapshot.SubsGiven), e.g. to reconcile against a
// count kept elsewhere, backfill totals from before this feature existed
// on a timer that predates the sqlite migration's backfill, or correct a
// miscount. Unlike AddEvent, this doesn't add a history entry or touch
// the clock/money total — it's a correction to these three totals only,
// same relationship SetMoneyRaised has to AddEvent's MoneyAdded.
func (t *Timer) SetContributionCounts(subsGiven, bitsGiven, donationsGiven int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.subsGiven = subsGiven
	t.bitsGiven = bitsGiven
	t.donationsGiven = donationsGiven
	return t.persistLocked()
}

// StatIcons returns this timer's configured rotating-stat-list icons,
// with defaults filled in for anything unset (see fillStatIconDefaults).
func (t *Timer) StatIcons() StatIcons {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.statIcons
}

// SetStatIcons changes each rotating stat list category's icon (and, in
// StatIconStyleSVG, its color). An empty field falls back to a default
// for that field (see fillStatIconDefaults).
func (t *Timer) SetStatIcons(icons StatIcons) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.statIcons = fillStatIconDefaults(icons)
	return t.persistLocked()
}

// contributionCounts reports how much e should add to subsGiven,
// bitsGiven, and donationsGiven — at most one of the three is nonzero
// per event, since EventType already partitions every real contribution
// into exactly one of these three "gift categories" (EventManual test
// events, which don't represent a real contribution kind, add to none).
// Subs/resubs/gifted subs and bits/Kicks sum e.Amount (the number of
// subs gifted, or the number of bits/Kicks) so e.g. a single 5-sub gift
// or 500-bit cheer counts as 5/500, not 1 — falling back to 1 if Amount
// wasn't set. Donations count events instead of summing Amount, since
// Amount there is a dollar figure the money pill already shows, not a
// "how many" count.
func contributionCounts(e Event) (subs, bits, donations int) {
	switch e.Type {
	case EventSub, EventResub, EventGiftedSub:
		return amountOrOne(e.Amount), 0, 0
	case EventBits:
		return 0, amountOrOne(e.Amount), 0
	case EventDonation:
		return 0, 0, 1
	default:
		return 0, 0, 0
	}
}

func amountOrOne(amount float64) int {
	if amount <= 0 {
		return 1
	}
	return int(amount)
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

	subs, bits, donations := contributionCounts(e)
	t.subsGiven += subs
	t.bitsGiven += bits
	t.donationsGiven += donations

	t.events = append(t.events, e)
	if len(t.events) > maxRecentEvents {
		t.events = t.events[len(t.events)-maxRecentEvents:]
	}

	if err := t.repo.InsertEvent(e); err != nil {
		return err
	}
	return t.persistLocked()
}

// RemoveEvent deletes a previously recorded event (e.g. a mistaken or
// fraudulent contribution caught after the fact) and reverses exactly what
// AddEvent did for it: the same SecondsAdded is subtracted from the
// running end time (or frozen remaining time) and totalAdded, and the same
// MoneyAdded is subtracted from totalMoneyRaised — so the clock and money
// goal end up exactly where they'd be had the event never been recorded.
// Returns an error if eventID isn't among this timer's recent events
// (Snapshot.RecentEvents/maxRecentEvents) — older history isn't
// removable this way, only what the dashboard's recent-contributors list
// can actually show a remove control for.
func (t *Timer) RemoveEvent(eventID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	idx := -1
	for i, e := range t.events {
		if e.ID == eventID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("event %s not found", eventID)
	}
	removed := t.events[idx]
	t.events = append(t.events[:idx:idx], t.events[idx+1:]...)

	removedSecs := time.Duration(removed.SecondsAdded) * time.Second
	t.totalAdded -= removed.SecondsAdded
	if t.running {
		t.endsAt = t.endsAt.Add(-removedSecs)
	} else {
		t.remaining -= removedSecs
	}
	t.totalMoneyRaised -= removed.MoneyAdded

	subs, bits, donations := contributionCounts(removed)
	t.subsGiven -= subs
	t.bitsGiven -= bits
	t.donationsGiven -= donations

	if err := t.repo.DeleteEvent(t.id, eventID); err != nil {
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
		ID:                   t.id,
		Name:                 t.name,
		Running:              t.running,
		StartedAt:            t.startedAt,
		EndsAt:               endsAt,
		RemainingSecs:        int(remaining.Seconds()),
		TotalAddedSecs:       t.totalAdded,
		RecentEvents:         events,
		TwitchChannel:        t.twitchBroadcasterUsername,
		KickChannel:          t.kickBroadcasterUsername,
		YouTubeChannel:       t.youtubeChannelTitle,
		YouTubeChannelID:     t.youtubeChannelID,
		TotalMoneyRaised:     t.totalMoneyRaised,
		MoneyGoal:            t.moneyGoal,
		Locked:               t.locked,
		Hidden:               t.hidden,
		Ended:                t.ended,
		OverlayColors:        t.overlayColors,
		SubsGiven:            t.subsGiven,
		BitsGiven:            t.bitsGiven,
		DonationsGiven:       t.donationsGiven,
		StatsRotationEnabled: t.statsRotationEnabled,
		StatIcons:            t.statIcons,
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
		ID:                           t.id,
		UserID:                       t.userID,
		Name:                         t.name,
		Running:                      t.running,
		StartedAt:                    t.startedAt,
		EndsAt:                       t.endsAt,
		RemainingSecs:                int(remaining.Seconds()),
		TotalAddedSecs:               t.totalAdded,
		TotalMoneyRaised:             t.totalMoneyRaised,
		MoneyGoal:                    t.moneyGoal,
		Locked:                       t.locked,
		Hidden:                       t.hidden,
		Ended:                        t.ended,
		TwitchBroadcasterID:          t.twitchBroadcasterID,
		TwitchBroadcasterUsername:    t.twitchBroadcasterUsername,
		KickBroadcasterID:            t.kickBroadcasterID,
		KickBroadcasterUsername:      t.kickBroadcasterUsername,
		YouTubeChannelID:             t.youtubeChannelID,
		YouTubeChannelTitle:          t.youtubeChannelTitle,
		StreamElementsToken:          t.streamElementsToken,
		StreamElementsRefreshToken:   t.streamElementsRefreshToken,
		StreamElementsTokenExpiresAt: t.streamElementsTokenExpiresAt,
		StreamElementsChannelID:      t.streamElementsChannelID,
		StreamElementsDisplayName:    t.streamElementsDisplayName,
		OverlayTimerBg:               t.overlayColors.TimerBg,
		OverlayTimerText:             t.overlayColors.TimerText,
		OverlayMoneyBg:               t.overlayColors.MoneyBg,
		OverlayMoneyText:             t.overlayColors.MoneyText,
		OverlayGoalBg:                t.overlayColors.GoalBg,
		OverlayGoalText:              t.overlayColors.GoalText,
		OverlayGoalAmountBg:          t.overlayColors.GoalAmountBg,
		OverlayGoalAmountText:        t.overlayColors.GoalAmountText,
		SubsGiven:                    t.subsGiven,
		BitsGiven:                    t.bitsGiven,
		DonationsGiven:               t.donationsGiven,
		StatsRotationEnabled:         t.statsRotationEnabled,
		StatIconStyle:                string(t.statIcons.Style),
		StatIconOutline:              t.statIcons.Outline,
		StatIconSubs:                 t.statIcons.Subs,
		StatIconBits:                 t.statIcons.Bits,
		StatIconDonations:            t.statIcons.Donations,
		StatIconSubsColor:            t.statIcons.SubsColor,
		StatIconBitsColor:            t.statIcons.BitsColor,
		StatIconDonationsColor:       t.statIcons.DonationsColor,
		UpdatedAt:                    time.Now(),
	})
}
