import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { CalendarClock, Ellipsis, ListChecks, MapPin, Pencil, Plus, RefreshCw, Rocket, Search, Trash2, TriangleAlert } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { bytes, clsx, duration, ms, n, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import type { MonitorView, SubPreview, Subscription } from '../lib/types'
import { Alert, Badge, Button, Card, Checkbox, Empty, Field, IconButton, Input, Menu, Modal, Segmented, Select, Skeleton, Spinner, Switch, confirm, toast, useNow } from '../components/ui'
import { ConfigCard } from '../components/xray'
import { Flag } from '../components/flag'
import { AdCard } from '../components/AdCard'

/* ---------- usage ---------- */

function Usage({ s }: { s: Subscription }) {
  const t = useT()
  const now = useNow(60_000)
  const i = s.info
  const used = (i.upload ?? 0) + (i.download ?? 0)
  const left = i.expire ? i.expire * 1000 - now : 0
  const days = Math.floor(left / 86400e3)
  return (
    <div className="grid sm:grid-cols-2 gap-3">
      {i.hasUsage && (
        <div className="rounded-xl bg-surface-2 border border-line px-3.5 py-2.5">
          <div className="flex items-center justify-between text-[12px] text-muted"><span>{t('Traffic used')}</span><span className="tnum" dir="ltr">{bytes(used)}{i.total ? ` / ${bytes(i.total)}` : ''}</span></div>
          {i.total ? (
            <div className="mt-2 h-1.5 rounded-full bg-surface-3 overflow-hidden">
              <div className={clsx('h-full rounded-full', used / i.total > 0.9 ? 'bg-down' : used / i.total > 0.7 ? 'bg-pending' : 'bg-up')} style={{ width: `${Math.min(100, (used / i.total) * 100)}%` }} />
            </div>
          ) : <p className="mt-1 text-[12.5px] font-medium">{t('Unlimited')}</p>}
        </div>
      )}
      {i.hasUsage && (
        <div className="rounded-xl bg-surface-2 border border-line px-3.5 py-2.5 flex items-center gap-2.5">
          <CalendarClock className={clsx('size-4.5 shrink-0', i.expire && left <= 0 ? 'text-down' : i.expire && days < 3 ? 'text-pending' : 'text-muted')} />
          <div className="min-w-0">
            <p className="text-[12px] text-muted">{t('Account')}</p>
            <p className={clsx('text-[13px] font-medium', !!i.expire && left <= 0 && 'text-down')}>
              {!i.expire ? t('Never expires') : left <= 0 ? t('Expired') : days >= 1 ? t('{n} days left', { n: days }) : t('Expires in {d}', { d: duration(Math.round(left / 1000)) })}
            </p>
          </div>
        </div>
      )}
    </div>
  )
}

/* ---------- edit ---------- */

function SubDialog({ sub, onClose }: { sub: Subscription | null; onClose: () => void }) {
  const t = useT()
  const agents = useApp((s) => s.agents)
  const [f, setF] = useState({ name: '', url: '', agentId: 0, interval: 60, timeout: 15, testUrl: '', autoImport: true, refreshMinutes: 60 })
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  useEffect(() => {
    if (!sub) return
    setF({ name: sub.name, url: sub.url, agentId: sub.agentId, interval: sub.interval, timeout: sub.timeout, testUrl: sub.testUrl, autoImport: sub.autoImport, refreshMinutes: sub.refreshMinutes })
    setErr('')
  }, [sub])
  const save = async () => {
    if (!sub) return
    setBusy(true)
    setErr('')
    try {
      const r = await api.put<{ error?: string }>(`/api/subscriptions/${sub.id}`, f)
      if (r.error) toast.error(t('Saved, but the subscription could not be read'), r.error)
      else toast.success(t('Subscription saved'))
      await Promise.all([useApp.getState().loadSubscriptions(), useApp.getState().loadDashboard()])
      onClose()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal open={!!sub} onClose={onClose} size="lg" title={t('Edit {name}', { name: sub?.name ?? '' })}
      footer={<><Button variant="ghost" onClick={onClose}>{t('Cancel')}</Button><Button variant="primary" loading={busy} onClick={save}>{t('Save')}</Button></>}>
      <div className="space-y-4">
        {err && <Alert tone="danger">{err}</Alert>}
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label={t('Name')} htmlFor="sn"><Input id="sn" dir="auto" value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
          <Field label={t('Test from')} htmlFor="sa">
            <Select id="sa" value={f.agentId} onChange={(e) => setF({ ...f, agentId: Number(e.target.value) })}>
              <option value={0}>{t('This panel’s server')}</option>
              {agents.map((a) => <option key={a.id} value={a.id}>{a.name}{a.online ? '' : ` (${t('offline')})`}</option>)}
            </Select>
          </Field>
        </div>
        <Field label={t('Subscription link')} htmlFor="su"><Input id="su" dir="ltr" className="font-mono" value={f.url} onChange={(e) => setF({ ...f, url: e.target.value })} /></Field>
        <div className="grid sm:grid-cols-3 gap-4">
          <Field label={t('Test every')} htmlFor="si">
            <Select id="si" value={f.interval} onChange={(e) => setF({ ...f, interval: Number(e.target.value) })}>
              {[30, 60, 120, 300, 600].map((s) => <option key={s} value={s}>{duration(s)}</option>)}
            </Select>
          </Field>
          <Field label={t('Give up after')} htmlFor="st">
            <Select id="st" value={f.timeout} onChange={(e) => setF({ ...f, timeout: Number(e.target.value) })}>
              {[5, 10, 15, 20, 30].map((s) => <option key={s} value={s}>{t('{n} seconds', { n: s })}</option>)}
            </Select>
          </Field>
          <Field label={t('Read the subscription')} htmlFor="sr">
            <Select id="sr" value={f.refreshMinutes} onChange={(e) => setF({ ...f, refreshMinutes: Number(e.target.value) })}>
              {[15, 30, 60, 180, 720, 1440, 0].map((m) => <option key={m} value={m}>{m === 0 ? t('Only by hand') : t('Every {d}', { d: duration(m * 60) })}</option>)}
            </Select>
          </Field>
        </div>
        <Field label={t('Test address')} htmlFor="tu" help={t('Fetched through each config. Empty: {url}', { url: 'https://www.gstatic.com/generate_204' })}>
          <Input id="tu" dir="ltr" className="font-mono" value={f.testUrl} onChange={(e) => setF({ ...f, testUrl: e.target.value })} placeholder="https://www.gstatic.com/generate_204" />
        </Field>
        <Switch checked={f.autoImport} onChange={(v) => setF({ ...f, autoImport: v })} label={t('Add new configs automatically')} description={t('Configs that appear in the subscription later are watched too.')} />
      </div>
    </Modal>
  )
}

/** Pick which of a subscription's configs to watch. */
function ChooseDialog({ sub, onClose, have }: { sub: Subscription | null; onClose: () => void; have: Set<string> }) {
  const t = useT()
  const [data, setData] = useState<SubPreview | null>(null)
  const [err, setErr] = useState('')
  const [pick, setPick] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!sub) return
    setData(null)
    setErr('')
    api.post<SubPreview>('/api/subscriptions/preview', { url: sub.url, agentId: sub.agentId })
      .then((r) => { setData(r); setPick(new Set()) })
      .catch((e) => setErr((e as Error).message))
  }, [sub])
  const run = async () => {
    if (!sub) return
    setBusy(true)
    try {
      const r = await api.post<{ result: { added: number } }>(`/api/subscriptions/${sub.id}/refresh`, { keys: [...pick] })
      toast.success(t('{n} configs added', { n: r.result.added }))
      await Promise.all([useApp.getState().loadSubscriptions(), useApp.getState().loadDashboard()])
      onClose()
    } catch (e) {
      toast.error(t('Could not add'), (e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const list = data?.configs ?? []
  return (
    <Modal open={!!sub} onClose={onClose} size="lg" title={t('Configs in {name}', { name: sub?.name ?? '' })} description={t('Tick the configs to watch. The ones already watched are marked.')}
      footer={<><Button variant="ghost" onClick={onClose}>{t('Close')}</Button><Button variant="primary" loading={busy} disabled={!pick.size} onClick={run}>{t('Watch')} {pick.size ? n(pick.size) : ''}</Button></>}>
      {err ? <Alert tone="danger">{err}</Alert> : !data ? <div className="space-y-2">{Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-11" />)}</div> : (
        <div className="rounded-2xl border border-line divide-y divide-line max-h-[55vh] overflow-y-auto">
          {list.map((c) => {
            const watched = have.has(c.key)
            return (
              <div key={c.key} className="flex items-center gap-3 px-4 py-2.5">
                {watched ? <span className="text-[12px] text-up font-medium w-16 shrink-0">{t('Watched')}</span>
                  : <Checkbox checked={pick.has(c.key)} onChange={(v) => { const s = new Set(pick); if (v) s.add(c.key); else s.delete(c.key); setPick(s) }} label={<span className="sr-only">{c.key}</span>} />}
                <Flag cc={c.country} size={18} />
                <span className="min-w-0 flex-1 truncate font-medium" dir="auto">{c.label}</span>
                <span className="text-[12px] text-muted" dir="ltr">{c.summary}</span>
              </div>
            )
          })}
        </div>
      )}
    </Modal>
  )
}

/* ---------- page ---------- */

function SubSection({ s, list, onEdit, onChoose }: { s: Subscription; list: MonitorView[]; onEdit: () => void; onChoose: () => void }) {
  const t = useT()
  const nav = useNavigate()
  const role = useApp((st) => st.role)
  const agents = useApp((st) => st.agents)
  const now = useNow(30_000)
  const [busy, setBusy] = useState(false)
  const up = list.filter((m) => m.active && m.last?.status === 1).length
  const active = list.filter((m) => m.active).length
  const from = s.agentId ? agents.find((a) => a.id === s.agentId)?.name ?? `#${s.agentId}` : ''

  const refresh = async () => {
    setBusy(true)
    try {
      const r = await api.post<{ result: { added: number; updated: number; removed: number } }>(`/api/subscriptions/${s.id}/refresh`, {})
      const { added, updated, removed } = r.result
      toast.success(t('Subscription read'), added + updated + removed ? t('{a} added, {u} changed, {r} removed', { a: added, u: updated, r: removed }) : t('No changes'))
      await Promise.all([useApp.getState().loadSubscriptions(), useApp.getState().loadDashboard()])
    } catch (e) {
      toast.error(t('Could not read the subscription'), (e as Error).message)
      useApp.getState().loadSubscriptions().catch(() => {})
    } finally {
      setBusy(false)
    }
  }
  const del = async () => {
    const ok = await confirm({ title: t('Delete {name}?', { name: s.name }), body: t('Its config monitors and their history are deleted too.'), confirmLabel: t('Delete'), danger: true })
    if (!ok) return
    try {
      await api.del(`/api/subscriptions/${s.id}`)
      toast.success(t('Subscription deleted'))
      await Promise.all([useApp.getState().loadSubscriptions(), useApp.getState().loadDashboard()])
    } catch (e) {
      toast.error(t('Could not delete'), (e as Error).message)
    }
  }

  return (
    <section className="space-y-3">
      <Card className="p-5 space-y-4">
        <div className="flex flex-wrap items-start gap-3">
          <span className="grid place-items-center size-11 rounded-2xl bg-accent-soft text-accent-ink shrink-0"><Rocket className="size-5" /></span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="font-display text-xl font-semibold" dir="auto">{s.name}</h2>
              <Badge tone={active && up === active ? 'up' : up === 0 && active ? 'down' : 'pending'}><span dir="ltr">{n(up)}/{n(active)}</span>&nbsp;{t('working')}</Badge>
              {s.autoImport && <Badge>{t('New configs added automatically')}</Badge>}
            </div>
            <p className="mt-1 text-[12.5px] text-muted flex flex-wrap items-center gap-x-3 gap-y-1">
              {s.info.title && s.info.title !== s.name && <span dir="auto">{s.info.title}</span>}
              <span className="inline-flex items-center gap-1"><MapPin className="size-3.5" />{from ? t('Tested from {name}', { name: from }) : t('Tested from this panel')}</span>
              <span className="tnum">{s.lastFetch ? t('read {ago}', { ago: timeAgo(s.lastFetch * 1000, now) }) : t('not read yet')}</span>
              {s.info.via && <span>{t('through {name}', { name: s.info.via })}</span>}
              <span>{t('{n} configs', { n: s.info.configs })}</span>
              {!!s.info.unusable && <span>{t('{n} not testable', { n: s.info.unusable })}</span>}
            </p>
          </div>
          <div className="flex items-center gap-1">
            {canEdit(role) && <IconButton label={t('Read the subscription now')} onClick={refresh} disabled={busy}>{busy ? <Spinner className="size-4" /> : <RefreshCw className="size-4" />}</IconButton>}
            {canEdit(role) && (
              <Menu trigger={(tr) => <IconButton {...tr} label={t('Subscription actions')}><Ellipsis className="size-4" /></IconButton>} items={[
                { label: t('Choose configs'), icon: <ListChecks />, onClick: onChoose },
                { label: t('Edit'), icon: <Pencil />, onClick: onEdit },
                { label: t('Open the group'), icon: <Rocket />, onClick: () => s.groupMonitorId && nav(`/dashboard/${s.groupMonitorId}`) },
                { divider: true, label: '' },
                { label: t('Delete'), icon: <Trash2 />, danger: true, onClick: del },
              ]} />
            )}
          </div>
        </div>
        {s.lastError && <p className="flex items-start gap-2 rounded-xl border border-down/30 bg-down/8 px-3 py-2 text-[13px] text-down"><TriangleAlert className="size-4 shrink-0 mt-0.5" /><span dir="auto">{s.lastError}</span></p>}
        <Usage s={s} />
      </Card>
      {list.length === 0 ? (
        <p className="px-1 text-sm text-muted">{t('No configs watched from this subscription.')} {canEdit(role) && <button className="text-accent-ink hover:underline" onClick={onChoose}>{t('Choose configs')}</button>}</p>
      ) : (
        <div className="grid md:grid-cols-2 2xl:grid-cols-3 gap-4">{list.map((m) => <ConfigCard key={m.id} m={m} />)}</div>
      )}
    </section>
  )
}

export default function Configs() {
  const t = useT()
  const nav = useNavigate()
  const monitors = useApp((s) => s.monitors)
  const subs = useApp((s) => s.subscriptions)
  const loaded = useApp((s) => s.loaded)
  const role = useApp((s) => s.role)
  const [q, setQ] = useState('')
  const [state, setState] = useState<'all' | 'ok' | 'bad'>('all')
  const [sort, setSort] = useState<'status' | 'fastest' | 'name'>('status')
  const [edit, setEdit] = useState<Subscription | null>(null)
  const [choose, setChoose] = useState<Subscription | null>(null)

  useEffect(() => { useApp.getState().loadSubscriptions().catch(() => {}) }, [])

  const all = useMemo(() => Object.values(monitors).filter((m) => m.type === 'xray'), [monitors])
  const filtered = useMemo(() => {
    const s = q.trim().toLowerCase()
    return all
      .filter((m) => !s || m.name.toLowerCase().includes(s) || (m.xray?.summary ?? '').toLowerCase().includes(s) || (m.xray?.endpoint ?? '').toLowerCase().includes(s))
      .filter((m) => state === 'all' || (state === 'ok' ? m.last?.status === 1 : m.active && m.last?.status !== 1))
      .sort((a, b) => {
        if (sort === 'name') return a.name.localeCompare(b.name)
        if (sort === 'fastest') return (a.last?.status === 1 ? a.last.ping ?? 1e9 : 1e9) - (b.last?.status === 1 ? b.last.ping ?? 1e9 : 1e9)
        const rank = (m: MonitorView) => (!m.active ? 3 : m.last?.status === 0 ? 0 : m.last?.status === 1 ? 2 : 1)
        return rank(a) - rank(b) || a.name.localeCompare(b.name)
      })
  }, [all, q, state, sort])

  const working = all.filter((m) => m.active && m.last?.status === 1)
  const pings = working.map((m) => m.last?.ping).filter((v): v is number => v != null)
  const avg = pings.length ? pings.reduce((a, b) => a + b, 0) / pings.length : -1
  const best = working.length ? working.reduce((a, b) => ((a.last?.ping ?? 1e9) <= (b.last?.ping ?? 1e9) ? a : b)) : null
  const loose = filtered.filter((m) => !m.config.subscriptionId || !subs.some((s) => s.id === m.config.subscriptionId))

  return (
    <div className="p-4 sm:p-6 lg:p-8 space-y-6 w-full max-w-[1440px] mx-auto">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="font-display text-[28px] font-semibold tracking-tight">{t('V2Ray configs')}</h1>
          <p className="text-sm text-muted mt-1">{t('Each config is connected for real through an Xray core — from this panel, or from one of your servers.')}</p>
        </div>
        {canEdit(role) && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => nav('/add?type=xray')}>{t('Add subscription or config')}</Button>}
      </div>
      <AdCard />

      {all.length > 0 && (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
          {[
            { l: t('Working'), v: <span dir="ltr">{n(working.length)}/{n(all.filter((m) => m.active).length)}</span>, tone: working.length < all.filter((m) => m.active).length ? 'text-down' : 'text-up' },
            { l: t('Average delay'), v: ms(avg) },
            { l: t('Fastest'), v: best ? <span className="flex items-center gap-2 min-w-0 max-w-full"><Flag cc={best.country} size={22} className="shrink-0" /><span className="min-w-0 truncate text-[20px]" dir="auto">{best.name}</span></span> : '—' },
            { l: t('Subscriptions'), v: n(subs.length) },
          ].map((k) => (
            <div key={k.l} className="card px-5 py-4 min-w-0">
              <p className="text-[12.5px] text-muted">{k.l}</p>
              <div className={clsx('mt-1 font-display text-[26px] font-semibold tnum truncate', k.tone)}>{k.v}</div>
            </div>
          ))}
        </div>
      )}

      {all.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative flex-1 min-w-[200px] max-w-sm">
            <Search className="absolute start-3 top-1/2 -translate-y-1/2 size-4 text-subtle" />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Search configs, protocols, servers')} className="ps-9" aria-label={t('Search configs')} />
          </div>
          <Segmented ariaLabel={t('State')} value={state} onChange={setState} options={[{ value: 'all', label: t('All') }, { value: 'ok', label: t('Working') }, { value: 'bad', label: t('Failing') }]} />
          <Select aria-label={t('Sort')} value={sort} onChange={(e) => setSort(e.target.value as typeof sort)} className="w-40 ms-auto">
            <option value="status">{t('Problems first')}</option>
            <option value="fastest">{t('Fastest first')}</option>
            <option value="name">{t('Name')}</option>
          </Select>
        </div>
      )}

      {!loaded ? (
        <div className="grid md:grid-cols-2 2xl:grid-cols-3 gap-4">{Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-80 rounded-[18px]" />)}</div>
      ) : all.length === 0 && subs.length === 0 ? (
        <Card>
          <Empty icon={<Rocket className="size-6" />} title={t('Watch your V2Ray configs')}
            body={t('Give Neo Uptime a subscription link or a vless://, vmess://, trojan://, ss:// or hysteria2:// config. Each one is connected through a real Xray core and you are told when it stops working — tested from here or from a server inside the country.')}
            action={canEdit(role) && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => nav('/add?type=xray')}>{t('Add subscription or config')}</Button>} />
        </Card>
      ) : (
        <div className="space-y-8">
          {subs.map((s) => (
            <SubSection key={s.id} s={s} list={filtered.filter((m) => m.config.subscriptionId === s.id)} onEdit={() => setEdit(s)} onChoose={() => setChoose(s)} />
          ))}
          {loose.length > 0 && (
            <section className="space-y-3">
              {subs.length > 0 && <h2 className="font-display text-xl font-semibold px-1">{t('Single configs')}</h2>}
              <div className="grid md:grid-cols-2 2xl:grid-cols-3 gap-4">{loose.map((m) => <ConfigCard key={m.id} m={m} />)}</div>
            </section>
          )}
        </div>
      )}
      <SubDialog sub={edit} onClose={() => setEdit(null)} />
      <ChooseDialog sub={choose} onClose={() => setChoose(null)} have={new Set(all.filter((m) => m.config.subscriptionId === choose?.id).map((m) => m.config.configName ?? ''))} />
    </div>
  )
}
