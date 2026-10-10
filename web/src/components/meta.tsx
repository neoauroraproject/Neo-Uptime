import {
  ArrowLeftRight, Box, Braces, Cable, Container, Cpu, Database, Gamepad2, Globe, Layers, LockKeyhole, Mail, Network,
  Radar, Radio, RadioTower, Router, Rows3, Search, Server, ShieldCheck, TextSearch, Waypoints, Webhook, Zap,
} from 'lucide-react'
import type { ReactNode } from 'react'
import type { MonitorType } from '../lib/types'
import { clsx } from '../lib/format'
import { useT } from '../lib/i18n'

// Labels and help are English keys; components translate them with t().
export const typeInfo: Record<MonitorType, { label: string; icon: ReactNode; group: string; help: string }> = {
  http: { label: 'HTTP(s)', icon: <Globe />, group: 'General', help: 'Checks that a URL answers with an accepted status code.' },
  keyword: { label: 'HTTP(s) – Keyword', icon: <TextSearch />, group: 'General', help: 'Checks that the response body contains (or lacks) a keyword.' },
  'json-query': { label: 'HTTP(s) – JSON query', icon: <Braces />, group: 'General', help: 'Reads a value from a JSON response and compares it.' },
  tcp: { label: 'TCP port', icon: <Network />, group: 'General', help: 'Opens a TCP connection to host:port.' },
  ping: { label: 'Ping (ICMP)', icon: <Radar />, group: 'General', help: 'Sends ICMP echo requests and measures round-trip time.' },
  dns: { label: 'DNS', icon: <Search />, group: 'General', help: 'Resolves a record through the resolver you choose.' },
  websocket: { label: 'WebSocket', icon: <ArrowLeftRight />, group: 'General', help: 'Checks that a ws:// or wss:// endpoint accepts the upgrade.' },
  tls: { label: 'SSL certificate', icon: <LockKeyhole />, group: 'General', help: 'Goes down when a certificate is invalid or about to expire.' },
  agent: { label: 'Server (agent)', icon: <Cpu />, group: 'Servers & tunnels', help: 'A server running neo-uptime-agent: online state, CPU, memory, disk and traffic.' },
  'agent-tunnel': { label: 'Tunnel via agent', icon: <Cable />, group: 'Servers & tunnels', help: 'A BackPack or Kariz tunnel read by the agent on its server — no panel or token needed there.' },
  'backpack-tunnel': { label: 'BackPack tunnel (panel API)', icon: <Cable />, group: 'Servers & tunnels', help: 'Watches one tunnel through its BackPack panel: state, latency and traffic.' },
  'backpack-panel': { label: 'BackPack panel', icon: <Server />, group: 'Servers & tunnels', help: 'Checks the panel answers and reports its host CPU and memory.' },
  push: { label: 'Push (heartbeat)', icon: <Webhook />, group: 'Passive', help: 'Your script calls a URL; silence marks it down. Uptime Kuma push compatible.' },
  udp: { label: 'UDP port', icon: <Radio />, group: 'Network services', help: 'Sends a datagram; a refused port is down, a reply or silence is up.' },
  smtp: { label: 'SMTP', icon: <Mail />, group: 'Network services', help: 'Connects to a mail server, says EHLO and optionally starts TLS.' },
  mqtt: { label: 'MQTT', icon: <RadioTower />, group: 'Network services', help: 'Connects to a broker and optionally waits for a message on a topic.' },
  grpc: { label: 'gRPC health', icon: <Waypoints />, group: 'Network services', help: 'Calls the standard grpc.health.v1 Check method.' },
  radius: { label: 'RADIUS', icon: <ShieldCheck />, group: 'Network services', help: 'Sends an Access-Request and expects Access-Accept.' },
  snmp: { label: 'SNMP', icon: <Router />, group: 'Network services', help: 'Reads an OID and optionally compares its value.' },
  postgres: { label: 'PostgreSQL', icon: <Database />, group: 'Databases & queues', help: 'Connects and optionally runs a query.' },
  mysql: { label: 'MySQL / MariaDB', icon: <Database />, group: 'Databases & queues', help: 'Connects and optionally runs a query.' },
  mssql: { label: 'SQL Server', icon: <Database />, group: 'Databases & queues', help: 'Connects and optionally runs a query.' },
  mongodb: { label: 'MongoDB', icon: <Database />, group: 'Databases & queues', help: 'Connects and pings the primary.' },
  redis: { label: 'Redis', icon: <Zap />, group: 'Databases & queues', help: 'Authenticates and sends PING.' },
  kafka: { label: 'Kafka', icon: <Rows3 />, group: 'Databases & queues', help: 'Asks a broker for its API versions.' },
  docker: { label: 'Docker container', icon: <Container />, group: 'Other', help: 'Checks a container is running (and healthy, if it has a health check).' },
  steam: { label: 'Game server (Steam)', icon: <Gamepad2 />, group: 'Other', help: 'Queries a Source-engine game server for its name, map and players.' },
  group: { label: 'Group', icon: <Layers />, group: 'Other', help: 'Down when any member is down. Members inherit its maintenance.' },
}

export const typeGroups = ['General', 'Servers & tunnels', 'Passive', 'Network services', 'Databases & queues', 'Other']

export function TypeIcon({ type, className }: { type: MonitorType; className?: string }) {
  return <span className={clsx('inline-flex [&>svg]:size-full', className ?? 'size-4')}>{typeInfo[type]?.icon ?? <Box />}</span>
}

export function TypeLabel({ type }: { type: MonitorType }) {
  const t = useT()
  return <>{t(typeInfo[type]?.label ?? type)}</>
}

export function SourceBadge({ source }: { source?: string }) {
  if (!source) return null
  return (
    <span className={clsx('inline-flex items-center h-5 px-1.5 rounded-md text-[10.5px] font-bold uppercase tracking-wider border',
      source === 'kariz' ? 'text-maint border-maint/30 bg-maint/10' : 'text-accent-ink border-accent/30 bg-accent-soft')}>
      {source === 'kariz' ? 'Kariz' : 'BackPack'}
    </span>
  )
}

export function Logo({ compact, title = 'Neo Uptime' }: { compact?: boolean; title?: string }) {
  return (
    <div className="flex items-center gap-2.5 min-w-0">
      <svg viewBox="0 0 64 64" className="size-8 shrink-0" aria-hidden>
        <rect width="64" height="64" rx="16" fill="var(--surface-2)" />
        <rect x="1" y="1" width="62" height="62" rx="15" fill="none" stroke="var(--accent)" strokeOpacity=".4" strokeWidth="2" />
        <path d="M10 36h10l5-12 8 22 6-16 4 6h11" fill="none" stroke="var(--accent)" strokeWidth="5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      {!compact && <span className="font-display text-[17px] font-semibold tracking-tight truncate" dir="ltr">{title}</span>}
    </div>
  )
}
