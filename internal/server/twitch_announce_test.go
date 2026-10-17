package server

import (
	"strings"
	"testing"
	"time"

	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

func TestTwitchDefaultTextKeepsBuiltInPhrasing(t *testing.T) {
	var a subathon.TwitchAnnouncements // templates empty = default
	if got, want := twitchStartText(a, "T", 2, 90*time.Minute), "⏱️ Happy Hour! Time contributions are DOUBLED for the next 1 hour 30 minutes!"; got != want {
		t.Errorf("2x start:\n got %q\nwant %q", got, want)
	}
	if got, want := twitchStartText(a, "T", 3, 30*time.Minute), "⏱️ Happy Hour! Time contributions are worth 3x for the next 30 minutes!"; got != want {
		t.Errorf("3x start:\n got %q\nwant %q", got, want)
	}
	if got := twitchEndText(a, "T", 2, 0, 0); strings.Contains(got, "0 seconds") {
		t.Errorf("default end with nothing added says %q", got)
	}
}

func TestTwitchCustomTemplateFillsPlaceholders(t *testing.T) {
	a := subathon.TwitchAnnouncements{
		Start: subathon.TwitchAnnouncement{Template: "{timer}: {multiplier} for {duration}! {ends}"},
		End:   subathon.TwitchAnnouncement{Template: "Done! +{time_added} ({bonus_time} bonus). Thanks!"},
	}
	// {ends} is Discord-only (a live timestamp), so it stays as written.
	if got, want := twitchStartText(a, "Big Stream", 2, 30*time.Minute), "Big Stream: 2x for 30 minutes! {ends}"; got != want {
		t.Errorf("start:\n got %q\nwant %q", got, want)
	}
	if got, want := twitchEndText(a, "T", 2, 10*time.Minute, 5*time.Minute), "Done! +10 minutes (5 minutes bonus). Thanks!"; got != want {
		t.Errorf("end:\n got %q\nwant %q", got, want)
	}
}

func TestTwitchAnnouncementCutToTwitchLimit(t *testing.T) {
	a := subathon.TwitchAnnouncements{Start: subathon.TwitchAnnouncement{Template: strings.Repeat("{timer}", 60)}}
	got := twitchStartText(a, "A Very Long Timer Name", 2, time.Minute)
	if n := len([]rune(got)); n != maxTwitchAnnouncementLen || !strings.HasSuffix(got, "…") {
		t.Errorf("got %d characters ending %q, want %d ending in …", n, got[len(got)-6:], maxTwitchAnnouncementLen)
	}
}

func TestTwitchDefaultTemplatesUseOnlyTheirOwnPlaceholders(t *testing.T) {
	for kind, tmpl := range defaultTwitchTemplates {
		got := renderTwitchAnnouncement(kind, tmpl, map[string]string{}, func() string { return "" })
		if strings.Contains(got, "{") {
			t.Errorf("%s default template leaves a placeholder unfilled: %q", kind, got)
		}
	}
}
