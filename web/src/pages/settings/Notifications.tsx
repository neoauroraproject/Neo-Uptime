import { useEffect, useState } from 'react'
import { Bell, Ellipsis, Pencil, Plus, Send, Trash2 } from 'lucide-react'
import { api } from '../../lib/api'
import { canEdit, useApp } from '../../lib/store'
import type { Notification, Provider } from '../../lib/types'
import { Alert, Badge, Button, Card, CardHeader, Checkbox, Empty, Field, IconButton, Input, Menu, Modal, Select, Switch, Textarea, confirm, toast } from '../../components/ui'

function NotificationDialog({ open, onClose, initial, providers }: { open: boolean; onClose: () => void; initial?: Notification; providers: Provider[] }) {
  const [n, setN] = useState<Notification>({ id: 0, name: '', type: 'telegram', config: {}, isDefault: true, active: true, createdAt: 0 })
  const [applyExisting, setApplyExisting] = useState(false)
  const [busy, setBusy] = useState<'' | 'save' | 'test'>('')
  const [err, setErr] = useState('')
  const [testOk, setTestOk] = useState(false)

  useEffect(() => {
    if (!open) return
    setErr('')
    setTestOk(false)
    setApplyExisting(false)
    setN(initial ? { ...initial, config: { ...initial.config } } : { id: 0, name: 'My Telegram', type: 'telegram', config: {}, isDefault: true, active: true, createdAt: 0 })
  }, [open, initial])

  const p = providers.find((x) => x.type === n.type)
  const val = (k: string, def = '') => n.config[k] ?? def
  const setVal = (k: string, v: string) => setN({ ...n, config: { ...n.config, [k]: v } })
  const groups = [...new Set(providers.map((x) => x.group))]

  const withDefaults = () => {
    const cfg = { ...n.config }
    for (const f of p?.fields ?? []) if (cfg[f.key] === undefined && f.default) cfg[f.key] = f.default
    return cfg
  }

  const test = async () => {
    setBusy('test'); setErr(''); setTestOk(false)
    try {
      await api.post('/api/notifications/test', { type: n.type, config: withDefaults() })
      setTestOk(true)
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy('')
    }
  }
  const save = async () => {
    setBusy('save'); setErr('')
    try {
      const body = { ...n, config: withDefaults(), applyExisting }
      if (n.id) await api.put(`/api/notifications/${n.id}`, body)
      else await api.post('/api/notifications', body)
      await useApp.getState().loadNotifications()
      toast.success('Notification saved')
      onClose()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  return (
    <Modal open={open} onClose={onClose} size="lg" title={n.id ? `Edit ${n.name}` : 'New notification channel'}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button onClick={test} loading={busy === 'test'} icon={<Send className="size-4" />}>Send test</Button><Button variant="primary" onClick={save} loading={busy === 'save'} disabled={!n.name}>Save</Button></>}>
      <div className="space-y-5">
        {err && <Alert tone="danger" title="Not delivered">{err}</Alert>}
        {testOk && <Alert tone="success">Test message sent — check that it arrived.</Alert>}
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label="Type" htmlFor="nt">
            <Select id="nt" value={n.type} onChange={(e) => setN({ ...n, type: e.target.value, config: {} })} disabled={!!n.id}>
              {groups.map((g) => <optgroup key={g} label={g}>{providers.filter((x) => x.group === g).map((x) => <option key={x.type} value={x.type}>{x.name}</option>)}</optgroup>)}
            </Select>
          </Field>
          <Field label="Name" htmlFor="nn"><Input id="nn" value={n.name} onChange={(e) => setN({ ...n, name: e.target.value })} /></Field>
        </div>
        {p?.fields.map((f) => (
          f.type === 'switch' ? (
            <Switch key={f.key} checked={val(f.key, f.default) === 'true'} onChange={(v) => setVal(f.key, String(v))} label={f.label} description={f.help} />
          ) : (
            <Field key={f.key} label={f.label} required={f.required} help={f.help} htmlFor={`f-${f.key}`}>
              {f.type === 'select' ? (
                <Select id={`f-${f.key}`} value={val(f.key, f.default)} onChange={(e) => setVal(f.key, e.target.value)}>{f.options?.map((o) => <option key={o}>{o}</option>)}</Select>
              ) : f.type === 'textarea' ? (
                <Textarea id={`f-${f.key}`} className="font-mono text-[13px]" value={val(f.key)} onChange={(e) => setVal(f.key, e.target.value)} placeholder={f.placeholder} />
              ) : (
                <Input id={`f-${f.key}`} type={f.type === 'password' ? 'password' : f.type === 'number' ? 'number' : 'text'} autoComplete="off" className={f.type === 'url' || f.type === 'password' ? 'font-mono' : ''} value={val(f.key)} onChange={(e) => setVal(f.key, e.target.value)} placeholder={f.placeholder || f.default} />
              )}
            </Field>
          )
        ))}
        <div className="pt-4 border-t border-line space-y-3">
          <Switch checked={n.isDefault} onChange={(v) => setN({ ...n, isDefault: v })} label="On by default" description="New monitors and imported tunnels get this channel." />
          <Checkbox checked={applyExisting} onChange={setApplyExisting} label={<span className="text-muted">Also add it to every existing monitor</span>} />
        </div>
      </div>
    </Modal>
  )
}

export default function NotificationsSettings() {
  const list = useApp((s) => s.notifications)
  const role = useApp((s) => s.role)
  const [providers, setProviders] = useState<Provider[]>([])
  const [open, setOpen] = useState(false)
  const [edit, setEdit] = useState<Notification | undefined>()
  useEffect(() => { api.get<Provider[]>('/api/notification-providers').then(setProviders).catch(() => {}) }, [])
  const name = (t: string) => providers.find((p) => p.type === t)?.name ?? t
  return (
    <Card>
      <CardHeader title="Notifications" subtitle="Where alerts go. Telegram can use a relay or proxy when the API is filtered." actions={canEdit(role) && <Button size="sm" variant="primary" icon={<Plus className="size-3.5" />} onClick={() => { setEdit(undefined); setOpen(true) }}>Add channel</Button>} />
      {list.length === 0 ? (
        <Empty icon={<Bell className="size-6" />} title="No channels yet" body="Telegram, Bale, Discord, Slack, Teams, email, ntfy, Gotify, Pushover, Google Chat or any webhook." />
      ) : (
        <div className="px-3 pb-3">
          {list.map((n) => (
            <div key={n.id} className="flex items-center gap-3 rounded-xl px-3 py-3 hover:bg-surface-2">
              <span className="grid place-items-center size-9 rounded-xl bg-surface-2 border border-line text-muted"><Bell className="size-4" /></span>
              <div className="min-w-0 flex-1">
                <p className="font-medium truncate">{n.name}</p>
                <p className="text-[12.5px] text-muted">{name(n.type)}</p>
              </div>
              {n.isDefault && <Badge tone="accent">Default</Badge>}
              {canEdit(role) && (
                <Menu trigger={(t) => <IconButton {...t} size="sm" label="Channel actions"><Ellipsis className="size-4" /></IconButton>} items={[
                  { label: 'Edit', icon: <Pencil />, onClick: () => { setEdit(n); setOpen(true) } },
                  { label: 'Send test', icon: <Send />, onClick: async () => {
                    try { await api.post('/api/notifications/test', { type: n.type, config: n.config }); toast.success('Test sent') } catch (e) { toast.error('Not delivered', (e as Error).message) }
                  } },
                  { divider: true, label: '' },
                  { label: 'Delete', icon: <Trash2 />, danger: true, onClick: async () => {
                    if (!(await confirm({ title: `Delete “${n.name}”?`, body: 'Monitors using it stop sending there.', confirmLabel: 'Delete', danger: true }))) return
                    await api.del(`/api/notifications/${n.id}`)
                    useApp.getState().loadNotifications()
                  } },
                ]} />
              )}
            </div>
          ))}
        </div>
      )}
      <NotificationDialog open={open} onClose={() => setOpen(false)} initial={edit} providers={providers} />
    </Card>
  )
}
