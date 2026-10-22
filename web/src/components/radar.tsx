import { useState } from 'react'
import { ChevronDown, ExternalLink, Radar } from 'lucide-react'
import { clsx, n, timeAgo } from '../lib/format'
import { useLang, useT } from '../lib/i18n'
import type { RadarDC, RadarSnapshot } from '../lib/types'

/*
 * The internet radar strip: Chabokan's radar, as the panel last read it.
 * Closed it says in one line whether Iran's datacenters have the
 * international internet; open it shows each datacenter. It only uses the
 * page's colour variables, so it takes on every status page theme.
 */

const stateColor: Record<RadarDC['state'], string> = { ok: 'var(--up)', partial: 'var(--pending)', down: 'var(--down)', unknown: 'var(--text-3)' }
const stateWord: Record<RadarDC['state'], string> = { ok: 'Normal', partial: 'Disrupted', down: 'Cut off', unknown: 'No data' }

// The radar's datacenters, as people in Iran call them.
const faNames: [RegExp, string][] = [
  [/^asiatech/, 'آسیاتک'], [/^hiweb/, 'های‌وب'], [/^sefroyek/, 'صفرویک'], [/^shatel/, 'شاتل'], [/^amin/, 'امین'],
  [/^irancell/, 'ایرانسل'], [/^mci|^hamrah/, 'همراه اول'], [/^mokhaberat|^tci/, 'مخابرات'], [/^afranet/, 'افرانت'],
  [/^respina/, 'رسپینا'], [/^pars-?online/, 'پارس‌آنلاین'], [/^mobinnet/, 'مبین‌نت'], [/^arvan/, 'ابرآروان'],
  [/^hetzner/, 'هتزنر'], [/^ovh/, 'OVH'],
]
const faPlaces: [RegExp, string][] = [
  [/tehran/, 'تهران'], [/karaj/, 'کرج'], [/qom/, 'قم'], [/mashhad/, 'مشهد'], [/tabriz/, 'تبریز'], [/isfahan|esfahan/, 'اصفهان'], [/shiraz/, 'شیراز'],
  [/germany/, 'آلمان'], [/finland/, 'فنلاند'], [/france/, 'فرانسه'], [/netherlands/, 'هلند'], [/turkey/, 'ترکیه'], [/uae|dubai/, 'امارات'],
]

export function useDCName() {
  const lang = useLang((s) => s.lang)
  return (dc: RadarDC, withPlace = true) => {
    if (lang !== 'fa') return dc.name
    const slug = dc.slug.toLowerCase()
    const name = faNames.find(([re]) => re.test(slug))?.[1]
    if (!name) return dc.name
    const place = withPlace ? faPlaces.find(([re]) => re.test(slug))?.[1] : ''
    return place ? `${name} ${place}` : name
  }
}

/** One line about the datacenters in Iran: are they reaching the world? */
function useHeadline(r: RadarSnapshot) {
  const t = useT()
  const name = useDCName()
  const home = r.datacenters.filter((d) => d.internal && d.state !== 'unknown')
  const list = home.length ? home : r.datacenters.filter((d) => d.state !== 'unknown')
  const bad = list.filter((d) => d.state !== 'ok')
  if (!list.length) return { color: stateColor.unknown, text: t('Internet radar: waiting for data') }
  if (!bad.length) return { color: stateColor.ok, text: home.length ? t('International internet from Iran’s datacenters: normal') : t('No disruption') }
  if (home.length && bad.length === home.length && bad.every((d) => d.state === 'down')) {
    return { color: stateColor.down, text: t('International internet cut off in Iran’s datacenters') }
  }
  const names = bad.slice(0, 2).map((d) => name(d, false)).join('، ')
  const more = bad.length > 2 ? ` ${t('and {n} more', { n: bad.length - 2 })}` : ''
  return { color: bad.some((d) => d.state === 'down') ? stateColor.down : stateColor.partial, text: t('Disruption at {names}', { names: names + more }) }
}

function History({ h }: { h: number[] }) {
  return (
    <div className="flex items-end gap-[3px] h-5" dir="ltr" aria-hidden>
      {h.map((v, i) => (
        <span key={i} className="flex-1 rounded-[2px] min-w-[4px]" style={{
          height: v < 0 ? '30%' : `${Math.max(30, v * 100)}%`,
          background: v < 0 ? 'var(--border-strong)' : v >= 0.8 ? stateColor.ok : v >= 0.3 ? stateColor.partial : stateColor.down,
          opacity: v < 0 ? 0.5 : 0.85,
        }} />
      ))}
    </div>
  )
}

function DCCard({ dc }: { dc: RadarDC }) {
  const t = useT()
  const name = useDCName()
  const c = stateColor[dc.state]
  return (
    <div className="rounded-xl border px-3.5 py-3 min-w-0" style={{ borderColor: dc.state === 'ok' || dc.state === 'unknown' ? 'var(--border)' : `color-mix(in srgb, ${c} 40%, transparent)` }}>
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium text-[13.5px] truncate" dir="auto">{name(dc)}</span>
        <span className="inline-flex items-center gap-1.5 text-[12px] font-medium shrink-0" style={{ color: c }}>
          <span className="size-2 rounded-full" style={{ background: c }} />{t(stateWord[dc.state])}
        </span>
      </div>
      {dc.state !== 'unknown' && (
        <>
          <p className="mt-1 text-[12px] text-muted tnum">
            {t('{up} of {total} services', { up: dc.up, total: dc.total })}{dc.latency > 0 ? ` · ${n(dc.latency)} ms` : ''}
          </p>
          <div className="mt-2"><History h={dc.history} /></div>
          {dc.down && dc.down.length > 0 && (
            <p className="mt-2 text-[11.5px] text-subtle leading-relaxed" dir="ltr">{t('Not reachable')}: {dc.down.join(', ')}</p>
          )}
        </>
      )}
    </div>
  )
}

export function RadarStrip({ r, rounded = 'rounded-2xl', now = Date.now() }: { r: RadarSnapshot; rounded?: string; now?: number }) {
  const t = useT()
  const name = useDCName()
  const [open, setOpen] = useState(false)
  const head = useHeadline(r)
  const home = r.datacenters.filter((d) => d.internal)
  const away = r.datacenters.filter((d) => !d.internal)
  return (
    <section className={clsx('border bg-surface overflow-hidden', rounded)} style={{ borderColor: head.color === stateColor.ok ? 'var(--border)' : `color-mix(in srgb, ${head.color} 35%, transparent)` }}>
      <button type="button" onClick={() => setOpen(!open)} aria-expanded={open}
        className="w-full flex items-center gap-3 px-4 sm:px-5 py-3 text-start hover:bg-surface-2/50 transition-colors">
        <span className="relative grid place-items-center size-7 shrink-0 rounded-full" style={{ background: `color-mix(in srgb, ${head.color} 14%, transparent)`, color: head.color }}>
          <Radar className="size-4" />
          {head.color !== stateColor.ok && <span className="absolute inset-0 rounded-full animate-ping opacity-30" style={{ boxShadow: `0 0 0 2px ${head.color}` }} aria-hidden />}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-[13.5px] font-medium leading-snug" dir="auto">{head.text}</span>
          <span className="flex flex-wrap items-center gap-x-2.5 gap-y-0.5 mt-0.5">
            {home.map((d) => (
              <span key={d.slug} className="inline-flex items-center gap-1 text-[11.5px] text-subtle" title={`${name(d)} · ${t(stateWord[d.state])}`}>
                <span className="size-1.5 rounded-full" style={{ background: stateColor[d.state] }} />{name(d, false)}
              </span>
            ))}
          </span>
        </span>
        <ChevronDown className={clsx('size-4 text-subtle shrink-0 transition-transform', open && 'rotate-180')} />
      </button>
      {open && (
        <div className="border-t px-4 sm:px-5 py-4 space-y-4" style={{ borderColor: 'var(--border)' }}>
          {home.length > 0 && (
            <div>
              <p className="text-[12px] text-muted mb-2">{t('From datacenters in Iran — reaching foreign services')}</p>
              <div className="grid sm:grid-cols-2 gap-2.5">{home.map((d) => <DCCard key={d.slug} dc={d} />)}</div>
            </div>
          )}
          {away.length > 0 && (
            <div>
              <p className="text-[12px] text-muted mb-2">{t('From abroad — reaching Iranian services')}</p>
              <div className="grid sm:grid-cols-2 gap-2.5">{away.map((d) => <DCCard key={d.slug} dc={d} />)}</div>
            </div>
          )}
          <p className="flex flex-wrap items-center justify-between gap-2 text-[11.5px] text-subtle">
            <span>{t('Last hour, in 5-minute steps · updated {ago}', { ago: timeAgo(r.at, now) })}</span>
            <a href={r.link} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 hover:underline">{t('Data: Chabokan radar')}<ExternalLink className="size-3" /></a>
          </p>
        </div>
      )}
    </section>
  )
}
