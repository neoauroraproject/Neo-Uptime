import type { Status } from './types'

let tz: string | undefined
export function setTimezone(zone: string) {
  tz = !zone || zone === 'auto' ? undefined : zone
}

const fmtCache = new Map<string, Intl.DateTimeFormat>()
function fmt(key: string, o: Intl.DateTimeFormatOptions) {
  const k = key + (tz ?? '')
  let f = fmtCache.get(k)
  if (!f) {
    f = new Intl.DateTimeFormat(undefined, { ...o, timeZone: tz })
    fmtCache.set(k, f)
  }
  return f
}

export const fmtDateTime = (ms: number) =>
  fmt('dt', { year: 'numeric', month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(ms)
export const fmtDate = (ms: number) => fmt('d', { year: 'numeric', month: 'short', day: '2-digit' }).format(ms)
export const fmtTime = (ms: number) => fmt('t', { hour: '2-digit', minute: '2-digit', hour12: false }).format(ms)
export const fmtTimeSec = (ms: number) => fmt('ts', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(ms)
export const fmtDayShort = (ms: number) => fmt('ds', { month: 'short', day: 'numeric' }).format(ms)
export const fmtWeekdayTime = (ms: number) =>
  fmt('wt', { weekday: 'long', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false }).format(ms)

export function timeAgo(ms: number, now = Date.now()) {
  const s = Math.max(0, Math.round((now - ms) / 1000))
  if (s < 10) return 'just now'
  if (s < 60) return `${s}s ago`
  const m = Math.round(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.round(m / 60)
  if (h < 48) return `${h}h ago`
  return `${Math.round(h / 24)}d ago`
}

export function duration(sec: number) {
  if (sec < 60) return `${Math.round(sec)}s`
  const m = Math.floor(sec / 60)
  if (m < 60) return `${m}m ${Math.round(sec % 60)}s`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ${m % 60}m`
  return `${Math.floor(h / 24)}d ${h % 24}h`
}

export function bytes(n: number | undefined | null, digits = 1) {
  if (n == null || !isFinite(n)) return '—'
  const u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let i = 0
  let v = n
  while (v >= 1000 && i < u.length - 1) {
    v /= 1000
    i++
  }
  return `${i === 0 ? Math.round(v) : v.toFixed(v >= 100 ? 0 : digits)} ${u[i]}`
}

export function rate(bps: number | undefined | null) {
  if (bps == null) return '—'
  const bits = bps * 8
  const u = ['bps', 'Kbps', 'Mbps', 'Gbps']
  let i = 0
  let v = bits
  while (v >= 1000 && i < u.length - 1) {
    v /= 1000
    i++
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${u[i]}`
}

export function pct(ratio: number | undefined | null, digits = 2) {
  if (ratio == null || ratio < 0) return '—'
  const v = ratio * 100
  if (v === 100) return '100%'
  return `${v.toFixed(digits)}%`
}

export function ms(v: number | null | undefined) {
  if (v == null || v < 0) return '—'
  if (v < 10) return `${v.toFixed(1)} ms`
  return `${Math.round(v)} ms`
}

export const statusName: Record<number, string> = { 0: 'Down', 1: 'Up', 2: 'Pending', 3: 'Maintenance' }

export function statusKey(s: Status | null | undefined, active = true): 'up' | 'down' | 'pending' | 'maint' | 'paused' | 'empty' {
  if (!active) return 'paused'
  switch (s) {
    case 1: return 'up'
    case 0: return 'down'
    case 2: return 'pending'
    case 3: return 'maint'
  }
  return 'empty'
}

export function uptimeTone(ratio: number) {
  if (ratio < 0) return 'empty'
  if (ratio >= 0.99) return 'up'
  if (ratio >= 0.95) return 'pending'
  return 'down'
}

export function clsx(...parts: (string | false | null | undefined)[]) {
  return parts.filter(Boolean).join(' ')
}
