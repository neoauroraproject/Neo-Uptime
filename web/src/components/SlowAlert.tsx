import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Bell, Pencil, Turtle } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { duration } from '../lib/format'
import { useT } from '../lib/i18n'
import type { Monitor, MonitorView } from '../lib/types'
import { Alert, Button, Card, Checkbox, Field, Input, Modal, Select, Switch, toast } from './ui'

const holds = [0, 60, 120, 300, 600, 900, 1800, 3600]

/** "Slower than … ms for at least …" — the two numbers of a slow alert. */
export function SlowAlertFields({ ms, forSec, onChange }: { ms: number; forSec: number; onChange: (ms: number, forSec: number) => void }) {
  const t = useT()
  return (
    <div className="grid sm:grid-cols-2 gap-4">
      <Field label={t('Slower than')} htmlFor="sa-ms">
        <div className="relative">
          <Input id="sa-ms" dir="ltr" type="number" min={1} max={600000} value={ms || ''} onChange={(e) => onChange(Number(e.target.value) || 0, forSec)} className="pe-12" />
          <span className="pointer-events-none absolute end-3 top-1/2 -translate-y-1/2 text-[13px] text-subtle">ms</span>
        </div>
      </Field>
      <Field label={t('For at least')} htmlFor="sa-for">
        <Select id="sa-for" value={forSec} onChange={(e) => onChange(ms, Number(e.target.value))}>
          {holds.map((s) => <option key={s} value={s}>{s === 0 ? t('Right away') : t('{n} minutes', { n: s / 60 })}</option>)}
        </Select>
      </Field>
    </div>
  )
}

/** Turns the slow alert of one monitor on, off or changes it. */
export function SlowAlertDialog({ open, onClose, monitorId }: { open: boolean; onClose: () => void; monitorId: number }) {
  const t = useT()
  const [m, setM] = useState<Monitor | null>(null)
  const [on, setOn] = useState(false)
  const [ms, setMs] = useState(300)
  const [forSec, setFor] = useState(120)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!open) return
    api.get<Monitor>(`/api/monitors/${monitorId}`).then((x) => {
      setM(x)
      setOn((x.config.latencyAlertMs ?? 0) > 0)
      setMs(x.config.latencyAlertMs || 300)
      setFor(x.config.latencyAlertMs ? x.config.latencyAlertFor ?? 0 : 120)
    }).catch(() => {})
  }, [open, monitorId])
  const save = async () => {
    if (!m) return
    setBusy(true)
    try {
      await api.put(`/api/monitors/${monitorId}`, { ...m, config: { ...m.config, latencyAlertMs: on ? ms : 0, latencyAlertFor: on ? forSec : 0 } })
      await useApp.getState().loadDashboard()
      toast.success(on ? t('Slow alert on') : t('Slow alert off'))
      onClose()
    } catch (e) {
      toast.error(t('Could not save'), (e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal open={open} onClose={onClose} size="sm" title={t('Slow alert')}
      footer={<><Button variant="ghost" onClick={onClose}>{t('Cancel')}</Button><Button variant="primary" loading={busy} disabled={!m || (on && ms <= 0)} onClick={save}>{t('Save')}</Button></>}>
      <div className="space-y-4">
        <Switch checked={on} onChange={setOn} label={t('Alert when it gets slow')} description={t('Up, but the ping or response time stays above a limit — common for tunnels before they drop.')} />
        {on && <SlowAlertFields ms={ms} forSec={forSec} onChange={(a, b) => { setMs(a); setFor(b) }} />}
        {on && <p className="text-[12.5px] text-muted">{t('You get one alert when it starts and one when the response time is back under the limit.')}</p>}
      </div>
    </Modal>
  )
}

/** Sets one slow alert on many tunnels at once, and the default for new ones. */
export function TunnelSlowAlertsDialog({ open, onClose, ids }: { open: boolean; onClose: () => void; ids: number[] }) {
  const t = useT()
  const role = useApp((s) => s.role)
  const [ms, setMs] = useState(300)
  const [forSec, setFor] = useState(120)
  const [forNew, setForNew] = useState(true)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!open || role !== 'admin') return
    api.get<Record<string, string>>('/api/settings').then((s) => {
      const d = Number(s.tunnelLatencyAlertMs)
      if (d > 0) {
        setMs(d)
        setFor(Number(s.tunnelLatencyAlertFor) || 0)
      }
    }).catch(() => {})
  }, [open, role])
  const apply = async (limit: number) => {
    setBusy(true)
    try {
      await api.post('/api/monitors/bulk', { action: 'latency', ids, latencyAlertMs: limit, latencyAlertFor: limit ? forSec : 0 })
      if (role === 'admin' && (forNew || limit === 0)) {
        await api.put('/api/settings', { tunnelLatencyAlertMs: String(forNew ? limit : 0), tunnelLatencyAlertFor: String(forSec) })
      }
      await useApp.getState().loadDashboard()
      toast.success(limit ? t('Slow alert set on {n} tunnels', { n: ids.length }) : t('Slow alert turned off on {n} tunnels', { n: ids.length }))
      onClose()
    } catch (e) {
      toast.error(t('Could not save'), (e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal open={open} onClose={onClose} size="md" title={t('Slow alerts for tunnels')}
      footer={<>
        <Button variant="ghost" className="me-auto" disabled={busy || !ids.length} onClick={() => apply(0)}>{t('Turn off for all')}</Button>
        <Button variant="ghost" onClick={onClose}>{t('Cancel')}</Button>
        <Button variant="primary" loading={busy} disabled={!ids.length || ms <= 0} onClick={() => apply(ms)}>{t('Apply to {n} tunnels', { n: ids.length })}</Button>
      </>}>
      <div className="space-y-4">
        <p className="text-sm text-muted">{t('Get a message when a tunnel stays slow — before it drops — and again when it is fast again. It uses each tunnel’s alert channels.')}</p>
        <SlowAlertFields ms={ms} forSec={forSec} onChange={(a, b) => { setMs(a); setFor(b) }} />
        {role === 'admin' && <Checkbox checked={forNew} onChange={setForNew} label={t('Also for tunnels added later')} />}
        {!ids.length && <Alert tone="info">{t('No tunnels yet')}</Alert>}
      </div>
    </Modal>
  )
}

/** The always-visible alert summary on a monitor's page. */
export function AlertsBox({ m }: { m: MonitorView }) {
  const t = useT()
  const role = useApp((s) => s.role)
  const notifications = useApp((s) => s.notifications)
  const [open, setOpen] = useState(false)
  const channels = notifications.filter((nt) => m.notificationIds.includes(nt.id)).map((nt) => nt.name)
  const slow = m.config.latencyAlertMs ?? 0
  const hasPing = m.type !== 'group' && m.type !== 'push'
  return (
    <Card className="px-5 py-3.5 flex flex-wrap items-center gap-x-6 gap-y-2.5 text-sm">
      <span className="inline-flex items-center gap-2 min-w-0">
        <Bell className="size-4 text-muted shrink-0" />
        <span className="text-muted">{t('Alerts to')}</span>
        {channels.length ? <span className="font-medium truncate" dir="auto">{channels.join('، ')}</span> : <span className="text-pending">{t('No channel — nobody is told')}</span>}
        {canEdit(role) && <Link to={`/edit/${m.id}?step=alerts`} className="text-accent-ink hover:underline text-[13px]">{t('Change')}</Link>}
      </span>
      {hasPing && (
        <span className="inline-flex items-center gap-2">
          <Turtle className="size-4 text-muted shrink-0" />
          <span className="text-muted">{t('Slow alert')}</span>
          {slow > 0
            ? <span className="font-medium">{t('above {ms} ms for {d}', { ms: slow, d: m.config.latencyAlertFor ? duration(m.config.latencyAlertFor) : t('Right away') })}</span>
            : <span className="text-subtle">{t('Off')}</span>}
          {canEdit(role) && (
            <Button size="sm" variant={slow > 0 ? 'ghost' : 'soft'} icon={slow > 0 ? <Pencil className="size-3.5" /> : <Turtle className="size-3.5" />} onClick={() => setOpen(true)}>
              {slow > 0 ? t('Change') : t('Set up')}
            </Button>
          )}
        </span>
      )}
      <SlowAlertDialog open={open} onClose={() => setOpen(false)} monitorId={m.id} />
    </Card>
  )
}
