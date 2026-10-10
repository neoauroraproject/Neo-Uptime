import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Cable, Copy, ExternalLink, Layers, LockKeyhole, Pause, Pencil, Play, RefreshCw, Server, Trash2, Webhook, Ellipsis, Eraser } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { clsx, duration, fmtDate, fmtDateTime, ms, pct, timeAgo } from '../lib/format'
import type { ChartPoint, Heartbeat, UptimeStat } from '../lib/types'
import { Badge, Button, Card, CardHeader, CopyButton, Empty, IconButton, Menu, Pagination, Segmented, Skeleton, StatusPill, TagChip, confirm, toast, useNow } from '../components/ui'
import { AreaChart, HeartbeatBar, beatsToPoints } from '../components/charts'
import { SystemPanel, TunnelDetails } from '../components/tunnel'
import { TypeIcon, typeInfo } from '../components/meta'
import { monitorState } from './Dashboard'

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
    <div className="card px-4 py-3.5">
      <p className="text-[12.5px] text-muted">{label}</p>
      <p className={clsx('mt-1 font-display text-[24px] leading-tight font-semibold tnum', tone)}>{value}</p>
      {sub && <p className="text-[11.5px] text-subtle mt-0.5">{sub}</p>}
    </div>
  )
}

function PushHelp({ token, interval }: { token: string; interval: number }) {
  const base = useApp((s) => s.settings.primaryBaseURL) || window.location.origin
  const url = `${base}/api/push/${token}?status=up&msg=OK&ping=`
  const curl = `curl -fsS "${url}"`
  const loop = `#!/bin/bash\n# Report to Neo Uptime every ${interval}s — run it as a systemd service or in cron.\nwhile true; do\n  curl -fsS -o /dev/null "${url}" || true\n  sleep ${interval}\ndone`
  return (
    <Card>
      <CardHeader title="Push URL" subtitle={`Call this at least every ${interval}s. Silence marks the monitor down. Same format as Uptime Kuma, so existing scripts work after changing the host.`} icon={<Webhook className="size-4" />} />
      <div className="px-5 pb-5 space-y-3">
        <div className="flex gap-2">
          <code className="flex-1 min-w-0 truncate rounded-xl bg-surface-2 border border-line px-3 py-2.5 font-mono text-[12.5px]">{url}</code>
          <CopyButton text={url} />
        </div>
        <details className="group">
          <summary className="cursor-pointer text-[13px] text-muted hover:text-fg select-none">Examples: curl, bash loop · <span className="text-subtle">status=down marks it down, ping= reports a latency</span></summary>
          <div className="mt-3 space-y-2">
            <div className="flex gap-2 items-start"><pre className="flex-1 min-w-0 overflow-x-auto rounded-xl bg-surface-2 border border-line px-3 py-2.5 font-mono text-[12px]">{curl}</pre><CopyButton text={curl} /></div>
            <div className="flex gap-2 items-start"><pre className="flex-1 min-w-0 overflow-x-auto rounded-xl bg-surface-2 border border-line px-3 py-2.5 font-mono text-[12px] leading-relaxed">{loop}</pre><CopyButton text={loop} /></div>
          </div>
        </details>
      </div>
    </Card>
  )
}

export default function MonitorDetail() {
  const { id: idStr } = useParams()
  const id = Number(idStr)
  const nav = useNavigate()
  const m = useApp((s) => s.monitors[id])
  const all = useApp((s) => s.monitors)
  const beats = useApp((s) => s.beats[id]) ?? []
  const loaded = useApp((s) => s.loaded)
  const role = useApp((s) => s.role)
  const panels = useApp((s) => s.panels)
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
    if (range === 'recent') return
    setChart(null)
    api.get<ChartPoint[]>(`/api/monitors/${id}/chart?range=${range}`).then(setChart).catch(() => setChart([]))
  }, [id, range, minute])

  useEffect(() => {
    api.get<Record<string, UptimeStat>>(`/api/monitors/${id}/stats`).then(setStats).catch(() => {})
  }, [id, minute, m?.last?.important])

  useEffect(() => {
    setPage(1)
  }, [id, evTab])

  useEffect(() => {
    api.get<{ items: Heartbeat[]; total: number }>(`/api/monitors/${id}/heartbeats?limit=${per}&offset=${(page - 1) * per}${evTab === 'important' ? '&important=1' : ''}`)
      .then((r) => { setEvents(r.items); setEvTotal(r.total) })
      .catch(() => setEvents([]))
  }, [id, page, evTab, evTab === 'all' ? lastTime : m?.last?.important ? lastTime : 0])

  const recentPoints = useMemo(() => beatsToPoints(beats), [beats])
  const children = useMemo(() => Object.values(all).filter((x) => x.parentId === id), [all, id])

  if (!m) {
    if (!loaded) return <div className="p-8 space-y-4"><Skeleton className="h-10 w-72" /><Skeleton className="h-40" /><Skeleton className="h-72" /></div>
    return <Empty title="Monitor not found" body="It may have been deleted." action={<Button onClick={() => nav('/dashboard')}>Back to dashboard</Button>} />
  }

  const state = monitorState(m)
  const editable = canEdit(role)
  const isTunnel = m.type === 'backpack-tunnel'
  const panel = m.config.panelId ? panels.find((p) => p.id === m.config.panelId) : undefined

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    try {
      await fn()
      toast.success(ok)
      await useApp.getState().loadDashboard()
    } catch (e) {
      toast.error('That did not work', (e as Error).message)
    }
  }

  const del = async () => {
    if (!(await confirm({ title: `Delete “${m.name}”?`, body: 'Its whole history goes with it. This cannot be undone.', confirmLabel: 'Delete monitor', danger: true }))) return
    try {
      await api.del(`/api/monitors/${id}`)
      useApp.getState().removeMonitor(id)
      toast.success('Monitor deleted')
      nav('/dashboard')
    } catch (e) {
      toast.error('Could not delete', (e as Error).message)
    }
  }

  const s24 = stats?.['24h']
  const curPing = m.last?.status === 1 ? m.last.ping : null
  const pages = Math.ceil(evTotal / per)

  return (
    <div className="@container p-4 sm:p-6 lg:p-8 space-y-5 max-w-[1300px] fade-up" key={id}>
      {/* header */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-[13px] text-muted">
            <TypeIcon type={m.type} className="size-4" />
            {typeInfo[m.type]?.label}
            {m.parentId && all[m.parentId] && (
              <>
                <span className="text-subtle">in</span>
                <Link to={`/dashboard/${m.parentId}`} className="inline-flex items-center gap-1 hover:text-fg"><Layers className="size-3.5" />{all[m.parentId].name}</Link>
              </>
            )}
          </div>
          <h1 className="mt-1.5 font-display text-[30px] leading-tight font-semibold tracking-tight break-words">{m.name}</h1>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <StatusPill status={m.maintenance ? 3 : m.last?.status} active={m.active} size="lg" />
            {m.target && (m.target.startsWith('http') ? (
              <a href={m.target} target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-1.5 text-sm text-accent-ink hover:underline break-all">
                {m.target}<ExternalLink className="size-3.5 shrink-0" />
              </a>
            ) : <span className="text-sm text-muted font-mono">{m.target}</span>)}
            {panel && <span className="text-sm text-muted">via <Link className="hover:text-fg" to="/tunnels/panels">{panel.name}</Link></span>}
            {m.tags.map((t) => <TagChip key={`${t.tagId}${t.value}`} {...t} />)}
          </div>
          {m.description && <p className="mt-3 text-sm text-muted max-w-2xl">{m.description}</p>}
        </div>
        {editable && (
          <div className="flex items-center gap-2">
            {m.active ? (
              <Button icon={<Pause className="size-4" />} onClick={async () => {
                if (await confirm({ title: 'Pause this monitor?', body: 'No checks run and no alerts are sent until you resume it.', confirmLabel: 'Pause' })) act(() => api.post(`/api/monitors/${id}/pause`), 'Monitor paused')
              }}>Pause</Button>
            ) : (
              <Button variant="primary" icon={<Play className="size-4" />} onClick={() => act(() => api.post(`/api/monitors/${id}/resume`), 'Monitor resumed')}>Resume</Button>
            )}
            <Button icon={<Pencil className="size-4" />} onClick={() => nav(`/edit/${id}`)}>Edit</Button>
            <Menu
              trigger={(p) => <IconButton {...p} label="More actions"><Ellipsis className="size-5" /></IconButton>}
              items={[
                { label: 'Check now', icon: <RefreshCw />, onClick: () => act(() => api.post(`/api/monitors/${id}/check`), 'Check started'), disabled: !m.active },
                { label: 'Clone', icon: <Copy />, onClick: () => nav(`/clone/${id}`) },
                { divider: true, label: '' },
                { label: 'Clear events', icon: <Eraser />, onClick: async () => { if (await confirm({ title: 'Clear events?', body: 'Removes the important-event marks; heartbeats stay.', confirmLabel: 'Clear' })) act(() => api.del(`/api/monitors/${id}/events`), 'Events cleared') } },
                { label: 'Clear heartbeats', icon: <Eraser />, onClick: async () => { if (await confirm({ title: 'Clear heartbeats?', body: 'Deletes this monitor’s raw check history. Uptime percentages are kept.', confirmLabel: 'Clear', danger: true })) act(() => api.del(`/api/monitors/${id}/heartbeats`), 'Heartbeats cleared') } },
                { divider: true, label: '' },
                { label: 'Delete', icon: <Trash2 />, danger: true, onClick: del },
              ]}
            />
          </div>
        )}
      </div>

      {/* heartbeat bar */}
      <Card className="px-5 py-4">
        <div className="flex items-center justify-between text-[13px] mb-3">
          <span className="text-muted">Last {Math.min(beats.length, 100)} checks</span>
          <span className="text-subtle tnum">
            {m.active ? (m.last ? `checked ${timeAgo(m.last.time, now)} · every ${duration(m.interval)}` : 'waiting for the first check…') : 'paused'}
          </span>
        </div>
        <HeartbeatBar beats={beats} height={34} barWidth={7} gap={3} maxBeats={100} newestId={m.last?.id} showLabels />
        {m.last?.msg && <p className={clsx('mt-3 text-[13px] break-words', state === 'down' ? 'text-down' : 'text-muted')}>{m.last.msg}</p>}
      </Card>

      {/* metrics */}
      <div className="grid grid-cols-2 @2xl:grid-cols-3 @5xl:grid-cols-6 gap-3">
        <Metric label="Response" value={ms(curPing)} sub="current" />
        <Metric label="Average" value={ms(s24?.avgPing ?? m.avgPing24)} sub="last 24 hours" />
        <Metric label="Uptime" value={pct(stats?.['24h']?.ratio ?? m.uptime24)} sub="24 hours" tone={(stats?.['24h']?.ratio ?? 1) < 0.95 && (stats?.['24h']?.ratio ?? 1) >= 0 ? 'text-down' : undefined} />
        <Metric label="Uptime" value={pct(stats?.['30d']?.ratio ?? m.uptime30)} sub="30 days" />
        <Metric label="Uptime" value={pct(stats?.['1y']?.ratio)} sub="1 year" />
        {m.cert ? (
          <Metric label="Certificate" value={`${m.cert.daysRemaining} days`} sub={`expires ${fmtDate(m.cert.validTo * 1000)}`} tone={m.cert.daysRemaining <= 7 ? 'text-down' : m.cert.daysRemaining <= 21 ? 'text-pending' : undefined} />
        ) : (
          <Metric label="Checks" value={(stats?.['24h']?.checks ?? 0).toLocaleString()} sub="last 24 hours" />
        )}
      </div>

      {/* chart */}
      {m.type !== 'group' && (
        <Card>
          <CardHeader
            title="Response time"
            subtitle={range === 'recent' ? 'Updates live with every check' : 'Averaged per slot; red bands are failed checks'}
            actions={<Segmented size="sm" ariaLabel="Chart range" value={range} onChange={setRange} options={ranges.map((r) => ({ value: r.value, label: r.label }))} />}
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

      {isTunnel && m.tunnel && (
        <Card>
          <CardHeader title="Tunnel" subtitle={`As reported by ${panel?.name ?? 'the BackPack panel'}`} icon={<Cable className="size-4" />} />
          <div className="px-5 pb-5"><TunnelDetails t={m.tunnel} /></div>
        </Card>
      )}

      {m.type === 'backpack-panel' && m.system && (
        <Card>
          <CardHeader title="Panel host" subtitle="CPU, memory and disk of the server running BackPack" icon={<Server className="size-4" />} />
          <div className="px-5 pb-5"><SystemPanel s={m.system} /></div>
        </Card>
      )}

      {m.cert && (
        <Card>
          <CardHeader title="TLS certificate" icon={<LockKeyhole className="size-4" />} actions={<Badge tone={m.cert.valid ? 'up' : 'down'}>{m.cert.valid ? 'Valid' : 'Invalid'}</Badge>} />
          <dl className="px-5 pb-5 grid @xl:grid-cols-2 @4xl:grid-cols-4 gap-4 text-sm">
            <div><dt className="text-muted text-[12.5px]">Subject</dt><dd className="mt-0.5 break-all">{m.cert.subject}</dd></div>
            <div><dt className="text-muted text-[12.5px]">Issuer</dt><dd className="mt-0.5">{m.cert.issuer}</dd></div>
            <div><dt className="text-muted text-[12.5px]">Valid from</dt><dd className="mt-0.5 tnum">{fmtDate(m.cert.validFrom * 1000)}</dd></div>
            <div><dt className="text-muted text-[12.5px]">Expires</dt><dd className="mt-0.5 tnum">{fmtDate(m.cert.validTo * 1000)} · {m.cert.daysRemaining} days</dd></div>
          </dl>
        </Card>
      )}

      {m.type === 'group' && (
        <Card>
          <CardHeader title="Members" subtitle={`${children.length} monitor${children.length === 1 ? '' : 's'} in this group`} icon={<Layers className="size-4" />} />
          <div className="px-2 pb-2">
            {children.length === 0 ? <p className="px-3 py-6 text-sm text-muted">Add monitors to this group by choosing it as their group.</p> : children.map((c) => (
              <Link key={c.id} to={`/dashboard/${c.id}`} className="flex items-center gap-3 rounded-xl px-3 py-2.5 hover:bg-surface-2">
                <StatusPill status={c.maintenance ? 3 : c.last?.status} active={c.active} size="sm" />
                <span className="font-medium flex-1 truncate">{c.name}</span>
                <span className="text-[13px] text-muted tnum">{c.last?.ping != null && c.last.status === 1 ? ms(c.last.ping) : ''}</span>
                <span className="text-[13px] tnum w-16 text-right">{pct(c.uptime24, 1)}</span>
              </Link>
            ))}
          </div>
        </Card>
      )}

      {/* events */}
      <Card>
        <CardHeader
          title="History"
          actions={<Segmented size="sm" ariaLabel="History view" value={evTab} onChange={setEvTab} options={[{ value: 'important', label: 'Events' }, { value: 'all', label: 'All checks' }]} />}
        />
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-[12px] uppercase tracking-wider text-subtle border-y border-line">
                <th className="font-semibold px-5 py-2.5">Status</th>
                <th className="font-semibold px-3 py-2.5">Time</th>
                <th className="font-semibold px-3 py-2.5 text-right">Response</th>
                <th className="font-semibold px-5 py-2.5">Message</th>
              </tr>
            </thead>
            <tbody>
              {events === null ? (
                <tr><td colSpan={4} className="px-5 py-3"><Skeleton className="h-6" /></td></tr>
              ) : events.length === 0 ? (
                <tr><td colSpan={4} className="px-5 py-8 text-center text-muted">Nothing recorded yet.</td></tr>
              ) : events.map((e) => (
                <tr key={e.id} className="border-b border-line last:border-0">
                  <td className="px-5 py-2.5"><StatusPill status={e.status} size="sm" /></td>
                  <td className="px-3 py-2.5 whitespace-nowrap tnum text-muted">{fmtDateTime(e.time)}</td>
                  <td className="px-3 py-2.5 text-right tnum whitespace-nowrap">{ms(e.ping)}</td>
                  <td className="px-5 py-2.5 text-muted max-w-lg truncate" title={e.msg}>{e.msg}</td>
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
