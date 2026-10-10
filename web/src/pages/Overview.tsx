import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Activity, ArrowDown, ArrowUp, BellRing, Cable, CirclePause, Plus, Radar, Server, Wrench, X } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { clsx, fmtDateTime, ms, n, pct, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import type { Heartbeat } from '../lib/types'
import { Button, Card, CardHeader, Empty, Pagination, Skeleton, StatusPill, useNow } from '../components/ui'
import { Flag } from '../components/flag'
import { monitorCountry, monitorState } from './Dashboard'

function Kpi({ label, value, tone, icon, sub }: { label: string; value: number | string; tone: string; icon: React.ReactNode; sub?: string }) {
  return (
    <div className="card p-4 sm:p-5 min-w-0">
      <div className="flex items-center gap-2.5 min-w-0">
        <span className={clsx('grid place-items-center size-7 shrink-0 rounded-lg [&>svg]:size-3.5', tone)}>{icon}</span>
        <span className="text-[13px] text-muted font-medium truncate">{label}</span>
      </div>
      <p className="mt-3 font-display text-[38px] leading-none font-semibold tnum">{typeof value === 'number' ? n(value) : value}</p>
      <p className="mt-2 text-[12px] text-subtle truncate">{sub ?? ' '}</p>
    </div>
  )
}

function AlertsHint() {
  const t = useT()
  const nav = useNavigate()
  const notifications = useApp((s) => s.notifications)
  const role = useApp((s) => s.role)
  const [hidden, setHidden] = useState(() => {
    try {
      return localStorage.getItem('neo-hide-alert-hint') === '1'
    } catch {
      return false
    }
  })
  if (hidden || notifications.length > 0 || !canEdit(role)) return null
  return (
    <div className="card relative overflow-hidden p-5 flex flex-wrap items-center gap-4">
      <div className="absolute inset-0 [background:radial-gradient(500px_200px_at_100%_0%,var(--accent-soft),transparent)]" aria-hidden />
      <span className="relative grid place-items-center size-11 rounded-2xl bg-accent text-on-accent"><BellRing className="size-5" /></span>
      <div className="relative min-w-0 flex-1">
        <p className="font-display font-semibold">{t('Get a Telegram message when something goes down')}</p>
        <p className="text-[13px] text-muted mt-0.5">{t('Connect a bot once; every monitor can use it. Bale, Discord, email and more work too.')}</p>
      </div>
      <Button className="relative" variant="primary" onClick={() => nav('/settings/notifications?new=telegram')}>{t('Set up Telegram')}</Button>
      <button className="relative text-subtle hover:text-fg" aria-label={t('Dismiss')} onClick={() => { setHidden(true); try { localStorage.setItem('neo-hide-alert-hint', '1') } catch { /* ignore */ } }}>
        <X className="size-4" />
      </button>
    </div>
  )
}

export default function Overview() {
  const t = useT()
  const monitors = useApp((s) => s.monitors)
  const loaded = useApp((s) => s.loaded)
  const role = useApp((s) => s.role)
  const agents = useApp((s) => s.agents)
  const nav = useNavigate()
  const now = useNow(30_000)
  const [events, setEvents] = useState<Heartbeat[] | null>(null)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const per = 15
  const lastImportant = useApp((s) => Object.values(s.monitors).reduce((a, m) => Math.max(a, m.last?.important ? m.last.time : 0), 0))

  useEffect(() => {
    api.get<{ items: Heartbeat[]; total: number }>(`/api/important-events?limit=${per}&offset=${(page - 1) * per}`)
      .then((r) => { setEvents(r.items); setTotal(r.total) })
      .catch(() => setEvents([]))
  }, [page, lastImportant])

  const stats = useMemo(() => {
    const list = Object.values(monitors).filter((m) => m.type !== 'group')
    const c = { up: 0, down: 0, maint: 0, paused: 0, pending: 0, empty: 0 }
    let pingSum = 0, pingN = 0, upSum = 0, upN = 0
    for (const m of list) {
      c[monitorState(m) as keyof typeof c]++
      if (m.active && m.avgPing24 >= 0) { pingSum += m.avgPing24; pingN++ }
      if (m.active && m.uptime24 >= 0) { upSum += m.uptime24; upN++ }
    }
    const tunnels = list.filter((m) => m.type === 'backpack-tunnel' || m.type === 'agent-tunnel')
    return { ...c, total: list.length, avgPing: pingN ? pingSum / pingN : -1, uptime: upN ? upSum / upN : -1, tunnels: tunnels.length, tunnelsUp: tunnels.filter((m) => m.last?.status === 1).length }
  }, [monitors])

  if (loaded && stats.total === 0) {
    return (
      <div className="p-6 lg:p-10 space-y-5">
        <Card className="max-w-3xl mx-auto">
          <Empty
            icon={<Activity className="size-6" />}
            title={t('Start monitoring')}
            body={t('Add a website, port, DNS record or database — or install the agent on a server and its BackPack and Kariz tunnels become monitors automatically.')}
            action={
              canEdit(role) && (
                <div className="flex flex-wrap justify-center gap-2">
                  <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => nav('/add')}>{t('Add monitor')}</Button>
                  <Button icon={<Server className="size-4" />} onClick={() => nav('/servers')}>{t('Add a server')}</Button>
                  <Button icon={<Cable className="size-4" />} onClick={() => nav('/tunnels/panels')}>{t('Connect BackPack panel')}</Button>
                </div>
              )
            }
          />
        </Card>
        <div className="max-w-3xl mx-auto"><AlertsHint /></div>
      </div>
    )
  }

  const pages = Math.ceil(total / per)
  const offline = agents.filter((a) => a.lastSeen > 0 && !a.online)
  return (
    <div className="@container p-4 sm:p-6 lg:p-8 space-y-6 max-w-[1400px]">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="font-display text-[28px] font-semibold tracking-tight">{t('Overview')}</h1>
          <p className="text-sm text-muted mt-1">
            {stats.down > 0 ? <span className="text-down font-medium">{t('{n} monitors are down right now.', { n: stats.down })}</span> : t('Everything that is being checked is answering.')}
          </p>
        </div>
        <div className="flex gap-6">
          <div>
            <p className="text-[12px] uppercase tracking-wider text-subtle font-semibold">{t('Uptime 24h')}</p>
            <p className="font-display text-2xl font-semibold tnum mt-0.5">{pct(stats.uptime)}</p>
          </div>
          <div>
            <p className="text-[12px] uppercase tracking-wider text-subtle font-semibold">{t('Avg response')}</p>
            <p className="font-display text-2xl font-semibold tnum mt-0.5">{ms(stats.avgPing)}</p>
          </div>
        </div>
      </div>

      <AlertsHint />

      {offline.length > 0 && (
        <Link to="/servers" className="block rounded-2xl border border-down/30 bg-down/8 px-4 py-3 text-sm text-down hover:bg-down/12 transition-colors">
          {t('{n} servers stopped reporting:', { n: offline.length })} {offline.map((a) => a.name).join('، ')}
        </Link>
      )}

      {!loaded ? (
        <div className="grid grid-cols-2 @lg:grid-cols-3 @2xl:grid-cols-5 gap-4">{Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-32 rounded-[18px]" />)}</div>
      ) : (
        <div className="grid grid-cols-2 @lg:grid-cols-3 @2xl:grid-cols-5 gap-3 sm:gap-4">
          <Kpi label={t('Up')} value={stats.up} tone="bg-up/14 text-up" icon={<ArrowUp />} sub={t('of {n} monitors', { n: stats.total })} />
          <Kpi label={t('Down')} value={stats.down} tone="bg-down/14 text-down" icon={<ArrowDown />} sub={stats.pending ? t('{n} retrying', { n: stats.pending }) : t('none retrying')} />
          <Kpi label={t('Maintenance')} value={stats.maint} tone="bg-maint/14 text-maint" icon={<Wrench />} />
          <Kpi label={t('Paused')} value={stats.paused} tone="bg-surface-3 text-muted" icon={<CirclePause />} />
          <Kpi label={t('Tunnels')} value={stats.tunnels} tone="bg-accent-soft text-accent-ink" icon={<Cable />} sub={t('{n} online', { n: stats.tunnelsUp })} />
        </div>
      )}

      <Card>
        <CardHeader title={t('Important events')} subtitle={t('Every change between up, down and maintenance.')} icon={<Radar className="size-4" />} />
        <div className="overflow-x-auto">
          <table className="w-full min-w-[620px] table-fixed text-sm">
            <thead>
              <tr className="text-start text-[12px] uppercase tracking-wider text-subtle border-y border-line">
                <th className="font-semibold px-5 py-2.5 text-start w-[32%]">{t('Monitor')}</th>
                <th className="font-semibold px-3 py-2.5 text-start w-28">{t('Status')}</th>
                <th className="font-semibold px-3 py-2.5 text-start w-32">{t('When')}</th>
                <th className="font-semibold px-5 py-2.5 text-start">{t('Message')}</th>
              </tr>
            </thead>
            <tbody>
              {events === null ? (
                Array.from({ length: 5 }).map((_, i) => (
                  <tr key={i}><td colSpan={4} className="px-5 py-2"><Skeleton className="h-6" /></td></tr>
                ))
              ) : events.length === 0 ? (
                <tr><td colSpan={4} className="px-5 py-10 text-center text-muted">{t('No events yet — they appear when a monitor changes state.')}</td></tr>
              ) : (
                events.map((e) => {
                  const m = monitors[e.monitorId]
                  return (
                    <tr key={e.id} className="border-b border-line last:border-0 hover:bg-surface-2/60 transition-colors">
                      <td className="px-5 py-3 font-medium">
                        {m ? (
                          <Link to={`/dashboard/${m.id}`} className="inline-flex max-w-full items-center gap-2 hover:text-accent-ink">
                            {monitorCountry(m) && <Flag cc={monitorCountry(m)} size={16} />}
                            <span className="min-w-0 truncate" dir="auto">{m.name}</span>
                          </Link>
                        ) : <span className="text-muted">{t('Deleted monitor')}</span>}
                      </td>
                      <td className="px-3 py-3"><StatusPill status={e.status} size="sm" /></td>
                      <td className="px-3 py-3 whitespace-nowrap text-muted tnum" title={fmtDateTime(e.time)}>{timeAgo(e.time, now)}</td>
                      <td className="px-5 py-3 text-muted"><div className="truncate" title={e.msg} dir="auto">{e.msg}</div></td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
        {pages > 1 && <div className="flex justify-end px-5 py-3 border-t border-line"><Pagination page={page} pages={pages} onPage={setPage} /></div>}
      </Card>
    </div>
  )
}
