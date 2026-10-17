import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { useSubathon } from '../lib/useSubathon'
import { getMilestoneAlerts } from '../lib/api'
import { formatDuration, formatWholeMoney, secondsUntil } from '../lib/format'
import { useMilestoneAlertSounds } from '../lib/alertSound'
import { DEFAULT_EXPLOSION_COLOR, explosionColorStyle, ExplosionBurst } from '../lib/explosion'
import { DEFAULT_HAPPY_HOUR_STYLE, HappyHourWrap } from '../lib/happyHour'
import { DEFAULT_OVERLAY_COLORS } from '../lib/overlayColors'
import { DEFAULT_STAT_ICONS, RotatingStatPill } from '../lib/statRotation'
import type { MilestoneAlert } from '../types'

/**
 * Minimal, transparent-background view meant to be added as an OBS
 * browser source, e.g. http://localhost:5173/t/<timerId>/overlay. The
 * timerId in the URL is the token that tells this view which timer's
 * clock to display.
 */
export default function Overlay() {
  const { timerId } = useParams<{ timerId: string }>()
  const { snapshot } = useSubathon(timerId)
  const [alerts, setAlerts] = useState<MilestoneAlert[]>([])

  // Refetched whenever alertsVersion bumps (see Styling.tsx's alert
  // editor) — same pattern as GoalsOverlay refetching money milestones
  // off milestonesVersion, since neither list is part of the snapshot
  // itself.
  useEffect(() => {
    if (!timerId) return
    getMilestoneAlerts(timerId)
      .then(setAlerts)
      .catch(() => setAlerts([]))
  }, [timerId, snapshot?.alertsVersion])

  // Plays each alert's chime as the clock counts down past it — called
  // unconditionally (before the early returns below) since hooks can't
  // be called conditionally; it already no-ops on a hidden/absent
  // snapshot.
  useMilestoneAlertSounds(snapshot, alerts)

  // The dashboard route wants an opaque page background; this route needs
  // to be transparent so OBS composites it over the scene.
  useEffect(() => {
    const prev = document.documentElement.style.background
    document.documentElement.style.background = 'transparent'
    return () => {
      document.documentElement.style.background = prev
    }
  }, [])

  if (!timerId) {
    return (
      <div className="overlay">
        <div className="overlay-error">No timer ID in URL.</div>
      </div>
    )
  }

  // Hidden (see Timer.SetHidden / the dashboard's Hide button / a
  // channel moderator's "!timer hide" chat command) means render nothing
  // at all, not just a blank clock — this is meant to actually disappear
  // from the stream.
  if (snapshot?.hidden) {
    return <div className="overlay" />
  }

  const colors = snapshot?.overlayColors ?? DEFAULT_OVERLAY_COLORS
  const icons = snapshot?.statIcons ?? DEFAULT_STAT_ICONS

  // True from the moment remainingSecs first reaches 0 onward (whether
  // that happens live while this page is open, or it's already 0 when
  // the page loads) — and back to false the instant more time is added,
  // so a reset/extend after the finale resumes a normal ticking pill
  // rather than staying "exploded" forever.
  const exploded = Boolean(snapshot?.explosionEnabled) && (snapshot?.remainingSecs ?? 1) <= 0
  const explosionColor = snapshot?.explosionColor ?? DEFAULT_EXPLOSION_COLOR

  return (
    <div className="overlay">
      <HappyHourWrap
        // The finale takes over once the clock hits zero, even if a happy
        // hour is still technically running.
        active={Boolean(snapshot?.boostActive) && !exploded}
        happyHourStyle={snapshot?.happyHourStyle ?? DEFAULT_HAPPY_HOUR_STYLE}
        multiplier={snapshot?.boostMultiplier ?? 2}
        secondsLeft={snapshot?.boostEndsAt ? secondsUntil(snapshot.boostEndsAt) : undefined}
      >
        <div
          className={exploded ? 'overlay-explosion-wrap' : undefined}
          style={exploded ? explosionColorStyle(explosionColor) : undefined}
        >
          {exploded && <ExplosionBurst color={explosionColor} />}
          <div
            className={exploded ? 'overlay-pill-group overlay-exploded' : 'overlay-pill-group'}
            style={{ background: colors.timerBg }}
          >
            {snapshot?.statsRotationEnabled ? (
              <>
                <RotatingStatPill
                  colors={colors}
                  icons={icons}
                  subsGiven={snapshot.subsGiven}
                  bitsGiven={snapshot.bitsGiven}
                  donationsGiven={snapshot.donationsGiven}
                />
                <div
                  className="overlay-pill-divider"
                  style={{ background: colors.timerText }}
                />
              </>
            ) : null}
            {snapshot?.moneyGoal ? (
              <>
                <div
                  className="overlay-pill"
                  style={{ background: colors.moneyBg, color: colors.moneyText }}
                >
                  <span className="overlay-money-label">
                    ${formatWholeMoney(snapshot.totalMoneyRaised)} / $
                    {formatWholeMoney(snapshot.moneyGoal)}
                  </span>
                </div>
                <div
                  className="overlay-pill-divider"
                  style={{ background: colors.timerText }}
                />
              </>
            ) : null}
            <div
              className="overlay-pill"
              style={{ background: colors.timerBg, color: colors.timerText }}
            >
              <span className="overlay-clock">
                {snapshot ? formatDuration(snapshot.remainingSecs) : '--:--:--'}
              </span>
            </div>
          </div>
        </div>
      </HappyHourWrap>
    </div>
  )
}
