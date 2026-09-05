import { useEffect, useState } from 'react'
import { Link, Navigate, useSearchParams } from 'react-router-dom'
import { getAuthProviders } from '../lib/api'
import { useAuth } from '../lib/AuthContext'
import type { AuthPlatform } from '../types'

const PROVIDER_LABELS: Record<AuthPlatform, string> = {
  twitch: 'Twitch',
  kick: 'Kick',
  youtube: 'YouTube',
}

const ERROR_MESSAGES: Record<string, string> = {
  oauth_failed: 'Something went wrong signing you in. Please try again.',
}

export default function Login() {
  const { user } = useAuth()
  const [providers, setProviders] = useState<string[] | null>(null)
  const [params] = useSearchParams()

  useEffect(() => {
    getAuthProviders()
      .then(setProviders)
      .catch(() => setProviders([]))
  }, [])

  if (user) {
    return <Navigate to="/" replace />
  }

  const error = params.get('error')

  return (
    <div className="dashboard login-page">
      <header>
        <h1>Subathon Tracker</h1>
      </header>

      <section className="clock-card login-card">
        <p>Sign up or log in with a streaming platform account.</p>

        {error && (
          <p className="error-message">
            {ERROR_MESSAGES[error] ?? 'Login failed. Please try again.'}
          </p>
        )}

        <div className="login-buttons">
          {(['twitch', 'kick', 'youtube'] as AuthPlatform[]).map((platform) => {
            const enabled = providers?.includes(platform) ?? false
            return (
              <a
                key={platform}
                href={enabled ? `/auth/${platform}/start?mode=login` : undefined}
                className={`login-button login-button-${platform} ${enabled ? '' : 'disabled'}`}
                aria-disabled={!enabled}
                title={
                  enabled
                    ? undefined
                    : `${PROVIDER_LABELS[platform]} login isn't configured on this server`
                }
              >
                Continue with {PROVIDER_LABELS[platform]}
              </a>
            )
          })}
        </div>

        {providers && providers.length === 0 && (
          <p className="empty">
            No login providers are configured yet. Set TWITCH_CLIENT_ID,
            KICK_CLIENT_ID, or YOUTUBE_CLIENT_ID on the server to enable
            sign-in.
          </p>
        )}
      </section>

      <footer>
        <p>
          <Link to="/privacy">Privacy Policy</Link>
        </p>
      </footer>
    </div>
  )
}
