import { useEffect, useId, useRef, useState } from 'react'
import { ArrowDown, ArrowUp, Activity, Gauge, Server } from 'lucide-react'
import { api } from '../lib/api'
import { useApp } from '../lib/store'
import { clsx, n, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import type { MonitorView, SpeedProgress, SpeedResult } from '../lib/types'
import { Alert, Field, Modal, Select, useNow, Button } from './ui'

/** Mbps, with a decimal below 100. */
export const mbps = (v: number) => n(v >= 100 ? Math.round(v) : Math.round(v * 10) / 10, v >= 100 ? 0 : 1)

const COLORS = { ping: '#fbbf24', download: '#22d3ee', upload: '#c084fc' }

/** One-line summary of the last speed test, for cards. */
export function SpeedLine({ s }: { s: SpeedResult }) {
  const t = useT()
  const now = useNow(60_000)
  return (
    <span className="inline-flex items-center gap-2 tnum">
      <span dir="ltr">↓ {mbps(s.down)} · ↑ {s.up > 0 ? mbps(s.up) : '—'} Mbps</span>
      <span className="text-subtle" dir="auto">{timeAgo(s.at, now)}</span>
      <span className="sr-only">{t('Speed test')}</span>
    </span>
  )
}

/* ---------- the gauge ---------- */

// Like the apps: the scale is not even, so that the speeds most lines
// have (a few to a hundred Mbps) get most of the dial.
const MARKS = [0, 1, 5, 10, 20, 30, 50, 75, 100, 250, 500, 1000]
function fraction(v: number) {
  if (v <= 0) return 0
  if (v >= 1000) return 1
  for (let i = 1; i < MARKS.length; i++) {
    if (v <= MARKS[i]) return (i - 1 + (v - MARKS[i - 1]) / (MARKS[i] - MARKS[i - 1])) / (MARKS.length - 1)
  }
  return 1
}

const CX = 160, CY = 150, R = 118, START = 150, SWEEP = 240
const pt = (r: number, deg: number): [number, number] => [CX + r * Math.cos((deg * Math.PI) / 180), CY + r * Math.sin((deg * Math.PI) / 180)]
const arc = (r: number, f: number) => {
  const [x0, y0] = pt(r, START)
  const [x1, y1] = pt(r, START + SWEEP * f)
  return `M${x0.toFixed(2)} ${y0.toFixed(2)} A${r} ${r} 0 ${SWEEP * f > 180 ? 1 : 0} 1 ${x1.toFixed(2)} ${y1.toFixed(2)}`
}

/** Eases towards a value, so the needle glides between the readings. */
function useSmooth(target: number, k = 0.16) {
  const [v, setV] = useState(target)
  const goal = useRef(target)
  goal.current = target
  useEffect(() => {
    let raf = 0
    const step = () => {
      setV((cur) => (Math.abs(goal.current - cur) < 0.005 ? goal.current : cur + (goal.current - cur) * k))
      raf = requestAnimationFrame(step)
    }
    raf = requestAnimationFrame(step)
    return () => cancelAnimationFrame(raf)
  }, [k])
  return v
}

function Dial({ value, color, label, unit, idle }: { value: number; color: string; label: string; unit: string; idle?: boolean }) {
  const id = useId().replace(/:/g, '')
  const shown = useSmooth(value)
  const f = unit === 'Mbps' ? fraction(shown) : 0
  const [nx, ny] = pt(R - 26, START + SWEEP * f)
  return (
    <svg viewBox="0 0 320 266" className="w-full max-w-[360px] mx-auto select-none" role="img" aria-label={`${label} ${shown.toFixed(1)} ${unit}`} style={{ direction: 'ltr' }}>
      <defs>
        <linearGradient id={`g${id}`} x1="0" y1="1" x2="1" y2="0">
          <stop offset="0" stopColor={color} stopOpacity="0.35" />
          <stop offset="1" stopColor={color} />
        </linearGradient>
        <filter id={`b${id}`} x="-20%" y="-20%" width="140%" height="140%"><feGaussianBlur stdDeviation="5" /></filter>
      </defs>
      <path d={arc(R, 1)} fill="none" stroke="rgba(255,255,255,0.09)" strokeWidth="14" strokeLinecap="round" />
      {f > 0.004 && <path d={arc(R, f)} fill="none" stroke={color} strokeWidth="14" strokeLinecap="round" opacity="0.5" filter={`url(#b${id})`} />}
      {f > 0.004 && <path d={arc(R, f)} fill="none" stroke={`url(#g${id})`} strokeWidth="14" strokeLinecap="round" />}
      {MARKS.map((m, i) => {
        const deg = START + (SWEEP * i) / (MARKS.length - 1)
        const [x0, y0] = pt(R + 12, deg)
        const [x1, y1] = pt(R + 17, deg)
        const [tx, ty] = pt(R + 30, deg)
        return (
          <g key={m}>
            <line x1={x0} y1={y0} x2={x1} y2={y1} stroke="rgba(255,255,255,0.35)" strokeWidth="1.5" />
            <text x={tx} y={ty} fill="rgba(255,255,255,0.55)" fontSize="10.5" textAnchor="middle" dominantBaseline="middle" fontFamily="inherit">{m}</text>
          </g>
        )
      })}
      <line x1={CX} y1={CY} x2={nx} y2={ny} stroke="#fff" strokeWidth="3" strokeLinecap="round" opacity={idle ? 0.35 : 1} />
      <circle cx={CX} cy={CY} r="9" fill="#0b1226" stroke={color} strokeWidth="3" />
      <text x={CX} y="188" fill={color} fontSize="13" fontWeight="700" textAnchor="middle" letterSpacing="2" fontFamily="inherit">{label.toUpperCase()}</text>
      <text x={CX} y="236" fill="#fff" fontSize="46" fontWeight="600" textAnchor="middle" fontFamily="inherit" style={{ fontVariantNumeric: 'tabular-nums' }}>{idle ? '—' : unit === 'Mbps' ? mbps(shown) : n(Math.round(shown * 10) / 10, 1)}</text>
      <text x={CX} y="258" fill="rgba(255,255,255,0.6)" fontSize="13" textAnchor="middle" fontFamily="inherit">{unit}</text>
    </svg>
  )
}

/* ---------- graph ---------- */

function Graph({ samples, color, height = 64, className }: { samples: number[]; color: string; height?: number; className?: string }) {
  const id = useId().replace(/:/g, '')
  const W = 300
  if (samples.length < 2) return <div style={{ height }} className={className} />
  // The first readings of a run are a burst from buffers; scale by what the
  // run mostly looked like, so one spike does not flatten the rest.
  const sorted = [...samples].sort((x, y) => x - y)
  const max = Math.max(sorted[Math.floor(sorted.length * 0.9)] * 1.3, 1)
  const pts = samples.map((v, i) => [(i / (samples.length - 1)) * W, height - 2 - Math.min(v / max, 1) * (height - 6)] as const)
  // A smooth curve through the points.
  let d = `M${pts[0][0]} ${pts[0][1]}`
  for (let i = 1; i < pts.length; i++) {
    const [x0, y0] = pts[i - 1]
    const [x1, y1] = pts[i]
    const mx = (x0 + x1) / 2
    d += ` C${mx} ${y0} ${mx} ${y1} ${x1} ${y1}`
  }
  return (
    <svg viewBox={`0 0 ${W} ${height}`} preserveAspectRatio="none" className={clsx('w-full', className)} style={{ height, direction: 'ltr' }} aria-hidden>
      <defs>
        <linearGradient id={`a${id}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor={color} stopOpacity="0.4" />
          <stop offset="1" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={`${d} L${W} ${height} L0 ${height} Z`} fill={`url(#a${id})`} />
      <path d={d} fill="none" stroke={color} strokeWidth="2" strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
    </svg>
  )
}

/* ---------- pieces ---------- */

function Tile({ icon, label, value, unit, sub, color, active }: { icon: React.ReactNode; label: string; value: string; unit: string; sub?: string; color: string; active?: boolean }) {
  return (
    <div className={clsx('rounded-2xl px-3 sm:px-3.5 py-3 min-w-0 transition-colors', active ? 'bg-white/10 ring-1 ring-white/20' : 'bg-white/5')}>
      <p className="flex items-center gap-1.5 text-[11.5px] font-semibold tracking-wide text-white/60" style={active ? { color } : undefined}>{icon}{label}</p>
      <p className="mt-1.5 font-display text-[22px] sm:text-[26px] leading-none font-semibold tnum text-white" dir="ltr">
        {value}<span className="block sm:inline mt-1 sm:mt-0 text-[11.5px] sm:text-[12px] font-medium text-white/55 sm:ms-1">{value === '—' ? '' : unit}</span>
      </p>
      {sub && <p className="mt-1 text-[11.5px] text-white/45 tnum" dir="ltr">{sub}</p>}
    </div>
  )
}

/** The big round button that starts a test, as in the apps. */
function Go({ onClick, again }: { onClick: () => void; again?: boolean }) {
  const t = useT()
  return (
    <button type="button" onClick={onClick} aria-label={again ? t('Test again') : t('Start the test')}
      className={clsx('group relative grid place-items-center rounded-full font-display font-bold tracking-widest text-white transition-transform hover:scale-105 active:scale-95', again ? 'size-[84px] text-[20px]' : 'size-[132px] text-[34px]')}
      style={{ background: 'radial-gradient(circle at 50% 35%, #0e1b3a, #070d1d)', boxShadow: `0 0 0 2px ${COLORS.download}, 0 0 28px ${COLORS.download}66, inset 0 0 22px ${COLORS.download}33` }}>
      <span className="absolute inset-0 rounded-full animate-ping opacity-20" style={{ boxShadow: `0 0 0 2px ${COLORS.download}` }} aria-hidden />
      {again ? <Gauge className="size-8" style={{ color: COLORS.download }} aria-hidden /> : t('GO')}
    </button>
  )
}

type Job = { state: 'starting' | 'queued' | 'running' | 'done' | 'error'; progress?: SpeedProgress; result?: SpeedResult; error?: string }

const PROVIDER_KEY = 'neo-speed-provider'
type Provider = 'ookla' | 'cloudflare'
const savedProvider = (): Provider => {
  try {
    return localStorage.getItem(PROVIDER_KEY) === 'cloudflare' ? 'cloudflare' : 'ookla'
  } catch {
    return 'ookla'
  }
}

/**
 * Measures real speed by hand: ping, then download and upload, with the
 * needle moving as it goes. Of a V2Ray config (monitorId) — from this panel
 * or from one of the servers — or of a server's own connection (direct).
 */
export function SpeedDialog({ open, onClose, title, monitorId, direct, agentId, last, onResult }: {
  open: boolean; onClose: () => void; title: string
  monitorId?: number; direct?: boolean; agentId?: number
  last?: SpeedResult | null; onResult?: (r: SpeedResult) => void
}) {
  const t = useT()
  const agents = useApp((s) => s.agents)
  const [from, setFrom] = useState(agentId ?? 0)
  const [provider, setProvider] = useState<Provider>(savedProvider)
  const [job, setJob] = useState<Job | null>(null)
  const run = useRef(0) // which start the polling belongs to
  const now = useNow(1000)

  useEffect(() => {
    if (open) {
      setFrom(agentId ?? 0)
      setJob(null)
    } else {
      run.current++ // closing stops following the test; it finishes on its own
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const start = async () => {
    const mine = ++run.current
    setJob({ state: 'starting' })
    try {
      const r = await api.post<{ ok: boolean; id?: string; error?: string }>('/api/xray/speed', { monitorId, direct, agentId: direct ? agentId ?? 0 : from, provider })
      if (!r.ok || !r.id) {
        setJob({ state: 'error', error: r.error || t('The speed test failed.') })
        return
      }
      for (;;) {
        await new Promise((res) => setTimeout(res, 350))
        if (run.current !== mine) return
        const j = await api.get<{ state: Job['state']; progress?: SpeedProgress; result?: SpeedResult; error?: string }>(`/api/xray/speed/${r.id}`)
        if (run.current !== mine) return
        setJob(j)
        if (j.state === 'done' && j.result) {
          onResult?.(j.result)
          return
        }
        if (j.state === 'error') return
      }
    } catch (e) {
      if (run.current === mine) setJob({ state: 'error', error: (e as Error).message })
    }
  }

  const live = job && (job.state === 'starting' || job.state === 'queued' || job.state === 'running')
  const p = job?.progress
  const res = job?.state === 'done' ? job.result! : !job ? last ?? null : null
  const phase = p?.phase ?? 'ping'
  const fromName = (direct ? agentId : from) ? agents.find((a) => a.id === (direct ? agentId : from))?.name : ''
  const waitingFor = direct ? agentId : from

  const dial = () => {
    if (!live) return null
    if (!p || p.phase === 'server') {
      // Not started yet (for a server: waiting for its next report), or
      // looking for the nearest test server.
      return <Dial value={0} color={COLORS.ping} label={t('Ping')} unit="ms" idle />
    }
    if (phase === 'ping') return <Dial value={p.cur} color={COLORS.ping} label={t('Ping')} unit="ms" />
    return <Dial value={p.cur} color={phase === 'download' ? COLORS.download : COLORS.upload} label={phase === 'download' ? t('Download') : t('Upload')} unit="Mbps" />
  }

  const body = () => {
    if (live) {
      const samples = phase === 'upload' ? p?.upSamples ?? [] : phase === 'download' ? p?.downSamples ?? [] : []
      const waiting = !p || phase === 'server'
      return (
        <div className="space-y-4">
          <div className="relative">
            {dial()}
            {waiting && (
              <p className="absolute inset-x-0 bottom-[26%] text-center text-[12.5px] text-white/60 px-6">
                {job?.state === 'starting' ? t('Starting…') : !p ? (waitingFor ? t('Waiting for {name} to pick it up — up to 20 seconds.', { name: fromName ?? '' }) : t('Starting…')) : t('Finding the nearest test server…')}
              </p>
            )}
          </div>
          <Graph samples={samples} color={phase === 'upload' ? COLORS.upload : COLORS.download} height={56} className="-mt-3" />
          <div className="grid grid-cols-3 gap-2.5">
            <Tile icon={<Activity className="size-3.5" />} label={t('Ping')} color={COLORS.ping} active={phase === 'ping' && !waiting}
              value={p?.ping ? n(Math.round(p.ping * 10) / 10, 1) : '—'} unit="ms" sub={p?.ping ? `${t('Jitter')} ${n(Math.round((p.jitter ?? 0) * 10) / 10, 1)}` : undefined} />
            <Tile icon={<ArrowDown className="size-3.5" />} label={t('Download')} color={COLORS.download} active={phase === 'download'}
              value={p && p.down ? mbps(p.down) : p && phase === 'download' ? mbps(p.cur) : '—'} unit="Mbps" />
            <Tile icon={<ArrowUp className="size-3.5" />} label={t('Upload')} color={COLORS.upload} active={phase === 'upload'}
              value={p && p.up ? mbps(p.up) : p && phase === 'upload' ? mbps(p.cur) : '—'} unit="Mbps" />
          </div>
          {p?.server && <p className="text-center text-[12px] text-white/45 flex items-center justify-center gap-1.5" dir="ltr"><Server className="size-3.5" />{p.server}</p>}
        </div>
      )
    }
    if (res) {
      return (
        <div className="space-y-4 fade-up">
          <div className="grid grid-cols-2 gap-3">
            {([['download', t('Download'), res.down, res.downSamples, ArrowDown], ['upload', t('Upload'), res.up, res.upSamples, ArrowUp]] as const).map(([k, label, v, samples, Icon]) => (
              <div key={k} className="rounded-2xl bg-white/5 px-4 pt-3.5 pb-2.5 overflow-hidden">
                <p className="flex items-center gap-1.5 text-[12px] font-semibold tracking-wide" style={{ color: COLORS[k] }}><Icon className="size-3.5" />{label}</p>
                <p className="mt-2 font-display text-[32px] sm:text-[40px] leading-none font-semibold tnum text-white" dir="ltr">{v > 0 ? mbps(v) : '—'}<span className="block sm:inline mt-1 sm:mt-0 text-[12px] sm:text-[13px] font-medium text-white/55 sm:ms-1.5">{v > 0 ? 'Mbps' : ''}</span></p>
                <Graph samples={samples ?? []} color={COLORS[k]} height={48} className="mt-2 -mx-1" />
              </div>
            ))}
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Tile icon={<Activity className="size-3.5" />} label={t('Ping')} color={COLORS.ping} value={n(Math.round(res.ping * 10) / 10, 1)} unit="ms" />
            <Tile icon={<Activity className="size-3.5" />} label={t('Jitter')} color={COLORS.ping} value={n(Math.round((res.jitter ?? 0) * 10) / 10, 1)} unit="ms" />
          </div>
          {res.upError && <p className="text-[12.5px] text-amber-300">{t('Upload was not measured: {why}', { why: res.upError })}</p>}
          <p className="text-center text-[12px] text-white/45 tnum" dir="auto">
            {t('{ago} · from {name} · via {server} · {mb} MB moved', { ago: timeAgo(res.at, now), name: res.from || t('this panel'), server: res.server, mb: n(Math.round(res.bytes / (1 << 20))) })}
          </p>
          <div className="flex justify-center pt-1"><Go onClick={start} again /></div>
        </div>
      )
    }
    return (
      <div className="py-4 space-y-5">
        {job?.state === 'error' && <Alert tone="danger" title={t('The speed test failed')}>{job.error}</Alert>}
        <div className="flex flex-col items-center gap-3">
          <Go onClick={start} />
          <p className="text-[12.5px] text-white/50">{direct ? t('Press to measure the connection of this server: ping, download and upload.') : t('Press to measure ping, download and upload through this config.')}</p>
        </div>
      </div>
    )
  }

  return (
    <Modal open={open} onClose={onClose} size="md" title={title}
      footer={<Button variant="ghost" onClick={onClose}>{t('Close')}</Button>}>
      <div className="space-y-4">
        {!live && (
          <div className={clsx('grid gap-3', !direct && 'sm:grid-cols-2')}>
            {!direct && (
              <Field label={t('Test from')} htmlFor="spf" help={t('Pick a server inside the country to see the speed your users there get.')}>
                <Select id="spf" value={from} onChange={(e) => { setFrom(Number(e.target.value)); if (job?.state === 'done') setJob(null) }}>
                  <option value={0}>{t('This panel’s server')}</option>
                  {agents.map((a) => <option key={a.id} value={a.id}>{a.name}{a.online ? '' : ` (${t('offline')})`}</option>)}
                </Select>
              </Field>
            )}
            <Field label={t('Test servers')} htmlFor="spp" help={provider === 'ookla' ? t('The nearest Speedtest.net server; Cloudflare if none answers.') : t('Cloudflare’s speed test servers.')}>
              <Select id="spp" value={provider} onChange={(e) => {
                const v = e.target.value as Provider
                setProvider(v)
                try { localStorage.setItem(PROVIDER_KEY, v) } catch { /* storage off */ }
              }}>
                <option value="ookla">Speedtest by Ookla</option>
                <option value="cloudflare">Cloudflare</option>
              </Select>
            </Field>
          </div>
        )}
        <div className="rounded-3xl px-4 py-5 text-white" style={{ background: 'radial-gradient(120% 90% at 50% 0%, #15244a 0%, #0a1226 55%, #070c1a 100%)', boxShadow: 'inset 0 0 0 1px rgba(255,255,255,0.07)' }}>
          {body()}
        </div>
        <p className="text-[12.5px] text-subtle leading-relaxed">{direct ? t('A test moves up to a few hundred MB through this server’s connection. It runs only when you press the button.') : t('A test moves up to a few hundred MB through the config, and counts against its traffic limit. It runs only when you press the button.')}</p>
      </div>
    </Modal>
  )
}

/** The speed test of a V2Ray config; its result is kept with the monitor. */
export function ConfigSpeedDialog({ m, open, onClose }: { m: MonitorView; open: boolean; onClose: () => void }) {
  const t = useT()
  return (
    <SpeedDialog open={open} onClose={onClose} title={t('Speed test — {name}', { name: m.name })} monitorId={m.id} agentId={m.config.agentId ?? 0} last={m.speed}
      onResult={(sp) => useApp.setState((s) => (s.monitors[m.id] ? { monitors: { ...s.monitors, [m.id]: { ...s.monitors[m.id], speed: sp } } } : s))} />
  )
}
