package youtube

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	"github.com/danotchuuy/multi-stream-subathon/internal/auth"
	"github.com/danotchuuy/multi-stream-subathon/internal/oauth"
	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
	"github.com/danotchuuy/multi-stream-subathon/internal/ws"
	"github.com/danotchuuy/multi-stream-subathon/internal/youtubepb"
)

// refreshMargin is how far ahead of its expiry an access token gets
// refreshed — well ahead of a live chat stream's normal lifetime, so a
// token never actually expires mid-stream.
const refreshMargin = 10 * time.Minute

// noBroadcastRetryInterval is how often Poller checks whether a watched
// channel has gone live, while it hasn't.
const noBroadcastRetryInterval = time.Minute

// streamReconnectDelay is the backoff before re-opening the chat stream
// (or re-checking for a live broadcast) after it ends or errors.
const streamReconnectDelay = 10 * time.Second

// Poller runs one goroutine per timer that has a YouTube channel
// configured (see Timer.SetYouTubeChannel), converting that channel's
// Super Chats/Super Stickers/new members/gifted memberships into
// subathon.Event through that timer's reward/money rules for
// PlatformYouTube — the same donation_unit/tier1_sub/gifted_sub rates
// the dashboard's manual "Add donation" form uses, just applied
// automatically instead of by hand. Each goroutine alternates between
// checking whether the channel is currently live (it usually isn't) and,
// once it is, holding open a liveChatMessages.streamList gRPC connection
// until the broadcast ends or the stream errors.
type Poller struct {
	client   *Client
	provider *oauth.Provider // refreshes an about-to-expire access token; nil if YouTube OAuth isn't configured
	auth     *auth.Service   // resolves a watched channel to its linked owner's own tokens
	hub      *ws.Hub

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // timer ID -> stop that timer's goroutine
}

func NewPoller(client *Client, provider *oauth.Provider, authSvc *auth.Service, hub *ws.Hub) *Poller {
	return &Poller{client: client, provider: provider, auth: authSvc, hub: hub, cancels: make(map[string]context.CancelFunc)}
}

// StartAll begins watching every timer in manager that already has a
// YouTube channel configured — call once at server startup so a restart
// resumes watching for timers set up in a previous run.
func (p *Poller) StartAll(manager *subathon.Manager) {
	for _, t := range manager.All() {
		p.Watch(t)
	}
}

// Watch (re)starts watching t's configured YouTube channel. Safe to call
// repeatedly, e.g. right after the channel is (re)saved — any previous
// goroutine for this timer is stopped first. A timer with no channel
// configured is left unwatched (or, if it was previously watched, stops
// being watched) — also the case if this server has no YouTube OAuth app
// configured (p.provider == nil), since there'd be no way to refresh a
// token past its expiry.
func (p *Poller) Watch(t *subathon.Timer) {
	p.Stop(t.ID())

	if p.provider == nil {
		return
	}

	channelID, _ := t.YouTubeChannel()
	if channelID == "" {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	p.cancels[t.ID()] = cancel
	p.mu.Unlock()

	go p.run(ctx, t, channelID)
}

// Stop stops watching timerID, if it was being watched. Called when the
// owner clears the channel, and internally by Watch before restarting.
func (p *Poller) Stop(timerID string) {
	p.mu.Lock()
	cancel, ok := p.cancels[timerID]
	delete(p.cancels, timerID)
	p.mu.Unlock()
	if ok {
		cancel()
	}
}

// run alternates between checking whether channelID is currently live and
// streaming its chat while it is, until ctx is cancelled (by Stop/Watch).
// The access/refresh token pair comes from whichever account has linked
// channelID as their own YouTube identity (see auth.Service.
// IdentityByPlatformUser) — resolved once per run, not re-resolved if
// that link changes mid-run (matched by a fresh Watch call, same as every
// other platform integration here).
func (p *Poller) run(ctx context.Context, t *subathon.Timer, channelID string) {
	ident, ok, err := p.auth.IdentityByPlatformUser(auth.PlatformYouTube, channelID)
	if err != nil {
		log.Printf("youtube: look up linked identity for channel %s (timer %s): %v", channelID, t.ID(), err)
		return
	}
	if !ok {
		log.Printf("youtube: channel %s (timer %s) isn't linked to any account here — its owner needs to sign in with that YouTube channel before it can be watched", channelID, t.ID())
		return
	}

	identityID := ident.ID
	token, refreshToken, expiresAt := ident.AccessToken, ident.RefreshToken, ident.TokenExpiresAt

	for {
		if !expiresAt.IsZero() && time.Now().After(expiresAt.Add(-refreshMargin)) {
			newToken, newRefreshToken, newExpiresAt, err := p.provider.Refresh(ctx, refreshToken)
			if err != nil {
				log.Printf("youtube: refresh access token for channel %s (timer %s): %v", channelID, t.ID(), err)
				if !sleepOrDone(ctx, noBroadcastRetryInterval) {
					return
				}
				continue
			}
			// Google normally omits refresh_token here — the original
			// keeps working — so only overwrite it if a new one actually
			// came back.
			if newRefreshToken == "" {
				newRefreshToken = refreshToken
			}
			if err := p.auth.RefreshIdentityTokens(identityID, newToken, newRefreshToken, newExpiresAt); err != nil {
				log.Printf("youtube: save refreshed token for channel %s (timer %s): %v", channelID, t.ID(), err)
			}
			token, refreshToken, expiresAt = newToken, newRefreshToken, newExpiresAt
		}

		liveChatID, live, err := p.client.FindActiveLiveChat(ctx, token)
		if err != nil {
			log.Printf("youtube: check live status for channel %s (timer %s): %v", channelID, t.ID(), err)
		}
		if !live {
			if !sleepOrDone(ctx, noBroadcastRetryInterval) {
				return
			}
			continue
		}

		if err := p.streamChat(ctx, t, token, liveChatID); err != nil && ctx.Err() == nil {
			log.Printf("youtube: chat stream for channel %s (timer %s): %v", channelID, t.ID(), err)
		}
		// Loop back regardless of how streamChat ended — the broadcast
		// may have ended normally (stream closed) or errored; re-checking
		// for an active live chat next covers both, same as the browser-
		// facing version of this reconnect loop in multi-stream-moderation.
		if !sleepOrDone(ctx, streamReconnectDelay) {
			return
		}
	}
}

// streamChat opens one liveChatMessages.streamList RPC for liveChatID and
// applies every text-adjacent event (Super Chat, Super Sticker, new
// member, gifted memberships) it delivers until the stream ends or ctx is
// cancelled.
func (p *Poller) streamChat(ctx context.Context, t *subathon.Timer, token, liveChatID string) error {
	client, err := p.client.chatClient()
	if err != nil {
		return err
	}

	req := &youtubepb.LiveChatMessageListRequest{
		LiveChatId: proto.String(liveChatID),
		Part:       []string{"snippet", "authorDetails"},
	}
	md := metadata.Pairs("authorization", "Bearer "+token)
	stream, err := client.StreamList(metadata.NewOutgoingContext(ctx, md), req)
	if err != nil {
		return err
	}

	for {
		resp, err := stream.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return nil // Stop/Watch cancelled us, not a real error
			}
			return err
		}

		rewardRules, err := t.RewardRules()
		if err != nil {
			log.Printf("youtube: load reward rules for timer %s: %v", t.ID(), err)
			continue
		}
		moneyRules, err := t.MoneyRules()
		if err != nil {
			log.Printf("youtube: load money rules for timer %s: %v", t.ID(), err)
			continue
		}

		changed := false
		for _, item := range resp.GetItems() {
			if p.applyMessage(t, item, rewardRules, moneyRules) {
				changed = true
			}
		}
		if changed {
			p.hub.Broadcast(t.ID(), t.Snapshot())
		}
	}
}

// applyMessage translates one chat item into a subathon.Event and records
// it, if it's a kind this app tracks (Super Chat, Super Sticker, new
// member, or membership gifting — see the package doc). Everything else,
// including plain text chat, gift membership *received* notices (the
// per-recipient echo of a membership-gifting purchase — counting those
// too would double-count the same purchase already reported by
// MEMBERSHIP_GIFTING_EVENT), bans, and polls, is silently ignored.
// Reports whether an event was actually recorded, so the caller only
// broadcasts a new snapshot when something changed.
func (p *Poller) applyMessage(t *subathon.Timer, item *youtubepb.LiveChatMessage, rewardRules subathon.RewardRules, moneyRules subathon.MoneyRules) bool {
	snippet := item.GetSnippet()
	author := item.GetAuthorDetails()
	username := author.GetDisplayName()
	if username == "" {
		username = "anonymous"
	}

	var (
		eventType  subathon.EventType
		rewardItem subathon.RewardItem
		count      float64 // dollars for Super Chat/Sticker, a flat 1 for a new member, gift count for gifted memberships
	)

	switch snippet.GetType() {
	case youtubepb.LiveChatMessageSnippet_TypeWrapper_SUPER_CHAT_EVENT:
		eventType, rewardItem = subathon.EventDonation, subathon.RewardDonation
		count = float64(snippet.GetSuperChatDetails().GetAmountMicros()) / 1_000_000

	case youtubepb.LiveChatMessageSnippet_TypeWrapper_SUPER_STICKER_EVENT:
		eventType, rewardItem = subathon.EventDonation, subathon.RewardDonation
		count = float64(snippet.GetSuperStickerDetails().GetAmountMicros()) / 1_000_000

	case youtubepb.LiveChatMessageSnippet_TypeWrapper_NEW_SPONSOR_EVENT:
		// "Sponsor" is YouTube's old term for "member" (see
		// LiveChatNewSponsorDetails' own doc comment). YouTube has no
		// Twitch-style sub tiers in this app's reward model (see the
		// Rewards page's ITEMS table), so this always prices as the flat
		// tier1_sub rate regardless of which membership level name
		// YouTube reports.
		eventType, rewardItem, count = subathon.EventSub, subathon.RewardTier1Sub, 1

	case youtubepb.LiveChatMessageSnippet_TypeWrapper_MEMBERSHIP_GIFTING_EVENT:
		giftCount := snippet.GetMembershipGiftingDetails().GetGiftMembershipsCount()
		if giftCount <= 0 {
			return false
		}
		eventType, rewardItem, count = subathon.EventGiftedSub, subathon.RewardGiftedSub, float64(giftCount)

	default:
		return false
	}

	secondsAdded := int(math.Round(count * float64(rewardRules[rewardItem][subathon.PlatformYouTube])))
	moneyAdded := count * moneyRules[rewardItem][subathon.PlatformYouTube]
	if secondsAdded <= 0 && moneyAdded <= 0 {
		return false
	}

	occurred := time.Now()
	if ts, err := time.Parse(time.RFC3339, snippet.GetPublishedAt()); err == nil {
		occurred = ts
	}

	if err := t.AddEvent(subathon.Event{
		Platform:     subathon.PlatformYouTube,
		Type:         eventType,
		Username:     username,
		SecondsAdded: secondsAdded,
		MoneyAdded:   moneyAdded,
		Amount:       count,
		Occurred:     occurred,
	}); err != nil {
		log.Printf("youtube: record event for timer %s: %v", t.ID(), err)
		return false
	}
	return true
}

// sleepOrDone waits for d, returning false early (without waiting) if ctx
// is cancelled first — callers treat false as "stop, don't loop again".
func sleepOrDone(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
