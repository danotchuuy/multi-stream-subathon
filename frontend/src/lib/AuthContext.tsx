import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react'
import { getMe, HttpError, logout as apiLogout } from './api'
import type { Me } from '../types'

interface AuthState {
  /** undefined while the initial /api/me check is in flight. */
  user: Me | null | undefined
  refresh: () => void
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<Me | null | undefined>(undefined)

  const refresh = useCallback(() => {
    getMe()
      .then(setUser)
      .catch((err) => {
        if (err instanceof HttpError && err.status === 401) {
          setUser(null)
        } else {
          // Network or server error: treat as logged out rather than
          // leaving the app stuck on a loading state.
          setUser(null)
        }
      })
  }, [])

  useEffect(refresh, [refresh])

  const logout = useCallback(async () => {
    await apiLogout()
    setUser(null)
  }, [])

  return (
    <AuthContext.Provider value={{ user, refresh, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
