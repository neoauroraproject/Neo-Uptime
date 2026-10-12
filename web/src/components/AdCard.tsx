import { useEffect, useMemo, useState, type CSSProperties } from 'react'
import { ArrowUpRight, X } from 'lucide-react'
import { api } from '../lib/api'
import { clsx } from '../lib/format'
import { useLang, useT } from '../lib/i18n'
import type { Ad } from '../lib/types'

// The project's text ads (ads.json in the repository), as the server last
// read them. Fetched once per page load and shared by every slot.
let feed: Promise<Ad[]> | null = null
function useAds(): Ad[] {
  const [ads, setAds] = useState<Ad[]>([])
  useEffect(() => {
    feed ??= api.get<{ ads: Ad[] }>('/api/ads').then((r) => r.ads ?? []).catch(() => {
      feed = null
      return []
    })
    let alive = true
    feed.then((a) => alive && setAds(a))
    return () => { alive = false }
  }, [])
  return ads
}

// Dark or light text on the ad's own colour, whichever reads better.
function textOn(hex: string) {
  const v = parseInt(hex.slice(1), 16)
  const [r, g, b] = [(v >> 16) & 255, (v >> 8) & 255, v & 255]
  return 0.299 * r + 0.587 * g + 0.114 * b > 150 ? '#0b0f0c' : '#ffffff'
}

const hideKey = (id: string) => `neo-ad-hidden-${id}`
const HIDE_FOR = 3 * 24 * 3600 * 1000

function hiddenNow(id: string) {
  try {
    const at = Number(localStorage.getItem(hideKey(id)) || 0)
    return at > 0 && Date.now() - at < HIDE_FOR
  } catch {
    return false
  }
}

/** One ad for a place, by weight; the same one for the whole visit. */
function choose(ads: Ad[], placement: string, lang: string): Ad | null {
  const fit = ads.filter((a) => (!a.placements?.length || a.placements.includes(placement)) && (!a.lang || a.lang === lang) && !hiddenNow(a.id))
  if (!fit.length) return null
  const key = `neo-ad-${placement}-${lang}`
  try {
    const kept = sessionStorage.getItem(key)
    const again = fit.find((a) => a.id === kept)
    if (again) return again
  } catch { /* storage off */ }
  const total = fit.reduce((s, a) => s + Math.max(a.weight ?? 1, 1), 0)
  let r = Math.random() * total
  let pick = fit[0]
  for (const a of fit) {
    r -= Math.max(a.weight ?? 1, 1)
    if (r < 0) { pick = a; break }
  }
  try { sessionStorage.setItem(key, pick.id) } catch { /* storage off */ }
  return pick
}

/** A text ad: on the dashboard as a wide card, in the side menu as a small one. */
export function AdCard({ placement, className }: { placement: 'dashboard' | 'sidebar'; className?: string }) {
  const t = useT()
  const lang = useLang((s) => s.lang)
  const ads = useAds()
  const [closed, setClosed] = useState(false)
  const ad = useMemo(() => choose(ads, placement, lang), [ads, placement, lang])
  if (!ad || closed) return null
  const style = (ad.color ? { '--ad': ad.color, '--ad-fg': textOn(ad.color) } : { '--ad': 'var(--accent)', '--ad-fg': 'var(--on-accent)' }) as CSSProperties
  const badge = ad.badge || t('Sponsored')
  const link = ad.url ? { href: ad.url, target: '_blank', rel: 'noopener noreferrer sponsored' } : null

  if (placement === 'sidebar') {
    return (
      <div style={style} className={clsx('relative rounded-2xl border border-line bg-surface-2/60 p-3 overflow-hidden', className)}>
        <span className="absolute inset-y-0 start-0 w-1 bg-[var(--ad)]" aria-hidden />
        <p className="text-[10.5px] uppercase tracking-wider text-subtle font-semibold" dir="auto">{badge}</p>
        <p className="mt-1 text-[13px] font-semibold leading-snug line-clamp-2" dir="auto">{ad.title}</p>
        {link && (
          <a {...link} className="mt-2 inline-flex items-center gap-1 text-[12.5px] font-medium text-[var(--ad)] hover:underline" dir="auto">
            {ad.button || t('Learn more')}<ArrowUpRight className="size-3.5 rtl:-scale-x-100" />
          </a>
        )}
      </div>
    )
  }

  const hide = () => {
    try { localStorage.setItem(hideKey(ad.id), String(Date.now())) } catch { /* storage off */ }
    setClosed(true)
  }
  return (
    <section style={style} aria-label={badge} className={clsx('card relative overflow-hidden p-5 flex flex-wrap items-center gap-4', className)}>
      <div className="absolute inset-0 pointer-events-none" aria-hidden style={{ background: 'radial-gradient(60% 140% at 0% 50%, color-mix(in srgb, var(--ad) 14%, transparent), transparent 70%)' }} />
      <span className="absolute inset-y-0 start-0 w-1 bg-[var(--ad)]" aria-hidden />
      <div className="relative min-w-0 flex-1 basis-64">
        <span className="inline-flex items-center h-6 px-2 rounded-md text-[11px] font-semibold uppercase tracking-wider border border-line text-muted" dir="auto">{badge}</span>
        <p className="mt-2 font-display text-lg font-semibold leading-snug" dir="auto">{ad.title}</p>
        {ad.text && <p className="mt-1 text-sm text-muted leading-relaxed" dir="auto">{ad.text}</p>}
      </div>
      {link && (
        <a {...link} className="relative inline-flex items-center gap-1.5 h-10 px-4 rounded-xl text-sm font-semibold text-[var(--ad-fg)] bg-[var(--ad)] hover:brightness-110 transition" dir="auto">
          {ad.button || t('Learn more')}<ArrowUpRight className="size-4 rtl:-scale-x-100" />
        </a>
      )}
      <button type="button" onClick={hide} aria-label={t('Hide for a few days')} title={t('Hide for a few days')}
        className="absolute top-2.5 end-2.5 grid place-items-center size-7 rounded-lg text-subtle hover:text-fg hover:bg-surface-2">
        <X className="size-3.5" />
      </button>
    </section>
  )
}
