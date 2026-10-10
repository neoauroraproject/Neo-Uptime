import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Bell, Ellipsis, Pencil, Plus, Send, Trash2 } from 'lucide-react'
import { api } from '../../lib/api'
import { canEdit, useApp } from '../../lib/store'
import { useT } from '../../lib/i18n'
import type { Notification } from '../../lib/types'
import { Badge, Button, Card, CardHeader, Empty, IconButton, Menu, confirm, toast } from '../../components/ui'
import { NotificationDialog, useProviders } from '../../components/NotificationDialog'

export default function NotificationsSettings() {
  const t = useT()
  const list = useApp((s) => s.notifications)
  const role = useApp((s) => s.role)
  const providers = useProviders()
  const [params, setParams] = useSearchParams()
  const [open, setOpen] = useState(false)
  const [edit, setEdit] = useState<Notification | undefined>()
  const [initialType, setInitialType] = useState<string | undefined>()

  useEffect(() => {
    const nt = params.get('new')
    if (nt && canEdit(role)) {
      setEdit(undefined)
      setInitialType(nt)
      setOpen(true)
      params.delete('new')
      setParams(params, { replace: true })
    }
  }, [params, setParams, role])

  const name = (type: string) => t(providers.find((p) => p.type === type)?.name ?? type)
  return (
    <Card>
      <CardHeader title={t('Notifications')} subtitle={t('Where alerts go when a monitor goes down or comes back. Telegram can use a relay or proxy when the API is filtered.')}
        actions={canEdit(role) && <Button size="sm" variant="primary" icon={<Plus className="size-3.5" />} onClick={() => { setEdit(undefined); setInitialType(undefined); setOpen(true) }}>{t('Add channel')}</Button>} />
      {list.length === 0 ? (
        <Empty icon={<Bell className="size-6" />} title={t('No channels yet')} body={t('Telegram, Bale, Discord, Slack, Teams, email, ntfy, Gotify, Pushover, Google Chat or any webhook.')}
          action={canEdit(role) && <Button variant="primary" onClick={() => { setInitialType('telegram'); setOpen(true) }}>{t('Set up Telegram')}</Button>} />
      ) : (
        <div className="px-3 pb-3">
          {list.map((nt) => (
            <div key={nt.id} className="flex items-center gap-3 rounded-xl px-3 py-3 hover:bg-surface-2">
              <span className="grid place-items-center size-9 rounded-xl bg-surface-2 border border-line text-muted"><Bell className="size-4" /></span>
              <div className="min-w-0 flex-1">
                <p className="font-medium truncate" dir="auto">{nt.name}</p>
                <p className="text-[12.5px] text-muted">{name(nt.type)}{nt.config.language === 'fa' ? ' · فارسی' : ''}</p>
              </div>
              {nt.isDefault && <Badge tone="accent">{t('Default')}</Badge>}
              {canEdit(role) && (
                <Menu trigger={(tr) => <IconButton {...tr} size="sm" label={t('Channel actions')}><Ellipsis className="size-4" /></IconButton>} items={[
                  { label: t('Edit'), icon: <Pencil />, onClick: () => { setEdit(nt); setOpen(true) } },
                  { label: t('Send test'), icon: <Send />, onClick: async () => {
                    try { await api.post('/api/notifications/test', { type: nt.type, config: nt.config }); toast.success(t('Test sent')) } catch (e) { toast.error(t('Not delivered'), (e as Error).message) }
                  } },
                  { divider: true, label: '' },
                  { label: t('Delete'), icon: <Trash2 />, danger: true, onClick: async () => {
                    if (!(await confirm({ title: t('Delete “{name}”?', { name: nt.name }), body: t('Monitors using it stop sending there.'), confirmLabel: t('Delete'), danger: true }))) return
                    await api.del(`/api/notifications/${nt.id}`)
                    useApp.getState().loadNotifications()
                  } },
                ]} />
              )}
            </div>
          ))}
        </div>
      )}
      <NotificationDialog open={open} onClose={() => setOpen(false)} initial={edit} initialType={initialType} />
    </Card>
  )
}
