import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { createTimer, listTimers } from '../lib/api'
import { useAuth } from '../lib/AuthContext'
import { formatDuration } from '../lib/format'
import type { TimerSummary } from '../types'

export default function Timers() {
  const { user } = useAuth()
  const [timers, setTimers] = useState<TimerSummary[] | null>(null)
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)

  const refresh = () => {
    listTimers()
      .then(setTimers)
      .catch(() => setTimers([]))
  }

  // Only logged-in users have any timers to list — skip the (otherwise
  // 401ing) fetch entirely while logged out or still loading auth state.
  useEffect(() => {
    if (user) refresh()
  }, [user])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await createTimer(name || 'Subathon')
      setName('')
      refresh()
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="dashboard">
      <header>
        <div>
          <h1>Multi Stream Subathon</h1>
          <p className="tagline">
            Run a countdown clock that extends automatically from subs,
            gifted subs, bits/Kicks, Super Chats, and donations — across
            Twitch, Kick, and YouTube at once. Control it from one
            dashboard and display it with an OBS browser-source overlay.
          </p>
        </div>
        <div className="header-links">
          {user ? (
            <Link to="/account" className="back-link">
              {user.displayName}
            </Link>
          ) : (
            <Link to="/login" className="login-link">
              Log in
            </Link>
          )}
        </div>
      </header>

      {user === undefined ? (
        <p className="empty">Loading…</p>
      ) : user === null ? (
        <section className="events">
          <p className="empty">
            Log in with Twitch, Kick, or YouTube to view or create your
            subathon timers.
          </p>
        </section>
      ) : (
        <>
          <section className="events">
            {timers === null ? (
              <p className="empty">Loading…</p>
            ) : timers.length === 0 ? (
              <p className="empty">No timers yet. Create one below.</p>
            ) : (
              <ul className="timer-list">
                {timers.map((t) => (
                  <li key={t.id}>
                    <Link to={`/t/${t.id}`}>{t.name}</Link>
                    {!t.owner && <span className="platform">moderator</span>}
                    <span className={t.running ? 'status-ok' : 'status-down'}>
                      {t.running ? 'running' : 'stopped'}
                    </span>
                    <span>{formatDuration(t.remainingSecs)}</span>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className="controls">
            <form className="control-group" onSubmit={handleCreate}>
              <label>
                New timer name
                <input
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="Main"
                />
              </label>
              <button type="submit" disabled={busy}>
                Create timer
              </button>
            </form>
          </section>
        </>
      )}
    </div>
  )
}
