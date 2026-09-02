import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useSubathon } from '../lib/useSubathon'
import {
  addManualEvent,
  HttpError,
  resetSubathon,
  resumeSubathon,
  stopSubathon,
} from '../lib/api'
import { formatDuration } from '../lib/format'

function controlErrorMessage(err: unknown): string {
  if (err instanceof HttpError) {
    if (err.status === 403) return "You don't own this timer."
    if (err.status === 401) return 'Please log in again.'
  }
  return 'That action failed. Please try again.'
}

export default function Dashboard() {
  const { timerId } = useParams<{ timerId: string }>()
  const { snapshot, connected } = useSubathon(timerId)
  const [initialMinutes, setInitialMinutes] = useState(60)
  const [username, setUsername] = useState('')
  const [secondsAdded, setSecondsAdded] = useState(60)
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (!timerId) {
    return (
      <div className="dashboard">
        <p>No timer ID in URL.</p>
        <Link to="/">Back to timers</Link>
      </div>
    )
  }

  const overlayUrl = `${window.location.origin}/t/${timerId}/overlay`

  const handleReset = async () => {
    setBusy(true)
    setError(null)
    try {
      await resetSubathon(timerId, initialMinutes * 60)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const handleResume = async () => {
    setBusy(true)
    setError(null)
    try {
      await resumeSubathon(timerId)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const handleStop = async () => {
    setBusy(true)
    setError(null)
    try {
      await stopSubathon(timerId)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const handleAddEvent = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await addManualEvent(timerId, {
        username: username || 'test_user',
        secondsAdded,
      })
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const handleCopyOverlayUrl = async () => {
    try {
      await navigator.clipboard.writeText(overlayUrl)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // clipboard API unavailable; the URL is still shown for manual copy
    }
  }

  return (
    <div className="dashboard">
      <header>
        <div>
          <Link to="/" className="back-link">
            &larr; Timers
          </Link>
          <h1>{snapshot?.name ?? 'Loading…'}</h1>
        </div>
        <span className={`status ${connected ? 'status-ok' : 'status-down'}`}>
          {connected ? 'connected' : 'disconnected'}
        </span>
      </header>

      {error && <p className="error-message">{error}</p>}

      <section className="clock-card">
        <div className="clock">
          {snapshot ? formatDuration(snapshot.remainingSecs) : '--:--:--'}
        </div>
        <div className="clock-meta">
          {snapshot?.running ? 'running' : 'stopped'} &middot; total added:{' '}
          {snapshot ? formatDuration(snapshot.totalAddedSecs) : '--:--:--'}
        </div>
      </section>

      <section className="overlay-link">
        <label>
          Overlay URL (add as an OBS browser source)
          <div className="overlay-link-row">
            <input type="text" readOnly value={overlayUrl} />
            <button type="button" onClick={handleCopyOverlayUrl}>
              {copied ? 'Copied!' : 'Copy'}
            </button>
          </div>
        </label>
      </section>

      <section className="controls">
        <div className="control-group">
          <label>
            Reset to (minutes)
            <input
              type="number"
              min={1}
              value={initialMinutes}
              onChange={(e) => setInitialMinutes(Number(e.target.value))}
            />
          </label>
          <button onClick={handleReset} disabled={busy}>
            Reset
          </button>
          <button
            onClick={handleResume}
            disabled={busy || snapshot?.running}
          >
            Start
          </button>
          <button onClick={handleStop} disabled={busy || !snapshot?.running}>
            Stop
          </button>
        </div>

        <form className="control-group" onSubmit={handleAddEvent}>
          <label>
            Test event: username
            <input
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="viewer123"
            />
          </label>
          <label>
            Seconds to add
            <input
              type="number"
              min={1}
              value={secondsAdded}
              onChange={(e) => setSecondsAdded(Number(e.target.value))}
            />
          </label>
          <button type="submit" disabled={busy}>
            Add test event
          </button>
        </form>
      </section>

      <section className="events">
        <h2>Recent contributors</h2>
        {snapshot && snapshot.recentEvents.length > 0 ? (
          <ul>
            {[...snapshot.recentEvents].reverse().map((event) => (
              <li key={event.id}>
                <span className="platform">{event.platform}</span>
                <span>{event.username || 'anonymous'}</span>
                <span>{event.type}</span>
                <span>+{formatDuration(event.secondsAdded)}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="empty">No events yet.</p>
        )}
      </section>

      <footer>
        <p>
          Kick, YouTube, and Twitch listeners are stubbed on the backend and
          not wired up yet.
        </p>
      </footer>
    </div>
  )
}
