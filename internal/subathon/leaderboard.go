package subathon

import (
	"fmt"
	"sort"
)

// leaderboardSize is how many contributors Leaderboard keeps per
// category.
const leaderboardSize = 10

// LeaderboardEntry is one contributor's total in a single "gift
// category" (see contributionCounts) — the same per-event counting the
// rotating stat list's SubsGiven/BitsGiven/DonationsGiven totals use,
// just broken out per contributor instead of summed across all of them.
type LeaderboardEntry struct {
	Username string `json:"username"`
	Amount   int    `json:"amount"`
}

// Leaderboard is the top leaderboardSize contributors in each of the
// three "gift categories", most-given first.
type Leaderboard struct {
	Subs      []LeaderboardEntry `json:"subs"`
	Bits      []LeaderboardEntry `json:"bits"`
	Donations []LeaderboardEntry `json:"donations"`
}

// Leaderboard computes the top leaderboardSize contributors in each
// gift category from this timer's full event history (see AllEvents) —
// unlike SubsGiven/BitsGiven/DonationsGiven, which are maintained
// incrementally for O(1) reads on every Snapshot, this re-scans the
// whole history on demand, so callers should fetch it on a page
// load/refresh interval rather than on every tick.
func (t *Timer) Leaderboard() (Leaderboard, error) {
	events, err := t.AllEvents()
	if err != nil {
		return Leaderboard{}, fmt.Errorf("load event history: %w", err)
	}

	subs := map[string]int{}
	bits := map[string]int{}
	donations := map[string]int{}
	for _, e := range events {
		s, b, d := contributionCounts(e)
		if s > 0 {
			subs[e.Username] += s
		}
		if b > 0 {
			bits[e.Username] += b
		}
		if d > 0 {
			donations[e.Username] += d
		}
	}

	return Leaderboard{
		Subs:      topContributors(subs),
		Bits:      topContributors(bits),
		Donations: topContributors(donations),
	}, nil
}

// topContributors returns the leaderboardSize highest-amount entries
// from totals, sorted by amount descending and, for a stable order
// between equal amounts, by username ascending.
func topContributors(totals map[string]int) []LeaderboardEntry {
	entries := make([]LeaderboardEntry, 0, len(totals))
	for username, amount := range totals {
		entries = append(entries, LeaderboardEntry{Username: username, Amount: amount})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Amount != entries[j].Amount {
			return entries[i].Amount > entries[j].Amount
		}
		return entries[i].Username < entries[j].Username
	})
	if len(entries) > leaderboardSize {
		entries = entries[:leaderboardSize]
	}
	return entries
}
