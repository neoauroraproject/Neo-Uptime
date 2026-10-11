import { useEffect, useMemo, useRef, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { ArrowDown, ArrowUp, ChevronDown, CircleAlert, CircleCheck, CircleX, Info, TriangleAlert, Wrench } from 'lucide-react'
import { api, ApiError } from '../../lib/api'
import { bytes, clsx, fmtDateTime, fmtWeekdayTime, ms, n, pct, statusKey, timeAgo } from '../../lib/format'
import { useLang, useT, type Lang } from '../../lib/i18n'
import type { ChartPoint, Theme } from '../../lib/types'
import { AreaChart, Columns, DayBars, HeartbeatBar, Sparkline, type DayBucket } from '../../components/charts'
import { TunnelBadges } from '../../components/tunnel'
import { Spinner, TagChip, useNow } from '../../components/ui'
import { Flag } from '../../components/flag'

interface PubBeat { s: 0 | 1 | 2 | 3; t: number; p?: number }
interface PubTunnel { source?: string; state: string; direction: string; carrier: string; preset?: string; tunnelPort?: string; uptime?: string; inBytes: number; outBytes: number; totalBytes: number; peerCountry?: string; peerLocation?: string; kcpLoss?: number }
interface PubMonitor {
  id: number; name: string; type: string; country?: string; url?: string; tags?: { tagId: number; name: string; color: string; value: string }[]
  status: number; ping: number | null; uptime24: number; uptime30: number; avgPing: number; certDays?: number; tunnel?: PubTunnel
  beats: PubBeat[]; days?: DayBucket[]
}
interface PubPage {
  slug: string; title: string; description: string; theme: Theme; layout: 'list' | 'cards'; barMode: 'heartbeat' | 'days'
  accentColor: string; logo: string; footer: string; customCss: string; showTags: boolean; showCertExpiry: boolean
  showPoweredBy: boolean; showCharts: boolean; refreshInterval: number; published: boolean
  language: 'auto' | 'en' | 'fa'; showUptime: boolean; showLatency: boolean; showBanner: boolean
  barDays?: number; domain?: string
}
interface PubData {
  page: PubPage
  groups: { name: string; monitors: PubMonitor[] }[]
  overall: 'operational' | 'partial' | 'major' | 'maintenance' | 'unknown'
  incidents: { id: number; title: string; content: string; style: string; createdAt: number; updatedAt: number }[]
  maintenance: { title: string; description: string; status: string; start?: number; end?: number }[]
  updatedAt: number
}

const overallKey = {
  operational: 'All systems operational',
  partial: 'Partial outage',
  major: 'Major outage',
  maintenance: 'Under maintenance',
  unknown: 'Waiting for data',
}
const overallColor = { operational: 'var(--up)', partial: 'var(--pending)', major: 'var(--down)', maintenance: 'var(--maint)', unknown: 'var(--text-3)' }

const cc = (m: PubMonitor) => m.country || m.tunnel?.peerCountry?.toLowerCase() || ''

function Linkify({ text }: { text: string }) {
  const parts = text.split(/(https?:\/\/[^\s)]+)/g)
  return <>{parts.map((p, i) => (/^https?:\/\//.test(p) ? <a key={i} href={p} target="_blank" rel="noreferrer noopener" className="underline underline-offset-2" dir="ltr">{p}</a> : <span key={i}>{p}</span>))}</>
}

function statusWordKey(s: number) {
  return s === -2 ? 'Paused' : s === -1 ? 'No data' : s === 1 ? 'Operational' : s === 0 ? 'Down' : s === 3 ? 'Maintenance' : 'Degraded'
}
function statusColor(s: number) {
  const k = s < 0 ? 'paused' : statusKey(s as 0 | 1 | 2 | 3)
  return { up: 'var(--up)', down: 'var(--down)', pending: 'var(--pending)', maint: 'var(--maint)', paused: 'var(--text-3)', empty: 'var(--text-3)' }[k]
}

function StatusLabel({ s }: { s: number }) {
  const t = useT()
  const color = statusColor(s)
  return <span className="inline-flex items-center gap-1.5 text-[13px] font-medium whitespace-nowrap" style={{ color }}><span className="size-2 rounded-full" style={{ background: color }} />{t(statusWordKey(s))}</span>
}

function Bars({ m, page, height = 30, barWidth = 6 }: { m: PubMonitor; page: PubPage; height?: number; barWidth?: number }) {
  return page.barMode === 'days' && m.days
    ? <DayBars days={m.days} count={page.barDays || 90} height={height} />
    : <HeartbeatBar beats={m.beats.map((b) => ({ status: b.s, time: b.t, ping: b.p }))} height={height} barWidth={barWidth} gap={3} maxBeats={100} />
}

/* ---------- charts (expandable) ---------- */

function ChartBlock({ slug, id, theme }: { slug: string; id: number; theme: Theme }) {
  const t = useT()
  const [range, setRange] = useState<'day' | 'week' | 'month'>('day')
  const [pts, setPts] = useState<ChartPoint[] | null>(null)
  useEffect(() => {
    setPts(null)
    api.get<ChartPoint[]>(`/api/status-page/${slug}/chart/${id}?range=${range}`).then(setPts).catch(() => setPts([]))
  }, [slug, id, range])
  const span = { day: 86400e3, week: 7 * 86400e3, month: 30 * 86400e3 }[range]
  const bento = theme === 'lime' || theme === 'noir'
  const ranges = [['day', t('Day')], ['week', t('Week')], ['month', t('Month')]] as const
  return (
    <div className="pt-4">
      <div className="flex items-center justify-between mb-2">
        <span className="text-[13px] text-muted">{t('Response time')}</span>
        {bento ? (
          <div className="flex gap-1.5">
            {ranges.map(([r, l]) => (
              <button key={r} onClick={() => setRange(r)} className={clsx('h-8 px-3.5 rounded-full text-[13px] font-medium border transition-colors', range === r ? 'bg-accent text-on-accent border-transparent' : 'border-line-strong text-muted hover:text-fg')}>{l}</button>
            ))}
          </div>
        ) : (
          <div className="flex items-center gap-2 text-[13px]">
            {ranges.map(([r, l], i) => (
              <span key={r} className="flex items-center gap-2">
                {i > 0 && <span className="text-subtle">|</span>}
                <button onClick={() => setRange(r)} className={clsx('transition-colors', range === r ? 'text-fg font-semibold' : 'text-subtle hover:text-muted')}>{l}</button>
              </span>
            ))}
          </div>
        )}
      </div>
      {pts === null ? <div className="h-[180px] grid place-items-center"><Spinner /></div> : bento ? (
        pts.length ? <Columns values={downsampleCols(pts, 28)} highlight={[downsampleCols(pts, 28).length - 1]} height={150} />
          : <p className="h-[150px] grid place-items-center text-sm text-muted">{t('No data yet')}</p>
      ) : (
        <AreaChart points={pts} height={180} rangeMs={span} variant={theme === 'clarity' || theme === 'zen' ? 'line' : 'area'} />
      )}
    </div>
  )
}

function downsampleCols(pts: ChartPoint[], count: number) {
  const vals = pts.map((p) => p.ping).filter((v): v is number => v != null)
  if (vals.length <= count) return vals
  const size = vals.length / count
  return Array.from({ length: count }, (_, i) => {
    const slice = vals.slice(Math.floor(i * size), Math.floor((i + 1) * size))
    return slice.reduce((a, b) => a + b, 0) / Math.max(1, slice.length)
  })
}

/* ---------- notices ---------- */

const incStyle: Record<string, { color: string; icon: typeof Info }> = {
  info: { color: 'var(--maint)', icon: Info },
  warning: { color: 'var(--pending)', icon: TriangleAlert },
  danger: { color: 'var(--down)', icon: CircleAlert },
  success: { color: 'var(--up)', icon: CircleCheck },
  maintenance: { color: 'var(--maint)', icon: Wrench },
}

function Notices({ d, rounded = 'rounded-2xl' }: { d: PubData; rounded?: string }) {
  const t = useT()
  return (
    <>
      {d.maintenance.map((m, i) => (
        <section key={`m${i}`} className={clsx('border px-5 py-4 bg-surface', rounded)} style={{ borderColor: 'color-mix(in srgb, var(--maint) 40%, transparent)' }}>
          <div className="flex items-center gap-2 font-semibold" style={{ color: 'var(--maint)' }}>
            <Wrench className="size-4" />{m.status === 'under-maintenance' ? t('Maintenance in progress') : t('Scheduled maintenance')} · <span className="text-fg" dir="auto">{m.title}</span>
          </div>
          {m.description && <p className="mt-1.5 text-sm text-muted whitespace-pre-line" dir="auto"><Linkify text={m.description} /></p>}
          {m.start && <p className="mt-1.5 text-[12.5px] text-subtle tnum">{fmtWeekdayTime(m.start)}{m.end ? ` → ${fmtWeekdayTime(m.end)}` : ''}</p>}
        </section>
      ))}
      {d.incidents.map((inc) => {
        const st = incStyle[inc.style] ?? incStyle.info
        return (
          <section key={inc.id} className={clsx('border px-5 py-4 bg-surface', rounded)} style={{ borderColor: `color-mix(in srgb, ${st.color} 40%, transparent)`, boxShadow: 'var(--shadow-card)' }}>
            <div className="flex items-start gap-3">
              <st.icon className="size-5 shrink-0 mt-0.5" style={{ color: st.color }} />
              <div className="min-w-0">
                <p className="font-semibold" dir="auto">{inc.title}</p>
                {inc.content && <p className="mt-1 text-sm text-muted whitespace-pre-line leading-relaxed" dir="auto"><Linkify text={inc.content} /></p>}
                <p className="mt-2 text-[12px] text-subtle">{fmtDateTime(inc.updatedAt * 1000)}</p>
              </div>
            </div>
          </section>
        )
      })}
    </>
  )
}

function PoweredBy() {
  const t = useT()
  return (
    <a href="https://github.com/neoauroraproject/Neo-Uptime" target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-2 hover:text-fg">
      <svg viewBox="0 0 64 64" className="size-4" aria-hidden><path d="M10 36h10l5-12 8 22 6-16 4 6h11" fill="none" stroke="currentColor" strokeWidth="6" strokeLinecap="round" strokeLinejoin="round" /></svg>
      {t('Powered by Neo Uptime')}
    </a>
  )
}

/* ---------- Zen ---------- */

function ZenRow({ m, page }: { m: PubMonitor; page: PubPage }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const flag = cc(m)
  const days = page.barMode === 'days' && m.days
  const ratio = days ? m.uptime30 : m.uptime24
  return (
    <div className="px-5 sm:px-6 py-5">
      <div className="flex items-center gap-3">
        {flag && <Flag cc={flag} size={22} className="rounded-[5px]" />}
        <div className="min-w-0 flex-1">
          <p className="font-medium text-[15px] truncate" dir="auto">{m.name}</p>
          {m.url && <p className="text-[12px] text-subtle truncate font-mono" dir="ltr">{m.url}</p>}
        </div>
        {page.showLatency && m.status === 1 && m.ping != null && <span className="hidden sm:inline text-[12.5px] text-subtle tnum">{ms(m.ping)}</span>}
        <StatusLabel s={m.status} />
      </div>
      <div className="mt-3.5">
        <Bars m={m} page={page} height={26} barWidth={5} />
        <div className="mt-2 flex items-center justify-between text-[11.5px] text-subtle bidi-auto" dir="ltr">
          <span>{days ? t('{n} days ago', { n: page.barDays || 90 }) : m.beats.length ? timeAgo(m.beats[0].t) : ''}</span>
          <span className="flex items-center gap-3">
            {page.showUptime && ratio >= 0 && <span className="tnum font-medium text-muted">{t('{p} uptime', { p: pct(ratio, 2) })}</span>}
            {page.showCharts && (
              <button onClick={() => setOpen(!open)} aria-expanded={open} className="inline-flex items-center gap-1 text-muted hover:text-fg">
                {t('Response time')}<ChevronDown className={clsx('size-3.5 transition-transform', open && 'rotate-180')} />
              </button>
            )}
          </span>
          <span>{days ? t('Today') : t('Now')}</span>
        </div>
      </div>
      {open && <ChartBlock slug={page.slug} id={m.id} theme="zen" />}
    </div>
  )
}

function Zen({ d, now, left }: { d: PubData; now: number; left: number }) {
  const t = useT()
  const page = d.page
  const color = overallColor[d.overall]
  return (
    <div className="relative mx-auto max-w-[700px] px-5 sm:px-6 py-14 sm:py-20 space-y-10">
      <header className="flex items-center gap-3.5">
        {page.logo && <img src={page.logo} alt="" className="size-10 rounded-xl object-contain" />}
        <div className="min-w-0">
          <h1 className="text-[19px] font-semibold tracking-tight" dir="auto">{page.title}</h1>
          {page.description && <p className="text-[13.5px] text-muted mt-0.5 whitespace-pre-line" dir="auto">{page.description}</p>}
        </div>
      </header>

      {page.showBanner && (
        <section className="space-y-2">
          <div className="flex items-center gap-4">
            <span className="relative size-3.5 rounded-full breathe shrink-0" style={{ background: color, color }} aria-hidden />
            <p className="text-[28px] sm:text-[34px] leading-tight font-semibold tracking-tight">{t(overallKey[d.overall])}</p>
          </div>
          <p className="text-[12.5px] text-subtle ps-[30px] tnum">{t('Updated {ago}', { ago: timeAgo(d.updatedAt, now) })} · {t('refresh in {n}s', { n: left })}</p>
        </section>
      )}

      {(d.maintenance.length > 0 || d.incidents.length > 0) && <div className="space-y-3"><Notices d={d} rounded="rounded-[20px]" /></div>}

      {d.groups.length === 0 && <p className="py-16 text-center text-muted">{t('Nothing is published on this page yet.')}</p>}

      {d.groups.map((g, gi) => (
        <section key={gi} className="space-y-3">
          {g.name && <h2 className="px-1 text-[12px] uppercase tracking-[0.14em] font-semibold text-subtle" dir="auto">{g.name}</h2>}
          <div className="bg-surface rounded-[22px] divide-y divide-line overflow-hidden" style={{ boxShadow: 'var(--shadow-card)', border: '1px solid var(--border)' }}>
            {g.monitors.length === 0 ? <p className="px-6 py-6 text-sm text-muted">{t('No services in this group.')}</p> : g.monitors.map((m) => <ZenRow key={m.id} m={m} page={page} />)}
          </div>
        </section>
      ))}

      {(page.footer || page.showPoweredBy || !page.showBanner) && (
        <footer className="pt-2 flex flex-col sm:flex-row items-center justify-between gap-3 text-[12px] text-subtle">
          <div className="whitespace-pre-line text-center sm:ltr:text-left sm:rtl:text-right" dir="auto">{page.footer && <Linkify text={page.footer} />}{!page.showBanner && !page.footer && t('Updated {ago}', { ago: timeAgo(d.updatedAt, now) })}</div>
          {page.showPoweredBy && <PoweredBy />}
        </footer>
      )}
    </div>
  )
}

/* ---------- classic themes ---------- */

function Banner({ d, theme }: { d: PubData; theme: Theme }) {
  const t = useT()
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
            <span className="inline-flex items-center gap-2 h-8 px-3 rounded-full bg-black/10 text-[13px] font-semibold"><Icon className="size-4" />{t('Status')}</span>
            <span className="text-[13px] font-medium opacity-80">{t('{online}/{total} online', { online, total: all.length })}</span>
          </div>
          <p className="font-display text-[40px] sm:text-[52px] leading-[1.05] font-semibold tracking-tight mt-8">{t(overallKey[d.overall])}</p>
        </div>
        <div className="rounded-[28px] bg-surface border border-line p-7 flex flex-col justify-between" style={{ boxShadow: 'var(--shadow-card)' }}>
          <div className="flex items-center justify-between text-[13px] text-muted"><span>{t('Uptime · 30 days')}</span><span className="inline-flex items-center h-7 px-2.5 rounded-full bg-accent-soft text-accent-ink font-semibold">{t('{n} services', { n: all.length })}</span></div>
          <p className="font-display text-[56px] leading-none font-semibold tnum tracking-tight mt-6">{avgUp < 0 ? '—' : n(avgUp * 100, 2)}<span className="text-2xl text-muted">%</span></p>
        </div>
      </section>
    )
  }
  if (theme === 'clarity') {
    return (
      <section className="rounded-2xl border-2 px-6 py-5 flex items-center gap-4" style={{ borderColor: d.overall === 'operational' ? 'var(--banner-border)' : color, background: d.overall === 'operational' ? 'var(--banner-bg)' : `color-mix(in srgb, ${color} 10%, var(--surface))`, boxShadow: 'var(--shadow-card)' }}>
        <span className="grid place-items-center size-9 rounded-full text-white" style={{ background: d.overall === 'operational' ? 'var(--up)' : color }}><Icon className="size-5" /></span>
        <p className="font-display text-2xl font-semibold tracking-tight">{t(overallKey[d.overall])}</p>
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
            <p className="font-display text-[26px] font-semibold tracking-tight">{t(overallKey[d.overall])}</p>
            <p className="text-sm text-muted">{t('{online} of {total} services online', { online, total: all.length })}</p>
          </div>
        </div>
        <div className="text-end">
          <p className="text-[12px] uppercase tracking-wider text-subtle font-semibold">{t('30-day uptime')}</p>
          <p className="font-display text-3xl font-semibold tnum">{pct(avgUp)}</p>
        </div>
      </div>
    </section>
  )
}

function MonitorRow({ m, page, theme }: { m: PubMonitor; page: PubPage; theme: Theme }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const days = page.barMode === 'days' && m.days
  const flag = cc(m)
  return (
    <div className="px-5 sm:px-6 py-4">
      <div className="flex items-center gap-3">
        {flag && <Flag cc={flag} size={22} />}
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium text-[15px]" dir="auto">{m.name}</span>
            {page.showTags && m.tags?.map((tg) => <TagChip key={`${tg.tagId}${tg.value}`} {...tg} />)}
            {page.showCertExpiry && m.certDays != null && (
              <span className={clsx('text-[12px] tnum', m.certDays <= 14 ? 'text-pending' : 'text-subtle')}>{t('cert {n}d', { n: m.certDays })}</span>
            )}
          </div>
          {m.url && <p className="text-[12.5px] text-muted truncate font-mono mt-0.5" dir="ltr">{m.url}</p>}
        </div>
        <StatusLabel s={m.status} />
        {page.showUptime && <span className="hidden sm:inline w-20 text-end text-[13px] font-semibold tnum">{pct(days ? m.uptime30 : m.uptime24, 2)}</span>}
      </div>
      <div className="mt-3">
        <Bars m={m} page={page} barWidth={theme === 'clarity' ? 5 : 6} />
        <div className="mt-1.5 flex items-center justify-between text-[11.5px] text-subtle bidi-auto" dir="ltr">
          <span>{days ? t('{n} days ago', { n: page.barDays || 90 }) : m.beats.length ? timeAgo(m.beats[0].t) : ''}</span>
          {page.showCharts ? (
            <button onClick={() => setOpen(!open)} aria-expanded={open} className="inline-flex items-center gap-1 text-muted hover:text-fg">
              {page.showLatency && m.avgPing >= 0 ? t('avg {v}', { v: ms(m.avgPing) }) : t('Response time')}<ChevronDown className={clsx('size-3.5 transition-transform', open && 'rotate-180')} />
            </button>
          ) : <span className="tnum">{page.showLatency && m.avgPing >= 0 ? t('avg {v}', { v: ms(m.avgPing) }) : ''}</span>}
          <span>{days ? t('Today') : t('Now')}</span>
        </div>
      </div>
      {open && <ChartBlock slug={page.slug} id={m.id} theme={theme} />}
    </div>
  )
}

function MonitorCardPub({ m, page, theme }: { m: PubMonitor; page: PubPage; theme: Theme }) {
  const t = useT()
  const bento = theme === 'lime' || theme === 'noir'
  const pings = m.beats.slice(-40).map((b) => (b.s === 1 ? b.p ?? null : null))
  const cur = m.status === 1 ? m.ping : null
  const flag = cc(m)
  return (
    <article className={clsx('bg-surface border border-line flex flex-col overflow-hidden', bento ? 'rounded-[28px]' : 'rounded-[20px]')} style={{ boxShadow: 'var(--shadow-card)' }}>
      <div className="p-5 pb-0">
        <div className="flex items-start justify-between gap-3">
          <div className="flex items-start gap-2.5 min-w-0">
            {flag && <Flag cc={flag} size={26} className="mt-0.5" />}
            <div className="min-w-0">
              <p className="font-display text-lg font-semibold leading-tight truncate" dir="auto">{m.name}</p>
              {m.tunnel?.peerLocation ? <p className="text-[12.5px] text-muted truncate">{m.tunnel.peerLocation}</p> : m.url ? <p className="text-[12.5px] text-muted truncate font-mono" dir="ltr">{m.url}</p> : null}
            </div>
          </div>
          <StatusLabel s={m.status} />
        </div>
        {m.tunnel && <TunnelBadges t={m.tunnel} className="mt-3" />}
      </div>
      <div className="px-5 pt-4 grid grid-cols-[auto_1fr] items-end gap-4">
        <div>
          {page.showLatency ? (
            <p className="font-display text-[44px] leading-none font-semibold tnum tracking-tight whitespace-nowrap" dir="ltr">{cur != null ? n(Math.round(cur)) : '—'}{cur != null && <span className="text-base text-muted ms-1">ms</span>}</p>
          ) : (
            <p className="font-display text-[36px] leading-none font-semibold tnum tracking-tight">{pct(m.uptime24, 1)}</p>
          )}
          <p className="mt-2 text-[12.5px] text-muted tnum">{m.tunnel ? `${bytes(m.tunnel.totalBytes)}${m.tunnel.uptime ? ` · ${t('up {d}', { d: m.tunnel.uptime })}` : ''}` : page.showLatency ? t('avg {v}', { v: ms(m.avgPing) }) : t('Uptime 24h')}</p>
        </div>
        {bento ? <Columns values={pings.map((p) => p ?? 0).slice(-16)} highlight={[15]} height={72} /> : <Sparkline values={pings} height={72} color={m.status === 0 ? 'var(--down)' : 'var(--chart)'} />}
      </div>
      <div className="mt-4 px-5 py-4 border-t border-line space-y-2">
        <Bars m={m} page={page} height={18} barWidth={4} />
        <div className="flex items-center justify-between text-[12.5px]">
          {m.tunnel ? (
            <span className="flex gap-3 text-muted tnum">
              <span className="inline-flex items-center gap-1"><ArrowDown className="size-3.5" />{bytes(m.tunnel.inBytes)}</span>
              <span className="inline-flex items-center gap-1"><ArrowUp className="size-3.5" />{bytes(m.tunnel.outBytes)}</span>
            </span>
          ) : <span className="text-muted">{t('Uptime 24h')}</span>}
          {page.showUptime && <span className="font-semibold tnum">{pct(m.uptime24, 2)}</span>}
        </div>
      </div>
    </article>
  )
}

function Classic({ d, theme, layout, now, left }: { d: PubData; theme: Theme; layout: 'list' | 'cards'; now: number; left: number }) {
  const t = useT()
  const page = d.page
  const bento = theme === 'lime' || theme === 'noir'
  return (
    <div className={clsx('relative mx-auto px-4 sm:px-6 py-8 sm:py-12 space-y-6', layout === 'cards' ? 'max-w-6xl' : 'max-w-4xl')}>
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-4 min-w-0">
          {page.logo && <img src={page.logo} alt="" className="size-12 rounded-2xl object-contain bg-surface border border-line" />}
          <div className="min-w-0">
            <h1 className={clsx('font-display font-semibold tracking-tight', bento ? 'text-[34px]' : 'text-[28px]')} dir="auto">{page.title}</h1>
            {page.description && <p className="text-[14.5px] text-muted mt-0.5 whitespace-pre-line" dir="auto">{page.description}</p>}
          </div>
        </div>
        <div className={clsx('text-[12.5px] text-muted tnum', bento && 'inline-flex items-center gap-2 h-9 px-4 rounded-full bg-surface border border-line')}>
          {t('Updated {ago}', { ago: timeAgo(d.updatedAt, now) })} · {t('refresh in {n}s', { n: left })}
        </div>
      </header>

      {page.showBanner && <Banner d={d} theme={theme} />}
      <Notices d={d} />

      {d.groups.length === 0 && <p className="py-16 text-center text-muted">{t('Nothing is published on this page yet.')}</p>}

      {d.groups.map((g, gi) => (
        <section key={gi} className="space-y-3">
          {g.name && <h2 className={clsx('font-display font-semibold tracking-tight', bento ? 'text-2xl px-1' : 'text-lg px-1')} dir="auto">{g.name}</h2>}
          {layout === 'cards' ? (
            <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-4">
              {g.monitors.map((m) => <MonitorCardPub key={m.id} m={m} page={page} theme={theme} />)}
            </div>
          ) : (
            <div className={clsx('bg-surface border border-line divide-y divide-line overflow-hidden', bento ? 'rounded-[28px]' : 'rounded-[20px]')} style={{ boxShadow: 'var(--shadow-card)' }}>
              {g.monitors.length === 0 ? <p className="px-6 py-6 text-sm text-muted">{t('No services in this group.')}</p> : g.monitors.map((m) => <MonitorRow key={m.id} m={m} page={page} theme={theme} />)}
            </div>
          )}
        </section>
      ))}

      <footer className="pt-6 pb-2 flex flex-col sm:flex-row items-center justify-between gap-3 text-[12.5px] text-subtle">
        <div className="whitespace-pre-line text-center sm:ltr:text-left sm:rtl:text-right" dir="auto">{page.footer && <Linkify text={page.footer} />}</div>
        {page.showPoweredBy && <PoweredBy />}
      </footer>
    </div>
  )
}

/* ---------- page ---------- */

const themeBg: Record<Theme, string> = { zen: '', aurora: '#050806', clarity: '#f5f6f8', lime: '#efefec', noir: '#0a0a0a' }

function pickLang(pref: string | undefined): Lang {
  if (pref === 'fa' || pref === 'en') return pref
  return navigator.language?.toLowerCase().startsWith('fa') ? 'fa' : 'en'
}

export default function PublicStatus({ slug: hostSlug }: { slug?: string } = {}) {
  const t = useT()
  const { slug: routeSlug = '' } = useParams()
  const slug = hostSlug ?? routeSlug
  const [params] = useSearchParams()
  const [d, setD] = useState<PubData | null>(null)
  const [err, setErr] = useState('')
  const [fetchedAt, setFetchedAt] = useState(Date.now())
  const now = useNow(1000)
  const timer = useRef<ReturnType<typeof setTimeout>>()
  const useForPage = useLang((s) => s.useForPage)

  const preview = params.get('preview') === '1'
  // In the editor the page shows the unsaved draft the editor posts to it.
  const draft = useRef<unknown>(null)
  const [hasDraft, setHasDraft] = useState(false)
  const reload = useRef<() => void>(() => {})
  const fromUrl = preview && !hasDraft
  const theme = (fromUrl && (params.get('theme') as Theme)) || d?.page.theme || 'zen'
  const layout = (fromUrl && (params.get('layout') as PubPage['layout'])) || d?.page.layout || 'list'
  const langPref = (fromUrl && params.get('lang')) || d?.page.language

  useEffect(() => {
    if (!preview || window.parent === window) return
    const onMsg = (e: MessageEvent) => {
      if (e.origin !== window.location.origin || e.data?.type !== 'neo-preview') return
      draft.current = e.data.page
      setHasDraft(true)
      reload.current()
    }
    window.addEventListener('message', onMsg)
    window.parent.postMessage({ type: 'neo-preview-ready' }, window.location.origin)
    return () => window.removeEventListener('message', onMsg)
  }, [preview])

  useEffect(() => {
    const logo = d?.page.logo
    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (!logo || !link) return
    const prev = link.href
    link.href = logo
    return () => { link.href = prev }
  }, [d?.page.logo])

  useEffect(() => {
    useForPage(pickLang(langPref ?? undefined))
  }, [langPref, useForPage])

  useEffect(() => {
    let alive = true
    const load = () => {
      clearTimeout(timer.current)
      const req = draft.current
        ? api.post<PubData>('/api/status-pages/preview', draft.current)
        : api.get<PubData>(`/api/status-page/${encodeURIComponent(slug)}`, { quiet401: true })
      req
        .then((r) => {
          if (!alive) return
          setD(r)
          setErr('')
          setFetchedAt(Date.now())
          document.title = r.page.title
          timer.current = setTimeout(load, Math.max(10, r.page.refreshInterval) * 1000)
        })
        .catch((e: ApiError) => {
          if (!alive) return
          setErr(e.status === 404 ? 'not-found' : e.message)
          timer.current = setTimeout(load, 30000)
        })
    }
    reload.current = load
    load()
    return () => {
      alive = false
      clearTimeout(timer.current)
    }
  }, [slug])

  useEffect(() => {
    const prev = document.body.style.background
    const dark = theme === 'aurora' || theme === 'noir' || (theme === 'zen' && matchMedia('(prefers-color-scheme: dark)').matches)
    document.body.style.background = themeBg[theme] || (dark ? '#0b0c0d' : '#f7f7f4')
    document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light')
    return () => { document.body.style.background = prev }
  }, [theme])

  const page = d?.page
  const accentStyle = useMemo(() => (page?.accentColor ? ({ '--accent': page.accentColor, '--chart': page.accentColor } as React.CSSProperties) : undefined), [page?.accentColor])

  if (err && !d) {
    return (
      <div className="sp min-h-dvh grid place-items-center bg-bg text-fg p-6" data-sp-theme={theme}>
        <div className="text-center">
          <p className="font-display text-6xl font-semibold text-subtle">{n(404)}</p>
          <p className="mt-3 text-muted">{err === 'not-found' ? t('This status page does not exist.') : err}</p>
        </div>
      </div>
    )
  }
  if (!d || !page) {
    return <div className="sp min-h-dvh grid place-items-center bg-bg" data-sp-theme={theme}><Spinner className="size-6" /></div>
  }

  const left = Math.max(0, page.refreshInterval - Math.floor((now - fetchedAt) / 1000))

  return (
    <div className="sp min-h-dvh bg-bg text-fg" data-sp-theme={theme} style={accentStyle}>
      {page.customCss && <style>{page.customCss}</style>}
      {theme === 'aurora' && <div className="pointer-events-none fixed inset-0 [background:radial-gradient(900px_420px_at_50%_-10%,rgb(34_197_94/0.12),transparent_70%)]" aria-hidden />}
      {!page.published && <div className="relative mx-auto max-w-4xl px-4 pt-4"><div className="rounded-xl border border-pending/40 bg-pending/10 px-4 py-2 text-[13px] text-pending">{t('Draft — only signed-in users can see this page.')}</div></div>}
      {theme === 'zen' ? <Zen d={d} now={now} left={left} /> : <Classic d={d} theme={theme} layout={layout} now={now} left={left} />}
    </div>
  )
}
