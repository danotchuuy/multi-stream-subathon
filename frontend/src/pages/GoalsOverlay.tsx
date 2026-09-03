import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { useSubathon } from '../lib/useSubathon'
import { getMoneyMilestones } from '../lib/api'
import { formatMoney } from '../lib/format'
import { DEFAULT_OVERLAY_COLORS } from '../lib/overlayColors'
import type { MoneyMilestone } from '../types'

const PULSING_DOT_COUNT = 7

/** Seven dots that pulse in sequence, standing in for a hidden
 * milestone's label (see MoneyMilestone.hidden) — a "surprise goal" that
 * still shows its amount pill and reached state, just not what it is. */
function PulsingDots() {
  return (
    <span className="overlay-goal-pulsing-dots">
      {Array.from({ length: PULSING_DOT_COUNT }, (_, i) => (
        <span key={i} style={{ animationDelay: `${i * 0.12}s` }} />
      ))}
    </span>
  )
}

/**
 * A second minimal, transparent-background OBS browser source — separate
 * from /overlay so it can be placed/sized independently in a scene — that
 * lists this timer's money milestones (see the Rewards page's "Money
 * milestones" section), e.g. http://localhost:5173/t/<timerId>/goals-overlay.
 * Each milestone renders as a pill with its label, containing a smaller
 * nested pill for the dollar amount; the amount pill switches to a
 * checkmark once totalMoneyRaised crosses it. A milestone flagged Hidden
 * still shows both pills, but its label is replaced with PulsingDots.
 */
export default function GoalsOverlay() {
  const { timerId } = useParams<{ timerId: string }>()
  const { snapshot } = useSubathon(timerId)
  const [milestones, setMilestones] = useState<MoneyMilestone[]>([])

  // The dashboard route wants an opaque page background; this route needs
  // to be transparent so OBS composites it over the scene.
  useEffect(() => {
    const prev = document.documentElement.style.background
    document.documentElement.style.background = 'transparent'
    return () => {
      document.documentElement.style.background = prev
    }
  }, [])

  useEffect(() => {
    if (!timerId) return
    getMoneyMilestones(timerId)
      .then(setMilestones)
      .catch(() => setMilestones([]))
  }, [timerId])

  if (!timerId) {
    return (
      <div className="overlay">
        <div className="overlay-error">No timer ID in URL.</div>
      </div>
    )
  }

  // Same hide semantics as the main overlay: nothing rendered at all
  // while the timer's hidden (dashboard/!hide), not just an empty list.
  if (snapshot?.hidden) {
    return <div className="overlay" />
  }

  const colors = snapshot?.overlayColors ?? DEFAULT_OVERLAY_COLORS
  const totalMoneyRaised = snapshot?.totalMoneyRaised ?? 0

  return (
    <div className="overlay">
      <div className="overlay-goals-list">
        {milestones.map((m) => {
          const reached = totalMoneyRaised >= m.amount
          return (
            <div
              key={m.amount}
              className="overlay-goal-pill"
              style={{ background: colors.goalBg, color: colors.goalText }}
            >
              <span
                className="overlay-goal-amount-pill"
                style={{
                  background: colors.goalAmountBg,
                  color: colors.goalAmountText,
                }}
              >
                {reached ? '✓' : `$${formatMoney(m.amount)}`}
              </span>
              <span className="overlay-goal-label">
                {m.hidden ? <PulsingDots /> : m.label}
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}
