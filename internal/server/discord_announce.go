package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/danotchuuy/multi-stream-subathon/internal/platform/discord"
	"github.com/danotchuuy/multi-stream-subathon/internal/subathon"
)

// discordPostTimeout bounds one announcement, independent of any request
// that triggered it.
const discordPostTimeout = 10 * time.Second

// notificationKind is one kind of Discord announcement, matching a
// field of subathon.DiscordNotifications (and its JSON key).
type notificationKind string

const (
	notifyHeadsUp notificationKind = "headsUp"
	notifyStart   notificationKind = "start"
	notifyEnd     notificationKind = "end"
)

func (k notificationKind) settings(n subathon.DiscordNotifications) subathon.DiscordNotification {
	switch k {
	case notifyHeadsUp:
		return n.HeadsUp
	case notifyStart:
		return n.Start
	default:
		return n.End
	}
}

// defaultTemplates is each announcement's built-in text, used when its
// template is left empty.
var defaultTemplates = map[notificationKind]string{
	notifyHeadsUp: "⏰ **Happy Hour starts {when}!** Time contributions will be worth **{multiplier}** for {duration}.",
	notifyStart:   "🔥 **Happy Hour has started!** Time contributions are worth **{multiplier}** for the next {duration}.",
	notifyEnd:     "⏱️ **Happy Hour has ended** — time contributions are back to normal. It added **{time_added}** to the timer ({bonus_time} of that from the {multiplier} boost). Thank you so much for all the support! 💜",
}

// templatePlaceholder is one {key} a template can use.
type templatePlaceholder struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

// notificationPlaceholders lists the placeholders each announcement
// fills in. Any other {text} in a template is left as written.
var notificationPlaceholders = map[notificationKind][]templatePlaceholder{
	notifyHeadsUp: {
		{"when", `when it starts: "in 10 minutes (8:00 PM)" for a date & time one, "in about 10 minutes, when the timer hits 1:00:00" for a time-left one`},
		{"multiplier", `e.g. "2x"`},
		{"duration", `how long it lasts, e.g. "30 minutes"`},
		{"timer", "this timer's name"},
	},
	notifyStart: {
		{"multiplier", `e.g. "2x"`},
		{"duration", `how long it lasts, e.g. "30 minutes"`},
		{"ends", `when it ends, as a live countdown, e.g. "in 30 minutes"`},
		{"timer", "this timer's name"},
	},
	notifyEnd: {
		{"multiplier", `the one that just ended, e.g. "2x"`},
		{"time_added", `total time contributions added to the timer during it, e.g. "45 minutes"`},
		{"bonus_time", `how much of that was extra from the multiplier, e.g. "22 minutes"`},
		{"timer", "this timer's name"},
	},
}

// renderNotification fills kind's placeholders into template (or its
// default, if template is blank) from vars. Used by both the real posts
// and the preview (handleDiscordPreview), so the two can't drift.
func renderNotification(kind notificationKind, template string, vars map[string]string) string {
	if strings.TrimSpace(template) == "" {
		template = defaultTemplates[kind]
	}
	var pairs []string
	for _, p := range notificationPlaceholders[kind] {
		pairs = append(pairs, "{"+p.Key+"}", vars[p.Key])
	}
	return strings.NewReplacer(pairs...).Replace(template)
}

// discordTimestamp formats t as Discord markup that each reader sees as
// a live relative time (style "R") or a time in their own timezone
// ("t").
func discordTimestamp(t time.Time, style string) string {
	return fmt.Sprintf("<t:%d:%s>", t.Unix(), style)
}

func headsUpVars(timerName string, u subathon.UpcomingHappyHour) map[string]string {
	when := fmt.Sprintf("in about %s, when the timer hits %s", humanDuration(u.StartsIn), clockDuration(u.RemainingSecs))
	if u.Trigger == subathon.HappyHourTriggerAt {
		when = discordTimestamp(u.StartAt, "R") + " (" + discordTimestamp(u.StartAt, "t") + ")"
	}
	return map[string]string{
		"when":       when,
		"multiplier": formatMultiplier(u.Multiplier),
		"duration":   humanDuration(time.Duration(u.DurationSecs) * time.Second),
		"timer":      timerName,
	}
}

func startVars(timerName string, multiplier float64, d time.Duration, endsAt time.Time) map[string]string {
	return map[string]string{
		"multiplier": formatMultiplier(multiplier),
		"duration":   humanDuration(d),
		"ends":       discordTimestamp(endsAt, "R"),
		"timer":      timerName,
	}
}

func endVars(timerName string, multiplier float64, added, bonus time.Duration) map[string]string {
	return map[string]string{
		"multiplier": formatMultiplier(multiplier),
		"time_added": humanDuration(added),
		"bonus_time": humanDuration(bonus),
		"timer":      timerName,
	}
}

// postDiscordNotification posts kind's announcement to t's Discord
// webhook in the background — if it has one and that announcement is
// turned on — rendered from its template with vars, pinging @everyone if
// it's set to. A failure is only logged. Settings are read at send time
// rather than passed in, so changes in the meantime (e.g. before a happy
// hour's scheduled end announcement) are respected.
func postDiscordNotification(deps Deps, t *subathon.Timer, kind notificationKind, vars map[string]string) {
	settings := t.DiscordSettings()
	n := kind.settings(settings.Notifications)
	if settings.WebhookURL == "" || !n.Enabled || deps.Discord == nil {
		return
	}
	m := discord.Message{Content: renderNotification(kind, n.Template, vars), PingEveryone: n.PingEveryone}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), discordPostTimeout)
		defer cancel()
		if err := deps.Discord.Post(ctx, settings.WebhookURL, m); err != nil {
			log.Printf("discord: %s announcement on timer %s: %v", kind, t.ID(), err)
		}
	}()
}

// announceUpcomingHappyHour posts a scheduled happy hour's one-time
// "starting soon" message (see subathon.DueHappyHours.Upcoming).
func announceUpcomingHappyHour(deps Deps, t *subathon.Timer, u subathon.UpcomingHappyHour) {
	postDiscordNotification(deps, t, notifyHeadsUp, headsUpVars(t.Name(), u))
}

// humanDuration renders d for an announcement, rounded to the minute
// (or to the second under one), e.g. "1 hour 30 minutes", "45 minutes",
// "30 seconds".
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return plural(int(d.Round(time.Second)/time.Second), "second")
	}
	mins := int(d.Round(time.Minute) / time.Minute)
	h, m := mins/60, mins%60
	switch {
	case h == 0:
		return plural(m, "minute")
	case m == 0:
		return plural(h, "hour")
	default:
		return plural(h, "hour") + " " + plural(m, "minute")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// clockDuration renders secs the way the overlay's clock does: H:MM:SS,
// or M:SS under an hour (see frontend lib/format.ts's formatDuration).
func clockDuration(secs int) string {
	h, m, s := secs/3600, secs%3600/60, secs%60
	if h == 0 {
		return fmt.Sprintf("%d:%02d", m, s)
	}
	return fmt.Sprintf("%d:%02d:%02d", h, m, s)
}

// discordInfoTimeout bounds looking up the webhook's name/avatar for
// the dashboard, which waits on it.
const discordInfoTimeout = 5 * time.Second

// discordSettingsResponse is the public-safe view of
// Timer.DiscordSettings: whether a webhook is set, never the URL itself
// (anyone holding it can post to that channel). WebhookName/
// WebhookAvatarURL are how Discord labels its posts, for the preview —
// empty if there's no webhook or the lookup failed. DefaultTemplates and
// Placeholders describe what each announcement's template can say.
type discordSettingsResponse struct {
	Configured       bool                                       `json:"configured"`
	LeadMinutes      int                                        `json:"leadMinutes"`
	Notifications    subathon.DiscordNotifications              `json:"notifications"`
	WebhookName      string                                     `json:"webhookName,omitempty"`
	WebhookAvatarURL string                                     `json:"webhookAvatarUrl,omitempty"`
	DefaultTemplates map[notificationKind]string                `json:"defaultTemplates"`
	Placeholders     map[notificationKind][]templatePlaceholder `json:"placeholders"`
	MaxTemplateLen   int                                        `json:"maxTemplateLength"`
}

func discordSettingsView(ctx context.Context, deps Deps, t *subathon.Timer) discordSettingsResponse {
	s := t.DiscordSettings()
	view := discordSettingsResponse{
		Configured:       s.WebhookURL != "",
		LeadMinutes:      int(s.HHLead / time.Minute),
		Notifications:    s.Notifications,
		DefaultTemplates: defaultTemplates,
		Placeholders:     notificationPlaceholders,
		MaxTemplateLen:   subathon.MaxDiscordTemplateLen,
	}
	if s.WebhookURL != "" && deps.Discord != nil {
		ctx, cancel := context.WithTimeout(ctx, discordInfoTimeout)
		defer cancel()
		if info, err := deps.Discord.Info(ctx, s.WebhookURL); err != nil {
			log.Printf("discord: look up webhook for timer %s: %v", t.ID(), err)
		} else {
			view.WebhookName, view.WebhookAvatarURL = info.Name, info.AvatarURL
		}
	}
	return view
}

func handleGetDiscordSettings(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, discordSettingsView(r.Context(), deps, timerFromContext(r)))
	}
}

type setDiscordSettingsRequest struct {
	// WebhookURL replaces the webhook when present; "" removes it, and
	// omitting it (null) keeps the current one, since the page never
	// gets the current URL back to resend.
	WebhookURL    *string                       `json:"webhookUrl"`
	LeadMinutes   int                           `json:"leadMinutes"`
	Notifications subathon.DiscordNotifications `json:"notifications"`
}

// normalizeTemplates stores a template that's just its default as empty,
// so it keeps tracking the default if that's ever reworded.
func normalizeTemplates(n subathon.DiscordNotifications) subathon.DiscordNotifications {
	for _, p := range []struct {
		kind notificationKind
		n    *subathon.DiscordNotification
	}{{notifyHeadsUp, &n.HeadsUp}, {notifyStart, &n.Start}, {notifyEnd, &n.End}} {
		if strings.TrimSpace(p.n.Template) == defaultTemplates[p.kind] {
			p.n.Template = ""
		}
	}
	return n
}

// handleSetDiscordSettings changes a timer's Discord webhook, heads-up
// lead time, and per-announcement settings (see Timer.SetDiscordSettings).
func handleSetDiscordSettings(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req setDiscordSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		settings := subathon.DiscordSettings{
			WebhookURL:    t.DiscordSettings().WebhookURL,
			HHLead:        time.Duration(req.LeadMinutes) * time.Minute,
			Notifications: normalizeTemplates(req.Notifications),
		}
		if req.WebhookURL != nil {
			settings.WebhookURL = strings.TrimSpace(*req.WebhookURL)
			if settings.WebhookURL != "" && !discord.ValidWebhookURL(settings.WebhookURL) {
				http.Error(w, "that doesn't look like a Discord webhook URL (https://discord.com/api/webhooks/...)", http.StatusBadRequest)
				return
			}
		}

		if err := t.SetDiscordSettings(settings); err != nil {
			if errors.Is(err, subathon.ErrInvalidDiscordSettings) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			log.Printf("server: set discord settings for timer %s: %v", t.ID(), err)
			http.Error(w, "failed to save Discord settings", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, discordSettingsView(r.Context(), deps, t))
	}
}

// discordPreviewMessage is one announcement as it'd appear in the
// channel, for the settings page's preview.
type discordPreviewMessage struct {
	Kind notificationKind `json:"kind"`
	// Label says when it's posted, e.g. "When a Happy Hour starts".
	Label string `json:"label"`
	// Content is the exact message text, Discord markdown and all.
	Content string `json:"content"`
}

type discordPreviewRequest struct {
	LeadMinutes   int                           `json:"leadMinutes"`
	Notifications subathon.DiscordNotifications `json:"notifications"`
}

// handleDiscordPreview renders every Discord announcement — on or off —
// for a sample 2x, 30 minute happy hour from the (possibly unsaved)
// settings in the request body, with the same code as the real posts.
// Makes no request to Discord.
func handleDiscordPreview() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		var req discordPreviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		lead := 10 * time.Minute // a sample, for a heads-up that's off
		if req.LeadMinutes > 0 {
			lead = min(time.Duration(req.LeadMinutes)*time.Minute, subathon.MaxDiscordHHLead)
		}

		const multiplier = 2.0
		const duration = 30 * time.Minute
		now := time.Now()
		render := func(kind notificationKind, vars map[string]string) string {
			n := kind.settings(req.Notifications)
			return discord.Message{Content: renderNotification(kind, n.Template, vars), PingEveryone: n.PingEveryone}.Text()
		}

		sample := subathon.ScheduledHappyHour{DurationSecs: int(duration / time.Second), Multiplier: multiplier}
		at, remaining := sample, sample
		at.Trigger, at.StartAt = subathon.HappyHourTriggerAt, now.Add(lead)
		remaining.Trigger, remaining.RemainingSecs = subathon.HappyHourTriggerRemaining, 3600

		writeJSON(w, http.StatusOK, []discordPreviewMessage{
			{
				Kind:    notifyHeadsUp,
				Label:   fmt.Sprintf("%s before a date & time Happy Hour", humanDuration(lead)),
				Content: render(notifyHeadsUp, headsUpVars(t.Name(), subathon.UpcomingHappyHour{ScheduledHappyHour: at, StartsIn: lead})),
			},
			{
				Kind:    notifyHeadsUp,
				Label:   fmt.Sprintf("%s before a time-left Happy Hour", humanDuration(lead)),
				Content: render(notifyHeadsUp, headsUpVars(t.Name(), subathon.UpcomingHappyHour{ScheduledHappyHour: remaining, StartsIn: lead})),
			},
			{Kind: notifyStart, Label: "When a Happy Hour starts", Content: render(notifyStart, startVars(t.Name(), multiplier, duration, now.Add(duration)))},
			// A sample: 45 minutes of 2x contributions, half of it bonus.
			{Kind: notifyEnd, Label: "When it ends", Content: render(notifyEnd, endVars(t.Name(), multiplier, 45*time.Minute, 22*time.Minute+30*time.Second))},
		})
	}
}

// handleTestDiscordWebhook posts a test message through the saved
// webhook, synchronously, so the dashboard can show whether it worked.
// It never pings @everyone, whatever the timer's setting, so testing
// doesn't notify a whole server.
func handleTestDiscordWebhook(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := timerFromContext(r)

		webhookURL := t.DiscordSettings().WebhookURL
		if webhookURL == "" {
			http.Error(w, "no Discord webhook is set", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), discordPostTimeout)
		defer cancel()
		m := discord.Message{Content: fmt.Sprintf("✅ Test message from the **%s** subathon timer — Happy Hour announcements will show up here.", t.Name())}
		if err := deps.Discord.Post(ctx, webhookURL, m); err != nil {
			log.Printf("discord: test webhook on timer %s: %v", t.ID(), err)
			http.Error(w, "Discord rejected the test message: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
