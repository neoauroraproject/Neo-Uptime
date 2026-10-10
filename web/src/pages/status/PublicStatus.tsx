import { useEffect, useMemo, useRef, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { ArrowDown, ArrowUp, ChevronDown, CircleAlert, CircleCheck, CircleX, Info, TriangleAlert, Wrench } from 'lucide-react'
import { api, ApiError } from '../../lib/api'
import { bytes, clsx, fmtDateTime, fmtWeekdayTime, ms, pct, statusKey, timeAgo } from '../../lib/format'
import type { ChartPoint, Theme } from '../../lib/types'
import { AreaChart, Columns, DayBars, HeartbeatBar, Sparkline, type DayBucket } from '../../components/charts'
import { TunnelBadges } from '../../components/tunnel'
import { Spinner, TagChip, useNow } from '../../components/ui'

interface PubBeat { s: 0 | 1 | 2 | 3; t: number; p?: number }
interface PubTunnel { state: string; direction: string; carrier: string; preset?: string; tunnelPort?: string; uptime?: string; inBytes: number; outBytes: number; totalBytes: number; peerCountry?: string; peerLocation?: string; kcpLoss?: number }
interface PubMonitor {
  id: number; name: string; type: string; url?: string; tags?: { tagId: number; name: string; color: string; value: string }[]
  status: number; ping: number | null; uptime24: number; uptime30: number; avgPing: number; certDays?: number; tunnel?: PubTunnel
  beats: PubBeat[]; days?: DayBucket[]
}
interface PubPage {
  slug: string; title: string; description: string; theme: Theme; layout: 'list' | 'cards'; barMode: 'heartbeat' | 'days'
  accentColor: string; logo: string; footer: string; customCss: string; showTags: boolean; showCertExpiry: boolean
  showPoweredBy: boolean; showCharts: boolean; refreshInterval: number; published: boolean
}
interface PubData {
  page: PubPage
  groups: { name: string; monitors: PubMonitor[] }[]
  overall: 'operational' | 'partial' | 'major' | 'maintenance' | 'unknown'
  incidents: { id: number; title: string; content: string; style: string; createdAt: number; updatedAt: number }[]
  maintenance: { title: string; description: string; status: string; start?: number; end?: number }[]
  updatedAt: number
}

const overallText = {
  operational: 'All systems operational',
  partial: 'Partial outage',
  major: 'Major outage',
  maintenance: 'Under maintenance',
  unknown: 'Waiting for data',
}
const overallColor = { operational: 'var(--up)', partial: 'var(--pending)', major: 'var(--down)', maintenance: 'var(--maint)', unknown: 'var(--text-3)' }

function Linkify({ text }: { text: string }) {
  const parts = text.split(/(https?:\/\/[^\s)]+)/g)
  return <>{parts.map((p, i) => (/^https?:\/\//.test(p) ? <a key={i} href={p} target="_blank" rel="noreferrer noopener" className="underline underline-offset-2">{p}</a> : <span key={i}>{p}</span>))}</>
}

function Banner({ d, theme }: { d: PubData; theme: Theme }) {
  const all = d.groups.flatMap((g) => g.monitors)
  const ups = all.filter((m) => m.uptime30 >= 0)
  const avgUp = ups.length ? ups.reduce((a, m) => a + m.uptime30, 0) / ups.length : -1
  const online = all.filter((m) => m.status === 1).length
  const Icon = d.overall === 'operational' ? CircleCheck : d.overall === 'maintenance' ? Wrench : d.overall === 'unknown' ? Info : d.overall === 'major' ? CircleX : TriangleAlert
  const color = overallColor[d.overall]

  if (theme === 'lime' || theme === 'noir') {
    const bg = d.overall === 'operational' ? 'var(--accent)' : color
    return (
      <section className="grid md:grid-cols-[1.4fr_1fr] gap-4">
        <div className="rounded-[28px] p-7 sm:p-8 flex flex-col justify-between min-h-[220px]" style={{ background: bg, color: d.overall === 'operational' ? 'var(--on-accent)' : '#fff' }}>
          <div className="flex items-center justify-between">
            <span className="inline-flex items-center gap-2 h-8 px-3 rounded-full bg-black/10 text-[13px] font-semibold"><Icon className="size-4" />Status</span>
            <span className="text-[13px] font-medium opacity-80">{online}/{all.length} online</span>
          </div>
          <p className="font-display text-[40px] sm:text-[52px] leading-[1] font-semibold tracking-tight mt-8">{overallText[d.overall]}</p>
        </div>
        <div className="rounded-[28px] bg-surface border border-line p-7 flex flex-col justify-between" style={{ boxShadow: 'var(--shadow-card)' }}>
          <div className="flex items-center justify-between text-[13px] text-muted"><span>Uptime · 30 days</span><span className="inline-flex items-center h-7 px-2.5 rounded-full bg-accent-soft text-accent-ink font-semibold">{all.length} services</span></div>
          <p className="font-display text-[56px] leading-none font-semibold tnum tracking-tight mt-6">{avgUp < 0 ? '—' : (avgUp * 100).toFixed(2)}<span className="text-2xl text-muted">%</span></p>
        </div>
      </section>
    )
  }
  if (theme === 'clarity') {
    return (
      <section className="rounded-2xl border-2 px-6 py-5 flex items-center gap-4" style={{ borderColor: d.overall === 'operational' ? 'var(--banner-border)' : color, background: d.overall === 'operational' ? 'var(--banner-bg)' : `color-mix(in srgb, ${color} 10%, var(--surface))`, boxShadow: 'var(--shadow-card)' }}>
        <span className="grid place-items-center size-9 rounded-full text-white" style={{ background: d.overall === 'operational' ? 'var(--up)' : color }}><Icon className="size-5" /></span>
        <p className="font-display text-2xl font-semibold tracking-tight">{overallText[d.overall]}</p>
      </section>
    )
  }
  return (
    <section className="relative overflow-hidden rounded-3xl border px-6 sm:px-8 py-7" style={{ borderColor: `color-mix(in srgb, ${color} 35%, transparent)`, background: `linear-gradient(135deg, color-mix(in srgb, ${color} 16%, transparent), color-mix(in srgb, ${color} 3%, transparent))` }}>
      <div className="absolute inset-0 dot-grid opacity-40 [mask-image:linear-gradient(to_left,black,transparent)]" aria-hidden />
      <div className="relative flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-4">
          <span className="relative grid place-items-center size-12 rounded-2xl" style={{ background: `color-mix(in srgb, ${color} 18%, transparent)`, color }}><Icon className="size-6" /></span>
          <div>
            <p className="font-display text-[26px] font-semibold tracking-tight">{overallText[d.overall]}</p>
            <p className="text-sm text-muted">{online} of {all.length} services online</p>
          </div>
        </div>
        <div className="text-right">
          <p className="text-[12px] uppercase tracking-wider text-subtle font-semibold">30-day uptime</p>
          <p className="font-display text-3xl font-semibold tnum">{pct(avgUp)}</p>
        </div>
      </div>
    </section>
  )
}

const incStyle: Record<string, { color: string; icon: typeof Info }> = {
  info: { color: 'var(--maint)', icon: Info },
  warning: { color: 'var(--pending)', icon: TriangleAlert },
  danger: { color: 'var(--down)', icon: CircleAlert },
  success: { color: 'var(--up)', icon: CircleCheck },
  maintenance: { color: 'var(--maint)', icon: Wrench },
}

function ChartBlock({ slug, id, theme }: { slug: string; id: number; theme: Theme }) {
  const [range, setRange] = useState<'day' | 'week' | 'month'>('day')
  const [pts, setPts] = useState<ChartPoint[] | null>(null)
  useEffect(() => {
    setPts(null)
    api.get<ChartPoint[]>(`/api/status-page/${slug}/chart/${id}?range=${range}`).then(setPts).catch(() => setPts([]))
  }, [slug, id, range])
  const span = { day: 86400e3, week: 7 * 86400e3, month: 30 * 86400e3 }[range]
  const bento = theme === 'lime' || theme === 'noir'
  return (
    <div className="pt-4">
      <div className="flex items-center justify-between mb-2">
        <span className="text-[13px] text-muted">Response time</span>
        {bento ? (
          <div className="flex gap-1.5">
            {(['day', 'week', 'month'] as const).map((r) => (
              <button key={r} onClick={() => setRange(r)} className={clsx('h-8 px-3.5 rounded-full text-[13px] font-medium border transition-colors capitalize', range === r ? 'bg-accent text-on-accent border-transparent' : 'border-line-strong text-muted hover:text-fg')}>{r}</button>
            ))}
          </div>
        ) : (
          <div className="flex items-center gap-2 text-[13px]">
            {(['day', 'week', 'month'] as const).map((r, i) => (
              <span key={r} className="flex items-center gap-2">
                {i > 0 && <span className="text-subtle">|</span>}
                <button onClick={() => setRange(r)} className={clsx('capitalize transition-colors', range === r ? 'text-fg font-semibold' : 'text-subtle hover:text-muted')}>{r}</button>
              </span>
            ))}
          </div>
        )}
      </div>
      {pts === null ? <div className="h-[180px] grid place-items-center"><Spinner /></div> : bento ? (
        pts.length ? (
          <Columns
            values={downsampleCols(pts, 28)}
            highlight={[downsampleCols(pts, 28).length - 1]}
            height={150}
          />
        ) : <p className="h-[150px] grid place-items-center text-sm text-muted">No data yet</p>
      ) : (
        <AreaChart points={pts} height={180} rangeMs={span} variant={theme === 'clarity' ? 'line' : 'area'} />
      )}
    </div>
  )
}

function downsampleCols(pts: ChartPoint[], n: number) {
  const vals = pts.map((p) => p.ping).filter((v): v is number => v != null)
  if (vals.length <= n) return vals
  const size = vals.length / n
  return Array.from({ length: n }, (_, i) => {
    const slice = vals.slice(Math.floor(i * size), Math.floor((i + 1) * size))
    return slice.reduce((a, b) => a + b, 0) / Math.max(1, slice.length)
  })
}

function statusWord(s: number) {
  return s === -2 ? 'Paused' : s === -1 ? 'No data' : s === 1 ? 'Operational' : s === 0 ? 'Down' : s === 3 ? 'Maintenance' : 'Degraded'
}

function StatusLabel({ s }: { s: number }) {
  const k = s < 0 ? 'paused' : statusKey(s as 0 | 1 | 2 | 3)
  const color = { up: 'var(--up)', down: 'var(--down)', pending: 'var(--pending)', maint: 'var(--maint)', paused: 'var(--text-3)', empty: 'var(--text-3)' }[k]
  return <span className="inline-flex items-center gap-1.5 text-[13px] font-medium" style={{ color }}><span className="size-2 rounded-full" style={{ background: color }} />{statusWord(s)}</span>
}

function MonitorRow({ m, page, theme }: { m: PubMonitor; page: PubPage; theme: Theme }) {
  const [open, setOpen] = useState(false)
  const days = page.barMode === 'days' && m.days
  return (
    <div className="px-5 sm:px-6 py-4">
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium text-[15px]">{m.name}</span>
            {m.tags?.map((t) => <TagChip key={`${t.tagId}${t.value}`} {...t} />)}
            {page.showCertExpiry && m.certDays != null && (
              <span className={clsx('text-[12px] tnum', m.certDays <= 14 ? 'text-pending' : 'text-subtle')}>cert {m.certDays}d</span>
            )}
          </div>
          {m.url && <p className="text-[12.5px] text-muted truncate font-mono mt-0.5">{m.url}</p>}
        </div>
        <StatusLabel s={m.status} />
        <span className="hidden sm:inline w-20 text-right text-[13px] font-semibold tnum">{pct(m.uptime24 >= 0 ? (days ? m.uptime30 : m.uptime24) : -1, 2)}</span>
      </div>
      <div className="mt-3">
        {days ? <DayBars days={m.days!} height={30} /> : <HeartbeatBar beats={m.beats.map((b) => ({ status: b.s, time: b.t, ping: b.p }))} height={30} barWidth={theme === 'clarity' ? 5 : 6} gap={theme === 'clarity' ? 3 : 3} maxBeats={100} />}
        <div className="mt-1.5 flex items-center justify-between text-[11.5px] text-subtle">
          <span>{days ? '90 days ago' : m.beats.length ? timeAgo(m.beats[0].t) : ''}</span>
          {page.showCharts ? (
            <button onClick={() => setOpen(!open)} aria-expanded={open} className="inline-flex items-center gap-1 text-muted hover:text-fg">
              {m.avgPing >= 0 ? `avg ${ms(m.avgPing)}` : 'Response time'}<ChevronDown className={clsx('size-3.5 transition-transform', open && 'rotate-180')} />
            </button>
          ) : <span className="tnum">{m.avgPing >= 0 ? `avg ${ms(m.avgPing)}` : ''}</span>}
          <span>{days ? 'Today' : 'Now'}</span>
        </div>
      </div>
      {open && <ChartBlock slug={page.slug} id={m.id} theme={theme} />}
    </div>
  )
}

function MonitorCardPub({ m, page, theme }: { m: PubMonitor; page: PubPage; theme: Theme }) {
  const bento = theme === 'lime' || theme === 'noir'
  const pings = m.beats.slice(-40).map((b) => (b.s === 1 ? b.p ?? null : null))
  const cur = m.status === 1 ? m.ping : null
  return (
    <article className={clsx('bg-surface border border-line flex flex-col overflow-hidden', bento ? 'rounded-[28px]' : 'rounded-[20px]')} style={{ boxShadow: 'var(--shadow-card)' }}>
      <div className="p-5 pb-0">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <p className="font-display text-lg font-semibold leading-tight truncate">{m.name}</p>
            {m.tunnel?.peerLocation ? <p className="text-[12.5px] text-muted truncate">{m.tunnel.peerLocation}</p> : m.url ? <p className="text-[12.5px] text-muted truncate font-mono">{m.url}</p> : null}
          </div>
          <StatusLabel s={m.status} />
        </div>
        {m.tunnel && <TunnelBadges t={m.tunnel} className="mt-3" />}
      </div>
      <div className="px-5 pt-4 grid grid-cols-[auto_1fr] items-end gap-4">
        <div>
          <p className="font-display text-[44px] leading-none font-semibold tnum tracking-tight whitespace-nowrap">{cur != null ? Math.round(cur) : '—'}{cur != null && <span className="text-base text-muted ml-1">ms</span>}</p>
          <p className="mt-2 text-[12.5px] text-muted tnum">{m.tunnel ? `${bytes(m.tunnel.totalBytes)}${m.tunnel.uptime ? ` · up ${m.tunnel.uptime}` : ''}` : `avg ${ms(m.avgPing)}`}</p>
        </div>
        {bento ? (
          <Columns values={pings.map((p) => p ?? 0).slice(-16)} highlight={[15]} height={72} />
        ) : (
          <Sparkline values={pings} height={72} color={m.status === 0 ? 'var(--down)' : 'var(--chart)'} />
        )}
      </div>
      <div className="mt-4 px-5 py-4 border-t border-line space-y-2">
        {page.barMode === 'days' && m.days ? <DayBars days={m.days} height={18} /> : <HeartbeatBar beats={m.beats.map((b) => ({ status: b.s, time: b.t, ping: b.p }))} height={18} barWidth={4} gap={2} maxBeats={60} />}
        <div className="flex items-center justify-between text-[12.5px]">
          {m.tunnel ? (
            <span className="flex gap-3 text-muted tnum">
              <span className="inline-flex items-center gap-1"><ArrowDown className="size-3.5" />{bytes(m.tunnel.inBytes)}</span>
              <span className="inline-flex items-center gap-1"><ArrowUp className="size-3.5" />{bytes(m.tunnel.outBytes)}</span>
            </span>
          ) : <span className="text-muted">Uptime 24h</span>}
          <span className="font-semibold tnum">{pct(m.uptime24, 2)}</span>
        </div>
      </div>
    </article>
  )
}

const themeBg: Record<Theme, string> = { aurora: '#050806', clarity: '#f5f6f8', lime: '#efefec', noir: '#0a0a0a' }

export default function PublicStatus() {
  const { slug = '' } = useParams()
  const [params] = useSearchParams()
  const [d, setD] = useState<PubData | null>(null)
  const [err, setErr] = useState('')
  const [fetchedAt, setFetchedAt] = useState(Date.now())
  const now = useNow(1000)
  const timer = useRef<ReturnType<typeof setTimeout>>()

  const preview = params.get('preview') === '1'
  const theme = (preview && (params.get('theme') as Theme)) || d?.page.theme || 'aurora'
  const layout = (preview && (params.get('layout') as PubPage['layout'])) || d?.page.layout || 'list'

  useEffect(() => {
    let alive = true
    const load = () => {
      api.get<PubData>(`/api/status-page/${encodeURIComponent(slug)}`, { quiet401: true })
        .then((r) => {
          if (!alive) return
          setD(r)
          setErr('')
          setFetchedAt(Date.now())
          document.title = `${r.page.title} — Status`
          timer.current = setTimeout(load, Math.max(10, r.page.refreshInterval) * 1000)
        })
        .catch((e: ApiError) => {
          if (!alive) return
          setErr(e.status === 404 ? 'This status page does not exist.' : e.message)
          timer.current = setTimeout(load, 30000)
        })
    }
    load()
    return () => {
      alive = false
      clearTimeout(timer.current)
    }
  }, [slug])

  useEffect(() => {
    const prev = document.body.style.background
    document.body.style.background = themeBg[theme]
    document.documentElement.setAttribute('data-theme', theme === 'clarity' || theme === 'lime' ? 'light' : 'dark')
    return () => { document.body.style.background = prev }
  }, [theme])

  const page = d?.page
  const accentStyle = useMemo(() => (page?.accentColor ? ({ '--accent': page.accentColor, '--chart': page.accentColor } as React.CSSProperties) : undefined), [page?.accentColor])

  if (err && !d) {
    return (
      <div className="sp min-h-dvh grid place-items-center bg-bg text-fg p-6" data-sp-theme={theme}>
        <div className="text-center">
          <p className="font-display text-6xl font-semibold text-subtle">404</p>
          <p className="mt-3 text-muted">{err}</p>
        </div>
      </div>
    )
  }
  if (!d || !page) {
    return <div className="sp min-h-dvh grid place-items-center bg-bg" data-sp-theme={theme}><Spinner className="size-6" /></div>
  }

  const left = Math.max(0, page.refreshInterval - Math.floor((now - fetchedAt) / 1000))
  const bento = theme === 'lime' || theme === 'noir'

  return (
    <div className="sp min-h-dvh bg-bg text-fg" data-sp-theme={theme} style={accentStyle}>
      {page.customCss && <style>{page.customCss}</style>}
      {theme === 'aurora' && <div className="pointer-events-none fixed inset-0 [background:radial-gradient(900px_420px_at_50%_-10%,rgb(34_197_94/0.12),transparent_70%)]" aria-hidden />}
      <div className={clsx('relative mx-auto px-4 sm:px-6 py-8 sm:py-12 space-y-6', layout === 'cards' ? 'max-w-6xl' : 'max-w-4xl')}>
        {!page.published && <div className="rounded-xl border border-pending/40 bg-pending/10 px-4 py-2 text-[13px] text-pending">Draft — only signed-in users can see this page.</div>}
        <header className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-4 min-w-0">
            {page.logo && <img src={page.logo} alt="" className="size-12 rounded-2xl object-contain bg-surface border border-line" />}
            <div className="min-w-0">
              <h1 className={clsx('font-display font-semibold tracking-tight', bento ? 'text-[34px]' : 'text-[28px]')}>{page.title}</h1>
              {page.description && <p className="text-[14.5px] text-muted mt-0.5 whitespace-pre-line">{page.description}</p>}
            </div>
          </div>
          <div className={clsx('text-[12.5px] text-muted tnum', bento && 'inline-flex items-center gap-2 h-9 px-4 rounded-full bg-surface border border-line')}>
            Updated {timeAgo(d.updatedAt, now)} · refresh in {left}s
          </div>
        </header>

        <Banner d={d} theme={theme} />

        {d.maintenance.map((m, i) => (
          <section key={`m${i}`} className="rounded-2xl border px-5 py-4 bg-surface" style={{ borderColor: 'color-mix(in srgb, var(--maint) 40%, transparent)' }}>
            <div className="flex items-center gap-2 font-semibold" style={{ color: 'var(--maint)' }}>
              <Wrench className="size-4" />{m.status === 'under-maintenance' ? 'Maintenance in progress' : 'Scheduled maintenance'} · <span className="text-fg">{m.title}</span>
            </div>
            {m.description && <p className="mt-1.5 text-sm text-muted whitespace-pre-line"><Linkify text={m.description} /></p>}
            {m.start && <p className="mt-1.5 text-[12.5px] text-subtle tnum">{fmtWeekdayTime(m.start)}{m.end ? ` → ${fmtWeekdayTime(m.end)}` : ''}</p>}
          </section>
        ))}

        {d.incidents.map((inc) => {
          const st = incStyle[inc.style] ?? incStyle.info
          return (
            <section key={inc.id} className="rounded-2xl border px-5 py-4 bg-surface" style={{ borderColor: `color-mix(in srgb, ${st.color} 40%, transparent)`, boxShadow: 'var(--shadow-card)' }}>
              <div className="flex items-start gap-3">
                <st.icon className="size-5 shrink-0 mt-0.5" style={{ color: st.color }} />
                <div className="min-w-0">
                  <p className="font-semibold">{inc.title}</p>
                  {inc.content && <p className="mt-1 text-sm text-muted whitespace-pre-line leading-relaxed"><Linkify text={inc.content} /></p>}
                  <p className="mt-2 text-[12px] text-subtle">{fmtDateTime(inc.updatedAt * 1000)}</p>
                </div>
              </div>
            </section>
          )
        })}

        {d.groups.length === 0 && <p className="py-16 text-center text-muted">Nothing is published on this page yet.</p>}

        {d.groups.map((g, gi) => (
          <section key={gi} className="space-y-3">
            {g.name && <h2 className={clsx('font-display font-semibold tracking-tight', bento ? 'text-2xl px-1' : 'text-lg px-1')}>{g.name}</h2>}
            {layout === 'cards' ? (
              <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-4">
                {g.monitors.map((m) => <MonitorCardPub key={m.id} m={m} page={page} theme={theme} />)}
              </div>
            ) : (
              <div className={clsx('bg-surface border border-line divide-y divide-line overflow-hidden', bento ? 'rounded-[28px]' : 'rounded-[20px]')} style={{ boxShadow: 'var(--shadow-card)' }}>
                {g.monitors.length === 0 ? <p className="px-6 py-6 text-sm text-muted">No services in this group.</p> : g.monitors.map((m) => <MonitorRow key={m.id} m={m} page={page} theme={theme} />)}
              </div>
            )}
          </section>
        ))}

        <footer className="pt-6 pb-2 flex flex-col sm:flex-row items-center justify-between gap-3 text-[12.5px] text-subtle">
          <div className="whitespace-pre-line text-center sm:text-left">{page.footer && <Linkify text={page.footer} />}</div>
          {page.showPoweredBy && (
            <a href="https://github.com/neoauroraproject/Neo-Uptime" target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-2 hover:text-fg">
              <svg viewBox="0 0 64 64" className="size-4" aria-hidden><path d="M10 36h10l5-12 8 22 6-16 4 6h11" fill="none" stroke="currentColor" strokeWidth="6" strokeLinecap="round" strokeLinejoin="round" /></svg>
              Powered by Neo Uptime
            </a>
          )}
        </footer>
      </div>
    </div>
  )
}
