import { Route, BrowserRouter, Routes } from 'react-router-dom'
import { AuthProvider } from './lib/AuthContext'
import RequireAuth from './RequireAuth'
import Account from './pages/Account'
import Dashboard from './pages/Dashboard'
import Login from './pages/Login'
import Overlay from './pages/Overlay'
import Timers from './pages/Timers'

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<Login />} />
          {/* Public: the timer ID in the URL is the token an OBS overlay
              or shared dashboard link needs, no login required. */}
          <Route path="/t/:timerId/overlay" element={<Overlay />} />

          <Route
            path="/"
            element={
              <RequireAuth>
                <Timers />
              </RequireAuth>
            }
          />
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
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  )
}
