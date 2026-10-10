import { useEffect, useState } from 'react'
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { api, setUnauthorizedHandler } from './lib/api'
import { useApp } from './lib/store'
import type { User } from './lib/types'
import { ConfirmHost, Spinner, Toaster } from './components/ui'
import Shell from './components/Shell'
import { Login, Setup } from './pages/Auth'
import Dashboard from './pages/Dashboard'
import Overview from './pages/Overview'
import MonitorDetail from './pages/MonitorDetail'
import MonitorForm from './pages/MonitorForm'
import Tunnels from './pages/Tunnels'
import Servers from './pages/Servers'
import StatusPages from './pages/StatusPages'
import StatusPageEditor from './pages/StatusPageEditor'
import PublicStatus from './pages/status/PublicStatus'
import MaintenancePage from './pages/Maintenance'
import Settings from './pages/settings/Settings'
import NotFound from './pages/NotFound'

interface AuthState { setupRequired: boolean; siteTitle: string; version: string; user?: User }

export default function App() {
  const loc = useLocation()
  const nav = useNavigate()
  const [state, setState] = useState<AuthState | null>(null)
  const me = useApp((s) => s.me)
  const isPublic = loc.pathname.startsWith('/status/')

  useEffect(() => {
    if (isPublic) return
    api.get<AuthState>('/api/auth/state', { quiet401: true })
      .then((s) => {
        useApp.getState().setMe(s.user ?? null, s.version)
        setState(s)
      })
      .catch(() => setState({ setupRequired: false, siteTitle: 'Neo Uptime', version: '' }))
  }, [isPublic])

  useEffect(() => {
    setUnauthorizedHandler(() => {
      useApp.getState().setMe(null)
      const back = window.location.pathname + window.location.search
      nav(`/login?next=${encodeURIComponent(back)}`, { replace: true })
    })
  }, [nav])

  if (isPublic) {
    return (
      <>
        <Routes>
          <Route path="/status/:slug" element={<PublicStatus />} />
        </Routes>
        <Toaster />
      </>
    )
  }

  if (!state) {
    return (
      <div className="app-bg min-h-dvh grid place-items-center">
        <Spinner className="size-6" />
      </div>
    )
  }

  const authed = !!me
  return (
    <>
      <Routes>
        <Route path="/setup" element={state.setupRequired && !authed ? <Setup onDone={() => setState({ ...state, setupRequired: false })} /> : <Navigate to="/dashboard" replace />} />
        <Route path="/login" element={state.setupRequired ? <Navigate to="/setup" replace /> : authed ? <Navigate to="/dashboard" replace /> : <Login />} />
        <Route element={authed ? <Shell /> : <Navigate to={state.setupRequired ? '/setup' : `/login?next=${encodeURIComponent(loc.pathname)}`} replace />}>
          <Route path="/" element={<Navigate to="/dashboard" replace />} />
          <Route path="/dashboard" element={<Dashboard />}>
            <Route index element={<Overview />} />
            <Route path=":id" element={<MonitorDetail />} />
          </Route>
          <Route path="/add" element={<MonitorForm mode="add" />} />
          <Route path="/edit/:id" element={<MonitorForm mode="edit" />} />
          <Route path="/clone/:id" element={<MonitorForm mode="clone" />} />
          <Route path="/tunnels" element={<Tunnels />} />
          <Route path="/tunnels/panels" element={<Tunnels tab="panels" />} />
          <Route path="/servers" element={<Servers />} />
          <Route path="/status-pages" element={<StatusPages />} />
          <Route path="/status-pages/:id" element={<StatusPageEditor />} />
          <Route path="/maintenance" element={<MaintenancePage />} />
          <Route path="/settings" element={<Navigate to="/settings/general" replace />} />
          <Route path="/settings/:section" element={<Settings />} />
          <Route path="*" element={<NotFound />} />
        </Route>
      </Routes>
      <ConfirmHost />
      <Toaster />
    </>
  )
}
