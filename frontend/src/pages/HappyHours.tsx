import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  cancelScheduledHappyHour,
  getDiscordPreview,
  getDiscordSettings,
  getScheduledHappyHours,
  getTwitchAnnouncements,
  getTwitchAnnouncementsPreview,
  HttpError,
  scheduleHappyHour,
  setDiscordSettings,
  setTwitchAnnouncements,
  testDiscordWebhook,
  validHappyHourMultiplier,
} from '../lib/api'
import { DiscordChannelPreview } from '../lib/discordPreview'
import { TwitchChatPreview } from '../lib/twitchPreview'
import { formatDuration } from '../lib/format'
import { useSubathon } from '../lib/useSubathon'
import type {
  DiscordNotification,
  DiscordNotifications,
  DiscordPreviewMessage,
  DiscordSettings,
  HappyHourTrigger,
  NotificationKind,
  ScheduledHappyHour,
  TemplatePlaceholder,
  TwitchAnnouncement,
  TwitchAnnouncementSettings,
  TwitchAnnouncements,
} from '../types'

function controlErrorMessage(err: unknown): string {
  if (err instanceof HttpError) {
    if (err.status === 403) return "You don't own this timer."
    if (err.status === 401) return 'Please log in again.'
    if (err.message) return err.message
  }
  return 'That action failed. Please try again.'
}

/** One scheduled happy hour's "when" for the list, e.g. "Sat, Oct 18,
 * 8:00 PM" or "at 1:30:00 left". */
function describeHappyHourTrigger(s: ScheduledHappyHour): string {
  if (s.trigger === 'at' && s.startAt) {
    return new Date(s.startAt).toLocaleString(undefined, {
      weekday: 'short',
      month: 'short',
      day: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
    })
  }
  return `at ${formatDuration(s.remainingSecs ?? 0)} left`
}

/** Queues happy hours to start later — at a specific date/time, or once
 * the countdown reaches a given amount of time left — and lists the
 * pending ones with a Cancel button each. Each starts exactly like the
 * dashboard's "Start Happy Hour" button (Twitch/Discord announcements
 * included). None starts while the timer is paused or locked. A due one
 * waits for Play/unlock, except that a date/time one is dropped once
 * over 5 minutes late (see subathon.MissedHappyHourGrace) and a
 * time-left one is dropped if its mark is reached while locked. The list
 * is refetched whenever snapshot.happyHourScheduleVersion bumps, so one
 * starting (or being added from another tab) drops off on its own. */
function ScheduledHappyHours({
  timerId,
  scheduleVersion,
}: {
  timerId: string
  scheduleVersion: number | undefined
}) {
  const [scheduled, setScheduled] = useState<ScheduledHappyHour[] | null>(null)
  const [trigger, setTrigger] = useState<HappyHourTrigger>('at')
  // A datetime-local input value ("2026-10-18T20:00"), in local time.
  const [startAt, setStartAt] = useState('')
  const [markHours, setMarkHours] = useState(1)
  const [markMinutes, setMarkMinutes] = useState(0)
  const [minutes, setMinutes] = useState(30)
  const [multiplier, setMultiplier] = useState(2)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getScheduledHappyHours(timerId)
      .then(setScheduled)
      .catch(() => setScheduled([]))
  }, [timerId, scheduleVersion])

  const markSecs = markHours * 3600 + markMinutes * 60
  const valid =
    minutes > 0 &&
    validHappyHourMultiplier(multiplier) &&
    (trigger === 'at' ? startAt !== '' : markSecs > 0)

  const handleSchedule = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const created = await scheduleHappyHour(
        timerId,
        trigger === 'at'
          ? { trigger, startAt: new Date(startAt).toISOString() }
          : { trigger, remainingSecs: markSecs },
        minutes * 60,
        multiplier,
      )
      // The broadcast snapshot refetches the list too; adding it here
      // just avoids a flash of the old list in the meantime.
      setScheduled((prev) => [...(prev ?? []), created])
      setStartAt('')
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const handleCancel = async (id: string) => {
    setBusy(true)
    setError(null)
    try {
      await cancelScheduledHappyHour(timerId, id)
      setScheduled((prev) => prev?.filter((s) => s.id !== id) ?? null)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="control-group control-group-stacked">
      <span className="control-hint">
        Won't start while the timer is paused or locked — waits for Play/unlock instead.
        Skipped if a date &amp; time one ends up over 5 minutes late, or a time-left
        mark is reached while locked.
      </span>
      {scheduled === null ? (
        <p className="empty">Loading…</p>
      ) : scheduled.length === 0 ? (
        <p className="empty">None scheduled.</p>
      ) : (
        <ul className="scheduled-hh-list">
          {scheduled.map((s) => (
            <li key={s.id}>
              <span>
                {describeHappyHourTrigger(s)} — {formatDuration(s.durationSecs)} at{' '}
                {s.multiplier}×
                {s.announced && <span className="control-hint"> · Discord heads-up sent</span>}
              </span>
              <button type="button" onClick={() => handleCancel(s.id)} disabled={busy}>
                Cancel
              </button>
            </li>
          ))}
        </ul>
      )}
      <form className="control-inline" onSubmit={handleSchedule}>
        <label>
          Start
          <select
            value={trigger}
            onChange={(e) => setTrigger(e.target.value as HappyHourTrigger)}
          >
            <option value="at">At a date &amp; time</option>
            <option value="remaining">When the timer reaches</option>
          </select>
        </label>
        {trigger === 'at' ? (
          <label>
            Date &amp; time
            <input
              type="datetime-local"
              value={startAt}
              onChange={(e) => setStartAt(e.target.value)}
            />
          </label>
        ) : (
          <>
            <label>
              Hours left
              <input
                type="number"
                min={0}
                value={markHours}
                onChange={(e) => setMarkHours(Math.max(0, Number(e.target.value) || 0))}
              />
            </label>
            <label>
              Minutes left
              <input
                type="number"
                min={0}
                max={59}
                value={markMinutes}
                onChange={(e) => setMarkMinutes(Math.max(0, Number(e.target.value) || 0))}
              />
            </label>
          </>
        )}
        <label>
          Lasts (minutes)
          <input
            type="number"
            min={1}
            value={minutes}
            onChange={(e) => setMinutes(Number(e.target.value))}
          />
        </label>
        <label>
          Multiplier
          <input
            type="number"
            min={1}
            max={10}
            step="any"
            value={multiplier}
            onChange={(e) => setMultiplier(Number(e.target.value))}
          />
        </label>
        <button type="submit" disabled={busy || !valid}>
          Schedule
        </button>
      </form>
      {error && <p className="error-message">{error}</p>}
    </div>
  )
}

/** The Discord webhook itself: paste/replace/remove it and send a test
 * message. The saved URL is never shown again (it's a secret), only
 * whether one is set. Saving here keeps the announcement settings as
 * they were last saved, not any unsaved edits below. */
function DiscordWebhookForm({
  timerId,
  settings,
  onSaved,
}: {
  timerId: string
  settings: DiscordSettings
  onSaved: (settings: DiscordSettings) => void
}) {
  const [urlInput, setUrlInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const run = async (action: () => Promise<unknown>, done: string) => {
    setBusy(true)
    setStatus(null)
    setError(null)
    try {
      await action()
      setStatus(done)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const save = (webhookUrl: string) =>
    run(async () => {
      onSaved(
        await setDiscordSettings(timerId, {
          webhookUrl,
          leadMinutes: settings.leadMinutes,
          notifications: settings.notifications,
        }),
      )
      setUrlInput('')
    }, webhookUrl === '' ? 'Removed.' : 'Saved!')

  return (
    <form
      className="control-group control-group-stacked"
      onSubmit={(e) => {
        e.preventDefault()
        save(urlInput.trim())
      }}
    >
      <span className="control-hint">
        {settings.configured
          ? `Webhook set${settings.webhookName ? ` — posting as "${settings.webhookName}"` : ''}.`
          : 'Paste a channel webhook URL (Server Settings → Integrations → Webhooks) to announce Happy Hours on Discord.'}
      </span>
      <div className="control-inline">
        <input
          type="password"
          autoComplete="off"
          value={urlInput}
          onChange={(e) => setUrlInput(e.target.value)}
          placeholder={
            settings.configured ? 'paste a new URL to replace it' : 'https://discord.com/api/webhooks/…'
          }
        />
        <button type="submit" disabled={busy || !urlInput.trim()}>
          Save
        </button>
        {settings.configured && (
          <>
            <button
              type="button"
              disabled={busy}
              onClick={() => run(() => testDiscordWebhook(timerId), 'Test message sent!')}
            >
              Send test (no ping)
            </button>
            <button type="button" disabled={busy} onClick={() => save('')}>
              Remove
            </button>
          </>
        )}
      </div>
      {status && <span className="control-hint">{status}</span>}
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

const NOTIFICATION_INFO: Record<NotificationKind, { title: string; description: string }> = {
  headsUp: {
    title: 'Heads-up before a scheduled Happy Hour',
    description:
      "Posted once ahead of each scheduled Happy Hour, while the timer is running and unlocked. Won't repeat if time is added back.",
  },
  start: {
    title: 'Happy Hour started',
    description: 'Posted when any Happy Hour starts — from the dashboard, !timer hh, or a schedule.',
  },
  end: {
    title: 'Happy Hour ended',
    description: "Posted when a Happy Hour ends, including when it's ended early.",
  },
}

const NOTIFICATION_KINDS: NotificationKind[] = ['headsUp', 'start', 'end']

/** The editable part of one announcement, Discord or Twitch: on/off and
 * its text, plus @everyone where the platform has it (Discord). */
interface EditableAnnouncement {
  enabled: boolean
  template: string
  pingEveryone?: boolean
}

/** One announcement's settings — on/off, @everyone (Discord only), and
 * its text with click-to-insert placeholders — beside preview, how it'll
 * look once posted (null while loading). children holds any extra
 * settings (the Discord heads-up's lead time). */
function NotificationCard<T extends EditableAnnouncement>({
  title,
  description,
  value,
  onChange,
  defaultTemplate,
  placeholders,
  maxLength,
  preview,
  children,
}: {
  title: string
  description: string
  value: T
  onChange: (value: T) => void
  defaultTemplate: string
  placeholders: TemplatePlaceholder[]
  maxLength: number
  preview: React.ReactNode | null
  children?: React.ReactNode
}) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const length = [...value.template].length

  // Inserts {key} at the cursor (or over the selection), then puts the
  // cursor right after it.
  const insertPlaceholder = (key: string) => {
    const el = textareaRef.current
    const token = `{${key}}`
    const start = el?.selectionStart ?? value.template.length
    const end = el?.selectionEnd ?? value.template.length
    onChange({ ...value, template: value.template.slice(0, start) + token + value.template.slice(end) })
    requestAnimationFrame(() => {
      el?.focus()
      el?.setSelectionRange(start + token.length, start + token.length)
    })
  }

  return (
    <div className="control-group control-group-stacked hh-notification">
      <div className="hh-notification-head">
        <label className="checkbox-label hh-notification-title">
          <input
            type="checkbox"
            checked={value.enabled}
            onChange={(e) => onChange({ ...value, enabled: e.target.checked })}
          />
          {title}
        </label>
        <span className="control-hint">{description}</span>
      </div>
      <div className="hh-notification-body">
        <div className="hh-notification-settings">
          <div className="control-inline">
            {children}
            {value.pingEveryone !== undefined && (
              <label className="checkbox-label">
                <input
                  type="checkbox"
                  checked={value.pingEveryone}
                  onChange={(e) => onChange({ ...value, pingEveryone: e.target.checked })}
                />
                Ping @everyone
              </label>
            )}
          </div>
          <label>
            Message
            <textarea
              ref={textareaRef}
              className="hh-template"
              value={value.template}
              maxLength={maxLength}
              onChange={(e) => onChange({ ...value, template: e.target.value })}
            />
          </label>
          <div className="hh-template-footer">
            <span className="control-hint">
              {length}/{maxLength}
            </span>
            {value.template !== defaultTemplate && (
              <button type="button" onClick={() => onChange({ ...value, template: defaultTemplate })}>
                Reset to default
              </button>
            )}
          </div>
          <div className="hh-placeholders">
            <span className="control-hint">Click to insert:</span>
            {placeholders.map((p) => (
              <button
                key={p.key}
                type="button"
                className="hh-placeholder"
                title={p.description}
                onClick={() => insertPlaceholder(p.key)}
              >
                {`{${p.key}}`}
              </button>
            ))}
          </div>
          <ul className="hh-placeholder-help">
            {placeholders.map((p) => (
              <li key={p.key}>
                <code>{`{${p.key}}`}</code> — {p.description}
              </li>
            ))}
          </ul>
        </div>
        <div
          className={
            value.enabled ? 'hh-notification-preview' : 'hh-notification-preview hh-notification-off'
          }
        >
          {!value.enabled && <span className="control-hint">Off — this won't be posted.</span>}
          {preview ?? <p className="empty">Loading preview…</p>}
        </div>
      </div>
    </div>
  )
}

/** Fills each unset template in with its default text, so the editor
 * always has something to edit from. The server stores a template left
 * at its default as unset again (so it follows any future rewording). */
function withDefaultTemplates(s: DiscordSettings): DiscordNotifications {
  const out = { ...s.notifications }
  for (const kind of NOTIFICATION_KINDS) {
    out[kind] = { ...out[kind], template: out[kind].template || s.defaultTemplates[kind] }
  }
  return out
}

/** Every announcement's settings, edited together and saved with one
 * button, each with a live preview of the unsaved edits. */
function DiscordNotificationsForm({
  timerId,
  settings,
  onSaved,
}: {
  timerId: string
  settings: DiscordSettings
  onSaved: (settings: DiscordSettings) => void
}) {
  const [notifications, setNotifications] = useState(() => withDefaultTemplates(settings))
  // A heads-up that's off still keeps a lead time to come back to.
  const [leadMinutes, setLeadMinutes] = useState(settings.leadMinutes || 10)
  const [preview, setPreview] = useState<DiscordPreviewMessage[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const leadValid = Number.isInteger(leadMinutes) && leadMinutes >= 1 && leadMinutes <= 1440

  // Debounced so typing doesn't fire a request per keystroke.
  useEffect(() => {
    const handle = setTimeout(() => {
      getDiscordPreview(timerId, leadValid ? leadMinutes : 0, notifications)
        .then(setPreview)
        .catch(() => setPreview(null))
    }, 300)
    return () => clearTimeout(handle)
  }, [timerId, leadMinutes, leadValid, notifications])

  const update = (kind: NotificationKind, value: DiscordNotification) => {
    setNotifications((prev) => ({ ...prev, [kind]: value }))
    setSaved(false)
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      onSaved(await setDiscordSettings(timerId, { leadMinutes, notifications }))
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="hh-notifications" onSubmit={handleSave}>
      {!settings.configured && (
        <p className="empty">
          No webhook set yet — these save, but nothing is posted until you add one above.
        </p>
      )}
      {NOTIFICATION_KINDS.map((kind) => (
        <NotificationCard
          key={kind}
          title={NOTIFICATION_INFO[kind].title}
          description={NOTIFICATION_INFO[kind].description}
          value={notifications[kind]}
          onChange={(value) => update(kind, value)}
          defaultTemplate={settings.defaultTemplates[kind]}
          placeholders={settings.placeholders[kind]}
          maxLength={settings.maxTemplateLength}
          preview={
            preview && (
              <DiscordChannelPreview
                messages={preview.filter((m) => m.kind === kind)}
                webhookName={settings.webhookName}
                webhookAvatarUrl={settings.webhookAvatarUrl}
                hideHeader
              />
            )
          }
        >
          {kind === 'headsUp' && (
            <label>
              Minutes before
              <input
                type="number"
                min={1}
                max={1440}
                value={leadMinutes}
                onChange={(e) => {
                  setLeadMinutes(Number(e.target.value))
                  setSaved(false)
                }}
              />
            </label>
          )}
        </NotificationCard>
      ))}
      <div className="control-inline">
        <button type="submit" disabled={busy || !leadValid}>
          {saved ? 'Saved!' : 'Save announcements'}
        </button>
        {!leadValid && <span className="control-hint">The heads-up needs 1–1440 minutes.</span>}
      </div>
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

type TwitchKind = 'start' | 'end'

const TWITCH_KINDS: TwitchKind[] = ['start', 'end']

/** The Twitch chat announcements' settings, edited together and saved
 * with one button, each with a live preview of the unsaved edits.
 * Templates start filled in with the default text; left at the default,
 * the server posts its built-in phrasing (e.g. "DOUBLED" at 2x, no
 * time-added sentence when nothing came in). */
function TwitchAnnouncementsForm({
  timerId,
  settings,
  onSaved,
}: {
  timerId: string
  settings: TwitchAnnouncementSettings
  onSaved: (settings: TwitchAnnouncementSettings) => void
}) {
  const [announcements, setAnnouncements] = useState<TwitchAnnouncements>(() => ({
    start: {
      ...settings.announcements.start,
      template: settings.announcements.start.template || settings.defaultTemplates.start,
    },
    end: {
      ...settings.announcements.end,
      template: settings.announcements.end.template || settings.defaultTemplates.end,
    },
  }))
  const [preview, setPreview] = useState<DiscordPreviewMessage[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Debounced so typing doesn't fire a request per keystroke.
  useEffect(() => {
    const handle = setTimeout(() => {
      getTwitchAnnouncementsPreview(timerId, announcements)
        .then(setPreview)
        .catch(() => setPreview(null))
    }, 300)
    return () => clearTimeout(handle)
  }, [timerId, announcements])

  const update = (kind: TwitchKind, value: TwitchAnnouncement) => {
    setAnnouncements((prev) => ({ ...prev, [kind]: value }))
    setSaved(false)
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      onSaved(await setTwitchAnnouncements(timerId, announcements))
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="hh-notifications" onSubmit={handleSave}>
      <p className="empty">
        {settings.channel
          ? `Posted as chat announcements in ${settings.channel}'s chat, from the streamer's own account.`
          : 'No Twitch channel set for this timer yet — these save, but nothing is posted until one is picked on the dashboard.'}{' '}
        Left at the default text, the start message says "DOUBLED" for a 2x Happy Hour and the
        end message skips the time added when nothing came in.
      </p>
      {TWITCH_KINDS.map((kind) => (
        <NotificationCard
          key={kind}
          title={NOTIFICATION_INFO[kind].title}
          description={NOTIFICATION_INFO[kind].description}
          value={announcements[kind]}
          onChange={(value) => update(kind, value)}
          defaultTemplate={settings.defaultTemplates[kind]}
          placeholders={settings.placeholders[kind]}
          maxLength={settings.maxTemplateLength}
          preview={
            preview && (
              <TwitchChatPreview
                messages={preview.filter((m) => m.kind === kind)}
                channel={settings.channel}
              />
            )
          }
        />
      ))}
      <div className="control-inline">
        <button type="submit" disabled={busy}>
          {saved ? 'Saved!' : 'Save Twitch announcements'}
        </button>
      </div>
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

/**
 * Happy Hour scheduling and announcement settings (Discord and Twitch
 * chat), moved off the dashboard (which keeps the live Start/End
 * control). Owner/moderator only, like the dashboard.
 */
export default function HappyHours() {
  const { timerId } = useParams<{ timerId: string }>()
  const { snapshot } = useSubathon(timerId)
  const [discord, setDiscord] = useState<DiscordSettings | null>(null)
  const [discordError, setDiscordError] = useState<string | null>(null)
  const [twitch, setTwitch] = useState<TwitchAnnouncementSettings | null>(null)
  const [twitchError, setTwitchError] = useState<string | null>(null)

  useEffect(() => {
    if (!timerId) return
    getDiscordSettings(timerId)
      .then(setDiscord)
      .catch((err) => setDiscordError(controlErrorMessage(err)))
    getTwitchAnnouncements(timerId)
      .then(setTwitch)
      .catch((err) => setTwitchError(controlErrorMessage(err)))
  }, [timerId])

  if (!timerId) {
    return (
      <div className="dashboard">
        <p>No timer ID in URL.</p>
        <Link to="/">Back to timers</Link>
      </div>
    )
  }

  return (
    <div className="dashboard dashboard-wide">
      <header>
        <div>
          <Link to={`/t/${timerId}`} className="back-link">
            &larr; Timer
          </Link>
          <h1>Happy Hours</h1>
        </div>
      </header>

      <section className="events">
        <h2>Scheduled Happy Hours</h2>
        <p className="empty">
          Queue a Happy Hour to start at a date &amp; time, or once the timer counts down to a
          given amount of time left. Start or end one right now from the dashboard.
        </p>
        <ScheduledHappyHours timerId={timerId} scheduleVersion={snapshot?.happyHourScheduleVersion} />
      </section>

      <section className="events">
        <h2>Discord announcements</h2>
        {discordError && <p className="error-message">{discordError}</p>}
        {discord ? (
          <>
            <DiscordWebhookForm timerId={timerId} settings={discord} onSaved={setDiscord} />
            <DiscordNotificationsForm timerId={timerId} settings={discord} onSaved={setDiscord} />
          </>
        ) : (
          !discordError && <p className="empty">Loading…</p>
        )}
      </section>

      <section className="events">
        <h2>Twitch chat announcements</h2>
        {twitchError && <p className="error-message">{twitchError}</p>}
        {twitch ? (
          <TwitchAnnouncementsForm timerId={timerId} settings={twitch} onSaved={setTwitch} />
        ) : (
          !twitchError && <p className="empty">Loading…</p>
        )}
      </section>
    </div>
  )
}
