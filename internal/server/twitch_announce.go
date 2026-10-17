package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

// maxTwitchAnnouncementLen is Twitch's limit on a chat announcement, in
// characters.
const maxTwitchAnnouncementLen = 500

// defaultTwitchTemplates is the text each Twitch announcement's editor
// starts from. A template left empty (the default) is rendered by
// twitchStartBuiltin/twitchHappyHourEndMessage instead, which phrase a
// couple of cases more naturally than a fixed template can.
var defaultTwitchTemplates = map[notificationKind]string{
	notifyStart: "⏱️ Happy Hour! Time contributions are worth {multiplier} for the next {duration}!",
	notifyEnd:   "⏱️ Happy Hour has ended — time contributions are back to normal. It added {time_added} to the timer ({bonus_time} of that from the {multiplier} boost). Thank you so much for all the support! 💜",
}

// twitchPlaceholders lists the placeholders each Twitch announcement
// fills in — the same as Discord's, minus {ends}, since Twitch chat can't
// show a live countdown.
var twitchPlaceholders = map[notificationKind][]templatePlaceholder{
	notifyStart: {
		{"multiplier", `e.g. "2x"`},
		{"duration", `how long it lasts, e.g. "30 minutes"`},
		{"timer", "this timer's name"},
	},
	notifyEnd: notificationPlaceholders[notifyEnd],
}

// twitchStartBuiltin is the default start announcement: "DOUBLED" for a
// 2x happy hour.
func twitchStartBuiltin(multiplier float64, d time.Duration) string {
	if multiplier == 2 {
		return fmt.Sprintf("⏱️ Happy Hour! Time contributions are DOUBLED for the next %s!", humanDuration(d))
	}
	return fmt.Sprintf("⏱️ Happy Hour! Time contributions are worth %s for the next %s!", formatMultiplier(multiplier), humanDuration(d))
}

// renderTwitchAnnouncement fills kind's placeholders into template from
// vars, or returns builtin() if template is blank, cut to Twitch's
// length limit. Used by both the real announcements and the preview.
func renderTwitchAnnouncement(kind notificationKind, template string, vars map[string]string, builtin func() string) string {
	text := builtin()
	if strings.TrimSpace(template) != "" {
		var pairs []string
		for _, p := range twitchPlaceholders[kind] {
			pairs = append(pairs, "{"+p.Key+"}", vars[p.Key])
		}
		text = strings.NewReplacer(pairs...).Replace(template)
	}
	if runes := []rune(text); len(runes) > maxTwitchAnnouncementLen {
		text = string(runes[:maxTwitchAnnouncementLen-1]) + "…"
	}
	return text
}

func (k notificationKind) twitchSettings(a subathon.TwitchAnnouncements) subathon.TwitchAnnouncement {
	if k == notifyStart {
		return a.Start
	}
	return a.End
}

func twitchStartText(a subathon.TwitchAnnouncements, timerName string, multiplier float64, d time.Duration) string {
	return renderTwitchAnnouncement(notifyStart, a.Start.Template,
		startVars(timerName, multiplier, d, time.Time{}),
		func() string { return twitchStartBuiltin(multiplier, d) })
}

func twitchEndText(a subathon.TwitchAnnouncements, timerName string, multiplier float64, added, bonus time.Duration) string {
	return renderTwitchAnnouncement(notifyEnd, a.End.Template,
		endVars(timerName, multiplier, added, bonus),
		func() string { return twitchHappyHourEndMessage(multiplier, added, bonus) })
}

// announceTwitch posts kind's announcement (already rendered as message)
// in broadcasterID's chat as the timer's owner, if broadcasterID is set
// and that announcement is turned on.
func announceTwitch(deps Deps, t *subathon.Timer, broadcasterID string, kind notificationKind, message string) {
	if broadcasterID == "" || !kind.twitchSettings(t.TwitchAnnouncements()).Enabled {
		return
	}
	if err := announceAsOwner(deps, t, broadcasterID, message); err != nil {
		log.Printf("twitch: announce happy hour %s on timer %s: %v", kind, t.ID(), err)
	}
}

// twitchAnnouncementsResponse is the Happy Hours page's view of a timer's
// Twitch announcement settings, plus what each template can say.
type twitchAnnouncementsResponse struct {
	// Channel is the Twitch channel they're posted in, empty if none is
	// set (then nothing is posted).
	Channel          string                                     `json:"channel"`
	Announcements    subathon.TwitchAnnouncements               `json:"announcements"`
	DefaultTemplates map[notificationKind]string                `json:"defaultTemplates"`
	Placeholders     map[notificationKind][]templatePlaceholder `json:"placeholders"`
	MaxTemplateLen   int                                        `json:"maxTemplateLength"`
}

func twitchAnnouncementsView(t *subathon.Timer) twitchAnnouncementsResponse {
	_, channel := t.TwitchChannel()
	return twitchAnnouncementsResponse{
		Channel:          channel,
		Announcements:    t.TwitchAnnouncements(),
		DefaultTemplates: defaultTwitchTemplates,
		Placeholders:     twitchPlaceholders,
		MaxTemplateLen:   subathon.MaxTwitchTemplateLen,
	}
}

func handleGetTwitchAnnouncements() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, twitchAnnouncementsView(timerFromContext(r)))
	}
}

// handleSetTwitchAnnouncements saves a timer's Twitch chat announcement
// settings. A template that's just its default text is stored as empty,
// so it keeps the built-in phrasing (see defaultTwitchTemplates).
func handleSetTwitchAnnouncements() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var a subathon.TwitchAnnouncements
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		for _, p := range []struct {
			kind notificationKind
			a    *subathon.TwitchAnnouncement
		}{{notifyStart, &a.Start}, {notifyEnd, &a.End}} {
			if strings.TrimSpace(p.a.Template) == defaultTwitchTemplates[p.kind] {
				p.a.Template = ""
			}
		}

		if err := t.SetTwitchAnnouncements(a); err != nil {
			if errors.Is(err, subathon.ErrInvalidTwitchAnnouncements) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			log.Printf("server: set twitch announcements for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to save Twitch announcements", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, twitchAnnouncementsView(t))
	}
}

// handleTwitchAnnouncementsPreview renders both Twitch announcements (on
// or off) for a sample 2x, 30 minute happy hour that added 45 minutes,
// from the (possibly unsaved) settings in the request body, with the
// same code as the real announcements. Template text that's still the
// default previews the built-in phrasing, exactly as it'd be posted.
func handleTwitchAnnouncementsPreview() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var a subathon.TwitchAnnouncements
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(a.Start.Template) == defaultTwitchTemplates[notifyStart] {
			a.Start.Template = ""
		}
		if strings.TrimSpace(a.End.Template) == defaultTwitchTemplates[notifyEnd] {
			a.End.Template = ""
		}

		const multiplier = 2.0
		writeJSON(w, http.StatusOK, []discordPreviewMessage{
			{Kind: notifyStart, Label: "When a Happy Hour starts", Content: twitchStartText(a, t.Name(), multiplier, 30*time.Minute)},
			{Kind: notifyEnd, Label: "When it ends", Content: twitchEndText(a, t.Name(), multiplier, 45*time.Minute, 22*time.Minute+30*time.Second)},
		})
	}
}
