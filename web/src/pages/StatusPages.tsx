import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ExternalLink, Pencil, Plus, Rss } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { useT } from '../lib/i18n'
import type { StatusPage, Theme } from '../lib/types'
import { Alert, Badge, Button, Card, Empty, Field, Input, Modal, Skeleton, toast } from '../components/ui'

export const themeMeta: Record<Theme, { name: string; desc: string; swatch: string[] }> = {
  zen: { name: 'Zen', desc: 'Nothing but the status — light or dark with the visitor', swatch: ['#f7f7f4', '#ffffff', '#22c55e'] },
  aurora: { name: 'Aurora', desc: 'Dark with a green glow — the BackPack look', swatch: ['#050806', '#0b100c', '#22c55e'] },
  clarity: { name: 'Clarity', desc: 'Bright, airy and familiar', swatch: ['#f5f6f8', '#ffffff', '#4f46e5'] },
  lime: { name: 'Lime', desc: 'Bento cards with bold numbers', swatch: ['#efefec', '#ffffff', '#d4f36b'] },
  noir: { name: 'Noir', desc: 'Black bento with lime accents', swatch: ['#0a0a0a', '#141414', '#d4f36b'] },
}

export function ThemeSwatch({ theme, className }: { theme: Theme; className?: string }) {
  const [bg, card, accent] = themeMeta[theme].swatch
  if (theme === 'zen') {
    return (
      <div className={className} style={{ background: `linear-gradient(90deg, ${bg} 50%, #0b0c0d 50%)` }} aria-hidden>
        <div className="h-full w-full p-3 flex flex-col justify-center gap-2">
          {[0, 1, 2].map((i) => (
            <div key={i} className="flex items-center gap-1.5">
              <span className="size-2 rounded-full" style={{ background: i === 1 ? '#f59e0b' : accent }} />
              <span className="h-1.5 flex-1 rounded-full" style={{ background: `repeating-linear-gradient(90deg, ${accent} 0 3px, transparent 3px 5px)`, opacity: 0.85 }} />
            </div>
          ))}
        </div>
      </div>
    )
  }
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
  const t = useT()
  const nav = useNavigate()
  const role = useApp((s) => s.role)
  const me = useApp((s) => s.me)
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
      const p = await api.post<StatusPage>('/api/status-pages', {
        title, slug, theme: 'zen', layout: 'list', barMode: 'days', published: true, showPoweredBy: false, showCharts: false,
        showUptime: true, showLatency: false, showBanner: true, language: 'auto', refreshInterval: 60, groups: [{ name: '', monitors: [] }],
      })
      toast.success(t('Status page created'))
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
          <h1 className="font-display text-[28px] font-semibold tracking-tight">{t('Status pages')}</h1>
          <p className="text-sm text-muted mt-1">{t('Public pages for your users, in five themes. Each one picks which monitors it shows.')}</p>
        </div>
        {canEdit(role) && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => { setOpen(true); setTitle(''); setSlug(''); setSlugTouched(false); setErr('') }}>{t('New status page')}</Button>}
      </div>

      {me?.isOwner && (
        <Alert tone="info" title={t('Show your status on your own site')}>
          {t('Create a private API key for a page under')} <Link to="/settings/status-api" className="text-accent-ink underline">{t('Settings → Status API')}</Link>. {t('Only the owner account can create them.')}
        </Alert>
      )}

      {pages === null ? (
        <div className="grid sm:grid-cols-2 xl:grid-cols-3 gap-4">{Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-64 rounded-[18px]" />)}</div>
      ) : pages.length === 0 ? (
        <Card><Empty icon={<Rss className="size-6" />} title={t('No status pages yet')} body={t('Create one to show your users what is up — with your logo, your monitors and incident notes.')} action={canEdit(role) && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setOpen(true)}>{t('New status page')}</Button>} /></Card>
      ) : (
        <div className="grid sm:grid-cols-2 xl:grid-cols-3 gap-4">
          {pages.map((p) => (
            <Card key={p.id} className="overflow-hidden flex flex-col">
              <ThemeSwatch theme={p.theme} className="h-32 border-b border-line" />
              <div className="p-5 flex-1 flex flex-col gap-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <p className="font-display text-lg font-semibold truncate" dir="auto">{p.title}</p>
                    <p className="text-[12.5px] text-muted font-mono truncate" dir="ltr">/status/{p.slug}</p>
                  </div>
                  {p.published ? <Badge tone="up">{t('Live')}</Badge> : <Badge>{t('Draft')}</Badge>}
                </div>
                <div className="flex flex-wrap gap-1.5">
                  <Badge tone="accent">{themeMeta[p.theme]?.name ?? p.theme}</Badge>
                  <Badge>{p.layout === 'cards' ? t('Cards') : t('List')}</Badge>
                  <Badge>{p.language === 'fa' ? 'فارسی' : p.language === 'en' ? 'English' : t('Auto language')}</Badge>
                </div>
                <div className="mt-auto flex gap-2 pt-2">
                  <a href={`/status/${p.slug}`} target="_blank" rel="noreferrer" className="flex-1"><Button className="w-full" size="sm" icon={<ExternalLink className="size-3.5" />}>{t('Open')}</Button></a>
                  {canEdit(role) && <Link to={`/status-pages/${p.id}`} className="flex-1"><Button className="w-full" size="sm" variant="soft" icon={<Pencil className="size-3.5" />}>{t('Edit')}</Button></Link>}
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      <Modal open={open} onClose={() => setOpen(false)} title={t('New status page')} size="sm"
        footer={<><Button variant="ghost" onClick={() => setOpen(false)}>{t('Cancel')}</Button><Button variant="primary" loading={busy} disabled={!title || !slug} onClick={create}>{t('Create')}</Button></>}>
        <div className="space-y-4">
          {err && <Alert tone="danger">{err}</Alert>}
          <Field label={t('Title')} htmlFor="spt"><Input id="spt" dir="auto" data-autofocus value={title} onChange={(e) => { setTitle(e.target.value); if (!slugTouched) setSlug(slugify(e.target.value)) }} placeholder={t('Acme status')} /></Field>
          <Field label={t('Address')} htmlFor="sps" help={`${window.location.origin}/status/${slug || '…'}`}>
            <Input id="sps" dir="ltr" className="font-mono" value={slug} onChange={(e) => { setSlugTouched(true); setSlug(slugify(e.target.value)) }} placeholder="acme" />
          </Field>
        </div>
      </Modal>
    </div>
  )
}
