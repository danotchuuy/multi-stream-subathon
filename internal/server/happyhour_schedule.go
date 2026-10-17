package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

// RunHappyHourScheduler starts each timer's scheduled happy hours (see
// subathon.Timer.ScheduleHappyHour) once they come due, checking every
// second until ctx is done. A due happy hour goes through startHappyHour,
// exactly like the dashboard's "Start Happy Hour" button, so it gets the
// same Twitch chat announcements. A paused or locked timer's due happy
// hours wait for Play/unlock (see subathon.Timer.TakeDueHappyHours). An
// ended timer is skipped entirely;
// its "at" happy hours whose start time passes meanwhile are dropped
// once it's reopened (see subathon.MissedHappyHourGrace). A time-left
// one whose mark is reached while locked is dropped too.
func RunHappyHourScheduler(ctx context.Context, deps Deps) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			for _, t := range deps.Manager.All() {
				if t.Ended() {
					continue
				}
				startDueHappyHours(deps, t, now)
			}
		}
	}
}

func startDueHappyHours(deps Deps, t *subathon.Timer, now time.Time) {
	found, err := t.TakeDueHappyHours(now)
	if err != nil {
		log.Printf("server: scheduled happy hours for timer %s: %v", t.ID(), err)
	}
	for _, s := range found.Missed {
		if s.Trigger == subathon.HappyHourTriggerAt {
			log.Printf("server: dropping scheduled happy hour %s on timer %s: start time %s passed %s ago",
				s.ID, t.ID(), s.StartAt.Format(time.RFC3339), now.Sub(s.StartAt).Round(time.Second))
		} else {
			log.Printf("server: dropping scheduled happy hour %s on timer %s: reached %s left while locked",
				s.ID, t.ID(), time.Duration(s.RemainingSecs)*time.Second)
		}
	}
	for _, u := range found.Upcoming {
		announceUpcomingHappyHour(deps, t, u)
	}

	if len(found.Start) == 0 {
		if len(found.Missed) > 0 || len(found.Upcoming) > 0 {
			// Push the bumped HappyHourScheduleVersion so dashboards refetch.
			deps.Hub.Broadcast(t.ID(), t.Snapshot())
		}
		return
	}

	broadcasterID, _ := t.TwitchChannel()
	// Two coming due on the same tick would just replace each other (see
	// Timer.StartTimeBoost), so only the last one in schedule order runs.
	s := found.Start[len(found.Start)-1]
	if err := startHappyHour(deps, t, broadcasterID, time.Duration(s.DurationSecs)*time.Second, s.Multiplier); err != nil {
		log.Printf("server: start scheduled happy hour %s on timer %s: %v", s.ID, t.ID(), err)
	}
}

type scheduleHappyHourRequest struct {
	// Trigger is "at" (start at StartAt) or "remaining" (start once the
	// countdown reaches RemainingSecs left).
	Trigger       subathon.HappyHourTrigger `json:"trigger"`
	StartAt       time.Time                 `json:"startAt,omitzero"`
	RemainingSecs int                       `json:"remainingSecs,omitempty"`
	// DurationSeconds/Multiplier are the same as startHappyHourRequest's.
	DurationSeconds int     `json:"durationSeconds"`
	Multiplier      float64 `json:"multiplier,omitempty"`
}

// handleListScheduledHappyHours returns this timer's pending happy hours.
// Owner/moderator only, unlike e.g. /milestone-alerts, so viewers holding
// the overlay token can't see a surprise happy hour coming.
func handleListScheduledHappyHours() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, timerFromContext(r).ScheduledHappyHours())
	}
}

// handleScheduleHappyHour queues a happy hour to start later (see
// subathon.Timer.ScheduleHappyHour), validating its duration and
// multiplier the same way handleStartHappyHour does.
func handleScheduleHappyHour(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req scheduleHappyHourRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.DurationSeconds <= 0 {
			http.Error(w, "durationSeconds must be positive", http.StatusBadRequest)
			return
		}
		if time.Duration(req.DurationSeconds)*time.Second > maxHHDuration {
			http.Error(w, fmt.Sprintf("a happy hour can last at most %s", maxHHDuration), http.StatusBadRequest)
			return
		}
		if req.Multiplier == 0 {
			req.Multiplier = subathon.DefaultBoostMultiplier
		}
		if req.Multiplier <= 1 || req.Multiplier > subathon.MaxBoostMultiplier {
			http.Error(w, fmt.Sprintf("multiplier must be greater than 1 and at most %g", subathon.MaxBoostMultiplier), http.StatusBadRequest)
			return
		}

		s, err := t.ScheduleHappyHour(subathon.ScheduledHappyHour{
			Trigger:       req.Trigger,
			StartAt:       req.StartAt,
			RemainingSecs: req.RemainingSecs,
			DurationSecs:  req.DurationSeconds,
			Multiplier:    req.Multiplier,
		})
		if err != nil {
			if errors.Is(err, subathon.ErrInvalidHappyHourSchedule) ||
				errors.Is(err, subathon.ErrHappyHourMarkPassed) ||
				errors.Is(err, subathon.ErrTooManyScheduledHappyHours) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			log.Printf("server: schedule happy hour for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to schedule happy hour", http.StatusInternalServerError)
			return
		}

		// Push the bumped HappyHourScheduleVersion so other open
		// dashboards refetch the list.
		deps.Hub.Broadcast(t.ID(), t.Snapshot())
		writeJSON(w, http.StatusCreated, s)
	}
}

// handleCancelScheduledHappyHour removes a pending happy hour before it
// starts.
func handleCancelScheduledHappyHour(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		if err := t.CancelScheduledHappyHour(chi.URLParam(r, "scheduleID")); err != nil {
			if errors.Is(err, subathon.ErrScheduledHappyHourNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			log.Printf("server: cancel scheduled happy hour for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to cancel scheduled happy hour", http.StatusInternalServerError)
			return
		}

		deps.Hub.Broadcast(t.ID(), t.Snapshot())
		w.WriteHeader(http.StatusNoContent)
	}
}
