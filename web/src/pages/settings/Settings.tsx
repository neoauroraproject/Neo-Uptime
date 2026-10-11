import { NavLink, Navigate, useParams } from 'react-router-dom'
import { Archive, Bell, Code, Database, Info, KeyRound, LockKeyhole, Palette, Settings2, ShieldCheck, Tag, Users } from 'lucide-react'
import { useApp } from '../../lib/store'
import { clsx } from '../../lib/format'
import { useT } from '../../lib/i18n'
import { Appearance, General, History } from './General'
import NotificationsSettings from './Notifications'
import Security from './Security'
import StatusAPI from './StatusAPI'
import HTTPS from './HTTPS'
import { About, ApiKeys, Backup, Tags, UsersSettings } from './Admin'

const sections = [
  { id: 'general', label: 'General', icon: Settings2, admin: true, el: <General /> },
  { id: 'https', label: 'Domains & HTTPS', icon: LockKeyhole, admin: true, el: <HTTPS /> },
  { id: 'appearance', label: 'Appearance & language', icon: Palette, el: <Appearance /> },
  { id: 'notifications', label: 'Notifications', icon: Bell, el: <NotificationsSettings /> },
  { id: 'tags', label: 'Tags', icon: Tag, el: <Tags /> },
  { id: 'history', label: 'Monitor history', icon: Database, admin: true, el: <History /> },
  { id: 'security', label: 'Security', icon: ShieldCheck, el: <Security /> },
  { id: 'users', label: 'Users', icon: Users, admin: true, el: <UsersSettings /> },
  { id: 'api-keys', label: 'API keys', icon: KeyRound, admin: true, el: <ApiKeys /> },
  { id: 'status-api', label: 'Status API', icon: Code, owner: true, el: <StatusAPI /> },
  { id: 'backup', label: 'Backup', icon: Archive, admin: true, el: <Backup /> },
  { id: 'about', label: 'About', icon: Info, el: <About /> },
]

export default function Settings() {
  const t = useT()
  const { section } = useParams()
  const role = useApp((s) => s.role)
  const me = useApp((s) => s.me)
  const visible = sections.filter((s) => (!s.admin || role === 'admin') && (!s.owner || me?.isOwner))
  const cur = visible.find((s) => s.id === section)
  if (!cur) return <Navigate to={`/settings/${visible[0].id}`} replace />
  return (
    <div className="p-4 sm:p-6 lg:p-8 max-w-[1200px]">
      <h1 className="font-display text-[28px] font-semibold tracking-tight">{t('Settings')}</h1>
      <div className="mt-6 grid md:grid-cols-[230px_minmax(0,1fr)] gap-6 items-start">
        <nav aria-label={t('Settings')} className="flex md:flex-col gap-1 overflow-x-auto md:overflow-visible -mx-4 px-4 md:mx-0 md:px-0 pb-1 md:sticky md:top-6">
          {visible.map((s) => (
            <NavLink key={s.id} to={`/settings/${s.id}`} className={({ isActive }) => clsx('flex items-center gap-2.5 h-10 px-3 rounded-xl text-sm font-medium whitespace-nowrap transition-colors', isActive ? 'bg-surface-3 text-fg' : 'text-muted hover:text-fg hover:bg-surface-2')}>
              <s.icon className="size-4" />{t(s.label)}
            </NavLink>
          ))}
        </nav>
        <div className="min-w-0 space-y-5 fade-up" key={cur.id}>{cur.el}</div>
      </div>
    </div>
  )
}
