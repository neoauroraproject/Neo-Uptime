import { Link } from 'react-router-dom'
import { ArrowDown, ArrowUp, Cpu, Gauge, HardDrive, MapPin, MemoryStick, Network, Pencil, Server, TriangleAlert } from 'lucide-react'
import { bytes, clsx, duration, n, pct, rate } from '../lib/format'
import { useT } from '../lib/i18n'
import type { AgentInfo, MonitorView, PanelSystem, Tunnel } from '../lib/types'
import { useApp } from '../lib/store'
import { HeartbeatBar, Meter, Sparkline } from './charts'
import { Badge, StatusPill } from './ui'
import { Flag, countryName } from './flag'
import { SourceBadge, tunnelSources } from './meta'

export function TunnelBadges({ t, className }: { t: { direction?: string; carrier?: string; transport?: string; preset?: string; tunnelPort?: string }; className?: string }) {
  return (
    <div className={clsx('flex flex-wrap items-center gap-1.5', className)} dir="ltr">
      {t.direction && <Badge>{t.direction}</Badge>}
      {(t.carrier || t.transport) && <Badge>{t.carrier || t.transport}</Badge>}
      {t.preset && <Badge tone="accent">{t.preset}</Badge>}
      {t.tunnelPort && <Badge tone="outline" mono>{t.tunnelPort}</Badge>}
    </div>
  )
}

/** The card on the Tunnels page — BackPack's monitor card, plus history. */
export function TunnelCard({ m, via }: { m: MonitorView; via?: string }) {
  const t = useT()
  const beats = useApp((s) => s.beats[m.id]) ?? []
  const tn = m.tunnel
  const pings = beats.slice(-60).map((b) => (b.status === 1 ? b.ping : null))
  const down = m.active && m.last?.status === 0
  const cur = m.last?.status === 1 ? m.last.ping : null
  const rates = tn?.rates ?? []
  const lastRate = rates.length ? rates[rates.length - 1].in + rates[rates.length - 1].out : null
  const country = m.country || tn?.peerCountry?.toLowerCase()
  const place = [tn?.peerLocation || (country ? countryName(country) : ''), tn?.peerISP].filter(Boolean).join(' · ')
  return (
    <article className={clsx('card relative overflow-hidden flex flex-col transition-[border-color] duration-200 hover:border-line-strong', down && 'border-down/40')}>
      <div className="absolute inset-0 dot-grid opacity-50 [mask-image:linear-gradient(to_bottom,black,transparent_70%)]" aria-hidden />
      <div
        className="absolute inset-x-0 bottom-0 h-40 pointer-events-none"
        style={{ background: `radial-gradient(80% 100% at 70% 100%, color-mix(in srgb, ${down ? 'var(--down)' : 'var(--up)'} 14%, transparent), transparent)` }}
        aria-hidden
      />
      <div className="relative p-5 pb-0">
        <div className="flex items-start gap-3">
          <Flag cc={country} size={36} className="mt-0.5 rounded-md" title />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 min-w-0">
              <Link to={`/dashboard/${m.id}`} className="font-display text-lg font-semibold leading-tight hover:text-accent-ink truncate" dir="auto">{m.name}</Link>
              <SourceBadge source={tn?.source ?? (m.type === 'backpack-tunnel' ? 'backpack' : m.config.source)} />
            </div>
            <p className="text-[12.5px] text-muted truncate">{place || via || m.target}</p>
          </div>
          <StatusPill status={m.last?.status} active={m.active} size="sm" />
        </div>
        {tn && <TunnelBadges t={tn} className="mt-4" />}
      </div>
      <div className="relative grid grid-cols-[auto_1fr] items-end gap-4 px-5 pt-4">
        <div className="pb-1">
          <p className="font-display text-[52px] leading-none font-semibold tnum tracking-tight whitespace-nowrap" dir="ltr">
            {cur != null ? n(Math.round(cur)) : '—'}
            {cur != null && <span className="text-lg font-medium text-muted ms-1">ms</span>}
          </p>
          <p className="mt-2 text-[13px] text-muted tnum whitespace-nowrap">
            {bytes(tn?.totalBytes)}{tn?.uptime ? ` · ${t('up {d}', { d: tn.uptime })}` : ''}
          </p>
        </div>
        <Sparkline values={pings} height={84} color={down ? 'var(--down)' : 'var(--chart)'} />
      </div>
      {tn?.lastError && m.last?.status !== 1 && (
        <p className="relative mx-5 mt-3 flex items-start gap-1.5 text-[12px] text-down"><TriangleAlert className="size-3.5 shrink-0 mt-0.5" /><span className="line-clamp-2" dir="auto">{tn.lastError}</span></p>
      )}
      <div className="relative mt-4 border-t border-line px-5 py-3.5 space-y-3 bg-surface/60">
        <HeartbeatBar beats={beats} height={18} barWidth={4} gap={2} maxBeats={60} />
        <div className="flex items-center justify-between gap-3 text-[12.5px]">
          <div className="flex items-center gap-3 tnum">
            <span className="inline-flex items-center gap-1 text-muted"><ArrowDown className="size-3.5 text-up" />{bytes(tn?.inBytes)}</span>
            <span className="inline-flex items-center gap-1 text-muted"><ArrowUp className="size-3.5 text-maint" />{bytes(tn?.outBytes)}</span>
            {lastRate != null && lastRate > 0 && <span className="hidden sm:inline text-subtle">{rate(lastRate)}</span>}
          </div>
          <div className="flex items-center gap-2">
            <span className="tnum font-semibold" title={t('Uptime, last 24 hours')}>{pct(m.uptime24, 1)}</span>
            <Link to={`/edit/${m.id}`} aria-label={t('Edit {name}', { name: m.name })} className="grid place-items-center size-8 rounded-lg text-muted hover:text-fg hover:bg-surface-2"><Pencil className="size-3.5" /></Link>
          </div>
        </div>
      </div>
    </article>
  )
}

function Stat({ label, value, hint }: { label: string; value: React.ReactNode; hint?: string }) {
  return (
    <div className="rounded-2xl bg-surface-2 border border-line px-4 py-3 min-w-0">
      <p className="text-[12px] text-muted truncate">{label}</p>
      <p className="mt-1 font-display text-lg font-semibold tnum truncate" title={hint}>{value}</p>
    </div>
  )
}

/** Tunnel details on the monitor page. */
export function TunnelDetails({ t: tn }: { t: Tunnel }) {
  const t = useT()
  const rates = tn.rates ?? []
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2"><SourceBadge source={tn.source} /><TunnelBadges t={tn} /></div>
        {(tn.peerLocation || tn.peerISP) && (
          <span className="inline-flex items-center gap-1.5 text-[13px] text-muted"><MapPin className="size-3.5" />{[tn.peerLocation, tn.peerISP].filter(Boolean).join(' · ')}</span>
        )}
      </div>
      {tn.serviceDown && (
        <p className="rounded-xl border border-pending/30 bg-pending/8 px-3 py-2 text-[13px] text-pending">{t('Forwarded service is refusing connections: {why}', { why: tn.serviceDown })}</p>
      )}
      {tn.lastError && (
        <p className="rounded-xl border border-down/30 bg-down/8 px-3 py-2 text-[13px] text-down" dir="auto">{t('Last error')}: {tn.lastError}</p>
      )}
      <div className="grid grid-cols-2 @3xl:grid-cols-4 gap-3">
        <Stat label={t('Total traffic')} value={bytes(tn.totalBytes)} />
        <Stat label={t('Download')} value={bytes(tn.inBytes)} />
        <Stat label={t('Upload')} value={bytes(tn.outBytes)} />
        <Stat label={t('Connected for')} value={tn.uptime || '—'} />
        {tn.kcpLossPercent != null && tn.kcpLossPercent > 0 && <Stat label={t('KCP loss')} value={pct(tn.kcpLossPercent / 100)} hint={t('Share of sent traffic that needed resending')} />}
        {tn.pool && <Stat label={t('Sessions')} value={<span dir="ltr">{n(tn.pool.live)}/{n(tn.pool.target)}</span>} />}
        {tn.quotaLimit ? <Stat label={t('Quota')} value={`${bytes(tn.quotaUsed ?? tn.totalBytes)} / ${bytes(tn.quotaLimit)}`} /> : null}
        {tn.ports && <Stat label={t('Forwarded ports')} value={<span className="font-mono text-base" dir="ltr">{tn.ports}</span>} />}
      </div>
      {rates.length > 1 && (
        <div>
          <div className="flex items-center justify-between text-[13px] mb-2">
            <span className="text-muted inline-flex items-center gap-1.5"><Gauge className="size-3.5" />{t('Live throughput (from BackPack)')}</span>
            <span className="tnum font-medium">{rate(rates[rates.length - 1].in + rates[rates.length - 1].out)}</span>
          </div>
          <Sparkline values={rates.map((r) => r.in + r.out)} height={70} color="var(--maint)" />
        </div>
      )}
    </div>
  )
}

export function SystemPanel({ s }: { s: PanelSystem }) {
  const t = useT()
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-1 text-[13px] text-muted">
        <span className="inline-flex items-center gap-1.5"><Server className="size-3.5" />{s.hostname}</span>
        {s.os && <span>{s.os}</span>}
        {s.uptime && <span>{t('up {d}', { d: s.uptime })}</span>}
        {s.version && <Badge tone="accent">BackPack {s.version}</Badge>}
      </div>
      <div className="grid @2xl:grid-cols-3 gap-5">
        <div className="flex gap-3"><Cpu className="size-4 text-muted mt-0.5" /><div className="flex-1"><Meter value={s.cpuPercent} label={t('CPU')} sub={s.cpuCores ? t('{n} cores', { n: s.cpuCores }) : undefined} /></div></div>
        <div className="flex gap-3"><MemoryStick className="size-4 text-muted mt-0.5" /><div className="flex-1"><Meter value={s.memPercent} label={t('Memory')} sub={s.memTotal || undefined} /></div></div>
        <div className="flex gap-3"><HardDrive className="size-4 text-muted mt-0.5" /><div className="flex-1"><Meter value={s.diskPercent} label={t('Disk')} sub={s.diskTotal || undefined} /></div></div>
      </div>
      <div className="grid grid-cols-2 @3xl:grid-cols-4 gap-3">
        <Stat label={t('Tunnels running')} value={<span dir="ltr">{n(s.tunnelsRunning)}/{n(s.tunnelsTotal)}</span>} />
        <Stat label={t('Received')} value={bytes(s.totalRecvBytes)} />
        <Stat label={t('Sent')} value={bytes(s.totalSentBytes)} />
        <Stat label={t('Location')} value={<span className="text-base">{s.location || '—'}</span>} />
      </div>
    </div>
  )
}

/** A server reporting through neo-uptime-agent. */
export function AgentPanel({ a }: { a: AgentInfo }) {
  const t = useT()
  const s = a.system
  const memPct = s.memTotal ? (100 * s.memUsed) / s.memTotal : 0
  const diskPct = s.diskTotal ? (100 * s.diskUsed) / s.diskTotal : 0
  const sources = tunnelSources.map((name) => [name, a[name]] as const).filter(([, x]) => x)
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-1 text-[13px] text-muted">
        <span className="inline-flex items-center gap-1.5"><Server className="size-3.5" /><span dir="ltr">{s.hostname}</span></span>
        {s.os && <span dir="ltr">{s.os}</span>}
        {s.uptimeSecs > 0 && <span>{t('up {d}', { d: duration(s.uptimeSecs) })}</span>}
        {a.ip && <span className="font-mono" dir="ltr">{a.ip}</span>}
        {a.version && <Badge tone="accent">agent {a.version}</Badge>}
      </div>
      <div className="grid @2xl:grid-cols-3 gap-5">
        <div className="flex gap-3"><Cpu className="size-4 text-muted mt-0.5" /><div className="flex-1"><Meter value={s.cpuPercent} label={t('CPU')} sub={t('{n} cores', { n: s.cpuCores })} /></div></div>
        <div className="flex gap-3"><MemoryStick className="size-4 text-muted mt-0.5" /><div className="flex-1"><Meter value={memPct} label={t('Memory')} sub={bytes(s.memTotal)} /></div></div>
        <div className="flex gap-3"><HardDrive className="size-4 text-muted mt-0.5" /><div className="flex-1"><Meter value={diskPct} label={t('Disk')} sub={bytes(s.diskTotal)} /></div></div>
      </div>
      <div className="grid grid-cols-2 @3xl:grid-cols-4 gap-3">
        <Stat label={t('Download now')} value={rate(s.netRxRate)} />
        <Stat label={t('Upload now')} value={rate(s.netTxRate)} />
        <Stat label={t('Load')} value={<span dir="ltr">{n(s.load1, 2)} · {n(s.load5, 2)} · {n(s.load15, 2)}</span>} />
        <Stat label={t('TCP connections')} value={<span className="inline-flex items-center gap-1.5"><Network className="size-4 text-muted" />{n(s.tcpConns)}</span>} />
      </div>
      {sources.length > 0 && (
        <div className="grid @xl:grid-cols-2 gap-3">
          {sources.map(([name, src]) => (
            <div key={name} className="rounded-2xl border border-line bg-surface-2 px-4 py-3 flex items-center gap-3 min-w-0">
              <SourceBadge source={name} />
              <span className="text-sm flex-1 truncate">{t('{online} of {total} tunnels online', { online: src!.online, total: src!.tunnels })}</span>
              {src!.version && <span className="text-[12px] text-subtle font-mono" dir="ltr">{src!.version}</span>}
              {src!.error && <span className="text-[12px] text-down truncate max-w-48" title={src!.error}>{src!.error}</span>}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
