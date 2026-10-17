package subathon

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDiscordNotificationsMigrateFromSinglePingSetting(t *testing.T) {
	got := discordNotificationsFromRecord(TimerRecord{DiscordHHLeadMins: 15, DiscordPingEveryone: true})
	want := DiscordNotifications{
		HeadsUp: DiscordNotification{Enabled: true, PingEveryone: true},
		Start:   DiscordNotification{Enabled: true, PingEveryone: true},
		End:     DiscordNotification{Enabled: true, PingEveryone: true},
	}
	if got != want {
		t.Errorf("with lead and ping: got %+v, want %+v", got, want)
	}

	if got := discordNotificationsFromRecord(TimerRecord{}); got.HeadsUp.Enabled || !got.Start.Enabled || !got.End.Enabled {
		t.Errorf("with no lead: got %+v, want heads-up off, start/end on", got)
	}

	// Once saved in the new format, that wins over the old columns.
	saved := DiscordNotifications{End: DiscordNotification{Enabled: true, Template: "bye"}}
	if got := discordNotificationsFromRecord(TimerRecord{DiscordHHLeadMins: 15, DiscordNotifications: saved.record()}); got != saved {
		t.Errorf("round trip: got %+v, want %+v", got, saved)
	}
}

func TestSetDiscordSettingsValidates(t *testing.T) {
	tm, _, _ := newScheduleTimer(t)
	for name, s := range map[string]DiscordSettings{
		"heads-up on, no lead": {Notifications: DiscordNotifications{HeadsUp: DiscordNotification{Enabled: true}}},
		"negative lead":        {HHLead: -time.Minute},
		"partial minute lead":  {HHLead: 90 * time.Second},
		"lead over max":        {HHLead: MaxDiscordHHLead + time.Minute},
		"template too long":    {Notifications: DiscordNotifications{Start: DiscordNotification{Template: strings.Repeat("é", MaxDiscordTemplateLen+1)}}},
	} {
		if err := tm.SetDiscordSettings(s); !errors.Is(err, ErrInvalidDiscordSettings) {
			t.Errorf("%s: err = %v, want ErrInvalidDiscordSettings", name, err)
		}
	}
	ok := DiscordSettings{HHLead: 5 * time.Minute, Notifications: DiscordNotifications{
		HeadsUp: DiscordNotification{Enabled: true, Template: strings.Repeat("é", MaxDiscordTemplateLen)},
	}}
	if err := tm.SetDiscordSettings(ok); err != nil {
		t.Errorf("valid settings: %v", err)
	}
}

func TestHeadsUpOffSendsNoHeadsUp(t *testing.T) {
	tm, repo, start := newScheduleTimer(t)
	if err := tm.SetDiscordSettings(DiscordSettings{
		WebhookURL:    "https://discord.com/api/webhooks/1/token",
		HHLead:        10 * time.Minute,
		Notifications: DiscordNotifications{Start: DiscordNotification{Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	s, err := tm.ScheduleHappyHour(ScheduledHappyHour{Trigger: HappyHourTriggerAt, StartAt: start.Add(5 * time.Minute), DurationSecs: 600})
	if err != nil {
		t.Fatal(err)
	}
	if got := take(t, tm, start); len(got.Upcoming) != 0 || repo.announced[s.ID] {
		t.Fatalf("heads-up off: got %+v, want no heads-up and not marked", got)
	}
}

func TestTwitchAnnouncementsDefaultOnAndValidate(t *testing.T) {
	if got := twitchAnnouncementsFromRecord(TimerRecord{}); !got.Start.Enabled || !got.End.Enabled {
		t.Errorf("unsaved timer: got %+v, want both on (how they always behaved)", got)
	}
	saved := TwitchAnnouncements{End: TwitchAnnouncement{Enabled: true, Template: "bye"}}
	if got := twitchAnnouncementsFromRecord(TimerRecord{TwitchAnnouncements: saved.record()}); got != saved {
		t.Errorf("round trip: got %+v, want %+v", got, saved)
	}

	tm, _, _ := newScheduleTimer(t)
	long := TwitchAnnouncements{Start: TwitchAnnouncement{Template: strings.Repeat("x", MaxTwitchTemplateLen+1)}}
	if err := tm.SetTwitchAnnouncements(long); !errors.Is(err, ErrInvalidTwitchAnnouncements) {
		t.Errorf("too long: err = %v", err)
	}
	if err := tm.SetTwitchAnnouncements(saved); err != nil || tm.TwitchAnnouncements() != saved {
		t.Errorf("valid: err = %v, got %+v", err, tm.TwitchAnnouncements())
	}
}
