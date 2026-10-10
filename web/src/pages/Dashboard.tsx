import { useMemo, useState } from 'react'
import { Link, Outlet, useNavigate, useParams } from 'react-router-dom'
import { ChevronRight, CircleCheck, ListFilter, Pause, Play, Search, SquareCheckBig, Trash2, X } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { clsx, pct, statusKey, uptimeTone } from '../lib/format'
import type { MonitorView } from '../lib/types'
import { Button, Checkbox, IconButton, Menu, Skeleton, TagChip, confirm, toast } from '../components/ui'
import { HeartbeatBar } from '../components/charts'
import { TypeIcon } from '../components/meta'

type Filter = 'all' | 'up' | 'down' | 'pending' | 'maint' | 'paused'
const rank: Record<string, number> = { down: 0, pending: 1, maint: 2, up: 3, empty: 4, paused: 5 }

export function monitorState(m: MonitorView) {
  if (!m.active) return 'paused'
  if (m.maintenance) return 'maint'
  return statusKey(m.last?.status)
}

function UptimeBadge({ ratio, state }: { ratio: number; state: string }) {
  const tone = state === 'paused' ? 'paused' : state === 'maint' ? 'maint' : ratio < 0 ? state : uptimeTone(ratio)
  const cls: Record<string, string> = {
    up: 'bg-up/14 text-up', down: 'bg-down/14 text-down', pending: 'bg-pending/14 text-pending',
    maint: 'bg-maint/14 text-maint', paused: 'bg-surface-3 text-muted', empty: 'bg-surface-3 text-muted',
  }
  return (
    <span className={clsx('inline-flex items-center justify-center h-6 min-w-[58px] px-2 rounded-lg text-[12px] font-semibold tnum', cls[tone])}>
      {state === 'paused' ? 'Paused' : ratio < 0 ? '—' : pct(ratio, ratio >= 0.9999 ? 0 : 1)}
    </span>
  )
}

function Row({ m, depth, selected, selecting, checked, onCheck, childCount, collapsed, onToggle }: {
  m: MonitorView; depth: number; selected: boolean; selecting: boolean; checked: boolean; onCheck: (v: boolean) => void
  childCount?: number; collapsed?: boolean; onToggle?: () => void
}) {
  const beats = useApp((s) => s.beats[m.id]) ?? []
  const state = monitorState(m)
  return (
    <div
      className={clsx(
        'group relative flex items-center gap-3 rounded-2xl px-3 py-2.5 transition-colors',
        selected ? 'bg-surface-3 ring-1 ring-line-strong' : 'hover:bg-surface-2',
      )}
      style={{ paddingLeft: 12 + depth * 18 }}
    >
      {selected && <span className="absolute left-0 top-3 bottom-3 w-[3px] rounded-full bg-accent" aria-hidden />}
      {selecting && <Checkbox checked={checked} onChange={onCheck} label={<span className="sr-only">Select {m.name}</span>} />}
      {childCount !== undefined ? (
        <button onClick={onToggle} aria-label={collapsed ? `Expand ${m.name}` : `Collapse ${m.name}`} aria-expanded={!collapsed} className="grid place-items-center size-6 -mx-1 rounded-md text-muted hover:text-fg hover:bg-surface-3">
          <ChevronRight className={clsx('size-4 transition-transform', !collapsed && 'rotate-90')} />
        </button>
      ) : null}
      <UptimeBadge ratio={m.uptime24} state={state} />
      <Link to={`/dashboard/${m.id}`} className="min-w-0 flex-1 after:absolute after:inset-0 after:content-[''] focus-visible:outline-none">
        <div className="flex items-center gap-1.5 min-w-0">
          <TypeIcon type={m.type} className="size-3.5 text-subtle shrink-0" />
          <span className="font-medium text-[14px] truncate">{m.name}</span>
          {childCount !== undefined && <span className="text-[11.5px] text-subtle tnum">({childCount})</span>}
        </div>
        {m.tags.length > 0 && (
          <div className="mt-1 flex flex-wrap gap-1">
            {m.tags.slice(0, 3).map((t) => <TagChip key={`${t.tagId}${t.value}`} {...t} />)}
          </div>
        )}
      </Link>
      <div className="hidden sm:block w-[30%] max-w-[136px] shrink-0">
        <HeartbeatBar beats={beats} height={20} barWidth={4} gap={2} maxBeats={30} />
      </div>
    </div>
  )
}

export function MonitorList({ selectedId }: { selectedId?: number }) {
  const monitors = useApp((s) => s.monitors)
  const loaded = useApp((s) => s.loaded)
  const tags = useApp((s) => s.tags)
  const role = useApp((s) => s.role)
  const [q, setQ] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [tagFilter, setTagFilter] = useState<number | null>(null)
  const [selecting, setSelecting] = useState(false)
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [collapsed, setCollapsed] = useState<Set<number>>(() => {
    try {
      return new Set(JSON.parse(localStorage.getItem('neo-collapsed') ?? '[]'))
    } catch {
      return new Set()
    }
  })

  const toggleCollapse = (id: number) => {
    const n = new Set(collapsed)
    if (n.has(id)) n.delete(id)
    else n.add(id)
    setCollapsed(n)
    try {
      localStorage.setItem('neo-collapsed', JSON.stringify([...n]))
    } catch {
      /* not persisted */
    }
  }

  const all = Object.values(monitors)
  const matches = (m: MonitorView) => {
    const s = q.trim().toLowerCase()
    if (s && !m.name.toLowerCase().includes(s) && !m.target.toLowerCase().includes(s) && !m.tags.some((t) => t.name.toLowerCase().includes(s))) return false
    if (filter !== 'all' && monitorState(m) !== filter) return false
    if (tagFilter && !m.tags.some((t) => t.tagId === tagFilter)) return false
    return true
  }
  const sorter = (a: MonitorView, b: MonitorView) => rank[monitorState(a)] - rank[monitorState(b)] || a.name.localeCompare(b.name)

  const tree = useMemo(() => {
    const byParent = new Map<number, MonitorView[]>()
    const ids = new Set(all.map((m) => m.id))
    for (const m of all) {
      const p = m.parentId && ids.has(m.parentId) ? m.parentId : 0
      byParent.set(p, [...(byParent.get(p) ?? []), m])
    }
    const filtering = q.trim() !== '' || filter !== 'all' || tagFilter !== null
    const out: { m: MonitorView; depth: number; childCount?: number }[] = []
    const walk = (parent: number, depth: number) => {
      for (const m of (byParent.get(parent) ?? []).sort(sorter)) {
        const kids = byParent.get(m.id)
        if (filtering) {
          if (matches(m)) out.push({ m, depth: 0 })
          if (kids) walk(m.id, 0)
          continue
        }
        out.push({ m, depth, childCount: kids || m.type === 'group' ? kids?.length ?? 0 : undefined })
        if (kids && !collapsed.has(m.id)) walk(m.id, depth + 1)
      }
    }
    walk(0, 0)
    return out
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [monitors, q, filter, tagFilter, collapsed])

  const bulk = async (action: 'pause' | 'resume' | 'delete') => {
    const ids = [...picked]
    if (!ids.length) return
    if (action === 'delete' && !(await confirm({ title: `Delete ${ids.length} monitor${ids.length > 1 ? 's' : ''}?`, body: 'Their history is deleted too. This cannot be undone.', confirmLabel: 'Delete', danger: true }))) return
    try {
      await api.post('/api/monitors/bulk', { action, ids })
      toast.success(`${ids.length} monitor${ids.length > 1 ? 's' : ''} ${action === 'delete' ? 'deleted' : action === 'pause' ? 'paused' : 'resumed'}`)
      setPicked(new Set())
      setSelecting(false)
      await useApp.getState().loadDashboard()
    } catch (e) {
      toast.error('Bulk action failed', (e as Error).message)
    }
  }

  const filterLabels: Record<Filter, string> = { all: 'All', up: 'Up', down: 'Down', pending: 'Pending', maint: 'Maintenance', paused: 'Paused' }

  return (
    <div className="flex flex-col h-full">
      <div className="p-3 space-y-2.5 border-b border-line">
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-subtle" aria-hidden />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search monitors, hosts, tags…"
            aria-label="Search monitors"
            className="w-full h-10 pl-9 pr-9 rounded-xl bg-surface-2 border border-line text-[15px] sm:text-sm placeholder:text-subtle focus:outline-none focus:border-accent focus:ring-4 focus:ring-accent/15"
          />
          {q && <button aria-label="Clear search" onClick={() => setQ('')} className="absolute right-2.5 top-1/2 -translate-y-1/2 text-subtle hover:text-fg"><X className="size-4" /></button>}
        </div>
        <div className="flex items-center gap-2">
          <Menu
            align="start"
            trigger={(p) => (
              <Button {...p} size="sm" variant={filter !== 'all' || tagFilter ? 'soft' : 'secondary'} icon={<ListFilter className="size-3.5" />}>
                {filter === 'all' ? 'Status' : filterLabels[filter]}
                {tagFilter ? ' · tag' : ''}
              </Button>
            )}
            items={[
              ...(Object.keys(filterLabels) as Filter[]).map((f) => ({ label: filterLabels[f], icon: filter === f ? <CircleCheck /> : <span className="size-4" />, onClick: () => setFilter(f) })),
              ...(tags.length ? [{ divider: true, label: '' }] : []),
              ...tags.map((t) => ({ label: `Tag: ${t.name}`, icon: tagFilter === t.id ? <CircleCheck /> : <span className="size-4 rounded-full inline-block" style={{ background: t.color }} />, onClick: () => setTagFilter(tagFilter === t.id ? null : t.id) })),
            ]}
          />
          {(filter !== 'all' || tagFilter) && (
            <button className="text-[12.5px] text-muted hover:text-fg" onClick={() => { setFilter('all'); setTagFilter(null) }}>Reset</button>
          )}
          <div className="ml-auto flex items-center gap-1">
            {canEdit(role) && (
              <IconButton size="sm" label={selecting ? 'Stop selecting' : 'Select monitors'} onClick={() => { setSelecting(!selecting); setPicked(new Set()) }} className={selecting ? 'text-accent-ink bg-accent-soft' : ''}>
                <SquareCheckBig className="size-4" />
              </IconButton>
            )}
          </div>
        </div>
        {selecting && (
          <div className="flex items-center gap-2 rounded-xl bg-surface-2 border border-line p-2 fade-up">
            <Checkbox checked={picked.size > 0 && picked.size === tree.length} onChange={(v) => setPicked(v ? new Set(tree.map((x) => x.m.id)) : new Set())} label={<span className="text-[12.5px] text-muted tnum">{picked.size} selected</span>} />
            <div className="ml-auto flex gap-1">
              <IconButton size="sm" label="Pause selected" onClick={() => bulk('pause')} disabled={!picked.size}><Pause className="size-4" /></IconButton>
              <IconButton size="sm" label="Resume selected" onClick={() => bulk('resume')} disabled={!picked.size}><Play className="size-4" /></IconButton>
              <IconButton size="sm" label="Delete selected" onClick={() => bulk('delete')} disabled={!picked.size} className="hover:text-down"><Trash2 className="size-4" /></IconButton>
            </div>
          </div>
        )}
      </div>
      <div className="flex-1 overflow-y-auto p-2 space-y-0.5">
        {!loaded ? (
          Array.from({ length: 7 }).map((_, i) => <Skeleton key={i} className="h-14 rounded-2xl" />)
        ) : tree.length === 0 ? (
          <p className="px-4 py-10 text-center text-sm text-muted">{all.length ? 'Nothing matches these filters.' : 'No monitors yet.'}</p>
        ) : (
          tree.map(({ m, depth, childCount }) => (
            <Row
              key={m.id}
              m={m}
              depth={depth}
              selected={m.id === selectedId}
              selecting={selecting}
              checked={picked.has(m.id)}
              onCheck={(v) => {
                const n = new Set(picked)
                if (v) n.add(m.id)
                else n.delete(m.id)
                setPicked(n)
              }}
              childCount={childCount}
              collapsed={collapsed.has(m.id)}
              onToggle={() => toggleCollapse(m.id)}
            />
          ))
        )}
      </div>
    </div>
  )
}

export default function Dashboard() {
  const { id } = useParams()
  const nav = useNavigate()
  const selected = id ? Number(id) : undefined
  return (
    <div className="lg:h-dvh flex">
      <section
        aria-label="Monitors"
        className={clsx(
          'w-full md:w-[340px] xl:w-[380px] shrink-0 border-r border-line bg-surface/40 md:sticky md:top-14 lg:top-0 md:h-[calc(100dvh-3.5rem)] lg:h-dvh',
          selected ? 'hidden md:flex md:flex-col' : 'flex flex-col h-[calc(100dvh-3.5rem)]',
        )}
      >
        <MonitorList selectedId={selected} />
      </section>
      <div className={clsx('flex-1 min-w-0 lg:overflow-y-auto', !selected && 'hidden md:block')}>
        {selected && (
          <button onClick={() => nav('/dashboard')} className="md:hidden flex items-center gap-1 px-4 pt-4 text-sm text-muted hover:text-fg">
            <ChevronRight className="size-4 rotate-180" /> All monitors
          </button>
        )}
        <Outlet />
      </div>
    </div>
  )
}
