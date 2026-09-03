import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getEventHistory } from '../lib/api'
import { formatDuration, formatMoney } from '../lib/format'
import type { SubathonEvent } from '../types'

type SortKey =
  | 'occurred'
  | 'username'
  | 'platform'
  | 'type'
  | 'amount'
  | 'secondsAdded'
  | 'moneyAdded'

const SORT_LABELS: Record<SortKey, string> = {
  occurred: 'Time',
  username: 'Username',
  platform: 'Platform',
  type: 'Event',
  amount: 'Amount',
  secondsAdded: 'Time added',
  moneyAdded: 'Money added',
}

const SORT_KEYS: SortKey[] = [
  'occurred',
  'username',
  'platform',
  'type',
  'amount',
  'secondsAdded',
  'moneyAdded',
]

function compare(a: SubathonEvent, b: SubathonEvent, key: SortKey): number {
  switch (key) {
    case 'occurred':
      return a.occurred.localeCompare(b.occurred)
    case 'username':
      return a.username.localeCompare(b.username)
    case 'platform':
      return a.platform.localeCompare(b.platform)
    case 'type':
      return a.type.localeCompare(b.type)
    case 'amount':
      return (a.amount ?? 0) - (b.amount ?? 0)
    case 'secondsAdded':
      return a.secondsAdded - b.secondsAdded
    case 'moneyAdded':
      return (a.moneyAdded ?? 0) - (b.moneyAdded ?? 0)
  }
}

interface ContributorTotal {
  username: string
  events: number
  totalSeconds: number
  totalMoney: number
}

export default function History() {
  const { timerId } = useParams<{ timerId: string }>()
  const [events, setEvents] = useState<SubathonEvent[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [sortKey, setSortKey] = useState<SortKey>('occurred')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc')
  const [selected, setSelected] = useState<string | null>(null)

  useEffect(() => {
    if (!timerId) return
    getEventHistory(timerId)
      .then(setEvents)
      .catch(() => setError('Failed to load contribution history.'))
  }, [timerId])

  const contributors = useMemo(() => {
    if (!events) return []
    const totals = new Map<string, ContributorTotal>()
    for (const e of events) {
      const username = e.username || 'anonymous'
      const entry = totals.get(username) ?? {
        username,
        events: 0,
        totalSeconds: 0,
        totalMoney: 0,
      }
      entry.events += 1
      entry.totalSeconds += e.secondsAdded
      entry.totalMoney += e.moneyAdded ?? 0
      totals.set(username, entry)
    }
    return [...totals.values()].sort((a, b) => b.totalSeconds - a.totalSeconds)
  }, [events])

  const visibleEvents = useMemo(() => {
    if (!events) return []
    const filtered = selected
      ? events.filter((e) => (e.username || 'anonymous') === selected)
      : events
    const sorted = [...filtered].sort((a, b) => {
      const cmp = compare(a, b, sortKey)
      return sortDir === 'asc' ? cmp : -cmp
    })
    return sorted
  }, [events, sortKey, sortDir, selected])

  if (!timerId) {
    return (
      <div className="dashboard">
        <p>No timer ID in URL.</p>
        <Link to="/">Back to timers</Link>
      </div>
    )
  }

  const handleSort = (key: SortKey) => {
    if (key === sortKey) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortKey(key)
      setSortDir('desc')
    }
  }

  const selectedTotal = contributors.find((c) => c.username === selected)

  return (
    <div className="dashboard">
      <header>
        <div>
          <Link to={`/t/${timerId}`} className="back-link">
            &larr; Timer
          </Link>
          <h1>Contribution history</h1>
        </div>
      </header>

      {error && <p className="error-message">{error}</p>}

      {!events ? (
        <p className="empty">Loading…</p>
      ) : events.length === 0 ? (
        <p className="empty">No contributions recorded yet.</p>
      ) : (
        <>
          <section className="events">
            <h2>Contributors</h2>
            <p className="empty">
              Click a contributor to filter the history below to just their
              events.
            </p>
            <div className="reward-table-wrap">
              <table className="reward-table">
                <thead>
                  <tr>
                    <th>Username</th>
                    <th>Events</th>
                    <th>Total time added</th>
                    <th>Total money added</th>
                  </tr>
                </thead>
                <tbody>
                  {contributors.map((c) => (
                    <tr
                      key={c.username}
                      onClick={() =>
                        setSelected(c.username === selected ? null : c.username)
                      }
                      className={
                        c.username === selected ? 'selected-row' : undefined
                      }
                      style={{ cursor: 'pointer' }}
                    >
                      <th scope="row">{c.username}</th>
                      <td>{c.events}</td>
                      <td>{formatDuration(c.totalSeconds)}</td>
                      <td>{c.totalMoney ? `$${formatMoney(c.totalMoney)}` : ''}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>

          <section className="events">
            <h2>
              {selected
                ? `${selected}'s contributions`
                : 'All contributions'}
            </h2>
            {selected && selectedTotal && (
              <p className="empty">
                {selectedTotal.events} event
                {selectedTotal.events === 1 ? '' : 's'}, totaling{' '}
                {formatDuration(selectedTotal.totalSeconds)}
                {selectedTotal.totalMoney
                  ? ` and $${formatMoney(selectedTotal.totalMoney)}`
                  : ''}{' '}
                added.{' '}
                <button type="button" onClick={() => setSelected(null)}>
                  Show all contributors
                </button>
              </p>
            )}
            <div className="reward-table-wrap">
              <table className="reward-table">
                <thead>
                  <tr>
                    {SORT_KEYS.map((key) => (
                      <th key={key}>
                        <button
                          type="button"
                          onClick={() => handleSort(key)}
                          className="sort-header"
                        >
                          {SORT_LABELS[key]}
                          {sortKey === key ? (sortDir === 'asc' ? ' ▲' : ' ▼') : ''}
                        </button>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {visibleEvents.map((e) => (
                    <tr key={e.id}>
                      <td>{new Date(e.occurred).toLocaleString()}</td>
                      <th scope="row">
                        <button
                          type="button"
                          onClick={() =>
                            setSelected(
                              (e.username || 'anonymous') === selected
                                ? null
                                : e.username || 'anonymous',
                            )
                          }
                          className="link-button"
                        >
                          {e.username || 'anonymous'}
                        </button>
                      </th>
                      <td>
                        <span className="platform">{e.platform}</span>
                      </td>
                      <td>{e.type}</td>
                      <td>{e.amount ? e.amount : ''}</td>
                      <td>+{formatDuration(e.secondsAdded)}</td>
                      <td>{e.moneyAdded ? `+$${formatMoney(e.moneyAdded)}` : ''}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        </>
      )}
    </div>
  )
}
