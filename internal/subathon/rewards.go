package subathon

// RewardItem identifies a category of contributor action that adds time to
// the clock, e.g. a specific sub tier or a per-unit bits/donation rate.
type RewardItem string

const (
	RewardTier1Sub  RewardItem = "tier1_sub"
	RewardTier2Sub  RewardItem = "tier2_sub"
	RewardTier3Sub  RewardItem = "tier3_sub"
	RewardGiftedSub RewardItem = "gifted_sub"
	RewardBits100   RewardItem = "bits_100"      // per 100 bits (Twitch) / Kicks (Kick)
	RewardDonation  RewardItem = "donation_unit" // per $1 donated/Super Chat'd
)

// RewardItems is every reward item, in display order.
var RewardItems = []RewardItem{
	RewardTier1Sub,
	RewardTier2Sub,
	RewardTier3Sub,
	RewardGiftedSub,
	RewardBits100,
	RewardDonation,
}

// RewardPlatforms is every platform reward rates are configured for. It
// excludes PlatformManual: manual test events specify their own seconds
// directly rather than looking up a rule. Only RewardDonation is ever
// actually looked up for PlatformStreamElements (see the streamelements
// package's poller) — the sub/bits items still get a configurable row for
// grid consistency, they just never fire for that platform.
var RewardPlatforms = []Platform{PlatformTwitch, PlatformKick, PlatformYouTube, PlatformStreamElements}

// RewardRules maps how many seconds a contribution is worth, keyed by
// [item][platform]. One set of rules is stored per user and applied when a
// platform listener translates a raw sub/bits/donation event into a
// subathon.Event.
type RewardRules map[RewardItem]map[Platform]int

// DefaultRewardRules returns a fresh set of reasonable starting values,
// used to fill in any item/platform pair a user hasn't configured yet.
func DefaultRewardRules() RewardRules {
	defaults := map[RewardItem]int{
		RewardTier1Sub:  300,
		RewardTier2Sub:  600,
		RewardTier3Sub:  1500,
		RewardGiftedSub: 300,
		RewardBits100:   60,
		RewardDonation:  60,
	}

	rules := make(RewardRules, len(RewardItems))
	for _, item := range RewardItems {
		row := make(map[Platform]int, len(RewardPlatforms))
		for _, p := range RewardPlatforms {
			row[p] = defaults[item]
		}
		rules[item] = row
	}
	return rules
}

// MoneyRules maps how many dollars a contribution is worth, keyed by
// [item][platform] — the money equivalent of RewardRules, tracked
// separately since a timer may care about elapsed time, dollars raised,
// or both. One set is stored per timer, same as RewardRules.
type MoneyRules map[RewardItem]map[Platform]float64

// DefaultMoneyRules returns every item/platform pair mapped to $0. Unlike
// DefaultRewardRules' non-zero defaults, money tracking is opt-in: a
// timer with no money rules explicitly configured raises $0 regardless of
// activity, rather than showing a dollar total nobody asked to track.
func DefaultMoneyRules() MoneyRules {
	rules := make(MoneyRules, len(RewardItems))
	for _, item := range RewardItems {
		row := make(map[Platform]float64, len(RewardPlatforms))
		for _, p := range RewardPlatforms {
			row[p] = 0
		}
		rules[item] = row
	}
	return rules
}

// MoneyMilestone is one dollar-amount checkpoint along the way to (or past)
// a timer's overall MoneyGoal, e.g. "$500: extra hour added". A timer can
// have any number of these, independent of whether MoneyGoal itself is
// set. Callers determine "reached" by comparing Amount against the
// timer's current TotalMoneyRaised — that isn't stored per milestone.
type MoneyMilestone struct {
	Amount float64 `json:"amount"`
	Label  string  `json:"label"`

	// Hidden marks a "surprise" milestone: the dashboard (owner/moderator
	// view) always shows Label as-is, but the public goals overlay
	// replaces it with a pulsing-dots placeholder instead of the real
	// text, while still showing the amount pill (and reached state)
	// normally. Purely cosmetic — doesn't affect AddEvent or totals.
	Hidden bool `json:"hidden"`
}
