export const STATUS = { DOWN: 0, UP: 1, PENDING: 2, MAINTENANCE: 3 } as const
export type Status = 0 | 1 | 2 | 3

export type MonitorType =
  | 'http' | 'keyword' | 'json-query' | 'tcp' | 'ping' | 'dns' | 'push' | 'group'
  | 'docker' | 'postgres' | 'mysql' | 'redis' | 'backpack-tunnel' | 'backpack-panel'

export interface MonitorConfig {
  url?: string
  method?: string
  headers?: string
  body?: string
  bodyEncoding?: string
  acceptedStatusCodes?: string[]
  maxRedirects?: number
  ignoreTls?: boolean
  basicAuthUser?: string
  basicAuthPass?: string
  keyword?: string
  invertKeyword?: boolean
  jsonPath?: string
  jsonOperator?: string
  expectedValue?: string
  certExpiryNotify?: boolean
  hostname?: string
  port?: number
  packetCount?: number
  dnsResolver?: string
  dnsRecordType?: string
  containerName?: string
  dockerHost?: string
  connectionString?: string
  query?: string
  panelId?: number
  tunnelName?: string
  serviceDownIsDown?: boolean
}

export interface MonitorTag { tagId: number; name: string; color: string; value: string }

export interface Monitor {
  id: number
  name: string
  type: MonitorType
  parentId: number | null
  active: boolean
  interval: number
  retryInterval: number
  maxRetries: number
  timeout: number
  resendInterval: number
  upsideDown: boolean
  description: string
  config: MonitorConfig
  pushToken?: string
  sortOrder: number
  createdAt: number
  updatedAt: number
  tags: MonitorTag[]
  notificationIds: number[]
}

export interface Heartbeat {
  id: number
  monitorId: number
  status: Status
  time: number
  ping: number | null
  msg: string
  important: boolean
  duration: number
  bytesIn?: number
  bytesOut?: number
}

export interface CertInfo {
  subject: string
  issuer: string
  validFrom: number
  validTo: number
  daysRemaining: number
  valid: boolean
  dnsNames?: string[]
}

export interface RatePoint { t: number; in: number; out: number }

export interface Tunnel {
  name: string
  role: string
  transport: string
  direction: string
  carrier: string
  addr: string
  ports: string
  state: string
  ping: number
  peerLocation: string
  peerISP: string
  country: string
  peerCountry: string
  tunnelPort: string
  serviceDown?: string
  uptime?: string
  inBytes?: number
  outBytes?: number
  totalBytes?: number
  quotaLimit?: number
  quotaHit?: boolean
  kcpLossPercent?: number
  pool?: { live: number; target: number; configured: number; mbps: number }
  preset?: string
  maxConnections?: number
  bandwidthMbps?: number
  rates?: RatePoint[]
  certExpiry?: string
}

export interface PanelSystem {
  hostname: string
  os: string
  uptime: string
  ipv4: string
  location: string
  isp: string
  cpuPercent: number
  cpuCores: number
  memPercent: number
  memUsed: string
  memTotal: string
  diskPercent: number
  diskUsed: string
  diskTotal: string
  totalRecvBytes: number
  totalSentBytes: number
  tunnelsTotal: number
  tunnelsRunning: number
  version?: string
}

export interface MonitorView extends Monitor {
  last: Heartbeat | null
  uptime24: number
  uptime30: number
  avgPing24: number
  cert?: CertInfo
  tunnel?: Tunnel
  system?: PanelSystem
  nextCheck: number
  maintenance: boolean
  target: string
}

export interface Tag { id: number; name: string; color: string; createdAt: number }

export interface Notification {
  id: number
  name: string
  type: string
  config: Record<string, string>
  isDefault: boolean
  active: boolean
  createdAt: number
}

export interface ProviderField {
  key: string
  label: string
  type: 'text' | 'password' | 'textarea' | 'number' | 'select' | 'switch' | 'url'
  required?: boolean
  placeholder?: string
  help?: string
  options?: string[]
  default?: string
}

export interface Provider { type: string; name: string; group: string; fields: ProviderField[] }

export type Theme = 'aurora' | 'clarity' | 'lime' | 'noir'

export interface StatusPageGroup { id?: number; name: string; monitors: { monitorId: number; showUrl: boolean }[] }

export interface StatusPage {
  id: number
  slug: string
  title: string
  description: string
  theme: Theme
  layout: 'list' | 'cards'
  barMode: 'heartbeat' | 'days'
  accentColor: string
  logo: string
  footer: string
  customCss: string
  showTags: boolean
  showCertExpiry: boolean
  showPoweredBy: boolean
  showCharts: boolean
  published: boolean
  refreshInterval: number
  createdAt: number
  updatedAt: number
  groups: StatusPageGroup[]
}

export interface Incident {
  id: number
  statusPageId: number
  title: string
  content: string
  style: 'info' | 'warning' | 'danger' | 'success' | 'maintenance'
  pinned: boolean
  createdAt: number
  updatedAt: number
}

export interface Maintenance {
  id: number
  title: string
  description: string
  strategy: 'manual' | 'single' | 'weekly' | 'monthly'
  active: boolean
  startAt: number
  endAt: number
  weekdays: number[]
  daysOfMonth: number[]
  timeFrom: string
  timeTo: string
  timezone: string
  monitorIds: number[]
  statusPageIds: number[]
  status?: 'under-maintenance' | 'scheduled' | 'ended' | 'inactive'
  nextStart?: number
  nextEnd?: number
}

export interface BackpackPanel {
  id: number
  name: string
  url: string
  token?: string
  ignoreTls: boolean
  interval: number
  autoImport: boolean
  groupMonitorId: number | null
  hasToken: boolean
  tunnels: number
  online: number
  error?: string
  fetchedAt?: number
  latencyMs: number
  hostname?: string
  version?: string
}

export interface PanelSnapshot {
  panelId: number
  tunnels: Tunnel[] | null
  system: PanelSystem | null
  fetchedAt: string
  latencyMs: number
  error?: string
}

export interface User {
  id: number
  username: string
  role: 'admin' | 'editor' | 'viewer'
  totpEnabled: boolean
  createdAt: number
  lastLogin: number
}

export interface ApiKey {
  id: number
  name: string
  prefix: string
  scope: 'read' | 'write'
  createdAt: number
  expiresAt: number
  lastUsed: number
  active: boolean
}

export interface ChartPoint { t: number; ping: number | null; min?: number; max?: number; status: Status; up?: number; down?: number }

export interface UptimeStat { ratio: number; checks: number; avgPing: number }
