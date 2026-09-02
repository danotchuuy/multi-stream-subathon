import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from './lib/AuthContext'

/** Wrap a route's element to redirect to /login when logged out. */
export default function RequireAuth({ children }: { children: ReactNode }) {
  const { user } = useAuth()

  if (user === undefined) {
    return <p className="empty">Loading…</p>
  }
  if (user === null) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}
