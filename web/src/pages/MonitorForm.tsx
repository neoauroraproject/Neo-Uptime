import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ArrowLeft, ArrowRight, Bell, Check, ChevronDown, CircleCheck, Play, Plus, Save, Search, Server, X } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { clsx, duration, n } from '../lib/format'
import { useT } from '../lib/i18n'
import type { AgentSource, Monitor, MonitorConfig, MonitorTag, MonitorType, PanelSnapshot, Tunnel } from '../lib/types'
import { Alert, Button, Checkbox, Empty, Field, Input, Select, Skeleton, Spinner, Switch, TagChip, Textarea, toast } from '../components/ui'
import { SourceBadge, TypeIcon, typeGroups, typeInfo } from '../components/meta'
import { InstallBox } from './Servers'
import { CountryPicker } from '../components/flag'
import { NotificationDialog } from '../components/NotificationDialog'

type Draft = Omit<Monitor, 'id' | 'createdAt' | 'updatedAt' | 'sortOrder'>

const httpTypes: MonitorType[] = ['http', 'keyword', 'json-query']
const hostTypes: MonitorType[] = ['tcp', 'udp', 'ping', 'dns', 'tls', 'smtp', 'mqtt', 'grpc', 'radius', 'snmp', 'steam', 'kafka']
const defaultPorts: Partial<Record<MonitorType, number>> = { tls: 443, smtp: 587, mqtt: 1883, grpc: 443, radius: 1812, snmp: 161, steam: 27015, kafka: 9092 }
const codePresets = ['200-299', '300-399', '400-499', '500-599']

// The few types most people need, shown first as big cards.
const popular: { type: MonitorType; label: string; help: string }[] = [
  { type: 'agent-tunnel', label: 'Tunnel', help: 'A BackPack or Kariz tunnel on one of your servers' },
  { type: 'agent', label: 'Server', help: 'CPU, memory and tunnels of a server running the agent' },
  { type: 'http', label: 'Website', help: 'Is a website or API answering?' },
  { type: 'ping', label: 'Ping', help: 'Does a host answer, and how fast?' },
  { type: 'tcp', label: 'Port', help: 'Is a TCP port open?' },
  { type: 'push', label: 'Cron job or script', help: 'Your script calls Neo Uptime; silence means down' },
]

const steps = ['Type', 'Details', 'Alerts', 'Finish'] as const

type Found = { backpack?: AgentSource; kariz?: AgentSource }
const tunnelKeys = (f: Found) => [
  ...(f.backpack?.tunnels ?? []).map((tn) => `backpack/${tn.name}`),
  ...(f.kariz?.tunnels ?? []).map((tn) => `kariz/${tn.name}`),
]

function blank(defaults: number[]): Draft {
  return {
    name: '', type: 'http', parentId: null, active: true, interval: 60, retryInterval: 60, maxRetries: 1, timeout: 0,
    resendInterval: 0, upsideDown: false, description: '', tags: [], notificationIds: defaults, country: '',
    config: { url: 'https://', method: 'GET', acceptedStatusCodes: ['200-299'], maxRedirects: 10, bodyEncoding: 'json', dnsResolver: '1.1.1.1', dnsRecordType: 'A', port: 0, packetCount: 3, jsonOperator: '==' },
  }
}

function NumberInput({ value, onChange, min, max, suffix, id }: { value: number; onChange: (v: number) => void; min?: number; max?: number; suffix?: string; id?: string }) {
  return (
    <div className="relative">
      <Input id={id} dir="ltr" type="number" inputMode="numeric" min={min} max={max} value={Number.isFinite(value) ? value : ''} onChange={(e) => onChange(e.target.value === '' ? 0 : Number(e.target.value))} className={suffix ? 'pe-16' : ''} />
      {suffix && <span className="pointer-events-none absolute end-3 top-1/2 -translate-y-1/2 text-[13px] text-subtle">{suffix}</span>}
    </div>
  )
}

/** A fold-out block for the options most people never need. */
function More({ title, children, open: initial = false }: { title: string; children: ReactNode; open?: boolean }) {
  const [open, setOpen] = useState(initial)
  return (
    <div className="rounded-2xl border border-line">
      <button type="button" onClick={() => setOpen(!open)} aria-expanded={open} className="w-full flex items-center justify-between gap-3 px-4 h-12 text-sm font-medium text-start hover:bg-surface-2/60 rounded-2xl">
        {title}<ChevronDown className={clsx('size-4 text-muted transition-transform', open && 'rotate-180')} />
      </button>
      {open && <div className="px-4 pb-4 pt-1 space-y-4">{children}</div>}
    </div>
  )
}

function Choice({ on, onClick, children, className }: { on: boolean; onClick: () => void; children: ReactNode; className?: string }) {
  return (
    <button type="button" aria-pressed={on} onClick={onClick} className={clsx('h-9 px-3.5 rounded-xl border text-[13px] font-medium transition-colors', on ? 'border-accent bg-accent-soft text-fg' : 'border-line bg-surface-2 text-muted hover:text-fg', className)}>
      {children}
    </button>
  )
}

export default function MonitorForm({ mode }: { mode: 'add' | 'edit' | 'clone' }) {
  const t = useT()
  const { id } = useParams()
  const nav = useNavigate()
  const monitors = useApp((s) => s.monitors)
  const tags = useApp((s) => s.tags)
  const notifications = useApp((s) => s.notifications)
  const panels = useApp((s) => s.panels)
  const agents = useApp((s) => s.agents)
  const role = useApp((s) => s.role)
  const [d, setD] = useState<Draft | null>(mode === 'add' ? blank(notifications.filter((x) => x.isDefault).map((x) => x.id)) : null)
  const [params] = useSearchParams()
  // ?step=alerts opens an existing monitor straight at its alert settings.
  const [step, setStep] = useState(mode === 'add' ? 0 : params.get('step') === 'alerts' ? 2 : 1)
  const [nameTouched, setNameTouched] = useState(mode !== 'add')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [snap, setSnap] = useState<PanelSnapshot | null>(null)
  const [snapLoading, setSnapLoading] = useState(false)
  const [agentData, setAgentData] = useState<{ backpack?: AgentSource; kariz?: AgentSource } | null>(null)
  const [newTag, setNewTag] = useState({ tagId: 0, name: '', color: '#a3e635', value: '' })
  const [customCode, setCustomCode] = useState('')
  const [notifOpen, setNotifOpen] = useState(false)
  const [allTypes, setAllTypes] = useState(false)
  const [typeQuery, setTypeQuery] = useState('')
  const body = useRef<HTMLDivElement>(null)
  // A new server set up from the wizard: created on the last step, then
  // monitored as soon as its agent reports.
  const [newServer, setNewServer] = useState(false)
  const [srv, setSrv] = useState({ name: '', country: '' })
  const [created, setCreated] = useState<{ id: number; install: string } | null>(null)
  const [found, setFound] = useState<Found | null>(null)
  const [pick, setPick] = useState<Set<string>>(new Set())
  const [laterToo, setLaterToo] = useState(true)
  const createdId = created?.id
  useEffect(() => {
    if (!createdId || found) return
    const check = () => api.get<{ agent: { lastSeen: number }; backpack?: AgentSource | null; kariz?: AgentSource | null }>(`/api/agents/${createdId}`)
      .then((r) => {
        if (!r.agent.lastSeen) return
        const f = { backpack: r.backpack ?? undefined, kariz: r.kariz ?? undefined }
        setFound(f)
        setPick(new Set(tunnelKeys(f)))
      })
      .catch(() => {})
    check()
    const h = setInterval(check, 3000)
    return () => clearInterval(h)
  }, [createdId, found])

  useEffect(() => {
    if (mode === 'add' || !id) return
    api.get<Monitor>(`/api/monitors/${id}`).then((m) => {
      const { id: _i, createdAt: _c, updatedAt: _u, sortOrder: _s, ...rest } = m
      void _i; void _c; void _u; void _s
      setD(mode === 'clone' ? { ...rest, name: `${m.name} (${t('copy')})`, pushToken: '' } : rest)
    }).catch((e) => setError(e.message))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, mode])

  useEffect(() => {
    if (mode !== 'add' || !d || d.notificationIds.length) return
    const defs = notifications.filter((x) => x.isDefault).map((x) => x.id)
    if (defs.length) setD({ ...d, notificationIds: defs })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [notifications])

  const panelId = d?.config.panelId
  useEffect(() => {
    if (!panelId || d?.type !== 'backpack-tunnel') return
    setSnapLoading(true)
    api.get<{ snapshot: PanelSnapshot }>(`/api/backpack/panels/${panelId}/snapshot`)
      .then((r) => setSnap(r.snapshot))
      .catch(() => setSnap(null))
      .finally(() => setSnapLoading(false))
  }, [panelId, d?.type])

  const agentId = d?.config.agentId
  useEffect(() => {
    if (!agentId || d?.type !== 'agent-tunnel') return
    api.get<{ backpack?: AgentSource; kariz?: AgentSource }>(`/api/agents/${agentId}`).then(setAgentData).catch(() => setAgentData(null))
  }, [agentId, d?.type])

  useEffect(() => {
    body.current?.scrollTo({ top: 0 })
    // Put the cursor in the first field of the step.
    const first = body.current?.querySelector<HTMLElement>('input:not([type=checkbox]):not([type=color]), select, textarea')
    if (step > 0 && first && !first.hasAttribute('disabled')) first.focus({ preventScroll: true })
  }, [step])

  const groups = useMemo(() => Object.values(monitors).filter((m) => m.type === 'group' && m.id !== Number(id)), [monitors, id])

  if (!canEdit(role)) return <Empty title={t('Read-only account')} body={t('Ask an admin for editor access to add or change monitors.')} />
  if (!d) return <div className="p-8 space-y-4 max-w-4xl">{error ? <Alert tone="danger">{error}</Alert> : <><Skeleton className="h-10 w-64" /><Skeleton className="h-96" /></>}</div>

  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setD({ ...d, [k]: v })
  const cfg = <K extends keyof MonitorConfig>(k: K, v: MonitorConfig[K]) => setD({ ...d, config: { ...d.config, [k]: v } })
  const c = d.config
  const isHttp = httpTypes.includes(d.type)
  const autoTimeout = Math.min(48, Math.max(5, Math.round(d.interval * 0.8)))
  const agentTunnels: Tunnel[] = (c.source === 'kariz' ? agentData?.kariz?.tunnels : agentData?.backpack?.tunnels) ?? []
  const last = steps.length - 1
  const hasLatency = d.type !== 'group' && d.type !== 'push'
  const serverFlow = mode === 'add' && (d.type === 'agent' || d.type === 'agent-tunnel') && (newServer || agents.length === 0)

  const chooseType = (type: MonitorType) => {
    const next: MonitorConfig = { ...c }
    if (type.startsWith('backpack') && !c.panelId) next.panelId = panels[0]?.id
    if (type.startsWith('agent') && !c.agentId) next.agentId = agents[0]?.id
    if (type === 'agent-tunnel' && !c.source) next.source = 'backpack'
    if (defaultPorts[type] && !c.port) next.port = defaultPorts[type]
    if (type === 'websocket' && !(c.url ?? '').startsWith('ws')) next.url = 'wss://'
    if (isHttp && !httpTypes.includes(type) && type !== 'websocket') next.port = defaultPorts[type] ?? 0
    // Servers report every 20 s; checking them more slowly loses detail.
    const interval = mode === 'add' && type.startsWith('agent') && d.interval === 60 ? 30 : d.interval
    setD({ ...d, type, config: next, interval, retryInterval: d.retryInterval === d.interval ? interval : d.retryInterval })
  }
  const pickAndGo = (type: MonitorType) => {
    chooseType(type)
    setStep(1)
  }

  const onUrl = (url: string) => {
    const next = { ...d, config: { ...c, url } }
    if (!nameTouched) {
      try {
        next.name = new URL(url.replace(/^ws/, 'http')).hostname
      } catch { /* not a URL yet */ }
    }
    setD(next)
  }
  const onHost = (hostname: string) => setD({ ...d, config: { ...c, hostname }, name: nameTouched ? d.name : hostname })

  const addTag = async () => {
    let tagId = newTag.tagId
    let name = '', color = ''
    if (tagId === -1) {
      if (!newTag.name.trim()) return
      try {
        const tg = await api.post<{ id: number; name: string; color: string }>('/api/tags', { name: newTag.name.trim(), color: newTag.color })
        await useApp.getState().loadTags()
        tagId = tg.id; name = tg.name; color = tg.color
      } catch (e) {
        toast.error(t('Could not create tag'), (e as Error).message)
        return
      }
    } else {
      const tg = tags.find((x) => x.id === tagId)
      if (!tg) return
      name = tg.name; color = tg.color
    }
    const tag: MonitorTag = { tagId, name, color, value: newTag.value.trim() }
    if (!d.tags.some((x) => x.tagId === tag.tagId && x.value === tag.value)) set('tags', [...d.tags, tag])
    setNewTag({ tagId: 0, name: '', color: '#a3e635', value: '' })
  }

  // The step's own fields must be valid before moving on.
  const stepValid = () => {
    const fields = body.current?.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>('input, select, textarea') ?? []
    for (const f of fields) {
      if (!f.checkValidity()) {
        f.reportValidity()
        return false
      }
    }
    return true
  }

  const save = async () => {
    setError('')
    setBusy(true)
    const payload = { ...d, timeout: d.timeout || 0 }
    try {
      const saved = mode === 'edit'
        ? await api.put<Monitor>(`/api/monitors/${id}`, payload)
        : await api.post<Monitor>('/api/monitors', payload)
      toast.success(mode === 'edit' ? t('Monitor saved') : t('Monitor created'), mode === 'edit' ? undefined : t('The first check is running now.'))
      await useApp.getState().loadDashboard()
      nav(`/dashboard/${saved.id}`)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const createServer = async () => {
    setError('')
    setBusy(true)
    try {
      const r = await api.post<{ agent: { id: number }; install: string }>('/api/agents', { name: srv.name.trim(), country: srv.country, interval: 20, autoImport: false })
      setCreated({ id: r.agent.id, install: r.install })
      useApp.getState().loadAgents().catch(() => {})
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const startMonitoring = async () => {
    if (!created) return
    setError('')
    setBusy(true)
    try {
      const r = await api.post<{ imported: number; monitorIds: number[]; groupId?: number; serverMonitorId?: number }>(`/api/agents/${created.id}/import`, {
        keys: [...pick], skip: tunnelKeys(found ?? {}).filter((k) => !pick.has(k)), notificationIds: d.notificationIds, interval: d.interval,
        latencyAlertMs: c.latencyAlertMs ?? 0, latencyAlertFor: c.latencyAlertFor ?? 0,
      })
      if (laterToo) await api.put(`/api/agents/${created.id}`, { name: srv.name.trim(), country: srv.country, interval: 20, autoImport: true })
      await Promise.all([useApp.getState().loadDashboard(), useApp.getState().loadAgents()])
      toast.success(t('Monitoring started'), t('{name} and {n} tunnels are being watched.', { name: srv.name.trim(), n: r.imported }))
      const tunnelIds = r.monitorIds.filter((x) => x !== r.serverMonitorId)
      const target = d.type === 'agent' ? r.serverMonitorId : tunnelIds.length === 1 ? tunnelIds[0] : r.groupId ?? r.serverMonitorId
      nav(target ? `/dashboard/${target}` : '/servers')
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!stepValid()) return
    if (step < last && mode === 'add') setStep(step + 1)
    else if (serverFlow) {
      if (!created) createServer()
      else if (found) startMonitoring()
    } else save()
  }
  const goTo = (i: number) => {
    if (i === step) return
    if (i > step && !stepValid()) return
    if (mode === 'add' && i > step + 1) return
    setStep(i)
  }

  const title = mode === 'edit' ? t('Edit {name}', { name: d.name || t('monitor') }) : mode === 'clone' ? t('Clone monitor') : t('Add monitor')
  const hostPlaceholder = d.type === 'dns' ? 'example.com' : d.type === 'kafka' ? 'broker1:9092,broker2:9092' : '203.0.113.10'
  const sec = (opts: string[]) => (
    <Select id="sec" value={c.security ?? opts[0]} onChange={(e) => cfg('security', e.target.value)}>
      {opts.map((o) => <option key={o} value={o}>{o === 'none' ? t('None') : o.toUpperCase()}</option>)}
    </Select>
  )
  const target = (() => {
    if (isHttp || d.type === 'websocket') return c.url ?? ''
    if (d.type.endsWith('tunnel')) return c.tunnelName ?? ''
    if (d.type === 'agent') return agents.find((a) => a.id === c.agentId)?.name ?? ''
    if (hostTypes.includes(d.type)) return `${c.hostname ?? ''}${c.port ? `:${c.port}` : ''}`
    return ''
  })()

  /* ---------- step 1: type ---------- */
  const q = typeQuery.trim().toLowerCase()
  const typeStep = (
    <div className="space-y-5">
      <div>
        <h2 className="font-display text-xl font-semibold">{t('What do you want to watch?')}</h2>
        <p className="text-sm text-muted mt-1">{t('Pick one — you can fine-tune everything later.')}</p>
      </div>
      {!allTypes ? (
        <>
          <div className="grid sm:grid-cols-2 xl:grid-cols-3 gap-3" role="radiogroup" aria-label={t('Monitor type')}>
            {popular.map((p) => (
              <button key={p.type} type="button" role="radio" aria-checked={d.type === p.type} onClick={() => pickAndGo(p.type)}
                className={clsx('group flex items-start gap-3 rounded-2xl border p-4 text-start transition-colors', d.type === p.type ? 'border-accent bg-accent-soft' : 'border-line bg-surface-2/50 hover:border-line-strong hover:bg-surface-2')}>
                <span className={clsx('grid place-items-center size-10 shrink-0 rounded-xl', d.type === p.type ? 'bg-accent text-on-accent' : 'bg-surface border border-line text-muted group-hover:text-fg')}>
                  <TypeIcon type={p.type} className="size-5" />
                </span>
                <span className="min-w-0">
                  <span className="block font-semibold">{t(p.label)}</span>
                  <span className="block text-[13px] text-muted leading-snug mt-0.5">{t(p.help)}</span>
                </span>
              </button>
            ))}
          </div>
          <button type="button" className="text-sm text-accent-ink hover:underline" onClick={() => setAllTypes(true)}>{t('All monitor types ({n})', { n: Object.keys(typeInfo).length })}</button>
        </>
      ) : (
        <>
          <div className="flex items-center gap-3">
            <div className="relative flex-1">
              <Search className="absolute start-3 top-1/2 -translate-y-1/2 size-4 text-subtle pointer-events-none" />
              <Input autoFocus value={typeQuery} onChange={(e) => setTypeQuery(e.target.value)} placeholder={t('Search types')} className="ps-9" aria-label={t('Search types')} />
            </div>
            <Button type="button" variant="ghost" size="sm" onClick={() => { setAllTypes(false); setTypeQuery('') }}>{t('Back')}</Button>
          </div>
          <div role="radiogroup" aria-label={t('Monitor type')} className="space-y-3">
            {typeGroups.map((g) => {
              const list = (Object.keys(typeInfo) as MonitorType[]).filter((tp) => typeInfo[tp].group === g && (!q || t(typeInfo[tp].label).toLowerCase().includes(q) || tp.includes(q)))
              if (!list.length) return null
              return (
                <div key={g}>
                  <p className="text-[12px] uppercase tracking-wider text-subtle font-semibold mb-1.5">{t(g)}</p>
                  <div className="grid grid-cols-2 sm:grid-cols-3 2xl:grid-cols-4 gap-2">
                    {list.map((tp) => (
                      <button key={tp} type="button" role="radio" aria-checked={d.type === tp} title={t(typeInfo[tp].help)}
                        disabled={mode === 'edit' && d.type !== tp && (d.type === 'push' || tp === 'push')}
                        onClick={() => (mode === 'add' ? pickAndGo(tp) : chooseType(tp))}
                        className={clsx('flex items-center gap-2.5 rounded-xl border px-3 h-11 text-start text-[13.5px] font-medium transition-colors disabled:opacity-40',
                          d.type === tp ? 'border-accent bg-accent-soft text-fg' : 'border-line bg-surface-2 text-muted hover:text-fg hover:border-line-strong')}>
                        <TypeIcon type={tp} className={clsx('size-4 shrink-0', d.type === tp && 'text-accent-ink')} />
                        <span className="truncate">{t(typeInfo[tp].label)}</span>
                      </button>
                    ))}
                  </div>
                </div>
              )
            })}
          </div>
        </>
      )}
    </div>
  )

  /* ---------- step 2: details ---------- */
  const detailsStep = (
    <div className="space-y-5">
      <div className="flex items-center gap-3">
        <span className="grid place-items-center size-10 rounded-xl bg-accent-soft text-accent-ink"><TypeIcon type={d.type} className="size-5" /></span>
        <div className="min-w-0">
          <h2 className="font-display text-xl font-semibold">{t(typeInfo[d.type].label)}</h2>
          <p className="text-[13px] text-muted">{t(typeInfo[d.type].help)}</p>
        </div>
      </div>

      {(isHttp || d.type === 'websocket') && (
        <Field label={t('URL')} required htmlFor="url">
          <Input id="url" dir="ltr" value={c.url ?? ''} onChange={(e) => onUrl(e.target.value)} placeholder={d.type === 'websocket' ? 'wss://example.com/socket' : 'https://example.com/health'} required className="font-mono" />
        </Field>
      )}
      {d.type === 'keyword' && (
        <div className="grid sm:grid-cols-[1fr_auto] gap-4 items-end">
          <Field label={t('Keyword')} required help={t('Case-sensitive, searched in the response body.')} htmlFor="kw">
            <Input id="kw" dir="auto" value={c.keyword ?? ''} onChange={(e) => cfg('keyword', e.target.value)} required />
          </Field>
          <div className="pb-2"><Switch checked={!!c.invertKeyword} onChange={(v) => cfg('invertKeyword', v)} label={t('Invert')} description={t('Up when NOT found')} /></div>
        </div>
      )}
      {d.type === 'json-query' && (
        <div className="grid sm:grid-cols-[1.4fr_0.8fr_1fr] gap-4">
          <Field label={t('JSON path')} required help={t('e.g. status or data.items[0].ok')} htmlFor="jp">
            <Input id="jp" dir="ltr" className="font-mono" value={c.jsonPath ?? ''} onChange={(e) => cfg('jsonPath', e.target.value)} required />
          </Field>
          <Field label={t('Condition')} htmlFor="op">
            <Select id="op" value={c.jsonOperator ?? '=='} onChange={(e) => cfg('jsonOperator', e.target.value)}>
              {['==', '!=', '<', '<=', '>', '>=', 'contains', 'notcontains', 'exists'].map((o) => <option key={o} value={o}>{o}</option>)}
            </Select>
          </Field>
          {c.jsonOperator !== 'exists' && (
            <Field label={t('Expected value')} htmlFor="ev">
              <Input id="ev" dir="ltr" className="font-mono" value={c.expectedValue ?? ''} onChange={(e) => cfg('expectedValue', e.target.value)} placeholder="ok" />
            </Field>
          )}
        </div>
      )}

      {hostTypes.includes(d.type) && (
        <div className={clsx('grid gap-4', !['ping', 'dns'].includes(d.type) ? 'sm:grid-cols-[1fr_140px]' : '')}>
          <Field label={d.type === 'kafka' ? t('Brokers') : t('Hostname or IP')} required htmlFor="host">
            <Input id="host" dir="ltr" className="font-mono" value={c.hostname ?? ''} onChange={(e) => onHost(e.target.value)} placeholder={hostPlaceholder} required />
          </Field>
          {!['ping', 'dns'].includes(d.type) && (
            <Field label={t('Port')} required={d.type === 'tcp' || d.type === 'udp'} htmlFor="port">
              <NumberInput id="port" value={c.port ?? 0} min={1} max={65535} onChange={(v) => cfg('port', v)} />
            </Field>
          )}
        </div>
      )}
      {d.type === 'dns' && (
        <div className="grid sm:grid-cols-3 gap-4">
          <Field label={t('Resolver')} htmlFor="rs"><Input id="rs" dir="ltr" className="font-mono" value={c.dnsResolver ?? ''} onChange={(e) => cfg('dnsResolver', e.target.value)} placeholder="1.1.1.1" /></Field>
          <Field label={t('Record type')} htmlFor="rt">
            <Select id="rt" value={c.dnsRecordType ?? 'A'} onChange={(e) => cfg('dnsRecordType', e.target.value)}>
              {['A', 'AAAA', 'CNAME', 'MX', 'NS', 'TXT', 'SRV'].map((r) => <option key={r}>{r}</option>)}
            </Select>
          </Field>
          <Field label={t('Expect (optional)')} help={t('Down unless a record contains this.')} htmlFor="ex"><Input id="ex" dir="ltr" className="font-mono" value={c.expectedValue ?? ''} onChange={(e) => cfg('expectedValue', e.target.value)} /></Field>
        </div>
      )}
      {d.type === 'udp' && (
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label={t('Payload (optional)')} htmlFor="pl" help={t('Text to send; empty sends one zero byte.')}><Input id="pl" dir="ltr" className="font-mono" value={c.payload ?? ''} onChange={(e) => cfg('payload', e.target.value)} /></Field>
          <Field label={t('Expect in reply (optional)')} htmlFor="ex" help={t('Set it to require an answer.')}><Input id="ex" dir="ltr" className="font-mono" value={c.expectedValue ?? ''} onChange={(e) => cfg('expectedValue', e.target.value)} /></Field>
        </div>
      )}
      {d.type === 'tls' && (
        <Field label={t('Go down when fewer days remain than')} htmlFor="wd">
          <NumberInput id="wd" value={c.warnDays ?? 14} min={1} max={365} suffix={t('days')} onChange={(v) => cfg('warnDays', v)} />
        </Field>
      )}
      {d.type === 'smtp' && (
        <div className="grid sm:grid-cols-2 gap-4 items-end">
          <Field label={t('Security')} htmlFor="sec">{sec(['starttls', 'tls', 'none'])}</Field>
          <div className="pb-2"><Switch checked={!!c.ignoreTls} onChange={(v) => cfg('ignoreTls', v)} label={t('Ignore TLS errors')} /></div>
        </div>
      )}
      {d.type === 'mqtt' && (
        <>
          <div className="grid sm:grid-cols-3 gap-4">
            <Field label={t('Security')} htmlFor="sec">{sec(['none', 'tls'])}</Field>
            <Field label={t('Username')} htmlFor="mu"><Input id="mu" dir="ltr" autoComplete="off" value={c.username ?? ''} onChange={(e) => cfg('username', e.target.value)} /></Field>
            <Field label={t('Password')} htmlFor="mp"><Input id="mp" dir="ltr" type="password" autoComplete="new-password" value={c.password ?? ''} onChange={(e) => cfg('password', e.target.value)} /></Field>
          </div>
          <div className="grid sm:grid-cols-2 gap-4">
            <Field label={t('Topic (optional)')} htmlFor="tp" help={t('Waits for a message on this topic.')}><Input id="tp" dir="ltr" className="font-mono" value={c.topic ?? ''} onChange={(e) => cfg('topic', e.target.value)} placeholder="sensors/health" /></Field>
            <Field label={t('Keyword (optional)')} htmlFor="kw2"><Input id="kw2" dir="auto" value={c.keyword ?? ''} onChange={(e) => cfg('keyword', e.target.value)} /></Field>
          </div>
        </>
      )}
      {d.type === 'grpc' && (
        <div className="grid sm:grid-cols-[1fr_auto] gap-4 items-end">
          <Field label={t('Service name (optional)')} htmlFor="gs" help={t('Empty checks the whole server.')}><Input id="gs" dir="ltr" className="font-mono" value={c.service ?? ''} onChange={(e) => cfg('service', e.target.value)} placeholder="my.package.Service" /></Field>
          <div className="pb-2"><Switch checked={c.security === 'tls'} onChange={(v) => cfg('security', v ? 'tls' : '')} label={t('Use TLS')} /></div>
        </div>
      )}
      {d.type === 'radius' && (
        <div className="grid sm:grid-cols-3 gap-4">
          <Field label={t('Shared secret')} required htmlFor="rsct"><Input id="rsct" dir="ltr" type="password" autoComplete="off" value={c.secret ?? ''} onChange={(e) => cfg('secret', e.target.value)} required /></Field>
          <Field label={t('Username')} htmlFor="ru"><Input id="ru" dir="ltr" autoComplete="off" value={c.username ?? ''} onChange={(e) => cfg('username', e.target.value)} /></Field>
          <Field label={t('Password')} htmlFor="rp"><Input id="rp" dir="ltr" type="password" autoComplete="new-password" value={c.password ?? ''} onChange={(e) => cfg('password', e.target.value)} /></Field>
        </div>
      )}
      {d.type === 'snmp' && (
        <>
          <div className="grid sm:grid-cols-3 gap-4">
            <Field label={t('Community')} htmlFor="sc"><Input id="sc" dir="ltr" value={c.community ?? ''} onChange={(e) => cfg('community', e.target.value)} placeholder="public" /></Field>
            <Field label="OID" required htmlFor="oid"><Input id="oid" dir="ltr" className="font-mono" value={c.oid ?? ''} onChange={(e) => cfg('oid', e.target.value)} placeholder="1.3.6.1.2.1.1.3.0" required /></Field>
            <Field label={t('Version')} htmlFor="sv"><Select id="sv" value={c.snmpVersion ?? '2c'} onChange={(e) => cfg('snmpVersion', e.target.value)}><option value="2c">v2c</option><option value="1">v1</option></Select></Field>
          </div>
          <div className="grid sm:grid-cols-2 gap-4">
            <Field label={t('Condition')} htmlFor="sop"><Select id="sop" value={c.jsonOperator ?? '=='} onChange={(e) => cfg('jsonOperator', e.target.value)}>{['==', '!=', '<', '<=', '>', '>=', 'contains'].map((o) => <option key={o}>{o}</option>)}</Select></Field>
            <Field label={t('Expected value (optional)')} htmlFor="sev"><Input id="sev" dir="ltr" className="font-mono" value={c.expectedValue ?? ''} onChange={(e) => cfg('expectedValue', e.target.value)} /></Field>
          </div>
        </>
      )}
      {d.type === 'kafka' && (
        <div className="grid sm:grid-cols-2 gap-4"><Field label={t('Security')} htmlFor="sec">{sec(['none', 'tls'])}</Field></div>
      )}

      {d.type === 'docker' && (
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label={t('Container name or ID')} required htmlFor="cn"><Input id="cn" dir="ltr" className="font-mono" value={c.containerName ?? ''} onChange={(e) => cfg('containerName', e.target.value)} required /></Field>
          <Field label={t('Docker host')} help={t('Socket path or tcp://host:2375')} htmlFor="dh"><Input id="dh" dir="ltr" className="font-mono" value={c.dockerHost ?? ''} onChange={(e) => cfg('dockerHost', e.target.value)} placeholder="/var/run/docker.sock" /></Field>
        </div>
      )}
      {['postgres', 'mysql', 'mssql', 'mongodb', 'redis'].includes(d.type) && (
        <>
          <Field label={t('Connection string')} required htmlFor="cs" help={{
            postgres: 'postgres://user:pass@host:5432/db?sslmode=disable', mysql: 'mysql://user:pass@host:3306/db',
            mssql: 'sqlserver://user:pass@host:1433?database=db', mongodb: 'mongodb://user:pass@host:27017/?authSource=admin',
            redis: 'redis://:password@host:6379/0 (rediss:// for TLS)',
          }[d.type as 'postgres']}>
            <Input id="cs" dir="ltr" type="password" autoComplete="off" className="font-mono" value={c.connectionString ?? ''} onChange={(e) => cfg('connectionString', e.target.value)} required />
          </Field>
          {['postgres', 'mysql', 'mssql'].includes(d.type) && (
            <Field label={t('Query (optional)')} htmlFor="q" help={t('Run after connecting; leave empty for a plain connection check.')}>
              <Input id="q" dir="ltr" className="font-mono" value={c.query ?? ''} onChange={(e) => cfg('query', e.target.value)} placeholder="SELECT 1" />
            </Field>
          )}
        </>
      )}

      {d.type.startsWith('backpack') && (
        panels.length === 0 ? (
          <Alert tone="info" title={t('No BackPack panel connected yet')}>
            {t('Connect your panel first under Tunnels → Panels, or install the agent on that server under Servers.')} <Link className="text-accent-ink underline" to="/tunnels/panels">{t('Connect panel')}</Link>
          </Alert>
        ) : (
          <div className="grid sm:grid-cols-2 gap-4">
            <Field label={t('BackPack panel')} required htmlFor="pn">
              <Select id="pn" required value={c.panelId ?? ''} onChange={(e) => setD({ ...d, config: { ...c, panelId: Number(e.target.value), tunnelName: '' } })}>
                <option value="" disabled>{t('Choose…')}</option>
                {panels.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </Select>
            </Field>
            {d.type === 'backpack-tunnel' && (
              <Field label={t('Tunnel')} required htmlFor="tn" help={snap?.error ? t('Panel answered: {e}', { e: snap.error }) : snapLoading ? t('Reading tunnels from the panel…') : undefined}>
                {snap?.tunnels?.length ? (
                  <Select id="tn" required value={c.tunnelName ?? ''} onChange={(e) => setD({ ...d, config: { ...c, tunnelName: e.target.value }, name: nameTouched ? d.name : e.target.value })}>
                    <option value="" disabled>{t('Choose…')}</option>
                    {snap.tunnels.map((tn) => <option key={tn.name} value={tn.name}>{tn.name} — {(tn.carrier || tn.transport).toUpperCase()} · {tn.state}</option>)}
                  </Select>
                ) : (
                  <Input id="tn" dir="ltr" required value={c.tunnelName ?? ''} onChange={(e) => cfg('tunnelName', e.target.value)} placeholder={t('Tunnel name as shown in BackPack')} />
                )}
              </Field>
            )}
          </div>
        )
      )}

      {d.type.startsWith('agent') && (
        serverFlow ? (
          <div className="rounded-2xl border border-line bg-surface-2/40 p-4 space-y-4">
            <div className="flex items-start justify-between gap-3">
              <div className="flex items-start gap-3">
                <span className="grid place-items-center size-9 shrink-0 rounded-xl bg-accent-soft text-accent-ink"><Server className="size-4" /></span>
                <div>
                  <p className="font-semibold">{t('New server')}</p>
                  <p className="text-[13px] text-muted">{t('Give it a name here. On the last step you run one command on it, and monitoring starts as soon as it reports.')}</p>
                </div>
              </div>
              {agents.length > 0 && <Button type="button" size="sm" variant="ghost" onClick={() => setNewServer(false)}>{t('Pick an existing server')}</Button>}
            </div>
            <div className="grid sm:grid-cols-[1fr_220px] gap-4">
              <Field label={t('Server name')} required htmlFor="srvn">
                <Input id="srvn" dir="auto" required maxLength={80} value={srv.name} onChange={(e) => setSrv({ ...srv, name: e.target.value })} placeholder={t('e.g. Frankfurt edge')} />
              </Field>
              <Field label={t('Flag')} htmlFor="srvf"><CountryPicker id="srvf" value={srv.country} onChange={(v) => setSrv({ ...srv, country: v })} /></Field>
            </div>
            {d.type === 'agent-tunnel' && <p className="text-[12.5px] text-muted">{t('You pick its tunnels once the agent has reported them.')}</p>}
          </div>
        ) : (
          <div className={clsx('grid gap-4', d.type === 'agent-tunnel' ? 'sm:grid-cols-3' : 'sm:grid-cols-2')}>
            <Field label={t('Server')} required htmlFor="ag">
              <Select id="ag" required value={c.agentId ?? ''} onChange={(e) => {
                if (e.target.value === 'new') return setNewServer(true)
                const a = agents.find((x) => x.id === Number(e.target.value))
                setD({ ...d, config: { ...c, agentId: Number(e.target.value), tunnelName: '' }, name: nameTouched || d.type !== 'agent' ? d.name : a?.name ?? d.name, country: d.country || a?.country || '' })
              }}>
                <option value="" disabled>{t('Choose…')}</option>
                {agents.map((a) => <option key={a.id} value={a.id}>{a.name}{a.online ? '' : ` (${t('offline')})`}</option>)}
                {mode === 'add' && <option value="new">{t('+ New server…')}</option>}
              </Select>
            </Field>
            {d.type === 'agent-tunnel' && (
              <>
                <Field label={t('Program')} htmlFor="src">
                  <Select id="src" value={c.source ?? 'backpack'} onChange={(e) => setD({ ...d, config: { ...c, source: e.target.value as 'backpack' | 'kariz', tunnelName: '' } })}>
                    <option value="backpack">BackPack</option>
                    <option value="kariz">Kariz</option>
                  </Select>
                </Field>
                <Field label={t('Tunnel')} required htmlFor="atn">
                  {agentTunnels.length ? (
                    <Select id="atn" required value={c.tunnelName ?? ''} onChange={(e) => setD({ ...d, config: { ...c, tunnelName: e.target.value }, name: nameTouched ? d.name : e.target.value })}>
                      <option value="" disabled>{t('Choose…')}</option>
                      {agentTunnels.map((tn) => <option key={tn.name} value={tn.name}>{tn.name} — {(tn.carrier || tn.transport || '').toUpperCase()} · {tn.state}</option>)}
                    </Select>
                  ) : (
                    <Input id="atn" dir="ltr" required value={c.tunnelName ?? ''} onChange={(e) => cfg('tunnelName', e.target.value)} placeholder={t('Tunnel name')} />
                  )}
                </Field>
              </>
            )}
          </div>
        )
      )}

      {d.type === 'push' && (
        <Alert tone="info" title={t('Push URL')}>
          {d.pushToken ? <>{t('Your script should call')} <code className="font-mono text-fg break-all" dir="ltr">/api/push/{d.pushToken}?status=up&amp;msg=OK&amp;ping=</code></> : t('A private URL is generated when you save. Your script calls it; if it stays silent longer than the interval, the monitor goes down.')}
        </Alert>
      )}

      {!serverFlow && <div className="grid sm:grid-cols-[1fr_220px] gap-4">
        <Field label={t('Friendly name')} required htmlFor="name">
          <Input id="name" dir="auto" value={d.name} onChange={(e) => { setNameTouched(true); set('name', e.target.value) }} placeholder={t('My website')} required maxLength={150} />
        </Field>
        <Field label={t('Flag')} htmlFor="flag" help={t('Shown on dashboards and status pages.')}>
          <CountryPicker id="flag" value={d.country} onChange={(v) => set('country', v)} />
        </Field>
      </div>}
    </div>
  )

  /* ---------- step 3: alerts ---------- */
  const intervals = [20, 30, 60, 300]
  const latencyOn = (c.latencyAlertMs ?? 0) > 0
  const alertsStep = (
    <div className="space-y-6">
      <div>
        <h2 className="font-display text-xl font-semibold">{t('When should we tell you?')}</h2>
        <p className="text-sm text-muted mt-1">{t('Alerts go out when it goes down and when it comes back.')}</p>
      </div>

      <Field label={d.type === 'push' ? t('Expected every') : t('Check every')}>
        <div className="flex flex-wrap items-center gap-2">
          {intervals.map((s) => (
            <Choice key={s} on={d.interval === s} onClick={() => setD({ ...d, interval: s, retryInterval: d.retryInterval === d.interval ? s : d.retryInterval })}>{duration(s)}</Choice>
          ))}
          <div className="w-40"><NumberInput value={d.interval} min={10} max={86400} suffix={t('seconds')} onChange={(v) => setD({ ...d, interval: v, retryInterval: d.retryInterval === d.interval ? v : d.retryInterval })} /></div>
        </div>
      </Field>

      <Field label={t('Send alerts to')}>
        {notifications.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-line-strong p-4 flex flex-wrap items-center justify-between gap-3">
            <p className="text-sm text-muted">{t('No channels yet. Set up Telegram once and every monitor can use it.')}</p>
            <Button type="button" variant="soft" icon={<Plus className="size-4" />} onClick={() => setNotifOpen(true)}>{t('Set up Telegram')}</Button>
          </div>
        ) : (
          <div className="flex flex-wrap gap-2">
            {notifications.map((nt) => {
              const on = d.notificationIds.includes(nt.id)
              return (
                <Choice key={nt.id} on={on} onClick={() => set('notificationIds', on ? d.notificationIds.filter((x) => x !== nt.id) : [...d.notificationIds, nt.id])} className="inline-flex items-center gap-2">
                  {on ? <Check className="size-3.5 text-accent-ink" /> : <Bell className="size-3.5" />}<span dir="auto">{nt.name}</span>
                </Choice>
              )
            })}
            <Choice on={false} onClick={() => setNotifOpen(true)} className="inline-flex items-center gap-1.5"><Plus className="size-3.5" />{t('New channel')}</Choice>
          </div>
        )}
      </Field>

      {hasLatency && (
        <div className="rounded-2xl border border-line p-4 space-y-4">
          <Switch checked={latencyOn} onChange={(v) => setD({ ...d, config: { ...c, latencyAlertMs: v ? (c.latencyAlertMs || 300) : 0, latencyAlertFor: v ? (c.latencyAlertFor ?? 120) : 0 } })}
            label={t('Alert when it gets slow')} description={t('Up, but the ping or response time stays above a limit — common for tunnels before they drop.')} />
          {latencyOn && (
            <div className="grid sm:grid-cols-2 gap-4">
              <Field label={t('Slower than')} htmlFor="lat">
                <NumberInput id="lat" value={c.latencyAlertMs ?? 300} min={1} max={600000} suffix="ms" onChange={(v) => cfg('latencyAlertMs', v)} />
              </Field>
              <Field label={t('For at least')} htmlFor="latf">
                <Select id="latf" value={c.latencyAlertFor ?? 120} onChange={(e) => cfg('latencyAlertFor', Number(e.target.value))}>
                  {[0, 60, 120, 300, 600, 900, 1800, 3600].map((s) => <option key={s} value={s}>{s === 0 ? t('Right away') : t('{n} minutes', { n: s / 60 })}</option>)}
                </Select>
              </Field>
              <p className="sm:col-span-2 text-[12.5px] text-muted">{t('You get one alert when it starts and one when the response time is back under the limit.')}</p>
            </div>
          )}
        </div>
      )}
    </div>
  )

  /* ---------- step 4: finish ---------- */
  const chosen = notifications.filter((nt) => d.notificationIds.includes(nt.id)).map((nt) => nt.name)
  const summary: [string, ReactNode][] = [
    [t('Type'), t(typeInfo[d.type].label)],
    [t('Name'), <span dir="auto">{d.name}</span>],
    ...(target ? [[t('Target'), <span dir="ltr" className="font-mono text-[13px] break-all">{target}</span>] as [string, ReactNode]] : []),
    [d.type === 'push' ? t('Expected every') : t('Check every'), duration(d.interval)],
    [t('Alerts'), chosen.length ? <span dir="auto">{chosen.join('، ')}</span> : <span className="text-pending">{t('No channel — nobody is told')}</span>],
    ...(latencyOn ? [[t('Slow alert'), t('above {ms} ms for {d}', { ms: c.latencyAlertMs ?? 0, d: c.latencyAlertFor ? duration(c.latencyAlertFor) : t('Right away') })] as [string, ReactNode]] : []),
  ]
  const finishStep = (
    <div className="space-y-5">
      <div>
        <h2 className="font-display text-xl font-semibold">{mode === 'edit' ? t('Everything else') : t('Ready to go')}</h2>
        <p className="text-sm text-muted mt-1">{t('Check the summary. The rest is optional.')}</p>
      </div>
      <dl className="rounded-2xl border border-line divide-y divide-line">
        {summary.map(([k, v]) => (
          <div key={k} className="flex items-start justify-between gap-4 px-4 py-2.5 text-sm">
            <dt className="text-muted shrink-0">{k}</dt><dd className="text-end min-w-0">{v}</dd>
          </div>
        ))}
      </dl>

      <More title={t('Retries and timing')}>
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label={t('Retries before down')} htmlFor="rt2" help={t('Failed checks in a row shown as pending before alerting.')}>
            <NumberInput id="rt2" value={d.maxRetries} min={0} max={20} onChange={(v) => set('maxRetries', v)} />
          </Field>
          <Field label={t('Retry every')} htmlFor="ri" help={t('How soon to re-check while pending.')}>
            <NumberInput id="ri" value={d.retryInterval} min={10} max={86400} suffix={t('seconds')} onChange={(v) => set('retryInterval', v)} />
          </Field>
          {d.type !== 'push' && d.type !== 'group' && (
            <Field label={t('Timeout')} htmlFor="to" help={t('Leave 0 for automatic ({n}s).', { n: autoTimeout })}>
              <NumberInput id="to" value={d.timeout} min={0} max={d.interval} suffix={t('seconds')} onChange={(v) => set('timeout', v)} />
            </Field>
          )}
          <Field label={t('Re-send alert while down')} htmlFor="rs2" help={t('Every N checks. 0 sends one alert per outage.')}>
            <NumberInput id="rs2" value={d.resendInterval} min={0} suffix={t('checks')} onChange={(v) => set('resendInterval', v)} />
          </Field>
        </div>
        {d.type !== 'group' && (
          <Switch checked={d.upsideDown} onChange={(v) => set('upsideDown', v)} label={t('Upside-down mode')} description={t('Flip the result: reachable means down. For things that should stay closed.')} />
        )}
        {d.type.endsWith('tunnel') && (
          <Switch checked={!!c.serviceDownIsDown} onChange={(v) => cfg('serviceDownIsDown', v)} label={t('Treat “service refusing” as down')} description={t('The tunnel can be up while the service behind it refuses connections. Off: show it as a note; on: alert.')} />
        )}
        {d.type === 'ping' && (
          <Field label={t('Packets per check')} help={t('The average round-trip is recorded; all lost means down.')} htmlFor="pc">
            <NumberInput id="pc" value={c.packetCount ?? 3} min={1} max={20} onChange={(v) => cfg('packetCount', v)} />
          </Field>
        )}
      </More>

      {isHttp && (
        <More title={t('HTTP options')}>
          <div className="grid sm:grid-cols-3 gap-4">
            <Field label={t('Method')} htmlFor="mt">
              <Select id="mt" value={c.method ?? 'GET'} onChange={(e) => cfg('method', e.target.value)}>
                {['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'].map((x) => <option key={x}>{x}</option>)}
              </Select>
            </Field>
            <Field label={t('Max redirects')} htmlFor="mr" help={t('0 to not follow.')}>
              <NumberInput id="mr" value={c.maxRedirects ?? 10} min={0} max={30} onChange={(v) => cfg('maxRedirects', v)} />
            </Field>
            <Field label={t('Body encoding')} htmlFor="be">
              <Select id="be" value={c.bodyEncoding ?? 'json'} onChange={(e) => cfg('bodyEncoding', e.target.value)}>
                <option value="json">JSON</option><option value="form">Form</option><option value="xml">XML</option><option value="raw">{t('Raw text')}</option>
              </Select>
            </Field>
          </div>
          <Field label={t('Accepted status codes')}>
            <div className="flex flex-wrap gap-2" dir="ltr">
              {[...new Set([...codePresets, ...(c.acceptedStatusCodes ?? [])])].map((code) => {
                const on = (c.acceptedStatusCodes ?? []).includes(code)
                return (
                  <button key={code} type="button" aria-pressed={on} onClick={() => cfg('acceptedStatusCodes', on ? (c.acceptedStatusCodes ?? []).filter((x) => x !== code) : [...(c.acceptedStatusCodes ?? []), code])}
                    className={clsx('h-8 px-3 rounded-lg border text-[13px] font-mono transition-colors', on ? 'bg-accent-soft border-accent/40 text-accent-ink' : 'bg-surface-2 border-line text-muted hover:text-fg')}>
                    {code}
                  </button>
                )
              })}
              <div className="flex gap-1">
                <Input value={customCode} onChange={(e) => setCustomCode(e.target.value)} placeholder="401" className="h-8 w-24 font-mono" aria-label={t('Custom status code')} />
                <Button type="button" size="sm" onClick={() => { if (customCode.trim()) { cfg('acceptedStatusCodes', [...(c.acceptedStatusCodes ?? []), customCode.trim()]); setCustomCode('') } }}>{t('Add')}</Button>
              </div>
            </div>
          </Field>
          <div className="grid sm:grid-cols-2 gap-4">
            <Field label={t('Request body')} htmlFor="bd"><Textarea id="bd" dir="ltr" className="font-mono text-[13px]" value={c.body ?? ''} onChange={(e) => cfg('body', e.target.value)} placeholder='{"ping": true}' /></Field>
            <Field label={t('Headers (JSON)')} htmlFor="hd" error={c.headers && (() => { try { JSON.parse(c.headers!); return undefined } catch { return t('Not valid JSON') } })() || undefined}>
              <Textarea id="hd" dir="ltr" className="font-mono text-[13px]" value={c.headers ?? ''} onChange={(e) => cfg('headers', e.target.value)} placeholder={'{\n  "Authorization": "Bearer …"\n}'} />
            </Field>
          </div>
          <div className="grid sm:grid-cols-2 gap-4">
            <Field label={t('Basic auth user')} htmlFor="bu"><Input id="bu" dir="ltr" autoComplete="off" value={c.basicAuthUser ?? ''} onChange={(e) => cfg('basicAuthUser', e.target.value)} /></Field>
            <Field label={t('Basic auth password')} htmlFor="bp"><Input id="bp" dir="ltr" type="password" autoComplete="new-password" value={c.basicAuthPass ?? ''} onChange={(e) => cfg('basicAuthPass', e.target.value)} /></Field>
          </div>
          <div className="grid sm:grid-cols-2 gap-4">
            <Switch checked={!!c.ignoreTls} onChange={(v) => cfg('ignoreTls', v)} label={t('Ignore TLS errors')} description={t('Accept self-signed or expired certificates.')} />
            <Switch checked={!!c.certExpiryNotify} onChange={(v) => cfg('certExpiryNotify', v)} label={t('Certificate expiry alerts')} description={t('Warn 21, 14 and 7 days before (Settings → History).')} />
          </div>
        </More>
      )}
      {d.type === 'websocket' && (
        <More title={t('Options')}>
          <Switch checked={!!c.ignoreTls} onChange={(v) => cfg('ignoreTls', v)} label={t('Ignore TLS errors')} description={t('Accept self-signed or expired certificates.')} />
        </More>
      )}

      <More title={t('Group, description and tags')}>
        <Field label={t('Group')} htmlFor="grp" help={t('Groups go down when a member does; their maintenance applies to members.')}>
          <Select id="grp" value={d.parentId ?? ''} onChange={(e) => set('parentId', e.target.value ? Number(e.target.value) : null)}>
            <option value="">{t('None')}</option>
            {groups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
          </Select>
        </Field>
        <Field label={t('Description')} htmlFor="desc"><Textarea id="desc" dir="auto" className="min-h-20" value={d.description} onChange={(e) => set('description', e.target.value)} placeholder={t('What this is and who to call')} /></Field>
        <Field label={t('Tags')}>
          <div className="space-y-2.5">
            {d.tags.length > 0 && (
              <div className="flex flex-wrap gap-1.5">
                {d.tags.map((tg, i) => (
                  <span key={i} className="inline-flex items-center gap-1">
                    <TagChip {...tg} />
                    <button type="button" aria-label={t('Remove tag {name}', { name: tg.name })} onClick={() => set('tags', d.tags.filter((_, j) => j !== i))} className="text-subtle hover:text-down"><X className="size-3.5" /></button>
                  </span>
                ))}
              </div>
            )}
            <div className="flex gap-2">
              <Select className="flex-1" aria-label={t('Tag')} value={newTag.tagId || ''} onChange={(e) => setNewTag({ ...newTag, tagId: Number(e.target.value) })}>
                <option value="">{t('Add a tag…')}</option>
                {tags.map((tg) => <option key={tg.id} value={tg.id}>{tg.name}</option>)}
                <option value={-1}>{t('+ New tag')}</option>
              </Select>
              <Input className="w-24" aria-label={t('Tag value')} placeholder={t('value')} value={newTag.value} onChange={(e) => setNewTag({ ...newTag, value: e.target.value })} />
            </div>
            {newTag.tagId === -1 && (
              <div className="flex gap-2">
                <Input className="flex-1" placeholder={t('Tag name')} aria-label={t('New tag name')} value={newTag.name} onChange={(e) => setNewTag({ ...newTag, name: e.target.value })} />
                <input type="color" aria-label={t('Tag colour')} value={newTag.color} onChange={(e) => setNewTag({ ...newTag, color: e.target.value })} className="h-10 w-12 rounded-xl border border-line-strong bg-surface-2 p-1 cursor-pointer" />
              </div>
            )}
            {newTag.tagId !== 0 && <Button type="button" size="sm" icon={<Plus className="size-3.5" />} onClick={addTag}>{t('Add tag')}</Button>}
          </div>
        </Field>
      </More>
    </div>
  )

  const counts = { bp: found?.backpack?.tunnels.length ?? 0, kz: found?.kariz?.tunnels.length ?? 0 }
  const serverStep = (
    <div className="space-y-5">
      <div>
        <h2 className="font-display text-xl font-semibold">{t('Connect the server')}</h2>
        <p className="text-sm text-muted mt-1">{t('Run one command on the server, as root. This page waits for its first report, then starts the monitoring.')}</p>
      </div>
      {!created ? (
        <div className="rounded-2xl border border-line p-4 flex items-center gap-3">
          <span className="grid place-items-center size-10 shrink-0 rounded-xl bg-accent-soft text-accent-ink"><Server className="size-5" /></span>
          <div className="min-w-0">
            <p className="font-semibold truncate" dir="auto">{srv.name}</p>
            <p className="text-[13px] text-muted">{t('Click “Create server” to get its install command.')}</p>
          </div>
        </div>
      ) : (
        <>
          <InstallBox command={created.install} />
          {!found ? (
            <div className="flex items-center gap-3 rounded-2xl border border-dashed border-line-strong p-4">
              <Spinner className="size-5" />
              <div>
                <p className="font-medium">{t('Waiting for the first report…')}</p>
                <p className="text-[13px] text-muted">{t('Run the install command on the server; it appears here within seconds.')}</p>
              </div>
            </div>
          ) : (
            <div className="space-y-3">
              <p className="flex items-center gap-2 font-medium text-up"><CircleCheck className="size-5" />{t('Connected — {bp} BackPack and {kz} Kariz tunnels found.', counts)}</p>
              {tunnelKeys(found).length > 0 && (
                <div className="rounded-2xl border border-line divide-y divide-line max-h-60 overflow-y-auto">
                  {(['backpack', 'kariz'] as const).flatMap((src) => (found[src]?.tunnels ?? []).map((tn) => {
                    const k = `${src}/${tn.name}`
                    return (
                      <Checkbox key={k} className="px-4 py-2.5" checked={pick.has(k)}
                        onChange={(v) => { const s = new Set(pick); if (v) s.add(k); else s.delete(k); setPick(s) }}
                        label={<span className="inline-flex flex-wrap items-center gap-2"><SourceBadge source={src} /><span className="font-medium" dir="auto">{tn.name}</span><span className="text-[12px] text-muted" dir="ltr">{(tn.carrier || tn.transport || '').toUpperCase()} · {tn.state}</span></span>} />
                    )
                  }))}
                </div>
              )}
              <Checkbox checked={laterToo} onChange={setLaterToo} label={t('Also add tunnels created later')} />
            </div>
          )}
        </>
      )}
    </div>
  )

  const content = [typeStep, detailsStep, alertsStep, serverFlow ? serverStep : finishStep][step]
  const back = mode === 'add' ? '/dashboard' : `/dashboard/${id}`

  return (
    <form onSubmit={submit} noValidate className="flex flex-col h-[calc(100dvh-3.5rem)] lg:h-dvh p-4 sm:p-6 lg:p-8 gap-4 max-w-[980px]">
      <div className="flex flex-wrap items-center justify-between gap-3 shrink-0">
        <div className="min-w-0">
          <Link to={back} className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-fg"><ArrowLeft className="size-4 rtl:rotate-180" />{t('Back')}</Link>
          <h1 className="mt-1 font-display text-[24px] font-semibold tracking-tight truncate" dir="auto">{title}</h1>
        </div>
        <ol className="flex items-center gap-1.5" aria-label={t('Steps')}>
          {steps.map((s, i) => (
            <li key={s} className="flex items-center gap-1.5">
              <button type="button" onClick={() => goTo(i)} aria-current={i === step ? 'step' : undefined}
                className={clsx('flex items-center gap-2 h-9 ps-1.5 pe-3 rounded-full text-[13px] font-medium transition-colors',
                  i === step ? 'bg-accent-soft text-fg' : i < step || mode !== 'add' ? 'text-muted hover:text-fg hover:bg-surface-2' : 'text-subtle')}>
                <span className={clsx('grid place-items-center size-6 rounded-full text-[12px] font-bold tnum', i < step ? 'bg-accent text-on-accent' : i === step ? 'bg-accent text-on-accent' : 'bg-surface-3 text-muted')}>
                  {i < step ? <Check className="size-3.5" /> : n(i + 1)}
                </span>
                <span className="hidden sm:inline">{t(s)}</span>
              </button>
              {i < steps.length - 1 && <span className="w-4 h-px bg-line-strong hidden sm:block" />}
            </li>
          ))}
        </ol>
      </div>

      <div className="flex-1 min-h-0 rounded-[22px] border border-line bg-surface shadow-[var(--shadow-card)] flex flex-col">
        <div ref={body} className="flex-1 min-h-0 overflow-y-auto p-5 sm:p-7">
          {error && <Alert tone="danger" className="mb-5">{error}</Alert>}
          {content}
        </div>
        <div className="shrink-0 flex items-center justify-between gap-2 border-t border-line px-5 sm:px-7 py-3.5">
          <Button type="button" variant="ghost" onClick={() => (step > 0 ? setStep(step - 1) : nav(back))}>{step > 0 ? t('Previous') : t('Cancel')}</Button>
          <span className="text-[12.5px] text-subtle tnum hidden sm:inline">{t('Step {n} of {total}', { n: step + 1, total: steps.length })}</span>
          <div className="flex gap-2">
            {mode !== 'add' && step < last && <Button type="button" variant="soft" loading={busy} icon={<Save className="size-4" />} onClick={() => stepValid() && save()}>{t('Save changes')}</Button>}
            {step < last ? (
              step > 0 || mode !== 'add' ? <Button type="submit" variant="primary" icon={<ArrowRight className="size-4 rtl:rotate-180" />}>{t('Next')}</Button> : null
            ) : (
              serverFlow ? (
                !created ? <Button type="submit" variant="primary" loading={busy} icon={<Server className="size-4" />}>{t('Create server')}</Button>
                  : !found ? <Button type="button" variant="primary" disabled icon={<Spinner className="size-4" />}>{t('Waiting for the server…')}</Button>
                    : <Button type="submit" variant="primary" loading={busy} icon={<Play className="size-4" />}>{t('Start monitoring')}</Button>
              ) : <Button type="submit" variant="primary" loading={busy} icon={<Save className="size-4" />}>{mode === 'edit' ? t('Save changes') : t('Create monitor')}</Button>
            )}
          </div>
        </div>
      </div>
      <NotificationDialog open={notifOpen} onClose={() => setNotifOpen(false)} initialType="telegram"
        onSaved={(nt) => setD((cur) => (cur ? { ...cur, notificationIds: [...new Set([...cur.notificationIds, nt.id])] } : cur))} />
    </form>
  )
}
