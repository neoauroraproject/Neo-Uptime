import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Cable, CircleCheck, Download, Ellipsis, KeyRound, Pencil, Plug, Plus, RefreshCw, Search, Server, Trash2, TriangleAlert } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { bytes, clsx, ms, timeAgo } from '../lib/format'
import type { BackpackPanel, PanelSnapshot } from '../lib/types'
import { Alert, Badge, Button, Card, Checkbox, Empty, Field, IconButton, Input, Menu, Modal, Segmented, Select, Skeleton, Switch, confirm, toast, useNow } from '../components/ui'
import { TunnelBadges, TunnelCard } from '../components/tunnel'

/* ---------- panel dialog ---------- */

function PanelDialog({ open, onClose, panel }: { open: boolean; onClose: () => void; panel?: BackpackPanel }) {
  const [f, setF] = useState({ name: '', url: '', token: '', ignoreTls: true, interval: 30, autoImport: true })
  const [test, setTest] = useState<PanelSnapshot | null>(null)
  const [testErr, setTestErr] = useState('')
  const [busy, setBusy] = useState<'' | 'test' | 'save'>('')
  const [err, setErr] = useState('')

  useEffect(() => {
    if (!open) return
    setF(panel ? { name: panel.name, url: panel.url, token: '', ignoreTls: panel.ignoreTls, interval: panel.interval, autoImport: panel.autoImport } : { name: '', url: '', token: '', ignoreTls: true, interval: 30, autoImport: true })
    setTest(null); setTestErr(''); setErr('')
  }, [open, panel])

  const runTest = async () => {
    setBusy('test'); setTestErr(''); setTest(null)
    try {
      setTest(await api.post<PanelSnapshot>('/api/backpack/test', { id: panel?.id ?? 0, url: f.url, token: f.token, ignoreTls: f.ignoreTls }))
    } catch (e) {
      setTestErr((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  const save = async () => {
    setBusy('save'); setErr('')
    try {
      if (panel) await api.put(`/api/backpack/panels/${panel.id}`, f)
      else await api.post('/api/backpack/panels', f)
      toast.success(panel ? 'Panel saved' : 'Panel connected', f.autoImport ? 'Its tunnels are being imported as monitors.' : undefined)
      await useApp.getState().loadPanels()
      setTimeout(() => useApp.getState().loadDashboard().catch(() => {}), 1500)
      onClose()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      size="lg"
      title={panel ? `Edit ${panel.name}` : 'Connect a BackPack panel'}
      description="Neo Uptime reads the panel's read-only API. It never changes a tunnel."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button onClick={runTest} loading={busy === 'test'} disabled={!f.url} icon={<Plug className="size-4" />}>Test connection</Button>
          <Button variant="primary" onClick={save} loading={busy === 'save'} disabled={!f.name || !f.url || (!panel && !f.token)}>{panel ? 'Save' : 'Connect panel'}</Button>
        </>
      }
    >
      <div className="space-y-5">
        <Alert tone="info" title="Create a read token in BackPack">
          In the BackPack web panel open <b className="text-fg">Settings → Security → API tokens</b>, create a token with scope <b className="text-fg">read</b> (name it “neo-uptime”), and paste it below. Use the same address you open the panel with — including the port and any secret path.
        </Alert>
        {err && <Alert tone="danger">{err}</Alert>}
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label="Name" required htmlFor="pn"><Input id="pn" value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="Iran edge" /></Field>
          <Field label="Panel URL" required htmlFor="pu"><Input id="pu" className="font-mono" value={f.url} onChange={(e) => setF({ ...f, url: e.target.value })} placeholder="https://1.2.3.4:7777" /></Field>
        </div>
        <Field label="API token" required={!panel} htmlFor="pt" help={panel?.hasToken ? 'Leave empty to keep the current token.' : 'Stored on this server only; never shown again.'}>
          <div className="relative">
            <KeyRound className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-subtle" />
            <Input id="pt" type="password" autoComplete="off" className="pl-9 font-mono" value={f.token} onChange={(e) => setF({ ...f, token: e.target.value })} placeholder={panel?.hasToken ? '••••••••••••' : 'paste the token'} />
          </div>
        </Field>
        <div className="grid sm:grid-cols-2 gap-4 items-start">
          <Field label="Check tunnels every" htmlFor="pi" help="One request per interval, shared by all tunnels.">
            <Select id="pi" value={f.interval} onChange={(e) => setF({ ...f, interval: Number(e.target.value) })}>
              {[20, 30, 45, 60, 120, 300].map((s) => <option key={s} value={s}>{s < 60 ? `${s} seconds` : `${s / 60} minute${s > 60 ? 's' : ''}`}</option>)}
            </Select>
          </Field>
          <div className="space-y-4 sm:pt-6">
            <Switch checked={f.ignoreTls} onChange={(v) => setF({ ...f, ignoreTls: v })} label="Accept self-signed certificate" description="BackPack panels usually use one." />
            <Switch checked={f.autoImport} onChange={(v) => setF({ ...f, autoImport: v })} label="Import new tunnels automatically" description="Tunnels you add in BackPack appear here on their own." />
          </div>
        </div>
        {testErr && <Alert tone="danger" title="Connection failed">{testErr}</Alert>}
        {test && (
          <div className="rounded-2xl border border-up/30 bg-up/6 p-4 fade-up">
            <p className="flex items-center gap-2 font-medium"><CircleCheck className="size-4.5 text-up" />Connected in {ms(test.latencyMs)} — {test.tunnels?.length ?? 0} tunnels found{test.system?.hostname ? ` on ${test.system.hostname}` : ''}</p>
            <div className="mt-3 flex flex-wrap gap-2">
              {(test.tunnels ?? []).map((t) => (
                <span key={t.name} className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-lg bg-surface border border-line text-[12.5px]">
                  <span className={clsx('size-1.5 rounded-full', t.state === 'online' ? 'bg-up' : 'bg-down')} />{t.name}<span className="text-subtle uppercase text-[11px]">{t.carrier || t.transport}</span>
                </span>
              ))}
            </div>
          </div>
        )}
      </div>
    </Modal>
  )
}

/* ---------- import dialog ---------- */

function ImportDialog({ panel, onClose }: { panel: BackpackPanel | null; onClose: () => void }) {
  const [data, setData] = useState<{ snapshot: PanelSnapshot; imported: Record<string, number> } | null>(null)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!panel) return
    setData(null)
    api.get<{ snapshot: PanelSnapshot; imported: Record<string, number> }>(`/api/backpack/panels/${panel.id}/snapshot?refresh=1`).then((r) => {
      setData(r)
      setPicked(new Set((r.snapshot.tunnels ?? []).filter((t) => !r.imported[t.name]).map((t) => t.name)))
    }).catch((e) => toast.error('Could not read the panel', e.message))
  }, [panel])
  const run = async () => {
    if (!panel) return
    setBusy(true)
    try {
      const r = await api.post<{ imported: number }>(`/api/backpack/panels/${panel.id}/import`, { names: [...picked] })
      toast.success(`${r.imported} tunnel${r.imported === 1 ? '' : 's'} imported`)
      await useApp.getState().loadDashboard()
      await useApp.getState().loadPanels()
      onClose()
    } catch (e) {
      toast.error('Import failed', (e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const tunnels = data?.snapshot.tunnels ?? []
  return (
    <Modal open={!!panel} onClose={onClose} size="lg" title={`Import tunnels from ${panel?.name ?? ''}`} description="Each tunnel becomes a monitor in the panel's group, with your default notifications."
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button variant="primary" loading={busy} disabled={!picked.size} onClick={run} icon={<Download className="size-4" />}>Import {picked.size || ''}</Button></>}>
      {!data ? <div className="space-y-2">{Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-12" />)}</div>
        : data.snapshot.error ? <Alert tone="danger">{data.snapshot.error}</Alert>
          : tunnels.length === 0 ? <p className="text-sm text-muted">This panel has no tunnels yet.</p>
            : (
              <div className="divide-y divide-line rounded-2xl border border-line">
                {tunnels.map((t) => {
                  const done = !!data.imported[t.name]
                  return (
                    <div key={t.name} className="flex items-center gap-3 px-4 py-3">
                      {done ? <CircleCheck className="size-[18px] text-up" aria-label="Already imported" /> : (
                        <Checkbox checked={picked.has(t.name)} onChange={(v) => { const n = new Set(picked); if (v) n.add(t.name); else n.delete(t.name); setPicked(n) }} label={<span className="sr-only">{t.name}</span>} />
                      )}
                      <div className="min-w-0 flex-1">
                        <p className="font-medium">{t.name}</p>
                        <TunnelBadges t={t} className="mt-1" />
                      </div>
                      <span className={clsx('text-[12.5px] font-medium', t.state === 'online' ? 'text-up' : 'text-down')}>{t.state}</span>
                      {done && <Link to={`/dashboard/${data.imported[t.name]}`} className="text-[12.5px] text-accent-ink hover:underline">View</Link>}
                    </div>
                  )
                })}
              </div>
            )}
    </Modal>
  )
}

/* ---------- panels tab ---------- */

function Panels() {
  const panels = useApp((s) => s.panels)
  const role = useApp((s) => s.role)
  const [edit, setEdit] = useState<BackpackPanel | undefined>()
  const [open, setOpen] = useState(false)
  const [importing, setImporting] = useState<BackpackPanel | null>(null)
  const now = useNow(10_000)

  useEffect(() => {
    const t = setInterval(() => useApp.getState().loadPanels().catch(() => {}), 15_000)
    return () => clearInterval(t)
  }, [])

  const del = async (p: BackpackPanel) => {
    const ok = await confirm({ title: `Disconnect ${p.name}?`, body: 'Its tunnel monitors and their history are deleted as well.', confirmLabel: 'Disconnect & delete', danger: true })
    if (!ok) return
    try {
      await api.del(`/api/backpack/panels/${p.id}?deleteMonitors=1`)
      toast.success('Panel disconnected')
      await useApp.getState().loadPanels()
      await useApp.getState().loadDashboard()
    } catch (e) {
      toast.error('Could not delete', (e as Error).message)
    }
  }

  return (
    <div className="space-y-4">
      {panels.length === 0 ? (
        <Card>
          <Empty icon={<Cable className="size-6" />} title="Connect your first BackPack panel" body="Give Neo Uptime the panel address and a read token; every tunnel shows up here with its latency, traffic and uptime history."
            action={canEdit(role) && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => { setEdit(undefined); setOpen(true) }}>Connect panel</Button>} />
        </Card>
      ) : (
        <div className="grid md:grid-cols-2 2xl:grid-cols-3 gap-4">
          {panels.map((p) => (
            <Card key={p.id} className="p-5 flex flex-col gap-4">
              <div className="flex items-start gap-3">
                <span className={clsx('grid place-items-center size-10 rounded-xl border', p.error ? 'border-down/30 bg-down/10 text-down' : 'border-up/30 bg-up/10 text-up')}><Server className="size-5" /></span>
                <div className="min-w-0 flex-1">
                  <p className="font-display text-lg font-semibold">{p.name}</p>
                  <p className="text-[12.5px] text-muted font-mono truncate">{p.url}</p>
                </div>
                {canEdit(role) && (
                  <Menu trigger={(t) => <IconButton {...t} size="sm" label="Panel actions"><Ellipsis className="size-4" /></IconButton>} items={[
                    { label: 'Import tunnels', icon: <Download />, onClick: () => setImporting(p) },
                    { label: 'Edit', icon: <Pencil />, onClick: () => { setEdit(p); setOpen(true) } },
                    { divider: true, label: '' },
                    { label: 'Disconnect', icon: <Trash2 />, danger: true, onClick: () => del(p) },
                  ]} />
                )}
              </div>
              {p.error ? (
                <p className="flex items-start gap-2 text-[13px] text-down"><TriangleAlert className="size-4 shrink-0 mt-0.5" />{p.error}</p>
              ) : (
                <div className="grid grid-cols-3 gap-2">
                  <div className="rounded-xl bg-surface-2 border border-line px-3 py-2"><p className="text-[11.5px] text-muted">Online</p><p className="font-display font-semibold tnum">{p.online}/{p.tunnels}</p></div>
                  <div className="rounded-xl bg-surface-2 border border-line px-3 py-2"><p className="text-[11.5px] text-muted">API</p><p className="font-display font-semibold tnum">{ms(p.latencyMs)}</p></div>
                  <div className="rounded-xl bg-surface-2 border border-line px-3 py-2"><p className="text-[11.5px] text-muted">Every</p><p className="font-display font-semibold tnum">{p.interval}s</p></div>
                </div>
              )}
              <div className="flex items-center justify-between text-[12.5px] text-muted">
                <span>{p.hostname ? `${p.hostname}${p.version ? ` · ${p.version}` : ''}` : 'Waiting for the first read'}</span>
                <span className="tnum">{p.fetchedAt ? `read ${timeAgo(p.fetchedAt, now)}` : ''}</span>
              </div>
              <div className="flex gap-2">
                {p.autoImport ? <Badge tone="accent">Auto-import on</Badge> : <Badge>Manual import</Badge>}
                {p.ignoreTls && <Badge>Self-signed OK</Badge>}
              </div>
            </Card>
          ))}
        </div>
      )}
      {panels.length > 0 && canEdit(role) && (
        <Button icon={<Plus className="size-4" />} onClick={() => { setEdit(undefined); setOpen(true) }}>Connect another panel</Button>
      )}
      <PanelDialog open={open} onClose={() => setOpen(false)} panel={edit} />
      <ImportDialog panel={importing} onClose={() => setImporting(null)} />
    </div>
  )
}

/* ---------- page ---------- */

export default function Tunnels({ tab = 'tunnels' }: { tab?: 'tunnels' | 'panels' }) {
  const nav = useNavigate()
  const monitors = useApp((s) => s.monitors)
  const panels = useApp((s) => s.panels)
  const loaded = useApp((s) => s.loaded)
  const [q, setQ] = useState('')
  const [panel, setPanel] = useState<number | 0>(0)
  const [state, setState] = useState<'all' | 'online' | 'offline'>('all')
  const [sort, setSort] = useState<'status' | 'name' | 'latency' | 'traffic'>('status')

  const tunnels = useMemo(() => Object.values(monitors).filter((m) => m.type === 'backpack-tunnel'), [monitors])
  const list = useMemo(() => {
    const s = q.trim().toLowerCase()
    return tunnels
      .filter((m) => !s || m.name.toLowerCase().includes(s) || (m.tunnel?.peerLocation ?? '').toLowerCase().includes(s) || (m.tunnel?.carrier ?? '').toLowerCase().includes(s))
      .filter((m) => !panel || m.config.panelId === panel)
      .filter((m) => state === 'all' || (state === 'online' ? m.last?.status === 1 : m.last?.status !== 1))
      .sort((a, b) => {
        switch (sort) {
          case 'name': return a.name.localeCompare(b.name)
          case 'latency': return (a.last?.ping ?? 1e9) - (b.last?.ping ?? 1e9)
          case 'traffic': return (b.tunnel?.totalBytes ?? 0) - (a.tunnel?.totalBytes ?? 0)
          default: return (a.last?.status === 0 ? 0 : 1) - (b.last?.status === 0 ? 0 : 1) || a.name.localeCompare(b.name)
        }
      })
  }, [tunnels, q, panel, state, sort])

  const online = tunnels.filter((m) => m.last?.status === 1).length
  const traffic = tunnels.reduce((a, m) => a + (m.tunnel?.totalBytes ?? 0), 0)
  const pings = tunnels.map((m) => (m.last?.status === 1 ? m.last.ping : null)).filter((v): v is number => v != null)
  const avg = pings.length ? pings.reduce((a, b) => a + b, 0) / pings.length : -1
  const panelName = (id?: number) => panels.find((p) => p.id === id)?.name

  return (
    <div className="p-4 sm:p-6 lg:p-8 space-y-6 max-w-[1600px]">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="font-display text-[28px] font-semibold tracking-tight">Tunnels</h1>
          <p className="text-sm text-muted mt-1">BackPack tunnels across {panels.length} panel{panels.length === 1 ? '' : 's'}, checked continuously.</p>
        </div>
        <Segmented ariaLabel="Section" value={tab} onChange={(v) => nav(v === 'panels' ? '/tunnels/panels' : '/tunnels')} options={[{ value: 'tunnels', label: 'Tunnels' }, { value: 'panels', label: `Panels (${panels.length})` }]} />
      </div>

      {tab === 'panels' ? <Panels /> : (
        <>
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
            {[
              { l: 'Online', v: `${online}/${tunnels.length}`, tone: online < tunnels.length ? 'text-down' : 'text-up' },
              { l: 'Total traffic', v: bytes(traffic) },
              { l: 'Average latency', v: ms(avg) },
              { l: 'Panels', v: String(panels.length) },
            ].map((k) => (
              <div key={k.l} className="card px-5 py-4">
                <p className="text-[12.5px] text-muted">{k.l}</p>
                <p className={clsx('mt-1 font-display text-[26px] font-semibold tnum', k.tone)}>{k.v}</p>
              </div>
            ))}
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <div className="relative flex-1 min-w-[200px] max-w-sm">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-subtle" />
              <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search tunnels, locations, transports" className="pl-9" aria-label="Search tunnels" />
            </div>
            {panels.length > 1 && (
              <Select aria-label="Panel" value={panel} onChange={(e) => setPanel(Number(e.target.value))} className="w-44">
                <option value={0}>All panels</option>
                {panels.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </Select>
            )}
            <Segmented ariaLabel="State" value={state} onChange={setState} options={[{ value: 'all', label: 'All' }, { value: 'online', label: 'Online' }, { value: 'offline', label: 'Offline' }]} />
            <Select aria-label="Sort" value={sort} onChange={(e) => setSort(e.target.value as typeof sort)} className="w-40 ml-auto">
              <option value="status">Problems first</option>
              <option value="name">Name</option>
              <option value="latency">Lowest latency</option>
              <option value="traffic">Most traffic</option>
            </Select>
            <IconButton label="Refresh" onClick={() => useApp.getState().loadDashboard()}><RefreshCw className="size-4" /></IconButton>
          </div>

          {!loaded ? (
            <div className="grid md:grid-cols-2 2xl:grid-cols-3 gap-4">{Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-80 rounded-[18px]" />)}</div>
          ) : tunnels.length === 0 ? (
            <Card>
              <Empty icon={<Cable className="size-6" />} title="No tunnels yet" body="Connect a BackPack panel and its tunnels are imported as monitors automatically."
                action={<Button variant="primary" icon={<Plus className="size-4" />} onClick={() => nav('/tunnels/panels')}>Connect a panel</Button>} />
            </Card>
          ) : list.length === 0 ? (
            <p className="py-12 text-center text-sm text-muted">No tunnel matches these filters.</p>
          ) : (
            <div className="grid md:grid-cols-2 2xl:grid-cols-3 gap-4">
              {list.map((m) => <TunnelCard key={m.id} m={m} panelName={panelName(m.config.panelId)} />)}
            </div>
          )}
        </>
      )}
    </div>
  )
}
