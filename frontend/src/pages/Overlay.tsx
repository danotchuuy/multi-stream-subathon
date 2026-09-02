import { useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { useSubathon } from '../lib/useSubathon'
import { formatDuration } from '../lib/format'

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

  return (
    <div className="overlay">
      <div className="overlay-clock">
        {snapshot ? formatDuration(snapshot.remainingSecs) : '--:--:--'}
      </div>
    </div>
  )
}
