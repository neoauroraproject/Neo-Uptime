import { useRef, useState } from 'react'
import { ArrowDown, ArrowUp, Cable, Eye, EyeOff, GripVertical, Plus, Rocket, Server, Trash2, X } from 'lucide-react'
import { useApp } from '../lib/store'
import { clsx } from '../lib/format'
import { useT } from '../lib/i18n'
import type { MonitorView, StatusPage } from '../lib/types'
import { Badge, Button, IconButton, Input, Select, toast } from '../components/ui'
import { SourceBadge, TypeIcon, tunnelSources, typeInfo } from '../components/meta'
import { Flag } from '../components/flag'
import { monitorCountry } from './Dashboard'

type Groups = StatusPage['groups']

/** What a monitor is, so tunnels of different programs never get mixed up. */
type Kind = 'backpack' | 'kariz' | 'neotunnel' | 'xray' | 'agent' | 'other'
const kinds: Kind[] = ['backpack', 'neotunnel', 'kariz', 'xray', 'agent', 'other']
const kindTitle: Record<Kind, string> = {
  backpack: 'BackPack tunnels', neotunnel: 'NeoTunnel tunnels', kariz: 'Kariz tunnels',
  xray: 'V2Ray configs', agent: 'Servers', other: 'Other monitors',
}

export function kindOf(m: MonitorView): Kind {
  if (m.type === 'backpack-tunnel') return 'backpack'
  if (m.type === 'agent-tunnel') return (m.config.source ?? 'backpack') as Kind
  if (m.type === 'xray') return 'xray'
  if (m.type === 'agent') return 'agent'
  return 'other'
}

function KindBadge({ m }: { m: MonitorView }) {
  const t = useT()
  const k = kindOf(m)
  if (tunnelSources.includes(k as never)) return <SourceBadge source={k} />
  if (k === 'xray') return <Badge tone="accent">V2Ray</Badge>
  if (k === 'agent') return <Badge>{t('Server')}</Badge>
  return <Badge tone="outline">{t(typeInfo[m.type]?.label ?? m.type)}</Badge>
}

/** The server or panel a monitor is watched through, by name. */
function useViaName() {
  const agents = useApp((s) => s.agents)
  const panels = useApp((s) => s.panels)
  return (m: MonitorView) => {
    if (m.type === 'agent') return ''
    if (m.config.panelId) return panels.find((p) => p.id === m.config.panelId)?.name ?? ''
    if (m.config.agentId) return agents.find((a) => a.id === m.config.agentId)?.name ?? ''
    return ''
  }
}

type Spot = { g: number; i: number }

/**
 * The Monitors tab of the status page editor: groups of monitors, each
 * marked with what it is (BackPack, NeoTunnel, a V2Ray config…) and where
 * it is watched from. Rows are reordered by dragging their handle — with a
 * mouse or a finger, also into another group — or with the arrows.
 */
export function MonitorsTab({ groups, setGroups, monitors }: { groups: Groups; setGroups: (g: Groups) => void; monitors: Record<number, MonitorView> }) {
  const t = useT()
  const via = useViaName()
  const [drag, setDrag] = useState<{ from: Spot; over: Spot | null } | null>(null)
  const dragRef = useRef(drag)
  dragRef.current = drag

  const used = new Set(groups.flatMap((g) => g.monitors.map((m) => m.monitorId)))
  const all = Object.values(monitors).sort((a, b) => a.name.localeCompare(b.name))
  const free = all.filter((m) => !used.has(m.id) && m.type !== 'group')

  const swap = <T,>(arr: T[], i: number, d: number) => {
    const a = [...arr]
    const j = i + d
    if (j < 0 || j >= a.length) return a
    ;[a[i], a[j]] = [a[j], a[i]]
    return a
  }
  const editGroup = (gi: number, f: (g: Groups[number]) => Groups[number]) => setGroups(groups.map((x, i) => (i === gi ? f(x) : x)))

  /** Moves a row to a spot (before row i of group g; i = length is the end). */
  const moveTo = (from: Spot, to: Spot) => {
    const next = groups.map((g) => ({ ...g, monitors: [...g.monitors] }))
    const [item] = next[from.g].monitors.splice(from.i, 1)
    let i = to.i
    if (from.g === to.g && from.i < to.i) i--
    next[to.g].monitors.splice(i, 0, item)
    setGroups(next)
  }

  // Pointer dragging, so it works on phones too (HTML drag and drop does not).
  const spotAt = (x: number, y: number): Spot | null => {
    const el = document.elementFromPoint(x, y) as HTMLElement | null
    const row = el?.closest<HTMLElement>('[data-sp-row]')
    if (row) {
      const g = Number(row.dataset.g), i = Number(row.dataset.i)
      const r = row.getBoundingClientRect()
      return { g, i: y > r.top + r.height / 2 ? i + 1 : i }
    }
    const box = el?.closest<HTMLElement>('[data-sp-group]')
    if (box) return { g: Number(box.dataset.g), i: groups[Number(box.dataset.g)].monitors.length }
    return null
  }
  const onDown = (e: React.PointerEvent, from: Spot) => {
    e.preventDefault()
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    setDrag({ from, over: null })
  }
  const onMove = (e: React.PointerEvent) => {
    if (!dragRef.current) return
    const over = spotAt(e.clientX, e.clientY)
    setDrag((d) => (d ? { ...d, over } : d))
  }
  const onUp = () => {
    const d = dragRef.current
    setDrag(null)
    if (!d?.over) return
    const { from, over } = d
    if (from.g === over.g && (over.i === from.i || over.i === from.i + 1)) return
    moveTo(from, over)
  }

  const addAll = (want: (m: MonitorView) => boolean) => {
    const list = free.filter(want)
    if (!list.length) return toast.info(t('They are all on this page already'))
    // One group per program, so BackPack and NeoTunnel tunnels stay apart.
    const parts = kinds.map((k) => [k, list.filter((m) => kindOf(m) === k)] as const).filter(([, l]) => l.length)
    const made = parts.map(([k, l]) => ({ name: t(kindTitle[k]), monitors: l.map((m) => ({ monitorId: m.id, showUrl: false })) }))
    // A new page starts with one empty, unnamed group: fill that one first.
    const blank = groups.findIndex((g) => !g.name.trim() && g.monitors.length === 0)
    if (blank >= 0) setGroups([...groups.slice(0, blank), ...made, ...groups.slice(blank + 1)])
    else setGroups([...groups, ...made])
  }
  const has = (k: Kind) => free.some((m) => kindOf(m) === k)
  const anyTunnel = free.some((m) => tunnelSources.includes(kindOf(m) as never))

  return (
    <>
      {groups.map((g, gi) => (
        <div key={gi} data-sp-group data-g={gi} className={clsx('rounded-2xl border bg-surface-2/40 transition-colors', drag?.over?.g === gi ? 'border-accent/50' : 'border-line')}>
          <div className="flex items-center gap-2 p-3 border-b border-line">
            <Input aria-label={t('Group name')} dir="auto" value={g.name} placeholder={t('Group name (optional)')} onChange={(e) => editGroup(gi, (x) => ({ ...x, name: e.target.value }))} className="h-9 font-medium" />
            <IconButton size="sm" label={t('Move group up')} disabled={gi === 0} onClick={() => setGroups(swap(groups, gi, -1))}><ArrowUp className="size-4" /></IconButton>
            <IconButton size="sm" label={t('Move group down')} disabled={gi === groups.length - 1} onClick={() => setGroups(swap(groups, gi, 1))}><ArrowDown className="size-4" /></IconButton>
            <IconButton size="sm" label={t('Remove group')} onClick={() => setGroups(groups.filter((_, i) => i !== gi))}><Trash2 className="size-4" /></IconButton>
          </div>
          <div className="p-2 space-y-1">
            {g.monitors.length === 0 && <p className="px-2 py-3 text-[13px] text-muted">{drag ? t('Drop here') : t('Empty group — add monitors below.')}</p>}
            {g.monitors.map((gm, mi) => {
              const m = monitors[gm.monitorId]
              const cc = m ? monitorCountry(m) : ''
              const where = m ? via(m) : ''
              const dragging = drag?.from.g === gi && drag.from.i === mi
              const lineBefore = drag?.over?.g === gi && drag.over.i === mi
              const lineAfter = drag?.over?.g === gi && drag.over.i === g.monitors.length && mi === g.monitors.length - 1
              return (
                <div key={gm.monitorId} data-sp-row data-g={gi} data-i={mi}
                  className={clsx('relative flex items-center gap-2 rounded-xl px-1.5 py-1.5 hover:bg-surface-2', dragging && 'opacity-40')}>
                  {lineBefore && <span className="absolute inset-x-2 -top-[3px] h-[3px] rounded-full bg-accent" aria-hidden />}
                  {lineAfter && <span className="absolute inset-x-2 -bottom-[3px] h-[3px] rounded-full bg-accent" aria-hidden />}
                  <button type="button" aria-label={t('Drag to reorder')} title={t('Drag to reorder')}
                    onPointerDown={(e) => onDown(e, { g: gi, i: mi })} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={() => setDrag(null)}
                    className="grid place-items-center size-7 shrink-0 rounded-lg text-subtle hover:text-fg hover:bg-surface cursor-grab active:cursor-grabbing touch-none">
                    <GripVertical className="size-4" />
                  </button>
                  {cc ? <Flag cc={cc} size={16} /> : m && <TypeIcon type={m.type} className="size-3.5 text-subtle" />}
                  <div className="min-w-0 me-auto">
                    <div className="flex items-center gap-1.5 min-w-0">
                      <span className="truncate text-sm" dir="auto">{m?.name ?? t('#{id} (deleted)', { id: gm.monitorId })}</span>
                      {m && <span className="max-sm:hidden shrink-0"><KindBadge m={m} /></span>}
                    </div>
                    {(where || m) && (
                      <p className="flex items-center gap-1.5 text-[11.5px] text-subtle min-w-0" dir="auto">
                        {m && <span className="sm:hidden shrink-0"><KindBadge m={m} /></span>}
                        <span className="truncate">{where}</span>
                      </p>
                    )}
                  </div>
                  <IconButton size="sm" label={gm.showUrl ? t('Hide address') : t('Show address')} onClick={() => editGroup(gi, (x) => ({ ...x, monitors: x.monitors.map((y, j) => (j === mi ? { ...y, showUrl: !y.showUrl } : y)) }))} className={gm.showUrl ? 'text-accent-ink' : ''}>
                    {gm.showUrl ? <Eye className="size-4" /> : <EyeOff className="size-4" />}
                  </IconButton>
                  <IconButton size="sm" label={t('Move up')} disabled={mi === 0} className="max-sm:hidden" onClick={() => editGroup(gi, (x) => ({ ...x, monitors: swap(x.monitors, mi, -1) }))}><ArrowUp className="size-4" /></IconButton>
                  <IconButton size="sm" label={t('Move down')} disabled={mi === g.monitors.length - 1} className="max-sm:hidden" onClick={() => editGroup(gi, (x) => ({ ...x, monitors: swap(x.monitors, mi, 1) }))}><ArrowDown className="size-4" /></IconButton>
                  <IconButton size="sm" label={t('Remove from page')} onClick={() => editGroup(gi, (x) => ({ ...x, monitors: x.monitors.filter((_, j) => j !== mi) }))}><X className="size-4" /></IconButton>
                </div>
              )
            })}
            <Select aria-label={t('Add a monitor to this group')} value="" onChange={(e) => {
              const mid = Number(e.target.value)
              if (mid) editGroup(gi, (x) => ({ ...x, monitors: [...x.monitors, { monitorId: mid, showUrl: false }] }))
            }}>
              <option value="">{t('+ Add a monitor…')}</option>
              {kinds.map((k) => {
                const list = free.filter((m) => kindOf(m) === k)
                if (!list.length) return null
                return (
                  <optgroup key={k} label={t(kindTitle[k])}>
                    {list.map((m) => {
                      const where = via(m)
                      return <option key={m.id} value={m.id}>{m.name}{where ? ` — ${where}` : ''}</option>
                    })}
                  </optgroup>
                )
              })}
            </Select>
          </div>
        </div>
      ))}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => setGroups([...groups, { name: '', monitors: [] }])}>{t('Add group')}</Button>
        {anyTunnel && <Button size="sm" variant="soft" icon={<Cable className="size-3.5" />} onClick={() => addAll((m) => tunnelSources.includes(kindOf(m) as never))}>{t('Add all tunnels')}</Button>}
        {has('xray') && <Button size="sm" variant="soft" icon={<Rocket className="size-3.5" />} onClick={() => addAll((m) => kindOf(m) === 'xray')}>{t('Add all configs')}</Button>}
        {has('agent') && <Button size="sm" variant="soft" icon={<Server className="size-3.5" />} onClick={() => addAll((m) => kindOf(m) === 'agent')}>{t('Add all servers')}</Button>}
      </div>
      <p className="text-[12px] text-subtle">{t('Drag a row by its handle to change the order, also into another group.')}</p>
    </>
  )
}
