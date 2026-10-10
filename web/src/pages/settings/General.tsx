import { useEffect, useMemo, useState } from 'react'
import { Monitor, Moon, Save, Sun } from 'lucide-react'
import { api } from '../../lib/api'
import { useApp } from '../../lib/store'
import { useAdminTheme, type AdminTheme } from '../../lib/theme'
import { bytes, clsx } from '../../lib/format'
import type { StatusPage } from '../../lib/types'
import { Alert, Button, Card, CardHeader, Field, Input, Select, Skeleton, Switch, confirm, toast } from '../../components/ui'

type S = Record<string, string>

function useSettings() {
  const [s, setS] = useState<S | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => { api.get<S>('/api/settings').then(setS).catch(() => setS({})) }, [])
  const save = async (patch: S) => {
    setBusy(true)
    try {
      const r = await api.put<S>('/api/settings', patch)
      setS(r)
      await useApp.getState().loadSettings()
      toast.success('Settings saved')
    } catch (e) {
      toast.error('Could not save', (e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return { s, setS, save, busy }
}

export function General() {
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
      <CardHeader title="General" subtitle="How this instance names itself and builds links." />
      <div className="px-5 pb-5 space-y-5">
        <Field label="Site title" htmlFor="st"><Input id="st" value={s.siteTitle} onChange={(e) => set('siteTitle', e.target.value)} /></Field>
        <Field label="Public address" htmlFor="pb" help="Used in alert links and push URLs, e.g. https://uptime.example.com"
          aside={<button type="button" className="text-[12.5px] text-accent-ink hover:underline" onClick={() => set('primaryBaseURL', window.location.origin)}>Use current</button>}>
          <Input id="pb" className="font-mono" value={s.primaryBaseURL} onChange={(e) => set('primaryBaseURL', e.target.value)} placeholder="https://uptime.example.com" />
        </Field>
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label="Display time zone" htmlFor="tz">
            <Select id="tz" value={s.timezone} onChange={(e) => set('timezone', e.target.value)}>
              <option value="auto">Browser's ({Intl.DateTimeFormat().resolvedOptions().timeZone})</option>
              {zones.map((z) => <option key={z}>{z}</option>)}
            </Select>
          </Field>
          <Field label="Root address opens" htmlFor="ep">
            <Select id="ep" value={s.entryPage} onChange={(e) => set('entryPage', e.target.value)}>
              <option value="dashboard">The dashboard (sign-in)</option>
              {pages.map((p) => <option key={p.id} value={`status:${p.slug}`}>Status page: {p.title}</option>)}
            </Select>
          </Field>
        </div>
        <Switch checked={s.trustProxy === 'true'} onChange={(v) => set('trustProxy', String(v))} label="Behind a reverse proxy" description="Trust X-Forwarded-For / CF-Connecting-IP for client addresses and HTTPS detection (nginx, Caddy, Cloudflare)." />
        <Switch checked={s.searchEngineIndex === 'true'} onChange={(v) => set('searchEngineIndex', String(v))} label="Let search engines index status pages" />
        <div className="flex justify-end"><Button variant="primary" loading={busy} icon={<Save className="size-4" />} onClick={() => save({ siteTitle: s.siteTitle, primaryBaseURL: s.primaryBaseURL, timezone: s.timezone, entryPage: s.entryPage, trustProxy: s.trustProxy, searchEngineIndex: s.searchEngineIndex })}>Save</Button></div>
      </div>
    </Card>
  )
}

export function Appearance() {
  const [theme, setTheme] = useAdminTheme()
  const opts: { v: AdminTheme; label: string; icon: typeof Sun; desc: string }[] = [
    { v: 'dark', label: 'Dark', icon: Moon, desc: 'Deep black with a green glow' },
    { v: 'light', label: 'Light', icon: Sun, desc: 'Soft grey canvas, white cards' },
    { v: 'system', label: 'System', icon: Monitor, desc: 'Follow this device' },
  ]
  return (
    <Card>
      <CardHeader title="Appearance" subtitle="Applies to this browser. Status pages have their own themes." />
      <div className="px-5 pb-5 grid sm:grid-cols-3 gap-3" role="radiogroup" aria-label="Admin theme">
        {opts.map((o) => (
          <button key={o.v} role="radio" aria-checked={theme === o.v} onClick={() => setTheme(o.v)}
            className={clsx('text-left rounded-2xl border-2 p-4 transition-colors', theme === o.v ? 'border-accent bg-accent-soft' : 'border-line hover:border-line-strong')}>
            <o.icon className="size-5 text-muted" />
            <p className="mt-3 font-semibold">{o.label}</p>
            <p className="text-[12.5px] text-muted">{o.desc}</p>
          </button>
        ))}
      </div>
    </Card>
  )
}

export function History() {
  const { s, setS, save, busy } = useSettings()
  const [info, setInfo] = useState<{ dbSize: number } | null>(null)
  const [vac, setVac] = useState(false)
  useEffect(() => { api.get<{ dbSize: number }>('/api/settings/info').then(setInfo).catch(() => {}) }, [])
  if (!s) return <Skeleton className="h-72 rounded-[18px]" />
  return (
    <>
      <Card>
        <CardHeader title="Monitor history" subtitle="Raw checks are pruned after this; daily uptime figures are kept." />
        <div className="px-5 pb-5 space-y-5">
          <div className="grid sm:grid-cols-2 gap-4">
            <Field label="Keep check history for" htmlFor="kd" help="Default 180 days.">
              <div className="relative"><Input id="kd" type="number" min={1} max={3650} value={s.keepDataDays} onChange={(e) => setS({ ...s, keepDataDays: e.target.value })} className="pr-14" /><span className="absolute right-3 top-1/2 -translate-y-1/2 text-[13px] text-subtle">days</span></div>
            </Field>
            <Field label="Certificate warnings (days before expiry)" htmlFor="tl" help="Comma separated, e.g. 7,14,21">
              <Input id="tl" value={s.tlsExpiryNotifyDays} onChange={(e) => setS({ ...s, tlsExpiryNotifyDays: e.target.value })} />
            </Field>
          </div>
          <div className="flex justify-end"><Button variant="primary" loading={busy} icon={<Save className="size-4" />} onClick={() => save({ keepDataDays: s.keepDataDays, tlsExpiryNotifyDays: s.tlsExpiryNotifyDays })}>Save</Button></div>
        </div>
      </Card>
      <Card>
        <CardHeader title="Database" subtitle={info ? `Currently ${bytes(info.dbSize)} on disk.` : 'Loading…'} />
        <div className="px-5 pb-5 flex flex-wrap gap-2">
          <Button loading={vac} onClick={async () => {
            setVac(true)
            try {
              const r = await api.post<{ dbSize: number }>('/api/settings/vacuum')
              setInfo(r)
              toast.success('Database compacted', `Now ${bytes(r.dbSize)}`)
            } finally { setVac(false) }
          }}>Compact (VACUUM)</Button>
          <Button variant="danger" onClick={async () => {
            if (!(await confirm({ title: 'Clear all statistics?', body: 'Every heartbeat and every uptime figure is deleted. Monitors, pages and settings stay.', confirmLabel: 'Clear everything', danger: true }))) return
            await api.post('/api/settings/clear-statistics')
            toast.success('Statistics cleared')
            useApp.getState().loadDashboard()
          }}>Clear all statistics</Button>
        </div>
        <div className="px-5 pb-5"><Alert tone="info">Uptime figures come from hourly and daily aggregates, so even with short raw history the 30-day and 1-year numbers stay exact.</Alert></div>
      </Card>
    </>
  )
}
