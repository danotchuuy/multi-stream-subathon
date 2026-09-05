import { useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { useSubathon } from '../lib/useSubathon'
import { formatDuration, formatMoney } from '../lib/format'
import { DEFAULT_OVERLAY_COLORS } from '../lib/overlayColors'

/**
 * Minimal, transparent-background view meant to be added as an OBS
 * browser source, e.g. http://localhost:5173/t/<timerId>/overlay. The
 * timerId in the URL is the token that tells this view which timer's
 * clock to display.
 */
export default function Overlay() {
  const { timerId } = useParams<{ timerId: string }>()
  const { snapshot } = useSubathon(timerId)

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

  return (
    <div className="overlay">
      <div
        className="overlay-pill-group"
        style={{ background: colors.timerBg }}
      >
        {snapshot?.moneyGoal ? (
          <>
            <div
              className="overlay-pill"
              style={{ background: colors.moneyBg, color: colors.moneyText }}
            >
              <span className="overlay-money-label">
                ${formatMoney(snapshot.totalMoneyRaised)} / $
                {formatMoney(snapshot.moneyGoal)}
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
  )
}
