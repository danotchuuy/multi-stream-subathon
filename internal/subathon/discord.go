package subathon

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// MaxDiscordHHLead caps the "starting soon" lead time.
const MaxDiscordHHLead = 24 * time.Hour

// MaxDiscordTemplateLen caps a custom notification template, in
// characters — well under Discord's 2000-character message limit, to
// leave room for placeholders expanding and an @everyone prefix.
const MaxDiscordTemplateLen = 1000

// ErrInvalidDiscordSettings is returned (wrapped, with the reason) by
// SetDiscordSettings.
var ErrInvalidDiscordSettings = errors.New("invalid Discord settings")

// DiscordNotification configures one kind of Discord announcement.
type DiscordNotification struct {
	// Enabled turns this announcement on.
	Enabled bool `json:"enabled"`
	// PingEveryone prefixes it with @everyone, and lets that ping.
	PingEveryone bool `json:"pingEveryone"`
	// Template is the message text, with {placeholders} filled in when
	// it's posted (see internal/server's discord_announce.go). Empty
	// means the built-in default.
	Template string `json:"template"`
}

// DiscordNotifications configures each kind of Discord announcement.
type DiscordNotifications struct {
	// HeadsUp is posted DiscordSettings.HHLead before a scheduled happy
	// hour starts (see TakeDueHappyHours).
	HeadsUp DiscordNotification `json:"headsUp"`
	// Start is posted when any happy hour starts.
	Start DiscordNotification `json:"start"`
	// End is posted when any happy hour ends, early or not.
	End DiscordNotification `json:"end"`
}

// DiscordSettings configures a timer's Discord happy hour announcements.
type DiscordSettings struct {
	// WebhookURL is where they're posted (empty = nowhere). A secret —
	// callers building a response must not include it.
	WebhookURL string
	// HHLead is how far ahead of a scheduled happy hour the heads-up is
	// posted.
	HHLead        time.Duration
	Notifications DiscordNotifications
}

// discordNotificationsFromRecord decodes TimerRecord.DiscordNotifications,
// or — for a timer saved before per-notification settings existed —
// derives them from the old single settings: the heads-up on if a lead
// time was set, start and end always on, and the one @everyone toggle
// applied to all three.
func discordNotificationsFromRecord(rec TimerRecord) DiscordNotifications {
	var n DiscordNotifications
	if rec.DiscordNotifications != "" && json.Unmarshal([]byte(rec.DiscordNotifications), &n) == nil {
		return n
	}
	ping := rec.DiscordPingEveryone
	return DiscordNotifications{
		HeadsUp: DiscordNotification{Enabled: rec.DiscordHHLeadMins > 0, PingEveryone: ping},
		Start:   DiscordNotification{Enabled: true, PingEveryone: ping},
		End:     DiscordNotification{Enabled: true, PingEveryone: ping},
	}
}

func (n DiscordNotifications) record() string {
	b, err := json.Marshal(n)
	if err != nil {
		return "" // can't happen for this plain struct
	}
	return string(b)
}

// DiscordSettings returns this timer's Discord announcement settings.
func (t *Timer) DiscordSettings() DiscordSettings {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.discord
}

// SetDiscordSettings replaces this timer's Discord announcement
// settings. The caller validates s.WebhookURL (see
// discord.ValidWebhookURL); empty removes it. Returns
// ErrInvalidDiscordSettings for a lead time that isn't whole minutes
// from 0 to MaxDiscordHHLead, a heads-up turned on with no lead time, or
// a template over MaxDiscordTemplateLen.
func (t *Timer) SetDiscordSettings(s DiscordSettings) error {
	if s.HHLead < 0 || s.HHLead > MaxDiscordHHLead || s.HHLead%time.Minute != 0 {
		return fmt.Errorf("%w: lead time must be whole minutes between 0 and %d", ErrInvalidDiscordSettings, int(MaxDiscordHHLead/time.Minute))
	}
	if s.Notifications.HeadsUp.Enabled && s.HHLead == 0 {
		return fmt.Errorf("%w: the heads-up needs a lead time of at least 1 minute", ErrInvalidDiscordSettings)
	}
	for name, n := range map[string]DiscordNotification{
		"heads-up": s.Notifications.HeadsUp,
		"start":    s.Notifications.Start,
		"end":      s.Notifications.End,
	} {
		if utf8.RuneCountInString(n.Template) > MaxDiscordTemplateLen {
			return fmt.Errorf("%w: the %s message is over %d characters", ErrInvalidDiscordSettings, name, MaxDiscordTemplateLen)
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.discord = s
	return t.persistLocked()
}
