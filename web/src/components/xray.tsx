import { useState } from 'react'
import { Link } from 'react-router-dom'
import { MapPin, Pencil, Play, TriangleAlert, Zap } from 'lucide-react'
import { api } from '../lib/api'
import { canEdit, useApp } from '../lib/store'
import { clsx, n, pct } from '../lib/format'
import { useT } from '../lib/i18n'
import type { MonitorView, XrayInfo } from '../lib/types'
import { HeartbeatBar, Sparkline } from './charts'
import { Badge, IconButton, Spinner, StatusPill, toast } from './ui'
import { Flag } from './flag'

const protoLabel: Record<string, string> = { vless: 'VLESS', vmess: 'VMess', trojan: 'Trojan', shadowsocks: 'Shadowsocks', hysteria: 'Hysteria2' }

/** Protocol, transport and security of a config as small badges. */
export function XrayBadges({ x, className }: { x: XrayInfo; className?: string }) {
  return (
    <div className={clsx('flex flex-wrap items-center gap-1.5', className)} dir="ltr">
      <Badge tone="accent">{protoLabel[x.protocol] ?? x.protocol.toUpperCase()}</Badge>
      {x.network && x.network !== 'tcp' && x.network !== 'hysteria' && <Badge>{x.network.toUpperCase()}</Badge>}
      {x.security && x.security !== 'none' && <Badge>{x.security.toUpperCase()}</Badge>}
    </div>
  )
}

/** Runs a config's test now and says how it went. */
export async function testNow(m: MonitorView, t: ReturnType<typeof useT>) {
  const r = await api.post<{ ok: boolean; ping?: number; connect?: number; error?: string }>('/api/xray/test', { monitorId: m.id })
  if (r.ok) toast.success(t('{name} works', { name: m.name }), t('Connected in {c} ms · {p} ms once connected', { c: Math.round(r.connect ?? 0), p: Math.round(r.ping ?? 0) }))
  else toast.error(t('{name} does not connect', { name: m.name }), r.error)
}

/** The card on the Configs page: one V2Ray config, tested for real. */
export function ConfigCard({ m }: { m: MonitorView }) {
  const t = useT()
  const role = useApp((s) => s.role)
  const beats = useApp((s) => s.beats[m.id]) ?? []
  const [testing, setTesting] = useState(false)
  const x = m.xray
  const pings = beats.slice(-60).map((b) => (b.status === 1 ? b.ping : null))
  const gone = !!m.config.gone
  const down = m.active && m.last?.status === 0
  const cur = m.last?.status === 1 ? m.last.ping : null
  const msg = m.last && m.last.status !== 1 ? m.last.msg : ''
  const run = async () => {
    setTesting(true)
    try {
      await testNow(m, t)
    } catch (e) {
      toast.error(t('Test failed'), (e as Error).message)
    } finally {
      setTesting(false)
    }
  }
  return (
    <article className={clsx('card relative overflow-hidden flex flex-col transition-[border-color] duration-200 hover:border-line-strong', down && 'border-down/40', gone && 'opacity-60')}>
      <div className="absolute inset-0 dot-grid opacity-40 [mask-image:linear-gradient(to_bottom,black,transparent_70%)]" aria-hidden />
      <div className="absolute inset-x-0 bottom-0 h-36 pointer-events-none" aria-hidden
        style={{ background: `radial-gradient(80% 100% at 70% 100%, color-mix(in srgb, ${down ? 'var(--down)' : 'var(--up)'} 12%, transparent), transparent)` }} />
      <div className="relative p-5 pb-0">
        <div className="flex items-start gap-3">
          <Flag cc={m.country} size={32} className="mt-0.5 rounded-md" title />
          <div className="min-w-0 flex-1">
            <Link to={`/dashboard/${m.id}`} className="block font-display text-[17px] font-semibold leading-tight hover:text-accent-ink truncate" dir="auto">{m.name}</Link>
            <p className="text-[12px] text-muted truncate font-mono mt-0.5" dir="ltr">{x?.endpoint}</p>
          </div>
          {gone ? <Badge tone="pending">{t('Removed from the subscription')}</Badge> : <StatusPill status={m.last?.status} active={m.active} size="sm" />}
        </div>
        {x && <XrayBadges x={x} className="mt-3.5" />}
      </div>
      <div className="relative grid grid-cols-[auto_1fr] items-end gap-4 px-5 pt-4">
        <div className="pb-1">
          <p className="font-display text-[46px] leading-none font-semibold tnum tracking-tight whitespace-nowrap" dir="ltr">
            {cur != null ? n(Math.round(cur)) : '—'}
            {cur != null && <span className="text-base font-medium text-muted ms-1">ms</span>}
          </p>
          <p className="mt-2 text-[12.5px] text-muted tnum whitespace-nowrap">
            {x?.connect && m.last?.status === 1 ? t('connects in {ms} ms', { ms: n(Math.round(x.connect)) }) : t('real delay')}
          </p>
        </div>
        <Sparkline values={pings} height={76} color={down ? 'var(--down)' : 'var(--chart)'} />
      </div>
      {msg && !gone && (
        <p className="relative mx-5 mt-3 flex items-start gap-1.5 text-[12px] text-down"><TriangleAlert className="size-3.5 shrink-0 mt-0.5" /><span className="line-clamp-2" dir="auto">{msg}</span></p>
      )}
      <div className="relative mt-4 border-t border-line px-5 py-3.5 space-y-3 bg-surface/60">
        <HeartbeatBar beats={beats} height={18} barWidth={4} gap={2} maxBeats={60} />
        <div className="flex items-center justify-between gap-3 text-[12.5px]">
          <span className="inline-flex items-center gap-1.5 text-muted min-w-0">
            <MapPin className="size-3.5 shrink-0" />
            <span className="truncate" dir="auto">{x?.from ? t('Tested from {name}', { name: x.from }) : t('Tested from this panel')}</span>
          </span>
          <div className="flex items-center gap-1">
            <span className="tnum font-semibold me-1" title={t('Uptime, last 24 hours')}>{pct(m.uptime24, 1)}</span>
            {canEdit(role) && !gone && (
              <IconButton size="sm" label={t('Test now')} onClick={run} disabled={testing}>{testing ? <Spinner className="size-3.5" /> : <Play className="size-3.5" />}</IconButton>
            )}
            {canEdit(role) && <Link to={`/edit/${m.id}`} aria-label={t('Edit {name}', { name: m.name })} className="grid place-items-center size-8 rounded-lg text-muted hover:text-fg hover:bg-surface-2"><Pencil className="size-3.5" /></Link>}
          </div>
        </div>
      </div>
    </article>
  )
}

/** Config details on the monitor page. */
export function XrayDetails({ m }: { m: MonitorView }) {
  const t = useT()
  const role = useApp((s) => s.role)
  const [testing, setTesting] = useState(false)
  if (!m.xray) return null
  const x = m.xray
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <XrayBadges x={x} />
        <span className="text-[13px] text-muted font-mono" dir="ltr">{x.endpoint}</span>
        <span className="inline-flex items-center gap-1.5 text-[13px] text-muted"><MapPin className="size-3.5" />{x.from ? t('Tested from {name}', { name: x.from }) : t('Tested from this panel')}</span>
        {x.connect ? <span className="inline-flex items-center gap-1.5 text-[13px] text-muted"><Zap className="size-3.5" />{t('connects in {ms} ms', { ms: n(Math.round(x.connect)) })}</span> : null}
      </div>
      {canEdit(role) && (
        <button type="button" disabled={testing} onClick={async () => { setTesting(true); try { await testNow(m, t) } catch (e) { toast.error(t('Test failed'), (e as Error).message) } finally { setTesting(false) } }}
          className="inline-flex items-center gap-1.5 h-8 px-3 rounded-lg border border-line text-[13px] font-medium hover:bg-surface-2 disabled:opacity-60">
          {testing ? <Spinner className="size-3.5" /> : <Play className="size-3.5" />}{t('Test now')}
        </button>
      )}
    </div>
  )
}
