import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useSubathon } from '../lib/useSubathon'
import {
  addManualEvent,
  addModerator,
  getModerators,
  getMoneyMilestones,
  getMoneyRules,
  getRewardRules,
  getStreamElementsStatus,
  getTwitchChannels,
  hideSubathon,
  HttpError,
  listTimers,
  lockSubathon,
  removeModerator,
  resetSubathon,
  resumeSubathon,
  setKickChannel,
  setMoneyGoal,
  setMoneyRaised,
  setOverlayColors,
  setStreamElementsToken,
  setTwitchChannel,
  stopSubathon,
  unhideSubathon,
  unlockSubathon,
} from '../lib/api'
import { formatDuration, formatMoney } from '../lib/format'
import type {
  AuthPlatform,
  Moderator,
  MoneyMilestone,
  MoneyRules,
  OverlayColors,
  RewardRules,
  TwitchChannelOption,
} from '../types'

function controlErrorMessage(err: unknown): string {
  if (err instanceof HttpError) {
    if (err.status === 403) return "You don't own this timer."
    if (err.status === 401) return 'Please log in again.'
    if (err.message) return err.message
  }
  return 'That action failed. Please try again.'
}

/** A "which channel should this timer watch" form for one platform. Only
 * mount this once the current value is known (e.g. after the snapshot has
 * loaded) — its input's starting value is captured once, on mount. */
function ChannelForm({
  label,
  placeholder,
  initialUsername,
  onSave,
}: {
  label: string
  placeholder: string
  initialUsername: string
  onSave: (username: string) => Promise<unknown>
}) {
  const [input, setInput] = useState(initialUsername)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setSaved(false)
    setError(null)
    try {
      await onSave(input.trim())
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="control-group" onSubmit={handleSubmit}>
      <label>
        {label}
        <input
          type="text"
          value={input}
          onChange={(e) => {
            setInput(e.target.value)
            setSaved(false)
          }}
          placeholder={placeholder}
        />
      </label>
      <button type="submit" disabled={busy}>
        {saved ? 'Saved!' : 'Save'}
      </button>
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

/** Connects this timer to a StreamElements account so its tips
 * automatically add time/money via the "Donation (per $1)" reward/money
 * rules for the "streamelements" platform (see the Rewards page) — the
 * server polls for new tips every ~20s once connected. The token itself
 * is never re-displayed after saving (it's a secret): this only ever
 * shows connected/disconnected status and, once connected, the account's
 * display name. */
function StreamElementsForm({ timerId }: { timerId: string }) {
  const [connected, setConnected] = useState(false)
  const [displayName, setDisplayName] = useState('')
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getStreamElementsStatus(timerId)
      .then((status) => {
        setConnected(status.connected)
        setDisplayName(status.displayName ?? '')
      })
      .catch(() => setError('Failed to load StreamElements status.'))
  }, [timerId])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const status = await setStreamElementsToken(timerId, token.trim())
      setConnected(status.connected)
      setDisplayName(status.displayName ?? '')
      setToken('')
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const handleDisconnect = async () => {
    setBusy(true)
    setError(null)
    try {
      await setStreamElementsToken(timerId, '')
      setConnected(false)
      setDisplayName('')
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="control-group" onSubmit={handleSubmit}>
      <label>
        <span
          className={`status ${connected ? 'status-ok' : 'status-down'}`}
        >
          {connected ? `Connected as ${displayName}` : 'Not connected'}
        </span>
        StreamElements JWT token
        <input
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="from streamelements.com/dashboard/account/channels"
          autoComplete="off"
        />
      </label>
      <button type="submit" disabled={busy || !token.trim()}>
        {connected ? 'Reconnect' : 'Connect'}
      </button>
      {connected && (
        <button type="button" onClick={handleDisconnect} disabled={busy}>
          Disconnect
        </button>
      )}
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

/** The dollar-goal control: how much money this timer is trying to raise
 * (see the "Money raised per contribution" grid on Reward settings for
 * what actually counts toward it). 0 means no goal set. */
function MoneyGoalForm({
  initialGoal,
  onSave,
}: {
  initialGoal: number
  onSave: (goal: number) => Promise<unknown>
}) {
  const [input, setInput] = useState(initialGoal)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setSaved(false)
    setError(null)
    try {
      await onSave(input)
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="control-group" onSubmit={handleSubmit}>
      <label>
        Dollar goal ($, 0 = none)
        <input
          type="number"
          min={0}
          step={0.01}
          value={input}
          onChange={(e) => {
            setInput(Math.max(0, Number(e.target.value) || 0))
            setSaved(false)
          }}
        />
      </label>
      <button type="submit" disabled={busy}>
        {saved ? 'Saved!' : 'Save'}
      </button>
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

/** Directly overrides the running dollar total (see "Money raised per
 * contribution" on Reward settings for what normally drives it via
 * events) — for reconciling against an external donation tracker rather
 * than reworking every rule. Doesn't add a history entry. */
function MoneyRaisedForm({
  initialAmount,
  onSave,
}: {
  initialAmount: number
  onSave: (amount: number) => Promise<unknown>
}) {
  const [input, setInput] = useState(initialAmount)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setSaved(false)
    setError(null)
    try {
      await onSave(input)
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="control-group" onSubmit={handleSubmit}>
      <label>
        Current dollar amount raised
        <input
          type="number"
          min={0}
          step={0.01}
          value={input}
          onChange={(e) => {
            setInput(Math.max(0, Number(e.target.value) || 0))
            setSaved(false)
          }}
        />
      </label>
      <button type="submit" disabled={busy}>
        {saved ? 'Saved!' : 'Save'}
      </button>
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

/** A read-only list of this timer's configured dollar milestones, marking
 * each one reached once totalMoneyRaised crosses its amount. "Reached" is
 * computed here rather than stored, since it's just a comparison against
 * state the dashboard/overlay already has live via the snapshot. Always
 * shows each milestone's real label, even a "Hidden" one — that flag only
 * affects the public goals overlay (see GoalsOverlay), not this owner/
 * moderator-only view. */
function MilestonesList({
  milestones,
  totalMoneyRaised,
}: {
  milestones: MoneyMilestone[]
  totalMoneyRaised: number
}) {
  if (milestones.length === 0) return null
  return (
    <ul className="milestone-progress-list">
      {milestones.map((m) => {
        const reached = totalMoneyRaised >= m.amount
        return (
          <li key={m.amount} className={reached ? 'milestone-reached' : ''}>
            <span
              className={`status ${reached ? 'status-ok' : 'status-down'}`}
            >
              {reached ? '✓' : `$${formatMoney(m.amount)}`}
            </span>
            <span>{m.label}</span>
            {m.hidden && (
              <span className="milestone-hidden-tag">hidden on overlay</span>
            )}
          </li>
        )
      })}
    </ul>
  )
}

/** Records a real donation for one contributor — e.g. a cash app/Venmo/
 * PayPal gift with no live webhook — as a proper history entry. Seconds
 * and money added are computed from the timer's "Donation (per $1)"
 * reward/money rules for the chosen platform (see the Rewards page),
 * same as a live sub/bits/gift-sub event would be — not typed in
 * directly, so it stays consistent with whatever rates are configured
 * instead of needing the owner to do that math themselves. Distinct from
 * the raw "Add test event" tool below, which lets you type arbitrary
 * seconds/dollars for testing rather than reflecting a real rate. */
function AddDonationForm({ timerId }: { timerId: string }) {
  const [rewardRules, setRewardRules] = useState<RewardRules | null>(null)
  const [moneyRules, setMoneyRules] = useState<MoneyRules | null>(null)
  const [username, setUsername] = useState('')
  const [platform, setPlatform] = useState<AuthPlatform>('twitch')
  const [amount, setAmount] = useState(0)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getRewardRules(timerId)
      .then(setRewardRules)
      .catch(() => setError('Failed to load donation reward rate.'))
    getMoneyRules(timerId)
      .then(setMoneyRules)
      .catch(() => setError('Failed to load donation money rate.'))
  }, [timerId])

  const secondsPerDollar = rewardRules?.donation_unit[platform] ?? 0
  const moneyPerDollar = moneyRules?.donation_unit[platform] ?? 0

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!username.trim() || amount <= 0) return
    setBusy(true)
    setSaved(false)
    setError(null)
    try {
      await addManualEvent(timerId, {
        platform,
        type: 'donation',
        username,
        secondsAdded: Math.round(amount * secondsPerDollar),
        moneyAdded: amount * moneyPerDollar || undefined,
        amount,
      })
      setUsername('')
      setAmount(0)
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="control-group" onSubmit={handleSubmit}>
      <label>
        Donor username
        <input
          type="text"
          value={username}
          onChange={(e) => {
            setUsername(e.target.value)
            setSaved(false)
          }}
          placeholder="donor123"
        />
      </label>
      <label>
        Platform
        <select
          value={platform}
          onChange={(e) => setPlatform(e.target.value as AuthPlatform)}
        >
          <option value="twitch">Twitch</option>
          <option value="kick">Kick</option>
          <option value="youtube">YouTube</option>
        </select>
      </label>
      <label>
        Amount donated ($)
        <input
          type="number"
          min={0.01}
          step={0.01}
          value={amount}
          onChange={(e) => {
            setAmount(Math.max(0, Number(e.target.value) || 0))
            setSaved(false)
          }}
        />
      </label>
      <button type="submit" disabled={busy || !username.trim() || amount <= 0}>
        {saved ? 'Added!' : 'Add donation'}
      </button>
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

/** Color pickers for both public overlays' pills — /overlay's timer and
 * money-goal pills, and /goals-overlay's per-milestone goal pill and its
 * nested dollar-amount pill (background + text color for each). Native
 * <input type="color"> inputs, no library needed — they always hold a
 * 6-digit hex value, matching what the server accepts. */
function OverlayColorsForm({
  initialColors,
  onSave,
}: {
  initialColors: OverlayColors
  onSave: (colors: OverlayColors) => Promise<unknown>
}) {
  const [colors, setColors] = useState(initialColors)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleChange = (field: keyof OverlayColors, value: string) => {
    setColors((prev) => ({ ...prev, [field]: value }))
    setSaved(false)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setSaved(false)
    setError(null)
    try {
      await onSave(colors)
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="control-group" onSubmit={handleSubmit}>
      <label>
        Timer pill background
        <input
          type="color"
          value={colors.timerBg}
          onChange={(e) => handleChange('timerBg', e.target.value)}
        />
      </label>
      <label>
        Timer pill text
        <input
          type="color"
          value={colors.timerText}
          onChange={(e) => handleChange('timerText', e.target.value)}
        />
      </label>
      <label>
        Money pill background
        <input
          type="color"
          value={colors.moneyBg}
          onChange={(e) => handleChange('moneyBg', e.target.value)}
        />
      </label>
      <label>
        Money pill text
        <input
          type="color"
          value={colors.moneyText}
          onChange={(e) => handleChange('moneyText', e.target.value)}
        />
      </label>
      <label>
        Goal pill background
        <input
          type="color"
          value={colors.goalBg}
          onChange={(e) => handleChange('goalBg', e.target.value)}
        />
      </label>
      <label>
        Goal pill text
        <input
          type="color"
          value={colors.goalText}
          onChange={(e) => handleChange('goalText', e.target.value)}
        />
      </label>
      <label>
        Goal amount pill background
        <input
          type="color"
          value={colors.goalAmountBg}
          onChange={(e) => handleChange('goalAmountBg', e.target.value)}
        />
      </label>
      <label>
        Goal amount pill text
        <input
          type="color"
          value={colors.goalAmountText}
          onChange={(e) => handleChange('goalAmountText', e.target.value)}
        />
      </label>
      <button type="submit" disabled={busy}>
        {saved ? 'Saved!' : 'Save'}
      </button>
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

/** A read-only URL field with its own "Copy" button and copied-state
 * feedback — used for the goals-overlay link, same pattern as the main
 * overlay URL field above it but self-contained since there's now more
 * than one overlay URL to show. */
function OverlayUrlField({ label, url }: { label: string; url: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // clipboard API unavailable; the URL is still shown for manual copy
    }
  }

  return (
    <label>
      {label}
      <div className="overlay-link-row">
        <input type="text" readOnly value={url} />
        <button type="button" onClick={handleCopy}>
          {copied ? 'Copied!' : 'Copy'}
        </button>
      </div>
    </label>
  )
}

/** The Twitch "which channel should this timer watch" picker: a dropdown
 * of the signed-in user's own channel plus every channel they moderate
 * (see GET /api/twitch/channels), rather than free text — a pick only
 * ever works for one of these (see handleSetTwitchChannel), so limiting
 * the choices to them avoids picking a channel that'll just sit inactive.
 * Only mount this once the current value is known (e.g. after the
 * snapshot has loaded). */
function TwitchChannelSelect({
  initialUsername,
  onSave,
}: {
  initialUsername: string
  onSave: (username: string) => Promise<unknown>
}) {
  const [options, setOptions] = useState<TwitchChannelOption[] | null>(null)
  const [value, setValue] = useState(initialUsername)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getTwitchChannels()
      .then((opts) => {
        setOptions(opts)
        // Twitch logins are case-insensitive, but a timer's saved channel
        // and these options aren't guaranteed to agree on casing (see
        // twitch.Client.LookupBroadcasterID) — once we know the options,
        // snap the selection to whichever one matches so the <select>
        // (which compares its value to each <option> exactly) actually
        // shows it as selected instead of appearing unset.
        const match = opts.find(
          (o) => o.username.toLowerCase() === initialUsername.toLowerCase(),
        )
        if (match) setValue(match.username)
      })
      .catch(() => setOptions([]))
  }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setSaved(false)
    setError(null)
    try {
      await onSave(value)
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  // The timer's already-saved channel might not be one of this viewer's
  // options (someone else configured it, or it's no longer moderated) —
  // keep it selectable rather than silently swapping the dropdown to a
  // different value out from under them. Compared case-insensitively:
  // Twitch logins are case-insensitive, and the saved value and these
  // options aren't guaranteed to agree on casing.
  const knowsCurrent =
    !initialUsername ||
    options?.some(
      (o) => o.username.toLowerCase() === initialUsername.toLowerCase(),
    )

  return (
    <form className="control-group" onSubmit={handleSubmit}>
      <label>
        Twitch channel to watch
        {options === null ? (
          <input type="text" value="Loading…" disabled readOnly />
        ) : options.length === 0 ? (
          <input type="text" value="No channels available" disabled readOnly />
        ) : (
          <select
            value={value}
            onChange={(e) => {
              setValue(e.target.value)
              setSaved(false)
            }}
          >
            <option value="">None</option>
            {!knowsCurrent && (
              <option value={initialUsername}>
                {initialUsername} (no longer available)
              </option>
            )}
            {options.map((opt) => (
              <option key={opt.id} value={opt.username}>
                {opt.username} {opt.mine ? '(you)' : '(moderator)'}
              </option>
            ))}
          </select>
        )}
      </label>
      <button type="submit" disabled={busy || options === null}>
        {saved ? 'Saved!' : 'Save'}
      </button>
      {error && <p className="error-message">{error}</p>}
      {options !== null && options.length === 0 && (
        <p className="empty">
          Link your Twitch account (or become a moderator somewhere) from{' '}
          <Link to="/account">Account</Link> to pick a channel.
        </p>
      )}
    </form>
  )
}

/** Lists this timer's moderators, and — only for the owner — lets them
 * add or remove one by Twitch/Kick username. Moderators get the same
 * control over the timer as the owner, just not over this list. */
function ModeratorsSection({
  timerId,
  isOwner,
}: {
  timerId: string
  isOwner: boolean | null
}) {
  const [moderators, setModerators] = useState<Moderator[] | null>(null)
  const [usernameInput, setUsernameInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refresh = () => {
    getModerators(timerId)
      .then(setModerators)
      .catch(() => setModerators([]))
  }

  useEffect(refresh, [timerId])

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault()
    const username = usernameInput.trim()
    if (!username) return
    setBusy(true)
    setError(null)
    try {
      await addModerator(timerId, username)
      setUsernameInput('')
      refresh()
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const handleRemove = async (userId: string) => {
    setBusy(true)
    setError(null)
    try {
      await removeModerator(timerId, userId)
      refresh()
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="control-group">
      <label>Moderators</label>
      {moderators === null ? (
        <p className="empty">Loading…</p>
      ) : moderators.length === 0 ? (
        <p className="empty">No moderators yet.</p>
      ) : (
        <ul>
          {moderators.map((m) => (
            <li key={m.userId}>
              {m.displayName}
              {isOwner && (
                <button
                  type="button"
                  onClick={() => handleRemove(m.userId)}
                  disabled={busy}
                >
                  Remove
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {isOwner && (
        <form onSubmit={handleAdd}>
          <input
            type="text"
            value={usernameInput}
            onChange={(e) => setUsernameInput(e.target.value)}
            placeholder="their Twitch or Kick username"
          />
          <button type="submit" disabled={busy}>
            Add moderator
          </button>
        </form>
      )}
      {error && <p className="error-message">{error}</p>}
    </div>
  )
}

export default function Dashboard() {
  const { timerId } = useParams<{ timerId: string }>()
  const { snapshot, connected } = useSubathon(timerId)
  const [initialMinutes, setInitialMinutes] = useState(60)
  const [username, setUsername] = useState('')
  const [secondsAdded, setSecondsAdded] = useState(60)
  const [moneyAdded, setMoneyAdded] = useState(0)
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isOwner, setIsOwner] = useState<boolean | null>(null)
  const [milestones, setMilestones] = useState<MoneyMilestone[]>([])

  useEffect(() => {
    if (!timerId) return
    listTimers()
      .then((timers) => {
        setIsOwner(timers.find((t) => t.id === timerId)?.owner ?? false)
      })
      .catch(() => setIsOwner(false))
  }, [timerId])

  useEffect(() => {
    if (!timerId) return
    getMoneyMilestones(timerId)
      .then(setMilestones)
      .catch(() => setMilestones([]))
  }, [timerId])

  if (!timerId) {
    return (
      <div className="dashboard">
        <p>No timer ID in URL.</p>
        <Link to="/">Back to timers</Link>
      </div>
    )
  }

  const overlayUrl = `${window.location.origin}/t/${timerId}/overlay`
  const goalsOverlayUrl = `${window.location.origin}/t/${timerId}/goals-overlay`

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

  // Shared by the lock/unlock and hide/unhide buttons below — same
  // action a channel moderator's "!timer lock"/"!timer unlock"/
  // "!timer hide"/"!timer unhide" chat command runs, just reached from
  // the dashboard instead of chat.
  const handleToggle = async (action: (timerId: string) => Promise<unknown>) => {
    setBusy(true)
    setError(null)
    try {
      await action(timerId)
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
        moneyAdded: moneyAdded || undefined,
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
        <Link to={`/t/${timerId}/history`} className="back-link">
          History
        </Link>
        <Link to={`/t/${timerId}/rewards`} className="back-link">
          Reward settings
        </Link>
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
        {snapshot &&
          (snapshot.totalMoneyRaised > 0 ||
            snapshot.moneyGoal ||
            milestones.length > 0) && (
          <div className="money-progress">
            <div className="clock-meta">
              ${formatMoney(snapshot.totalMoneyRaised)} raised
              {snapshot.moneyGoal
                ? ` of $${formatMoney(snapshot.moneyGoal)} goal`
                : ''}
            </div>
            <MilestonesList
              milestones={milestones}
              totalMoneyRaised={snapshot.totalMoneyRaised}
            />
          </div>
        )}
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
        <OverlayUrlField
          label="Goals overlay URL (a separate OBS browser source for the milestone list)"
          url={goalsOverlayUrl}
        />
      </section>

      <section className="controls">
        {snapshot && (
          <>
            <TwitchChannelSelect
              initialUsername={snapshot.twitchChannel ?? ''}
              onSave={(username) => setTwitchChannel(timerId, username)}
            />
            <ChannelForm
              label="Kick channel to watch"
              placeholder="channel slug, e.g. xqc"
              initialUsername={snapshot.kickChannel ?? ''}
              onSave={(username) => setKickChannel(timerId, username)}
            />
            <StreamElementsForm timerId={timerId} />
            <MoneyGoalForm
              initialGoal={snapshot.moneyGoal ?? 0}
              onSave={(goal) => setMoneyGoal(timerId, goal)}
            />
            <MoneyRaisedForm
              initialAmount={snapshot.totalMoneyRaised}
              onSave={(amount) => setMoneyRaised(timerId, amount)}
            />
            <OverlayColorsForm
              initialColors={snapshot.overlayColors}
              onSave={(colors) => setOverlayColors(timerId, colors)}
            />
          </>
        )}

        <ModeratorsSection timerId={timerId} isOwner={isOwner} />

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

        <div className="control-group control-group-stacked">
          <div className="control-row">
            <div className="control-toggle">
              <span
                className={`status ${snapshot?.locked ? 'status-down' : 'status-ok'}`}
              >
                {snapshot?.locked ? '🔒 Locked' : 'Unlocked'}
              </span>
              <span className="control-hint">
                {snapshot?.locked
                  ? 'Contributions no longer extend the clock'
                  : 'Contributions extend the clock as usual'}
              </span>
            </div>
            <button
              onClick={() =>
                handleToggle(snapshot?.locked ? unlockSubathon : lockSubathon)
              }
              disabled={busy}
            >
              {snapshot?.locked ? 'Unlock' : 'Lock'}
            </button>
          </div>
          <div className="control-row">
            <div className="control-toggle">
              <span
                className={`status ${snapshot?.hidden ? 'status-down' : 'status-ok'}`}
              >
                {snapshot?.hidden ? '🙈 Hidden' : 'Visible'}
              </span>
              <span className="control-hint">
                {snapshot?.hidden
                  ? 'Not shown on the public overlay'
                  : 'Shown on the public overlay'}
              </span>
            </div>
            <button
              onClick={() =>
                handleToggle(snapshot?.hidden ? unhideSubathon : hideSubathon)
              }
              disabled={busy}
            >
              {snapshot?.hidden ? 'Unhide' : 'Hide'}
            </button>
          </div>
        </div>

        <AddDonationForm timerId={timerId} />

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
          <label>
            Dollars to add
            <input
              type="number"
              min={0}
              step={0.01}
              value={moneyAdded}
              onChange={(e) =>
                setMoneyAdded(Math.max(0, Number(e.target.value) || 0))
              }
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
                {event.moneyAdded ? (
                  <span>+${formatMoney(event.moneyAdded)}</span>
                ) : null}
              </li>
            ))}
          </ul>
        ) : (
          <p className="empty">No events yet.</p>
        )}
      </section>

      <footer>
        <p>
          Once a channel is set above and this timer is running, its subs
          and gift subs add time automatically (Twitch cheers/Kicks-gifted
          too; rates are set on this timer's Reward settings page). For
          Twitch, the watched channel's own broadcaster (not just a
          moderator) must have signed into this app with Twitch at least
          once to grant permission — Kick channels work immediately, no
          sign-in needed from anyone. YouTube listeners are still stubbed
          on the backend.
        </p>
        <p>
          On Twitch, this timer's own moderators or broadcaster can also
          type <code>!timer pause</code>, <code>!timer unpause</code>,{' '}
          <code>!timer lock</code>, <code>!timer unlock</code>,{' '}
          <code>!timer hide</code>, or <code>!timer unhide</code> in that
          channel's chat — same effect as the buttons above.
        </p>
      </footer>
    </div>
  )
}
