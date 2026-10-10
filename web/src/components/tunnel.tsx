import { Link } from 'react-router-dom'
import { ArrowDown, ArrowUp, Cpu, Gauge, HardDrive, MapPin, MemoryStick, Pencil, Server } from 'lucide-react'
import { bytes, clsx, pct, rate } from '../lib/format'
import type { MonitorView, PanelSystem, Tunnel } from '../lib/types'
import { useApp } from '../lib/store'
import { HeartbeatBar, Meter, Sparkline } from './charts'
import { Badge, StatusPill } from './ui'

export function TunnelBadges({ t, className }: { t: { direction?: string; carrier?: string; transport?: string; preset?: string; tunnelPort?: string }; className?: string }) {
  return (
    <div className={clsx('flex flex-wrap items-center gap-1.5', className)}>
      {t.direction && <Badge>{t.direction}</Badge>}
      {(t.carrier || t.transport) && <Badge>{t.carrier || t.transport}</Badge>}
      {t.preset && <Badge tone="accent">{t.preset}</Badge>}
      {t.tunnelPort && <Badge tone="outline" mono>{t.tunnelPort}</Badge>}
    </div>
  )
}

export function CountryChip({ cc, name }: { cc?: string; name: string }) {
  return (
    <span className="grid place-items-center size-10 shrink-0 rounded-xl border border-line-strong bg-surface-2 font-display text-[13px] font-semibold tracking-wide">
      {(cc || name.replace(/[^A-Za-z]/g, '').slice(0, 2) || '··').toUpperCase().slice(0, 2)}
    </span>
  )
}

/** The card on the Tunnels page — BackPack's monitor card, plus history. */
export function TunnelCard({ m, panelName }: { m: MonitorView; panelName?: string }) {
  const beats = useApp((s) => s.beats[m.id]) ?? []
  const t = m.tunnel
  const pings = beats.slice(-60).map((b) => (b.status === 1 ? b.ping : null))
  const down = m.active && m.last?.status === 0
  const cur = m.last?.status === 1 ? m.last.ping : null
  const rates = t?.rates ?? []
  const lastRate = rates.length ? rates[rates.length - 1].in + rates[rates.length - 1].out : null
  return (
    <article className={clsx('card relative overflow-hidden flex flex-col transition-[border-color,transform] duration-200 hover:border-line-strong', down && 'border-down/40')}>
      <div className="absolute inset-0 dot-grid opacity-50 [mask-image:linear-gradient(to_bottom,black,transparent_70%)]" aria-hidden />
      <div
        className="absolute inset-x-0 bottom-0 h-40 pointer-events-none"
        style={{ background: `radial-gradient(80% 100% at 70% 100%, color-mix(in srgb, ${down ? 'var(--down)' : 'var(--up)'} 14%, transparent), transparent)` }}
        aria-hidden
      />
      <div className="relative p-5 pb-0">
        <div className="flex items-start gap-3">
          <CountryChip cc={t?.peerCountry} name={m.name} />
          <div className="min-w-0 flex-1">
            <Link to={`/dashboard/${m.id}`} className="font-display text-lg font-semibold leading-tight hover:text-accent-ink">{m.name}</Link>
            <p className="text-[12.5px] text-muted truncate">{[t?.peerLocation, t?.peerISP].filter(Boolean).join(' · ') || panelName || m.target}</p>
          </div>
          <StatusPill status={m.last?.status} active={m.active} size="sm" />
        </div>
        {t && <TunnelBadges t={t} className="mt-4" />}
      </div>
      <div className="relative grid grid-cols-[auto_1fr] items-end gap-4 px-5 pt-4">
        <div className="pb-1">
          <p className="font-display text-[52px] leading-none font-semibold tnum tracking-tight whitespace-nowrap">
            {cur != null ? Math.round(cur) : '—'}
            {cur != null && <span className="text-lg font-medium text-muted ml-1">ms</span>}
          </p>
          <p className="mt-2 text-[13px] text-muted tnum whitespace-nowrap">
            {bytes(t?.totalBytes)}{t?.uptime ? ` · up ${t.uptime}` : ''}
          </p>
        </div>
        <Sparkline values={pings} height={84} color={down ? 'var(--down)' : 'var(--chart)'} />
      </div>
      <div className="relative mt-4 border-t border-line px-5 py-3.5 space-y-3 bg-surface/60">
        <HeartbeatBar beats={beats} height={18} barWidth={4} gap={2} maxBeats={60} />
        <div className="flex items-center justify-between gap-3 text-[12.5px]">
          <div className="flex items-center gap-3 tnum">
            <span className="inline-flex items-center gap-1 text-muted"><ArrowDown className="size-3.5 text-up" />{bytes(t?.inBytes)}</span>
            <span className="inline-flex items-center gap-1 text-muted"><ArrowUp className="size-3.5 text-maint" />{bytes(t?.outBytes)}</span>
            {lastRate != null && lastRate > 0 && <span className="hidden sm:inline text-subtle">{rate(lastRate)}</span>}
          </div>
          <div className="flex items-center gap-2">
            <span className="tnum font-semibold" title="Uptime, last 24 hours">{pct(m.uptime24, 1)}</span>
            <Link to={`/edit/${m.id}`} aria-label={`Edit ${m.name}`} className="grid place-items-center size-8 rounded-lg text-muted hover:text-fg hover:bg-surface-2"><Pencil className="size-3.5" /></Link>
          </div>
        </div>
      </div>
    </article>
  )
}

function Stat({ label, value, hint }: { label: string; value: React.ReactNode; hint?: string }) {
  return (
    <div className="rounded-2xl bg-surface-2 border border-line px-4 py-3">
      <p className="text-[12px] text-muted">{label}</p>
      <p className="mt-1 font-display text-lg font-semibold tnum" title={hint}>{value}</p>
    </div>
  )
}

/** Tunnel details on the monitor page. */
export function TunnelDetails({ t }: { t: Tunnel }) {
  const rates = t.rates ?? []
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <TunnelBadges t={t} />
        {(t.peerLocation || t.peerISP) && (
          <span className="inline-flex items-center gap-1.5 text-[13px] text-muted"><MapPin className="size-3.5" />{[t.peerLocation, t.peerISP].filter(Boolean).join(' · ')}</span>
        )}
      </div>
      {t.serviceDown && (
        <p className="rounded-xl border border-pending/30 bg-pending/8 px-3 py-2 text-[13px] text-pending">Forwarded service is refusing connections: {t.serviceDown}</p>
      )}
      <div className="grid grid-cols-2 @3xl:grid-cols-4 gap-3">
        <Stat label="Total traffic" value={bytes(t.totalBytes)} />
        <Stat label="Download ↓" value={bytes(t.inBytes)} />
        <Stat label="Upload ↑" value={bytes(t.outBytes)} />
        <Stat label="Up for" value={t.uptime || '—'} />
        {t.kcpLossPercent != null && t.kcpLossPercent > 0 && <Stat label="KCP loss" value={`${t.kcpLossPercent.toFixed(2)}%`} hint="Share of sent traffic that needed resending" />}
        {t.pool && <Stat label="Pool" value={`${t.pool.live}/${t.pool.target}`} hint={`configured ${t.pool.configured}, ${t.pool.mbps} Mbps`} />}
        {t.quotaLimit ? <Stat label="Quota" value={`${bytes(t.totalBytes)} / ${bytes(t.quotaLimit)}`} /> : null}
        {t.ports && <Stat label="Forwarded ports" value={<span className="font-mono text-base">{t.ports}</span>} />}
      </div>
      {rates.length > 1 && (
        <div>
          <div className="flex items-center justify-between text-[13px] mb-2">
            <span className="text-muted inline-flex items-center gap-1.5"><Gauge className="size-3.5" />Live throughput (from BackPack)</span>
            <span className="tnum font-medium">{rate(rates[rates.length - 1].in + rates[rates.length - 1].out)}</span>
          </div>
          <Sparkline values={rates.map((r) => r.in + r.out)} height={70} color="var(--maint)" />
        </div>
      )}
    </div>
  )
}

export function SystemPanel({ s }: { s: PanelSystem }) {
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-1 text-[13px] text-muted">
        <span className="inline-flex items-center gap-1.5"><Server className="size-3.5" />{s.hostname}</span>
        {s.os && <span>{s.os}</span>}
        {s.uptime && <span>up {s.uptime}</span>}
        {s.version && <Badge tone="accent">BackPack {s.version}</Badge>}
      </div>
      <div className="grid @2xl:grid-cols-3 gap-5">
        <div className="flex gap-3"><Cpu className="size-4 text-muted mt-0.5" /><div className="flex-1"><Meter value={s.cpuPercent} label="CPU" sub={s.cpuCores ? `${s.cpuCores} cores` : undefined} /></div></div>
        <div className="flex gap-3"><MemoryStick className="size-4 text-muted mt-0.5" /><div className="flex-1"><Meter value={s.memPercent} label="Memory" sub={s.memTotal || undefined} /></div></div>
        <div className="flex gap-3"><HardDrive className="size-4 text-muted mt-0.5" /><div className="flex-1"><Meter value={s.diskPercent} label="Disk" sub={s.diskTotal || undefined} /></div></div>
      </div>
      <div className="grid grid-cols-2 @3xl:grid-cols-4 gap-3">
        <Stat label="Tunnels running" value={`${s.tunnelsRunning}/${s.tunnelsTotal}`} />
        <Stat label="Received" value={bytes(s.totalRecvBytes)} />
        <Stat label="Sent" value={bytes(s.totalSentBytes)} />
        <Stat label="Location" value={<span className="text-base">{s.location || '—'}</span>} />
      </div>
    </div>
  )
}

