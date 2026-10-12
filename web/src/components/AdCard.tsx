import { useEffect, useMemo, useState } from 'react'
import { ArrowUpRight, X } from 'lucide-react'
import { api } from '../lib/api'
import { clsx } from '../lib/format'
import { useT } from '../lib/i18n'
import type { Ad } from '../lib/types'
import { CHANNEL_URL } from './brand'

// Ads from Neo Aurora's ad service, as the server last read them. Fetched
// once per page load and shared by every page.
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

/** One ad, the same one on every page during a visit. */
function choose(ads: Ad[]): Ad | null {
  const fit = ads.filter((a) => !hiddenNow(a.id))
  if (!fit.length) return null
  try {
    const kept = sessionStorage.getItem('neo-ad')
    const again = fit.find((a) => a.id === kept)
    if (again) return again
  } catch { /* storage off */ }
  const pick = fit[Math.floor(Math.random() * fit.length)]
  try { sessionStorage.setItem('neo-ad', pick.id) } catch { /* storage off */ }
  return pick
}

/** The ad card at the top of each page; it can be closed for a few days. */
export function AdCard({ className }: { className?: string }) {
  const t = useT()
  const ads = useAds()
  const [closed, setClosed] = useState(false)
  const ad = useMemo(() => choose(ads), [ads])
  if (!ad || closed) return null
  const badge = ad.badge || t('Sponsored')
  const hide = () => {
    try { localStorage.setItem(hideKey(ad.id), String(Date.now())) } catch { /* storage off */ }
    setClosed(true)
  }
  return (
    <section aria-label={badge} className={clsx('card relative overflow-hidden p-5 flex flex-wrap items-center gap-4', className)}>
      <div className="absolute inset-0 pointer-events-none" aria-hidden style={{ background: 'radial-gradient(60% 140% at 0% 50%, color-mix(in srgb, var(--up) 12%, transparent), transparent 70%)' }} />
      <span className="absolute inset-y-0 start-0 w-1 bg-up" aria-hidden />
      <div className="relative min-w-0 flex-1 basis-64">
        <span className="inline-flex items-center h-6 px-2 rounded-md text-[11px] font-semibold tracking-wider border border-line text-muted" dir="auto">{badge}</span>
        <p className="mt-2 font-display text-lg font-semibold leading-snug" dir="auto">{ad.title}</p>
        {ad.text && <p className="mt-1 text-sm text-muted leading-relaxed whitespace-pre-line" dir="auto">{ad.text}</p>}
      </div>
      {ad.url && (
        <a href={ad.url} target="_blank" rel="noopener noreferrer sponsored" dir="auto"
          className="relative inline-flex items-center gap-1.5 h-10 px-4 rounded-xl text-sm font-semibold text-white bg-up hover:brightness-110 transition">
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

/** Our own Telegram channel, in the side menu. */
export function ChannelCard({ className }: { className?: string }) {
  const t = useT()
  return (
    <div className={clsx('relative rounded-2xl border border-line bg-surface-2/60 p-3 overflow-hidden', className)}>
      <span className="absolute inset-y-0 start-0 w-1 bg-up" aria-hidden />
      <p className="text-[10.5px] uppercase tracking-wider text-subtle font-semibold">{t('News')}</p>
      <p className="mt-1 text-[13px] font-semibold leading-snug">{t('Neo Aurora Telegram channel')}</p>
      <a href={CHANNEL_URL} target="_blank" rel="noopener noreferrer" className="mt-2 inline-flex items-center gap-1 text-[12.5px] font-medium text-up hover:underline">
        {t('Join the channel')}<ArrowUpRight className="size-3.5 rtl:-scale-x-100" />
      </a>
    </div>
  )
}
