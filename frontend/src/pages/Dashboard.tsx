import { useEffect, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { useSubathon } from '../lib/useSubathon'
import {
  addManualEvent,
  addModerator,
  disconnectStreamElements,
  endSubathon,
  getModerators,
  getMoneyMilestones,
  getMoneyRules,
  getRewardRules,
  getStreamElementsStatus,
  getTwitchChannels,
  getYouTubeChannels,
  hideSubathon,
  HttpError,
  listTimers,
  lockSubathon,
  removeEvent,
  removeModerator,
  resetSubathon,
  resumeSubathon,
  setKickChannel,
  setMoneyGoal,
  setMoneyRaised,
  setOverlayColors,
  setTwitchChannel,
  setYouTubeChannel,
  stopSubathon,
  streamElementsOAuthStartUrl,
  unendSubathon,
  unhideSubathon,
  unlockSubathon,
} from '../lib/api'
import { formatDuration, formatMoney } from '../lib/format'
import type {
  Moderator,
  MoneyMilestone,
  MoneyRules,
  OverlayColors,
  RewardItem,
  RewardPlatform,
  RewardRules,
  TwitchChannelOption,
  YouTubeChannelOption,
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

/** Connects this timer to a StreamElements account (via OAuth2 — see
 * streamElementsOAuthStartUrl) so its tips automatically add time/money
 * via the "Donation (per $1)" reward/money rules for the
 * "streamelements" platform (see the Rewards page) — the server polls
 * for new tips every ~20s once connected. This only ever shows
 * connected/disconnected status and, once connected, the account's
 * display name — never a token, which the frontend never sees at all
 * now (the server exchanges/refreshes it directly). */
function StreamElementsForm({ timerId }: { timerId: string }) {
  const [params] = useSearchParams()
  const [connected, setConnected] = useState(false)
  const [displayName, setDisplayName] = useState('')
  const [oauthConfigured, setOauthConfigured] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(
    params.get('error') ? 'Failed to connect StreamElements. Please try again.' : null,
  )

  useEffect(() => {
    getStreamElementsStatus(timerId)
      .then((status) => {
        setConnected(status.connected)
        setDisplayName(status.displayName ?? '')
        setOauthConfigured(status.oauthConfigured)
      })
      .catch(() => setError('Failed to load StreamElements status.'))
  }, [timerId])

  const handleDisconnect = async () => {
    setBusy(true)
    setError(null)
    try {
      await disconnectStreamElements(timerId)
      setConnected(false)
      setDisplayName('')
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="control-group">
      <span className={`status ${connected ? 'status-ok' : 'status-down'}`}>
        {connected ? `Connected as ${displayName}` : 'Not connected'}
      </span>
      {oauthConfigured ? (
        <a href={streamElementsOAuthStartUrl(timerId)}>
          {connected ? 'Reconnect' : 'Connect'} StreamElements
        </a>
      ) : (
        <span className="control-hint">
          StreamElements isn't configured on this server.
        </span>
      )}
      {connected && (
        <button type="button" onClick={handleDisconnect} disabled={busy}>
          Disconnect
        </button>
      )}
      {error && <p className="error-message">{error}</p>}
    </div>
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

/** What kind of contribution AddDonationForm is recording — matches
 * EventType, minus 'manual' (that's the raw "Add test event" tool
 * below). */
type DonationKind = 'sub' | 'gifted_sub' | 'bits' | 'donation'

/** Which DonationKinds are offered per platform in AddDonationForm's
 * "What was contributed" dropdown — StreamElements/Throne aren't listed
 * here since they skip the dropdown entirely (see AddDonationForm), only
 * ever recording a dollar donation. Twitch and Kick both have a genuine
 * monetized cash-equivalent action (bits/Kicks) plus subs, so a raw
 * dollar amount isn't a real contribution kind for either — that's what
 * StreamElements/Throne's own "Dollars" is for. YouTube has neither bits
 * nor a Kicks equivalent (see the Rewards page's ITEMS table), so Dollars
 * (Super Chat) stays as its one cash-like option instead. */
const DONATION_KINDS: Record<
  Exclude<RewardPlatform, 'streamelements' | 'throne'>,
  DonationKind[]
> = {
  twitch: ['sub', 'gifted_sub', 'bits'],
  kick: ['sub', 'gifted_sub', 'bits'],
  youtube: ['donation', 'sub', 'gifted_sub'],
}

const DONATION_KIND_LABELS: Record<DonationKind, string> = {
  donation: 'Dollars',
  sub: 'Sub',
  gifted_sub: 'Gifted subs',
  bits: 'Bits / Kicks',
}

/** The RewardItem/MoneyRules row a given kind/platform/tier combination
 * looks its rate up from. Only Twitch actually distinguishes sub tiers —
 * Kick prices every sub as tier 1 (it doesn't have tiers) and YouTube has
 * none either, so 'sub' on those platforms always resolves to tier1_sub
 * regardless of the picked tier. Twitch's gifted subs are tiered the same
 * way regular subs are; Kick/YouTube's gifted subs are a single flat
 * rate — see the Rewards page's ITEMS table for the same split.
 * StreamElements/Throne only ever have a donation_unit rate configured
 * (see ITEMS there too), so the UI only ever offers kind 'donation' for
 * them — sub/gifted_sub/bits below are unreachable for those platforms in
 * practice, not specially handled. */
function donationRewardItem(
  kind: DonationKind,
  platform: RewardPlatform,
  tier: 1 | 2 | 3,
): RewardItem {
  const tiered = platform === 'twitch'
  switch (kind) {
    case 'sub':
      if (!tiered) return 'tier1_sub'
      return tier === 1 ? 'tier1_sub' : tier === 2 ? 'tier2_sub' : 'tier3_sub'
    case 'gifted_sub':
      if (!tiered) return 'gifted_sub'
      return tier === 1
        ? 'gifted_tier1_sub'
        : tier === 2
          ? 'gifted_tier2_sub'
          : 'gifted_tier3_sub'
    case 'bits':
      return 'bits_100'
    case 'donation':
      return 'donation_unit'
  }
}

/** Records a real contribution for one contributor — a sub, a gifted sub,
 * bits/Kicks, or a cash app/Venmo/PayPal-style dollar gift with no live
 * webhook — as a proper history entry. Seconds and money added are
 * computed from the timer's reward/money rules for the chosen kind and
 * platform (see the Rewards page), same as a live event of that kind
 * would be — not typed in directly, so it stays consistent with whatever
 * rates are configured instead of needing the owner to do that math
 * themselves. Distinct from the raw "Add test event" tool below, which
 * lets you type arbitrary seconds/dollars for testing rather than
 * reflecting a real rate.
 *
 * "Add time to timer" is checked by default, same as a real event. When
 * unchecked, secondsAdded is sent as 0 — the contribution is still
 * recorded (money, history, count) but doesn't extend the clock, e.g.
 * for a contribution that already got credited manually or shouldn't
 * count twice. */
function AddDonationForm({ timerId }: { timerId: string }) {
  const [rewardRules, setRewardRules] = useState<RewardRules | null>(null)
  const [moneyRules, setMoneyRules] = useState<MoneyRules | null>(null)
  const [username, setUsername] = useState('')
  const [platform, setPlatform] = useState<RewardPlatform>('twitch')
  const [kind, setKind] = useState<DonationKind>('sub')
  const [tier, setTier] = useState<1 | 2 | 3>(1)
  const [count, setCount] = useState(0)
  const [addTime, setAddTime] = useState(true)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getRewardRules(timerId)
      .then(setRewardRules)
      .catch(() => setError('Failed to load reward rates.'))
    getMoneyRules(timerId)
      .then(setMoneyRules)
      .catch(() => setError('Failed to load money rates.'))
  }, [timerId])

  const item = donationRewardItem(kind, platform, tier)
  const showTier = platform === 'twitch' && (kind === 'sub' || kind === 'gifted_sub')
  const secondsPerUnit = rewardRules?.[item][platform] ?? 0
  const moneyPerUnit = moneyRules?.[item][platform] ?? 0
  // Bits/Kicks are priced per 100 (see RewardBits100); everything else is
  // priced per unit directly — same split as twitch.ParsedEvent.Seconds/
  // Money.
  const seconds =
    item === 'bits_100'
      ? Math.ceil((count * secondsPerUnit) / 100)
      : count * secondsPerUnit
  const money =
    item === 'bits_100'
      ? (count * moneyPerUnit) / 100
      : count * moneyPerUnit

  const countLabel =
    kind === 'donation'
      ? 'Amount donated ($)'
      : kind === 'bits'
        ? 'Bits/Kicks'
        : kind === 'gifted_sub'
          ? 'Subs gifted'
          : 'Number of subs'

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!username.trim() || count <= 0) return
    setBusy(true)
    setSaved(false)
    setError(null)
    try {
      await addManualEvent(timerId, {
        platform,
        type: kind,
        username,
        secondsAdded: addTime ? seconds : 0,
        moneyAdded: money || undefined,
        amount: count,
      })
      setUsername('')
      setCount(0)
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
          onChange={(e) => {
            const next = e.target.value as RewardPlatform
            setPlatform(next)
            // Keep kind valid for the new platform's options (see
            // DONATION_KINDS) rather than leaving a stale, no-longer
            // offered one selected — e.g. switching from YouTube's
            // Dollars to Twitch, which doesn't offer it.
            if (next === 'streamelements' || next === 'throne') {
              setKind('donation')
            } else if (!DONATION_KINDS[next].includes(kind)) {
              setKind(DONATION_KINDS[next][0])
            }
            setSaved(false)
          }}
        >
          <option value="twitch">Twitch</option>
          <option value="kick">Kick</option>
          <option value="youtube">YouTube</option>
          <option value="streamelements">StreamElements</option>
          <option value="throne">Throne</option>
        </select>
      </label>
      {platform === 'streamelements' || platform === 'throne' ? (
        <p className="control-hint">Recorded as a dollar donation.</p>
      ) : (
        <label>
          What was contributed
          <select
            value={kind}
            onChange={(e) => {
              setKind(e.target.value as DonationKind)
              setSaved(false)
            }}
          >
            {DONATION_KINDS[platform].map((k) => (
              <option key={k} value={k}>
                {DONATION_KIND_LABELS[k]}
              </option>
            ))}
          </select>
        </label>
      )}
      {showTier && (
        <label>
          Tier
          <select
            value={tier}
            onChange={(e) => {
              setTier(Number(e.target.value) as 1 | 2 | 3)
              setSaved(false)
            }}
          >
            <option value={1}>Tier 1</option>
            <option value={2}>Tier 2</option>
            <option value={3}>Tier 3</option>
          </select>
        </label>
      )}
      <label>
        {countLabel}
        <input
          type="number"
          min={kind === 'donation' ? 0.01 : 1}
          step={kind === 'donation' ? 0.01 : 1}
          value={count}
          onChange={(e) => {
            setCount(Math.max(0, Number(e.target.value) || 0))
            setSaved(false)
          }}
        />
      </label>
      <label className="checkbox-label">
        <input
          type="checkbox"
          checked={addTime}
          onChange={(e) => {
            setAddTime(e.target.checked)
            setSaved(false)
          }}
        />
        Add time to timer
      </label>
      <button type="submit" disabled={busy || !username.trim() || count <= 0}>
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
    <form className="control-group control-group-stacked" onSubmit={handleSubmit}>
      <div className="overlay-colors-grid">
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
      </div>
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

  // Tracks the (initialUsername, options) pair `value` was last derived
  // from, so the block below can tell whether either changed since the
  // last render — see its comment.
  const [syncedFrom, setSyncedFrom] = useState({
    username: initialUsername,
    options: options as TwitchChannelOption[] | null,
  })

  useEffect(() => {
    getTwitchChannels()
      .then(setOptions)
      .catch(() => setOptions([]))
  }, [])

  // Keep the shown selection in sync with the saved channel — both once
  // `options` loads and if another moderator changes it while this
  // dashboard is already open. Snapshot (and so twitchChannel) is pushed
  // to every viewer of this timer over the websocket, but a plain
  // `useState(initialUsername)` would otherwise only ever reflect that
  // at mount, leaving an already-open dashboard showing a stale channel
  // until reloaded. Adjusting state during render (rather than in a
  // useEffect keyed on the same values) avoids the extra commit/render
  // pass a useEffect would add for what's really a synchronous
  // derivation. Twitch logins are case-insensitive, but the saved
  // channel and these options aren't guaranteed to agree on casing (see
  // twitch.Client.LookupBroadcasterID), so match case-insensitively and
  // prefer the option's own casing so the <select> (which compares value
  // to each <option> exactly) shows it as selected instead of appearing
  // unset.
  if (syncedFrom.username !== initialUsername || syncedFrom.options !== options) {
    setSyncedFrom({ username: initialUsername, options })
    const match = options?.find(
      (o) => o.username.toLowerCase() === initialUsername.toLowerCase(),
    )
    setValue(match?.username ?? initialUsername)
  }

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
  // options — most commonly because whoever's logged in now isn't the
  // channel's broadcaster and doesn't personally moderate it on Twitch
  // (e.g. a different app-level moderator of this timer than whoever set
  // the channel originally), not because the channel is actually gone —
  // GET /api/twitch/channels only ever reflects the signed-in viewer's
  // own Twitch identity, never whether the saved channel is still valid.
  // Keep it selectable rather than silently swapping the dropdown to a
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
              <option value={initialUsername}>{initialUsername} (currently set)</option>
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

/** The YouTube "which channel should this timer watch" picker: a
 * dropdown of the signed-in user's own linked YouTube channel(s) (see
 * GET /api/youtube/channels) — unlike TwitchChannelSelect, never a
 * "channels I moderate" list, since YouTube only lets a channel's own
 * linked owner read its live chat (see handleSetYouTubeChannel). Only
 * mount this once the current value is known (e.g. after the snapshot
 * has loaded). */
function YouTubeChannelSelect({
  initialChannel,
  initialChannelId,
  onSave,
}: {
  initialChannel: string
  initialChannelId: string
  onSave: (channelId: string) => Promise<unknown>
}) {
  const [options, setOptions] = useState<YouTubeChannelOption[] | null>(null)
  const [value, setValue] = useState(initialChannelId)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getYouTubeChannels()
      .then(setOptions)
      .catch(() => setOptions([]))
  }, [])

  // Keep the shown selection in sync if another moderator changes the
  // watched channel while this dashboard is already open. Snapshot (and
  // so youtubeChannelId) is pushed to every viewer of this timer over
  // the websocket, but a plain `useState(initialChannelId)` would
  // otherwise only ever reflect that at mount, leaving an already-open
  // dashboard showing a stale channel until reloaded. Adjusted during
  // render rather than in a useEffect keyed on the same prop, since this
  // is really a synchronous derivation, not a sync with an external
  // system.
  const [syncedChannelId, setSyncedChannelId] = useState(initialChannelId)
  if (syncedChannelId !== initialChannelId) {
    setSyncedChannelId(initialChannelId)
    setValue(initialChannelId)
  }

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
  // options — most commonly because whoever's logged in now isn't the
  // channel's own linked YouTube owner (e.g. a different app-level
  // moderator of this timer than whoever set the channel originally),
  // not because the channel is actually gone — GET /api/youtube/channels
  // only ever reflects the signed-in viewer's own linked YouTube
  // identity, never whether the saved channel is still valid. Keep it
  // selectable (by ID, its title just for display) rather than silently
  // swapping the dropdown to a different value out from under them.
  const knowsCurrent =
    !initialChannelId || options?.some((o) => o.id === initialChannelId)

  return (
    <form className="control-group" onSubmit={handleSubmit}>
      <label>
        YouTube channel to watch
        {options === null ? (
          <input type="text" value="Loading…" disabled readOnly />
        ) : options.length === 0 && knowsCurrent ? (
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
              <option value={initialChannelId}>{initialChannel} (currently set)</option>
            )}
            {options.map((opt) => (
              <option key={opt.id} value={opt.id}>
                {opt.title}
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
          Link your YouTube account from <Link to="/account">Account</Link>{' '}
          to pick a channel.
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

// Options for the "Recent contributors" time-window filter. minutes is 0
// for "All time" — every other value filters events out once they're
// older than that many minutes.
const EVENTS_WINDOW_OPTIONS: { label: string; minutes: number }[] = [
  { label: 'All time', minutes: 0 },
  { label: 'Last 5 minutes', minutes: 5 },
  { label: 'Last 15 minutes', minutes: 15 },
  { label: 'Last 30 minutes', minutes: 30 },
  { label: 'Last hour', minutes: 60 },
  { label: 'Last 3 hours', minutes: 180 },
  { label: 'Last 24 hours', minutes: 1440 },
]

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
  const [eventsWindowMinutes, setEventsWindowMinutes] = useState(0)
  const [removingEventId, setRemovingEventId] = useState<string | null>(null)

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
  const throneWebhookUrl = `${window.location.origin}/webhooks/throne/${timerId}`

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

  // Removes a contribution from the recent-contributors list, reversing
  // its effect on the clock/money total server-side (see
  // Timer.RemoveEvent) — e.g. for a mistaken or fraudulent entry. Tracks
  // its own busy state (removingEventId) rather than the shared `busy`
  // flag so it doesn't disable every other control on the page while in
  // flight.
  const handleRemoveEvent = async (eventId: string) => {
    if (
      !window.confirm(
        'Remove this contribution? Its time and money will be subtracted from the timer.',
      )
    ) {
      return
    }
    setRemovingEventId(eventId)
    setError(null)
    try {
      await removeEvent(timerId, eventId)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setRemovingEventId(null)
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
            <YouTubeChannelSelect
              initialChannel={snapshot.youtubeChannel ?? ''}
              initialChannelId={snapshot.youtubeChannelId ?? ''}
              onSave={(channelId) => setYouTubeChannel(timerId, channelId)}
            />
            <StreamElementsForm timerId={timerId} />
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
            Play
          </button>
          <button onClick={handleStop} disabled={busy || !snapshot?.running}>
            Pause
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
          <div className="control-row">
            <div className="control-toggle">
              <span
                className={`status ${snapshot?.ended ? 'status-down' : 'status-ok'}`}
              >
                {snapshot?.ended ? '⏹ Ended' : 'Active'}
              </span>
              <span className="control-hint">
                {snapshot?.ended
                  ? 'No longer receiving live events or chat commands'
                  : 'Reachable by live events and "!timer ..." chat commands'}
              </span>
            </div>
            <button
              onClick={() => {
                if (
                  !snapshot?.ended &&
                  !window.confirm(
                    'End this subathon? It will stop receiving live events and "!timer ..." chat commands.',
                  )
                ) {
                  return
                }
                handleToggle(snapshot?.ended ? unendSubathon : endSubathon)
              }}
              disabled={busy}
            >
              {snapshot?.ended ? 'Reopen' : 'End subathon'}
            </button>
          </div>
        </div>

        <AddDonationForm timerId={timerId} />

        {snapshot && (
          <>
            <MoneyGoalForm
              initialGoal={snapshot.moneyGoal ?? 0}
              onSave={(goal) => setMoneyGoal(timerId, goal)}
            />
            <MoneyRaisedForm
              initialAmount={snapshot.totalMoneyRaised}
              onSave={(amount) => setMoneyRaised(timerId, amount)}
            />
          </>
        )}
      </section>

      <section className="events">
        <div className="events-header">
          <h2>Recent contributors</h2>
          <label className="events-window">
            Show
            <select
              value={eventsWindowMinutes}
              onChange={(e) => setEventsWindowMinutes(Number(e.target.value))}
            >
              {EVENTS_WINDOW_OPTIONS.map((opt) => (
                <option key={opt.minutes} value={opt.minutes}>
                  {opt.label}
                </option>
              ))}
            </select>
          </label>
        </div>
        {(() => {
          const cutoff =
            eventsWindowMinutes > 0 ? Date.now() - eventsWindowMinutes * 60_000 : null
          const visibleEvents =
            cutoff === null
              ? snapshot?.recentEvents ?? []
              : (snapshot?.recentEvents ?? []).filter(
                  (event) => new Date(event.occurred).getTime() >= cutoff,
                )

          return visibleEvents.length > 0 ? (
            <ul>
              {[...visibleEvents].reverse().map((event) => (
                <li key={event.id}>
                  <span className="platform">{event.platform}</span>
                  <span>{event.username || 'anonymous'}</span>
                  <span>
                    {event.type}
                    {event.type === 'gifted_sub' && event.amount ? ` x${event.amount}` : ''}
                  </span>
                  <span>+{formatDuration(event.secondsAdded)}</span>
                  {event.moneyAdded ? (
                    <span>+${formatMoney(event.moneyAdded)}</span>
                  ) : null}
                  <button
                    type="button"
                    className="event-remove"
                    title="Remove this contribution"
                    onClick={() => handleRemoveEvent(event.id)}
                    disabled={removingEventId === event.id}
                  >
                    {removingEventId === event.id ? '…' : '✕'}
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="empty">
              {snapshot && snapshot.recentEvents.length > 0
                ? 'No events in this time window.'
                : 'No events yet.'}
            </p>
          )
        })()}
      </section>

      <section className="overlay-link">
        <h2>Throne integration</h2>
        <p className="empty">
          <a href="https://throne.com" target="_blank" rel="noreferrer">
            Throne
          </a>{' '}
          is a wishlist/gifting platform creators can link fans to instead
          of (or alongside) Twitch/Kick/YouTube — gifts and contributions
          count toward this timer's clock and money goal using the "Throne"
          row on the Reward settings page, the same "Donation (per $1)"
          rate the other donation platforms use.
        </p>
        <p className="empty">
          To set it up: in Throne, go to{' '}
          <a
            href="https://throne.com/profile/integrations/webhook"
            target="_blank"
            rel="noreferrer"
          >
            Profile &rarr; Integrations &rarr; Webhook
          </a>
          , paste the URL below as the subscriber URL, and enable the
          integration. Every gift and contribution sent to your Throne
          wishlist will then be applied to this timer automatically —
          there's nothing to connect on this side, and this timer doesn't
          need to be running for it to apply (same as YouTube above).
        </p>
        <OverlayUrlField
          label="Throne webhook URL (paste into Throne's Webhook integration settings)"
          url={throneWebhookUrl}
        />
      </section>

      <footer>
        <p>
          Once a channel is set above and this timer is running, its subs
          and gift subs add time automatically (Twitch cheers/Kicks-gifted
          too; rates are set on this timer's Reward settings page). For
          Twitch, the watched channel's own broadcaster (not just a
          moderator) must have signed into this app with Twitch at least
          once to grant permission — Kick channels work immediately, no
          sign-in needed from anyone. YouTube channels apply regardless of
          whether this timer is running, and the watched channel's owner
          must have signed in with YouTube first.
        </p>
        <p>
          On Twitch, this timer's own moderators or broadcaster can also
          type <code>!timer pause</code>, <code>!timer play</code>,{' '}
          <code>!timer lock</code>, <code>!timer unlock</code>,{' '}
          <code>!timer hide</code>, or <code>!timer show</code> in that
          channel's chat — same effect as the buttons above.
        </p>
      </footer>
    </div>
  )
}
