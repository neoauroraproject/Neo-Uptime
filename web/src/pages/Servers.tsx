import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ArrowDown, ArrowUp, CircleCheck, Cpu, Download, Ellipsis, HardDrive, KeyRound, LoaderCircle, MemoryStick, Pencil, Plus, Server, Terminal, Trash2 } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { bytes, clsx, duration, n, pct, rate, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import type { Agent, AgentSource } from '../lib/types'
import { Alert, Button, Card, Checkbox, CopyButton, Empty, Field, IconButton, Input, Menu, Modal, Select, Skeleton, Switch, confirm, toast, useNow } from '../components/ui'
import { CountryPicker, Flag, countryName } from '../components/flag'
import { SourceBadge } from '../components/meta'
import { TunnelBadges } from '../components/tunnel'

function Bar({ value, label }: { value: number; label: string }) {
  const tone = value >= 90 ? 'var(--down)' : value >= 75 ? 'var(--pending)' : 'var(--up)'
  return (
    <div className="min-w-0">
      <div className="flex items-center justify-between text-[11.5px] text-muted"><span>{label}</span><span className="tnum">{pct(value / 100, 0)}</span></div>
      <div className="mt-1 h-1.5 rounded-full bg-surface-3 overflow-hidden"><div className="h-full rounded-full transition-[width] duration-500" style={{ width: `${Math.min(100, value)}%`, background: tone }} /></div>
    </div>
  )
}

function InstallBox({ command }: { command: string }) {
  const t = useT()
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 text-[13px] font-medium"><Terminal className="size-4 text-muted" />{t('Run this on the server, as root:')}</div>
      <div className="flex gap-2 items-start">
        <pre className="flex-1 min-w-0 overflow-x-auto whitespace-pre-wrap break-all rounded-xl bg-[#0b0f0c] text-[#d9f99d] border border-line px-3 py-3 font-mono text-[12px] leading-relaxed" dir="ltr">{command}</pre>
        <CopyButton text={command} />
      </div>
    </div>
  )
}

function AddDialog({ open, onClose, edit }: { open: boolean; onClose: () => void; edit?: Agent }) {
  const t = useT()
  const agents = useApp((s) => s.agents)
  const [f, setF] = useState({ name: '', country: '', interval: 20, autoImport: true })
  const [created, setCreated] = useState<{ id: number; install: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  useEffect(() => {
    if (!open) return
    setErr('')
    setCreated(null)
    setF(edit ? { name: edit.name, country: edit.country, interval: edit.interval, autoImport: edit.autoImport } : { name: '', country: '', interval: 20, autoImport: true })
  }, [open, edit])

  // While the dialog waits for the first report, look often.
  useEffect(() => {
    if (!created) return
    const tm = setInterval(() => useApp.getState().loadAgents().catch(() => {}), 3000)
    return () => clearInterval(tm)
  }, [created])
  const live = created ? agents.find((a) => a.id === created.id) : undefined

  const save = async () => {
    setBusy(true)
    setErr('')
    try {
      if (edit) {
        await api.put(`/api/agents/${edit.id}`, f)
        toast.success(t('Server saved'))
        await useApp.getState().loadAgents()
        onClose()
      } else {
        const r = await api.post<{ agent: Agent; token: string; install: string }>('/api/agents', f)
        setCreated({ id: r.agent.id, install: r.install })
        await useApp.getState().loadAgents()
      }
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} size="lg" title={edit ? t('Edit {name}', { name: edit.name }) : created ? t('Install the agent') : t('Add a server')}
      description={created ? undefined : t('The agent reports this server’s health and its BackPack and Kariz tunnels to Neo Uptime. It only sends; nothing connects to it.')}
      footer={created ? <Button variant="primary" onClick={onClose}>{t('Done')}</Button> : <><Button variant="ghost" onClick={onClose}>{t('Cancel')}</Button><Button variant="primary" loading={busy} disabled={!f.name} onClick={save}>{edit ? t('Save') : t('Create & show install command')}</Button></>}>
      {created ? (
        <div className="space-y-5">
          <InstallBox command={created.install} />
          <Alert tone="warning">{t('The command contains this server’s token. It is shown only now — regenerate it from the server’s menu if you lose it.')}</Alert>
          <div className={clsx('flex items-center gap-3 rounded-2xl border px-4 py-3', live?.online ? 'border-up/30 bg-up/8' : 'border-line bg-surface-2')}>
            {live?.online ? <CircleCheck className="size-5 text-up" /> : <LoaderCircle className="size-5 text-muted animate-spin" />}
            <div className="text-sm">
              {live?.online
                ? t('Reporting! {bp} BackPack and {kz} Kariz tunnels found; they are being added as monitors.', { bp: live.info?.backpack?.tunnels ?? 0, kz: live.info?.kariz?.tunnels ?? 0 })
                : t('Waiting for the first report…')}
            </div>
          </div>
          <details className="text-[13px] text-muted">
            <summary className="cursor-pointer select-none hover:text-fg">{t('Options: BackPack through its panel API, a GitHub mirror, uninstall')}</summary>
            <div className="mt-2 space-y-1.5 leading-relaxed" dir="ltr">
              <p><code className="font-mono">--backpack-api https://127.0.0.1:7777 --backpack-token TOKEN</code></p>
              <p><code className="font-mono">--mirror https://your-gh-proxy/</code> · <code className="font-mono">--interval 30</code> · <code className="font-mono">--insecure</code></p>
              <p><code className="font-mono">bash &lt;(curl -fsSL …/agent.sh) --uninstall</code></p>
            </div>
          </details>
        </div>
      ) : (
        <div className="space-y-4">
          {err && <Alert tone="danger">{err}</Alert>}
          <div className="grid sm:grid-cols-2 gap-4">
            <Field label={t('Name')} required htmlFor="an"><Input id="an" dir="auto" data-autofocus value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder={t('Iran edge')} /></Field>
            <Field label={t('Flag')} htmlFor="ac"><CountryPicker id="ac" value={f.country} onChange={(v) => setF({ ...f, country: v })} /></Field>
          </div>
          <div className="grid sm:grid-cols-2 gap-4 items-start">
            <Field label={t('Report every')} htmlFor="ai">
              <Select id="ai" value={f.interval} onChange={(e) => setF({ ...f, interval: Number(e.target.value) })}>
                {[10, 20, 30, 60, 120].map((s) => <option key={s} value={s}>{s < 60 ? t('{n} seconds', { n: s }) : t('{n} minutes', { n: s / 60 })}</option>)}
              </Select>
            </Field>
            <div className="sm:pt-6"><Switch checked={f.autoImport} onChange={(v) => setF({ ...f, autoImport: v })} label={t('Add its tunnels automatically')} description={t('Every BackPack and Kariz tunnel on the server becomes a monitor.')} /></div>
          </div>
        </div>
      )}
    </Modal>
  )
}

function TunnelsDialog({ agent, onClose }: { agent: Agent | null; onClose: () => void }) {
  const t = useT()
  const [data, setData] = useState<{ backpack?: AgentSource; kariz?: AgentSource; imported: Record<string, number>; install: string } | null>(null)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!agent) return
    setData(null)
    api.get<{ backpack?: AgentSource; kariz?: AgentSource; imported: Record<string, number>; install: string }>(`/api/agents/${agent.id}`).then((r) => {
      setData(r)
      const keys: string[] = []
      for (const [src, s] of [['backpack', r.backpack], ['kariz', r.kariz]] as const) for (const tn of s?.tunnels ?? []) if (!r.imported[`${src}/${tn.name}`]) keys.push(`${src}/${tn.name}`)
      setPicked(new Set(keys))
    }).catch(() => {})
  }, [agent])
  const run = async () => {
    if (!agent) return
    setBusy(true)
    try {
      const r = await api.post<{ imported: number }>(`/api/agents/${agent.id}/import`, { keys: [...picked] })
      toast.success(t('{n} tunnels imported', { n: r.imported }))
      await useApp.getState().loadDashboard()
      onClose()
    } catch (e) {
      toast.error(t('Import failed'), (e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const sections = data ? ([['backpack', data.backpack], ['kariz', data.kariz]] as const).filter(([, s]) => s) : []
  return (
    <Modal open={!!agent} onClose={onClose} size="lg" title={t('Tunnels on {name}', { name: agent?.name ?? '' })} description={t('What the agent found on this server. Pick the ones to watch.')}
      footer={<><Button variant="ghost" onClick={onClose}>{t('Close')}</Button><Button variant="primary" loading={busy} disabled={!picked.size} onClick={run} icon={<Download className="size-4" />}>{t('Import')} {picked.size ? n(picked.size) : ''}</Button></>}>
      {!data ? <Skeleton className="h-40" /> : sections.length === 0 ? (
        <p className="text-sm text-muted">{agent?.lastSeen ? t('Neither BackPack nor Kariz was found on this server.') : t('The agent has not reported yet.')}</p>
      ) : (
        <div className="space-y-5">
          {sections.map(([src, s]) => (
            <div key={src} className="space-y-2">
              <div className="flex items-center gap-2"><SourceBadge source={src} />{s!.version && <span className="text-[12px] text-subtle font-mono" dir="ltr">{s!.version}</span>}{s!.mode && <span className="text-[12px] text-subtle">· {s!.mode}</span>}</div>
              {s!.error && <Alert tone="danger">{s!.error}</Alert>}
              {s!.tunnels.length === 0 ? <p className="text-sm text-muted">{t('No tunnels.')}</p> : (
                <div className="divide-y divide-line rounded-2xl border border-line">
                  {s!.tunnels.map((tn) => {
                    const key = `${src}/${tn.name}`
                    const done = !!data.imported[key]
                    return (
                      <div key={key} className="flex items-center gap-3 px-4 py-3">
                        {done ? <CircleCheck className="size-[18px] text-up" aria-label={t('Already imported')} /> : (
                          <Checkbox checked={picked.has(key)} onChange={(v) => { const nx = new Set(picked); if (v) nx.add(key); else nx.delete(key); setPicked(nx) }} label={<span className="sr-only">{tn.name}</span>} />
                        )}
                        <div className="min-w-0 flex-1">
                          <p className="font-medium">{tn.name}</p>
                          <TunnelBadges t={tn} className="mt-1" />
                          {tn.lastError && <p className="mt-1 text-[12px] text-down truncate" dir="auto">{tn.lastError}</p>}
                        </div>
                        <span className={clsx('text-[12.5px] font-medium', tn.state === 'online' ? 'text-up' : 'text-down')}>{t(tn.state === 'online' ? 'Online' : tn.state === 'stopped' ? 'Stopped' : 'Offline')}</span>
                        {done && <Link to={`/dashboard/${data.imported[key]}`} className="text-[12.5px] text-accent-ink hover:underline">{t('View')}</Link>}
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </Modal>
  )
}

function ServerCard({ a, onEdit, onTunnels }: { a: Agent; onEdit: () => void; onTunnels: () => void }) {
  const t = useT()
  const role = useApp((s) => s.role)
  const now = useNow(10_000)
  const [token, setToken] = useState<string | null>(null)
  const s = a.info?.system
  const memPct = s?.memTotal ? (100 * s.memUsed) / s.memTotal : 0
  const diskPct = s?.diskTotal ? (100 * s.diskUsed) / s.diskTotal : 0
  const never = !a.lastSeen
  const del = async () => {
    if (!(await confirm({ title: t('Remove {name}?', { name: a.name }), body: t('Its server and tunnel monitors are deleted too. Uninstall the agent on the server with agent.sh --uninstall.'), confirmLabel: t('Remove'), danger: true }))) return
    try {
      await api.del(`/api/agents/${a.id}?deleteMonitors=1`)
      await Promise.all([useApp.getState().loadAgents(), useApp.getState().loadDashboard()])
      toast.success(t('Server removed'))
    } catch (e) {
      toast.error(t('Could not delete'), (e as Error).message)
    }
  }
  const regen = async () => {
    if (!(await confirm({ title: t('Make a new token?'), body: t('The current token stops working at once; run the new install command on the server.'), confirmLabel: t('New token'), danger: true }))) return
    const r = await api.post<{ install: string }>(`/api/agents/${a.id}/token`)
    setToken(r.install)
  }
  return (
    <Card className={clsx('p-5 flex flex-col gap-4 relative overflow-hidden', !a.online && !never && 'border-down/40')}>
      <div className="flex items-start gap-3">
        <Flag cc={a.country} size={34} className="mt-0.5 rounded-md" title />
        <div className="min-w-0 flex-1">
          <p className="font-display text-lg font-semibold leading-tight truncate" dir="auto">{a.name}</p>
          <p className="text-[12.5px] text-muted truncate" dir="ltr">{[s?.hostname, a.lastIp].filter(Boolean).join(' · ') || (a.country ? countryName(a.country) : '')}</p>
        </div>
        <span className={clsx('inline-flex items-center gap-1.5 h-6 px-2.5 rounded-full border text-[12px] font-semibold', a.online ? 'bg-up/12 text-up border-up/25' : never ? 'bg-surface-3 text-muted border-line' : 'bg-down/12 text-down border-down/25')}>
          <span className={clsx('size-1.5 rounded-full', a.online ? 'bg-up live-dot' : never ? 'bg-subtle' : 'bg-down')} />
          {a.online ? t('Online') : never ? t('Not installed') : t('Offline')}
        </span>
        {canEdit(role) && (
          <Menu trigger={(tr) => <IconButton {...tr} size="sm" label={t('Server actions')}><Ellipsis className="size-4" /></IconButton>} items={[
            { label: t('Tunnels on this server'), icon: <Download />, onClick: onTunnels },
            ...(role === 'admin' ? [
              { label: t('Edit'), icon: <Pencil />, onClick: onEdit },
              { label: t('New token & install command'), icon: <KeyRound />, onClick: regen },
              { divider: true, label: '' },
              { label: t('Remove'), icon: <Trash2 />, danger: true, onClick: del },
            ] : []),
          ]} />
        )}
      </div>
      {never ? (
        <p className="text-sm text-muted">{t('Run the install command on the server; it appears here within seconds.')}</p>
      ) : s && (s.memTotal > 0 || s.cpuCores > 0) ? (
        <>
          <div className="grid grid-cols-3 gap-3">
            <div className="flex gap-2 min-w-0"><Cpu className="size-4 text-subtle shrink-0" /><div className="flex-1 min-w-0"><Bar value={s.cpuPercent} label={t('CPU')} /></div></div>
            <div className="flex gap-2 min-w-0"><MemoryStick className="size-4 text-subtle shrink-0" /><div className="flex-1 min-w-0"><Bar value={memPct} label={t('RAM')} /></div></div>
            <div className="flex gap-2 min-w-0"><HardDrive className="size-4 text-subtle shrink-0" /><div className="flex-1 min-w-0"><Bar value={diskPct} label={t('Disk')} /></div></div>
          </div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[12.5px] text-muted tnum">
            <span className="inline-flex items-center gap-1"><ArrowDown className="size-3.5 text-up" />{rate(s.netRxRate)}</span>
            <span className="inline-flex items-center gap-1"><ArrowUp className="size-3.5 text-maint" />{rate(s.netTxRate)}</span>
            {s.uptimeSecs > 0 && <span>{t('up {d}', { d: duration(s.uptimeSecs) })}</span>}
            {s.cpuCores > 0 && <span>{t('{n} cores', { n: s.cpuCores })}</span>}
            {s.memTotal > 0 && <span>{bytes(s.memTotal)} {t('RAM')}</span>}
          </div>
        </>
      ) : null}
      {(a.info?.backpack || a.info?.kariz) && (
        <div className="flex flex-wrap gap-2">
          {a.info?.backpack && <button onClick={onTunnels} className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-lg border border-line bg-surface-2 text-[12px] hover:border-line-strong"><SourceBadge source="backpack" /><span className="tnum" dir="ltr">{n(a.info.backpack.online)}/{n(a.info.backpack.tunnels)}</span></button>}
          {a.info?.kariz && <button onClick={onTunnels} className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-lg border border-line bg-surface-2 text-[12px] hover:border-line-strong"><SourceBadge source="kariz" /><span className="tnum" dir="ltr">{n(a.info.kariz.online)}/{n(a.info.kariz.tunnels)}</span></button>}
        </div>
      )}
      <div className="flex items-center justify-between text-[12px] text-subtle mt-auto">
        <span>{a.version ? `agent ${a.version}` : ''}</span>
        <span>{a.lastSeen ? t('last report {ago}', { ago: timeAgo(a.lastSeen * 1000, now) }) : ''}</span>
      </div>
      <Modal open={!!token} onClose={() => setToken(null)} title={t('New install command for {name}', { name: a.name })} footer={<Button variant="primary" onClick={() => setToken(null)}>{t('Done')}</Button>}>
        {token && <InstallBox command={token} />}
      </Modal>
    </Card>
  )
}

export default function Servers() {
  const t = useT()
  const agents = useApp((s) => s.agents)
  const role = useApp((s) => s.role)
  const [loaded, setLoaded] = useState(false)
  const [open, setOpen] = useState(false)
  const [edit, setEdit] = useState<Agent | undefined>()
  const [tunnelsOf, setTunnelsOf] = useState<Agent | null>(null)

  useEffect(() => {
    const load = () => useApp.getState().loadAgents().catch(() => {}).finally(() => setLoaded(true))
    load()
    const tm = setInterval(load, 10_000)
    return () => clearInterval(tm)
  }, [])

  const online = agents.filter((a) => a.online).length
  return (
    <div className="p-4 sm:p-6 lg:p-8 space-y-6 max-w-[1600px]">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="font-display text-[28px] font-semibold tracking-tight">{t('Servers')}</h1>
          <p className="text-sm text-muted mt-1">{t('Servers running neo-uptime-agent — their health, and their BackPack and Kariz tunnels, pushed here with a token.')}</p>
        </div>
        {role === 'admin' && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => { setEdit(undefined); setOpen(true) }}>{t('Add a server')}</Button>}
      </div>

      {agents.length > 0 && (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
          {[
            { l: t('Online'), v: <span dir="ltr">{n(online)}/{n(agents.length)}</span>, tone: online < agents.length ? 'text-down' : 'text-up' },
            { l: t('BackPack tunnels'), v: n(agents.reduce((s, a) => s + (a.info?.backpack?.tunnels ?? 0), 0)) },
            { l: t('Kariz tunnels'), v: n(agents.reduce((s, a) => s + (a.info?.kariz?.tunnels ?? 0), 0)) },
            { l: t('Traffic now'), v: rate(agents.reduce((s, a) => s + (a.info?.system.netRxRate ?? 0) + (a.info?.system.netTxRate ?? 0), 0)) },
          ].map((k) => (
            <div key={k.l} className="card px-5 py-4">
              <p className="text-[12.5px] text-muted">{k.l}</p>
              <p className={clsx('mt-1 font-display text-[26px] font-semibold tnum', k.tone)}>{k.v}</p>
            </div>
          ))}
        </div>
      )}

      {!loaded ? (
        <div className="grid md:grid-cols-2 2xl:grid-cols-3 gap-4">{Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-56 rounded-[18px]" />)}</div>
      ) : agents.length === 0 ? (
        <Card>
          <Empty icon={<Server className="size-6" />} title={t('Add your first server')}
            body={t('Install the small agent on any server with one command. It reads BackPack (from its files) and Kariz (from its control sockets) on that machine and sends everything here — so Neo Uptime can run on a different server.')}
            action={role === 'admin' && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setOpen(true)}>{t('Add a server')}</Button>} />
        </Card>
      ) : (
        <div className="grid md:grid-cols-2 2xl:grid-cols-3 gap-4">
          {agents.map((a) => <ServerCard key={a.id} a={a} onEdit={() => { setEdit(a); setOpen(true) }} onTunnels={() => setTunnelsOf(a)} />)}
        </div>
      )}
      <AddDialog open={open} onClose={() => setOpen(false)} edit={edit} />
      <TunnelsDialog agent={tunnelsOf} onClose={() => setTunnelsOf(null)} />
    </div>
  )
}
