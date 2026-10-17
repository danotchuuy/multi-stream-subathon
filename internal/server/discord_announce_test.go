package server

import (
	"strings"
	"testing"
	"time"

	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

func TestRenderNotificationFillsOnlyItsOwnPlaceholders(t *testing.T) {
	vars := endVars("Big Stream", 3, time.Hour, 40*time.Minute)
	got := renderNotification(notifyEnd, "{timer}: {multiplier} is over {when} {nope}", vars)
	// {when} belongs to the heads-up and {nope} to nothing, so both stay
	// as written rather than vanishing.
	if want := "Big Stream: 3x is over {when} {nope}"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderNotificationBlankTemplateUsesDefault(t *testing.T) {
	got := renderNotification(notifyStart, "  ", startVars("T", 2, 30*time.Minute, time.Unix(1000, 0)))
	want := "🔥 **Happy Hour has started!** Time contributions are worth **2x** for the next 30 minutes."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := renderNotification(notifyStart, "ends {ends}", startVars("T", 2, time.Minute, time.Unix(1000, 0))); got != "ends <t:1000:R>" {
		t.Errorf("{ends} = %q, want a Discord relative timestamp", got)
	}
}

func TestHeadsUpWhenDependsOnTrigger(t *testing.T) {
	at := subathon.UpcomingHappyHour{ScheduledHappyHour: subathon.ScheduledHappyHour{
		Trigger: subathon.HappyHourTriggerAt, StartAt: time.Unix(5000, 0), DurationSecs: 1800, Multiplier: 2,
	}}
	if got := headsUpVars("T", at)["when"]; got != "<t:5000:R> (<t:5000:t>)" {
		t.Errorf("date & time {when} = %q", got)
	}
	left := subathon.UpcomingHappyHour{
		ScheduledHappyHour: subathon.ScheduledHappyHour{Trigger: subathon.HappyHourTriggerRemaining, RemainingSecs: 3600},
		StartsIn:           10 * time.Minute,
	}
	if got := headsUpVars("T", left)["when"]; got != "in about 10 minutes, when the timer hits 1:00:00" {
		t.Errorf("time-left {when} = %q", got)
	}
}

func TestNormalizeTemplatesStoresDefaultAsEmpty(t *testing.T) {
	n := normalizeTemplates(subathon.DiscordNotifications{
		Start: subathon.DiscordNotification{Template: defaultTemplates[notifyStart] + "\n"},
		End:   subathon.DiscordNotification{Template: "custom"},
	})
	if n.Start.Template != "" || n.End.Template != "custom" {
		t.Errorf("got %+v", n)
	}
}

func TestEveryPlaceholderIsDocumentedAndDefaultsUseOnlyTheirOwn(t *testing.T) {
	for kind, tmpl := range defaultTemplates {
		rendered := renderNotification(kind, tmpl, map[string]string{})
		if strings.Contains(rendered, "{") {
			t.Errorf("%s default leaves a placeholder unfilled: %q", kind, rendered)
		}
	}
}

func TestEndMessageCanStateTimeAdded(t *testing.T) {
	got := renderNotification(notifyEnd, "Happy Hour added {time_added} ({bonus_time} bonus)!",
		endVars("T", 2, time.Hour+5*time.Minute, 32*time.Minute+30*time.Second))
	if want := "Happy Hour added 1 hour 5 minutes (33 minutes bonus)!"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTwitchEndMessageStatesTimeAddedAndThanks(t *testing.T) {
	for _, tc := range []struct {
		added, bonus time.Duration
		want         string
	}{
		{time.Hour + 5*time.Minute, 32*time.Minute + 30*time.Second,
			"⏱️ Happy Hour has ended — time contributions are back to normal. It added 1 hour 5 minutes to the timer (33 minutes of that from the 2x boost). Thank you so much for all the support! 💜"},
		// Only manual contributions: time added, but no multiplier bonus.
		{10 * time.Minute, 0,
			"⏱️ Happy Hour has ended — time contributions are back to normal. It added 10 minutes to the timer. Thank you so much for all the support! 💜"},
		// Nothing came in: no "added 0 seconds", just the thanks.
		{0, 0,
			"⏱️ Happy Hour has ended — time contributions are back to normal. Thank you so much for all the support! 💜"},
	} {
		if got := twitchHappyHourEndMessage(2, tc.added, tc.bonus); got != tc.want {
			t.Errorf("added=%s bonus=%s:\n got %q\nwant %q", tc.added, tc.bonus, got, tc.want)
		}
	}
}

func TestDiscordEndDefaultStatesTimeAddedAndThanks(t *testing.T) {
	got := renderNotification(notifyEnd, "", endVars("T", 2, 45*time.Minute, 22*time.Minute+30*time.Second))
	want := "⏱️ **Happy Hour has ended** — time contributions are back to normal. It added **45 minutes** to the timer (23 minutes of that from the 2x boost). Thank you so much for all the support! 💜"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
