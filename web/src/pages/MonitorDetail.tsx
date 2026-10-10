import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Cable, Copy, Cpu, Ellipsis, Eraser, ExternalLink, Layers, LockKeyhole, Pause, Pencil, Play, RefreshCw, Server, Trash2, Webhook } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { clsx, duration, fmtDate, fmtDateTime, ms, n, pct, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import type { ChartPoint, Heartbeat, UptimeStat } from '../lib/types'
import { Badge, Button, Card, CardHeader, CopyButton, Empty, IconButton, Menu, Pagination, Segmented, Skeleton, StatusPill, TagChip, confirm, toast, useNow } from '../components/ui'
import { AreaChart, HeartbeatBar, beatsToPoints } from '../components/charts'
import { AgentPanel, SystemPanel, TunnelDetails } from '../components/tunnel'
import { TypeIcon, TypeLabel } from '../components/meta'
import { Flag, countryName } from '../components/flag'
import { monitorCountry, monitorState } from './Dashboard'

const ranges = [
  { value: 'recent', label: 'Live', ms: 0 },
  { value: '3h', label: '3h', ms: 3 * 3600e3 },
  { value: '24h', label: '24h', ms: 24 * 3600e3 },
  { value: '7d', label: '7d', ms: 7 * 86400e3 },
  { value: '30d', label: '30d', ms: 30 * 86400e3 },
] as const
type Range = (typeof ranges)[number]['value']

function Metric({ label, value, sub, tone }: { label: string; value: React.ReactNode; sub?: string; tone?: string }) {
  return (
    <div className="card px-4 py-3.5 min-w-0">
      <p className="text-[12.5px] text-muted truncate">{label}</p>
      <p className={clsx('mt-1 font-display text-[24px] leading-tight font-semibold tnum truncate', tone)}>{value}</p>
      {sub && <p className="text-[11.5px] text-subtle mt-0.5 truncate">{sub}</p>}
    </div>
  )
}

function PushHelp({ token, interval }: { token: string; interval: number }) {
  const t = useT()
  const base = useApp((s) => s.settings.primaryBaseURL) || window.location.origin
  const url = `${base}/api/push/${token}?status=up&msg=OK&ping=`
  const curl = `curl -fsS "${url}"`
  const loop = `#!/bin/bash\n# Report to Neo Uptime every ${interval}s — run it as a systemd service or in cron.\nwhile true; do\n  curl -fsS -o /dev/null "${url}" || true\n  sleep ${interval}\ndone`
  return (
    <Card>
      <CardHeader title={t('Push URL')} subtitle={t('Call this at least every {n}s. Silence marks the monitor down. Same format as Uptime Kuma, so existing scripts work after changing the host.', { n: interval })} icon={<Webhook className="size-4" />} />
      <div className="px-5 pb-5 space-y-3">
        <div className="flex gap-2">
          <code className="flex-1 min-w-0 truncate rounded-xl bg-surface-2 border border-line px-3 py-2.5 font-mono text-[12.5px]" dir="ltr">{url}</code>
          <CopyButton text={url} />
        </div>
        <details className="group">
          <summary className="cursor-pointer text-[13px] text-muted hover:text-fg select-none">{t('Examples: curl, bash loop')} · <span className="text-subtle">{t('status=down marks it down, ping= reports a latency')}</span></summary>
          <div className="mt-3 space-y-2" dir="ltr">
            <div className="flex gap-2 items-start"><pre className="flex-1 min-w-0 overflow-x-auto rounded-xl bg-surface-2 border border-line px-3 py-2.5 font-mono text-[12px]">{curl}</pre><CopyButton text={curl} /></div>
            <div className="flex gap-2 items-start"><pre className="flex-1 min-w-0 overflow-x-auto rounded-xl bg-surface-2 border border-line px-3 py-2.5 font-mono text-[12px] leading-relaxed">{loop}</pre><CopyButton text={loop} /></div>
          </div>
        </details>
      </div>
    </Card>
  )
}

export default function MonitorDetail() {
  const t = useT()
  const { id: idStr } = useParams()
  const id = Number(idStr)
  const valid = Number.isInteger(id) && id > 0
  const nav = useNavigate()
  const m = useApp((s) => s.monitors[id])
  const all = useApp((s) => s.monitors)
  const beats = useApp((s) => s.beats[id]) ?? []
  const loaded = useApp((s) => s.loaded)
  const role = useApp((s) => s.role)
  const panels = useApp((s) => s.panels)
  const agents = useApp((s) => s.agents)
  const now = useNow(15_000)
  const [range, setRange] = useState<Range>('recent')
  const [chart, setChart] = useState<ChartPoint[] | null>(null)
  const [stats, setStats] = useState<Record<string, UptimeStat> | null>(null)
  const [evTab, setEvTab] = useState<'important' | 'all'>('important')
  const [events, setEvents] = useState<Heartbeat[] | null>(null)
  const [evTotal, setEvTotal] = useState(0)
  const [page, setPage] = useState(1)
  const per = 12
  const lastTime = m?.last?.time ?? 0
  const minute = Math.floor(now / 60000)

  useEffect(() => {
    if (range === 'recent' || !valid) return
    setChart(null)
    api.get<ChartPoint[]>(`/api/monitors/${id}/chart?range=${range}`).then(setChart).catch(() => setChart([]))
  }, [id, range, minute])

  useEffect(() => {
    if (!valid) return
    api.get<Record<string, UptimeStat>>(`/api/monitors/${id}/stats`).then(setStats).catch(() => {})
  }, [id, minute, m?.last?.important])

  useEffect(() => {
    setPage(1)
  }, [id, evTab])

  useEffect(() => {
    if (!valid) return
    api.get<{ items: Heartbeat[]; total: number }>(`/api/monitors/${id}/heartbeats?limit=${per}&offset=${(page - 1) * per}${evTab === 'important' ? '&important=1' : ''}`)
      .then((r) => { setEvents(r.items); setEvTotal(r.total) })
      .catch(() => setEvents([]))
  }, [id, page, evTab, evTab === 'all' ? lastTime : m?.last?.important ? lastTime : 0])

  const recentPoints = useMemo(() => beatsToPoints(beats), [beats])
  const children = useMemo(() => Object.values(all).filter((x) => x.parentId === id), [all, id])

  if (!m) {
    if (!loaded) return <div className="p-8 space-y-4"><Skeleton className="h-10 w-72" /><Skeleton className="h-40" /><Skeleton className="h-72" /></div>
    return <Empty title={t('Monitor not found')} body={t('It may have been deleted.')} action={<Button onClick={() => nav('/dashboard')}>{t('Back to dashboard')}</Button>} />
  }

  const state = monitorState(m)
  const editable = canEdit(role)
  const isTunnel = m.type === 'backpack-tunnel' || m.type === 'agent-tunnel'
  const panel = m.config.panelId ? panels.find((p) => p.id === m.config.panelId) : undefined
  const agent = m.config.agentId ? agents.find((a) => a.id === m.config.agentId) : undefined
  const via = panel?.name ?? agent?.name
  const cc = monitorCountry(m)

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    try {
      await fn()
      toast.success(ok)
      await useApp.getState().loadDashboard()
    } catch (e) {
      toast.error(t('That did not work'), (e as Error).message)
    }
  }

  const del = async () => {
    if (!(await confirm({ title: t('Delete “{name}”?', { name: m.name }), body: t('Its whole history goes with it. This cannot be undone.'), confirmLabel: t('Delete monitor'), danger: true }))) return
    try {
      await api.del(`/api/monitors/${id}`)
      useApp.getState().removeMonitor(id)
      toast.success(t('Monitor deleted'))
      nav('/dashboard')
    } catch (e) {
      toast.error(t('Could not delete'), (e as Error).message)
    }
  }

  const s24 = stats?.['24h']
  const curPing = m.last?.status === 1 ? m.last.ping : null
  const pages = Math.ceil(evTotal / per)

  return (
    <div className="@container p-4 sm:p-6 lg:p-8 space-y-5 max-w-[1300px] fade-up" key={id}>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-[13px] text-muted">
            <TypeIcon type={m.type} className="size-4" />
            <TypeLabel type={m.type} />
            {m.parentId && all[m.parentId] && (
              <>
                <span className="text-subtle">{t('in')}</span>
                <Link to={`/dashboard/${m.parentId}`} className="inline-flex items-center gap-1 hover:text-fg"><Layers className="size-3.5" />{all[m.parentId].name}</Link>
              </>
            )}
          </div>
          <h1 className="mt-1.5 font-display text-[30px] leading-tight font-semibold tracking-tight break-words flex items-center gap-3">
            {cc && <Flag cc={cc} size={34} className="rounded-md" title />}
            <span dir="auto">{m.name}</span>
          </h1>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <StatusPill status={m.maintenance ? 3 : m.last?.status} active={m.active} size="lg" />
            {m.target && (m.target.startsWith('http') || m.target.startsWith('ws') ? (
              <a href={m.target} target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-1.5 text-sm text-accent-ink hover:underline break-all" dir="ltr">
                {m.target}<ExternalLink className="size-3.5 shrink-0" />
              </a>
            ) : <span className="text-sm text-muted font-mono" dir="ltr">{m.target}</span>)}
            {via && <span className="text-sm text-muted">{t('via')} <Link className="hover:text-fg" to={panel ? '/tunnels/panels' : '/servers'}>{via}</Link></span>}
            {cc && <span className="text-sm text-muted">{countryName(cc)}</span>}
            {m.tags.map((tg) => <TagChip key={`${tg.tagId}${tg.value}`} {...tg} />)}
          </div>
          {m.description && <p className="mt-3 text-sm text-muted max-w-2xl" dir="auto">{m.description}</p>}
        </div>
        {editable && (
          <div className="flex items-center gap-2">
            {m.active ? (
              <Button icon={<Pause className="size-4" />} onClick={async () => {
                if (await confirm({ title: t('Pause this monitor?'), body: t('No checks run and no alerts are sent until you resume it.'), confirmLabel: t('Pause') })) act(() => api.post(`/api/monitors/${id}/pause`), t('Monitor paused'))
              }}>{t('Pause')}</Button>
            ) : (
              <Button variant="primary" icon={<Play className="size-4" />} onClick={() => act(() => api.post(`/api/monitors/${id}/resume`), t('Monitor resumed'))}>{t('Resume')}</Button>
            )}
            <Button icon={<Pencil className="size-4" />} onClick={() => nav(`/edit/${id}`)}>{t('Edit')}</Button>
            <Menu
              trigger={(p) => <IconButton {...p} label={t('More actions')}><Ellipsis className="size-5" /></IconButton>}
              items={[
                { label: t('Check now'), icon: <RefreshCw />, onClick: () => act(() => api.post(`/api/monitors/${id}/check`), t('Check started')), disabled: !m.active },
                { label: t('Clone'), icon: <Copy />, onClick: () => nav(`/clone/${id}`) },
                { divider: true, label: '' },
                { label: t('Clear events'), icon: <Eraser />, onClick: async () => { if (await confirm({ title: t('Clear events?'), body: t('Removes the important-event marks; heartbeats stay.'), confirmLabel: t('Clear') })) act(() => api.del(`/api/monitors/${id}/events`), t('Events cleared')) } },
                { label: t('Clear heartbeats'), icon: <Eraser />, onClick: async () => { if (await confirm({ title: t('Clear heartbeats?'), body: t('Deletes this monitor’s raw check history. Uptime percentages are kept.'), confirmLabel: t('Clear'), danger: true })) act(() => api.del(`/api/monitors/${id}/heartbeats`), t('Heartbeats cleared')) } },
                { divider: true, label: '' },
                { label: t('Delete'), icon: <Trash2 />, danger: true, onClick: del },
              ]}
            />
          </div>
        )}
      </div>

      <Card className="px-5 py-4">
        <div className="flex items-center justify-between text-[13px] mb-3">
          <span className="text-muted">{t('Last {n} checks', { n: Math.min(beats.length, 100) })}</span>
          <span className="text-subtle tnum">
            {m.active ? (m.last ? t('checked {ago} · every {every}', { ago: timeAgo(m.last.time, now), every: duration(m.interval) }) : t('waiting for the first check…')) : t('paused')}
          </span>
        </div>
        <HeartbeatBar beats={beats} height={34} barWidth={7} gap={3} maxBeats={100} newestId={m.last?.id} showLabels />
        {m.last?.msg && <p className={clsx('mt-3 text-[13px] break-words', state === 'down' ? 'text-down' : 'text-muted')} dir="auto">{m.last.msg}</p>}
      </Card>

      <div className="grid grid-cols-2 @2xl:grid-cols-3 @5xl:grid-cols-6 gap-3">
        <Metric label={t('Response')} value={ms(curPing)} sub={t('current')} />
        <Metric label={t('Average')} value={ms(s24?.avgPing ?? m.avgPing24)} sub={t('last 24 hours')} />
        <Metric label={t('Uptime')} value={pct(stats?.['24h']?.ratio ?? m.uptime24)} sub={t('24 hours')} tone={(stats?.['24h']?.ratio ?? 1) < 0.95 && (stats?.['24h']?.ratio ?? 1) >= 0 ? 'text-down' : undefined} />
        <Metric label={t('Uptime')} value={pct(stats?.['30d']?.ratio ?? m.uptime30)} sub={t('30 days')} />
        <Metric label={t('Uptime')} value={pct(stats?.['1y']?.ratio)} sub={t('1 year')} />
        {m.cert ? (
          <Metric label={t('Certificate')} value={t('{n} days', { n: m.cert.daysRemaining })} sub={t('expires {d}', { d: fmtDate(m.cert.validTo * 1000) })} tone={m.cert.daysRemaining <= 7 ? 'text-down' : m.cert.daysRemaining <= 21 ? 'text-pending' : undefined} />
        ) : (
          <Metric label={t('Checks')} value={n(stats?.['24h']?.checks ?? 0)} sub={t('last 24 hours')} />
        )}
      </div>

      {m.type !== 'group' && (
        <Card>
          <CardHeader
            title={t('Response time')}
            subtitle={range === 'recent' ? t('Updates live with every check') : t('Averaged per slot; red bands are failed checks')}
            actions={<Segmented size="sm" ariaLabel={t('Chart range')} value={range} onChange={setRange} options={ranges.map((r) => ({ value: r.value, label: r.value === 'recent' ? t('Live') : n(r.label) }))} />}
          />
          <div className="px-3 sm:px-5 pb-4">
            {range === 'recent' ? (
              <AreaChart points={recentPoints} height={240} />
            ) : chart === null ? (
              <Skeleton className="h-[240px]" />
            ) : (
              <AreaChart points={chart} height={240} rangeMs={ranges.find((r) => r.value === range)!.ms} />
            )}
          </div>
        </Card>
      )}

      {m.type === 'push' && m.pushToken && <PushHelp token={m.pushToken} interval={m.interval} />}

      {m.type === 'agent' && m.agent && (
        <Card>
          <CardHeader title={t('Server')} subtitle={t('Reported by neo-uptime-agent')} icon={<Cpu className="size-4" />} actions={<Link to="/servers" className="text-[13px] text-accent-ink hover:underline">{t('All servers')}</Link>} />
          <div className="px-5 pb-5"><AgentPanel a={m.agent} /></div>
        </Card>
      )}

      {isTunnel && m.tunnel && (
        <Card>
          <CardHeader title={t('Tunnel')} subtitle={via ? t('As reported by {name}', { name: via }) : undefined} icon={<Cable className="size-4" />} />
          <div className="px-5 pb-5"><TunnelDetails t={m.tunnel} /></div>
        </Card>
      )}

      {m.type === 'backpack-panel' && m.system && (
        <Card>
          <CardHeader title={t('Panel host')} subtitle={t('CPU, memory and disk of the server running BackPack')} icon={<Server className="size-4" />} />
          <div className="px-5 pb-5"><SystemPanel s={m.system} /></div>
        </Card>
      )}

      {m.cert && (
        <Card>
          <CardHeader title={t('TLS certificate')} icon={<LockKeyhole className="size-4" />} actions={<Badge tone={m.cert.valid ? 'up' : 'down'}>{m.cert.valid ? t('Valid') : t('Invalid')}</Badge>} />
          <dl className="px-5 pb-5 grid @xl:grid-cols-2 @4xl:grid-cols-4 gap-4 text-sm">
            <div><dt className="text-muted text-[12.5px]">{t('Subject')}</dt><dd className="mt-0.5 break-all" dir="ltr">{m.cert.subject}</dd></div>
            <div><dt className="text-muted text-[12.5px]">{t('Issuer')}</dt><dd className="mt-0.5" dir="ltr">{m.cert.issuer}</dd></div>
            <div><dt className="text-muted text-[12.5px]">{t('Valid from')}</dt><dd className="mt-0.5 tnum">{fmtDate(m.cert.validFrom * 1000)}</dd></div>
            <div><dt className="text-muted text-[12.5px]">{t('Expires')}</dt><dd className="mt-0.5 tnum">{fmtDate(m.cert.validTo * 1000)} · {t('{n} days', { n: m.cert.daysRemaining })}</dd></div>
          </dl>
        </Card>
      )}

      {m.type === 'group' && (
        <Card>
          <CardHeader title={t('Members')} subtitle={t('{n} monitors in this group', { n: children.length })} icon={<Layers className="size-4" />} />
          <div className="px-2 pb-2">
            {children.length === 0 ? <p className="px-3 py-6 text-sm text-muted">{t('Add monitors to this group by choosing it as their group.')}</p> : children.map((c) => (
              <Link key={c.id} to={`/dashboard/${c.id}`} className="flex items-center gap-3 rounded-xl px-3 py-2.5 hover:bg-surface-2">
                <StatusPill status={c.maintenance ? 3 : c.last?.status} active={c.active} size="sm" />
                {monitorCountry(c) && <Flag cc={monitorCountry(c)} size={18} />}
                <span className="font-medium min-w-0 me-auto truncate" dir="auto">{c.name}</span>
                <span className="text-[13px] text-muted tnum">{c.last?.ping != null && c.last.status === 1 ? ms(c.last.ping) : ''}</span>
                <span className="text-[13px] tnum w-16 text-end">{pct(c.uptime24, 1)}</span>
              </Link>
            ))}
          </div>
        </Card>
      )}

      <Card>
        <CardHeader
          title={t('History')}
          actions={<Segmented size="sm" ariaLabel={t('History view')} value={evTab} onChange={setEvTab} options={[{ value: 'important', label: t('Events') }, { value: 'all', label: t('All checks') }]} />}
        />
        <div className="overflow-x-auto">
          <table className="w-full min-w-[560px] table-fixed text-sm">
            <thead>
              <tr className="text-[12px] uppercase tracking-wider text-subtle border-y border-line">
                <th className="font-semibold px-5 py-2.5 text-start w-32">{t('Status')}</th>
                <th className="font-semibold px-3 py-2.5 text-start w-44">{t('Time')}</th>
                <th className="font-semibold px-3 py-2.5 text-end w-28">{t('Response')}</th>
                <th className="font-semibold px-5 py-2.5 text-start">{t('Message')}</th>
              </tr>
            </thead>
            <tbody>
              {events === null ? (
                <tr><td colSpan={4} className="px-5 py-3"><Skeleton className="h-6" /></td></tr>
              ) : events.length === 0 ? (
                <tr><td colSpan={4} className="px-5 py-8 text-center text-muted">{t('Nothing recorded yet.')}</td></tr>
              ) : events.map((e) => (
                <tr key={e.id} className="border-b border-line last:border-0">
                  <td className="px-5 py-2.5"><StatusPill status={e.status} size="sm" /></td>
                  <td className="px-3 py-2.5 whitespace-nowrap tnum text-muted">{fmtDateTime(e.time)}</td>
                  <td className="px-3 py-2.5 text-end tnum whitespace-nowrap">{ms(e.ping)}</td>
                  <td className="px-5 py-2.5 text-muted"><div className="truncate" title={e.msg} dir="auto">{e.msg}</div></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {pages > 1 && <div className="flex justify-end px-5 py-3 border-t border-line"><Pagination page={page} pages={Math.min(pages, 500)} onPage={setPage} /></div>}
      </Card>
    </div>
  )
}
