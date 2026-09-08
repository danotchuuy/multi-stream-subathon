import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { useSubathon } from '../lib/useSubathon'
import { getLeaderboard } from '../lib/api'
import { formatCompactCount } from '../lib/format'
import { DEFAULT_PANEL_COLORS } from '../lib/overlayColors'
import { CategoryIcon, DEFAULT_STAT_ICONS, STAT_CATEGORIES } from '../lib/statRotation'
import type { Leaderboard } from '../types'

/** How often the panel re-fetches the leaderboard — this isn't a live
 * per-second HUD like Overlay.tsx, so a slow poll is plenty. */
const LEADERBOARD_REFRESH_MS = 60_000

/**
 * A standalone, opaque page showing the top 10 contributors in each
 * gift category (subs, bits/Kicks, tips/donations) — meant for a
 * channel's Twitch "panel" (the About-section tiles) or any other
 * static embed, unlike Overlay.tsx/GoalsOverlay.tsx's transparent OBS
 * browser sources: Twitch panels themselves only support a static image
 * plus a link, not a live embedded page, so this is meant to be
 * screenshotted for that, added as an OBS browser source, or just
 * linked directly — whichever fits.
 */
export default function Panel() {
  const { timerId } = useParams<{ timerId: string }>()
  const { snapshot } = useSubathon(timerId)
  const [board, setBoard] = useState<Leaderboard | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!timerId) return
    let cancelled = false

    const load = () => {
      getLeaderboard(timerId)
        .then((b) => {
          if (!cancelled) setBoard(b)
        })
        .catch(() => {
          if (!cancelled) setError('Failed to load leaderboard.')
        })
    }

    load()
    const id = setInterval(load, LEADERBOARD_REFRESH_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [timerId])

  if (!timerId) {
    return (
      <div className="panel-page">
        <p className="panel-error">No timer ID in URL.</p>
      </div>
    )
  }

  const colors = snapshot?.panelColors ?? DEFAULT_PANEL_COLORS
  const icons = snapshot?.statIcons ?? DEFAULT_STAT_ICONS

  return (
    <div className="panel-page" style={{ background: colors.bg, color: colors.text }}>
      <h1 className="panel-title">{snapshot?.name ?? 'Top Contributors'}</h1>

      {error && <p className="panel-error">{error}</p>}

      {!board ? (
        <p className="panel-loading">Loading…</p>
      ) : (
        <div className="panel-categories">
          {STAT_CATEGORIES.map((cat) => {
            const entries = board[cat.iconKey]
            return (
              <section key={cat.iconKey} className="panel-category">
                <div
                  className="panel-category-heading"
                  style={{ background: colors.accentBg, color: colors.accentText }}
                >
                  <CategoryIcon icons={icons} category={cat} className="panel-category-icon" />
                  {cat.label}
                </div>
                {entries.length === 0 ? (
                  <p className="panel-empty">No contributions yet.</p>
                ) : (
                  <ol className="panel-list">
                    {entries.map((entry, i) => (
                      <li key={entry.username}>
                        <span className="panel-rank">{i + 1}</span>
                        <span className="panel-username">{entry.username}</span>
                        <span className="panel-amount">
                          {cat.countPrefix}
                          {formatCompactCount(entry.amount)}
                        </span>
                      </li>
                    ))}
                  </ol>
                )}
              </section>
            )
          })}
        </div>
      )}
    </div>
  )
}
