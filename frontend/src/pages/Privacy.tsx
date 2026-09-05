import { Link } from 'react-router-dom'

/** Contact shown at the bottom of this page and used for data requests.
 * Self-hosted deployments each register their own OAuth apps (Twitch,
 * Kick, Google) and so each need their own reachable privacy policy — if
 * you're running your own instance of this project, replace this with
 * your own contact details before linking this page from your Google
 * Cloud OAuth consent screen or anywhere else. */
const OPERATOR_CONTACT = 'the person who operates this Subathon Tracker instance'

/** Static privacy policy, served at /privacy with no login required (see
 * App.tsx) so it can be linked from a Google Cloud OAuth consent screen's
 * "Privacy Policy URL" field — required for any app requesting a
 * sensitive scope like youtube.readonly (see
 * internal/oauth.NewYouTube) — and from Twitch/Kick's own app
 * registration if they ask for one too. Describes what this specific
 * codebase actually stores/does, not a generic template; keep this in
 * sync with internal/sqlite's schema and internal/oauth/providers.go's
 * scopes if either changes. */
export default function Privacy() {
  return (
    <div className="dashboard">
      <header>
        <div>
          <Link to="/" className="back-link">
            &larr; Subathon Tracker
          </Link>
          <h1>Privacy Policy</h1>
        </div>
      </header>

      <section className="events">
        <h2>What this is</h2>
        <p className="empty">
          Subathon Tracker is a self-hosted tool that runs a countdown
          clock alongside a streamer's channel: it watches for subs, gift
          subs, cheers/Kicks, Super Chats, and gift-platform contributions
          (Throne) and adds time and/or dollars to the clock accordingly.
          This page explains what data the software collects and why —
          it applies to this specific instance of it.
        </p>
      </section>

      <section className="events">
        <h2>Account data</h2>
        <p className="empty">
          Signing in links a Twitch, Kick, or YouTube (Google) account.
          We store: an internal account ID, the display name and
          platform user ID from whichever platform(s) you've linked, and
          — server-side only, never sent to your browser — the OAuth
          access/refresh tokens needed to keep that connection working.
          A session cookie (a random token, not your platform password)
          keeps you signed in; it carries no personal data itself.
        </p>
        <p className="empty">
          You can unlink a platform identity at any time from{' '}
          <Link to="/account">Account settings</Link>, which deletes its
          stored tokens from our database (unless it's your only linked
          identity, since there's no password login to fall back on).
          Unlinking here doesn't itself revoke the grant on the
          platform's side — for Google/YouTube specifically, you can also
          remove this app's access directly at{' '}
          <a
            href="https://myaccount.google.com/permissions"
            target="_blank"
            rel="noreferrer"
          >
            myaccount.google.com/permissions
          </a>
          .
        </p>
      </section>

      <section className="events">
        <h2>YouTube data specifically</h2>
        <p className="empty">
          Linking YouTube requests one scope,{' '}
          <code>youtube.readonly</code>, used only to: identify which
          channel your Google account owns, check whether it's currently
          live, and read that live broadcast's chat to detect Super
          Chats, Super Stickers, new memberships, and gifted memberships
          — each translated into time/dollars added to a timer you've
          configured to watch that channel. This app never posts,
          comments, or moderates on your behalf, and never reads chat on
          a channel you haven't explicitly linked and configured a timer
          to watch.
        </p>
        <p className="empty">
          <strong>
            Subathon Tracker's use and transfer of information received
            from Google APIs to any other app will adhere to the{' '}
            <a
              href="https://developers.google.com/terms/api-services-user-data-policy"
              target="_blank"
              rel="noreferrer"
            >
              Google API Services User Data Policy
            </a>
            , including the Limited Use requirements.
          </strong>
        </p>
      </section>

      <section className="events">
        <h2>Timer and contribution data</h2>
        <p className="empty">
          Each timer stores its own clock state, configured reward/money
          rates, watched channel(s), overlay appearance, and its
          contribution history: for each sub/gift/cheer/tip/gift event,
          the contributor's platform username, the platform it came from,
          how much time/money it added, and when. This is the data that
          drives the on-stream overlay and the dashboard's history page.
        </p>
        <p className="empty">
          <strong>
            A timer's public overlay, goals overlay, and history page are
            reachable by anyone with that timer's link, no login
            required
          </strong>{' '}
          — the same way a shared Google Doc link works. Don't share a
          timer's dashboard/overlay URLs anywhere you wouldn't want its
          contribution history seen, and be aware that anyone who
          contributes to a public subathon timer will have their
          platform username and contribution shown on it.
        </p>
      </section>

      <section className="events">
        <h2>Third-party platforms</h2>
        <p className="empty">
          Connecting a channel or donation platform (Twitch, Kick,
          YouTube/Google, StreamElements, Throne) means that platform
          sends this app the minimum event data needed to detect a
          contribution — it doesn't pull your full follower list, message
          history, or anything unrelated. Each of those platforms
          handles your data under its own privacy policy as well; this
          page only covers what Subathon Tracker itself does with what
          it receives.
        </p>
      </section>

      <section className="events">
        <h2>What we don't do</h2>
        <p className="empty">
          No advertising, no analytics/tracking scripts, and no selling
          or sharing of your data with third parties beyond what's
          needed to run the integrations you've explicitly connected
          (described above).
        </p>
      </section>

      <section className="events">
        <h2>Data removal</h2>
        <p className="empty">
          To request deletion of your account or a timer's contribution
          history, contact {OPERATOR_CONTACT}.
        </p>
      </section>

      <footer>
        <p>This policy may be updated as the app's features change.</p>
      </footer>
    </div>
  )
}
