import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ExternalLink, Pencil, Plus, Rss } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import type { StatusPage, Theme } from '../lib/types'
import { Alert, Badge, Button, Card, Empty, Field, Input, Modal, Skeleton, toast } from '../components/ui'

export const themeMeta: Record<Theme, { name: string; desc: string; swatch: string[] }> = {
  aurora: { name: 'Aurora', desc: 'Dark with a green glow — the BackPack look', swatch: ['#050806', '#0b100c', '#22c55e'] },
  clarity: { name: 'Clarity', desc: 'Bright, airy and familiar', swatch: ['#f5f6f8', '#ffffff', '#4f46e5'] },
  lime: { name: 'Lime', desc: 'Bento cards with bold numbers', swatch: ['#efefec', '#ffffff', '#d4f36b'] },
  noir: { name: 'Noir', desc: 'Black bento with lime accents', swatch: ['#0a0a0a', '#141414', '#d4f36b'] },
}

export function ThemeSwatch({ theme, className }: { theme: Theme; className?: string }) {
  const [bg, card, accent] = themeMeta[theme].swatch
  return (
    <div className={className} style={{ background: bg }} aria-hidden>
      <div className="h-full w-full p-2.5 flex flex-col gap-1.5">
        <div className="h-3 w-1/2 rounded-full" style={{ background: accent }} />
        <div className="flex-1 rounded-lg p-1.5 flex items-end gap-[2px]" style={{ background: card }}>
          {Array.from({ length: 18 }).map((_, i) => (
            <span key={i} className="flex-1 rounded-full" style={{ height: `${40 + ((i * 37) % 60)}%`, background: i === 12 ? '#ef4444' : theme === 'lime' ? '#1d1d1d' : accent, opacity: 0.9 }} />
          ))}
        </div>
      </div>
    </div>
  )
}

export function slugify(s: string) {
  return s.toLowerCase().normalize('NFKD').replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 63)
}

export default function StatusPages() {
  const nav = useNavigate()
  const role = useApp((s) => s.role)
  const [pages, setPages] = useState<StatusPage[] | null>(null)
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [slug, setSlug] = useState('')
  const [slugTouched, setSlugTouched] = useState(false)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.get<StatusPage[]>('/api/status-pages').then(setPages).catch(() => setPages([]))
  }, [])

  const create = async () => {
    setBusy(true)
    setErr('')
    try {
      const p = await api.post<StatusPage>('/api/status-pages', { title, slug, theme: 'aurora', layout: 'list', barMode: 'heartbeat', published: true, showPoweredBy: true, showCharts: true, refreshInterval: 60, groups: [{ name: 'Services', monitors: [] }] })
      toast.success('Status page created')
      nav(`/status-pages/${p.id}`)
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="p-4 sm:p-6 lg:p-8 space-y-6 max-w-[1400px]">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="font-display text-[28px] font-semibold tracking-tight">Status pages</h1>
          <p className="text-sm text-muted mt-1">Public pages for your users, in four themes. Each one picks which monitors it shows.</p>
        </div>
        {canEdit(role) && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => { setOpen(true); setTitle(''); setSlug(''); setSlugTouched(false); setErr('') }}>New status page</Button>}
      </div>

      {pages === null ? (
        <div className="grid sm:grid-cols-2 xl:grid-cols-3 gap-4">{Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-64 rounded-[18px]" />)}</div>
      ) : pages.length === 0 ? (
        <Card><Empty icon={<Rss className="size-6" />} title="No status pages yet" body="Create one to show your users what is up — with your logo, your monitors and incident notes." action={canEdit(role) && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setOpen(true)}>New status page</Button>} /></Card>
      ) : (
        <div className="grid sm:grid-cols-2 xl:grid-cols-3 gap-4">
          {pages.map((p) => (
            <Card key={p.id} className="overflow-hidden flex flex-col">
              <ThemeSwatch theme={p.theme} className="h-32 border-b border-line" />
              <div className="p-5 flex-1 flex flex-col gap-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <p className="font-display text-lg font-semibold truncate">{p.title}</p>
                    <p className="text-[12.5px] text-muted font-mono truncate">/status/{p.slug}</p>
                  </div>
                  {p.published ? <Badge tone="up">Live</Badge> : <Badge>Draft</Badge>}
                </div>
                <div className="flex flex-wrap gap-1.5">
                  <Badge tone="accent">{themeMeta[p.theme].name}</Badge>
                  <Badge>{p.layout === 'cards' ? 'Cards' : 'List'}</Badge>
                  <Badge>{p.barMode === 'days' ? '90 days' : 'Heartbeats'}</Badge>
                </div>
                <div className="mt-auto flex gap-2 pt-2">
                  <a href={`/status/${p.slug}`} target="_blank" rel="noreferrer" className="flex-1"><Button className="w-full" size="sm" icon={<ExternalLink className="size-3.5" />}>Open</Button></a>
                  {canEdit(role) && <Link to={`/status-pages/${p.id}`} className="flex-1"><Button className="w-full" size="sm" variant="soft" icon={<Pencil className="size-3.5" />}>Edit</Button></Link>}
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      <Modal open={open} onClose={() => setOpen(false)} title="New status page" size="sm"
        footer={<><Button variant="ghost" onClick={() => setOpen(false)}>Cancel</Button><Button variant="primary" loading={busy} disabled={!title || !slug} onClick={create}>Create</Button></>}>
        <div className="space-y-4">
          {err && <Alert tone="danger">{err}</Alert>}
          <Field label="Title" htmlFor="spt"><Input id="spt" data-autofocus value={title} onChange={(e) => { setTitle(e.target.value); if (!slugTouched) setSlug(slugify(e.target.value)) }} placeholder="Acme status" /></Field>
          <Field label="Address" htmlFor="sps" help={`${window.location.origin}/status/${slug || '…'}`}>
            <Input id="sps" className="font-mono" value={slug} onChange={(e) => { setSlugTouched(true); setSlug(slugify(e.target.value)) }} placeholder="acme" />
          </Field>
        </div>
      </Modal>
    </div>
  )
}
