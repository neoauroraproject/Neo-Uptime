import { useEffect, useState } from 'react'
import { Code, Ellipsis, KeyRound, Plus, Trash2 } from 'lucide-react'
import { api } from '../../lib/api'
import { useApp } from '../../lib/store'
import { fmtDate, timeAgo } from '../../lib/format'
import { useT } from '../../lib/i18n'
import type { StatusApiKey, StatusPage } from '../../lib/types'
import { Alert, Badge, Button, Card, CardHeader, CopyButton, Empty, Field, IconButton, Input, Menu, Modal, Segmented, Select, Textarea, confirm, toast } from '../../components/ui'

function Snippet({ code }: { code: string }) {
  return (
    <div className="flex gap-2 items-start">
      <pre className="flex-1 min-w-0 overflow-x-auto rounded-xl bg-surface-2 border border-line px-3 py-2.5 font-mono text-[12px] leading-relaxed" dir="ltr">{code}</pre>
      <CopyButton text={code} />
    </div>
  )
}

function Usage({ secret, slug }: { secret: string; slug: string }) {
  const t = useT()
  const base = useApp((s) => s.settings.primaryBaseURL) || window.location.origin
  const [tab, setTab] = useState<'widget' | 'curl' | 'js'>('widget')
  const url = `${base}/api/v1/status/${slug}`
  const widget = `<div id="neo-status"></div>\n<script src="${base}/embed.js" data-target="#neo-status"\n  data-server="${base}" data-slug="${slug}" data-key="${secret}"\n  data-lang="fa" data-theme="auto" async></script>`
  const curl = `curl -H "Authorization: Bearer ${secret}" ${url}`
  const js = `const res = await fetch('${url}', {\n  headers: { Authorization: 'Bearer ${secret}' },\n})\nconst status = await res.json()\nconsole.log(status.overall, status.groups)`
  return (
    <div className="space-y-3">
      <Segmented size="sm" ariaLabel={t('Example')} value={tab} onChange={setTab} options={[{ value: 'widget', label: t('Widget') }, { value: 'curl', label: 'curl' }, { value: 'js', label: 'JavaScript' }]} />
      <Snippet code={tab === 'widget' ? widget : tab === 'curl' ? curl : js} />
      {tab === 'widget' && <p className="text-[12.5px] text-muted">{t('Paste it into any page of your site. It renders the servers with their flags and refreshes every minute. data-lang: fa or en · data-theme: auto, light or dark.')}</p>}
    </div>
  )
}

export default function StatusAPI() {
  const t = useT()
  const me = useApp((s) => s.me)
  const [keys, setKeys] = useState<StatusApiKey[]>([])
  const [pages, setPages] = useState<StatusPage[]>([])
  const [open, setOpen] = useState(false)
  const [f, setF] = useState({ name: '', statusPageId: 0, allowedOrigins: '' })
  const [created, setCreated] = useState<{ secret: string; slug: string } | null>(null)
  const [err, setErr] = useState('')
  const load = () => api.get<StatusApiKey[]>('/api/status-api-keys').then(setKeys).catch(() => {})
  useEffect(() => {
    load()
    api.get<StatusPage[]>('/api/status-pages').then((p) => { setPages(p); setF((x) => ({ ...x, statusPageId: p[0]?.id ?? 0 })) }).catch(() => {})
  }, [])

  if (!me?.isOwner) {
    return <Card><Empty icon={<KeyRound className="size-6" />} title={t('Owner only')} body={t('Only the owner account can create status API keys.')} /></Card>
  }

  const create = async () => {
    setErr('')
    try {
      const r = await api.post<{ key: StatusApiKey; secret: string }>('/api/status-api-keys', f)
      setCreated({ secret: r.secret, slug: pages.find((p) => p.id === f.statusPageId)?.slug ?? '' })
      load()
    } catch (e) {
      setErr((e as Error).message)
    }
  }
  const pageName = (id: number) => pages.find((p) => p.id === id)?.title ?? `#${id}`
  const example = `{
  "overall": "operational",
  "updatedAt": 1791650000000,
  "groups": [{
    "name": "Tunnels",
    "monitors": [{
      "name": "DE2", "country": "de", "status": "up",
      "latencyMs": 92, "uptime24h": 0.9995, "uptime30d": 0.9981,
      "history": ["u","u","u","d","u"]
    }]
  }],
  "incidents": [], "maintenance": []
}`

  return (
    <>
      <Card>
        <CardHeader title={t('Status API')} subtitle={t('A private JSON feed of one status page, to show your servers’ status on your own website. Only you, the owner, can create keys.')} icon={<Code className="size-4" />}
          actions={<Button size="sm" variant="primary" icon={<Plus className="size-3.5" />} disabled={!pages.length} onClick={() => { setCreated(null); setErr(''); setF({ name: '', statusPageId: pages[0]?.id ?? 0, allowedOrigins: '' }); setOpen(true) }}>{t('New key')}</Button>} />
        {!pages.length ? (
          <div className="px-5 pb-5"><Alert tone="info">{t('Create a status page first — a key gives access to one page’s monitors.')}</Alert></div>
        ) : keys.length === 0 ? (
          <Empty icon={<KeyRound className="size-6" />} title={t('No keys yet')} body={t('Create one, choose which page it reads, and restrict it to your site’s address.')} />
        ) : (
          <div className="px-3 pb-3">
            {keys.map((k) => (
              <div key={k.id} className="flex items-center gap-3 rounded-xl px-3 py-3 hover:bg-surface-2">
                <KeyRound className="size-4 text-muted" />
                <div className="min-w-0 flex-1">
                  <p className="font-medium"><span dir="auto">{k.name}</span> <code className="ms-1 text-[12px] text-subtle font-mono" dir="ltr">{k.prefix}…</code></p>
                  <p className="text-[12.5px] text-muted truncate">{pageName(k.statusPageId)} · {k.allowedOrigins ? <span dir="ltr">{k.allowedOrigins}</span> : t('any site')} · {k.lastUsed ? t('used {ago}', { ago: timeAgo(k.lastUsed * 1000) }) : t('never used')} · {fmtDate(k.createdAt * 1000)}</p>
                </div>
                {!k.active && <Badge>{t('Disabled')}</Badge>}
                <Menu trigger={(tr) => <IconButton {...tr} size="sm" label={t('Key actions')}><Ellipsis className="size-4" /></IconButton>} items={[
                  { label: k.active ? t('Disable') : t('Enable'), onClick: async () => { await api.put(`/api/status-api-keys/${k.id}`, { ...k, active: !k.active }); load() } },
                  { label: t('Delete'), icon: <Trash2 />, danger: true, onClick: async () => { if (await confirm({ title: t('Delete key “{name}”?', { name: k.name }), body: t('Sites using it stop showing your status.'), confirmLabel: t('Delete'), danger: true })) { await api.del(`/api/status-api-keys/${k.id}`); load(); toast.success(t('Key deleted')) } } },
                ]} />
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card>
        <CardHeader title={t('Response format')} subtitle={t('GET /api/v1/status/{slug} with Authorization: Bearer <key> (or ?key=). Statuses: up, down, pending, maintenance, paused, unknown.')} />
        <div className="px-5 pb-5"><Snippet code={example} /></div>
      </Card>

      <Modal open={open} onClose={() => setOpen(false)} size="lg" title={created ? t('Your key') : t('New status API key')}
        footer={created ? <Button variant="primary" onClick={() => setOpen(false)}>{t('Done')}</Button> : <><Button variant="ghost" onClick={() => setOpen(false)}>{t('Cancel')}</Button><Button variant="primary" disabled={!f.name || !f.statusPageId} onClick={create}>{t('Create key')}</Button></>}>
        {created ? (
          <div className="space-y-4">
            <Alert tone="warning">{t('Copy it now — only its hash is stored, so it cannot be shown again.')}</Alert>
            <Snippet code={created.secret} />
            <Usage secret={created.secret} slug={created.slug} />
            <Alert tone="info">{t('A key used in a web page is visible to its visitors. That is why it only reads one page’s status, and why it should be limited to your site’s address.')}</Alert>
          </div>
        ) : (
          <div className="space-y-4">
            {err && <Alert tone="danger">{err}</Alert>}
            <div className="grid sm:grid-cols-2 gap-4">
              <Field label={t('Name')} htmlFor="kn"><Input id="kn" dir="auto" data-autofocus value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder={t('my website')} /></Field>
              <Field label={t('Status page')} htmlFor="kp">
                <Select id="kp" value={f.statusPageId} onChange={(e) => setF({ ...f, statusPageId: Number(e.target.value) })}>
                  {pages.map((p) => <option key={p.id} value={p.id}>{p.title}</option>)}
                </Select>
              </Field>
            </div>
            <Field label={t('Allowed sites (optional)')} htmlFor="ko" help={t('One per line, e.g. https://example.com. Browsers on other sites are refused. Empty allows any site; server-to-server calls are never limited.')}>
              <Textarea id="ko" dir="ltr" className="font-mono text-[13px] min-h-20" value={f.allowedOrigins} onChange={(e) => setF({ ...f, allowedOrigins: e.target.value })} placeholder={'https://example.com\nhttps://www.example.com'} />
            </Field>
          </div>
        )}
      </Modal>
    </>
  )
}
