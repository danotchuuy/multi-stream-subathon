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
  | 'addedBy'

const SORT_LABELS: Record<SortKey, string> = {
  occurred: 'Time',
  username: 'Username',
  platform: 'Platform',
  type: 'Event',
  amount: 'Amount',
  secondsAdded: 'Time added',
  moneyAdded: 'Money added',
  addedBy: 'Added by',
}

const SORT_KEYS: SortKey[] = [
  'occurred',
  'username',
  'platform',
  'type',
  'amount',
  'secondsAdded',
  'moneyAdded',
  'addedBy',
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
    case 'addedBy':
      return (a.addedBy ?? '').localeCompare(b.addedBy ?? '')
  }
}

interface ContributorTotal {
  username: string
  events: number
  totalSeconds: number
  totalMoney: number
}

type ContributorSortKey = 'username' | 'events' | 'totalSeconds' | 'totalMoney'

const CONTRIBUTOR_SORT_LABELS: Record<ContributorSortKey, string> = {
  username: 'Username',
  events: 'Events',
  totalSeconds: 'Total time added',
  totalMoney: 'Total money added',
}

const CONTRIBUTOR_SORT_KEYS: ContributorSortKey[] = [
  'username',
  'events',
  'totalSeconds',
  'totalMoney',
]

function compareContributors(
  a: ContributorTotal,
  b: ContributorTotal,
  key: ContributorSortKey,
): number {
  switch (key) {
    case 'username':
      return a.username.localeCompare(b.username)
    case 'events':
      return a.events - b.events
    case 'totalSeconds':
      return a.totalSeconds - b.totalSeconds
    case 'totalMoney':
      return a.totalMoney - b.totalMoney
  }
}

// Options for the contributors panel's row limit; 0 means show everyone.
const CONTRIBUTOR_LIMIT_OPTIONS = [5, 10, 25, 50, 0]

export default function History() {
  const { timerId } = useParams<{ timerId: string }>()
  const [events, setEvents] = useState<SubathonEvent[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [sortKey, setSortKey] = useState<SortKey>('occurred')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc')
  const [contributorSortKey, setContributorSortKey] = useState<ContributorSortKey>('totalSeconds')
  const [contributorSortDir, setContributorSortDir] = useState<'asc' | 'desc'>('desc')
  const [contributorLimit, setContributorLimit] = useState(10)
  const [selected, setSelected] = useState<string | null>(null)
  // '' means all platforms.
  const [platformFilter, setPlatformFilter] = useState('')

  useEffect(() => {
    if (!timerId) return
    getEventHistory(timerId)
      .then(setEvents)
      .catch(() => setError('Failed to load contribution history.'))
  }, [timerId])

  const platforms = useMemo(
    () => [...new Set((events ?? []).map((e) => e.platform))].sort(),
    [events],
  )

  // Events after the platform filter — feeds both the contributor totals
  // and the event table, so the two always agree.
  const platformEvents = useMemo(
    () =>
      platformFilter
        ? (events ?? []).filter((e) => e.platform === platformFilter)
        : (events ?? []),
    [events, platformFilter],
  )

  const contributors = useMemo(() => {
    const totals = new Map<string, ContributorTotal>()
    for (const e of platformEvents) {
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
    return [...totals.values()]
  }, [platformEvents])

  const sortedContributors = useMemo(() => {
    return [...contributors].sort((a, b) => {
      const cmp = compareContributors(a, b, contributorSortKey)
      return contributorSortDir === 'asc' ? cmp : -cmp
    })
  }, [contributors, contributorSortKey, contributorSortDir])

  const visibleEvents = useMemo(() => {
    const filtered = selected
      ? platformEvents.filter((e) => (e.username || 'anonymous') === selected)
      : platformEvents
    const sorted = [...filtered].sort((a, b) => {
      const cmp = compare(a, b, sortKey)
      return sortDir === 'asc' ? cmp : -cmp
    })
    return sorted
  }, [platformEvents, sortKey, sortDir, selected])

  if (!timerId) {
    return (
      <div className="dashboard dashboard-history">
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

  const handleContributorSort = (key: ContributorSortKey) => {
    if (key === contributorSortKey) {
      setContributorSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setContributorSortKey(key)
      setContributorSortDir('desc')
    }
  }

  const selectedTotal = contributors.find((c) => c.username === selected)

  return (
    <div className="dashboard dashboard-history">
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
            <div className="events-header">
              <h2>Contributors</h2>
              <label className="events-window">
                Show
                <select
                  value={contributorLimit}
                  onChange={(e) => setContributorLimit(Number(e.target.value))}
                >
                  {CONTRIBUTOR_LIMIT_OPTIONS.map((n) => (
                    <option key={n} value={n}>
                      {n === 0 ? 'All' : `Top ${n}`}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            <p className="empty">
              Click a contributor to filter the history below to just their
              events.
            </p>
            <div className="reward-table-wrap">
              <table className="reward-table">
                <thead>
                  <tr>
                    {CONTRIBUTOR_SORT_KEYS.map((key) => (
                      <th key={key}>
                        <button
                          type="button"
                          onClick={() => handleContributorSort(key)}
                          className="sort-header"
                        >
                          {CONTRIBUTOR_SORT_LABELS[key]}
                          {contributorSortKey === key
                            ? contributorSortDir === 'asc'
                              ? ' ▲'
                              : ' ▼'
                            : ''}
                        </button>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {(contributorLimit > 0
                    ? sortedContributors.slice(0, contributorLimit)
                    : sortedContributors
                  ).map((c) => (
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
            <div className="events-header">
              <h2>
                {selected
                  ? `${selected}'s contributions`
                  : 'All contributions'}
              </h2>
              <label className="events-window">
                Platform
                <select
                  value={platformFilter}
                  onChange={(e) => setPlatformFilter(e.target.value)}
                >
                  <option value="">All platforms</option>
                  {platforms.map((platform) => (
                    <option key={platform} value={platform}>
                      {platform}
                    </option>
                  ))}
                </select>
              </label>
            </div>
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
                      <td>{e.addedBy ?? ''}</td>
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
