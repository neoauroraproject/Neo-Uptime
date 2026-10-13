import { useEffect, useState } from 'react'
import { ArrowDown, ArrowUp, Gauge, Zap } from 'lucide-react'
import { api } from '../lib/api'
import { useApp } from '../lib/store'
import { n, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import type { MonitorView, SpeedResult } from '../lib/types'
import { Alert, Button, Field, Modal, Select, Spinner, useNow } from './ui'

/** Mbps, with a decimal below 100. */
export const mbps = (v: number) => n(v >= 100 ? Math.round(v) : Math.round(v * 10) / 10, v >= 100 ? 0 : 1)

function Stat({ icon, label, value, unit }: { icon: React.ReactNode; label: string; value: string; unit: string }) {
  return (
    <div className="rounded-2xl bg-surface-2 border border-line px-4 py-3 min-w-0">
      <p className="flex items-center gap-1.5 text-[12px] text-muted">{icon}{label}</p>
      <p className="mt-1.5 font-display text-[30px] leading-none font-semibold tnum" dir="ltr">{value}<span className="text-sm font-medium text-muted ms-1">{unit}</span></p>
    </div>
  )
}

/** One-line summary of the last speed test, for cards. */
export function SpeedLine({ s }: { s: SpeedResult }) {
  const t = useT()
  const now = useNow(60_000)
  return (
    <span className="tnum" dir="auto">
      <span dir="ltr">↓ {mbps(s.down)} · ↑ {s.up > 0 ? mbps(s.up) : '—'} Mbps</span> · {timeAgo(s.at, now)}
      <span className="sr-only">{t('Speed test')}</span>
    </span>
  )
}

/**
 * Measures a config's real speed by hand: download and upload through the
 * config, from this panel or from one of the servers.
 */
export function SpeedDialog({ m, open, onClose }: { m: MonitorView; open: boolean; onClose: () => void }) {
  const t = useT()
  const agents = useApp((s) => s.agents)
  const [from, setFrom] = useState(m.config.agentId ?? 0)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [local, setLocal] = useState<SpeedResult | null>(null)
  const now = useNow(1000)
  const [started, setStarted] = useState(0)
  const res = local ?? m.speed ?? null

  useEffect(() => {
    if (open) {
      setFrom(m.config.agentId ?? 0)
      setErr('')
      setLocal(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const run = async () => {
    setBusy(true)
    setErr('')
    setStarted(Date.now())
    try {
      const r = await api.post<{ ok: boolean; error?: string; speed?: SpeedResult }>('/api/xray/speed', { monitorId: m.id, agentId: from })
      if (!r.ok || !r.speed) {
        setErr(r.error || t('The speed test failed.'))
        return
      }
      setLocal(r.speed)
      useApp.setState((s) => (s.monitors[m.id] ? { monitors: { ...s.monitors, [m.id]: { ...s.monitors[m.id], speed: r.speed } } } : s))
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open={open} onClose={busy ? () => {} : onClose} size="md" title={t('Speed test — {name}', { name: m.name })}
      description={t('Downloads and uploads real data through this config and measures how fast it goes.')}
      footer={<><Button variant="ghost" onClick={onClose} disabled={busy}>{t('Close')}</Button><Button variant="primary" loading={busy} icon={<Gauge className="size-4" />} onClick={run}>{res ? t('Test again') : t('Start the test')}</Button></>}>
      <div className="space-y-4">
        <Field label={t('Test from')} htmlFor="spf" help={t('Pick a server inside the country to see the speed your users there get.')}>
          <Select id="spf" value={from} disabled={busy} onChange={(e) => { setFrom(Number(e.target.value)); setLocal(null) }}>
            <option value={0}>{t('This panel’s server')}</option>
            {agents.map((a) => <option key={a.id} value={a.id}>{a.name}{a.online ? '' : ` (${t('offline')})`}</option>)}
          </Select>
        </Field>

        {busy ? (
          <div className="flex items-center gap-3 rounded-2xl border border-dashed border-line-strong p-4">
            <Spinner className="size-5" />
            <div>
              <p className="font-medium">{t('Measuring…')}</p>
              <p className="text-[13px] text-muted tnum">{t('Download first, then upload. About 20 seconds, {s} so far.', { s: Math.max(0, Math.round((now - started) / 1000)) })}</p>
            </div>
          </div>
        ) : err ? (
          <Alert tone="danger" title={t('The speed test failed')}>{err}</Alert>
        ) : res ? (
          <div className="space-y-3 fade-up">
            <div className="grid grid-cols-3 gap-3">
              <Stat icon={<ArrowDown className="size-3.5 text-up" />} label={t('Download')} value={mbps(res.down)} unit="Mbps" />
              <Stat icon={<ArrowUp className="size-3.5 text-maint" />} label={t('Upload')} value={res.up > 0 ? mbps(res.up) : '—'} unit={res.up > 0 ? 'Mbps' : ''} />
              <Stat icon={<Zap className="size-3.5" />} label={t('Ping')} value={n(Math.round(res.ping))} unit="ms" />
            </div>
            {res.upError && <p className="text-[12.5px] text-pending">{t('Upload was not measured: {why}', { why: res.upError })}</p>}
            <p className="text-[12.5px] text-muted tnum" dir="auto">
              {t('{ago} · from {name} · via {server} · {mb} MB moved', {
                ago: timeAgo(res.at, now), name: res.from || t('this panel'), server: res.server, mb: n(Math.round(res.bytes / (1 << 20))),
              })}
            </p>
          </div>
        ) : (
          <p className="text-sm text-muted">{t('No speed test of this config yet.')}</p>
        )}

        <p className="text-[12.5px] text-subtle leading-relaxed">{t('A test moves up to a few hundred MB through the config, and counts against its traffic limit. It runs only when you press the button.')}</p>
      </div>
    </Modal>
  )
}
