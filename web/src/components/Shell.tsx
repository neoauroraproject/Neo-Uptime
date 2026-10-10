import { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Cable, LayoutDashboard, LogOut, Menu as MenuIcon, Monitor, Moon, Plus, Rss, Settings, Sun, Wrench, X } from 'lucide-react'
import { api } from '../lib/api'
import { useLiveEvents } from '../lib/live'
import { canEdit, useApp } from '../lib/store'
import { useAdminTheme, type AdminTheme } from '../lib/theme'
import { clsx } from '../lib/format'
import { Button, Menu } from './ui'
import { Logo } from './meta'

const nav = [
  { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/tunnels', label: 'Tunnels', icon: Cable },
  { to: '/status-pages', label: 'Status pages', icon: Rss },
  { to: '/maintenance', label: 'Maintenance', icon: Wrench },
  { to: '/settings', label: 'Settings', icon: Settings },
]

function Counts() {
  const monitors = useApp((s) => s.monitors)
  let up = 0, down = 0
  for (const m of Object.values(monitors)) {
    if (!m.active || m.type === 'group') continue
    if (m.last?.status === 1) up++
    else if (m.last?.status === 0) down++
  }
  return (
    <div className="flex items-center gap-3 text-[12.5px] tnum">
      <span className="inline-flex items-center gap-1.5 text-muted"><span className="size-2 rounded-full bg-up" />{up} up</span>
      <span className={clsx('inline-flex items-center gap-1.5', down ? 'text-down font-semibold' : 'text-muted')}><span className="size-2 rounded-full bg-down" />{down} down</span>
    </div>
  )
}

function ThemeSwitch() {
  const [theme, setTheme] = useAdminTheme()
  const opts: { v: AdminTheme; icon: typeof Sun; label: string }[] = [
    { v: 'light', icon: Sun, label: 'Light' },
    { v: 'dark', icon: Moon, label: 'Dark' },
    { v: 'system', icon: Monitor, label: 'System' },
  ]
  return (
    <div role="radiogroup" aria-label="Theme" className="flex p-1 rounded-xl bg-surface-2 border border-line">
      {opts.map((o) => (
        <button
          key={o.v}
          role="radio"
          aria-checked={theme === o.v}
          aria-label={o.label}
          title={o.label}
          onClick={() => setTheme(o.v)}
          className={clsx('flex-1 grid place-items-center h-7 rounded-lg transition-colors', theme === o.v ? 'bg-surface text-fg shadow-sm' : 'text-subtle hover:text-fg')}
        >
          <o.icon className="size-3.5" />
        </button>
      ))}
    </div>
  )
}

export default function Shell() {
  useLiveEvents()
  const me = useApp((s) => s.me)
  const role = useApp((s) => s.role)
  const live = useApp((s) => s.live)
  const settings = useApp((s) => s.settings)
  const loc = useLocation()
  const navigate = useNavigate()
  const [drawer, setDrawer] = useState(false)

  useEffect(() => {
    const s = useApp.getState()
    s.loadSettings().catch(() => {})
    s.loadDashboard().catch(() => {})
    s.loadTags().catch(() => {})
    s.loadNotifications().catch(() => {})
    s.loadPanels().catch(() => {})
  }, [])

  useEffect(() => setDrawer(false), [loc.pathname])

  const logout = async () => {
    await api.post('/api/auth/logout').catch(() => {})
    useApp.getState().setMe(null)
    navigate('/login')
  }

  const sidebar = (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between px-5 h-16">
        <Logo title={settings.siteTitle} />
        <button className="lg:hidden text-muted hover:text-fg" aria-label="Close menu" onClick={() => setDrawer(false)}>
          <X className="size-5" />
        </button>
      </div>
      {canEdit(role) && (
        <div className="px-4 pb-3">
          <Button variant="primary" className="w-full" icon={<Plus className="size-4" />} onClick={() => navigate('/add')}>
            Add monitor
          </Button>
        </div>
      )}
      <nav className="px-3 space-y-0.5" aria-label="Main">
        {nav.map((n) => (
          <NavLink
            key={n.to}
            to={n.to}
            className={({ isActive }) =>
              clsx(
                'group flex items-center gap-3 h-10 px-3 rounded-xl text-[14px] font-medium transition-colors',
                isActive ? 'bg-accent-soft text-accent-ink' : 'text-muted hover:text-fg hover:bg-surface-2',
              )
            }
          >
            <n.icon className="size-[18px]" />
            {n.label}
          </NavLink>
        ))}
      </nav>
      <div className="mt-auto p-4 space-y-3">
        <div className="rounded-2xl border border-line bg-surface-2/60 p-3 space-y-2.5">
          <div className="flex items-center justify-between">
            <span className="text-[12px] uppercase tracking-wider text-subtle font-semibold">Live</span>
            <span className={clsx('inline-flex items-center gap-1.5 text-[12px]', live === 'open' ? 'text-up' : live === 'connecting' ? 'text-pending' : 'text-down')}>
              <span className={clsx('size-1.5 rounded-full', live === 'open' ? 'bg-up live-dot' : live === 'connecting' ? 'bg-pending' : 'bg-down')} />
              {live === 'open' ? 'Connected' : live === 'connecting' ? 'Connecting' : 'Reconnecting'}
            </span>
          </div>
          <Counts />
        </div>
        <ThemeSwitch />
        <Menu
          align="start"
          trigger={(p) => (
            <button {...p} className="w-full flex items-center gap-3 rounded-xl px-2 py-2 hover:bg-surface-2 transition-colors text-left">
              <span className="grid place-items-center size-8 rounded-full bg-accent text-on-accent font-display font-semibold uppercase">{me?.username?.[0] ?? '?'}</span>
              <span className="min-w-0">
                <span className="block text-sm font-medium truncate">{me?.username}</span>
                <span className="block text-[12px] text-muted capitalize">{role}</span>
              </span>
            </button>
          )}
          items={[
            { label: 'Account & security', icon: <Settings />, onClick: () => navigate('/settings/security') },
            { divider: true, label: '' },
            { label: 'Sign out', icon: <LogOut />, onClick: logout, danger: true },
          ]}
        />
      </div>
    </div>
  )

  return (
    <div className="app-bg min-h-dvh">
      <a href="#main" className="sr-only focus:not-sr-only focus:fixed focus:z-50 focus:top-2 focus:left-2 focus:bg-surface focus:px-3 focus:py-2 focus:rounded-lg">Skip to content</a>
      <aside className="hidden lg:block fixed inset-y-0 left-0 w-64 border-r border-line bg-surface/70 backdrop-blur-xl z-30">{sidebar}</aside>
      {drawer && (
        <div className="lg:hidden fixed inset-0 z-40">
          <div className="absolute inset-0 bg-black/60" onClick={() => setDrawer(false)} aria-hidden />
          <aside className="absolute inset-y-0 left-0 w-72 max-w-[85vw] bg-surface border-r border-line fade-up">{sidebar}</aside>
        </div>
      )}
      <header className="lg:hidden sticky top-0 z-20 flex items-center justify-between gap-3 h-14 px-4 border-b border-line bg-surface/80 backdrop-blur-xl">
        <button aria-label="Open menu" onClick={() => setDrawer(true)} className="grid place-items-center size-10 -ml-2 rounded-xl text-muted hover:text-fg">
          <MenuIcon className="size-5" />
        </button>
        <Logo title={settings.siteTitle} />
        {canEdit(role) ? (
          <button aria-label="Add monitor" onClick={() => navigate('/add')} className="grid place-items-center size-10 -mr-2 rounded-xl bg-accent text-on-accent">
            <Plus className="size-5" />
          </button>
        ) : <span className="size-10" />}
      </header>
      <main id="main" className="lg:pl-64 min-w-0">
        <Outlet />
      </main>
    </div>
  )
}
