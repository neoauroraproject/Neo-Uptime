import { useMemo, useState } from 'react'
import { Activity, ArrowDown, ArrowUp, Gauge, Server } from 'lucide-react'
import { clsx, fmtTime, ms, n, timeAgo } from '../../lib/format'
import { useT } from '../../lib/i18n'
import { useMeasure } from '../../components/ui'
import {
  MonitorCardPub, MonitorRow, Notices, PoweredBy, Linkify, overallColor, overallKey, statusColor, statusWordKey,
  type PubData, type PubMonitor,
} from './PublicStatus'

/* Pulse: dark panels around one glowing gauge. The gauge is the page's single
   loud element; everything else stays quiet so it reads first. */

const SEGS = 17
// Healthy services light the arc from its start; the rest follow, then the unlit.
const order = (s: number) => (s === 1 ? 0 : s === 2 ? 1 : s === 3 ? 2 : s === 0 ? 3 : 4)

function avgUptime(all: PubMonitor[]) {
  const ups = all.filter((m) => m.uptime30 >= 0)
  return ups.length ? ups.reduce((a, m) => a + m.uptime30, 0) / ups.length : -1
}

function PulseGauge({ all, showUptime }: { all: PubMonitor[]; showUptime: boolean }) {
  const t = useT()
  const sorted = useMemo(() => [...all].sort((a, b) => order(a.status) - order(b.status)), [all])
  const online = all.filter((m) => m.status === 1).length
  const up = avgUptime(all)
  // Each segment stands for an equal share of the services, so the arc shows the
  // mix of states whether a page has three services or three hundred.
  const segs = Array.from({ length: SEGS }, (_, i) => {
    if (!sorted.length) return { s: -1, names: [] as string[] }
    const a = Math.floor((i * sorted.length) / SEGS)
    const b = Math.max(a + 1, Math.floor(((i + 1) * sorted.length) / SEGS))
    const rep = sorted[Math.min(sorted.length - 1, Math.floor(((i + 0.5) * sorted.length) / SEGS))]
    return { s: rep.status, names: sorted.slice(a, b).map((m) => m.name) }
  })
  const lit = segs.filter((g) => g.s >= 0).length
  const cx = 220, cy = 228, r = 182
  const lineColor = online === all.length ? 'var(--up)' : online === 0 ? 'var(--down)' : 'var(--pending)'
  const labels = (
    <>
      <p className="mt-3 text-[15px] sm:text-[17px] text-fg/90">{showUptime && up >= 0 ? t('Uptime over the last 30 days') : t('Services online')}</p>
      {all.length > 0 && (
        <p className="mt-1.5 inline-flex items-center gap-1.5 text-[14px] sm:text-[15px]" style={{ color: lineColor }}>
          {online === all.length ? <ArrowUp className="size-4" /> : <ArrowDown className="size-4" />}
          {t('{online} of {total} services online', { online, total: all.length })}
        </p>
      )}
    </>
  )
  return (
    <div className="mx-auto w-full max-w-[520px]">
      <div className="relative">
      <svg viewBox="0 0 440 250" className="w-full overflow-visible" role="img" aria-label={t('{online} of {total} services online', { online, total: all.length })}>
        <defs>
          <filter id="pulse-glow" x="-60%" y="-60%" width="220%" height="220%">
            <feGaussianBlur stdDeviation="7" result="b" />
            <feMerge><feMergeNode in="b" /><feMergeNode in="SourceGraphic" /></feMerge>
          </filter>
        </defs>
        {segs.map((g, i) => {
          const deg = 180 - ((i + 0.5) * 180) / SEGS
          const rad = (deg * Math.PI) / 180
          const x = cx + r * Math.cos(rad)
          const y = cy - r * Math.sin(rad)
          const on = g.s >= 0 && g.s !== -2
          // Lit segments brighten toward the end of the lit run, as a level meter does.
          const op = on ? 0.42 + 0.58 * (lit > 1 ? Math.min(i, lit - 1) / (lit - 1) : 1) : 1
          return (
            <g key={i} transform={`translate(${x} ${y}) rotate(${90 - deg})`}>
              <rect
                x={-14} y={-18} width={28} height={36} rx={9}
                className="pulse-seg"
                style={{ animationDelay: `${i * 45}ms`, fill: on ? statusColor(g.s) : 'var(--surface-3)', opacity: op }}
                filter={on ? 'url(#pulse-glow)' : undefined}
              >
                <title>{`${t(statusWordKey(g.s))}${g.names.length ? ` — ${g.names.join(', ')}` : ''}`}</title>
              </rect>
            </g>
          )
        })}
      </svg>
      <div className="pointer-events-none absolute inset-x-0 bottom-0 flex flex-col items-center text-center">
        {showUptime && up >= 0 ? (
          <p className="pulse-num font-display font-medium leading-none tracking-tight text-[46px] sm:text-[76px] tnum" dir="ltr">
            {n(up * 100, up === 1 ? 0 : 2)}<span className="text-[20px] sm:text-[28px] text-muted ms-1 align-top">%</span>
          </p>
        ) : (
          <p className="pulse-num font-display font-medium leading-none tracking-tight text-[46px] sm:text-[76px] tnum" dir="ltr">{n(online)}<span className="text-[24px] sm:text-[34px] text-muted">/{n(all.length)}</span></p>
        )}
        <div className="hidden sm:contents">{labels}</div>
      </div>
      </div>
      {/* On a phone the arc is too small to hold the labels as well. */}
      <div className="sm:hidden flex flex-col items-center text-center pt-1">{labels}</div>
    </div>
  )
}

/* ---------- response time columns ---------- */

interface Bucket { t0: number; t1: number; v: number | null }

function buckets(all: PubMonitor[], count: number): { list: Bucket[]; avg: number | null; change: number | null } {
  const pts = all.flatMap((m) => m.beats.filter((b) => b.s === 1 && b.p != null).map((b) => ({ t: b.t, p: b.p as number })))
  if (pts.length < 2) return { list: [], avg: null, change: null }
  const lo = Math.min(...pts.map((p) => p.t))
  const hi = Math.max(...pts.map((p) => p.t)) + 1
  const span = (hi - lo) / count
  const sums = Array.from({ length: count }, () => ({ s: 0, c: 0 }))
  for (const p of pts) {
    const i = Math.min(count - 1, Math.floor((p.t - lo) / span))
    sums[i].s += p.p
    sums[i].c++
  }
  const list = sums.map((x, i) => ({ t0: lo + i * span, t1: lo + (i + 1) * span, v: x.c ? x.s / x.c : null }))
  const avg = pts.reduce((a, p) => a + p.p, 0) / pts.length
  const half = (from: number, to: number) => {
    const xs = sums.slice(from, to).filter((x) => x.c)
    const c = xs.reduce((a, x) => a + x.c, 0)
    return c ? xs.reduce((a, x) => a + x.s, 0) / c : null
  }
  const early = half(0, Math.floor(count / 2))
  const late = half(Math.floor(count / 2), count)
  return { list, avg, change: early && late ? (late - early) / early : null }
}

function niceMax(v: number) {
  const p = Math.pow(10, Math.floor(Math.log10(Math.max(1, v))))
  const x = v / p
  return (x <= 1 ? 1 : x <= 2 ? 2 : x <= 2.5 ? 2.5 : x <= 5 ? 5 : 10) * p
}

function ResponseColumns({ list }: { list: Bucket[] }) {
  const t = useT()
  const [ref, w] = useMeasure<HTMLDivElement>()
  const last = list.length - 1
  const [hover, setHover] = useState<number | null>(null)
  const sel = hover ?? last
  const max = niceMax(Math.max(1, ...list.map((b) => b.v ?? 0)) * 1.12)
  const ticks = [1, 0.5, 0]
  const b = list[sel]
  const cx = ((sel + 0.5) / list.length) * w
  const bubbleW = 128
  const left = Math.min(Math.max(0, cx - bubbleW / 2), Math.max(0, w - bubbleW))
  // The callout floats above every bar it spans, so it never hides one.
  const slot = w / Math.max(1, list.length)
  const hPct = Math.max(4, ...list.map((x, i) => {
    const c = (i + 0.5) * slot
    return c > left - slot / 2 && c < left + bubbleW + slot / 2 && x.v != null ? (x.v / max) * 100 : 0
  }))
  return (
    <div className="grid grid-cols-[auto_1fr] gap-x-3" dir="ltr">
      <div className="relative h-[170px] mt-[72px] w-10">
        {ticks.map((f) => (
          <span key={f} className="absolute end-0 -translate-y-1/2 text-[11px] text-subtle font-mono pulse-num tnum" style={{ top: `${(1 - f) * 100}%` }}>{n(Math.round(max * f))}</span>
        ))}
      </div>
      <div className="min-w-0">
        <div ref={ref} className="relative h-[170px] mt-[72px]" onMouseLeave={() => setHover(null)}>
          {ticks.map((f) => (
            <span key={f} className={clsx('absolute inset-x-0 border-t', f === 0 ? 'border-line-strong' : 'border-dashed border-line')} style={{ top: `${(1 - f) * 100}%` }} aria-hidden />
          ))}
          <div className="absolute inset-0 flex items-end gap-[clamp(6px,2.4%,14px)]">
            {list.map((x, i) => (
              <button
                key={i}
                type="button"
                className="relative flex-1 h-full flex items-end focus-visible:outline-offset-4"
                onMouseEnter={() => setHover(i)}
                onFocus={() => setHover(i)}
                onBlur={() => setHover(null)}
                aria-label={`${fmtTime(x.t0)}–${fmtTime(x.t1)}: ${x.v != null ? ms(x.v) : t('No data')}`}
              >
                <span className={clsx('pulse-col w-full', i === sel && 'pulse-col-on', x.v == null && 'opacity-40')} style={{ height: `${x.v != null ? Math.max(4, (x.v / max) * 100) : 4}%` }} />
              </button>
            ))}
          </div>
          {b && w > 0 && (
            <div className="pointer-events-none absolute" style={{ left, bottom: `calc(${hPct}% + 14px)`, width: bubbleW }}>
              <div className="rounded-xl border border-line-strong bg-surface-2 px-3 py-2 shadow-[var(--shadow-card)]">
                <p className="text-[12px] text-muted whitespace-nowrap">{sel === last && hover == null ? t('Latest') : `${fmtTime(b.t0)}–${fmtTime(b.t1)}`}</p>
                <p className="pulse-num font-mono text-[20px] leading-tight text-accent-ink tnum whitespace-nowrap">{b.v != null ? ms(b.v) : '—'}</p>
              </div>
              <span className="absolute -bottom-[6px] size-3 rotate-45 border-b border-r border-line-strong bg-surface-2" style={{ left: cx - left - 6 }} aria-hidden />
            </div>
          )}
        </div>
        <div className="mt-2.5 flex justify-between text-[11px] text-subtle font-mono pulse-num tnum">
          <span>{fmtTime(list[0].t0)}</span>
          <span>{fmtTime(list[Math.floor(list.length / 2)].t0)}</span>
          <span>{fmtTime(list[last].t1)}</span>
        </div>
      </div>
    </div>
  )
}

/* ---------- panels ---------- */

function SummaryCard({ d, all, now, left }: { d: PubData; all: PubMonitor[]; now: number; left: number }) {
  const t = useT()
  const page = d.page
  const online = all.filter((m) => m.status === 1).length
  const pings = all.filter((m) => m.avgPing >= 0)
  const avg = pings.length ? pings.reduce((a, m) => a + m.avgPing, 0) / pings.length : null
  const color = overallColor[d.overall]
  const stats = [
    { icon: Server, v: n(all.length), label: t('Services') },
    { icon: Activity, v: n(online), label: t('Online') },
    ...(page.showLatency ? [{ icon: Gauge, v: avg != null ? ms(avg) : '—', label: t('Avg response') }] : []),
  ]
  return (
    <section className="pulse-panel pulse-tiles relative overflow-hidden p-5 sm:p-8 grid place-items-center min-h-[250px]">
      <div className="relative w-full max-w-[400px]">
        <div className="relative z-10 rounded-[22px] border border-line-strong bg-bg p-5" style={{ boxShadow: '0 24px 48px -20px rgb(0 0 0 / 0.9)' }}>
          <div className="flex items-center gap-3">
            {page.logo ? (
              <img src={page.logo} alt="" className="size-11 rounded-full object-contain bg-surface-2" />
            ) : (
              <span className="grid place-items-center size-11 rounded-full bg-surface-2" style={{ color }}>
                <span className="relative size-2.5 rounded-full breathe" style={{ background: color, color }} aria-hidden />
              </span>
            )}
            <div className="min-w-0">
              <p className="font-medium truncate" dir="auto">{t('Live status')}</p>
              <p className="text-[12.5px] text-subtle tnum">{t('Updated {ago}', { ago: timeAgo(d.updatedAt, now) })} · {t('refresh in {n}s', { n: left })}</p>
            </div>
          </div>
          <div className={clsx('mt-5 grid gap-3', stats.length === 3 ? 'grid-cols-3' : 'grid-cols-2')}>
            {stats.map((s) => (
              <div key={s.label} className="min-w-0">
                <p className="flex items-center gap-1.5 text-[15px] font-medium tnum whitespace-nowrap"><span className="pulse-num">{s.v}</span><s.icon className="size-3.5 text-subtle shrink-0" aria-hidden /></p>
                <p className="text-[12px] text-subtle truncate">{s.label}</p>
              </div>
            ))}
          </div>
        </div>
        <div className="relative -mt-5 mx-0 rounded-b-[22px] pt-7 pb-2.5 px-4 text-center text-[13px] font-medium" style={{ background: color, color: d.overall === 'unknown' ? 'var(--text)' : '#08130c' }}>
          {t(overallKey[d.overall])}
        </div>
      </div>
    </section>
  )
}

function ResponsePanel({ all }: { all: PubMonitor[] }) {
  const t = useT()
  const [ref, w] = useMeasure<HTMLDivElement>()
  const count = w && w < 380 ? 8 : 12
  const { list, avg, change } = useMemo(() => buckets(all, count), [all, count])
  return (
    <section ref={ref} className="pulse-panel p-5 sm:p-8">
      <p className="text-[14px] text-muted">{t('Response time')}</p>
      <div className="mt-2 flex items-baseline gap-3">
        <p className="pulse-num font-mono text-[40px] sm:text-[46px] leading-none tracking-tight tnum" dir="ltr">{avg != null ? n(Math.round(avg)) : '—'}<span className="text-[20px] text-muted ms-1">ms</span></p>
        {change != null && Math.abs(change) >= 0.005 && (
          <span className={clsx('text-[13px] font-mono pulse-num tnum', change < 0 ? 'text-accent-ink' : 'text-muted')} title={t('Change from the first half of this window')} dir="ltr">
            {change > 0 ? '+' : '−'}{n(Math.abs(change * 100), 0)}%
          </span>
        )}
      </div>
      {list.length ? <ResponseColumns list={list} /> : <p className="h-[200px] grid place-items-center text-sm text-muted">{t('No response data yet')}</p>}
    </section>
  )
}

export default function Pulse({ d, layout, now, left }: { d: PubData; layout: 'list' | 'cards'; now: number; left: number }) {
  const t = useT()
  const page = d.page
  const all = d.groups.flatMap((g) => g.monitors)
  const side = page.showBanner || page.showLatency
  return (
    <div className="relative mx-auto max-w-[1180px] px-4 sm:px-6 py-6 sm:py-10 space-y-4">
      <div className={clsx('grid gap-4', side && 'lg:grid-cols-[1.3fr_1fr]')}>
        <section className={clsx('pulse-panel relative overflow-hidden px-6 pt-7 pb-8 sm:px-10 sm:pt-10 flex flex-col gap-8', side && page.showBanner && page.showLatency && 'lg:row-span-2')}>
          <header className="flex items-start gap-4">
            {page.logo && !page.showBanner && <img src={page.logo} alt="" className="size-12 rounded-2xl object-contain" />}
            <div className="min-w-0">
              <h1 className="font-display text-[34px] sm:text-[46px] leading-[1.08] font-semibold tracking-tight max-w-[14ch]" dir="auto">{page.title}</h1>
              {page.description && <p className="mt-3 text-[14px] text-muted max-w-[44ch] whitespace-pre-line" dir="auto">{page.description}</p>}
              {!page.showBanner && <p className="mt-3 text-[12.5px] text-subtle tnum">{t('Updated {ago}', { ago: timeAgo(d.updatedAt, now) })} · {t('refresh in {n}s', { n: left })}</p>}
            </div>
          </header>
          <div className="flex-1 flex items-end">
            <PulseGauge all={all} showUptime={page.showUptime} />
          </div>
        </section>
        {page.showBanner && <SummaryCard d={d} all={all} now={now} left={left} />}
        {page.showLatency && <ResponsePanel all={all} />}
      </div>

      {(d.maintenance.length > 0 || d.incidents.length > 0 || d.radar) && <div className="space-y-3"><Notices d={d} rounded="rounded-[22px]" /></div>}

      {d.groups.length === 0 && <p className="py-16 text-center text-muted">{t('Nothing is published on this page yet.')}</p>}

      {d.groups.map((g, gi) => (
        <section key={gi} className="pt-4 space-y-3">
          {g.name && <h2 className="px-1 font-display text-[22px] font-semibold tracking-tight" dir="auto">{g.name}</h2>}
          {layout === 'cards' ? (
            <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-4">
              {g.monitors.map((m) => <MonitorCardPub key={m.id} m={m} page={page} theme="pulse" />)}
            </div>
          ) : (
            <div className="pulse-panel divide-y divide-line overflow-hidden">
              {g.monitors.length === 0 ? <p className="px-6 py-6 text-sm text-muted">{t('No services in this group.')}</p> : g.monitors.map((m) => <MonitorRow key={m.id} m={m} page={page} theme="pulse" />)}
            </div>
          )}
        </section>
      ))}

      <footer className="pt-8 pb-2 flex flex-col items-center gap-2 text-center text-[12.5px] text-subtle">
        <div className="whitespace-pre-line text-center" dir="auto">{page.footer && <Linkify text={page.footer} />}</div>
        {page.showPoweredBy && <PoweredBy />}
      </footer>
    </div>
  )
}
