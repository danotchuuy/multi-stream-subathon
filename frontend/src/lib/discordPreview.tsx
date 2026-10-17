import type { ReactNode } from 'react'
import type { DiscordPreviewMessage } from '../types'

/** Shown when the webhook's own name couldn't be looked up (no webhook
 * saved yet, or Discord didn't answer). */
const FALLBACK_NAME = 'Subathon Timer'

const relativeTime = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })

/** Renders a Discord <t:unix:R> timestamp the way Discord does, e.g.
 * "in 10 minutes". */
function formatRelative(unix: number): string {
  const secs = Math.round(unix - Date.now() / 1000)
  const abs = Math.abs(secs)
  if (abs < 60) return relativeTime.format(secs, 'second')
  if (abs < 3600) return relativeTime.format(Math.round(secs / 60), 'minute')
  if (abs < 86400) return relativeTime.format(Math.round(secs / 3600), 'hour')
  return relativeTime.format(Math.round(secs / 86400), 'day')
}

function formatShortTime(date: Date): string {
  return date.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

/** Timestamps and @everyone mentions, the non-bold markup the server's
 * announcements use (see internal/server/discord_announce.go). */
function renderTokens(text: string, keyPrefix: string): ReactNode[] {
  return text.split(/(<t:\d+:[Rt]>|@everyone)/).map((part, i) => {
    const key = `${keyPrefix}-${i}`
    if (part === '@everyone') {
      return (
        <span key={key} className="discord-mention">
          @everyone
        </span>
      )
    }
    const ts = /^<t:(\d+):([Rt])>$/.exec(part)
    if (ts) {
      const unix = Number(ts[1])
      return (
        <span key={key} className="discord-timestamp">
          {ts[2] === 'R' ? formatRelative(unix) : formatShortTime(new Date(unix * 1000))}
        </span>
      )
    }
    return part
  })
}

/** The subset of Discord markdown the announcements use: **bold**, which
 * can itself contain timestamps. Built as React nodes, never raw HTML. */
function renderContent(content: string): ReactNode[] {
  return content.split(/\*\*(.+?)\*\*/).map((part, i) =>
    i % 2 === 1 ? <strong key={i}>{renderTokens(part, `b${i}`)}</strong> : renderTokens(part, `t${i}`),
  )
}

/**
 * A mock Discord text channel showing each happy hour announcement the
 * way it'll look when posted: the webhook's name and avatar, an APP tag,
 * Discord's rendering of bold text, timestamps (in the viewer's own
 * timezone, as Discord shows them) and @everyone, and the highlight
 * Discord gives a message that pings you.
 */
export function DiscordChannelPreview({
  messages,
  webhookName,
  webhookAvatarUrl,
  hideHeader = false,
}: {
  messages: DiscordPreviewMessage[]
  webhookName?: string
  webhookAvatarUrl?: string
  /** Leave off the "# announcements" channel header, e.g. when showing
   * one announcement at a time. */
  hideHeader?: boolean
}) {
  const name = webhookName || FALLBACK_NAME
  const postedAt = `Today at ${formatShortTime(new Date())}`

  return (
    <div className="discord-preview" aria-label="Discord channel preview">
      {!hideHeader && (
        <div className="discord-preview-header">
          <span className="discord-hash">#</span> announcements
        </div>
      )}
      {messages.map((m) => (
        <div key={`${m.kind}-${m.label}`}>
          <div className="discord-divider">
            <span>{m.label}</span>
          </div>
          <div
            className={
              m.content.includes('@everyone') ? 'discord-message discord-message-mentioned' : 'discord-message'
            }
          >
            {webhookAvatarUrl ? (
              <img className="discord-avatar" src={webhookAvatarUrl} alt="" />
            ) : (
              <div className="discord-avatar discord-avatar-default" aria-hidden="true">
                {name.charAt(0).toUpperCase()}
              </div>
            )}
            <div className="discord-message-body">
              <div className="discord-message-meta">
                <span className="discord-username">{name}</span>
                <span className="discord-app-tag">APP</span>
                <span className="discord-posted-at">{postedAt}</span>
              </div>
              <div className="discord-content">{renderContent(m.content)}</div>
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}
