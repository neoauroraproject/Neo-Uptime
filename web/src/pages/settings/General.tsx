import { useEffect, useMemo, useState } from 'react'
import { Monitor, Moon, Save, Sun } from 'lucide-react'
import { api } from '../../lib/api'
import { useApp } from '../../lib/store'
import { useAdminTheme, type AdminTheme } from '../../lib/theme'
import { bytes, clsx } from '../../lib/format'
import { useLang, useT, type Lang } from '../../lib/i18n'
import type { StatusPage } from '../../lib/types'
import { Alert, Button, Card, CardHeader, Field, Input, Select, Skeleton, Switch, confirm, toast } from '../../components/ui'

type S = Record<string, string>

function useSettings() {
  const t = useT()
  const [s, setS] = useState<S | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => { api.get<S>('/api/settings').then(setS).catch(() => setS({})) }, [])
  const save = async (patch: S) => {
    setBusy(true)
    try {
      const r = await api.put<S>('/api/settings', patch)
      setS(r)
      await useApp.getState().loadSettings()
      toast.success(t('Settings saved'))
    } catch (e) {
      toast.error(t('Could not save'), (e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return { s, setS, save, busy }
}

export function General() {
  const t = useT()
  const { s, setS, save, busy } = useSettings()
  const [pages, setPages] = useState<StatusPage[]>([])
  const zones = useMemo(() => {
    try {
      return (Intl as unknown as { supportedValuesOf: (k: string) => string[] }).supportedValuesOf('timeZone')
    } catch {
      return ['UTC']
    }
  }, [])
  useEffect(() => { api.get<StatusPage[]>('/api/status-pages').then(setPages).catch(() => {}) }, [])
  if (!s) return <Skeleton className="h-96 rounded-[18px]" />
  const set = (k: string, v: string) => setS({ ...s, [k]: v })
  return (
    <Card>
      <CardHeader title={t('General')} subtitle={t('How this instance names itself and builds links.')} />
      <div className="px-5 pb-5 space-y-5">
        <Field label={t('Site title')} htmlFor="st"><Input id="st" dir="auto" value={s.siteTitle} onChange={(e) => set('siteTitle', e.target.value)} /></Field>
        <Field label={t('Public address')} htmlFor="pb" help={t('Used in alert links, push URLs and agent install commands, e.g. https://uptime.example.com')}
          aside={<button type="button" className="text-[12.5px] text-accent-ink hover:underline" onClick={() => set('primaryBaseURL', window.location.origin)}>{t('Use current')}</button>}>
          <Input id="pb" dir="ltr" className="font-mono" value={s.primaryBaseURL} onChange={(e) => set('primaryBaseURL', e.target.value)} placeholder="https://uptime.example.com" />
        </Field>
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label={t('Time zone (display and alerts)')} htmlFor="tz">
            <Select id="tz" value={s.timezone} onChange={(e) => set('timezone', e.target.value)}>
              <option value="auto">{t('Browser’s ({tz})', { tz: Intl.DateTimeFormat().resolvedOptions().timeZone })}</option>
              {zones.map((z) => <option key={z}>{z}</option>)}
            </Select>
          </Field>
          <Field label={t('Root address opens')} htmlFor="ep">
            <Select id="ep" value={s.entryPage} onChange={(e) => set('entryPage', e.target.value)}>
              <option value="dashboard">{t('The dashboard (sign-in)')}</option>
              {pages.map((p) => <option key={p.id} value={`status:${p.slug}`}>{t('Status page: {name}', { name: p.title })}</option>)}
            </Select>
          </Field>
        </div>
        <Switch checked={s.trustProxy === 'true'} onChange={(v) => set('trustProxy', String(v))} label={t('Behind a reverse proxy')} description={t('Trust X-Forwarded-For / CF-Connecting-IP for client addresses and HTTPS detection (nginx, Caddy, Cloudflare).')} />
        <Switch checked={s.searchEngineIndex === 'true'} onChange={(v) => set('searchEngineIndex', String(v))} label={t('Let search engines index status pages')} />
        <div className="flex justify-end"><Button variant="primary" loading={busy} icon={<Save className="size-4" />} onClick={() => save({ siteTitle: s.siteTitle, primaryBaseURL: s.primaryBaseURL, timezone: s.timezone, entryPage: s.entryPage, trustProxy: s.trustProxy, searchEngineIndex: s.searchEngineIndex })}>{t('Save')}</Button></div>
      </div>
    </Card>
  )
}

export function Appearance() {
  const t = useT()
  const [theme, setTheme] = useAdminTheme()
  const lang = useLang((s) => s.lang)
  const setLang = useLang((s) => s.setLang)
  const opts: { v: AdminTheme; label: string; icon: typeof Sun; desc: string }[] = [
    { v: 'dark', label: t('Dark'), icon: Moon, desc: t('Deep black with a green glow') },
    { v: 'light', label: t('Light'), icon: Sun, desc: t('Soft grey canvas, white cards') },
    { v: 'system', label: t('System'), icon: Monitor, desc: t('Follow this device') },
  ]
  const langs: { v: Lang; label: string; desc: string }[] = [
    { v: 'fa', label: 'فارسی', desc: 'راست‌به‌چپ، اعداد فارسی و تاریخ شمسی' },
    { v: 'en', label: 'English', desc: 'Left-to-right, Gregorian calendar' },
  ]
  return (
    <>
      <Card>
        <CardHeader title={t('Language')} subtitle={t('For this browser. Status pages have their own language setting.')} />
        <div className="px-5 pb-5 grid sm:grid-cols-2 gap-3" role="radiogroup" aria-label={t('Language')}>
          {langs.map((o) => (
            <button key={o.v} role="radio" aria-checked={lang === o.v} onClick={() => setLang(o.v)} dir={o.v === 'fa' ? 'rtl' : 'ltr'}
              className={clsx('text-start rounded-2xl border-2 p-4 transition-colors', lang === o.v ? 'border-accent bg-accent-soft' : 'border-line hover:border-line-strong')}>
              <p className="font-semibold text-lg" style={{ fontFamily: o.v === 'fa' ? '"Vazirmatn Variable"' : undefined }}>{o.label}</p>
              <p className="text-[12.5px] text-muted mt-0.5" style={{ fontFamily: o.v === 'fa' ? '"Vazirmatn Variable"' : undefined }}>{o.desc}</p>
            </button>
          ))}
        </div>
      </Card>
      <Card>
        <CardHeader title={t('Appearance')} subtitle={t('Applies to this browser. Status pages have their own themes.')} />
        <div className="px-5 pb-5 grid sm:grid-cols-3 gap-3" role="radiogroup" aria-label={t('Admin theme')}>
          {opts.map((o) => (
            <button key={o.v} role="radio" aria-checked={theme === o.v} onClick={() => setTheme(o.v)}
              className={clsx('text-start rounded-2xl border-2 p-4 transition-colors', theme === o.v ? 'border-accent bg-accent-soft' : 'border-line hover:border-line-strong')}>
              <o.icon className="size-5 text-muted" />
              <p className="mt-3 font-semibold">{o.label}</p>
              <p className="text-[12.5px] text-muted">{o.desc}</p>
            </button>
          ))}
        </div>
      </Card>
    </>
  )
}

export function History() {
  const t = useT()
  const { s, setS, save, busy } = useSettings()
  const [info, setInfo] = useState<{ dbSize: number } | null>(null)
  const [vac, setVac] = useState(false)
  useEffect(() => { api.get<{ dbSize: number }>('/api/settings/info').then(setInfo).catch(() => {}) }, [])
  if (!s) return <Skeleton className="h-72 rounded-[18px]" />
  return (
    <>
      <Card>
        <CardHeader title={t('Monitor history')} subtitle={t('Raw checks are pruned after this; daily uptime figures are kept.')} />
        <div className="px-5 pb-5 space-y-5">
          <div className="grid sm:grid-cols-2 gap-4">
            <Field label={t('Keep check history for')} htmlFor="kd" help={t('Default 180 days.')}>
              <div className="relative"><Input id="kd" dir="ltr" type="number" min={1} max={3650} value={s.keepDataDays} onChange={(e) => setS({ ...s, keepDataDays: e.target.value })} className="pe-14" /><span className="absolute end-3 top-1/2 -translate-y-1/2 text-[13px] text-subtle">{t('days')}</span></div>
            </Field>
            <Field label={t('Certificate warnings (days before expiry)')} htmlFor="tl" help={t('Comma separated, e.g. 7,14,21')}>
              <Input id="tl" dir="ltr" value={s.tlsExpiryNotifyDays} onChange={(e) => setS({ ...s, tlsExpiryNotifyDays: e.target.value })} />
            </Field>
          </div>
          <div className="flex justify-end"><Button variant="primary" loading={busy} icon={<Save className="size-4" />} onClick={() => save({ keepDataDays: s.keepDataDays, tlsExpiryNotifyDays: s.tlsExpiryNotifyDays })}>{t('Save')}</Button></div>
        </div>
      </Card>
      <Card>
        <CardHeader title={t('Database')} subtitle={info ? t('Currently {size} on disk.', { size: bytes(info.dbSize) }) : t('Loading…')} />
        <div className="px-5 pb-5 flex flex-wrap gap-2">
          <Button loading={vac} onClick={async () => {
            setVac(true)
            try {
              const r = await api.post<{ dbSize: number }>('/api/settings/vacuum')
              setInfo(r)
              toast.success(t('Database compacted'), t('Now {size}', { size: bytes(r.dbSize) }))
            } finally { setVac(false) }
          }}>{t('Compact (VACUUM)')}</Button>
          <Button variant="danger" onClick={async () => {
            if (!(await confirm({ title: t('Clear all statistics?'), body: t('Every heartbeat and every uptime figure is deleted. Monitors, pages and settings stay.'), confirmLabel: t('Clear everything'), danger: true }))) return
            await api.post('/api/settings/clear-statistics')
            toast.success(t('Statistics cleared'))
            useApp.getState().loadDashboard()
          }}>{t('Clear all statistics')}</Button>
        </div>
        <div className="px-5 pb-5"><Alert tone="info">{t('Uptime figures come from hourly and daily aggregates, so even with short raw history the 30-day and 1-year numbers stay exact.')}</Alert></div>
      </Card>
    </>
  )
}
