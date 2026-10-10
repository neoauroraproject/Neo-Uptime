import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, Bell, Plus, Save, X } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { clsx } from '../lib/format'
import type { Monitor, MonitorConfig, MonitorTag, MonitorType, PanelSnapshot } from '../lib/types'
import { Alert, Button, Card, CardHeader, Checkbox, Empty, Field, Input, Select, Skeleton, Switch, TagChip, Textarea, toast } from '../components/ui'
import { TypeIcon, typeGroups, typeInfo } from '../components/meta'

type Draft = Omit<Monitor, 'id' | 'createdAt' | 'updatedAt' | 'sortOrder'>

const httpTypes: MonitorType[] = ['http', 'keyword', 'json-query']
const codePresets = ['200-299', '300-399', '400-499', '500-599']

function blank(defaults: number[]): Draft {
  return {
    name: '', type: 'http', parentId: null, active: true, interval: 60, retryInterval: 60, maxRetries: 1, timeout: 0,
    resendInterval: 0, upsideDown: false, description: '', tags: [], notificationIds: defaults,
    config: { url: 'https://', method: 'GET', acceptedStatusCodes: ['200-299'], maxRedirects: 10, bodyEncoding: 'json', dnsResolver: '1.1.1.1', dnsRecordType: 'A', port: 0, packetCount: 3, jsonOperator: '==' },
  }
}

function Section({ title, subtitle, children }: { title: string; subtitle?: string; children: ReactNode }) {
  return (
    <Card>
      <CardHeader title={title} subtitle={subtitle} />
      <div className="px-5 pb-5 space-y-4">{children}</div>
    </Card>
  )
}

function NumberInput({ value, onChange, min, max, suffix, id }: { value: number; onChange: (n: number) => void; min?: number; max?: number; suffix?: string; id?: string }) {
  return (
    <div className="relative">
      <Input id={id} type="number" inputMode="numeric" min={min} max={max} value={Number.isFinite(value) ? value : ''} onChange={(e) => onChange(e.target.value === '' ? 0 : Number(e.target.value))} className={suffix ? 'pr-16' : ''} />
      {suffix && <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[13px] text-subtle">{suffix}</span>}
    </div>
  )
}

export default function MonitorForm({ mode }: { mode: 'add' | 'edit' | 'clone' }) {
  const { id } = useParams()
  const nav = useNavigate()
  const monitors = useApp((s) => s.monitors)
  const tags = useApp((s) => s.tags)
  const notifications = useApp((s) => s.notifications)
  const panels = useApp((s) => s.panels)
  const role = useApp((s) => s.role)
  const [d, setD] = useState<Draft | null>(mode === 'add' ? blank(notifications.filter((n) => n.isDefault).map((n) => n.id)) : null)
  const [nameTouched, setNameTouched] = useState(mode !== 'add')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [snap, setSnap] = useState<PanelSnapshot | null>(null)
  const [snapLoading, setSnapLoading] = useState(false)
  const [newTag, setNewTag] = useState({ tagId: 0, name: '', color: '#a3e635', value: '' })
  const [customCode, setCustomCode] = useState('')

  useEffect(() => {
    if (mode === 'add' || !id) return
    api.get<Monitor>(`/api/monitors/${id}`).then((m) => {
      const { id: _i, createdAt: _c, updatedAt: _u, sortOrder: _s, ...rest } = m
      void _i; void _c; void _u; void _s
      setD(mode === 'clone' ? { ...rest, name: `${m.name} (copy)`, pushToken: '' } : rest)
    }).catch((e) => setError(e.message))
  }, [id, mode])

  // Defaults loaded after the form opened still apply to a new monitor.
  useEffect(() => {
    if (mode !== 'add' || !d || d.notificationIds.length) return
    const defs = notifications.filter((n) => n.isDefault).map((n) => n.id)
    if (defs.length) setD({ ...d, notificationIds: defs })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [notifications])

  const panelId = d?.config.panelId
  useEffect(() => {
    if (!panelId || (d?.type !== 'backpack-tunnel')) return
    setSnapLoading(true)
    api.get<{ snapshot: PanelSnapshot }>(`/api/backpack/panels/${panelId}/snapshot`)
      .then((r) => setSnap(r.snapshot))
      .catch(() => setSnap(null))
      .finally(() => setSnapLoading(false))
  }, [panelId, d?.type])

  const groups = useMemo(() => Object.values(monitors).filter((m) => m.type === 'group' && m.id !== Number(id)), [monitors, id])

  if (!canEdit(role)) return <Empty title="Read-only account" body="Ask an admin for editor access to add or change monitors." />
  if (!d) return <div className="p-8 space-y-4 max-w-5xl">{error ? <Alert tone="danger">{error}</Alert> : <><Skeleton className="h-10 w-64" /><Skeleton className="h-64" /><Skeleton className="h-64" /></>}</div>

  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setD({ ...d, [k]: v })
  const cfg = <K extends keyof MonitorConfig>(k: K, v: MonitorConfig[K]) => setD({ ...d, config: { ...d.config, [k]: v } })
  const c = d.config
  const isHttp = httpTypes.includes(d.type)
  const autoTimeout = Math.min(48, Math.max(5, Math.round(d.interval * 0.8)))

  const onUrl = (url: string) => {
    const next = { ...d, config: { ...c, url } }
    if (!nameTouched) {
      try {
        next.name = new URL(url).hostname
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
        const t = await api.post<{ id: number; name: string; color: string }>('/api/tags', { name: newTag.name.trim(), color: newTag.color })
        await useApp.getState().loadTags()
        tagId = t.id; name = t.name; color = t.color
      } catch (e) {
        toast.error('Could not create tag', (e as Error).message)
        return
      }
    } else {
      const t = tags.find((x) => x.id === tagId)
      if (!t) return
      name = t.name; color = t.color
    }
    const tag: MonitorTag = { tagId, name, color, value: newTag.value.trim() }
    if (!d.tags.some((x) => x.tagId === tag.tagId && x.value === tag.value)) set('tags', [...d.tags, tag])
    setNewTag({ tagId: 0, name: '', color: '#a3e635', value: '' })
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    const body = { ...d, timeout: d.timeout || 0 }
    try {
      const saved = mode === 'edit'
        ? await api.put<Monitor>(`/api/monitors/${id}`, body)
        : await api.post<Monitor>('/api/monitors', body)
      toast.success(mode === 'edit' ? 'Monitor saved' : 'Monitor created', mode === 'edit' ? undefined : 'The first check is running now.')
      await useApp.getState().loadDashboard()
      nav(`/dashboard/${saved.id}`)
    } catch (err) {
      setError((err as Error).message)
      window.scrollTo({ top: 0, behavior: 'smooth' })
    } finally {
      setBusy(false)
    }
  }

  const title = mode === 'edit' ? `Edit ${d.name || 'monitor'}` : mode === 'clone' ? 'Clone monitor' : 'Add monitor'

  return (
    <form onSubmit={submit} className="p-4 sm:p-6 lg:p-8 pb-28 max-w-[1300px]">
      <Link to={mode === 'add' ? '/dashboard' : `/dashboard/${id}`} className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-fg"><ArrowLeft className="size-4" />Back</Link>
      <h1 className="mt-3 font-display text-[28px] font-semibold tracking-tight">{title}</h1>
      {error && <Alert tone="danger" className="mt-4">{error}</Alert>}

      <div className="mt-6 grid xl:grid-cols-[minmax(0,1fr)_380px] gap-5 items-start">
        <div className="space-y-5 min-w-0">
          <Section title="What to monitor">
            <div role="radiogroup" aria-label="Monitor type" className="space-y-3">
              {typeGroups.map((g) => (
                <div key={g}>
                  <p className="text-[12px] uppercase tracking-wider text-subtle font-semibold mb-1.5">{g}</p>
                  <div className="grid grid-cols-2 sm:grid-cols-3 2xl:grid-cols-4 gap-2">
                    {(Object.keys(typeInfo) as MonitorType[]).filter((t) => typeInfo[t].group === g).map((t) => (
                      <button
                        key={t}
                        type="button"
                        role="radio"
                        aria-checked={d.type === t}
                        disabled={mode === 'edit' && d.type !== t && (d.type === 'push' || t === 'push')}
                        onClick={() => setD({ ...d, type: t, config: { ...c, panelId: t.startsWith('backpack') ? c.panelId || panels[0]?.id : c.panelId } })}
                        className={clsx(
                          'flex items-center gap-2.5 rounded-xl border px-3 h-11 text-left text-[13.5px] font-medium transition-colors disabled:opacity-40',
                          d.type === t ? 'border-accent bg-accent-soft text-fg' : 'border-line bg-surface-2 text-muted hover:text-fg hover:border-line-strong',
                        )}
                      >
                        <TypeIcon type={t} className={clsx('size-4 shrink-0', d.type === t && 'text-accent-ink')} />
                        <span className="truncate">{typeInfo[t].label}</span>
                      </button>
                    ))}
                  </div>
                </div>
              ))}
            </div>
            <p className="text-[13px] text-muted">{typeInfo[d.type].help}</p>
          </Section>

          <Section title="Target">
            <Field label="Friendly name" required htmlFor="name">
              <Input id="name" value={d.name} onChange={(e) => { setNameTouched(true); set('name', e.target.value) }} placeholder="My website" required maxLength={150} />
            </Field>

            {isHttp && (
              <Field label="URL" required htmlFor="url">
                <Input id="url" type="url" value={c.url ?? ''} onChange={(e) => onUrl(e.target.value)} placeholder="https://example.com/health" required className="font-mono" />
              </Field>
            )}
            {d.type === 'keyword' && (
              <div className="grid sm:grid-cols-[1fr_auto] gap-4 items-end">
                <Field label="Keyword" required help="Case-sensitive, searched in the response body." htmlFor="kw">
                  <Input id="kw" value={c.keyword ?? ''} onChange={(e) => cfg('keyword', e.target.value)} required />
                </Field>
                <div className="pb-2"><Switch checked={!!c.invertKeyword} onChange={(v) => cfg('invertKeyword', v)} label="Invert" description="Up when NOT found" /></div>
              </div>
            )}
            {d.type === 'json-query' && (
              <div className="grid sm:grid-cols-[1.4fr_0.8fr_1fr] gap-4">
                <Field label="JSON path" required help="e.g. status or data.items[0].ok" htmlFor="jp">
                  <Input id="jp" className="font-mono" value={c.jsonPath ?? ''} onChange={(e) => cfg('jsonPath', e.target.value)} required />
                </Field>
                <Field label="Condition" htmlFor="op">
                  <Select id="op" value={c.jsonOperator ?? '=='} onChange={(e) => cfg('jsonOperator', e.target.value)}>
                    {['==', '!=', '<', '<=', '>', '>=', 'contains', 'notcontains', 'exists'].map((o) => <option key={o} value={o}>{o}</option>)}
                  </Select>
                </Field>
                {c.jsonOperator !== 'exists' && (
                  <Field label="Expected value" htmlFor="ev">
                    <Input id="ev" className="font-mono" value={c.expectedValue ?? ''} onChange={(e) => cfg('expectedValue', e.target.value)} placeholder="ok" />
                  </Field>
                )}
              </div>
            )}

            {['tcp', 'ping', 'dns'].includes(d.type) && (
              <div className={clsx('grid gap-4', d.type === 'tcp' ? 'sm:grid-cols-[1fr_140px]' : '')}>
                <Field label="Hostname or IP" required htmlFor="host">
                  <Input id="host" className="font-mono" value={c.hostname ?? ''} onChange={(e) => onHost(e.target.value)} placeholder={d.type === 'dns' ? 'example.com' : '203.0.113.10'} required />
                </Field>
                {d.type === 'tcp' && (
                  <Field label="Port" required htmlFor="port">
                    <NumberInput id="port" value={c.port ?? 0} min={1} max={65535} onChange={(n) => cfg('port', n)} />
                  </Field>
                )}
              </div>
            )}
            {d.type === 'ping' && (
              <Field label="Packets per check" help="The average round-trip is recorded; all lost means down." htmlFor="pc">
                <NumberInput id="pc" value={c.packetCount ?? 3} min={1} max={20} onChange={(n) => cfg('packetCount', n)} />
              </Field>
            )}
            {d.type === 'dns' && (
              <div className="grid sm:grid-cols-3 gap-4">
                <Field label="Resolver" htmlFor="rs"><Input id="rs" className="font-mono" value={c.dnsResolver ?? ''} onChange={(e) => cfg('dnsResolver', e.target.value)} placeholder="1.1.1.1" /></Field>
                <Field label="Record type" htmlFor="rt">
                  <Select id="rt" value={c.dnsRecordType ?? 'A'} onChange={(e) => cfg('dnsRecordType', e.target.value)}>
                    {['A', 'AAAA', 'CNAME', 'MX', 'NS', 'TXT', 'SRV'].map((t) => <option key={t}>{t}</option>)}
                  </Select>
                </Field>
                <Field label="Expect (optional)" help="Down unless a record contains this." htmlFor="ex"><Input id="ex" className="font-mono" value={c.expectedValue ?? ''} onChange={(e) => cfg('expectedValue', e.target.value)} /></Field>
              </div>
            )}

            {d.type === 'docker' && (
              <div className="grid sm:grid-cols-2 gap-4">
                <Field label="Container name or ID" required htmlFor="cn"><Input id="cn" className="font-mono" value={c.containerName ?? ''} onChange={(e) => cfg('containerName', e.target.value)} required /></Field>
                <Field label="Docker host" help="Socket path or tcp://host:2375" htmlFor="dh"><Input id="dh" className="font-mono" value={c.dockerHost ?? ''} onChange={(e) => cfg('dockerHost', e.target.value)} placeholder="/var/run/docker.sock" /></Field>
              </div>
            )}
            {['postgres', 'mysql', 'redis'].includes(d.type) && (
              <>
                <Field label="Connection string" required htmlFor="cs" help={d.type === 'postgres' ? 'postgres://user:pass@host:5432/db?sslmode=disable' : d.type === 'mysql' ? 'mysql://user:pass@host:3306/db' : 'redis://:password@host:6379/0 (rediss:// for TLS)'}>
                  <Input id="cs" type="password" autoComplete="off" className="font-mono" value={c.connectionString ?? ''} onChange={(e) => cfg('connectionString', e.target.value)} required />
                </Field>
                {d.type !== 'redis' && (
                  <Field label="Query (optional)" htmlFor="q" help="Run after connecting; leave empty for a plain connection check.">
                    <Input id="q" className="font-mono" value={c.query ?? ''} onChange={(e) => cfg('query', e.target.value)} placeholder="SELECT 1" />
                  </Field>
                )}
              </>
            )}

            {d.type.startsWith('backpack') && (
              panels.length === 0 ? (
                <Alert tone="info" title="No BackPack panel connected yet">
                  Connect your panel first under <Link className="text-accent-ink underline" to="/tunnels/panels">Tunnels → Panels</Link>; its tunnels can then be imported in one click.
                </Alert>
              ) : (
                <div className="grid sm:grid-cols-2 gap-4">
                  <Field label="BackPack panel" required htmlFor="pn">
                    <Select id="pn" value={c.panelId ?? ''} onChange={(e) => setD({ ...d, config: { ...c, panelId: Number(e.target.value), tunnelName: '' } })}>
                      <option value="" disabled>Choose…</option>
                      {panels.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                    </Select>
                  </Field>
                  {d.type === 'backpack-tunnel' && (
                    <Field label="Tunnel" required htmlFor="tn" help={snap?.error ? `Panel answered: ${snap.error}` : snapLoading ? 'Reading tunnels from the panel…' : undefined}>
                      {snap?.tunnels?.length ? (
                        <Select id="tn" value={c.tunnelName ?? ''} onChange={(e) => setD({ ...d, config: { ...c, tunnelName: e.target.value }, name: nameTouched ? d.name : e.target.value })}>
                          <option value="" disabled>Choose…</option>
                          {snap.tunnels.map((t) => <option key={t.name} value={t.name}>{t.name} — {(t.carrier || t.transport).toUpperCase()} · {t.state}</option>)}
                        </Select>
                      ) : (
                        <Input id="tn" value={c.tunnelName ?? ''} onChange={(e) => cfg('tunnelName', e.target.value)} placeholder="Tunnel name as shown in BackPack" />
                      )}
                    </Field>
                  )}
                  {d.type === 'backpack-tunnel' && (
                    <div className="sm:col-span-2">
                      <Switch checked={!!c.serviceDownIsDown} onChange={(v) => cfg('serviceDownIsDown', v)} label="Treat “service refusing” as down" description="BackPack can see the tunnel up while the service behind it refuses connections. Off: show it as a note; on: alert." />
                    </div>
                  )}
                </div>
              )
            )}

            {d.type === 'push' && (
              <Alert tone="info" title="Push URL">
                {d.pushToken ? <>Your script should call <code className="font-mono text-fg break-all">/api/push/{d.pushToken}?status=up&amp;msg=OK&amp;ping=</code></> : 'A private URL is generated when you save. Your script calls it; if it stays silent longer than the interval, the monitor goes down.'}
              </Alert>
            )}
          </Section>

          <Section title="Checks & alerts">
            <div className="grid sm:grid-cols-2 gap-4">
              <Field label={d.type === 'push' ? 'Expected every' : 'Check every'} htmlFor="iv" help="Minimum 10 seconds.">
                <NumberInput id="iv" value={d.interval} min={10} max={86400} suffix="seconds" onChange={(n) => setD({ ...d, interval: n, retryInterval: d.retryInterval === d.interval ? n : d.retryInterval })} />
              </Field>
              <Field label="Retries before down" htmlFor="rt2" help="Failed checks in a row shown as pending before alerting.">
                <NumberInput id="rt2" value={d.maxRetries} min={0} max={20} onChange={(n) => set('maxRetries', n)} />
              </Field>
              <Field label="Retry every" htmlFor="ri" help="How soon to re-check while pending.">
                <NumberInput id="ri" value={d.retryInterval} min={10} max={86400} suffix="seconds" onChange={(n) => set('retryInterval', n)} />
              </Field>
              {d.type !== 'push' && d.type !== 'group' && (
                <Field label="Timeout" htmlFor="to" help={`Leave 0 for automatic (${autoTimeout}s).`}>
                  <NumberInput id="to" value={d.timeout} min={0} max={d.interval} suffix="seconds" onChange={(n) => set('timeout', n)} />
                </Field>
              )}
              <Field label="Re-send alert while down" htmlFor="rs2" help="Every N checks. 0 sends one alert per outage.">
                <NumberInput id="rs2" value={d.resendInterval} min={0} suffix="checks" onChange={(n) => set('resendInterval', n)} />
              </Field>
            </div>
            {d.type !== 'group' && (
              <Switch checked={d.upsideDown} onChange={(v) => set('upsideDown', v)} label="Upside-down mode" description="Flip the result: reachable means down. For things that should stay closed." />
            )}
          </Section>

          {isHttp && (
            <Section title="HTTP options">
              <div className="grid sm:grid-cols-3 gap-4">
                <Field label="Method" htmlFor="mt">
                  <Select id="mt" value={c.method ?? 'GET'} onChange={(e) => cfg('method', e.target.value)}>
                    {['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'].map((x) => <option key={x}>{x}</option>)}
                  </Select>
                </Field>
                <Field label="Max redirects" htmlFor="mr" help="0 to not follow.">
                  <NumberInput id="mr" value={c.maxRedirects ?? 10} min={0} max={30} onChange={(n) => cfg('maxRedirects', n)} />
                </Field>
                <Field label="Body encoding" htmlFor="be">
                  <Select id="be" value={c.bodyEncoding ?? 'json'} onChange={(e) => cfg('bodyEncoding', e.target.value)}>
                    <option value="json">JSON</option><option value="form">Form</option><option value="xml">XML</option><option value="raw">Raw text</option>
                  </Select>
                </Field>
              </div>
              <Field label="Accepted status codes">
                <div className="flex flex-wrap gap-2">
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
                    <Input value={customCode} onChange={(e) => setCustomCode(e.target.value)} placeholder="e.g. 401" className="h-8 w-28 font-mono" aria-label="Custom status code" />
                    <Button type="button" size="sm" onClick={() => { if (customCode.trim()) { cfg('acceptedStatusCodes', [...(c.acceptedStatusCodes ?? []), customCode.trim()]); setCustomCode('') } }}>Add</Button>
                  </div>
                </div>
              </Field>
              <div className="grid sm:grid-cols-2 gap-4">
                <Field label="Request body" htmlFor="bd"><Textarea id="bd" className="font-mono text-[13px]" value={c.body ?? ''} onChange={(e) => cfg('body', e.target.value)} placeholder='{"ping": true}' /></Field>
                <Field label="Headers (JSON)" htmlFor="hd" error={c.headers && (() => { try { JSON.parse(c.headers!); return undefined } catch { return 'Not valid JSON' } })() || undefined}>
                  <Textarea id="hd" className="font-mono text-[13px]" value={c.headers ?? ''} onChange={(e) => cfg('headers', e.target.value)} placeholder={'{\n  "Authorization": "Bearer …"\n}'} />
                </Field>
              </div>
              <div className="grid sm:grid-cols-2 gap-4">
                <Field label="Basic auth user" htmlFor="bu"><Input id="bu" autoComplete="off" value={c.basicAuthUser ?? ''} onChange={(e) => cfg('basicAuthUser', e.target.value)} /></Field>
                <Field label="Basic auth password" htmlFor="bp"><Input id="bp" type="password" autoComplete="new-password" value={c.basicAuthPass ?? ''} onChange={(e) => cfg('basicAuthPass', e.target.value)} /></Field>
              </div>
              <div className="grid sm:grid-cols-2 gap-4">
                <Switch checked={!!c.ignoreTls} onChange={(v) => cfg('ignoreTls', v)} label="Ignore TLS errors" description="Accept self-signed or expired certificates." />
                <Switch checked={!!c.certExpiryNotify} onChange={(v) => cfg('certExpiryNotify', v)} label="Certificate expiry alerts" description="Warn 21, 14 and 7 days before (Settings → History)." />
              </div>
            </Section>
          )}
        </div>

        <div className="space-y-5 xl:sticky xl:top-6">
          <Section title="Organise">
            <Field label="Group" htmlFor="grp" help="Groups go down when a member does; their maintenance applies to members.">
              <Select id="grp" value={d.parentId ?? ''} onChange={(e) => set('parentId', e.target.value ? Number(e.target.value) : null)}>
                <option value="">None</option>
                {groups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
              </Select>
            </Field>
            <Field label="Description" htmlFor="desc"><Textarea id="desc" className="min-h-20" value={d.description} onChange={(e) => set('description', e.target.value)} placeholder="What this is and who to call" /></Field>
            <Field label="Tags">
              <div className="space-y-2.5">
                {d.tags.length > 0 && (
                  <div className="flex flex-wrap gap-1.5">
                    {d.tags.map((t, i) => (
                      <span key={i} className="inline-flex items-center gap-1">
                        <TagChip {...t} />
                        <button type="button" aria-label={`Remove tag ${t.name}`} onClick={() => set('tags', d.tags.filter((_, j) => j !== i))} className="text-subtle hover:text-down"><X className="size-3.5" /></button>
                      </span>
                    ))}
                  </div>
                )}
                <div className="flex gap-2">
                  <Select className="flex-1" aria-label="Tag" value={newTag.tagId || ''} onChange={(e) => setNewTag({ ...newTag, tagId: Number(e.target.value) })}>
                    <option value="">Add a tag…</option>
                    {tags.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
                    <option value={-1}>+ New tag</option>
                  </Select>
                  <Input className="w-24" aria-label="Tag value" placeholder="value" value={newTag.value} onChange={(e) => setNewTag({ ...newTag, value: e.target.value })} />
                </div>
                {newTag.tagId === -1 && (
                  <div className="flex gap-2">
                    <Input className="flex-1" placeholder="Tag name" aria-label="New tag name" value={newTag.name} onChange={(e) => setNewTag({ ...newTag, name: e.target.value })} />
                    <input type="color" aria-label="Tag colour" value={newTag.color} onChange={(e) => setNewTag({ ...newTag, color: e.target.value })} className="h-10 w-12 rounded-xl border border-line-strong bg-surface-2 p-1 cursor-pointer" />
                  </div>
                )}
                {newTag.tagId !== 0 && <Button type="button" size="sm" icon={<Plus className="size-3.5" />} onClick={addTag}>Add tag</Button>}
              </div>
            </Field>
          </Section>

          <Section title="Notifications" subtitle="Who hears about it when this goes down or comes back.">
            {notifications.length === 0 ? (
              <div className="text-sm text-muted">
                No channels yet. <Link to="/settings/notifications" className="text-accent-ink underline">Set up Telegram, email, Discord…</Link>
              </div>
            ) : (
              <div className="space-y-2.5">
                {notifications.map((n) => (
                  <Checkbox
                    key={n.id}
                    checked={d.notificationIds.includes(n.id)}
                    onChange={(v) => set('notificationIds', v ? [...d.notificationIds, n.id] : d.notificationIds.filter((x) => x !== n.id))}
                    label={<span className="inline-flex items-center gap-2"><Bell className="size-3.5 text-muted" />{n.name}<span className="text-[12px] text-subtle">{n.type}</span>{n.isDefault && <span className="text-[11px] text-accent-ink">default</span>}</span>}
                  />
                ))}
              </div>
            )}
          </Section>
        </div>
      </div>

      <div className="fixed bottom-0 left-0 right-0 lg:left-64 z-20 border-t border-line bg-surface/85 backdrop-blur-xl">
        <div className="max-w-[1300px] px-4 sm:px-6 lg:px-8 py-3 flex items-center justify-end gap-2">
          <Button type="button" variant="ghost" onClick={() => nav(-1)}>Cancel</Button>
          <Button type="submit" variant="primary" loading={busy} icon={<Save className="size-4" />}>{mode === 'edit' ? 'Save changes' : 'Create monitor'}</Button>
        </div>
      </div>
    </form>
  )
}
