import { useEffect, useMemo, useState } from 'react'
import { CalendarClock, Ellipsis, Pause, Pencil, Play, Plus, Search, Trash2, Wrench } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { clsx, fmtWeekdayTime } from '../lib/format'
import type { Maintenance, StatusPage } from '../lib/types'
import { Alert, Badge, Button, Card, Checkbox, Empty, Field, IconButton, Input, Menu, Modal, Segmented, Select, Skeleton, Textarea, confirm, toast } from '../components/ui'

const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

function toLocalInput(sec: number) {
  if (!sec) return ''
  const d = new Date(sec * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}
const fromLocalInput = (s: string) => (s ? Math.floor(new Date(s).getTime() / 1000) : 0)

function describe(m: Maintenance) {
  switch (m.strategy) {
    case 'manual': return 'Manual — active until you end it'
    case 'single': return `${fmtWeekdayTime(m.startAt * 1000)} → ${fmtWeekdayTime(m.endAt * 1000)}`
    case 'weekly': return `Every ${m.weekdays.map((d) => days[d]).join(', ')} · ${m.timeFrom}–${m.timeTo} ${m.timezone}`
    case 'monthly': return `Monthly on day ${m.daysOfMonth.map((d) => (d === -1 ? 'last' : d)).join(', ')} · ${m.timeFrom}–${m.timeTo} ${m.timezone}`
  }
}

const statusBadge = (s?: string) =>
  s === 'under-maintenance' ? <Badge tone="maint">In progress</Badge> : s === 'scheduled' ? <Badge tone="accent">Scheduled</Badge> : s === 'ended' ? <Badge>Ended</Badge> : <Badge>Paused</Badge>

function MaintDialog({ open, onClose, initial }: { open: boolean; onClose: () => void; initial?: Maintenance }) {
  const monitors = useApp((s) => s.monitors)
  const [pages, setPages] = useState<StatusPage[]>([])
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone
  const fresh = (): Maintenance => ({ id: 0, title: '', description: '', strategy: 'single', active: true, startAt: Math.floor(Date.now() / 1000 / 3600 + 1) * 3600, endAt: Math.floor(Date.now() / 1000 / 3600 + 3) * 3600, weekdays: [], daysOfMonth: [], timeFrom: '02:00', timeTo: '04:00', timezone: tz, monitorIds: [], statusPageIds: [] })
  const [f, setF] = useState<Maintenance>(fresh)
  const [q, setQ] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const zones = useMemo(() => {
    try {
      return (Intl as unknown as { supportedValuesOf: (k: string) => string[] }).supportedValuesOf('timeZone')
    } catch {
      return ['UTC', tz]
    }
  }, [tz])

  useEffect(() => {
    if (!open) return
    setF(initial ? { ...initial } : fresh())
    setErr('')
    setQ('')
    api.get<StatusPage[]>('/api/status-pages').then(setPages).catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, initial])

  const list = Object.values(monitors).filter((m) => m.name.toLowerCase().includes(q.toLowerCase())).sort((a, b) => a.name.localeCompare(b.name))
  const save = async () => {
    setBusy(true)
    setErr('')
    try {
      if (f.id) await api.put(`/api/maintenance/${f.id}`, f)
      else await api.post('/api/maintenance', f)
      toast.success(f.id ? 'Maintenance saved' : 'Maintenance scheduled')
      onClose()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const toggle = (arr: number[], v: number) => (arr.includes(v) ? arr.filter((x) => x !== v) : [...arr, v])

  return (
    <Modal open={open} onClose={onClose} size="lg" title={f.id ? 'Edit maintenance' : 'Schedule maintenance'} description="Affected monitors report maintenance instead of down, and stay quiet."
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button variant="primary" loading={busy} disabled={!f.title} onClick={save}>{f.id ? 'Save' : 'Schedule'}</Button></>}>
      <div className="space-y-5">
        {err && <Alert tone="danger">{err}</Alert>}
        <Field label="Title" htmlFor="mt"><Input id="mt" data-autofocus value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} placeholder="Kernel upgrade on edge servers" /></Field>
        <Field label="Description" htmlFor="md" help="Shown on linked status pages."><Textarea id="md" className="min-h-16" value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
        <Field label="When">
          <Segmented ariaLabel="Strategy" value={f.strategy} onChange={(v) => setF({ ...f, strategy: v })} options={[{ value: 'single', label: 'One-time' }, { value: 'weekly', label: 'Weekly' }, { value: 'monthly', label: 'Monthly' }, { value: 'manual', label: 'Manual' }]} />
        </Field>
        {f.strategy === 'single' && (
          <div className="grid sm:grid-cols-2 gap-4">
            <Field label="Starts" htmlFor="ms"><Input id="ms" type="datetime-local" value={toLocalInput(f.startAt)} onChange={(e) => setF({ ...f, startAt: fromLocalInput(e.target.value) })} /></Field>
            <Field label="Ends" htmlFor="me"><Input id="me" type="datetime-local" value={toLocalInput(f.endAt)} onChange={(e) => setF({ ...f, endAt: fromLocalInput(e.target.value) })} /></Field>
          </div>
        )}
        {(f.strategy === 'weekly' || f.strategy === 'monthly') && (
          <>
            {f.strategy === 'weekly' ? (
              <Field label="Days">
                <div className="flex flex-wrap gap-1.5">
                  {days.map((d, i) => (
                    <button key={d} type="button" aria-pressed={f.weekdays.includes(i)} onClick={() => setF({ ...f, weekdays: toggle(f.weekdays, i) })}
                      className={clsx('h-9 w-12 rounded-xl border text-[13px] font-medium transition-colors', f.weekdays.includes(i) ? 'bg-accent text-on-accent border-transparent' : 'bg-surface-2 border-line text-muted hover:text-fg')}>{d}</button>
                  ))}
                </div>
              </Field>
            ) : (
              <Field label="Days of the month">
                <div className="grid grid-cols-8 gap-1.5">
                  {[...Array.from({ length: 31 }, (_, i) => i + 1), -1].map((d) => (
                    <button key={d} type="button" aria-pressed={f.daysOfMonth.includes(d)} onClick={() => setF({ ...f, daysOfMonth: toggle(f.daysOfMonth, d) })}
                      className={clsx('h-9 rounded-lg border text-[12.5px] font-medium tnum transition-colors', f.daysOfMonth.includes(d) ? 'bg-accent text-on-accent border-transparent' : 'bg-surface-2 border-line text-muted hover:text-fg', d === -1 && 'col-span-2')}>{d === -1 ? 'Last' : d}</button>
                  ))}
                </div>
              </Field>
            )}
            <div className="grid sm:grid-cols-3 gap-4">
              <Field label="From" htmlFor="tf"><Input id="tf" type="time" value={f.timeFrom} onChange={(e) => setF({ ...f, timeFrom: e.target.value })} /></Field>
              <Field label="To" htmlFor="tt" help="Earlier than From = past midnight."><Input id="tt" type="time" value={f.timeTo} onChange={(e) => setF({ ...f, timeTo: e.target.value })} /></Field>
              <Field label="Time zone" htmlFor="tz"><Select id="tz" value={f.timezone} onChange={(e) => setF({ ...f, timezone: e.target.value })}>{zones.map((z) => <option key={z}>{z}</option>)}</Select></Field>
            </div>
          </>
        )}
        {f.strategy === 'manual' && <Alert tone="info">Starts as soon as you save and lasts until you pause or delete it.</Alert>}

        <Field label={`Affected monitors (${f.monitorIds.length})`} help="Choosing a group covers every monitor in it.">
          <div className="rounded-2xl border border-line">
            <div className="p-2 border-b border-line relative">
              <Search className="absolute left-4 top-1/2 -translate-y-1/2 size-4 text-subtle" />
              <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filter monitors" className="pl-9 h-9" aria-label="Filter monitors" />
            </div>
            <div className="max-h-52 overflow-y-auto p-2 grid sm:grid-cols-2 gap-1">
              {list.map((m) => (
                <Checkbox key={m.id} className="rounded-lg px-2 py-1.5 hover:bg-surface-2" checked={f.monitorIds.includes(m.id)} onChange={() => setF({ ...f, monitorIds: toggle(f.monitorIds, m.id) })} label={<span className="truncate">{m.name}{m.type === 'group' && <span className="text-subtle"> (group)</span>}</span>} />
              ))}
            </div>
          </div>
        </Field>
        {pages.length > 0 && (
          <Field label="Announce on status pages">
            <div className="flex flex-wrap gap-3">
              {pages.map((p) => <Checkbox key={p.id} checked={f.statusPageIds.includes(p.id)} onChange={() => setF({ ...f, statusPageIds: toggle(f.statusPageIds, p.id) })} label={p.title} />)}
            </div>
          </Field>
        )}
      </div>
    </Modal>
  )
}

export default function MaintenancePage() {
  const role = useApp((s) => s.role)
  const monitors = useApp((s) => s.monitors)
  const [list, setList] = useState<Maintenance[] | null>(null)
  const [open, setOpen] = useState(false)
  const [edit, setEdit] = useState<Maintenance | undefined>()
  const load = () => api.get<Maintenance[]>('/api/maintenance').then(setList).catch(() => setList([]))
  useEffect(() => { load() }, [])

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    try {
      await fn()
      toast.success(ok)
      load()
    } catch (e) {
      toast.error('That did not work', (e as Error).message)
    }
  }

  return (
    <div className="p-4 sm:p-6 lg:p-8 space-y-6 max-w-[1200px]">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="font-display text-[28px] font-semibold tracking-tight">Maintenance</h1>
          <p className="text-sm text-muted mt-1">Planned windows keep monitors quiet and tell your status pages what is going on.</p>
        </div>
        {canEdit(role) && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => { setEdit(undefined); setOpen(true) }}>Schedule maintenance</Button>}
      </div>
      {list === null ? <Skeleton className="h-40 rounded-[18px]" /> : list.length === 0 ? (
        <Card><Empty icon={<Wrench className="size-6" />} title="No maintenance planned" body="Schedule a window before you reboot a server or move a tunnel, so nobody gets paged for it." /></Card>
      ) : (
        <div className="space-y-3">
          {list.map((m) => (
            <Card key={m.id} className="p-5 flex flex-wrap items-start gap-4">
              <span className={clsx('grid place-items-center size-11 rounded-2xl', m.status === 'under-maintenance' ? 'bg-maint/14 text-maint' : 'bg-surface-2 text-muted')}><CalendarClock className="size-5" /></span>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2"><p className="font-display text-lg font-semibold">{m.title}</p>{statusBadge(m.status)}</div>
                <p className="text-[13px] text-muted mt-0.5">{describe(m)}</p>
                {m.status === 'scheduled' && m.nextStart ? <p className="text-[12.5px] text-accent-ink mt-1">Next: {fmtWeekdayTime(m.nextStart)}</p> : null}
                <p className="text-[12.5px] text-subtle mt-1.5 truncate">{m.monitorIds.length ? m.monitorIds.map((id) => monitors[id]?.name ?? `#${id}`).join(', ') : 'No monitors selected'}</p>
              </div>
              {canEdit(role) && (
                <Menu trigger={(t) => <IconButton {...t} label="Maintenance actions"><Ellipsis className="size-5" /></IconButton>} items={[
                  { label: 'Edit', icon: <Pencil />, onClick: () => { setEdit(m); setOpen(true) } },
                  m.active ? { label: m.strategy === 'manual' ? 'End maintenance' : 'Pause', icon: <Pause />, onClick: () => act(() => api.post(`/api/maintenance/${m.id}/pause`), 'Maintenance paused') }
                    : { label: 'Resume', icon: <Play />, onClick: () => act(() => api.post(`/api/maintenance/${m.id}/resume`), 'Maintenance resumed') },
                  { divider: true, label: '' },
                  { label: 'Delete', icon: <Trash2 />, danger: true, onClick: async () => { if (await confirm({ title: `Delete “${m.title}”?`, confirmLabel: 'Delete', danger: true })) act(() => api.del(`/api/maintenance/${m.id}`), 'Maintenance deleted') } },
                ]} />
              )}
            </Card>
          ))}
        </div>
      )}
      <MaintDialog open={open} onClose={() => { setOpen(false); load() }} initial={edit} />
    </div>
  )
}
