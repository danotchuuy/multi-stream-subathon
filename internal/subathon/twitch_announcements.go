package subathon

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

// MaxTwitchTemplateLen caps a custom Twitch chat announcement template,
// in characters — under Twitch's 500-character announcement limit, to
// leave room for placeholders expanding.
const MaxTwitchTemplateLen = 400

// ErrInvalidTwitchAnnouncements is returned (wrapped, with the reason) by
// SetTwitchAnnouncements.
var ErrInvalidTwitchAnnouncements = errors.New("invalid Twitch announcement settings")

// TwitchAnnouncement configures one Twitch chat happy hour announcement.
type TwitchAnnouncement struct {
	Enabled bool `json:"enabled"`
	// Template is the message text, with {placeholders} filled in when
	// it's posted (see internal/server's twitch_announce.go). Empty
	// means the built-in default.
	Template string `json:"template"`
}

// TwitchAnnouncements configures the Twitch chat announcements posted in
// the timer's Twitch channel when any happy hour starts and ends.
type TwitchAnnouncements struct {
	Start TwitchAnnouncement `json:"start"`
	End   TwitchAnnouncement `json:"end"`
}

// twitchAnnouncementsFromRecord decodes TimerRecord.TwitchAnnouncements,
// or — for a timer saved before they were customizable — returns both on
// with the default text, which is how they always behaved.
func twitchAnnouncementsFromRecord(rec TimerRecord) TwitchAnnouncements {
	var a TwitchAnnouncements
	if rec.TwitchAnnouncements != "" && json.Unmarshal([]byte(rec.TwitchAnnouncements), &a) == nil {
		return a
	}
	return TwitchAnnouncements{Start: TwitchAnnouncement{Enabled: true}, End: TwitchAnnouncement{Enabled: true}}
}

func (a TwitchAnnouncements) record() string {
	b, err := json.Marshal(a)
	if err != nil {
		return "" // can't happen for this plain struct
	}
	return string(b)
}

// TwitchAnnouncements returns this timer's Twitch chat happy hour
// announcement settings.
func (t *Timer) TwitchAnnouncements() TwitchAnnouncements {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.twitchAnnouncements
}

// SetTwitchAnnouncements replaces this timer's Twitch chat happy hour
// announcement settings. Returns ErrInvalidTwitchAnnouncements for a
// template over MaxTwitchTemplateLen.
func (t *Timer) SetTwitchAnnouncements(a TwitchAnnouncements) error {
	for name, n := range map[string]TwitchAnnouncement{"start": a.Start, "end": a.End} {
		if utf8.RuneCountInString(n.Template) > MaxTwitchTemplateLen {
			return fmt.Errorf("%w: the %s message is over %d characters", ErrInvalidTwitchAnnouncements, name, MaxTwitchTemplateLen)
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.twitchAnnouncements = a
	return t.persistLocked()
}
