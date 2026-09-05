# multi-stream-subathon

A subathon tracker for creators streaming across multiple platforms at
once (Twitch, Kick, YouTube). It runs one or more countdown clocks that
get extended by subs, gifted subs, bits/Kicks, Super Chats, and
donations, with a dashboard to control them and an OBS browser-source
overlay to display one.

## Contents

- [Status](#status)
- [Quick start](#quick-start)
- [Project layout](#project-layout)
- [Accounts and moderators](#accounts-and-moderators)
- [Timers](#timers)
- [Reward rules, money rules, and live events](#reward-rules-money-rules-and-live-events)
  - [Money rules, goals, and milestones](#money-rules-goals-and-milestones)
  - [Overlays](#overlays)
  - [Watched channels](#watched-channels)
  - [Twitch](#twitch)
  - [Kick](#kick)
  - [YouTube](#youtube)
  - [StreamElements](#streamelements)
  - [Throne](#throne)
- [Twitch chat commands](#twitch-chat-commands)
- [Ending a timer](#ending-a-timer)
- [Storage](#storage)
- [Configuration](#configuration)

## Status

The Go backend supports multiple independent timers, each with a durable
SQLite-backed clock and contributor event history, owned by an account
created via Twitch, Kick, or YouTube (Google) login.

Live, automatic event tracking is wired up for:

- **Twitch** — subs, gift subs, cheers
- **Kick** — subs, gift subs
- **YouTube** — Super Chats, Super Stickers, new members, gifted memberships
- **StreamElements** — tips
- **Throne** — gifts, contributions

See [Reward rules, money rules, and live events](#reward-rules-money-rules-and-live-events)
for how each one activates.

## Quick start

```
make run
```

Starts the Go API and the Vite dev server together (Ctrl+C stops both).
Use `make server` / `make frontend` to run them separately in different
terminals.

Then open `http://localhost:5173/` — you'll be sent to `/login` until you
sign up with Twitch, Kick, or YouTube.

Both servers bind to all network interfaces, not just localhost, so
they're also reachable from other devices on your network (e.g. a second
PC running OBS, or a phone) — Vite prints the LAN URL it's listening on
when it starts.

> This is meant for local/trusted-network testing. The overlay/state/ws
> endpoints are intentionally public (see [Timers](#timers)), so don't
> expose these ports to the open internet without more hardening.

## Project layout

```
cmd/server/            entrypoint: wires config, db, manager, hub, HTTP server
internal/config/       env-based configuration
internal/subathon/     core domain: Timer (clock + events), Manager (multiple timers), Repo interface
internal/auth/         accounts: User, Identity (linked platform), Session, Repo interface
internal/oauth/        minimal OAuth2 (+ PKCE) client used for login/link; Twitch, Kick, YouTube, and StreamElements providers
internal/sqlite/       SQLite implementation of both Repo interfaces above
internal/ws/           WebSocket broadcast hub, scoped per timer ID
internal/server/       HTTP routes (REST + /ws/{timerId} + /auth/*)
internal/platform/     one package per streaming platform: Twitch/Kick webhook clients, YouTube's gRPC live chat client, StreamElements' poller
internal/youtubepb/    generated gRPC bindings for YouTube's liveChatMessages.streamList
frontend/              React + Vite + TypeScript: login, account, timer picker, dashboard, overlay
```

## Accounts and moderators

**Signing in.** Sign up or log in with a Twitch, Kick, or YouTube
(Google) account at `/login`. The first login via any of them creates
your account, named after that platform's username.

From `/account` you can link the other platforms to the same account —
linking reuses the same OAuth flow with `mode=link` instead of
`mode=login`, and fails with a clear error if that platform account is
already linked to someone else.

**Moderators.** An owner can grant other accounts the same control over a
timer as themselves — reset/resume/stop, adding events, reward rules,
watched channels:

| Endpoint | Access |
| --- | --- |
| `GET/POST /api/timers/{id}/moderators` | list is visible to the owner and existing moderators; adding is owner-only |
| `DELETE /api/timers/{id}/moderators/{userId}` | owner-only |

Adding a moderator takes a Twitch or Kick username (whichever they've
linked here, matched case-insensitively) — they need to have signed into
this app at least once first, same as any account. Moderators can't
manage this list themselves, so they can't add more moderators or remove
the owner's access. `GET /api/timers` includes every timer an account
moderates alongside the ones it owns, each tagged `owner: true/false`.

**Setting up OAuth apps.** Each platform needs its own registered OAuth
app before sign-in/linking works for it. Without credentials configured,
the corresponding login/link button is just disabled in the UI
(`GET /api/auth/providers` reports which ones are live) — the rest of the
app still runs.

- **Twitch:** register at https://dev.twitch.tv/console/apps. Set the
  OAuth Redirect URL to match `TWITCH_REDIRECT_URL` (see
  [Configuration](#configuration)) exactly. The app requests
  `channel:read:subscriptions`/`bits:read` (live sub/gift-sub/cheer
  EventSub), `user:read:chat`/`user:bot` (reading
  [chat commands](#twitch-chat-commands)), and
  `user:read:moderated_channels` (listing moderated channels) — see
  [Twitch](#twitch) below for exactly which of these unlock what.
- **Kick:** register at https://kick.com/settings/developer (see
  https://docs.kick.com), and set its webhook URL to
  `<PUBLIC_BASE_URL>/webhooks/kick` (Kick's webhook destination is
  configured on the app itself, unlike Twitch's per-subscription
  callback).
  > Kick's public API is comparatively new — every endpoint,
  > request/response shape, and the webhook signing scheme in
  > `internal/oauth/providers.go` and `internal/platform/kick` are
  > cross-checked against docs.kick.com and against another local
  > project that exercises the same API in production, but not run
  > against a live Kick app from this repo itself. Check them against
  > Kick's current docs before relying on this in production, and in
  > particular confirm `parseKickUser`'s response shape and the
  > `events:subscribe` scope name are still current if Kick's
  > app-approval error ever resurfaces.
- **YouTube:** create an OAuth client ID at
  https://console.cloud.google.com/apis/credentials (with the YouTube
  Data API v3 enabled on that Google Cloud project), and add
  `YOUTUBE_REDIRECT_URL` as an "Authorized redirect URI" on it. The app
  requests only `youtube.readonly` — this app never posts to or
  moderates a channel's live chat, only reads it. Signing in also
  doubles as authorizing that channel to be watched — see
  [YouTube](#youtube) below.
- **StreamElements:** register at
  https://streamelements.com/dashboard/apps and set its Redirect URI to
  match `STREAMELEMENTS_REDIRECT_URL` exactly. Unlike the platforms
  above, this isn't used to log into the app — it's what lets a timer
  owner connect their own StreamElements account for tip polling from
  that timer's dashboard (see [StreamElements](#streamelements) below).
  The app requests `channel:read`, `tips:read`, and `activities:read`.
  > Like Kick, StreamElements' OAuth2 app-registration flow and its
  > scope names here are sourced from
  > https://github.com/StreamElements/api-docs, not verified against a
  > live application — check that doc before relying on this in
  > production.

StreamElements' "Connect" link follows the same disabled-until-configured
rule as the login buttons above (`GET .../stream-elements-token`'s
`oauthConfigured` field reports whether it's set up), just per-timer
instead of a single app-wide login button.

## Timers

The server can track more than one subathon clock at once — e.g.
separate timers for separate events, or a rehearsal timer alongside the
real one. Each timer has a stable ID, which is also the token the
overlay page uses to know which timer to display: `/t/<timerId>/overlay`.

Each timer is owned by the account that created it. Anyone with a
timer's ID can *view* it (`GET /api/timers/{id}/state`, `/ws/{id}`, and
`/t/{id}/overlay`) — that's the point of the overlay token — but only the
owner (and its moderators) can list it in `/api/timers`, create timers,
or hit its control endpoints.

| Endpoint | Access |
| --- | --- |
| `GET/POST /api/timers` | list (owned + moderated) / create your timers — requires login |
| `GET /api/timers/{id}/state` | current snapshot — public |
| `GET /api/timers/{id}/events` | full contribution history — public |
| `POST /api/timers/{id}/control/{reset,resume,stop,lock,unlock,hide,unhide,end,unend}` | control it — owner or moderator (see [Ending a timer](#ending-a-timer) for `end`/`unend`) |
| `POST /api/timers/{id}/events` | record a contributor event — owner or moderator |
| `GET /ws/{id}` | live snapshot updates — public |

| Frontend route | What it is |
| --- | --- |
| `/` | lists and creates your timers |
| `/t/<id>` | that timer's dashboard |
| `/t/<id>/overlay` | the public OBS view |
| `/t/<id>/history` | full public contribution history (sortable, filterable to one contributor) |
| `/t/<id>/rewards` | that timer's reward- and money-rule settings (owner or moderator) |

## Reward rules, money rules, and live events

Each timer has its own **reward rules**
(`GET/PUT /api/timers/{id}/reward-rules`, owner or moderator) — seconds
added per contribution item, configured separately per platform. New
timers start with `subathon.DefaultRewardRules()` until explicitly
saved.

Not every item applies to every platform:

- Only Twitch distinguishes sub tiers — Kick has no tiers (every Kick sub
  prices as `tier1_sub`), and neither does YouTube — so
  `tier2_sub`/`tier3_sub` are Twitch-only.
- Gifted subs are tiered the same way on Twitch (`gifted_tier1_sub`/`2`/`3`,
  fed by `channel.subscription.gift`'s own `tier` field). Kick and
  YouTube gifted subs use the flat `gifted_sub` rate instead (Kick's
  `channel.subscription.gifts` webhook event doesn't carry a tier).
- StreamElements only ever fires a `donation_unit` event (see
  [StreamElements](#streamelements) below).

The Rewards page hides any item/platform cell that doesn't apply. Kick's
`kicks.gifted` webhook event feeds the bits/Kicks rate, priced per 100
Kicks — same as Twitch bits.

**Recording contributions manually.** For anything not covered by a live
webhook — a sub/gifted sub/bits count told to you directly, or a cash
app/Venmo/PayPal gift with no API of its own — the dashboard's **"Add
donation"** form (distinct from the raw "Add test event" tool beside it)
is how those get recorded. Give it a contributor username, a platform,
what was contributed (dollars, a sub — tiered on Twitch — gifted subs, or
bits/Kicks), and the count. It computes `secondsAdded`/`moneyAdded` from
that item/platform's reward/money rule before submitting a normal
`POST /api/timers/{id}/events`:

```json
{ "platform": "...", "type": "...", "username": "...", "secondsAdded": 0, "moneyAdded": 0, "amount": 0 }
```

— the same endpoint the raw test-event tool uses, just with the numbers
computed from configured rates instead of typed in directly. Unchecking
its "Add time to timer" box sends `secondsAdded: 0`, so the contribution
is still recorded (money, history, count) without extending the clock.

### Money rules, goals, and milestones

Alongside reward rules, each timer has its own **money rules**
(`GET/PUT /api/timers/{id}/money-rules`, same shape and permissions as
reward rules) — dollars counted toward the timer's money goal per
contribution, tracked independently of the time rules above.

Unlike reward rules, money rules default to $0 across the board
(`subathon.DefaultMoneyRules()`): money tracking is opt-in per
item/platform, so a timer with nothing configured raises $0 regardless of
activity rather than surfacing a total nobody asked for. A contribution
that's worth time, money, both, or neither is still recorded either way —
only a contribution worth nothing on both counts is dropped.

Each timer's **money goal** (`PUT /api/timers/{id}/money-goal`, body
`{"goal": 500}`, owner or moderator; `0` clears it) is the target its
money rules count toward — shown as a running-total-of-goal pill on both
the dashboard and the public overlay once set. `Snapshot.totalMoneyRaised`
is always present (0 if nothing's been raised); `Snapshot.moneyGoal` is
only present once one's been set.

`Snapshot.totalMoneyRaised` normally only moves via recorded events, but
it can also be set directly with `PUT /api/timers/{id}/money-raised`
(body `{"amount": 750}`, owner or moderator) — for reconciling against an
external donation tracker instead of reworking every money rule to
match. Unlike a manual event, this doesn't add a history entry; it's a
correction to the running total, not a contribution.

A timer can also have any number of **money milestones**
(`GET/PUT /api/timers/{id}/money-milestones`, body a JSON array of
`{"amount": 500, "label": "extra hour added", "hidden": false}`; GET is
public like `/state`, PUT is owner or moderator) — dollar-amount
checkpoints shown as a list on the dashboard, each marked reached once
the total raised crosses it. These are independent of (and don't have to
match) the overall money goal; "reached" isn't stored anywhere, it's
just each milestone's `amount` compared against the current
`totalMoneyRaised`.

A milestone's `hidden` flag makes it a "surprise goal": the dashboard
still always shows its real `label`, but the public goals overlay
(below) replaces the label with pulsing dots instead — the amount pill
and reached state stay visible either way, only the label text is
hidden.

### Overlays

The public overlay (`/t/{id}/overlay`) shows the timer and, once a money
goal is set, the money total as two pill-shaped elements joined into one
(no gap or individual rounding between them — see `.overlay-pill-group`).

A separate public overlay, `/t/{id}/goals-overlay` — its own OBS browser
source so it can be placed/sized independently — lists the money
milestones from above, each as its own pill containing the milestone's
label (or, if `hidden`, pulsing dots) with a smaller pill nested inside
it for the dollar amount (which switches to a checkmark once reached).

Every pill's background and text color is customizable
(`PUT /api/timers/{id}/overlay-colors`, owner or moderator; colors must
be 6-digit hex, e.g. what an HTML `<input type="color">` produces; an
empty field falls back to `subathon.DefaultOverlayColors`) via color
pickers on the dashboard:

```json
{
  "timerBg": "#111111", "timerText": "#ffffff",
  "moneyBg": "#111111", "moneyText": "#ffffff",
  "goalBg": "#111111", "goalText": "#ffffff",
  "goalAmountBg": "#ffffff", "goalAmountText": "#111111"
}
```

`Snapshot.overlayColors` is always fully populated, customized or not,
and covers both overlays' pills.

### Watched channels

Each timer also has its own Twitch channel, Kick channel, and/or YouTube
channel to watch — set from the timer's dashboard, or directly:

| Endpoint | Body | Notes |
| --- | --- | --- |
| `PUT /api/timers/{id}/twitch-channel` | `{"username": "..."}` | Empty clears it. Doesn't have to be the timer owner's own channel — pick whichever streamer's activity should feed this particular timer. |
| `PUT /api/timers/{id}/kick-channel` | `{"username": "..."}` | Same as Twitch. |
| `PUT /api/timers/{id}/youtube-channel` | `{"channelId": "..."}` | Must be a channel the *acting* account (owner or moderator, whichever calls this) has themselves linked via YouTube sign-in — see [YouTube](#youtube) below for why. |

A live event applies to every currently-*running* timer watching that
broadcaster, using each timer's own reward rules; it's silently dropped
if no reward rule is configured for that item/platform. (YouTube events
are the exception — they apply regardless of `Running`, same as a manual
event, since they arrive from a per-timer background stream rather than
a shared webhook.)

Activating live events differs meaningfully by platform:

### Twitch

Requires `TWITCH_WEBHOOK_SECRET` (see [Configuration](#configuration)).

Saving a timer's Twitch channel subscribes to that broadcaster's
EventSub (`channel.subscribe`, `channel.subscription.gift`,
`channel.cheer`) using an app-level token. This requires
`PUBLIC_BASE_URL` to be a real `https://` domain Twitch can reach — it
delivers notifications by POSTing to
`<PUBLIC_BASE_URL>/webhooks/twitch`. During local dev, that's proxied
through the Vite dev server, so `frontend/vite.config.ts`'s `proxy` map
needs a `/webhooks` entry too (same as `/api`/`/auth`/`/ws`), or Twitch's
and Kick's deliveries silently hit Vite's own 404 instead of the Go
backend.

**The channel's own broadcaster specifically — confirmed empirically,
not just a moderator — must have signed into this app with Twitch at
least once** (`/login` or linking from `/account`) to grant
`channel:read:subscriptions`/`bits:read`; Twitch enforces this when the
subscription is created. Setting the channel before that still saves it,
but subscription creation fails (logged server-side) until the
broadcaster authorizes — every Twitch OAuth completion retries it for
that identity's own channel.

> **Moderator delegation:** `twitch.Client.ModeratedChannels`
> (`user:read:moderated_channels` scope) and the retry for a signed-in
> moderator's moderated channels are still here — they don't hurt — but
> don't actually unlock anything for this webhook + app-token
> integration. Twitch rejects app-token subscription creation for a
> channel a moderator (not its broadcaster) authorized, with the same
> 403 as no authorization at all. Moderator delegation *is* real on
> Twitch, but only when the *moderator's own user token* creates the
> subscription over WebSocket transport — see the NOTE atop
> `internal/platform/twitch/twitch.go` for why that path wasn't built
> here. The Twitch channel picker still lists moderated channels
> (harmless to pick), but only ones the timer owner or a signed-in
> moderator actually *broadcasts* will ever go live.

### Kick

Requires `KICK_CLIENT_ID`.

Saving a timer's Kick channel subscribes to
`channel.subscription.new`, `channel.subscription.renewal`,
`channel.subscription.gifts`, and `kicks.gifted` using this app's own
token. Kick's events API accepts an explicit `broadcaster_user_id` from
an app-level token for *any* channel, so unlike Twitch **the watched
channel never has to sign into this app at all** — only
`KICK_CLIENT_ID`/`KICK_CLIENT_SECRET` need to be set.

(The one thing an app token can't do is act as a particular signed-in
user for endpoints like updating their channel info — not a concern
here, since this only ever reads public channel data and subscribes to
webhook events.)

### YouTube

Requires `YOUTUBE_CLIENT_ID`.

`youtube.readonly` (see `internal/oauth.NewYouTube`) is a sensitive
scope, so Google's OAuth consent screen setup requires a reachable
"Privacy Policy URL" before it'll let real users through. This app
serves one at `/privacy` (`frontend/src/pages/Privacy.tsx`, no login
required) — set that field to `<PUBLIC_BASE_URL>/privacy` (e.g.
`https://subathon.example.com/privacy`), and edit that page's
`OPERATOR_CONTACT` constant to your own contact details first, since
each deployment of this app is its own data controller.

Unlike Twitch/Kick, YouTube has no webhook API for Super
Chats/memberships, and unlike Twitch's app-token EventSub, there's no
server-level credential that can read an *arbitrary* channel's live chat
— only that channel's own linked owner can (YouTube Data API's
`liveBroadcasts.list?mine=true`, which
`internal/platform/youtube.Client.FindActiveLiveChat` uses to find the
channel's current broadcast, requires it).

So **the watched channel's owner (or, same as Twitch/Kick, whichever
account is setting it) must have signed into this app with that YouTube
channel** via `/login` or linking from `/account` before it can be
picked at all — `GET /api/youtube/channels` only ever offers
already-linked channels, never free text.

Once picked, `internal/platform/youtube.Poller` runs one goroutine per
watched timer that alternates between checking whether the channel is
live and, once it is, holding open YouTube's
`liveChatMessages.streamList` gRPC connection (push-based, not
REST-polled — see the package doc comment) until the broadcast ends. It
translates:

| YouTube event | Reward item |
| --- | --- |
| Super Chat / Super Sticker | `donation_unit` |
| New member | flat `tier1_sub` (YouTube has no Twitch-style tiers — see the Rewards page's ITEMS table) |
| Membership gifting | `gifted_sub` |

The OAuth2 access token Google issues is short-lived (about an hour); the
poller refreshes it well before it expires, the same way the
StreamElements poller does.

> This gRPC client (`internal/youtubepb`, generated from
> `stream_list.proto`) and its reconnect handling are ported from this
> same author's `multi-stream-moderation` project, which exercises this
> service against a live YouTube account — unlike this project's Kick
> integration, this isn't a from-the-docs guess.

### StreamElements

Requires `STREAMELEMENTS_CLIENT_ID`.

Each timer owner connects their own account via OAuth2 from the
dashboard's StreamElements panel:

1. A "Connect StreamElements" link
   (`GET /api/timers/{id}/stream-elements/oauth/start`, owner or
   moderator; a plain browser navigation, not a fetch, since it ends in
   a redirect) sends them to StreamElements' own authorize page.
2. `GET /auth/streamelements/callback` (a fixed public URL, registered
   as this app's redirect URI on StreamElements' developer portal — see
   `internal/oauth.NewStreamElements`) exchanges the resulting code,
   resolves which channel it belongs to (`GET /channels/me`), saves it
   against that timer, and redirects back to its dashboard.

This replaced an earlier version of this integration where the owner
pasted in their account's long-lived JWT token directly.
`DELETE /api/timers/{id}/stream-elements-token` disconnects; either way,
`GET .../stream-elements-token` → `{"connected", "displayName",
"oauthConfigured"}` never echoes any token back.

Once connected, the server polls `GET /activities/{channel}?types=tip`
every ~20s (`internal/platform/streamelements`) and feeds new tips
through the `donation_unit` reward/money rules for the `streamelements`
platform, same as any other live event. (This polls rather than using
StreamElements' real-time Socket.IO gateway — see the package doc
comment for why.)

The access token StreamElements issues is short-lived (about a week);
the poller refreshes it in the background well before it expires
(`internal/oauth.Provider.Refresh`) using the connection's refresh
token, persisting the new pair — no action needed from the owner unless
a refresh itself fails (logged server-side), in which case reconnecting
fixes it.

### Throne

No setup needed on this server at all — unlike every integration above,
Throne needs no client ID/secret and no per-timer OAuth connection.
Instead, each timer gets its own webhook URL
(`POST /webhooks/throne/{id}`, shown with a copy button under "Throne
integration" at the bottom of its dashboard page) that the creator pastes
into Throne's own dashboard, under Profile → Integrations → Webhook (see
[Throne's webhook help
article](https://help.throne.com/en/articles/15935990-how-do-i-set-up-webhook-integration)).
From then on Throne POSTs every gift/contribution to that URL directly.

Requests are authenticated by an Ed25519 signature against Throne's own
published public key (`internal/platform/throne`) — the same scheme
Discord uses for interaction webhooks — rather than a shared secret this
server issues, so there's nothing to configure by default.
`THRONE_WEBHOOK_PUBLIC_KEY` overrides the built-in key, only useful if
Throne rotates it before this app is updated to match.

`gift_purchased` and `contribution_purchased` notifications feed the
`donation_unit` reward/money rules for the `throne` platform, same as any
other donation. `gift_crowdfunded` (a crowdfunded gift's goal being met by
many people's contributions, each already counted individually as they
came in) is deliberately not counted again.

---

Without `TWITCH_WEBHOOK_SECRET`/`KICK_CLIENT_ID`/`YOUTUBE_CLIENT_ID`/
`STREAMELEMENTS_CLIENT_ID` set, reward rules still work for
manually-added events (`POST /api/timers/{id}/events`), just not real
platform activity. Throne is the exception — it works with no
configuration at all (see [Throne](#throne) above).

## Twitch chat commands

A timer's own watched Twitch channel's moderators (or its broadcaster)
can run these in that channel's chat, with the same effect as the
matching button on the dashboard. The `!timer` prefix is required — a
bare `!pause` is ignored — so this doesn't collide with any other bot's
own single-word commands in the same chat.

| Command | Effect |
| --- | --- |
| `!timer pause` / `!timer play` | `control/stop` / `control/resume` — starts/stops the countdown itself |
| `!timer lock` / `!timer unlock` | Toggles `Snapshot.locked`: contributions still get recorded (and still count toward `totalMoneyRaised`) but stop extending the clock — freezing the countdown against new contributions without losing track of what came in during the freeze. Independent of pause/play; combine both if wanted. |
| `!timer hide` / `!timer show` | Toggles `Snapshot.hidden`: the public overlay renders nothing at all while hidden (not just a blank clock) — the dashboard always shows the clock to the owner/moderators regardless. |

This reads chat via a `channel.chat.message` EventSub subscription,
created (like Twitch's other subscriptions above) via an app-level token
over webhook transport. Unlike those, Twitch accepts this one as long as
the *reading* identity (here, always the timer owner's own linked Twitch
identity — not the watched channel's broadcaster, if they're different)
is that channel's broadcaster or one of its moderators, and has granted
`user:read:chat`/`user:bot` to this app. So this activates as soon as the
timer's Twitch channel is set, *if* the owner is that channel's
broadcaster or already one of its moderators — no separate consent from
the watched channel needed, unlike subs/gifts/cheers above.

Only the actual chat message's sender needs moderator/broadcaster status
in Twitch's own badge data for their command to be honored — the owner's
identity is just how the app is allowed to read the channel's chat at
all, not who's allowed to run commands in it.

## Ending a timer

`POST /api/timers/{id}/control/end` (owner or moderator; `.../unend`
reverses it) is a stronger, deliberately dashboard-only shutdown —
there's no `!timer end` chat command, unlike every other toggle above,
so it can't be triggered (or undone) by anyone with just chat access.

Ending a timer:

- **Stops it from receiving any further live Twitch/Kick/StreamElements
  events.** `Manager.RunningByTwitchChannel`/`RunningByKickChannel`/
  `TimersByTwitchChannel` all exclude an ended timer, so it simply drops
  out of the set a live notification gets routed to — same as if
  nothing were watching that channel on its behalf. Other timers still
  watching the same channel are unaffected; these are per-timer routing
  exclusions, not an actual unsubscribe from Twitch/Kick's webhooks
  (which are shared per-channel infrastructure, not per-timer).
- **Stops responding to *every* `!timer ...` chat command** — a side
  effect of `TimersByTwitchChannel` backing both live-event routing and
  chat command routing.
- **Stops (and, on `unend`, resumes) its StreamElements poller
  goroutine** — the one listener that's genuinely per-timer rather than
  per-channel, so it needs an explicit `Poller.Stop`/`Watch` rather than
  falling out of a Manager query.

Ending does *not* itself pause the clock, change `locked`/`hidden`, or
block manual events or any other dashboard control (reset/resume/stop,
lock/hide, "Add donation", reward rules, etc.) — those stay
independently usable even once ended, e.g. to make a final correction to
the totals. `Snapshot.ended` reflects the current state.

## Storage

Timer state, contributor events, accounts, linked platform identities,
and sessions are all persisted to a SQLite database (pure-Go driver, no
cgo, so cross-compiling and Docker builds stay simple). State is written
on every control action and event, not on every tick, so a restart or
crash never loses more than the last action.

Set `DB_PATH` to point it at a mounted volume for Docker/Kubernetes:

```
docker run -v subathon-data:/data -e DB_PATH=/data/subathon.db ...
```

The parent directory is created automatically if it doesn't exist, so a
fresh empty volume works out of the box. Schema changes are applied
additively on startup, so upgrading in place is safe.

## Configuration

The server reads these environment variables (see `internal/config`).
Only `PUBLIC_BASE_URL` is likely to need changing for a typical
deployment — everything else has a sensible default or derives from it.

**Core**

| Var | Default | Meaning |
| --- | --- | --- |
| `ADDR` | `:8090` | HTTP/WS listen address |
| `PUBLIC_BASE_URL` | `http://localhost:5173` | The single origin the app is served under. `ALLOWED_ORIGIN`, `COOKIE_SECURE`, and every provider's `*_REDIRECT_URL` default from this — set it to your `https://` domain and everything else lines up |
| `ALLOWED_ORIGIN` | *(`PUBLIC_BASE_URL`)* | CORS origin allowed to hit the API, and where OAuth flows redirect back to |
| `INITIAL_DURATION` | `1h` | Fallback clock length if not set on reset |
| `DB_PATH` | `data/subathon.db` | SQLite database file path (create the dir via a volume) |
| `UI_DIST_DIR` | *(unset — static serving disabled)* | Path to the built frontend (`frontend/dist`); serving is skipped locally since Vite's dev server handles the UI instead. Baked into the Docker image — see Dockerfile |
| `COOKIE_SECURE` | *(`true` iff `PUBLIC_BASE_URL` is `https://`)* | Mark the session cookie HTTPS-only; set `true` once served over TLS |

**Twitch**

| Var | Default | Meaning |
| --- | --- | --- |
| `TWITCH_CLIENT_ID` | *(unset — Twitch login disabled)* | Twitch app client ID |
| `TWITCH_CLIENT_SECRET` | *(unset)* | Twitch app client secret |
| `TWITCH_REDIRECT_URL` | *(`PUBLIC_BASE_URL`)*`/auth/twitch/callback` | Must exactly match the app's registered redirect URL |
| `TWITCH_WEBHOOK_SECRET` | *(unset — live Twitch events disabled)* | Signs/verifies Twitch EventSub webhook calls; 10-100 chars. Requires `PUBLIC_BASE_URL` to be a real `https://` domain — see [Twitch](#twitch) above |

**Kick**

| Var | Default | Meaning |
| --- | --- | --- |
| `KICK_CLIENT_ID` | *(unset — Kick login disabled)* | Kick app client ID |
| `KICK_CLIENT_SECRET` | *(unset)* | Kick app client secret |
| `KICK_REDIRECT_URL` | *(`PUBLIC_BASE_URL`)*`/auth/kick/callback` | Must exactly match the app's registered redirect URL |

**YouTube**

| Var | Default | Meaning |
| --- | --- | --- |
| `YOUTUBE_CLIENT_ID` | *(unset — YouTube login/watching disabled)* | Google Cloud OAuth client ID |
| `YOUTUBE_CLIENT_SECRET` | *(unset)* | Google Cloud OAuth client secret |
| `YOUTUBE_REDIRECT_URL` | *(`PUBLIC_BASE_URL`)*`/auth/youtube/callback` | Must exactly match the app's registered redirect URL |

**StreamElements**

| Var | Default | Meaning |
| --- | --- | --- |
| `STREAMELEMENTS_CLIENT_ID` | *(unset — connecting StreamElements disabled)* | StreamElements app client ID |
| `STREAMELEMENTS_CLIENT_SECRET` | *(unset)* | StreamElements app client secret |
| `STREAMELEMENTS_REDIRECT_URL` | *(`PUBLIC_BASE_URL`)*`/auth/streamelements/callback` | Must exactly match the app's registered redirect URL — unlike Twitch/Kick this is per-*timer*, not per-login, see [StreamElements](#streamelements) above |

**Throne**

| Var | Default | Meaning |
| --- | --- | --- |
| `THRONE_WEBHOOK_PUBLIC_KEY` | *(unset — uses the built-in key)* | PEM-encoded Ed25519 public key overriding Throne's built-in published webhook-signing key; see [Throne](#throne) above |

---

Locally, the default `PUBLIC_BASE_URL` points at the Vite dev server's
port, not the API's — `frontend/vite.config.ts` proxies `/auth` through
to the API, so the whole app stays same-origin from the browser's
perspective and the session cookie "just works".

For a real deployment, put the frontend and API behind one `https://`
domain and set `PUBLIC_BASE_URL` to it — `ALLOWED_ORIGIN`,
`COOKIE_SECURE`, and every provider's redirect URL all follow
automatically. Register these with each OAuth app:

- `<PUBLIC_BASE_URL>/auth/twitch/callback`
- `<PUBLIC_BASE_URL>/auth/kick/callback`
- `<PUBLIC_BASE_URL>/auth/youtube/callback`
- `<PUBLIC_BASE_URL>/auth/streamelements/callback`

Set the `*_REDIRECT_URL` vars individually only if a provider's callback
needs to live on a different origin than the rest of the app.
