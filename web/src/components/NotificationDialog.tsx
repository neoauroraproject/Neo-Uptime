import { useEffect, useState } from 'react'
import { Search, Send } from 'lucide-react'
import { api } from '../lib/api'
import { useApp } from '../lib/store'
import { useLang, useT } from '../lib/i18n'
import type { Notification, Provider } from '../lib/types'
import { Alert, Button, Checkbox, Field, Input, Modal, Select, Switch, Textarea, toast } from './ui'

let providerCache: Provider[] | null = null
export async function loadProviders(): Promise<Provider[]> {
  if (!providerCache) providerCache = await api.get<Provider[]>('/api/notification-providers')
  return providerCache
}

export function useProviders() {
  const [list, setList] = useState<Provider[]>(providerCache ?? [])
  useEffect(() => {
    loadProviders().then(setList).catch(() => {})
  }, [])
  return list
}

interface Chat { id: string; type: string; name: string }

export function NotificationDialog({ open, onClose, initial, initialType, onSaved }: {
  open: boolean
  onClose: () => void
  initial?: Notification
  initialType?: string
  onSaved?: (n: Notification) => void
}) {
  const t = useT()
  const lang = useLang((s) => s.lang)
  const providers = useProviders()
  const blank = (type = 'telegram'): Notification => ({ id: 0, name: type === 'telegram' ? t('My Telegram') : '', type, config: { language: lang }, isDefault: true, active: true, createdAt: 0 })
  const [n, setN] = useState<Notification>(blank())
  const [applyExisting, setApplyExisting] = useState(false)
  const [busy, setBusy] = useState<'' | 'save' | 'test' | 'chats'>('')
  const [err, setErr] = useState('')
  const [testOk, setTestOk] = useState(false)
  const [chats, setChats] = useState<Chat[] | null>(null)

  useEffect(() => {
    if (!open) return
    setErr('')
    setTestOk(false)
    setChats(null)
    setApplyExisting(false)
    setN(initial ? { ...initial, config: { ...initial.config } } : blank(initialType))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, initial, initialType])

  const p = providers.find((x) => x.type === n.type)
  const val = (k: string, def = '') => n.config[k] ?? def
  const setVal = (k: string, v: string) => setN({ ...n, config: { ...n.config, [k]: v } })
  const groups = [...new Set(providers.map((x) => x.group))]

  const withDefaults = () => {
    const cfg = { ...n.config }
    for (const f of p?.fields ?? []) if (cfg[f.key] === undefined && f.default) cfg[f.key] = f.default
    return cfg
  }

  const findChats = async () => {
    setBusy('chats')
    setErr('')
    try {
      const list = await api.post<Chat[]>('/api/notifications/telegram/chats', { botToken: val('botToken'), apiBase: val('apiBase'), proxy: val('proxy') })
      setChats(list)
      if (list.length === 1) setVal('chatId', list[0].id)
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy('')
    }
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
      const saved = n.id ? await api.put<Notification>(`/api/notifications/${n.id}`, body) : await api.post<Notification>('/api/notifications', body)
      await useApp.getState().loadNotifications()
      toast.success(t('Notification saved'))
      onSaved?.(saved)
      onClose()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  const langLabel = (o: string) => (o === 'fa' ? 'فارسی' : o === 'en' ? 'English' : o)

  return (
    <Modal open={open} onClose={onClose} size="lg" title={n.id ? t('Edit {name}', { name: n.name }) : t('New notification channel')}
      description={t('Alerts are sent when a monitor goes down and when it comes back, like Uptime Kuma.')}
      footer={<><Button variant="ghost" onClick={onClose}>{t('Cancel')}</Button><Button onClick={test} loading={busy === 'test'} icon={<Send className="size-4" />}>{t('Send test')}</Button><Button variant="primary" onClick={save} loading={busy === 'save'} disabled={!n.name}>{t('Save')}</Button></>}>
      <div className="space-y-5">
        {err && <Alert tone="danger" title={t('Not delivered')}>{err}</Alert>}
        {testOk && <Alert tone="success">{t('Test message sent — check that it arrived.')}</Alert>}
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label={t('Type')} htmlFor="nt">
            <Select id="nt" value={n.type} onChange={(e) => setN({ ...n, type: e.target.value, config: { language: val('language', lang) } })} disabled={!!n.id}>
              {groups.map((g) => <optgroup key={g} label={t(g)}>{providers.filter((x) => x.group === g).map((x) => <option key={x.type} value={x.type}>{t(x.name)}</option>)}</optgroup>)}
            </Select>
          </Field>
          <Field label={t('Name')} htmlFor="nn"><Input id="nn" value={n.name} onChange={(e) => setN({ ...n, name: e.target.value })} /></Field>
        </div>
        {n.type === 'telegram' && (
          <Alert tone="info" title={t('Telegram in three steps')}>
            {t('1. Create a bot with @BotFather and paste its token. 2. Send any message to the bot (or add it to your group or channel). 3. Press “Find chats” and pick the chat.')}
          </Alert>
        )}
        {p?.fields.map((f) => (
          f.type === 'switch' ? (
            <Switch key={f.key} checked={val(f.key, f.default) === 'true'} onChange={(v) => setVal(f.key, String(v))} label={t(f.label)} description={f.help ? t(f.help) : undefined} />
          ) : (
            <Field key={f.key} label={t(f.label)} required={f.required} help={f.help ? t(f.help) : undefined} htmlFor={`f-${f.key}`}>
              {f.type === 'select' ? (
                <Select id={`f-${f.key}`} value={val(f.key, f.default)} onChange={(e) => setVal(f.key, e.target.value)}>{f.options?.map((o) => <option key={o} value={o}>{f.key === 'language' ? langLabel(o) : o}</option>)}</Select>
              ) : f.type === 'textarea' ? (
                <Textarea id={`f-${f.key}`} dir="ltr" className="font-mono text-[13px]" value={val(f.key)} onChange={(e) => setVal(f.key, e.target.value)} placeholder={f.placeholder} />
              ) : (
                <div className="flex gap-2">
                  <Input id={`f-${f.key}`} dir="ltr" type={f.type === 'password' ? 'password' : f.type === 'number' ? 'number' : 'text'} autoComplete="off" className={f.type === 'url' || f.type === 'password' ? 'font-mono' : ''} value={val(f.key)} onChange={(e) => setVal(f.key, e.target.value)} placeholder={f.placeholder || f.default} />
                  {n.type === 'telegram' && f.key === 'chatId' && (
                    <Button type="button" onClick={findChats} loading={busy === 'chats'} disabled={!val('botToken')} icon={<Search className="size-4" />}>{t('Find chats')}</Button>
                  )}
                </div>
              )}
              {n.type === 'telegram' && f.key === 'chatId' && chats && (
                chats.length === 0 ? (
                  <p className="text-[12.5px] text-pending">{t('The bot has not seen any chat yet — send it a message first, then try again.')}</p>
                ) : (
                  <div className="flex flex-wrap gap-1.5 pt-1">
                    {chats.map((c) => (
                      <button key={c.id} type="button" onClick={() => setVal('chatId', c.id)}
                        className={`h-8 px-3 rounded-lg border text-[12.5px] transition-colors ${val('chatId') === c.id ? 'bg-accent-soft border-accent/40 text-accent-ink' : 'bg-surface-2 border-line text-muted hover:text-fg'}`}>
                        {c.name || c.id} <span className="text-subtle">· {c.type}</span>
                      </button>
                    ))}
                  </div>
                )
              )}
            </Field>
          )
        ))}
        <div className="pt-4 border-t border-line space-y-3">
          <Switch checked={n.isDefault} onChange={(v) => setN({ ...n, isDefault: v })} label={t('On by default')} description={t('New monitors, servers and imported tunnels get this channel.')} />
          <Checkbox checked={applyExisting} onChange={setApplyExisting} label={<span className="text-muted">{t('Also add it to every existing monitor')}</span>} />
        </div>
      </div>
    </Modal>
  )
}
