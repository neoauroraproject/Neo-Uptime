import { Box, Cable, Container, Database, Globe, Layers, Network, Radar, Search, Server, Webhook, Braces, TextSearch, Zap } from 'lucide-react'
import type { ReactNode } from 'react'
import type { MonitorType } from '../lib/types'
import { clsx } from '../lib/format'

export const typeInfo: Record<MonitorType, { label: string; short: string; icon: ReactNode; group: string; help: string }> = {
  http: { label: 'HTTP(s)', short: 'HTTP', icon: <Globe />, group: 'General', help: 'Checks that a URL answers with an accepted status code.' },
  keyword: { label: 'HTTP(s) – Keyword', short: 'Keyword', icon: <TextSearch />, group: 'General', help: 'Checks that the response body contains (or lacks) a keyword.' },
  'json-query': { label: 'HTTP(s) – JSON query', short: 'JSON', icon: <Braces />, group: 'General', help: 'Reads a value from a JSON response and compares it.' },
  tcp: { label: 'TCP port', short: 'TCP', icon: <Network />, group: 'General', help: 'Opens a TCP connection to host:port.' },
  ping: { label: 'Ping (ICMP)', short: 'Ping', icon: <Radar />, group: 'General', help: 'Sends ICMP echo requests and measures round-trip time.' },
  dns: { label: 'DNS', short: 'DNS', icon: <Search />, group: 'General', help: 'Resolves a record through the resolver you choose.' },
  push: { label: 'Push (heartbeat)', short: 'Push', icon: <Webhook />, group: 'Passive', help: 'Your script calls a URL; silence marks it down. Uptime Kuma push compatible.' },
  'backpack-tunnel': { label: 'BackPack tunnel', short: 'Tunnel', icon: <Cable />, group: 'BackPack', help: 'Watches one tunnel through its BackPack panel: state, latency and traffic.' },
  'backpack-panel': { label: 'BackPack panel', short: 'Panel', icon: <Server />, group: 'BackPack', help: 'Checks the panel answers and reports its host CPU and memory.' },
  docker: { label: 'Docker container', short: 'Docker', icon: <Container />, group: 'Services', help: 'Checks a container is running (and healthy, if it has a health check).' },
  postgres: { label: 'PostgreSQL', short: 'Postgres', icon: <Database />, group: 'Services', help: 'Connects and optionally runs a query.' },
  mysql: { label: 'MySQL / MariaDB', short: 'MySQL', icon: <Database />, group: 'Services', help: 'Connects and optionally runs a query.' },
  redis: { label: 'Redis', short: 'Redis', icon: <Zap />, group: 'Services', help: 'Authenticates and sends PING.' },
  group: { label: 'Group', short: 'Group', icon: <Layers />, group: 'Other', help: 'Down when any member is down. Members inherit its maintenance.' },
}

export const typeGroups = ['General', 'BackPack', 'Passive', 'Services', 'Other']

export function TypeIcon({ type, className }: { type: MonitorType; className?: string }) {
  return <span className={clsx('inline-flex [&>svg]:size-full', className ?? 'size-4')}>{typeInfo[type]?.icon ?? <Box />}</span>
}

export function Logo({ compact, title = 'Neo Uptime' }: { compact?: boolean; title?: string }) {
  return (
    <div className="flex items-center gap-2.5 min-w-0">
      <svg viewBox="0 0 64 64" className="size-8 shrink-0" aria-hidden>
        <rect width="64" height="64" rx="16" fill="var(--surface-2)" />
        <rect x="1" y="1" width="62" height="62" rx="15" fill="none" stroke="var(--accent)" strokeOpacity=".4" strokeWidth="2" />
        <path d="M10 36h10l5-12 8 22 6-16 4 6h11" fill="none" stroke="var(--accent)" strokeWidth="5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      {!compact && <span className="font-display text-[17px] font-semibold tracking-tight truncate">{title}</span>}
    </div>
  )
}
