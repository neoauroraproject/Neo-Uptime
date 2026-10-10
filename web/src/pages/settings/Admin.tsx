import { useEffect, useRef, useState } from 'react'
import { Download, Ellipsis, ExternalLink, KeyRound, Pencil, Plus, Tag as TagIcon, Trash2, Upload } from 'lucide-react'
import { api } from '../../lib/api'
import { canEdit, useApp } from '../../lib/store'
import { bytes, duration, fmtDate, n, timeAgo } from '../../lib/format'
import { useT } from '../../lib/i18n'
import type { ApiKey, Tag, User } from '../../lib/types'
import { Alert, Badge, Button, Card, CardHeader, CopyButton, Empty, Field, IconButton, Input, Menu, Modal, Segmented, Select, Switch, TagChip, confirm, toast } from '../../components/ui'

/* ---------- tags ---------- */

export function Tags() {
  const t = useT()
  const tags = useApp((s) => s.tags)
  const monitors = useApp((s) => s.monitors)
  const role = useApp((s) => s.role)
  const [edit, setEdit] = useState<Partial<Tag> | null>(null)
  const used = (id: number) => Object.values(monitors).filter((m) => m.tags.some((tg) => tg.tagId === id)).length
  const save = async () => {
    if (!edit) return
    try {
      if (edit.id) await api.put(`/api/tags/${edit.id}`, edit)
      else await api.post('/api/tags', edit)
      await useApp.getState().loadTags()
      await useApp.getState().loadDashboard()
      setEdit(null)
    } catch (e) {
      toast.error(t('Could not save tag'), (e as Error).message)
    }
  }
  return (
    <Card>
      <CardHeader title={t('Tags')} subtitle={t('Colour-coded labels for filtering monitors and status pages.')} actions={canEdit(role) && <Button size="sm" variant="primary" icon={<Plus className="size-3.5" />} onClick={() => setEdit({ name: '', color: '#a3e635' })}>{t('New tag')}</Button>} />
      {tags.length === 0 ? <Empty icon={<TagIcon className="size-6" />} title={t('No tags yet')} body={t('Create tags here or while editing a monitor.')} /> : (
        <div className="px-3 pb-3">
          {tags.map((tg) => (
            <div key={tg.id} className="flex items-center gap-3 rounded-xl px-3 py-2.5 hover:bg-surface-2">
              <TagChip name={tg.name} color={tg.color} />
              <span className="text-[12.5px] text-muted flex-1">{t('{n} monitors', { n: used(tg.id) })}</span>
              {canEdit(role) && (
                <>
                  <IconButton size="sm" label={t('Edit {name}', { name: tg.name })} onClick={() => setEdit(tg)}><Pencil className="size-4" /></IconButton>
                  <IconButton size="sm" label={t('Delete {name}', { name: tg.name })} onClick={async () => {
                    if (!(await confirm({ title: t('Delete tag “{name}”?', { name: tg.name }), body: t('It is removed from every monitor.'), confirmLabel: t('Delete'), danger: true }))) return
                    await api.del(`/api/tags/${tg.id}`)
                    useApp.getState().loadTags()
                    useApp.getState().loadDashboard()
                  }}><Trash2 className="size-4" /></IconButton>
                </>
              )}
            </div>
          ))}
        </div>
      )}
      <Modal open={!!edit} onClose={() => setEdit(null)} size="sm" title={edit?.id ? t('Edit tag') : t('New tag')} footer={<><Button variant="ghost" onClick={() => setEdit(null)}>{t('Cancel')}</Button><Button variant="primary" disabled={!edit?.name} onClick={save}>{t('Save')}</Button></>}>
        {edit && (
          <div className="space-y-4">
            <Field label={t('Name')} htmlFor="tn"><Input id="tn" dir="auto" data-autofocus value={edit.name ?? ''} onChange={(e) => setEdit({ ...edit, name: e.target.value })} /></Field>
            <Field label={t('Colour')}>
              <div className="flex flex-wrap items-center gap-2">
                {['#a3e635', '#22c55e', '#3b82f6', '#8b5cf6', '#ec4899', '#ef4444', '#f59e0b', '#14b8a6', '#64748b'].map((c) => (
                  <button key={c} type="button" aria-label={c} onClick={() => setEdit({ ...edit, color: c })} className="size-8 rounded-full border-2 transition-transform hover:scale-110" style={{ background: c, borderColor: edit.color === c ? 'var(--text)' : 'transparent' }} />
                ))}
                <input type="color" aria-label={t('Custom colour')} value={edit.color} onChange={(e) => setEdit({ ...edit, color: e.target.value })} className="h-8 w-10 rounded-lg border border-line-strong bg-surface-2 p-0.5 cursor-pointer" />
              </div>
            </Field>
            <div><TagChip name={edit.name || t('preview')} color={edit.color ?? '#a3e635'} /></div>
          </div>
        )}
      </Modal>
    </Card>
  )
}

/* ---------- users ---------- */

const roleHelp = { admin: 'Everything, including users, servers, keys and backups', editor: 'Add and change monitors, pages, maintenance and alerts', viewer: 'Read-only' }

export function UsersSettings() {
  const t = useT()
  const me = useApp((s) => s.me)
  const [list, setList] = useState<User[]>([])
  const [edit, setEdit] = useState<{ id?: number; username: string; password: string; role: User['role']; resetTotp: boolean; isOwner?: boolean } | null>(null)
  const [err, setErr] = useState('')
  const load = () => api.get<User[]>('/api/users').then(setList).catch(() => {})
  useEffect(() => { load() }, [])
  const save = async () => {
    if (!edit) return
    setErr('')
    try {
      if (edit.id) await api.put(`/api/users/${edit.id}`, edit)
      else await api.post('/api/users', edit)
      toast.success(t('User saved'))
      setEdit(null)
      load()
    } catch (e) {
      setErr((e as Error).message)
    }
  }
  return (
    <Card>
      <CardHeader title={t('Users')} subtitle={t('Give your team access without sharing one password.')} actions={<Button size="sm" variant="primary" icon={<Plus className="size-3.5" />} onClick={() => { setErr(''); setEdit({ username: '', password: '', role: 'viewer', resetTotp: false }) }}>{t('Add user')}</Button>} />
      <div className="px-3 pb-3">
        {list.map((u) => (
          <div key={u.id} className="flex items-center gap-3 rounded-xl px-3 py-3 hover:bg-surface-2">
            <span className="grid place-items-center size-9 rounded-full bg-accent text-on-accent font-display font-semibold uppercase">{u.username[0]}</span>
            <div className="min-w-0 flex-1">
              <p className="font-medium">{u.username}{u.id === me?.id && <span className="text-muted font-normal"> ({t('you')})</span>}</p>
              <p className="text-[12.5px] text-muted">{u.lastLogin ? t('last sign-in {ago}', { ago: timeAgo(u.lastLogin * 1000) }) : t('never signed in')}</p>
            </div>
            {u.isOwner && <Badge tone="accent">{t('Owner')}</Badge>}
            {u.totpEnabled && <Badge tone="up">2FA</Badge>}
            <Badge>{t(u.role === 'admin' ? 'Admin' : u.role === 'editor' ? 'Editor' : 'Viewer')}</Badge>
            <Menu trigger={(tr) => <IconButton {...tr} size="sm" label={t('User actions')}><Ellipsis className="size-4" /></IconButton>} items={[
              { label: t('Edit'), icon: <Pencil />, onClick: () => { setErr(''); setEdit({ id: u.id, username: u.username, password: '', role: u.role, resetTotp: false, isOwner: u.isOwner }) } },
              { label: t('Delete'), icon: <Trash2 />, danger: true, disabled: u.id === me?.id || u.isOwner, onClick: async () => {
                if (!(await confirm({ title: t('Delete {name}?', { name: u.username }), confirmLabel: t('Delete user'), danger: true }))) return
                try { await api.del(`/api/users/${u.id}`); load() } catch (e) { toast.error(t('Could not delete'), (e as Error).message) }
              } },
            ]} />
          </div>
        ))}
      </div>
      <Modal open={!!edit} onClose={() => setEdit(null)} size="sm" title={edit?.id ? t('Edit {name}', { name: edit.username }) : t('Add user')} footer={<><Button variant="ghost" onClick={() => setEdit(null)}>{t('Cancel')}</Button><Button variant="primary" onClick={save} disabled={!edit?.username || (!edit?.id && !edit?.password)}>{t('Save')}</Button></>}>
        {edit && (
          <div className="space-y-4">
            {err && <Alert tone="danger">{err}</Alert>}
            <Field label={t('Username')} htmlFor="un"><Input id="un" dir="ltr" data-autofocus value={edit.username} onChange={(e) => setEdit({ ...edit, username: e.target.value })} /></Field>
            <Field label={edit.id ? t('New password') : t('Password')} htmlFor="up" help={edit.id ? t('Leave empty to keep the current one.') : t('At least 8 characters.')}><Input id="up" dir="ltr" type="password" autoComplete="new-password" value={edit.password} onChange={(e) => setEdit({ ...edit, password: e.target.value })} /></Field>
            {edit.isOwner ? (
              <Alert tone="info">{t('The owner is always an admin. Transfer ownership on the server with neo-uptime set-owner <user>.')}</Alert>
            ) : (
              <Field label={t('Role')} help={t(roleHelp[edit.role])}>
                <Segmented ariaLabel={t('Role')} value={edit.role} onChange={(v) => setEdit({ ...edit, role: v })} options={[{ value: 'viewer', label: t('Viewer') }, { value: 'editor', label: t('Editor') }, { value: 'admin', label: t('Admin') }]} />
              </Field>
            )}
            {edit.id && <Switch checked={edit.resetTotp} onChange={(v) => setEdit({ ...edit, resetTotp: v })} label={t('Reset two-factor sign-in')} description={t('For a user who lost their phone.')} />}
          </div>
        )}
      </Modal>
    </Card>
  )
}

/* ---------- API keys ---------- */

export function ApiKeys() {
  const t = useT()
  const [list, setList] = useState<ApiKey[]>([])
  const [open, setOpen] = useState(false)
  const [f, setF] = useState({ name: '', scope: 'read', expiresDays: 365 })
  const [secret, setSecret] = useState('')
  const load = () => api.get<ApiKey[]>('/api/api-keys').then(setList).catch(() => {})
  useEffect(() => { load() }, [])
  const create = async () => {
    try {
      const r = await api.post<{ secret: string }>('/api/api-keys', f)
      setSecret(r.secret)
      load()
    } catch (e) {
      toast.error(t('Could not create'), (e as Error).message)
    }
  }
  const origin = useApp.getState().settings.primaryBaseURL || window.location.origin
  return (
    <Card>
      <CardHeader title={t('API keys')} subtitle={t('For Prometheus (/metrics), scripts and automation. Sent as Authorization: Bearer <key>. To show status on your website, use Status API instead.')} actions={<Button size="sm" variant="primary" icon={<Plus className="size-3.5" />} onClick={() => { setSecret(''); setF({ name: '', scope: 'read', expiresDays: 365 }); setOpen(true) }}>{t('New key')}</Button>} />
      {list.length === 0 ? <Empty icon={<KeyRound className="size-6" />} title={t('No API keys')} /> : (
        <div className="px-3 pb-3">
          {list.map((k) => {
            const expired = k.expiresAt > 0 && k.expiresAt * 1000 < Date.now()
            return (
              <div key={k.id} className="flex items-center gap-3 rounded-xl px-3 py-3 hover:bg-surface-2">
                <KeyRound className="size-4 text-muted" />
                <div className="min-w-0 flex-1">
                  <p className="font-medium"><span dir="auto">{k.name}</span> <code className="ms-1 text-[12px] text-subtle font-mono" dir="ltr">{k.prefix}…</code></p>
                  <p className="text-[12.5px] text-muted">{k.lastUsed ? t('used {ago}', { ago: timeAgo(k.lastUsed * 1000) }) : t('never used')} · {k.expiresAt ? t('expires {d}', { d: fmtDate(k.expiresAt * 1000) }) : t('no expiry')}</p>
                </div>
                <Badge tone={k.scope === 'write' ? 'pending' : 'neutral'}>{k.scope === 'write' ? t('Read & write') : t('Read')}</Badge>
                {expired ? <Badge tone="down">{t('Expired')}</Badge> : !k.active ? <Badge>{t('Disabled')}</Badge> : null}
                <Menu trigger={(tr) => <IconButton {...tr} size="sm" label={t('Key actions')}><Ellipsis className="size-4" /></IconButton>} items={[
                  { label: k.active ? t('Disable') : t('Enable'), onClick: async () => { await api.post(`/api/api-keys/${k.id}/${k.active ? 'disable' : 'enable'}`); load() } },
                  { label: t('Delete'), icon: <Trash2 />, danger: true, onClick: async () => { if (await confirm({ title: t('Delete key “{name}”?', { name: k.name }), confirmLabel: t('Delete'), danger: true })) { await api.del(`/api/api-keys/${k.id}`); load() } } },
                ]} />
              </div>
            )
          })}
        </div>
      )}
      <Modal open={open} onClose={() => setOpen(false)} title={secret ? t('Copy your key now') : t('New API key')} size="md"
        footer={secret ? <Button variant="primary" onClick={() => setOpen(false)}>{t('Done')}</Button> : <><Button variant="ghost" onClick={() => setOpen(false)}>{t('Cancel')}</Button><Button variant="primary" disabled={!f.name} onClick={create}>{t('Create key')}</Button></>}>
        {secret ? (
          <div className="space-y-4">
            <Alert tone="warning">{t('This is the only time the key is shown. Only its hash is stored.')}</Alert>
            <div className="flex gap-2"><code className="flex-1 min-w-0 break-all rounded-xl bg-surface-2 border border-line px-3 py-2.5 font-mono text-[13px]" dir="ltr">{secret}</code><CopyButton text={secret} /></div>
            <Field label="Prometheus">
              <pre className="rounded-xl bg-surface-2 border border-line px-3 py-2.5 font-mono text-[12px] overflow-x-auto" dir="ltr">{`scrape_configs:\n  - job_name: neo-uptime\n    scheme: ${origin.startsWith('https') ? 'https' : 'http'}\n    authorization:\n      credentials: ${secret}\n    static_configs:\n      - targets: ['${origin.replace(/^https?:\/\//, '')}']`}</pre>
            </Field>
          </div>
        ) : (
          <div className="space-y-4">
            <Field label={t('Name')} htmlFor="kn" help={t('What uses it, so you know when it can go.')}><Input id="kn" dir="auto" data-autofocus value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="prometheus" /></Field>
            <Field label={t('Scope')} help={f.scope === 'write' ? t('Can read and change monitors, pages and maintenance — never users, keys or backups.') : t('Can read monitors, history and /metrics.')}>
              <Segmented ariaLabel={t('Scope')} value={f.scope} onChange={(v) => setF({ ...f, scope: v })} options={[{ value: 'read', label: t('Read') }, { value: 'write', label: t('Read & write') }]} />
            </Field>
            <Field label={t('Expires after')} htmlFor="ke">
              <Select id="ke" value={f.expiresDays} onChange={(e) => setF({ ...f, expiresDays: Number(e.target.value) })}>
                <option value={30}>{t('{n} days', { n: 30 })}</option><option value={90}>{t('{n} days', { n: 90 })}</option><option value={365}>{t('1 year')}</option><option value={0}>{t('Never')}</option>
              </Select>
            </Field>
          </div>
        )}
      </Modal>
    </Card>
  )
}

/* ---------- backup ---------- */

export function Backup() {
  const t = useT()
  const fileRef = useRef<HTMLInputElement>(null)
  const [mode, setMode] = useState<'merge' | 'replace'>('merge')
  const [busy, setBusy] = useState(false)
  const exportNow = async () => {
    const res = await fetch('/api/backup', { credentials: 'same-origin', headers: { 'X-Neo-Request': '1' } })
    if (!res.ok) return toast.error(t('Export failed'))
    const blob = await res.blob()
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `neo-uptime-backup-${new Date().toISOString().slice(0, 10)}.json`
    a.click()
    URL.revokeObjectURL(a.href)
  }
  const importFile = async (file: File) => {
    let data: unknown
    try {
      data = JSON.parse(await file.text())
    } catch {
      return toast.error(t('That file is not JSON'))
    }
    if (!(await confirm({ title: mode === 'replace' ? t('Replace everything with this backup?') : t('Import this backup?'), body: mode === 'replace' ? t('Every monitor, page, channel and their history is deleted first.') : t('Everything in the file is added next to what you have.'), confirmLabel: t('Import'), danger: mode === 'replace' }))) return
    setBusy(true)
    try {
      const r = await api.post<{ imported: Record<string, number> }>('/api/backup/import', { mode, data })
      toast.success(t('Backup imported'), Object.entries(r.imported).map(([k, v]) => `${n(v)} ${k}`).join('، ') || t('nothing new'))
      await Promise.all([useApp.getState().loadDashboard(), useApp.getState().loadNotifications(), useApp.getState().loadTags(), useApp.getState().loadPanels()])
    } catch (e) {
      toast.error(t('Import failed'), (e as Error).message)
    } finally {
      setBusy(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }
  return (
    <Card>
      <CardHeader title={t('Backup')} subtitle={t('Monitors, notification channels, BackPack panels, status pages, maintenance, tags and settings — as one JSON file.')} />
      <div className="px-5 pb-5 space-y-5">
        <Alert tone="warning">{t('The file contains secrets (bot tokens, panel tokens, database passwords). Keep it somewhere safe. History and agent tokens are not included — copy neo-uptime.db for those.')}</Alert>
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="primary" icon={<Download className="size-4" />} onClick={exportNow}>{t('Download backup')}</Button>
        </div>
        <div className="pt-5 border-t border-line space-y-3">
          <p className="font-medium">{t('Restore')}</p>
          <Segmented ariaLabel={t('Import mode')} value={mode} onChange={setMode} options={[{ value: 'merge', label: t('Add to existing') }, { value: 'replace', label: t('Replace everything') }]} />
          <input ref={fileRef} type="file" accept="application/json,.json" className="hidden" onChange={(e) => e.target.files?.[0] && importFile(e.target.files[0])} />
          <div><Button loading={busy} icon={<Upload className="size-4" />} onClick={() => fileRef.current?.click()}>{t('Choose backup file…')}</Button></div>
        </div>
      </div>
    </Card>
  )
}

/* ---------- about ---------- */

export function About() {
  const t = useT()
  const role = useApp((s) => s.role)
  const version = useApp((s) => s.version)
  const [info, setInfo] = useState<{ version: string; go: string; os: string; arch: string; startedAt: number; dbSize: number; dataDir: string; monitors: number; liveClients: number; repo: string } | null>(null)
  useEffect(() => { if (role === 'admin') api.get<typeof info>('/api/settings/info').then(setInfo).catch(() => {}) }, [role])
  const repo = info?.repo ?? 'neoauroraproject/Neo-Uptime'
  const install = `bash <(curl -fsSL https://raw.githubusercontent.com/${repo}/main/install.sh)`
  return (
    <Card>
      <CardHeader title={t('About Neo Uptime')} subtitle={t('Self-hosted uptime monitoring with BackPack and Kariz tunnel support.')} />
      <div className="px-5 pb-5 space-y-5">
        <dl className="grid sm:grid-cols-2 gap-4 text-sm">
          <div><dt className="text-muted text-[12.5px]">{t('Version')}</dt><dd className="font-mono mt-0.5" dir="ltr">{info?.version ?? version}</dd></div>
          {info && <>
            <div><dt className="text-muted text-[12.5px]">{t('Running for')}</dt><dd className="mt-0.5">{duration((Date.now() - info.startedAt) / 1000)}</dd></div>
            <div><dt className="text-muted text-[12.5px]">{t('Platform')}</dt><dd className="font-mono mt-0.5" dir="ltr">{info.os}/{info.arch} · {info.go}</dd></div>
            <div><dt className="text-muted text-[12.5px]">{t('Database')}</dt><dd className="mt-0.5">{bytes(info.dbSize)} · <span className="font-mono" dir="ltr">{info.dataDir}</span></dd></div>
            <div><dt className="text-muted text-[12.5px]">{t('Monitors')}</dt><dd className="mt-0.5">{n(info.monitors)}</dd></div>
            <div><dt className="text-muted text-[12.5px]">{t('Live browsers')}</dt><dd className="mt-0.5">{n(info.liveClients)}</dd></div>
          </>}
        </dl>
        <Field label={t('Update')} help={t('Running the installer again upgrades in place and keeps your data.')}>
          <div className="flex gap-2"><code className="flex-1 min-w-0 truncate rounded-xl bg-surface-2 border border-line px-3 py-2.5 font-mono text-[12.5px]" dir="ltr">{install}</code><CopyButton text={install} /></div>
        </Field>
        <a className="inline-flex items-center gap-2 text-sm text-accent-ink hover:underline" href={`https://github.com/${repo}`} target="_blank" rel="noreferrer" dir="ltr"><ExternalLink className="size-4" />github.com/{repo}</a>
      </div>
    </Card>
  )
}
