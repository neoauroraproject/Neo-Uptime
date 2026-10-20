import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, CircleCheck, CircleDashed, ExternalLink, Eye, EyeOff, Globe, ImageUp, LockKeyhole, Megaphone, Save, Trash2, TriangleAlert, X } from 'lucide-react'
import { api, uploadImage } from '../lib/api'
import { useApp } from '../lib/store'
import { clsx, fmtDate, fmtDateTime } from '../lib/format'
import { useT } from '../lib/i18n'
import type { HideKey, HttpsStatus, Incident, StatusPage, Theme } from '../lib/types'
import { Alert, Badge, Card, CopyButton, Field, IconButton, Input, Segmented, Select, Skeleton, Spinner, Switch, Textarea, Button, confirm, toast } from '../components/ui'
import { ThemeSwatch, domainURL, pageURL, slugify, themeMeta } from './StatusPages'
import { MonitorsTab } from './StatusMonitorsTab'

type Tab = 'general' | 'appearance' | 'monitors' | 'incidents'

function LogoField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const t = useT()
  const file = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const pick = async (f: File) => {
    setBusy(true)
    try {
      onChange(await uploadImage(f))
    } catch (e) {
      toast.error(t('Upload failed'), (e as Error).message)
    } finally {
      setBusy(false)
      if (file.current) file.current.value = ''
    }
  }
  return (
    <Field label={t('Logo')} help={t('PNG, JPEG, WebP, SVG or ICO, up to 2 MB. Square works best; it is also the tab icon.')}>
      <div className="flex items-center gap-3">
        <div className="grid place-items-center size-14 shrink-0 rounded-2xl border border-line bg-surface-2 overflow-hidden">
          {value ? <img src={value} alt="" className="size-full object-contain" /> : <ImageUp className="size-5 text-subtle" />}
        </div>
        <div className="flex flex-wrap gap-2">
          <input ref={file} type="file" accept="image/png,image/jpeg,image/webp,image/gif,image/svg+xml,image/x-icon,.ico" className="hidden" onChange={(e) => e.target.files?.[0] && pick(e.target.files[0])} />
          <Button size="sm" loading={busy} icon={<ImageUp className="size-3.5" />} onClick={() => file.current?.click()}>{value ? t('Replace') : t('Upload logo')}</Button>
          {value && <Button size="sm" variant="ghost" icon={<X className="size-3.5" />} onClick={() => onChange('')}>{t('Remove')}</Button>}
        </div>
      </div>
      <Input dir="ltr" className="mt-2 font-mono text-[12.5px]" value={value} onChange={(e) => onChange(e.target.value)} placeholder={t('…or paste an image address')} aria-label={t('Logo address')} />
    </Field>
  )
}

function DomainState({ domain, https, onRefresh }: { domain: string; https: HttpsStatus | null | undefined; onRefresh: () => void }) {
  const t = useT()
  const role = useApp((s) => s.role)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const d0 = https?.domains.find((x) => x.domain === domain)
  // While a certificate is on its way, keep checking.
  useEffect(() => {
    if (!busy && !d0?.issuing) return
    const h = setInterval(onRefresh, 3000)
    return () => clearInterval(h)
  }, [busy, d0?.issuing, onRefresh])
  useEffect(() => {
    if (busy && (d0?.issued || (d0?.error && !d0.issuing))) setBusy(false)
  }, [busy, d0?.issued, d0?.error, d0?.issuing])
  if (!domain) return null
  const getSSL = async () => {
    setErr('')
    setBusy(true)
    try {
      await api.post('/api/https/issue', { domain, enable: true })
      toast.info(t('Requesting a certificate for {d}…', { d: domain }))
      setTimeout(onRefresh, 1500)
    } catch (e) {
      setErr((e as Error).message)
      setBusy(false)
    }
  }
  if (https === undefined) return <div className="flex items-center gap-2 text-[13px] text-muted"><Spinner className="size-3.5" />{t('Checking the domain…')}</div>
  const d = https?.domains.find((x) => x.domain === domain)
  const ip = https?.serverIps[0] ?? t('this server’s IP')
  return (
    <div className="rounded-2xl border border-line bg-surface-2/50 p-3.5 space-y-2.5 text-[13px]">
      <div>
        <p className="text-muted">{t('At your DNS provider add an A record:')}</p>
        <div className="mt-1.5 flex items-center gap-2"><code className="flex-1 min-w-0 truncate rounded-lg bg-surface border border-line px-2.5 py-1.5 font-mono text-[12.5px]" dir="ltr">{domain}  A  {ip}</code><CopyButton text={ip} /></div>
      </div>
      {d && (
        <div className="flex flex-wrap gap-x-4 gap-y-1.5">
          <span className={clsx('inline-flex items-center gap-1.5', d.pointsHere ? 'text-up' : 'text-pending')}>
            {d.pointsHere ? <CircleCheck className="size-4" /> : <TriangleAlert className="size-4" />}
            {d.pointsHere ? t('DNS points here') : d.ips.length ? t('DNS points to {ip}', { ip: d.ips.join(', ') }) : t('No DNS record yet')}
          </span>
          <span className={clsx('inline-flex items-center gap-1.5', d.issued ? 'text-up' : 'text-muted')}>
            {d.issued ? <LockKeyhole className="size-4" /> : d.issuing ? <Spinner className="size-3.5" /> : <CircleDashed className="size-4" />}
            {d.issued ? t('HTTPS until {d}', { d: fmtDate((d.notAfter ?? 0) * 1000) }) : d.issuing ? t('Getting a certificate…') : t('No certificate yet')}
          </span>
        </div>
      )}
      {/* A DNS problem is already shown above, in the page language. */}
      {d?.error && d.pointsHere && !d.issued && !busy && <p className="text-[12.5px] text-down" dir="auto">{d.error}</p>}
      {err && <p className="text-[12.5px] text-down" dir="auto">{err}</p>}
      <div className="flex flex-wrap items-center gap-2 pt-0.5">
        {role === 'admin' && !d?.issued && (
          <Button type="button" size="sm" variant="primary" loading={busy || d?.issuing} icon={<LockKeyhole className="size-3.5" />} onClick={getSSL}>
            {d?.pointsHere || !d ? t('Get SSL now') : t('Get SSL anyway')}
          </Button>
        )}
        {d?.issued && https?.running && (
          <a href={domainURL(domain, https)} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 text-[12.5px] text-accent-ink hover:underline" dir="ltr">
            <ExternalLink className="size-3.5" />{domainURL(domain, https).replace(/^https:\/\//, '')}
          </a>
        )}
        <button type="button" onClick={onRefresh} className="text-[12.5px] text-muted hover:text-fg hover:underline">{t('Check again')}</button>
      </div>
      {role === 'admin' && !d?.issued && !d?.pointsHere && d && (
        <p className="text-[12px] text-muted">{t('Let’s Encrypt can only issue once the A record points here; a failed try is allowed a few times an hour.')}</p>
      )}
      {role !== 'admin' && https && !https.enabled && <p className="text-muted">{t('An admin can turn on HTTPS for this domain.')}</p>}
    </div>
  )
}

type VisItem = { label: string; on: boolean; set: (v: boolean) => void; hint?: string }

function VisGroup({ title, note, items }: { title: string; note?: string; items: VisItem[] }) {
  const t = useT()
  const allOn = items.every((i) => i.on)
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[12px] uppercase tracking-wider font-semibold text-subtle">{title}</span>
        <button type="button" className="text-[12px] text-muted hover:text-fg" onClick={() => items.forEach((i) => i.set(!allOn))}>{allOn ? t('Hide all') : t('Show all')}</button>
      </div>
      {note && <p className="text-[12px] text-muted -mt-1">{note}</p>}
      <div className="grid grid-cols-2 gap-2">
        {items.map((i) => (
          <button key={i.label} type="button" role="switch" aria-checked={i.on} aria-label={i.label} title={i.hint} onClick={() => i.set(!i.on)}
            className={clsx('flex items-center gap-2 min-h-9 px-3 py-1.5 rounded-xl border text-[13px] text-start transition-colors', i.on ? 'border-accent/40 bg-accent-soft text-fg' : 'border-line text-subtle hover:text-muted hover:border-line-strong')}>
            {i.on ? <Eye className="size-4 shrink-0 text-accent-ink" /> : <EyeOff className="size-4 shrink-0" />}
            <span className={clsx('min-w-0', !i.on && 'line-through decoration-1')}>{i.label}</span>
          </button>
        ))}
      </div>
    </div>
  )
}

type BoolKey = 'showBanner' | 'showPoweredBy' | 'showLatency' | 'showUptime' | 'showCharts' | 'showTags' | 'showCertExpiry'

/** Every part of the page a visitor can see, each one on or off. */
function Visibility({ p, up }: { p: StatusPage; up: (patch: Partial<StatusPage>) => void }) {
  const t = useT()
  // "Hide all" changes several items in one click. `up` starts from this
  // render's page, so each change carries the ones made before it.
  let pending: Partial<StatusPage> = {}
  const change = (patch: Partial<StatusPage>) => {
    pending = { ...pending, ...patch }
    up(pending)
  }
  const hideItem = (k: HideKey, label: string, hint?: string): VisItem => ({
    label, hint, on: !p.hide?.includes(k),
    set: (v) => {
      const cur = (pending.hide ?? p.hide ?? []).filter((x) => x !== k)
      change({ hide: v ? cur : [...cur, k] })
    },
  })
  const flag = (key: BoolKey, label: string, hint?: string): VisItem => ({ label, hint, on: p[key], set: (v) => change({ [key]: v } as Partial<StatusPage>) })
  const cardsOnly = p.theme === 'zen' || p.layout === 'list'
  return (
    <div className="space-y-5 rounded-2xl border border-line p-4">
      <div>
        <p className="text-sm font-semibold">{t('What visitors see')}</p>
        <p className="text-[12.5px] text-muted mt-0.5">{t('Hidden details are not sent to visitors at all.')}</p>
      </div>
      <VisGroup title={t('Page')} items={[
        flag('showBanner', t('Overall status line'), t('“All systems operational” at the top.')),
        flag('showPoweredBy', t('“Powered by Neo Uptime”')),
      ]} />
      <VisGroup title={t('Every service')} items={[
        hideItem('flag', t('Flag')),
        hideItem('status', t('Status label'), t('Operational / Down next to the name.')),
        flag('showLatency', t('Response time')),
        flag('showUptime', t('Uptime percentage')),
        hideItem('bars', t('Status bars')),
        hideItem('graph', t('Small chart'), t('The response-time line on cards.')),
        flag('showCharts', t('Response time charts'), t('Visitors can expand a day / week / month chart per service.')),
        flag('showTags', t('Tags')),
        flag('showCertExpiry', t('Certificate expiry')),
      ]} />
      <VisGroup title={t('Tunnels')} note={cardsOnly ? t('Tunnel details show in the Cards layout.') : undefined} items={[
        hideItem('location', t('Location')),
        hideItem('direction', t('Mode (direct / reverse)')),
        hideItem('transport', t('Transport')),
        hideItem('profile', t('Profile')),
        hideItem('port', t('Port')),
        hideItem('traffic', t('Traffic')),
        hideItem('connected', t('Connected time')),
      ]} />
    </div>
  )
}

function IncidentsTab({ pageId }: { pageId: number }) {
  const t = useT()
  const [list, setList] = useState<Incident[] | null>(null)
  const [f, setF] = useState<Partial<Incident>>({ title: '', content: '', style: 'warning', pinned: true })
  const [busy, setBusy] = useState(false)
  const load = () => api.get<Incident[]>(`/api/status-pages/${pageId}/incidents`).then(setList).catch(() => setList([]))
  useEffect(() => { load() }, [pageId]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async () => {
    setBusy(true)
    try {
      if (f.id) await api.put(`/api/status-pages/${pageId}/incidents/${f.id}`, f)
      else await api.post(`/api/status-pages/${pageId}/incidents`, f)
      toast.success(f.id ? t('Incident updated') : t('Incident posted'))
      setF({ title: '', content: '', style: 'warning', pinned: true })
      load()
    } catch (e) {
      toast.error(t('Could not save'), (e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const styles: Incident['style'][] = ['info', 'warning', 'danger', 'success', 'maintenance']
  const styleLabel = (s: string) => t(s[0].toUpperCase() + s.slice(1))
  const tone = (s: string) => (s === 'danger' ? 'down' : s === 'warning' ? 'pending' : s === 'success' ? 'up' : 'maint') as 'down'
  return (
    <div className="space-y-5">
      <div className="space-y-4 rounded-2xl border border-line p-4 bg-surface-2/40">
        <Field label={t('Title')} htmlFor="it"><Input id="it" dir="auto" value={f.title ?? ''} onChange={(e) => setF({ ...f, title: e.target.value })} placeholder={t('Degraded performance in Frankfurt')} /></Field>
        <Field label={t('Details')} htmlFor="ic" help={t('Plain text; links are clickable.')}><Textarea id="ic" dir="auto" value={f.content ?? ''} onChange={(e) => setF({ ...f, content: e.target.value })} placeholder={t('We are investigating…')} /></Field>
        <div className="flex flex-wrap items-center gap-4">
          <Segmented size="sm" ariaLabel={t('Style')} value={f.style ?? 'warning'} onChange={(v) => setF({ ...f, style: v })} options={styles.map((s) => ({ value: s, label: styleLabel(s) }))} />
          <Switch checked={!!f.pinned} onChange={(v) => setF({ ...f, pinned: v })} label={t('Show on page')} />
        </div>
        <div className="flex gap-2">
          <Button variant="primary" size="sm" loading={busy} disabled={!f.title} onClick={save} icon={<Megaphone className="size-3.5" />}>{f.id ? t('Update incident') : t('Post incident')}</Button>
          {f.id && <Button size="sm" variant="ghost" onClick={() => setF({ title: '', content: '', style: 'warning', pinned: true })}>{t('Cancel edit')}</Button>}
        </div>
      </div>
      {list === null ? <Skeleton className="h-20" /> : list.length === 0 ? <p className="text-sm text-muted">{t('No incidents posted.')}</p> : (
        <div className="space-y-2">
          {list.map((i) => (
            <div key={i.id} className="rounded-2xl border border-line p-4 flex items-start gap-3">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2"><Badge tone={tone(i.style)}>{styleLabel(i.style)}</Badge>{!i.pinned && <Badge>{t('Hidden')}</Badge>}<span className="font-medium" dir="auto">{i.title}</span></div>
                {i.content && <p className="mt-1.5 text-[13px] text-muted whitespace-pre-line line-clamp-3" dir="auto">{i.content}</p>}
                <p className="mt-1.5 text-[12px] text-subtle">{fmtDateTime(i.updatedAt * 1000)}</p>
              </div>
              <Button size="sm" variant="ghost" onClick={() => setF(i)}>{t('Edit')}</Button>
              <IconButton size="sm" label={t('Delete incident')} onClick={async () => {
                if (await confirm({ title: t('Delete this incident?'), confirmLabel: t('Delete'), danger: true })) {
                  await api.del(`/api/status-pages/${pageId}/incidents/${i.id}`).catch(() => {})
                  load()
                }
              }}><Trash2 className="size-4" /></IconButton>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export default function StatusPageEditor() {
  const t = useT()
  const { id } = useParams()
  const nav = useNavigate()
  const monitors = useApp((s) => s.monitors)
  const role = useApp((s) => s.role)
  const [p, setP] = useState<StatusPage | null>(null)
  const [savedSlug, setSavedSlug] = useState('')
  const [tab, setTab] = useState<Tab>('general')
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [entry, setEntry] = useState('')
  const [https, setHttps] = useState<HttpsStatus | null | undefined>(undefined)
  const frame = useRef<HTMLIFrameElement>(null)
  const loadHttps = useCallback(() => {
    if (role !== 'admin') return setHttps(null)
    api.get<HttpsStatus>('/api/https').then(setHttps).catch(() => setHttps(null))
  }, [role])

  useEffect(() => {
    api.get<StatusPage>(`/api/status-pages/${id}`).then((r) => { setP(r); setSavedSlug(r.slug) }).catch((e) => setErr(e.message))
    if (role === 'admin') api.get<Record<string, string>>('/api/settings').then((s) => setEntry(s.entryPage)).catch(() => {})
    loadHttps()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, role])

  // The preview shows the draft: the page in the frame asks for it once it
  // has loaded, and gets every change after that.
  const latest = useRef<StatusPage | null>(null)
  latest.current = p
  useEffect(() => {
    const send = () => frame.current?.contentWindow?.postMessage({ type: 'neo-preview', page: latest.current }, window.location.origin)
    const onMsg = (e: MessageEvent) => {
      if (e.origin === window.location.origin && e.data?.type === 'neo-preview-ready' && e.source === frame.current?.contentWindow) send()
    }
    window.addEventListener('message', onMsg)
    return () => window.removeEventListener('message', onMsg)
  }, [])
  useEffect(() => {
    if (!p) return
    const h = setTimeout(() => frame.current?.contentWindow?.postMessage({ type: 'neo-preview', page: p }, window.location.origin), 250)
    return () => clearTimeout(h)
  }, [p])


  if (!p) return <div className="p-8">{err ? <Alert tone="danger">{err}</Alert> : <Skeleton className="h-96" />}</div>

  const up = (patch: Partial<StatusPage>) => { setP({ ...p, ...patch }); setDirty(true) }
  const setGroups = (groups: StatusPage['groups']) => up({ groups })

  const save = async () => {
    setBusy(true)
    setErr('')
    try {
      const r = await api.put<StatusPage>(`/api/status-pages/${p.id}`, p)
      setP(r)
      setSavedSlug(r.slug)
      setDirty(false)
      if (r.domain) loadHttps()
      toast.success(t('Status page saved'))
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const del = async () => {
    if (!(await confirm({ title: t('Delete “{name}”?', { name: p.title }), body: t('The public page stops working immediately. Monitors are not affected.'), confirmLabel: t('Delete page'), danger: true }))) return
    await api.del(`/api/status-pages/${p.id}`)
    toast.success(t('Status page deleted'))
    nav('/status-pages')
  }

  const setEntryPage = async (on: boolean) => {
    const v = on ? `status:${savedSlug}` : 'dashboard'
    try {
      await api.put('/api/settings', { entryPage: v })
      setEntry(v)
      toast.success(on ? t('This page now opens at the root address') : t('The root address opens the dashboard again'))
    } catch (e) {
      toast.error(t('Could not change'), (e as Error).message)
    }
  }

  const previewSrc = `/status/${savedSlug}?preview=1`
  const openURL = pageURL({ slug: savedSlug, domain: p.domain }, https)

  return (
    <div className="p-4 sm:p-6 lg:p-8 w-full max-w-[1440px] mx-auto">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <Link to="/status-pages" className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-fg"><ArrowLeft className="size-4 rtl:rotate-180" />{t('Status pages')}</Link>
          <h1 className="mt-2 font-display text-[26px] font-semibold tracking-tight truncate" dir="auto">{p.title}</h1>
        </div>
        <div className="flex items-center gap-2">
          <a href={openURL} target="_blank" rel="noreferrer"><Button icon={<ExternalLink className="size-4" />}>{t('Open')}</Button></a>
          <Button variant="primary" loading={busy} disabled={!dirty} onClick={save} icon={<Save className="size-4" />}>{dirty ? t('Save changes') : t('Saved')}</Button>
        </div>
      </div>
      {err && <Alert tone="danger" className="mt-4">{err}</Alert>}

      <div className="mt-6 grid 2xl:grid-cols-[560px_minmax(0,1fr)] xl:grid-cols-[500px_minmax(0,1fr)] gap-6 items-start">
        <Card className="overflow-hidden">
          <div className="px-4 pt-4"><Segmented ariaLabel={t('Editor section')} value={tab} onChange={setTab} className="w-full [&>button]:flex-1" options={[{ value: 'general', label: t('General') }, { value: 'appearance', label: t('Look') }, { value: 'monitors', label: t('Monitors') }, { value: 'incidents', label: t('Incidents') }]} /></div>
          <div className="p-5 space-y-5">
            {tab === 'general' && (
              <>
                <Field label={t('Title')} htmlFor="tt"><Input id="tt" dir="auto" value={p.title} onChange={(e) => up({ title: e.target.value })} /></Field>
                <Field label={t('Address')} htmlFor="s" help={`${window.location.origin}/status/${p.slug}`}><Input id="s" dir="ltr" className="font-mono" value={p.slug} onChange={(e) => up({ slug: slugify(e.target.value) })} /></Field>
                <Field label={t('Description')} htmlFor="d"><Textarea id="d" dir="auto" value={p.description} onChange={(e) => up({ description: e.target.value })} placeholder={t('Live status of our services.')} /></Field>
                <Field label={t('Page language')} htmlFor="pl" help={t('Auto follows the visitor’s browser.')}>
                  <Select id="pl" value={p.language} onChange={(e) => up({ language: e.target.value as StatusPage['language'] })}>
                    <option value="auto">{t('Auto')}</option>
                    <option value="fa">فارسی</option>
                    <option value="en">English</option>
                  </Select>
                </Field>
                <LogoField value={p.logo} onChange={(v) => up({ logo: v })} />
                <Field label={t('Own domain (optional)')} htmlFor="dom" help={role === 'admin' ? t('Show this page at its own address, such as status.example.com. Visitors there see only this page.') : t('Only an admin can change the domain.')}>
                  <div className="relative">
                    <Globe className="absolute start-3 top-1/2 -translate-y-1/2 size-4 text-subtle pointer-events-none" />
                    <Input id="dom" dir="ltr" className="font-mono ps-9" value={p.domain} disabled={role !== 'admin'} onChange={(e) => up({ domain: e.target.value.trim().toLowerCase() })} placeholder="status.example.com" />
                  </div>
                </Field>
                {!dirty && <DomainState domain={p.domain} https={https} onRefresh={loadHttps} />}
                <Field label={t('Footer text')} htmlFor="f"><Textarea id="f" dir="auto" className="min-h-16" value={p.footer} onChange={(e) => up({ footer: e.target.value })} placeholder={t('Questions? support@example.com')} /></Field>
                <Switch checked={p.published} onChange={(v) => up({ published: v })} label={t('Published')} description={t('Off keeps it visible only to signed-in users.')} />
                {role === 'admin' && (
                  <Switch checked={entry === `status:${savedSlug}`} onChange={setEntryPage} label={t('Open at the root address')} description={t('Visitors to / see this page instead of the sign-in screen.')} />
                )}
                <div className="pt-3 border-t border-line">
                  <Button variant="danger" size="sm" icon={<Trash2 className="size-3.5" />} onClick={del}>{t('Delete status page')}</Button>
                </div>
              </>
            )}

            {tab === 'appearance' && (
              <>
                <Field label={t('Theme')}>
                  <div className="grid grid-cols-2 gap-3" role="radiogroup" aria-label={t('Theme')}>
                    {(Object.keys(themeMeta) as Theme[]).map((th) => (
                      <button key={th} role="radio" aria-checked={p.theme === th} onClick={() => up({ theme: th })} className={clsx('text-start rounded-2xl border-2 overflow-hidden transition-colors', p.theme === th ? 'border-accent' : 'border-line hover:border-line-strong', (th === 'zen' || th === 'pulse') && 'col-span-2')}>
                        <ThemeSwatch theme={th} className={th === 'zen' || th === 'pulse' ? 'h-16' : 'h-20'} />
                        <div className="px-3 py-2"><p className="text-sm font-semibold">{themeMeta[th].name}{th === 'zen' && <span className="ms-2 text-[11px] text-accent-ink">{t('recommended')}</span>}{th === 'pulse' && <span className="ms-2 text-[11px] text-accent-ink">{t('new')}</span>}</p><p className="text-[11.5px] text-muted leading-snug">{t(themeMeta[th].desc)}</p></div>
                      </button>
                    ))}
                  </div>
                </Field>
                <div className="grid grid-cols-2 gap-4">
                  <Field label={t('Layout')}><Segmented ariaLabel={t('Layout')} value={p.layout} onChange={(v) => up({ layout: v })} options={[{ value: 'list', label: t('List') }, { value: 'cards', label: t('Cards') }]} /></Field>
                  <Field label={t('Bars show')}><Segmented ariaLabel={t('Bars show')} value={p.barMode} onChange={(v) => up({ barMode: v })} options={[{ value: 'heartbeat', label: t('Checks') }, { value: 'days', label: t('Days') }]} /></Field>
                </div>
                {p.barMode === 'days' ? (
                  <Field label={t('Days shown')} htmlFor="bd" help={t('One bar per day, coloured by that day’s uptime.')}>
                    <Select id="bd" value={p.barDays || 90} onChange={(e) => up({ barDays: Number(e.target.value) })}>
                      {[7, 14, 30, 60, 90].map((d) => <option key={d} value={d}>{t('{n} days', { n: d })}</option>)}
                    </Select>
                  </Field>
                ) : <p className="-mt-2 text-[12.5px] text-muted">{t('One bar per check, the last 100 — what the dashboard shows.')}</p>}
                <Visibility p={p} up={up} />
                <div className="grid grid-cols-2 gap-4">
                  <Field label={t('Accent colour')} help={t('Empty uses the theme’s.')}>
                    <div className="flex gap-2">
                      <input type="color" aria-label={t('Accent colour')} value={p.accentColor || '#22c55e'} onChange={(e) => up({ accentColor: e.target.value })} className="h-10 w-12 rounded-xl border border-line-strong bg-surface-2 p-1 cursor-pointer" />
                      <Input dir="ltr" value={p.accentColor} onChange={(e) => up({ accentColor: e.target.value })} placeholder={t('theme')} className="font-mono" />
                    </div>
                  </Field>
                  <Field label={t('Refresh every')} htmlFor="ri">
                    <Select id="ri" value={p.refreshInterval} onChange={(e) => up({ refreshInterval: Number(e.target.value) })}>
                      {[30, 60, 120, 300].map((s) => <option key={s} value={s}>{s < 60 ? t('{n} seconds', { n: s }) : t('{n} minutes', { n: s / 60 })}</option>)}
                    </Select>
                  </Field>
                </div>
                <Field label={t('Custom CSS')} htmlFor="css" help={t('Applied last; target .sp and Tailwind classes.')}><Textarea id="css" dir="ltr" className="font-mono text-[12.5px] min-h-28" value={p.customCss} onChange={(e) => up({ customCss: e.target.value })} placeholder={'.sp h1 { letter-spacing: -0.04em; }'} /></Field>
              </>
            )}

            {tab === 'monitors' && <MonitorsTab groups={p.groups} setGroups={setGroups} monitors={monitors} />}

            {tab === 'incidents' && <IncidentsTab pageId={p.id} />}
          </div>
        </Card>

        <div className="xl:sticky xl:top-6 space-y-2">
          <div className="flex items-center justify-between text-[13px] text-muted px-1">
            <span>{t('Preview')}{dirty && <span className="text-pending"> · {t('unsaved — visitors see it after you save')}</span>}</span>
          </div>
          <div className="rounded-[22px] border border-line-strong overflow-hidden bg-surface shadow-[var(--shadow-pop)]">
            <div className="flex items-center gap-1.5 px-4 h-9 border-b border-line bg-surface-2" dir="ltr">
              <span className="size-2.5 rounded-full bg-down/70" /><span className="size-2.5 rounded-full bg-pending/70" /><span className="size-2.5 rounded-full bg-up/70" />
              <span className="ms-3 text-[12px] text-subtle font-mono truncate">{p.domain || `${window.location.host}/status/${savedSlug}`}</span>
            </div>
            <iframe ref={frame} key={previewSrc} title={t('Status page preview')} src={previewSrc} className="w-full h-[78vh] bg-white" />
          </div>
        </div>
      </div>
    </div>
  )
}
