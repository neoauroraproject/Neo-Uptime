import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Cable, CircleCheck, Download, Ellipsis, KeyRound, Pencil, Plug, Plus, RefreshCw, Search, Server, Trash2, TriangleAlert, Turtle } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { bytes, clsx, ms, n, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import type { BackpackPanel, PanelSnapshot } from '../lib/types'
import { Alert, Badge, Button, Card, Checkbox, Empty, Field, IconButton, Input, Menu, Modal, Segmented, Select, Skeleton, Switch, confirm, toast, useNow } from '../components/ui'
import { TunnelBadges, TunnelCard } from '../components/tunnel'
import { TunnelSlowAlertsDialog } from '../components/SlowAlert'
import { AdCard } from '../components/AdCard'

/* ---------- panel dialog ---------- */

function PanelDialog({ open, onClose, panel }: { open: boolean; onClose: () => void; panel?: BackpackPanel }) {
  const t = useT()
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
      toast.success(panel ? t('Panel saved') : t('Panel connected'), f.autoImport ? t('Its tunnels are being imported as monitors.') : undefined)
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
      title={panel ? t('Edit {name}', { name: panel.name }) : t('Connect a BackPack panel')}
      description={t('Neo Uptime reads the panel’s read-only API. It never changes a tunnel.')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>{t('Cancel')}</Button>
          <Button onClick={runTest} loading={busy === 'test'} disabled={!f.url} icon={<Plug className="size-4" />}>{t('Test connection')}</Button>
          <Button variant="primary" onClick={save} loading={busy === 'save'} disabled={!f.name || !f.url || (!panel && !f.token)}>{panel ? t('Save') : t('Connect panel')}</Button>
        </>
      }
    >
      <div className="space-y-5">
        <Alert tone="info" title={t('Create a read token in BackPack')}>
          {t('In the BackPack web panel open Settings → Security → API tokens, create a token with scope “read” (name it neo-uptime) and paste it below. Use the address you open the panel with — including the port and any secret path.')}
        </Alert>
        <Alert tone="success" title={t('No panel, or BackPack runs on another server?')}>
          {t('Install the agent on that server instead (Servers → Add server): it reads BackPack and Kariz directly, with no panel token.')}
        </Alert>
        {err && <Alert tone="danger">{err}</Alert>}
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label={t('Name')} required htmlFor="pn"><Input id="pn" dir="auto" value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder={t('Iran edge')} /></Field>
          <Field label={t('Panel URL')} required htmlFor="pu"><Input id="pu" dir="ltr" className="font-mono" value={f.url} onChange={(e) => setF({ ...f, url: e.target.value })} placeholder="https://1.2.3.4:7777" /></Field>
        </div>
        <Field label={t('API token')} required={!panel} htmlFor="pt" help={panel?.hasToken ? t('Leave empty to keep the current token.') : t('Stored on this server only; never shown again.')}>
          <div className="relative">
            <KeyRound className="absolute start-3 top-1/2 -translate-y-1/2 size-4 text-subtle" />
            <Input id="pt" dir="ltr" type="password" autoComplete="off" className="ps-9 font-mono" value={f.token} onChange={(e) => setF({ ...f, token: e.target.value })} placeholder={panel?.hasToken ? '••••••••••••' : t('paste the token')} />
          </div>
        </Field>
        <div className="grid sm:grid-cols-2 gap-4 items-start">
          <Field label={t('Check tunnels every')} htmlFor="pi" help={t('One request per interval, shared by all tunnels.')}>
            <Select id="pi" value={f.interval} onChange={(e) => setF({ ...f, interval: Number(e.target.value) })}>
              {[20, 30, 45, 60, 120, 300].map((s) => <option key={s} value={s}>{s < 60 ? t('{n} seconds', { n: s }) : t('{n} minutes', { n: s / 60 })}</option>)}
            </Select>
          </Field>
          <div className="space-y-4 sm:pt-6">
            <Switch checked={f.ignoreTls} onChange={(v) => setF({ ...f, ignoreTls: v })} label={t('Accept self-signed certificate')} description={t('BackPack panels usually use one.')} />
            <Switch checked={f.autoImport} onChange={(v) => setF({ ...f, autoImport: v })} label={t('Import new tunnels automatically')} description={t('Tunnels you add in BackPack appear here on their own.')} />
          </div>
        </div>
        {testErr && <Alert tone="danger" title={t('Connection failed')}>{testErr}</Alert>}
        {test && (
          <div className="rounded-2xl border border-up/30 bg-up/6 p-4 fade-up">
            <p className="flex items-center gap-2 font-medium"><CircleCheck className="size-4.5 text-up" />{t('Connected in {ms} — {n} tunnels found', { ms: ms(test.latencyMs), n: test.tunnels?.length ?? 0 })}{test.system?.hostname ? ` · ${test.system.hostname}` : ''}</p>
            <div className="mt-3 flex flex-wrap gap-2">
              {(test.tunnels ?? []).map((tn) => (
                <span key={tn.name} className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-lg bg-surface border border-line text-[12.5px]">
                  <span className={clsx('size-1.5 rounded-full', tn.state === 'online' ? 'bg-up' : 'bg-down')} />{tn.name}<span className="text-subtle uppercase text-[11px]">{tn.carrier || tn.transport}</span>
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
  const t = useT()
  const [data, setData] = useState<{ snapshot: PanelSnapshot; imported: Record<string, number> } | null>(null)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!panel) return
    setData(null)
    api.get<{ snapshot: PanelSnapshot; imported: Record<string, number> }>(`/api/backpack/panels/${panel.id}/snapshot?refresh=1`).then((r) => {
      setData(r)
      setPicked(new Set((r.snapshot.tunnels ?? []).filter((tn) => !r.imported[tn.name]).map((tn) => tn.name)))
    }).catch((e) => toast.error(t('Could not read the panel'), e.message))
  }, [panel, t])
  const run = async () => {
    if (!panel) return
    setBusy(true)
    try {
      const r = await api.post<{ imported: number }>(`/api/backpack/panels/${panel.id}/import`, { names: [...picked] })
      toast.success(t('{n} tunnels imported', { n: r.imported }))
      await useApp.getState().loadDashboard()
      await useApp.getState().loadPanels()
      onClose()
    } catch (e) {
      toast.error(t('Import failed'), (e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const tunnels = data?.snapshot.tunnels ?? []
  return (
    <Modal open={!!panel} onClose={onClose} size="lg" title={t('Import tunnels from {name}', { name: panel?.name ?? '' })} description={t('Each tunnel becomes a monitor in the panel’s group, with your default notifications.')}
      footer={<><Button variant="ghost" onClick={onClose}>{t('Cancel')}</Button><Button variant="primary" loading={busy} disabled={!picked.size} onClick={run} icon={<Download className="size-4" />}>{t('Import')} {picked.size ? n(picked.size) : ''}</Button></>}>
      {!data ? <div className="space-y-2">{Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-12" />)}</div>
        : data.snapshot.error ? <Alert tone="danger">{data.snapshot.error}</Alert>
          : tunnels.length === 0 ? <p className="text-sm text-muted">{t('This panel has no tunnels yet.')}</p>
            : (
              <div className="divide-y divide-line rounded-2xl border border-line">
                {tunnels.map((tn) => {
                  const done = !!data.imported[tn.name]
                  return (
                    <div key={tn.name} className="flex items-center gap-3 px-4 py-3">
                      {done ? <CircleCheck className="size-[18px] text-up" aria-label={t('Already imported')} /> : (
                        <Checkbox checked={picked.has(tn.name)} onChange={(v) => { const nx = new Set(picked); if (v) nx.add(tn.name); else nx.delete(tn.name); setPicked(nx) }} label={<span className="sr-only">{tn.name}</span>} />
                      )}
                      <div className="min-w-0 flex-1">
                        <p className="font-medium">{tn.name}</p>
                        <TunnelBadges t={tn} className="mt-1" />
                      </div>
                      <span className={clsx('text-[12.5px] font-medium', tn.state === 'online' ? 'text-up' : 'text-down')}>{t(tn.state === 'online' ? 'Online' : 'Offline')}</span>
                      {done && <Link to={`/dashboard/${data.imported[tn.name]}`} className="text-[12.5px] text-accent-ink hover:underline">{t('View')}</Link>}
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
  const t = useT()
  const panels = useApp((s) => s.panels)
  const role = useApp((s) => s.role)
  const [edit, setEdit] = useState<BackpackPanel | undefined>()
  const [open, setOpen] = useState(false)
  const [importing, setImporting] = useState<BackpackPanel | null>(null)
  const now = useNow(10_000)

  useEffect(() => {
    const tm = setInterval(() => useApp.getState().loadPanels().catch(() => {}), 15_000)
    return () => clearInterval(tm)
  }, [])

  const del = async (p: BackpackPanel) => {
    const ok = await confirm({ title: t('Disconnect {name}?', { name: p.name }), body: t('Its tunnel monitors and their history are deleted as well.'), confirmLabel: t('Disconnect & delete'), danger: true })
    if (!ok) return
    try {
      await api.del(`/api/backpack/panels/${p.id}?deleteMonitors=1`)
      toast.success(t('Panel disconnected'))
      await useApp.getState().loadPanels()
      await useApp.getState().loadDashboard()
    } catch (e) {
      toast.error(t('Could not delete'), (e as Error).message)
    }
  }

  return (
    <div className="space-y-4">
      <Alert tone="info">
        {t('Panels are read over BackPack’s API. To watch BackPack or Kariz on another server without a panel token, add the server under')} <Link to="/servers" className="text-accent-ink underline">{t('Servers')}</Link>.
      </Alert>
      {panels.length === 0 ? (
        <Card>
          <Empty icon={<Cable className="size-6" />} title={t('Connect your first BackPack panel')} body={t('Give Neo Uptime the panel address and a read token; every tunnel shows up here with its latency, traffic and uptime history.')}
            action={canEdit(role) && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => { setEdit(undefined); setOpen(true) }}>{t('Connect panel')}</Button>} />
        </Card>
      ) : (
        <div className="grid md:grid-cols-2 2xl:grid-cols-3 gap-4">
          {panels.map((p) => (
            <Card key={p.id} className="p-5 flex flex-col gap-4">
              <div className="flex items-start gap-3">
                <span className={clsx('grid place-items-center size-10 rounded-xl border', p.error ? 'border-down/30 bg-down/10 text-down' : 'border-up/30 bg-up/10 text-up')}><Server className="size-5" /></span>
                <div className="min-w-0 flex-1">
                  <p className="font-display text-lg font-semibold" dir="auto">{p.name}</p>
                  <p className="text-[12.5px] text-muted font-mono truncate" dir="ltr">{p.url}</p>
                </div>
                {canEdit(role) && (
                  <Menu trigger={(tr) => <IconButton {...tr} size="sm" label={t('Panel actions')}><Ellipsis className="size-4" /></IconButton>} items={[
                    { label: t('Import tunnels'), icon: <Download />, onClick: () => setImporting(p) },
                    { label: t('Edit'), icon: <Pencil />, onClick: () => { setEdit(p); setOpen(true) } },
                    { divider: true, label: '' },
                    { label: t('Disconnect'), icon: <Trash2 />, danger: true, onClick: () => del(p) },
                  ]} />
                )}
              </div>
              {p.error ? (
                <p className="flex items-start gap-2 text-[13px] text-down"><TriangleAlert className="size-4 shrink-0 mt-0.5" />{p.error}</p>
              ) : (
                <div className="grid grid-cols-3 gap-2">
                  <div className="rounded-xl bg-surface-2 border border-line px-3 py-2"><p className="text-[11.5px] text-muted">{t('Online')}</p><p className="font-display font-semibold tnum" dir="ltr">{n(p.online)}/{n(p.tunnels)}</p></div>
                  <div className="rounded-xl bg-surface-2 border border-line px-3 py-2"><p className="text-[11.5px] text-muted">API</p><p className="font-display font-semibold tnum">{ms(p.latencyMs)}</p></div>
                  <div className="rounded-xl bg-surface-2 border border-line px-3 py-2"><p className="text-[11.5px] text-muted">{t('Every')}</p><p className="font-display font-semibold tnum">{t('{n}s', { n: p.interval })}</p></div>
                </div>
              )}
              <div className="flex items-center justify-between text-[12.5px] text-muted">
                <span>{p.hostname ? `${p.hostname}${p.version ? ` · ${p.version}` : ''}` : t('Waiting for the first read')}</span>
                <span className="tnum">{p.fetchedAt ? t('read {ago}', { ago: timeAgo(p.fetchedAt, now) }) : ''}</span>
              </div>
              <div className="flex gap-2">
                {p.autoImport ? <Badge tone="accent">{t('Auto-import on')}</Badge> : <Badge>{t('Manual import')}</Badge>}
                {p.ignoreTls && <Badge>{t('Self-signed OK')}</Badge>}
              </div>
            </Card>
          ))}
        </div>
      )}
      {panels.length > 0 && canEdit(role) && (
        <Button icon={<Plus className="size-4" />} onClick={() => { setEdit(undefined); setOpen(true) }}>{t('Connect another panel')}</Button>
      )}
      <PanelDialog open={open} onClose={() => setOpen(false)} panel={edit} />
      <ImportDialog panel={importing} onClose={() => setImporting(null)} />
    </div>
  )
}

/* ---------- page ---------- */

export default function Tunnels({ tab = 'tunnels' }: { tab?: 'tunnels' | 'panels' }) {
  const t = useT()
  const nav = useNavigate()
  const monitors = useApp((s) => s.monitors)
  const panels = useApp((s) => s.panels)
  const agents = useApp((s) => s.agents)
  const loaded = useApp((s) => s.loaded)
  const [q, setQ] = useState('')
  const [via, setVia] = useState('')
  const [source, setSource] = useState<'all' | 'backpack' | 'kariz'>('all')
  const [state, setState] = useState<'all' | 'online' | 'offline'>('all')
  const [sort, setSort] = useState<'status' | 'name' | 'latency' | 'traffic'>('status')
  const [slowOpen, setSlowOpen] = useState(false)
  const role = useApp((s) => s.role)

  const tunnels = useMemo(() => Object.values(monitors).filter((m) => m.type === 'backpack-tunnel' || m.type === 'agent-tunnel'), [monitors])
  const viaOf = (m: (typeof tunnels)[number]) => (m.type === 'backpack-tunnel' ? `p${m.config.panelId}` : `a${m.config.agentId}`)
  const viaName = (key: string) => (key.startsWith('p') ? panels.find((p) => `p${p.id}` === key)?.name : agents.find((a) => `a${a.id}` === key)?.name) ?? ''
  const sourceOf = (m: (typeof tunnels)[number]) => (m.type === 'backpack-tunnel' ? 'backpack' : m.config.source ?? 'backpack')
  const vias = [...new Set(tunnels.map(viaOf))]

  const list = useMemo(() => {
    const s = q.trim().toLowerCase()
    return tunnels
      .filter((m) => !s || m.name.toLowerCase().includes(s) || (m.tunnel?.peerLocation ?? '').toLowerCase().includes(s) || (m.tunnel?.carrier ?? '').toLowerCase().includes(s))
      .filter((m) => !via || viaOf(m) === via)
      .filter((m) => source === 'all' || sourceOf(m) === source)
      .filter((m) => state === 'all' || (state === 'online' ? m.last?.status === 1 : m.last?.status !== 1))
      .sort((a, b) => {
        switch (sort) {
          case 'name': return a.name.localeCompare(b.name)
          case 'latency': return (a.last?.ping ?? 1e9) - (b.last?.ping ?? 1e9)
          case 'traffic': return (b.tunnel?.totalBytes ?? 0) - (a.tunnel?.totalBytes ?? 0)
          default: return (a.last?.status === 0 ? 0 : 1) - (b.last?.status === 0 ? 0 : 1) || a.name.localeCompare(b.name)
        }
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tunnels, q, via, source, state, sort])

  const online = tunnels.filter((m) => m.last?.status === 1).length
  const traffic = tunnels.reduce((a, m) => a + (m.tunnel?.totalBytes ?? 0), 0)
  const pings = tunnels.map((m) => (m.last?.status === 1 ? m.last.ping : null)).filter((v): v is number => v != null)
  const avg = pings.length ? pings.reduce((a, b) => a + b, 0) / pings.length : -1
  const hasKariz = tunnels.some((m) => sourceOf(m) === 'kariz')

  return (
    <div className="p-4 sm:p-6 lg:p-8 space-y-6 w-full max-w-[1440px] mx-auto">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="font-display text-[28px] font-semibold tracking-tight">{t('Tunnels')}</h1>
          <p className="text-sm text-muted mt-1">{t('BackPack and Kariz tunnels from your panels and servers, checked continuously.')}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {tab === 'tunnels' && canEdit(role) && tunnels.length > 0 && (
            <Button icon={<Turtle className="size-4" />} onClick={() => setSlowOpen(true)}>
              {t('Slow alerts')} <span className="text-muted tnum">{n(tunnels.filter((m) => (m.config.latencyAlertMs ?? 0) > 0).length)}/{n(tunnels.length)}</span>
            </Button>
          )}
          <Segmented ariaLabel={t('Section')} value={tab} onChange={(v) => nav(v === 'panels' ? '/tunnels/panels' : '/tunnels')} options={[{ value: 'tunnels', label: t('Tunnels') }, { value: 'panels', label: `${t('BackPack panels')} (${n(panels.length)})` }]} />
        </div>
      </div>
      <TunnelSlowAlertsDialog open={slowOpen} onClose={() => setSlowOpen(false)} ids={list.map((m) => m.id)} />
      <AdCard />

      {tab === 'panels' ? <Panels /> : (
        <>
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
            {[
              { l: t('Online'), v: <span dir="ltr">{n(online)}/{n(tunnels.length)}</span>, tone: online < tunnels.length ? 'text-down' : 'text-up' },
              { l: t('Total traffic'), v: bytes(traffic) },
              { l: t('Average latency'), v: ms(avg) },
              { l: t('Sources'), v: n(vias.length) },
            ].map((k) => (
              <div key={k.l} className="card px-5 py-4">
                <p className="text-[12.5px] text-muted">{k.l}</p>
                <p className={clsx('mt-1 font-display text-[26px] font-semibold tnum', k.tone)}>{k.v}</p>
              </div>
            ))}
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <div className="relative flex-1 min-w-[200px] max-w-sm">
              <Search className="absolute start-3 top-1/2 -translate-y-1/2 size-4 text-subtle" />
              <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Search tunnels, locations, transports')} className="ps-9" aria-label={t('Search tunnels')} />
            </div>
            {vias.length > 1 && (
              <Select aria-label={t('Server or panel')} value={via} onChange={(e) => setVia(e.target.value)} className="w-48">
                <option value="">{t('All servers & panels')}</option>
                {vias.map((v) => <option key={v} value={v}>{viaName(v)}</option>)}
              </Select>
            )}
            {hasKariz && (
              <Segmented ariaLabel={t('Program')} value={source} onChange={setSource} options={[{ value: 'all', label: t('All') }, { value: 'backpack', label: 'BackPack' }, { value: 'kariz', label: 'Kariz' }]} />
            )}
            <Segmented ariaLabel={t('State')} value={state} onChange={setState} options={[{ value: 'all', label: t('All') }, { value: 'online', label: t('Online') }, { value: 'offline', label: t('Offline') }]} />
            <Select aria-label={t('Sort')} value={sort} onChange={(e) => setSort(e.target.value as typeof sort)} className="w-40 ms-auto">
              <option value="status">{t('Problems first')}</option>
              <option value="name">{t('Name')}</option>
              <option value="latency">{t('Lowest latency')}</option>
              <option value="traffic">{t('Most traffic')}</option>
            </Select>
            <IconButton label={t('Refresh')} onClick={() => useApp.getState().loadDashboard()}><RefreshCw className="size-4" /></IconButton>
          </div>

          {!loaded ? (
            <div className="grid md:grid-cols-2 2xl:grid-cols-3 gap-4">{Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-80 rounded-[18px]" />)}</div>
          ) : tunnels.length === 0 ? (
            <Card>
              <Empty icon={<Cable className="size-6" />} title={t('No tunnels yet')} body={t('Install the agent on a server that runs BackPack or Kariz, or connect a BackPack panel — tunnels are imported as monitors automatically.')}
                action={<div className="flex flex-wrap justify-center gap-2"><Button variant="primary" icon={<Server className="size-4" />} onClick={() => nav('/servers')}>{t('Add a server')}</Button><Button icon={<Plus className="size-4" />} onClick={() => nav('/tunnels/panels')}>{t('Connect panel')}</Button></div>} />
            </Card>
          ) : list.length === 0 ? (
            <p className="py-12 text-center text-sm text-muted">{t('No tunnel matches these filters.')}</p>
          ) : (
            <div className="grid md:grid-cols-2 2xl:grid-cols-3 gap-4">
              {list.map((m) => <TunnelCard key={m.id} m={m} via={viaName(viaOf(m))} />)}
            </div>
          )}
        </>
      )}
    </div>
  )
}
