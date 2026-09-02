import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { getAuthProviders } from '../lib/api'
import { useAuth } from '../lib/AuthContext'
import type { AuthPlatform } from '../types'

const PROVIDER_LABELS: Record<AuthPlatform, string> = {
  twitch: 'Twitch',
  kick: 'Kick',
  youtube: 'YouTube',
}

// Platforms that can be linked via OAuth today. YouTube isn't wired up
// yet (see internal/oauth), so it's left off this list rather than
// linking to a 404.
const LINKABLE_PLATFORMS: AuthPlatform[] = ['twitch', 'kick']

const ERROR_MESSAGES: Record<string, string> = {
  already_linked: 'That account is already linked to a different user.',
  link_failed: 'Something went wrong linking that account. Please try again.',
}

export default function Account() {
  const { user, logout } = useAuth()
  const [providers, setProviders] = useState<string[] | null>(null)
  const [params] = useSearchParams()

  useEffect(() => {
    getAuthProviders()
      .then(setProviders)
      .catch(() => setProviders([]))
  }, [])

  if (!user) return null

  const error = params.get('error')
  const linkedPlatforms = new Set(user.identities.map((i) => i.platform))
  const linkable = LINKABLE_PLATFORMS.filter((p) => !linkedPlatforms.has(p))

  return (
    <div className="dashboard">
      <header>
        <div>
          <Link to="/" className="back-link">
            &larr; Timers
          </Link>
          <h1>{user.displayName}</h1>
        </div>
        <button type="button" onClick={() => logout()}>
          Log out
        </button>
      </header>

      {error && (
        <p className="error-message">
          {ERROR_MESSAGES[error] ?? 'Something went wrong.'}
        </p>
      )}

      <section className="events">
        <h2>Linked platforms</h2>
        <ul>
          {user.identities.map((i) => (
            <li key={i.platform}>
              <span className="platform">{i.platform}</span>
              <span>{i.username}</span>
            </li>
          ))}
        </ul>
      </section>

      {linkable.length > 0 && (
        <section className="controls">
          <div className="control-group">
            {linkable.map((platform) => {
              const enabled = providers?.includes(platform) ?? false
              return (
                <a
                  key={platform}
                  href={
                    enabled ? `/auth/${platform}/start?mode=link` : undefined
                  }
                  className={`login-button login-button-${platform} ${enabled ? '' : 'disabled'}`}
                  aria-disabled={!enabled}
                  title={
                    enabled
                      ? undefined
                      : `${PROVIDER_LABELS[platform]} login isn't configured on this server`
                  }
                >
                  Link {PROVIDER_LABELS[platform]}
                </a>
              )
            })}
          </div>
        </section>
      )}
    </div>
  )
}
