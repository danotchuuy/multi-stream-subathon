# multi-stream-subathon

Service to handle subathon-like tracking from multiple streaming platforms
(Kick, YouTube, Twitch): one or more running clocks that get extended by
subs, donations, etc., a dashboard to control them, and an OBS
browser-source overlay to display one.

## Status

Initial scaffold. The Go backend supports multiple independent timers, each
with a durable SQLite-backed clock and contributor event history, owned by
an account created via Twitch or Kick login. Twitch subs, gift subs, and
cheers, and Kick subs and gift subs, are wired up live (see Reward rules
below); the YouTube listener (`internal/platform/youtube`) is still a stub
that doesn't connect to anything yet.

## Structure

```
cmd/server/            entrypoint: wires config, db, manager, hub, HTTP server
internal/config/       env-based configuration
internal/subathon/     core domain: Timer (clock + events), Manager (multiple timers), Repo interface
internal/auth/         accounts: User, Identity (linked platform), Session, Repo interface
internal/oauth/        minimal OAuth2 (+ PKCE) client used for login/link; Twitch + Kick providers
internal/sqlite/       SQLite implementation of both Repo interfaces above
internal/ws/           WebSocket broadcast hub, scoped per timer ID
internal/server/       HTTP routes (REST + /ws/{timerId} + /auth/*)
internal/platform/     one package per streaming platform: Twitch/Kick webhook clients, YouTube still a stub
frontend/              React + Vite + TypeScript: login, account, timer picker, dashboard, overlay
```

## Accounts

Sign up or log in with a Twitch or Kick account (`/login`); the first login
via either creates your account, named after that platform's username.
From `/account` you can link the other platform (Kick if you signed up
with Twitch, or vice versa) to the same account — linking reuses the same
OAuth flow with `mode=link` instead of `mode=login`, and fails with a clear
error if that platform account is already linked to someone else. YouTube
*linking* isn't wired up yet (`internal/oauth` only has Twitch and Kick
providers today).

Each timer is owned by the account that created it. Anyone with a timer's
ID can view it (`GET /api/timers/{id}/state`, `/ws/{id}`, and the
`/t/{id}/overlay` page) — that's the point of the overlay token — but only
the owner (and, since they get the same control access, its moderators —
see below) can list it in `/api/timers`, create timers, or hit its control
endpoints (`reset`/`resume`/`stop`/`events`).

**Moderators:** an owner can grant other accounts the same control over a
timer as themselves — reset/resume/stop, adding events, reward rules,
watched channels — via `GET/POST /api/timers/{id}/moderators` (list is
visible to the owner and existing moderators; adding is owner-only) and
`DELETE /api/timers/{id}/moderators/{userId}` (owner-only). Adding one
takes a Twitch or Kick username (whichever they've linked here, matched
case-insensitively) — they need to have signed into this app at least
once first, same as any account. Moderators can't manage this list
themselves, so they can't add more moderators or remove the owner's
access; `GET /api/timers` includes every timer an account moderates
alongside the ones it owns, each tagged `owner: true/false`.

**Setting up OAuth apps:**

- Twitch: register at https://dev.twitch.tv/console/apps. Set the OAuth
  Redirect URL to match `TWITCH_REDIRECT_URL` below exactly. The app
  requests `channel:read:subscriptions` and `bits:read` (see
  `internal/oauth/providers.go`) so live sub/gift-sub/cheer EventSub
  subscriptions can be created for whoever authorizes it, plus
  `user:read:chat`/`user:bot` for reading chat commands (see Twitch chat
  commands below) and `user:read:moderated_channels` for listing which
  channels someone moderates (see below) — see the Reward rules & live
  Twitch/Kick events section for exactly which of these actually unlock
  what.
- Kick: register at https://kick.com/settings/developer (see
  https://docs.kick.com), and set its webhook URL to
  `<PUBLIC_BASE_URL>/webhooks/kick` (Kick's webhook destination is
  configured on the app itself, unlike Twitch's per-subscription
  callback). **Kick's public API is comparatively new — every endpoint,
  request/response shape, and the webhook signing scheme in
  `internal/oauth/providers.go` and `internal/platform/kick` are cross-
  checked against docs.kick.com and against another local project that
  exercises the same API in production, but not run against a live Kick
  app from this repo itself.** Check them against Kick's current docs
  before relying on this in production, and in particular confirm
  `parseKickUser`'s response shape and the `events:subscribe` scope name
  are still current if Kick's app-approval error ever resurfaces.

Without credentials configured, the corresponding login/link button is
just disabled in the UI (`GET /api/auth/providers` reports which ones are
live) — the rest of the app still runs.

## Multiple timers

The server can track more than one subathon clock at once (e.g. separate
timers for separate events, or a rehearsal timer alongside the real one).
Each timer has a stable ID, which is also the token the overlay page uses
to know which timer to display: `/t/<timerId>/overlay`.

- `GET/POST /api/timers` — list (owned + moderated) / create your timers (requires login)
- `GET /api/timers/{id}/state` — one timer's current snapshot (public)
- `GET /api/timers/{id}/events` — its full contribution history (public)
- `POST /api/timers/{id}/control/{reset,resume,stop,lock,unlock,hide,unhide}` — control it (owner or moderator)
- `POST /api/timers/{id}/events` — record a contributor event (owner or moderator)
- `GET /ws/{id}` — live snapshot updates for that timer (public)

The frontend's `/` route lists and creates your timers; `/t/<id>` is that
timer's dashboard; `/t/<id>/overlay` is the public OBS view;
`/t/<id>/history` is its full public contribution history (sortable,
filterable to one contributor); `/t/<id>/rewards` is that timer's reward-
and money-rule settings (owner or moderator).

## Reward rules, money rules & live Twitch/Kick events

Each timer has its own reward rules (`GET/PUT /api/timers/{id}/reward-rules`,
owner or moderator) — seconds added per tier-1/2/3 sub, gifted sub,
per-100 bits/Kicks, and per-$1 donation, configured separately per
platform. New timers start with `subathon.DefaultRewardRules()` until
explicitly saved. Kick's `kicks.gifted` webhook event feeds the
bits/Kicks rate (priced per 100 Kicks, same as Twitch bits).

For anything not covered by StreamElements (see below) — e.g. a cash
app/Venmo/PayPal gift with no API of its own — the dashboard's
**"Add donation"** form (distinct from the raw "Add test event" tool
beside it) is how those get recorded: give it a donor username, a
platform, and the dollar amount, and it computes `secondsAdded`/
`moneyAdded` from that platform's `donation_unit` reward/money rule
itself before submitting a normal `POST /api/timers/{id}/events`
(`{"platform", "type": "donation", "username", "secondsAdded",
"moneyAdded", "amount"}`) — the same endpoint the raw test-event tool
uses, just with the numbers computed from configured rates instead of
typed in directly.

Alongside reward rules, each timer also has its own **money rules**
(`GET/PUT /api/timers/{id}/money-rules`, same shape and permissions as
reward rules) — dollars counted toward the timer's money goal per
contribution, tracked independently of the time rules above. Unlike
reward rules, money rules default to $0 across the board
(`subathon.DefaultMoneyRules()`): money tracking is opt-in per item/
platform, so a timer with nothing configured raises $0 regardless of
activity rather than surfacing a total nobody asked for. A contribution
that's worth time, money, both, or neither is still recorded either way —
only a contribution worth nothing on both counts is dropped.

Each timer's **money goal** (`PUT /api/timers/{id}/money-goal`, body
`{"goal": 500}`, owner or moderator; `0` clears it) is the target its
money rules count toward — shown as a running-total-of-goal pill on both
the dashboard and the public overlay once set. `Snapshot.totalMoneyRaised`
is always present (0 if nothing's been raised); `Snapshot.moneyGoal` is
only present once one's been set.

`Snapshot.totalMoneyRaised` normally only moves via recorded events (see
above), but it can also be set directly with
`PUT /api/timers/{id}/money-raised` (body `{"amount": 750}`, owner or
moderator) — for reconciling against an external donation tracker instead
of reworking every money rule to match. Unlike a manual event, this
doesn't add a history entry; it's a correction to the running total, not
a contribution.

A timer can also have any number of **money milestones**
(`GET/PUT /api/timers/{id}/money-milestones`, body a JSON array of
`{"amount": 500, "label": "extra hour added", "hidden": false}`; GET is
public like `/state`, PUT is owner or moderator) — dollar-amount
checkpoints shown as a list on the dashboard, each marked reached once
the total raised crosses it. These are independent of (and don't have to
match) the overall money goal above; "reached" isn't stored anywhere,
it's just each milestone's `amount` compared against the current
`totalMoneyRaised`.

A milestone's `hidden` flag makes it a "surprise goal": the dashboard
still always shows its real `label`, but the public goals overlay (below)
replaces the label with pulsing dots instead — the amount pill and
reached state stay visible either way, only the label text is hidden.

The public overlay (`/t/{id}/overlay`) shows the timer and, once a money
goal is set, the money total as two pill-shaped elements joined into one
(no gap or individual rounding between them — see `.overlay-pill-group`).
A separate public overlay, `/t/{id}/goals-overlay` — its own OBS browser
source so it can be placed/sized independently — lists the money
milestones from above, each as its own pill containing the milestone's
label (or, if `hidden`, pulsing dots) with a smaller pill nested inside
it for the dollar amount (which switches to a checkmark once reached).

Every pill's background and text color is customizable
(`PUT /api/timers/{id}/overlay-colors`, owner or moderator, body
`{"timerBg": "#111111", "timerText": "#ffffff", "moneyBg": "#111111",
"moneyText": "#ffffff", "goalBg": "#111111", "goalText": "#ffffff",
"goalAmountBg": "#ffffff", "goalAmountText": "#111111"}` — colors must be
6-digit hex, e.g. what an HTML `<input type="color">` produces; an empty
field falls back to `subathon.DefaultOverlayColors`) via color pickers on
the dashboard. `Snapshot.overlayColors` is always fully populated,
customized or not, and covers both overlays' pills.

Each timer also has its own Twitch channel and/or Kick channel to watch
(`PUT /api/timers/{id}/twitch-channel` / `.../kick-channel`, owner or
moderator, body `{"username": "..."}`; empty clears it — both are also settable from
the timer's dashboard). Neither has to be the timer owner's own channel —
pick whichever streamer's activity should feed this particular timer,
useful for a multi-streamer subathon where the timer owner isn't the one
being watched. A live event applies to every currently-*running* timer
watching that broadcaster, using each timer's own reward rules; it's
silently dropped if no reward rule is configured for that item/platform.

Activating live events differs meaningfully by platform:

- **Twitch** (`TWITCH_WEBHOOK_SECRET` set, see Configuration below):
  saving a timer's Twitch channel subscribes to that broadcaster's
  EventSub (`channel.subscribe`, `channel.subscription.gift`,
  `channel.cheer`) using an app-level token, which requires
  `PUBLIC_BASE_URL` to be a real `https://` domain Twitch can reach (it
  delivers notifications by POSTing to `<PUBLIC_BASE_URL>/webhooks/twitch`
  — during local dev, that's proxied through the Vite dev server, so
  `frontend/vite.config.ts`'s `proxy` map needs a `/webhooks` entry too,
  same as `/api`/`/auth`/`/ws`, or Twitch's and Kick's deliveries silently
  hit Vite's own 404 instead of the Go backend). **The channel's own
  broadcaster specifically — confirmed empirically, not just a
  moderator — must have signed into this app with Twitch at least once**
  (`/login` or linking from `/account`) to grant
  `channel:read:subscriptions`/`bits:read`; Twitch enforces this when the
  subscription is created. Setting the channel before that still saves
  it, but subscription creation fails (logged server-side, and every
  Twitch OAuth completion retries it for that identity's own channel)
  until the broadcaster themselves authorizes.
  `twitch.Client.ModeratedChannels` (`user:read:moderated_channels`
  scope) and the retry for a signed-in moderator's moderated channels
  are still here — they don't hurt — but don't actually unlock
  anything for this webhook + app-token integration: Twitch rejects
  app-token subscription creation for a channel a moderator (not its
  broadcaster) authorized, with the same 403 as no authorization at
  all. Moderator delegation is real on Twitch, but only when the
  *moderator's own user token* creates the subscription over WebSocket
  transport — see the NOTE atop `internal/platform/twitch/twitch.go` for
  why that path wasn't built here. The Twitch channel picker still lists
  moderated channels (harmless to pick), but only ones the timer owner
  or a signed-in moderator actually *broadcasts* will ever go live.
- **Kick** (`KICK_CLIENT_ID` set): saving a timer's Kick channel
  subscribes to `channel.subscription.new`, `channel.subscription.renewal`,
  `channel.subscription.gifts`, and `kicks.gifted` using this app's own
  token — Kick's events API accepts an explicit `broadcaster_user_id` from
  an app-level token for *any* channel, so unlike Twitch **the watched
  channel never has to sign into this app at all**; only `KICK_CLIENT_ID`/
  `KICK_CLIENT_SECRET` need to be set. (The one thing an app token can't
  do is act as a particular signed-in user for endpoints like updating
  their channel info — not a concern here, since this only ever reads
  public channel data and subscribes to webhook events.)

- **StreamElements** — no server-level credential at all: each timer
  owner connects their own account's JWT token (from
  streamelements.com/dashboard/account/channels) on the dashboard
  (`PUT /api/timers/{id}/stream-elements-token`, owner or moderator; body
  `{"token": "..."}`, empty disconnects). The token is validated
  immediately by resolving it to a channel (`GET /channels/me`) before
  being saved — an invalid/expired one is rejected there rather than
  silently never producing anything — and the response
  (`GET/PUT .../stream-elements-token` → `{"connected", "displayName"}`)
  never echoes the token itself back. Once connected, the server polls
  `GET /activities/{channel}?types=tip` every ~20s
  (`internal/platform/streamelements`) and feeds new tips through the
  `donation_unit` reward/money rules for the `streamelements` platform,
  same as any other live event. This polls rather than using
  StreamElements' real-time Socket.IO gateway — see the package doc
  comment for why. StreamElements tokens expire after roughly 180 days;
  when that happens polling for that timer will start failing (logged
  server-side) until the owner pastes a fresh one.

Without `TWITCH_WEBHOOK_SECRET`/`KICK_CLIENT_ID` set, reward rules still
work for manually-added events (`POST /api/timers/{id}/events`), just not
real platform activity. StreamElements has no such server-level gate — it
works out of the box, per-timer, as soon as an owner connects a token.

## Twitch chat commands

A timer's own watched Twitch channel's moderators (or its broadcaster) can
run `!timer pause`, `!timer unpause`, `!timer lock`, `!timer unlock`,
`!timer hide`, or `!timer unhide` in that channel's chat, with the same
effect as the matching button on the dashboard
(`POST /api/timers/{id}/control/{lock,unlock,hide,unhide}`, alongside the
existing `reset`/`resume`/`stop`). The `!timer` prefix is required — a
bare `!pause` is ignored — so this doesn't collide with any other bot's
own single-word commands in the same chat:

- `!timer pause`/`!timer unpause` are `control/stop`/`control/resume` —
  the countdown itself starts/stops.
- `!timer lock`/`!timer unlock` toggle `Snapshot.locked`: while locked,
  contributions still get recorded (and still count toward
  `totalMoneyRaised`) but stop extending the clock — freezing the
  countdown against new contributions without losing track of what came
  in during the freeze. It's independent of pause/unpause; combine both
  if wanted.
- `!timer hide`/`!timer unhide` toggle `Snapshot.hidden`: the public
  overlay renders nothing at all while hidden (not just a blank clock) —
  the dashboard always shows the clock to the owner/moderators
  regardless.

This reads chat via a `channel.chat.message` EventSub subscription,
created (like Twitch's other subscriptions above) via an app-level token
over webhook transport — but unlike those, Twitch accepts this one as
long as the *reading* identity (here, always the timer owner's own linked
Twitch identity — not the watched channel's broadcaster, if they're
different) is that channel's broadcaster or one of its moderators, and
has granted `user:read:chat`/`user:bot` to this app. So this activates as
soon as the timer's Twitch channel is set, *if* the owner is that
channel's broadcaster or already one of its moderators — no separate
consent from the watched channel needed, unlike subs/gifts/cheers above.
Only the actual chat message's sender needs moderator/broadcaster status
in Twitch's own badge data for their command to be honored — the owner's
identity is just how the app is allowed to read the channel's chat at
all, not who's allowed to run commands in it.

## Running locally

```
make run
```

starts the Go API and the Vite dev server together (Ctrl+C stops both).
`make server` / `make frontend` run them separately if you want them in
different terminals.

Both bind to all network interfaces, not just localhost, so they're also
reachable from other devices on your network (e.g. a second PC running OBS,
or a phone) — Vite prints the LAN URL it's listening on when it starts.
This is meant for local/trusted-network testing; the overlay/state/ws
endpoints are intentionally public (see Accounts above), so don't expose
these ports to the open internet without more hardening.

Open `http://localhost:5173/` (or `http://<your-lan-ip>:5173/`) — you'll
be sent to `/login` until you sign up with Twitch or Kick.

## Storage

Timer state, contributor events, accounts, linked platform identities, and
sessions are all persisted to a SQLite database (pure-Go driver, no cgo,
so cross-compiling and Docker builds stay simple). State is written on
every control action and event, not on every tick, so a restart or crash
never loses more than the last action.

Set `DB_PATH` to point it at a mounted volume for Docker/Kubernetes, e.g.:

```
docker run -v subathon-data:/data -e DB_PATH=/data/subathon.db ...
```

The parent directory is created automatically if it doesn't exist, so a
fresh empty volume works out of the box. Schema changes (e.g. this
feature's new `users`/`identities`/`sessions` tables and `timers.user_id`
column) are applied additively on startup, so upgrading in place is safe.

## Configuration

The server reads these environment variables (see `internal/config`):

| Var                     | Default                                     | Meaning                                                   |
| ------------------------ | -------------------------------------------- | ----------------------------------------------------------- |
| `ADDR`                   | `:8090`                                      | HTTP/WS listen address                                      |
| `PUBLIC_BASE_URL`        | `http://localhost:5173`                      | The single origin the app is served under. `ALLOWED_ORIGIN`, `COOKIE_SECURE`, and both providers' `*_REDIRECT_URL` default from this — set it to your `https://` domain and everything else lines up |
| `ALLOWED_ORIGIN`         | *(`PUBLIC_BASE_URL`)*                        | CORS origin allowed to hit the API, and where OAuth flows redirect back to |
| `INITIAL_DURATION`       | `1h`                                          | Fallback clock length if not set on reset                   |
| `DB_PATH`                | `data/subathon.db`                           | SQLite database file path (create the dir via a volume)     |
| `COOKIE_SECURE`          | *(`true` iff `PUBLIC_BASE_URL` is `https://`)* | Mark the session cookie HTTPS-only; set `true` once served over TLS |
| `TWITCH_CLIENT_ID`       | *(unset — Twitch login disabled)*             | Twitch app client ID                                        |
| `TWITCH_CLIENT_SECRET`   | *(unset)*                                     | Twitch app client secret                                     |
| `TWITCH_REDIRECT_URL`    | *(`PUBLIC_BASE_URL`)*`/auth/twitch/callback`  | Must exactly match the app's registered redirect URL         |
| `TWITCH_WEBHOOK_SECRET`  | *(unset — live Twitch events disabled)*       | Signs/verifies Twitch EventSub webhook calls; 10-100 chars. Requires `PUBLIC_BASE_URL` to be a real `https://` domain — see Reward rules & live Twitch events above |
| `KICK_CLIENT_ID`         | *(unset — Kick login disabled)*               | Kick app client ID                                           |
| `KICK_CLIENT_SECRET`     | *(unset)*                                     | Kick app client secret                                       |
| `KICK_REDIRECT_URL`      | *(`PUBLIC_BASE_URL`)*`/auth/kick/callback`    | Must exactly match the app's registered redirect URL         |

Locally, the default `PUBLIC_BASE_URL` points at the Vite dev server's
port, not the API's — `frontend/vite.config.ts` proxies `/auth` through to
the API, so the whole app stays same-origin from the browser's perspective
and the session cookie "just works". For a real deployment, put the
frontend and API behind one `https://` domain and set `PUBLIC_BASE_URL` to
it — `ALLOWED_ORIGIN`, `COOKIE_SECURE`, and both providers' redirect URLs
all follow automatically; register `<PUBLIC_BASE_URL>/auth/twitch/callback`
and `<PUBLIC_BASE_URL>/auth/kick/callback` with each OAuth app. Set the
`*_REDIRECT_URL` vars individually only if a provider's callback needs to
live on a different origin than the rest of the app.
