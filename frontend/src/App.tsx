import { Route, BrowserRouter, Routes } from 'react-router-dom'
import { AuthProvider } from './lib/AuthContext'
import RequireAuth from './RequireAuth'
import Account from './pages/Account'
import Dashboard from './pages/Dashboard'
import GoalsOverlay from './pages/GoalsOverlay'
import History from './pages/History'
import Login from './pages/Login'
import Overlay from './pages/Overlay'
import Panel from './pages/Panel'
import Privacy from './pages/Privacy'
import Rewards from './pages/Rewards'
import Styling from './pages/Styling'
import Timers from './pages/Timers'

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<Login />} />
          {/* Public: linked from Login and from Google/Twitch/Kick's own
              OAuth app registration (a "Privacy Policy URL" field), so it
              must be reachable with no session. */}
          <Route path="/privacy" element={<Privacy />} />
          {/* Public: the timer ID in the URL is the token an OBS overlay
              or shared dashboard link needs, no login required. */}
          <Route path="/t/:timerId/overlay" element={<Overlay />} />
          {/* Public too, same token — a separate OBS browser source for
              the timer's money-milestone list, so it can be placed/sized
              independently in a scene from the main timer/money overlay. */}
          <Route path="/t/:timerId/goals-overlay" element={<GoalsOverlay />} />
          {/* Public too, same token — the top-10 leaderboard panel, meant
              for a Twitch "panel" or other static embed rather than an
              OBS browser source (see Panel.tsx's own doc comment). */}
          <Route path="/t/:timerId/panel" element={<Panel />} />
          {/* Public too — same token, and the contribution history it
              shows is already part of the public Snapshot's recent-events
              list, just uncapped. */}
          <Route path="/t/:timerId/history" element={<History />} />

          {/* Public: shows a login option in the header rather than
              force-redirecting to /login when logged out — see Timers,
              which renders its own logged-out state. */}
          <Route path="/" element={<Timers />} />
          <Route
            path="/account"
            element={
              <RequireAuth>
                <Account />
              </RequireAuth>
            }
          />
          <Route
            path="/t/:timerId"
            element={
              <RequireAuth>
                <Dashboard />
              </RequireAuth>
            }
          />
          <Route
            path="/t/:timerId/rewards"
            element={
              <RequireAuth>
                <Rewards />
              </RequireAuth>
            }
          />
          <Route
            path="/t/:timerId/styling"
            element={
              <RequireAuth>
                <Styling />
              </RequireAuth>
            }
          />
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  )
}
