import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Radar } from 'lucide-react'
import { api } from '../lib/api'
import { useApp } from '../lib/store'
import { clsx } from '../lib/format'
import { useT } from '../lib/i18n'
import type { RadarSnapshot, StatusPage } from '../lib/types'
import { Switch } from '../components/ui'
import { useDCName } from '../components/radar'

type Value = StatusPage['radar']

/**
 * A status page's internet radar strip: on or off, and which datacenters it
 * shows (none picked = every one).
 */
export function StatusRadarField({ value, onChange }: { value: Value | undefined; onChange: (v: Value) => void }) {
  const t = useT()
  const name = useDCName()
  const role = useApp((s) => s.role)
  const v: Value = value ?? { show: false, datacenters: [] }
  const [info, setInfo] = useState<{ enabled: boolean; snapshot: RadarSnapshot | null } | null>(null)
  useEffect(() => { api.get<{ enabled: boolean; snapshot: RadarSnapshot | null }>('/api/radar').then(setInfo).catch(() => {}) }, [])
  const dcs = info?.snapshot?.datacenters ?? []
  const picked = (slug: string) => v.datacenters.length === 0 || v.datacenters.includes(slug)
  const toggle = (slug: string) => {
    const now = dcs.map((d) => d.slug).filter(picked)
    const next = now.includes(slug) ? now.filter((s) => s !== slug) : [...now, slug]
    // Every one picked is stored as "all", so new datacenters show too.
    onChange({ ...v, datacenters: next.length === dcs.length ? [] : next })
  }
  return (
    <div className="rounded-2xl border border-line bg-surface-2/40 p-4 space-y-3">
      <div className="flex items-start gap-3">
        <Radar className="size-4 mt-1 text-accent-ink shrink-0" />
        <div className="flex-1 min-w-0">
          <Switch checked={v.show} onChange={(show) => onChange({ ...v, show })} label={t('Internet radar')}
            description={t('A small strip that says whether Iran’s datacenters reach the international internet; visitors open it for each datacenter.')} />
        </div>
      </div>
      {v.show && info && !info.enabled && (
        <p className="text-[12.5px] text-pending">
          {t('The radar is off for the whole panel, so the strip does not show yet.')}{' '}
          {role === 'admin' && <Link to="/settings/general" className="underline">{t('Turn it on in Settings')}</Link>}
        </p>
      )}
      {v.show && dcs.length > 0 && (
        <div>
          <p className="text-[12.5px] text-muted mb-2">{t('Datacenters on this page')}</p>
          <div className="flex flex-wrap gap-1.5">
            {dcs.map((d) => (
              <button key={d.slug} type="button" role="checkbox" aria-checked={picked(d.slug)} onClick={() => toggle(d.slug)}
                className={clsx('h-8 px-3 rounded-full border text-[12.5px] transition-colors', picked(d.slug) ? 'border-accent/50 bg-accent-soft text-accent-ink' : 'border-line text-muted hover:text-fg')}>
                {name(d)}{!d.internal && <span className="ms-1 text-subtle">↗</span>}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
