import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  getMoneyMilestones,
  getMoneyRules,
  getRewardRules,
  saveMoneyMilestones,
  saveMoneyRules,
  saveRewardRules,
} from '../lib/api'
import type { MoneyMilestone, RewardItem, RewardPlatform } from '../types'

const PLATFORMS: RewardPlatform[] = ['twitch', 'kick', 'youtube', 'streamelements']

const PLATFORM_LABELS: Record<RewardPlatform, string> = {
  twitch: 'Twitch',
  kick: 'Kick',
  youtube: 'YouTube',
  streamelements: 'StreamElements',
}

const ITEMS: { key: RewardItem; label: string }[] = [
  { key: 'tier1_sub', label: 'Tier 1 sub' },
  { key: 'tier2_sub', label: 'Tier 2 sub' },
  { key: 'tier3_sub', label: 'Tier 3 sub' },
  { key: 'gifted_sub', label: 'Gifted sub' },
  { key: 'bits_100', label: 'Bits / Kicks (per 100)' },
  { key: 'donation_unit', label: 'Donation (per $1)' },
]

/** Rules mapping a RewardItem, per platform, to a number — the shape
 * shared by RewardRules (seconds) and MoneyRules (dollars). */
type Grid = Record<RewardItem, Record<RewardPlatform, number>>

/** One editable item x platform grid, e.g. "seconds per contribution" or
 * "dollars per contribution" — same table shape, different unit/values,
 * loaded and saved independently. */
function RulesGrid({
  timerId,
  unitLabel,
  step,
  getRules,
  saveRules,
}: {
  timerId: string
  unitLabel: string
  step: number
  getRules: (timerId: string) => Promise<Grid>
  saveRules: (timerId: string, rules: Grid) => Promise<Grid>
}) {
  const [rules, setRules] = useState<Grid | null>(null)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getRules(timerId)
      .then(setRules)
      .catch(() => setError(`Failed to load ${unitLabel} settings.`))
  }, [timerId, unitLabel, getRules])

  const handleChange = (
    item: RewardItem,
    platform: AuthPlatform,
    value: string,
  ) => {
    setRules((prev) => {
      if (!prev) return prev
      return {
        ...prev,
        [item]: { ...prev[item], [platform]: Math.max(0, Number(value) || 0) },
      }
    })
    setSaved(false)
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!rules) return
    setBusy(true)
    setError(null)
    try {
      await saveRules(timerId, rules)
      setSaved(true)
    } catch {
      setError(`Failed to save ${unitLabel} settings. Please try again.`)
    } finally {
      setBusy(false)
    }
  }

  if (!rules) {
    return (
      <>
        {error && <p className="error-message">{error}</p>}
        <p className="empty">Loading…</p>
      </>
    )
  }

  return (
    <form onSubmit={handleSave}>
      {error && <p className="error-message">{error}</p>}
      <div className="reward-table-wrap">
        <table className="reward-table">
          <thead>
            <tr>
              <th>Item</th>
              {PLATFORMS.map((p) => (
                <th key={p}>{PLATFORM_LABELS[p]}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {ITEMS.map(({ key, label }) => (
              <tr key={key}>
                <th scope="row">{label}</th>
                {PLATFORMS.map((p) => (
                  <td key={p}>
                    <input
                      type="number"
                      min={0}
                      step={step}
                      value={rules[key]?.[p] ?? 0}
                      onChange={(e) => handleChange(key, p, e.target.value)}
                    />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="control-group">
        <button type="submit" disabled={busy}>
          {saved ? 'Saved!' : 'Save'}
        </button>
      </div>
    </form>
  )
}

/** The list of dollar-amount milestones (e.g. "$500: extra hour added")
 * shown on the dashboard/overlay progress bar as it's crossed. Edited as a
 * free-form list rather than a fixed grid like RulesGrid, since there's no
 * natural fixed set of rows. */
function MilestonesEditor({ timerId }: { timerId: string }) {
  const [milestones, setMilestones] = useState<MoneyMilestone[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getMoneyMilestones(timerId)
      .then(setMilestones)
      .catch(() => setError('Failed to load milestones.'))
  }, [timerId])

  const handleChange = (
    index: number,
    field: 'amount' | 'label',
    value: string,
  ) => {
    setMilestones((prev) => {
      if (!prev) return prev
      const next = [...prev]
      next[index] = {
        ...next[index],
        [field]: field === 'amount' ? Math.max(0, Number(value) || 0) : value,
      }
      return next
    })
    setSaved(false)
  }

  const handleToggleHidden = (index: number) => {
    setMilestones((prev) => {
      if (!prev) return prev
      const next = [...prev]
      next[index] = { ...next[index], hidden: !next[index].hidden }
      return next
    })
    setSaved(false)
  }

  const handleAdd = () => {
    setMilestones((prev) => [
      ...(prev ?? []),
      { amount: 0, label: '', hidden: false },
    ])
    setSaved(false)
  }

  const handleRemove = (index: number) => {
    setMilestones((prev) => prev?.filter((_, i) => i !== index) ?? prev)
    setSaved(false)
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!milestones) return
    setError(null)
    const cleaned = milestones
      .filter((m) => m.label.trim() && m.amount > 0)
      .sort((a, b) => a.amount - b.amount)
    setBusy(true)
    try {
      const result = await saveMoneyMilestones(timerId, cleaned)
      setMilestones(result)
      setSaved(true)
    } catch {
      setError('Failed to save milestones. Please try again.')
    } finally {
      setBusy(false)
    }
  }

  if (!milestones) {
    return (
      <>
        {error && <p className="error-message">{error}</p>}
        <p className="empty">Loading…</p>
      </>
    )
  }

  return (
    <form onSubmit={handleSave}>
      {error && <p className="error-message">{error}</p>}
      <div className="milestone-list">
        {milestones.map((m, i) => (
          <div className="control-group" key={i}>
            <label>
              Amount ($)
              <input
                type="number"
                min={0}
                step={0.01}
                value={m.amount}
                onChange={(e) => handleChange(i, 'amount', e.target.value)}
              />
            </label>
            <label>
              Label
              <input
                type="text"
                value={m.label}
                placeholder="e.g. extra hour added"
                onChange={(e) => handleChange(i, 'label', e.target.value)}
              />
            </label>
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={m.hidden}
                onChange={() => handleToggleHidden(i)}
              />
              Hidden (surprise goal)
            </label>
            <button
              type="button"
              className="link-button"
              onClick={() => handleRemove(i)}
            >
              Remove
            </button>
          </div>
        ))}
      </div>

      <div className="control-group">
        <button type="button" onClick={handleAdd}>
          Add milestone
        </button>
        <button type="submit" disabled={busy}>
          {saved ? 'Saved!' : 'Save'}
        </button>
      </div>
    </form>
  )
}

export default function Rewards() {
  const { timerId } = useParams<{ timerId: string }>()

  if (!timerId) {
    return (
      <div className="dashboard">
        <p>No timer ID in URL.</p>
        <Link to="/">Back to timers</Link>
      </div>
    )
  }

  return (
    <div className="dashboard">
      <header>
        <div>
          <Link to={`/t/${timerId}`} className="back-link">
            &larr; Timer
          </Link>
          <h1>Reward settings</h1>
        </div>
      </header>

      <section className="events">
        <h2>Time added per contribution</h2>
        <p className="empty">
          Seconds this timer adds per contribution, by platform. Twitch subs,
          gift subs, and cheers apply these automatically once the channel is
          linked in Account settings; StreamElements tips apply the
          "Donation (per $1)" row automatically once a token is connected on
          the dashboard. Other platforms/manual entries still need a bot or
          manual event for now.
        </p>
        <RulesGrid
          timerId={timerId}
          unitLabel="reward"
          step={1}
          getRules={getRewardRules}
          saveRules={saveRewardRules}
        />
      </section>

      <section className="events">
        <h2>Money raised per contribution</h2>
        <p className="empty">
          Dollars this timer counts toward its money goal per contribution
          — independent of the time it adds above. Leave at $0 for
          anything you don't want counted toward the goal.
        </p>
        <RulesGrid
          timerId={timerId}
          unitLabel="money"
          step={0.01}
          getRules={getMoneyRules}
          saveRules={saveMoneyRules}
        />
      </section>

      <section className="events">
        <h2>Money milestones</h2>
        <p className="empty">
          Dollar-amount checkpoints shown on the dashboard as the total
          raised crosses them, e.g. "$500: extra hour added" — independent
          of (and doesn't have to match) the overall dollar goal. Mark one
          "Hidden" to keep it a surprise on the public goals overlay: the
          dashboard still shows its real label, but the overlay replaces it
          with pulsing dots (the amount pill still shows normally).
        </p>
        <MilestonesEditor timerId={timerId} />
      </section>
    </div>
  )
}
