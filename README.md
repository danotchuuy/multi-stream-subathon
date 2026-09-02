# multi-stream-subathon

Service to handle subathon-like tracking from multiple streaming platforms
(Kick, YouTube, Twitch): one or more running clocks that get extended by
subs, donations, etc., a dashboard to control them, and an OBS
browser-source overlay to display one.

## Status

Initial scaffold. The Go backend supports multiple independent timers, each
with a durable SQLite-backed clock and contributor event history, owned by
an account created via Twitch or Kick login; the platform *event*
listeners (`internal/platform/{kick,youtube,twitch}`, for pulling subs/
donations in) are stubs that don't connect to anything yet.

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
internal/platform/     one package per streaming platform (event listener stubs, not auth)
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
the owner can list it in `/api/timers`, create timers, or hit its control
endpoints (`reset`/`resume`/`stop`/`events`).

**Setting up OAuth apps:**

- Twitch: register at https://dev.twitch.tv/console/apps. Set the OAuth
  Redirect URL to match `TWITCH_REDIRECT_URL` below exactly.
- Kick: register at https://kick.com/settings/developer (see
  https://docs.kick.com). **Kick's public OAuth API is comparatively new —
  `internal/oauth/providers.go`'s endpoints and response parsing for Kick
  are this project's best-known values, not verified against a live app.**
  Check them against Kick's current docs and adjust `parseKickUser` if the
  user-info response shape differs before relying on this in production.

Without credentials configured, the corresponding login/link button is
just disabled in the UI (`GET /api/auth/providers` reports which ones are
live) — the rest of the app still runs.

## Multiple timers

The server can track more than one subathon clock at once (e.g. separate
timers for separate events, or a rehearsal timer alongside the real one).
Each timer has a stable ID, which is also the token the overlay page uses
to know which timer to display: `/t/<timerId>/overlay`.

- `GET/POST /api/timers` — list / create your timers (requires login)
- `GET /api/timers/{id}/state` — one timer's current snapshot (public)
- `POST /api/timers/{id}/control/{reset,resume,stop}` — control it (owner only)
- `POST /api/timers/{id}/events` — record a contributor event (owner only)
- `GET /ws/{id}` — live snapshot updates for that timer (public)

The frontend's `/` route lists and creates your timers; `/t/<id>` is that
timer's dashboard; `/t/<id>/overlay` is the public OBS view.

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
| `ALLOWED_ORIGIN`         | `http://localhost:5173`                      | CORS origin allowed to hit the API, and where OAuth flows redirect back to |
| `INITIAL_DURATION`       | `1h`                                          | Fallback clock length if not set on reset                   |
| `DB_PATH`                | `data/subathon.db`                           | SQLite database file path (create the dir via a volume)     |
| `COOKIE_SECURE`          | `false`                                       | Mark the session cookie HTTPS-only; set `true` once served over TLS |
| `TWITCH_CLIENT_ID`       | *(unset — Twitch login disabled)*             | Twitch app client ID                                        |
| `TWITCH_CLIENT_SECRET`   | *(unset)*                                     | Twitch app client secret                                     |
| `TWITCH_REDIRECT_URL`    | `http://localhost:5173/auth/twitch/callback`  | Must exactly match the app's registered redirect URL         |
| `KICK_CLIENT_ID`         | *(unset — Kick login disabled)*               | Kick app client ID                                           |
| `KICK_CLIENT_SECRET`     | *(unset)*                                     | Kick app client secret                                       |
| `KICK_REDIRECT_URL`      | `http://localhost:5173/auth/kick/callback`    | Must exactly match the app's registered redirect URL         |

The `*_REDIRECT_URL` defaults point at the Vite dev server's port, not the
API's — `frontend/vite.config.ts` proxies `/auth` through to the API, so
the whole app stays same-origin from the browser's perspective and the
session cookie "just works". If you access the app from another device's
browser (e.g. by LAN IP) or deploy it for real, register and set redirect
URLs that that browser can actually reach — `localhost` only resolves on
the machine running the browser.
