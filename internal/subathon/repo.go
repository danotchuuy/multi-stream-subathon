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

	// TotalMoneyRaised is the running total of every event's MoneyAdded,
	// mirroring TotalAddedSecs for time. MoneyGoal is the configured
	// target to raise toward; 0 means no goal set.
	TotalMoneyRaised float64
	MoneyGoal        float64

	// TwitchBroadcasterID/TwitchBroadcasterUsername identify the Twitch
	// channel (if any) this timer watches for live events. Empty means
	// none configured.
	TwitchBroadcasterID       string
	TwitchBroadcasterUsername string

	// KickBroadcasterID/KickBroadcasterUsername are the Kick equivalent.
	KickBroadcasterID       string
	KickBroadcasterUsername string

	// YouTubeChannelID/YouTubeChannelTitle identify the YouTube channel
	// (if any) this timer watches for live events (see
	// Timer.SetYouTubeChannel). Empty means none configured.
	YouTubeChannelID    string
	YouTubeChannelTitle string

	// StreamElementsToken/StreamElementsRefreshToken/
	// StreamElementsTokenExpiresAt/StreamElementsChannelID/
	// StreamElementsDisplayName identify the StreamElements account (if
	// any) this timer polls for tips (see Timer.SetStreamElementsAccount)
	// — an OAuth2 access/refresh token pair obtained via
	// internal/server/streamelements_oauth.go, refreshed by
	// streamelements.Poller as StreamElementsTokenExpiresAt approaches.
	// Unlike the Twitch/Kick fields above, StreamElementsToken/
	// StreamElementsRefreshToken are secrets and must never be returned
	// from any API response.
	StreamElementsToken          string
	StreamElementsRefreshToken   string
	StreamElementsTokenExpiresAt time.Time
	StreamElementsChannelID      string
	StreamElementsDisplayName    string

	// Locked/Hidden are toggled via the dashboard or a channel
	// moderator's "!timer lock"/"!timer unlock"/"!timer hide"/
	// "!timer show" chat command; see Timer.SetLocked/SetHidden.
	Locked bool
	Hidden bool

	// Ended is toggled from the dashboard only — see Timer.SetEnded.
	Ended bool

	// OverlayTimerBg/OverlayTimerText/OverlayMoneyBg/OverlayMoneyText/
	// OverlayGoalBg/OverlayGoalText/OverlayGoalAmountBg/
	// OverlayGoalAmountText are hex colors customizing the public
	// overlays' pills (see Timer.SetOverlayColors). Empty means not yet
	// customized — callers layer DefaultOverlayColors on top.
	OverlayTimerBg   string
	OverlayTimerText string
	OverlayMoneyBg   string
	OverlayMoneyText string

	OverlayGoalBg         string
	OverlayGoalText       string
	OverlayGoalAmountBg   string
	OverlayGoalAmountText string

	// SubsGiven/BitsGiven/DonationsGiven/StatsRotationEnabled back the
	// overlay's optional rotating stat list — see
	// Timer.contributionCounts and Snapshot.SubsGiven.
	SubsGiven            int
	BitsGiven            int
	DonationsGiven       int
	StatsRotationEnabled bool

	// StatIconStyle/StatIconSubs/StatIconBits/StatIconDonations are the
	// rotating stat list's per-category icon (see Timer.SetStatIcons) —
	// StatIconStyle is "emoji" or "svg", and Subs/Bits/Donations hold
	// either an emoji character or an SVG icon key depending on it.
	// StatIcon{Subs,Bits,Donations}Color are hex colors, and
	// StatIconOutline swaps every icon from filled to hollow-stroked —
	// both meaningful only when StatIconStyle is "svg". Empty/false means
	// not yet customized — callers layer DefaultStatIcons on top.
	StatIconStyle     string
	StatIconOutline   bool
	StatIconSubs      string
	StatIconBits      string
	StatIconDonations string

	StatIconSubsColor      string
	StatIconBitsColor      string
	StatIconDonationsColor string

	// PanelBg/PanelText/PanelAccentBg/PanelAccentText are hex colors
	// customizing the top-10 leaderboard panel (see Timer.SetPanelColors).
	// Empty means not yet customized — callers layer DefaultPanelColors
	// on top.
	PanelBg         string
	PanelText       string
	PanelAccentBg   string
	PanelAccentText string

	CreatedAt time.Time
	UpdatedAt time.Time
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

	// DeleteEvent removes a previously recorded event. Not an error if no
	// event with that ID exists for timerID.
	DeleteEvent(timerID, eventID string) error

	// RecentEvents returns up to limit of a timer's most recent events,
	// newest first.
	RecentEvents(timerID string, limit int) ([]Event, error)

	// AllEvents returns every event ever recorded against timerID, newest
	// first — the full contribution history, unlike RecentEvents' capped
	// view. Callers needing it sorted a different way or filtered to one
	// contributor do so themselves (see the /history page).
	AllEvents(timerID string) ([]Event, error)

	// RewardRules returns timerID's explicitly configured reward rules.
	// Implementations return an empty (not nil-erroring) map if the timer
	// hasn't had any saved yet; callers layer defaults on top.
	RewardRules(timerID string) (RewardRules, error)

	// SaveRewardRules replaces timerID's reward rules wholesale.
	SaveRewardRules(timerID string, rules RewardRules) error

	// MoneyRules returns timerID's explicitly configured money rules.
	// Implementations return an empty (not nil-erroring) map if the timer
	// hasn't had any saved yet; callers layer DefaultMoneyRules on top.
	MoneyRules(timerID string) (MoneyRules, error)

	// SaveMoneyRules replaces timerID's money rules wholesale.
	SaveMoneyRules(timerID string, rules MoneyRules) error

	// MoneyMilestones returns timerID's configured dollar-amount
	// milestones, ordered by amount ascending. Implementations return an
	// empty (not nil-erroring) slice if none have been saved yet.
	MoneyMilestones(timerID string) ([]MoneyMilestone, error)

	// SaveMoneyMilestones replaces timerID's milestone list wholesale.
	SaveMoneyMilestones(timerID string, milestones []MoneyMilestone) error

	// ListModerators returns the user IDs of every account with moderator
	// access to timerID (not including its owner).
	ListModerators(timerID string) ([]string, error)

	// AddModerator grants userID moderator access to timerID. Idempotent.
	AddModerator(timerID, userID string) error

	// RemoveModerator revokes userID's moderator access to timerID, if
	// they had it. Not an error if they didn't.
	RemoveModerator(timerID, userID string) error
}
