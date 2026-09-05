package streamelements

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"github.com/danotchuuy/multi-stream-subathon/internal/oauth"
	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
	"github.com/danotchuuy/multi-stream-subathon/internal/ws"
)

// pollInterval is how often a watched timer's tips are checked. Donations
// aren't as time-critical as a live sub/bits webhook, so this trades a
// little latency for a much simpler polling-based implementation (see
// package doc).
const pollInterval = 20 * time.Second

// tipsPerPoll is the max tips fetched per poll — comfortably above what a
// single 20s window should ever produce.
const tipsPerPoll = 50

// refreshMargin is how far ahead of its expiry an access token gets
// refreshed — comfortably more than one pollInterval, so a token never
// actually expires mid-poll.
const refreshMargin = 10 * time.Minute

// Poller runs one polling goroutine per timer that has a StreamElements
// account connected (see Timer.SetStreamElementsAccount), converting new
// tips into subathon.Event through that timer's reward/money rules for
// PlatformStreamElements — the same donation_unit rate the dashboard's
// manual "Add donation" form uses, just applied automatically instead of
// by hand.
type Poller struct {
	client   *Client
	provider *oauth.Provider // refreshes an about-to-expire access token; nil if StreamElements OAuth isn't configured
	hub      *ws.Hub

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // timer ID -> stop that timer's goroutine
}

func NewPoller(client *Client, provider *oauth.Provider, hub *ws.Hub) *Poller {
	return &Poller{client: client, provider: provider, hub: hub, cancels: make(map[string]context.CancelFunc)}
}

// StartAll begins watching every timer in manager that already has a
// StreamElements account connected — call once at server startup so a
// restart resumes polling for timers set up in a previous run.
func (p *Poller) StartAll(manager *subathon.Manager) {
	for _, t := range manager.All() {
		p.Watch(t)
	}
}

// Watch (re)starts polling t for tips, using its currently configured
// account. Safe to call repeatedly, e.g. right after a (re)connect — any
// previous goroutine for this timer is stopped first. A timer with no
// account configured is left unwatched (or, if it was previously watched,
// stops being watched) — also the case if this server has no
// StreamElements OAuth app configured (p.provider == nil), since there'd
// be no way to refresh a token past its expiry.
func (p *Poller) Watch(t *subathon.Timer) {
	p.Stop(t.ID())

	if p.provider == nil {
		return
	}

	token, refreshToken, expiresAt, channelID, _ := t.StreamElementsAccount()
	if token == "" || channelID == "" {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	p.cancels[t.ID()] = cancel
	p.mu.Unlock()

	go p.run(ctx, t, token, refreshToken, expiresAt, channelID)
}

// Stop stops watching timerID, if it was being watched. Called when the
// owner disconnects, and internally by Watch before restarting.
func (p *Poller) Stop(timerID string) {
	p.mu.Lock()
	cancel, ok := p.cancels[timerID]
	delete(p.cancels, timerID)
	p.mu.Unlock()
	if ok {
		cancel()
	}
}

// run polls timer t for new tips every pollInterval until ctx is
// cancelled (by Stop/Watch) — never replaying tips from before this
// goroutine started, same as a live webhook subscription only reporting
// events from the moment it's created. token/refreshToken/expiresAt are
// refreshed in place as the access token approaches expiry (see
// refreshMargin), persisting the new pair via
// Timer.SetStreamElementsTokens so a server restart picks up the latest
// one instead of the one this goroutine started with.
func (p *Poller) run(ctx context.Context, t *subathon.Timer, token, refreshToken string, expiresAt time.Time, channelID string) {
	since := time.Now()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if !expiresAt.IsZero() && time.Now().After(expiresAt.Add(-refreshMargin)) {
			newToken, newRefreshToken, newExpiresAt, err := p.provider.Refresh(ctx, refreshToken)
			if err != nil {
				log.Printf("streamelements: refresh access token for timer %s: %v", t.ID(), err)
				continue
			}
			if err := t.SetStreamElementsTokens(newToken, newRefreshToken, newExpiresAt); err != nil {
				log.Printf("streamelements: save refreshed token for timer %s: %v", t.ID(), err)
				continue
			}
			token, refreshToken, expiresAt = newToken, newRefreshToken, newExpiresAt
		}

		tips, err := p.client.RecentTips(token, channelID, since, tipsPerPoll)
		if err != nil {
			log.Printf("streamelements: poll tips for timer %s: %v", t.ID(), err)
			continue
		}
		if len(tips) == 0 {
			continue
		}

		rewardRules, err := t.RewardRules()
		if err != nil {
			log.Printf("streamelements: load reward rules for timer %s: %v", t.ID(), err)
			continue
		}
		moneyRules, err := t.MoneyRules()
		if err != nil {
			log.Printf("streamelements: load money rules for timer %s: %v", t.ID(), err)
			continue
		}
		secondsPerDollar := rewardRules[subathon.RewardDonation][subathon.PlatformStreamElements]
		dollarsPerDollar := moneyRules[subathon.RewardDonation][subathon.PlatformStreamElements]

		for _, tip := range tips {
			username := tip.Username
			if username == "" {
				username = "anonymous"
			}

			err := t.AddEvent(subathon.Event{
				Platform:     subathon.PlatformStreamElements,
				Type:         subathon.EventDonation,
				Username:     username,
				SecondsAdded: int(math.Round(tip.Amount * float64(secondsPerDollar))),
				MoneyAdded:   tip.Amount * dollarsPerDollar,
				Amount:       tip.Amount,
				Occurred:     tip.CreatedAt,
			})
			if err != nil {
				log.Printf("streamelements: record tip for timer %s: %v", t.ID(), err)
				continue
			}
			if tip.CreatedAt.After(since) {
				since = tip.CreatedAt
			}
		}

		p.hub.Broadcast(t.ID(), t.Snapshot())
	}
}
