import { useId, useMemo, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { clsx, fmtDate, fmtDateTime, fmtTime, ms, n, pct, statusKey, statusName } from '../lib/format'
import { t } from '../lib/i18n'
import type { ChartPoint, Heartbeat, Status } from '../lib/types'
import { useMeasure } from './ui'

/* ---------- floating tooltip ---------- */

interface Tip { x: number; y: number; content: ReactNode }

function TipLayer({ tip }: { tip: Tip | null }) {
  if (!tip) return null
  const left = Math.min(Math.max(tip.x, 110), window.innerWidth - 110)
  return createPortal(
    <div
      className="pointer-events-none fixed z-[70] -translate-x-1/2 -translate-y-full rounded-xl border border-line-strong bg-surface px-3 py-2 text-[12.5px] shadow-[var(--shadow-pop)] whitespace-nowrap"
      style={{ left, top: tip.y - 10 }}
      role="tooltip"
    >
      {tip.content}
    </div>,
    document.body,
  )
}

const barColor: Record<string, string> = {
  up: 'var(--up)', down: 'var(--down)', pending: 'var(--pending)', maint: 'var(--maint)', paused: 'var(--paused)', empty: 'var(--bar-empty, var(--surface-3))',
}

/* ---------- heartbeat bar ---------- */

export interface BeatLike { status: Status; time: number; ping?: number | null; msg?: string }

export function HeartbeatBar({ beats, height = 28, barWidth = 6, gap = 3, maxBeats = 100, className, newestId, showLabels = false }: {
  beats: BeatLike[]; height?: number; barWidth?: number; gap?: number; maxBeats?: number; className?: string; newestId?: number; showLabels?: boolean
}) {
  const [ref, w] = useMeasure<HTMLDivElement>()
  const [tip, setTip] = useState<Tip | null>(null)
  const fit = Math.max(1, Math.floor((w + gap) / (barWidth + gap)))
  const count = Math.min(maxBeats, fit)
  const shown = beats.slice(-count)
  const pad = Math.max(0, count - shown.length)
  return (
    <div className={className} dir="ltr">
      <div ref={ref} className="flex items-end justify-end w-full overflow-hidden" style={{ height, gap }} onMouseLeave={() => setTip(null)}>
        {w > 0 && Array.from({ length: pad }).map((_, i) => (
          <span key={`p${i}`} className="shrink-0 rounded-full" style={{ width: barWidth, height: '100%', background: barColor.empty, opacity: 0.6 }} />
        ))}
        {w > 0 && shown.map((b, i) => {
          const k = statusKey(b.status)
          const isNew = newestId !== undefined && i === shown.length - 1
          return (
            <span
              key={`${b.time}-${i}`}
              className={clsx('shrink-0 rounded-full transition-transform duration-150 hover:scale-y-110 origin-bottom', isNew && 'beat-new')}
              style={{ width: barWidth, height: '100%', background: barColor[k] }}
              onMouseEnter={(e) => {
                const r = (e.target as HTMLElement).getBoundingClientRect()
                setTip({
                  x: r.left + r.width / 2,
                  y: r.top,
                  content: (
                    <div className="space-y-0.5">
                      <div className="font-medium text-fg">{fmtDateTime(b.time)}</div>
                      <div className="flex items-center gap-2 text-muted">
                        <span className="size-2 rounded-full" style={{ background: barColor[k] }} />
                        {statusName(b.status)}
                        {b.ping != null && <span className="tnum">· {ms(b.ping)}</span>}
                      </div>
                      {b.msg && <div className="text-muted max-w-72 truncate">{b.msg}</div>}
                    </div>
                  ),
                })
              }}
            />
          )
        })}
      </div>
      {showLabels && shown.length > 0 && (
        <div className="mt-1.5 flex justify-between text-[11px] text-subtle tnum">
          <span>{fmtTime(shown[0].time)}</span>
          <span>{fmtTime(shown[shown.length - 1].time)}</span>
        </div>
      )}
      <TipLayer tip={tip} />
    </div>
  )
}

/* ---------- daily uptime bars (status page "90 days") ---------- */

export interface DayBucket { t: number; up: number; down: number; avg: number | null }

export function DayBars({ days, count = 90, height = 30, className }: { days: DayBucket[]; count?: number; height?: number; className?: string }) {
  const [ref, w] = useMeasure<HTMLDivElement>()
  const [tip, setTip] = useState<Tip | null>(null)
  const gap = 3
  const fit = Math.max(1, Math.min(count, Math.floor((w + gap) / (3 + gap))))
  const byDay = new Map(days.map((d) => [Math.floor(d.t / 86400), d]))
  const today = Math.floor(Date.now() / 86400000)
  const slots = Array.from({ length: fit }, (_, i) => today - (fit - 1 - i))
  return (
    <div className={className} dir="ltr">
      <div ref={ref} className="flex items-end w-full" style={{ height, gap }} onMouseLeave={() => setTip(null)}>
        {w > 0 && slots.map((day) => {
          const d = byDay.get(day)
          const total = d ? d.up + d.down : 0
          const ratio = total ? d!.up / total : -1
          const color = ratio < 0 ? barColor.empty : ratio >= 0.999 ? barColor.up : ratio >= 0.95 ? barColor.pending : barColor.down
          return (
            <span
              key={day}
              className="flex-1 rounded-[3px] min-w-[3px] transition-opacity hover:opacity-80"
              style={{ height: '100%', background: color }}
              onMouseEnter={(e) => {
                const r = (e.target as HTMLElement).getBoundingClientRect()
                setTip({
                  x: r.left + r.width / 2, y: r.top,
                  content: (
                    <div className="space-y-0.5">
                      <div className="font-medium text-fg">{fmtDate(day * 86400000)}</div>
                      <div className="text-muted">{ratio < 0 ? t('No data') : t('{p} uptime', { p: pct(ratio) })}{d?.down ? ` · ${t('{n} failed checks', { n: d.down })}` : ''}</div>
                      {d?.avg != null && <div className="text-muted tnum">{t('avg {v}', { v: ms(d.avg) })}</div>}
                    </div>
                  ),
                })
              }}
            />
          )
        })}
      </div>
      <TipLayer tip={tip} />
    </div>
  )
}

/* ---------- area chart (response time) ---------- */

function niceMax(v: number) {
  if (v <= 0) return 10
  const p = Math.pow(10, Math.floor(Math.log10(v)))
  const n = v / p
  const m = n <= 1 ? 1 : n <= 2 ? 2 : n <= 2.5 ? 2.5 : n <= 5 ? 5 : 10
  return m * p
}

export function AreaChart({ points, height = 220, color = 'var(--chart)', rangeMs, emptyText, compact, variant = 'area' }: {
  points: ChartPoint[]; height?: number; color?: string; rangeMs?: number; emptyText?: string; compact?: boolean; variant?: 'area' | 'line'
}) {
  const [ref, w] = useMeasure<HTMLDivElement>()
  const [hover, setHover] = useState<number | null>(null)
  const gid = useId().replace(/:/g, '')
  const padL = compact ? 0 : 44, padR = compact ? 0 : 10, padT = 12, padB = compact ? 4 : 26
  const innerW = Math.max(10, w - padL - padR)
  const innerH = height - padT - padB

  const model = useMemo(() => {
    if (!points.length) return null
    const end = Date.now()
    const start = rangeMs ? end - rangeMs : points[0].t
    const span = Math.max(1, end - start)
    const pings = points.map((p) => p.ping).filter((v): v is number => v != null)
    const maxV = niceMax(Math.max(1, ...pings) * 1.1)
    const x = (t: number) => padL + ((t - start) / span) * innerW
    const y = (v: number) => padT + innerH - (v / maxV) * innerH
    // Split into runs so a gap in data — a down period, or time when nothing
    // was checked at all — breaks the line instead of being drawn across.
    const deltas = points.slice(1).map((p, i) => p.t - points[i].t).sort((a, b) => a - b)
    const typical = deltas.length ? deltas[Math.floor(deltas.length / 2)] : Infinity
    const runs: { x: number; y: number }[][] = []
    let cur: { x: number; y: number }[] = []
    let prevT = -Infinity
    for (const p of points) {
      const gap = p.t - prevT > typical * 4
      prevT = p.t
      if (p.ping == null || gap) {
        if (cur.length) runs.push(cur)
        cur = []
        if (p.ping == null) continue
      }
      cur.push({ x: x(p.t), y: y(p.ping) })
    }
    if (cur.length) runs.push(cur)
    const downs = points.filter((p) => p.status === 0 || (p.down ?? 0) > 0)
    const slotW = Math.max(2, innerW / Math.max(30, points.length))
    const avg = pings.length ? pings.reduce((a, b) => a + b, 0) / pings.length : null
    const ticks = [0, 0.5, 1].map((f) => ({ v: maxV * f, y: y(maxV * f) }))
    const xt = [0, 0.25, 0.5, 0.75, 1].map((f) => ({ t: start + span * f, x: padL + innerW * f }))
    return { x, y, runs, downs, slotW, maxV, avg, ticks, xt, start, span, min: pings.length ? Math.min(...pings) : null, max: pings.length ? Math.max(...pings) : null }
  }, [points, innerW, innerH, padL, padT, rangeMs])

  const smooth = (pts: { x: number; y: number }[]) => {
    if (pts.length < 2) return pts.length ? `M${pts[0].x},${pts[0].y}` : ''
    let d = `M${pts[0].x},${pts[0].y}`
    for (let i = 1; i < pts.length; i++) {
      const p0 = pts[i - 1], p1 = pts[i]
      const cx = (p0.x + p1.x) / 2
      d += ` C${cx},${p0.y} ${cx},${p1.y} ${p1.x},${p1.y}`
    }
    return d
  }

  const hp = hover != null ? points[hover] : null
  const longRange = (rangeMs ?? 0) > 36 * 3600 * 1000
  const empty = emptyText ?? t('No data in this range yet')
  const summary = model?.avg != null ? t('Response time: average {a}, min {lo}, max {hi}', { a: ms(model.avg), lo: ms(model.min), hi: ms(model.max) }) : empty

  return (
    <div ref={ref} className="relative w-full select-none" style={{ height }} dir="ltr">
      {!model ? (
        <div className="absolute inset-0 grid place-items-center text-sm text-muted">{empty}</div>
      ) : w > 0 ? (
        <svg
          width={w}
          height={height}
          role="img"
          aria-label={summary}
          className="overflow-visible"
          onMouseMove={(e) => {
            const r = (e.currentTarget as SVGElement).getBoundingClientRect()
            const px = e.clientX - r.left
            let best = 0, bd = Infinity
            points.forEach((p, i) => {
              const d = Math.abs(model.x(p.t) - px)
              if (d < bd) { bd = d; best = i }
            })
            setHover(best)
          }}
          onMouseLeave={() => setHover(null)}
        >
          <defs>
            <linearGradient id={`g${gid}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.32} />
              <stop offset="100%" stopColor={color} stopOpacity={0} />
            </linearGradient>
          </defs>
          {!compact && model.ticks.map((tk, i) => (
            <g key={i}>
              <line x1={padL} x2={padL + innerW} y1={tk.y} y2={tk.y} stroke="var(--border)" strokeDasharray={i === 0 ? undefined : '3 4'} />
              <text x={padL - 8} y={tk.y + 4} textAnchor="end" className="fill-[var(--text-3)] text-[11px] tnum">{n(Math.round(tk.v))}</text>
            </g>
          ))}
          {model.downs.map((p, i) => (
            <rect key={i} x={model.x(p.t) - model.slotW / 2} y={padT} width={model.slotW} height={innerH} fill="var(--down)" opacity={0.14} />
          ))}
          {model.runs.map((run, i) => {
            const line = smooth(run)
            const area = `${line} L${run[run.length - 1].x},${padT + innerH} L${run[0].x},${padT + innerH} Z`
            return (
              <g key={i}>
                {variant === 'area' && <path d={area} fill={`url(#g${gid})`} />}
                <path d={line} fill="none" stroke={color} strokeWidth={compact ? 1.75 : 2} strokeLinejoin="round" strokeLinecap="round" />
                {run.length === 1 && <circle cx={run[0].x} cy={run[0].y} r={2.5} fill={color} />}
              </g>
            )
          })}
          {!compact && model.xt.map((tk, i) => (
            <text key={i} x={tk.x} y={height - 6} textAnchor={i === 0 ? 'start' : i === 4 ? 'end' : 'middle'} className="fill-[var(--text-3)] text-[11px] tnum">
              {longRange ? fmtDate(tk.t) : fmtTime(tk.t)}
            </text>
          ))}
          {hp && (
            <g>
              <line x1={model.x(hp.t)} x2={model.x(hp.t)} y1={padT} y2={padT + innerH} stroke="var(--text-3)" strokeWidth={1} />
              {hp.ping != null && <circle cx={model.x(hp.t)} cy={model.y(hp.ping)} r={4.5} fill="var(--surface)" stroke={color} strokeWidth={2.5} />}
            </g>
          )}
        </svg>
      ) : null}
      {hp && model && (
        <div
          className="pointer-events-none absolute top-1 z-10 rounded-xl border border-line-strong bg-surface px-3 py-2 text-[12.5px] shadow-[var(--shadow-pop)] whitespace-nowrap"
          style={{ left: Math.min(Math.max(model.x(hp.t), 80), w - 80), transform: 'translateX(-50%)' }}
        >
          <div className="flex items-center gap-2">
            <span className="font-medium">{longRange ? fmtDateTime(hp.t) : fmtDateTime(hp.t)}</span>
            <span className="size-2 rounded-full" style={{ background: barColor[statusKey(hp.status)] }} />
          </div>
          <div className="text-muted tnum mt-0.5">
            {hp.ping != null ? ms(hp.ping) : statusName(hp.status)}
            {hp.min != null && hp.max != null && hp.max !== hp.min && <span> · {ms(hp.min)}–{ms(hp.max)}</span>}
          </div>
        </div>
      )}
    </div>
  )
}

/* ---------- sparkline ---------- */

export function Sparkline({ values, height = 56, color = 'var(--chart)', fill = true, className }: { values: (number | null)[]; height?: number; color?: string; fill?: boolean; className?: string }) {
  const [ref, w] = useMeasure<HTMLDivElement>()
  const gid = useId().replace(/:/g, '')
  const nums = values.filter((v): v is number => v != null)
  if (!nums.length) return <div ref={ref} className={className} style={{ height }} />
  const max = Math.max(...nums) * 1.12 || 1
  const min = Math.min(0, Math.min(...nums))
  const step = values.length > 1 ? w / (values.length - 1) : w
  const pts = values.map((v, i) => (v == null ? null : { x: i * step, y: height - 2 - ((v - min) / (max - min)) * (height - 6) }))
  let d = ''
  let started = false
  pts.forEach((p) => {
    if (!p) { started = false; return }
    d += started ? ` L${p.x.toFixed(1)},${p.y.toFixed(1)}` : `M${p.x.toFixed(1)},${p.y.toFixed(1)}`
    started = true
  })
  const valid = pts.filter(Boolean) as { x: number; y: number }[]
  const area = valid.length > 1 ? `M${valid[0].x},${height} ${valid.map((p) => `L${p.x},${p.y}`).join(' ')} L${valid[valid.length - 1].x},${height} Z` : ''
  return (
    <div ref={ref} className={className} style={{ height }}>
      {w > 0 && (
        <svg width={w} height={height} aria-hidden className="overflow-visible">
          <defs>
            <linearGradient id={`s${gid}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.35} />
              <stop offset="100%" stopColor={color} stopOpacity={0} />
            </linearGradient>
          </defs>
          {fill && area && <path d={area} fill={`url(#s${gid})`} />}
          <path d={d} fill="none" stroke={color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
        </svg>
      )}
    </div>
  )
}

/* ---------- column chart (lime theme) ---------- */

export function Columns({ values, labels, highlight, height = 160, className }: { values: number[]; labels?: string[]; highlight?: number[]; height?: number; className?: string }) {
  const max = Math.max(1, ...values)
  const [tip, setTip] = useState<Tip | null>(null)
  return (
    <div className={className} dir="ltr">
      <div className="flex items-end gap-[6px] w-full" style={{ height }} onMouseLeave={() => setTip(null)}>
        {values.map((v, i) => (
          <div
            key={i}
            className="flex-1 rounded-full transition-opacity hover:opacity-85"
            style={{ height: `${Math.max(6, (v / max) * 100)}%`, background: highlight?.includes(i) ? 'var(--accent)' : 'var(--text)' }}
            onMouseEnter={(e) => {
              const r = (e.target as HTMLElement).getBoundingClientRect()
              setTip({ x: r.left + r.width / 2, y: r.top, content: <span className="tnum">{labels?.[i] ? `${labels[i]} · ` : ''}{ms(v)}</span> })
            }}
          />
        ))}
      </div>
      <TipLayer tip={tip} />
    </div>
  )
}

/* ---------- meter ---------- */

export function Meter({ value, label, sub }: { value: number; label: string; sub?: string }) {
  const tone = value >= 90 ? 'var(--down)' : value >= 75 ? 'var(--pending)' : 'var(--up)'
  return (
    <div>
      <div className="flex items-baseline justify-between text-[13px]">
        <span className="text-muted">{label}</span>
        <span className="tnum font-medium">{pct(value / 100, 0)}{sub && <span className="text-muted font-normal"> · {sub}</span>}</span>
      </div>
      <div className="mt-1.5 h-2 rounded-full bg-surface-3 overflow-hidden" role="meter" aria-valuenow={Math.round(value)} aria-valuemin={0} aria-valuemax={100} aria-label={label}>
        <div className="h-full rounded-full transition-[width] duration-500" style={{ width: `${Math.min(100, value)}%`, background: tone }} />
      </div>
    </div>
  )
}

export function beatsToPoints(beats: Heartbeat[]): ChartPoint[] {
  return beats.map((b) => ({ t: b.time, ping: b.ping, status: b.status }))
}
