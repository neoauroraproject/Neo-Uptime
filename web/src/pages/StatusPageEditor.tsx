import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowDown, ArrowLeft, ArrowUp, Cable, ExternalLink, Eye, EyeOff, Megaphone, Plus, Save, Trash2, X } from 'lucide-react'
import { api } from '../lib/api'
import { useApp } from '../lib/store'
import { clsx, fmtDateTime } from '../lib/format'
import type { Incident, StatusPage, Theme } from '../lib/types'
import { Alert, Badge, Button, Card, Field, IconButton, Input, Segmented, Select, Skeleton, Switch, Textarea, confirm, toast } from '../components/ui'
import { ThemeSwatch, slugify, themeMeta } from './StatusPages'
import { TypeIcon } from '../components/meta'

type Tab = 'general' | 'appearance' | 'monitors' | 'incidents'

function IncidentsTab({ pageId }: { pageId: number }) {
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
      toast.success(f.id ? 'Incident updated' : 'Incident posted')
      setF({ title: '', content: '', style: 'warning', pinned: true })
      load()
    } catch (e) {
      toast.error('Could not save', (e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const styles: Incident['style'][] = ['info', 'warning', 'danger', 'success', 'maintenance']
  const tone = (s: string) => (s === 'danger' ? 'down' : s === 'warning' ? 'pending' : s === 'success' ? 'up' : 'maint') as 'down'
  return (
    <div className="space-y-5">
      <div className="space-y-4 rounded-2xl border border-line p-4 bg-surface-2/40">
        <Field label="Title" htmlFor="it"><Input id="it" value={f.title ?? ''} onChange={(e) => setF({ ...f, title: e.target.value })} placeholder="Degraded performance in Frankfurt" /></Field>
        <Field label="Details" htmlFor="ic" help="Plain text; links are clickable."><Textarea id="ic" value={f.content ?? ''} onChange={(e) => setF({ ...f, content: e.target.value })} placeholder="We are investigating…" /></Field>
        <div className="flex flex-wrap items-center gap-4">
          <Segmented size="sm" ariaLabel="Style" value={f.style ?? 'warning'} onChange={(v) => setF({ ...f, style: v })} options={styles.map((s) => ({ value: s, label: s[0].toUpperCase() + s.slice(1) }))} />
          <Switch checked={!!f.pinned} onChange={(v) => setF({ ...f, pinned: v })} label="Show on page" />
        </div>
        <div className="flex gap-2">
          <Button variant="primary" size="sm" loading={busy} disabled={!f.title} onClick={save} icon={<Megaphone className="size-3.5" />}>{f.id ? 'Update incident' : 'Post incident'}</Button>
          {f.id && <Button size="sm" variant="ghost" onClick={() => setF({ title: '', content: '', style: 'warning', pinned: true })}>Cancel edit</Button>}
        </div>
      </div>
      {list === null ? <Skeleton className="h-20" /> : list.length === 0 ? <p className="text-sm text-muted">No incidents posted.</p> : (
        <div className="space-y-2">
          {list.map((i) => (
            <div key={i.id} className="rounded-2xl border border-line p-4 flex items-start gap-3">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2"><Badge tone={tone(i.style)}>{i.style}</Badge>{!i.pinned && <Badge>Hidden</Badge>}<span className="font-medium">{i.title}</span></div>
                {i.content && <p className="mt-1.5 text-[13px] text-muted whitespace-pre-line line-clamp-3">{i.content}</p>}
                <p className="mt-1.5 text-[12px] text-subtle">{fmtDateTime(i.updatedAt * 1000)}</p>
              </div>
              <Button size="sm" variant="ghost" onClick={() => setF(i)}>Edit</Button>
              <IconButton size="sm" label="Delete incident" onClick={async () => {
                if (await confirm({ title: 'Delete this incident?', confirmLabel: 'Delete', danger: true })) {
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
  const [reload, setReload] = useState(0)
  const [entry, setEntry] = useState('')

  useEffect(() => {
    api.get<StatusPage>(`/api/status-pages/${id}`).then((r) => { setP(r); setSavedSlug(r.slug) }).catch((e) => setErr(e.message))
    if (role === 'admin') api.get<Record<string, string>>('/api/settings').then((s) => setEntry(s.entryPage)).catch(() => {})
  }, [id, role])

  const used = useMemo(() => new Set(p?.groups.flatMap((g) => g.monitors.map((m) => m.monitorId)) ?? []), [p])
  const sortedMonitors = useMemo(() => Object.values(monitors).sort((a, b) => a.name.localeCompare(b.name)), [monitors])

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
      setReload((x) => x + 1)
      toast.success('Status page saved')
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const del = async () => {
    if (!(await confirm({ title: `Delete “${p.title}”?`, body: 'The public page stops working immediately. Monitors are not affected.', confirmLabel: 'Delete page', danger: true }))) return
    await api.del(`/api/status-pages/${p.id}`)
    toast.success('Status page deleted')
    nav('/status-pages')
  }

  const setEntryPage = async (on: boolean) => {
    const v = on ? `status:${savedSlug}` : 'dashboard'
    try {
      await api.put('/api/settings', { entryPage: v })
      setEntry(v)
      toast.success(on ? 'This page now opens at the root address' : 'The root address opens the dashboard again')
    } catch (e) {
      toast.error('Could not change', (e as Error).message)
    }
  }

  const move = <T,>(arr: T[], i: number, d: number) => {
    const a = [...arr]
    const j = i + d
    if (j < 0 || j >= a.length) return a
    ;[a[i], a[j]] = [a[j], a[i]]
    return a
  }

  const addAllTunnels = () => {
    const tunnels = sortedMonitors.filter((m) => m.type === 'backpack-tunnel' && !used.has(m.id))
    if (!tunnels.length) return toast.info('Every tunnel is already on this page')
    setGroups([...p.groups, { name: 'Tunnels', monitors: tunnels.map((m) => ({ monitorId: m.id, showUrl: false })) }])
  }

  const previewSrc = `/status/${savedSlug}?preview=1&theme=${p.theme}&layout=${p.layout}&r=${reload}`

  return (
    <div className="p-4 sm:p-6 lg:p-8 max-w-[1700px]">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <Link to="/status-pages" className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-fg"><ArrowLeft className="size-4" />Status pages</Link>
          <h1 className="mt-2 font-display text-[26px] font-semibold tracking-tight truncate">{p.title}</h1>
        </div>
        <div className="flex items-center gap-2">
          <a href={`/status/${savedSlug}`} target="_blank" rel="noreferrer"><Button icon={<ExternalLink className="size-4" />}>Open</Button></a>
          <Button variant="primary" loading={busy} disabled={!dirty} onClick={save} icon={<Save className="size-4" />}>{dirty ? 'Save changes' : 'Saved'}</Button>
        </div>
      </div>
      {err && <Alert tone="danger" className="mt-4">{err}</Alert>}

      <div className="mt-6 grid 2xl:grid-cols-[560px_minmax(0,1fr)] xl:grid-cols-[500px_minmax(0,1fr)] gap-6 items-start">
        <Card className="overflow-hidden">
          <div className="px-4 pt-4"><Segmented ariaLabel="Editor section" value={tab} onChange={setTab} className="w-full [&>button]:flex-1" options={[{ value: 'general', label: 'General' }, { value: 'appearance', label: 'Look' }, { value: 'monitors', label: 'Monitors' }, { value: 'incidents', label: 'Incidents' }]} /></div>
          <div className="p-5 space-y-5">
            {tab === 'general' && (
              <>
                <Field label="Title" htmlFor="t"><Input id="t" value={p.title} onChange={(e) => up({ title: e.target.value })} /></Field>
                <Field label="Address" htmlFor="s" help={`${window.location.origin}/status/${p.slug}`}><Input id="s" className="font-mono" value={p.slug} onChange={(e) => up({ slug: slugify(e.target.value) })} /></Field>
                <Field label="Description" htmlFor="d"><Textarea id="d" value={p.description} onChange={(e) => up({ description: e.target.value })} placeholder="Live status of our services." /></Field>
                <Field label="Logo URL" htmlFor="l" help="Square image, shown beside the title."><Input id="l" value={p.logo} onChange={(e) => up({ logo: e.target.value })} placeholder="https://example.com/logo.png" /></Field>
                <Field label="Footer text" htmlFor="f"><Textarea id="f" className="min-h-16" value={p.footer} onChange={(e) => up({ footer: e.target.value })} placeholder="Questions? support@example.com" /></Field>
                <Switch checked={p.published} onChange={(v) => up({ published: v })} label="Published" description="Off keeps it visible only to signed-in users." />
                {role === 'admin' && (
                  <Switch checked={entry === `status:${savedSlug}`} onChange={setEntryPage} label="Open at the root address" description="Visitors to / see this page instead of the sign-in screen." />
                )}
                <div className="pt-3 border-t border-line">
                  <Button variant="danger" size="sm" icon={<Trash2 className="size-3.5" />} onClick={del}>Delete status page</Button>
                </div>
              </>
            )}

            {tab === 'appearance' && (
              <>
                <Field label="Theme">
                  <div className="grid grid-cols-2 gap-3" role="radiogroup" aria-label="Theme">
                    {(Object.keys(themeMeta) as Theme[]).map((t) => (
                      <button key={t} role="radio" aria-checked={p.theme === t} onClick={() => up({ theme: t })} className={clsx('text-left rounded-2xl border-2 overflow-hidden transition-colors', p.theme === t ? 'border-accent' : 'border-line hover:border-line-strong')}>
                        <ThemeSwatch theme={t} className="h-20" />
                        <div className="px-3 py-2"><p className="text-sm font-semibold">{themeMeta[t].name}</p><p className="text-[11.5px] text-muted leading-snug">{themeMeta[t].desc}</p></div>
                      </button>
                    ))}
                  </div>
                </Field>
                <div className="grid grid-cols-2 gap-4">
                  <Field label="Layout"><Segmented ariaLabel="Layout" value={p.layout} onChange={(v) => up({ layout: v })} options={[{ value: 'list', label: 'List' }, { value: 'cards', label: 'Cards' }]} /></Field>
                  <Field label="Bars show"><Segmented ariaLabel="Bars" value={p.barMode} onChange={(v) => up({ barMode: v })} options={[{ value: 'heartbeat', label: 'Checks' }, { value: 'days', label: '90 days' }]} /></Field>
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <Field label="Accent colour" help="Empty uses the theme's.">
                    <div className="flex gap-2">
                      <input type="color" aria-label="Accent colour" value={p.accentColor || '#22c55e'} onChange={(e) => up({ accentColor: e.target.value })} className="h-10 w-12 rounded-xl border border-line-strong bg-surface-2 p-1 cursor-pointer" />
                      <Input value={p.accentColor} onChange={(e) => up({ accentColor: e.target.value })} placeholder="theme" className="font-mono" />
                    </div>
                  </Field>
                  <Field label="Refresh every" htmlFor="ri">
                    <Select id="ri" value={p.refreshInterval} onChange={(e) => up({ refreshInterval: Number(e.target.value) })}>
                      {[30, 60, 120, 300].map((s) => <option key={s} value={s}>{s < 60 ? `${s} seconds` : `${s / 60} min`}</option>)}
                    </Select>
                  </Field>
                </div>
                <div className="space-y-3.5">
                  <Switch checked={p.showCharts} onChange={(v) => up({ showCharts: v })} label="Response time charts" description="Visitors can expand a day / week / month chart per service." />
                  <Switch checked={p.showTags} onChange={(v) => up({ showTags: v })} label="Show tags" />
                  <Switch checked={p.showCertExpiry} onChange={(v) => up({ showCertExpiry: v })} label="Show certificate expiry" />
                  <Switch checked={p.showPoweredBy} onChange={(v) => up({ showPoweredBy: v })} label="“Powered by Neo Uptime”" />
                </div>
                <Field label="Custom CSS" htmlFor="css" help="Applied last; target .sp and Tailwind classes."><Textarea id="css" className="font-mono text-[12.5px] min-h-28" value={p.customCss} onChange={(e) => up({ customCss: e.target.value })} placeholder={'.sp h1 { letter-spacing: -0.04em; }'} /></Field>
              </>
            )}

            {tab === 'monitors' && (
              <>
                {p.groups.map((g, gi) => (
                  <div key={gi} className="rounded-2xl border border-line bg-surface-2/40">
                    <div className="flex items-center gap-2 p-3 border-b border-line">
                      <Input aria-label="Group name" value={g.name} onChange={(e) => setGroups(p.groups.map((x, i) => (i === gi ? { ...x, name: e.target.value } : x)))} className="h-9 font-medium" />
                      <IconButton size="sm" label="Move group up" disabled={gi === 0} onClick={() => setGroups(move(p.groups, gi, -1))}><ArrowUp className="size-4" /></IconButton>
                      <IconButton size="sm" label="Move group down" disabled={gi === p.groups.length - 1} onClick={() => setGroups(move(p.groups, gi, 1))}><ArrowDown className="size-4" /></IconButton>
                      <IconButton size="sm" label="Remove group" onClick={() => setGroups(p.groups.filter((_, i) => i !== gi))}><Trash2 className="size-4" /></IconButton>
                    </div>
                    <div className="p-2 space-y-1">
                      {g.monitors.length === 0 && <p className="px-2 py-3 text-[13px] text-muted">Empty group — add monitors below.</p>}
                      {g.monitors.map((gm, mi) => {
                        const m = monitors[gm.monitorId]
                        return (
                          <div key={gm.monitorId} className="flex items-center gap-2 rounded-xl px-2 py-1.5 hover:bg-surface-2">
                            {m && <TypeIcon type={m.type} className="size-3.5 text-subtle" />}
                            <span className="flex-1 truncate text-sm">{m?.name ?? `#${gm.monitorId} (deleted)`}</span>
                            <IconButton size="sm" label={gm.showUrl ? 'Hide address' : 'Show address'} onClick={() => setGroups(p.groups.map((x, i) => (i === gi ? { ...x, monitors: x.monitors.map((y, j) => (j === mi ? { ...y, showUrl: !y.showUrl } : y)) } : x)))} className={gm.showUrl ? 'text-accent-ink' : ''}>
                              {gm.showUrl ? <Eye className="size-4" /> : <EyeOff className="size-4" />}
                            </IconButton>
                            <IconButton size="sm" label="Move up" disabled={mi === 0} onClick={() => setGroups(p.groups.map((x, i) => (i === gi ? { ...x, monitors: move(x.monitors, mi, -1) } : x)))}><ArrowUp className="size-4" /></IconButton>
                            <IconButton size="sm" label="Move down" disabled={mi === g.monitors.length - 1} onClick={() => setGroups(p.groups.map((x, i) => (i === gi ? { ...x, monitors: move(x.monitors, mi, 1) } : x)))}><ArrowDown className="size-4" /></IconButton>
                            <IconButton size="sm" label="Remove from page" onClick={() => setGroups(p.groups.map((x, i) => (i === gi ? { ...x, monitors: x.monitors.filter((_, j) => j !== mi) } : x)))}><X className="size-4" /></IconButton>
                          </div>
                        )
                      })}
                      <Select aria-label="Add a monitor to this group" value="" onChange={(e) => {
                        const mid = Number(e.target.value)
                        if (mid) setGroups(p.groups.map((x, i) => (i === gi ? { ...x, monitors: [...x.monitors, { monitorId: mid, showUrl: false }] } : x)))
                      }}>
                        <option value="">+ Add a monitor…</option>
                        {sortedMonitors.filter((m) => !used.has(m.id)).map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}
                      </Select>
                    </div>
                  </div>
                ))}
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => setGroups([...p.groups, { name: 'New group', monitors: [] }])}>Add group</Button>
                  <Button size="sm" variant="soft" icon={<Cable className="size-3.5" />} onClick={addAllTunnels}>Add all tunnels</Button>
                </div>
              </>
            )}

            {tab === 'incidents' && <IncidentsTab pageId={p.id} />}
          </div>
        </Card>

        <div className="xl:sticky xl:top-6 space-y-2">
          <div className="flex items-center justify-between text-[13px] text-muted px-1">
            <span>Preview{dirty && <span className="text-pending"> · theme and layout are live, other changes show after saving</span>}</span>
          </div>
          <div className="rounded-[22px] border border-line-strong overflow-hidden bg-surface shadow-[var(--shadow-pop)]">
            <div className="flex items-center gap-1.5 px-4 h-9 border-b border-line bg-surface-2">
              <span className="size-2.5 rounded-full bg-down/70" /><span className="size-2.5 rounded-full bg-pending/70" /><span className="size-2.5 rounded-full bg-up/70" />
              <span className="ml-3 text-[12px] text-subtle font-mono truncate">{window.location.host}/status/{savedSlug}</span>
            </div>
            <iframe key={previewSrc} title="Status page preview" src={previewSrc} className="w-full h-[78vh] bg-white" />
          </div>
        </div>
      </div>
    </div>
  )
}
