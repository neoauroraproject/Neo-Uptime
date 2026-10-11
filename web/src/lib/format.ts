import type { Status } from './types'
import { digits, t, useLang } from './i18n'

let tz: string | undefined
export function setTimezone(zone: string) {
  tz = !zone || zone === 'auto' ? undefined : zone
}

const fmtCache = new Map<string, Intl.DateTimeFormat>()
function fmt(key: string, o: Intl.DateTimeFormatOptions) {
  const lang = useLang.getState().lang
  const k = key + lang + (tz ?? '')
  let f = fmtCache.get(k)
  if (!f) {
    // Persian gets the Solar Hijri calendar and Persian digits from Intl itself.
    f = new Intl.DateTimeFormat(lang === 'fa' ? 'fa-IR' : 'en-GB', { ...o, timeZone: tz })
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

/** A number in the current language's digits. */
export function n(v: number | string, frac?: number): string {
  const s = typeof v === 'number' ? (frac === undefined ? String(v) : v.toFixed(frac)) : v
  return digits(s)
}

export function timeAgo(ms: number, now = Date.now()) {
  const s = Math.max(0, Math.round((now - ms) / 1000))
  if (s < 10) return t('just now')
  if (s < 60) return t('{n}s ago', { n: s })
  const m = Math.round(s / 60)
  if (m < 60) return t('{n}m ago', { n: m })
  const h = Math.round(m / 60)
  if (h < 48) return t('{n}h ago', { n: h })
  return t('{n}d ago', { n: Math.round(h / 24) })
}

export function duration(sec: number) {
  sec = Math.round(sec)
  if (sec < 60) return t('{s}s', { s: sec })
  const m = Math.floor(sec / 60)
  if (m < 60) return sec % 60 ? t('{m}m {s}s', { m, s: sec % 60 }) : t('{m}m', { m })
  const h = Math.floor(m / 60)
  if (h < 24) return m % 60 ? t('{h}h {m}m', { h, m: m % 60 }) : t('{h}h', { h })
  return h % 24 ? t('{d}d {h}h', { d: Math.floor(h / 24), h: h % 24 }) : t('{d}d', { d: Math.floor(h / 24) })
}

export function bytes(v: number | undefined | null, frac = 1) {
  if (v == null || !isFinite(v)) return '—'
  const u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let i = 0
  let x = v
  while (x >= 1000 && i < u.length - 1) {
    x /= 1000
    i++
  }
  return `${digits(i === 0 ? String(Math.round(x)) : x.toFixed(x >= 100 ? 0 : frac))} ${u[i]}`
}

export function rate(bps: number | undefined | null) {
  if (bps == null) return '—'
  const bits = bps * 8
  const u = ['bps', 'Kbps', 'Mbps', 'Gbps']
  let i = 0
  let x = bits
  while (x >= 1000 && i < u.length - 1) {
    x /= 1000
    i++
  }
  return `${digits(x.toFixed(x >= 100 || i === 0 ? 0 : 1))} ${u[i]}`
}

export function pct(ratio: number | undefined | null, frac = 2) {
  if (ratio == null || ratio < 0) return '—'
  const v = ratio * 100
  if (v === 100) return digits('100%')
  return digits(`${v.toFixed(frac)}%`)
}

export function ms(v: number | null | undefined) {
  if (v == null || v < 0) return '—'
  return `${digits(v < 10 ? v.toFixed(1) : String(Math.round(v)))} ms`
}

export const statusName = (s: number) => [t('Down'), t('Up'), t('Pending'), t('Maintenance')][s] ?? t('Unknown')

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
