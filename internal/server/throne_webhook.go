package server

import (
	"io"
	"log"
	"math"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/danotchuuy/multi-stream-subathon/internal/platform/throne"
	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

const maxThroneBodyBytes = 1 << 20 // 1MB: generous for a webhook payload

// handleThroneWebhook receives a Throne webhook notification for one
// specific timer (identified by {timerID} in the URL — see
// internal/platform/throne's package doc for why Throne needs a per-timer
// URL rather than a single shared endpoint like Twitch/Kick's). It
// verifies the request's Ed25519 signature, translates a gift/contribution
// into a subathon.Event using that timer's donation reward/money rules
// (subathon.RewardDonation, subathon.PlatformThrone — the same rate the
// dashboard's Reward settings page configures for other donation
// platforms), and broadcasts the result.
func handleThroneWebhook(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, ok := deps.Manager.Get(chi.URLParam(r, "timerID"))
		if !ok {
			http.Error(w, "timer not found", http.StatusNotFound)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxThroneBodyBytes))
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if !deps.Throne.VerifyMessage(r.Header, body) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}

		parsed, ok, err := throne.ParseNotification(body)
		if err != nil {
			log.Printf("throne: parse notification for timer %s: %v", t.ID(), err)
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		if !ok {
			w.WriteHeader(http.StatusOK)
			return
		}

		if parsed.EventID != "" && deps.Throne.SeenBefore(parsed.EventID) {
			w.WriteHeader(http.StatusOK) // Throne redelivery of a notification we already applied
			return
		}

		rewardRules, err := t.RewardRules()
		if err != nil {
			log.Printf("throne: load reward rules for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to load reward rules", http.StatusInternalServerError)
			return
		}
		moneyRules, err := t.MoneyRules()
		if err != nil {
			log.Printf("throne: load money rules for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to load money rules", http.StatusInternalServerError)
			return
		}
		secondsPerDollar := rewardRules[subathon.RewardDonation][subathon.PlatformThrone]
		dollarsPerDollar := moneyRules[subathon.RewardDonation][subathon.PlatformThrone]

		err = t.AddEvent(subathon.Event{
			Platform:     subathon.PlatformThrone,
			Type:         subathon.EventDonation,
			Username:     parsed.Username,
			SecondsAdded: int(math.Round(parsed.Amount * float64(secondsPerDollar))),
			MoneyAdded:   parsed.Amount * dollarsPerDollar,
			Amount:       parsed.Amount,
		})
		if err != nil {
			log.Printf("throne: add event to timer %s: %v", t.ID(), err)
			http.Error(w, "failed to record event", http.StatusInternalServerError)
			return
		}

		deps.Hub.Broadcast(t.ID(), t.Snapshot())
		w.WriteHeader(http.StatusOK)
	}
}
