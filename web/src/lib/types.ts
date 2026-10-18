export const STATUS = { DOWN: 0, UP: 1, PENDING: 2, MAINTENANCE: 3 } as const
export type Status = 0 | 1 | 2 | 3

export type MonitorType =
  | 'http' | 'keyword' | 'json-query' | 'tcp' | 'ping' | 'dns' | 'push' | 'group'
  | 'docker' | 'postgres' | 'mysql' | 'redis' | 'backpack-tunnel' | 'backpack-panel'
  | 'agent' | 'agent-tunnel' | 'smtp' | 'mongodb' | 'mssql' | 'mqtt' | 'grpc' | 'radius'
  | 'snmp' | 'steam' | 'kafka' | 'udp' | 'websocket' | 'tls' | 'xray'

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
  agentId?: number
  source?: TunnelSource
  username?: string
  password?: string
  topic?: string
  secret?: string
  community?: string
  oid?: string
  snmpVersion?: string
  service?: string
  security?: string
  payload?: string
  warnDays?: number
  latencyAlertMs?: number
  latencyAlertFor?: number
  // V2Ray / Xray config
  link?: string
  testUrl?: string
  subscriptionId?: number
  configName?: string
  gone?: boolean
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
  country: string
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

/** The tunnel programs an agent reads. */
export type TunnelSource = 'backpack' | 'kariz' | 'neotunnel'

export interface Tunnel {
  source?: TunnelSource
  lastError?: string
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
  /** What counts against the quota, when that is not the total (NeoTunnel: this month). */
  quotaUsed?: number
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
  agent?: AgentInfo
  xray?: XrayInfo
  speed?: SpeedResult
  nextCheck: number
  maintenance: boolean
  target: string
}

/** The last speed test of a config (Mbps; ms). */
export interface SpeedProgress {
  phase: 'server' | 'ping' | 'download' | 'upload'
  cur: number
  pct: number
  ping?: number
  jitter?: number
  down?: number
  up?: number
  server?: string
  downSamples?: number[]
  upSamples?: number[]
}

export interface SpeedResult {
  down: number
  up: number
  ping: number
  jitter: number
  downSamples?: number[]
  upSamples?: number[]
  server: string
  from?: string
  bytes: number
  upError?: string
  at: number
}

/** A V2Ray/Xray config as the UI shows it — never the link itself. */
export interface XrayInfo {
  summary: string
  protocol: string
  network: string
  security: string
  endpoint: string
  connect?: number
  from?: string
}

export interface SubscriptionInfo {
  title?: string
  upload?: number
  download?: number
  total?: number
  expire?: number
  hasUsage?: boolean
  configs: number
  unusable?: number
  via?: string
}

export interface Subscription {
  id: number
  name: string
  url: string
  agentId: number
  interval: number
  timeout: number
  testUrl: string
  autoImport: boolean
  refreshMinutes: number
  groupMonitorId: number | null
  skip: string[]
  info: SubscriptionInfo
  lastFetch: number
  lastError: string
  createdAt: number
  updatedAt: number
}

export interface SubPreviewItem { key: string; label: string; name: string; summary: string; protocol: string; endpoint: string; country?: string; link?: string }
export interface SubPreview {
  title: string
  usage?: { upload: number; download: number; total: number; expire: number } | null
  configs: SubPreviewItem[]
  unusable: { name: string; error: string }[]
  via: string
}

/** A text ad from Neo Aurora's ad service. */
export interface Ad {
  id: string
  badge?: string
  title: string
  text?: string
  button?: string
  url?: string
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

export type Theme = 'zen' | 'pulse' | 'aurora' | 'clarity' | 'lime' | 'noir'

/** Details a status page can leave out; hidden data is not sent at all. */
export type HideKey = 'flag' | 'status' | 'location' | 'graph' | 'bars' | 'direction' | 'transport' | 'profile' | 'port' | 'traffic' | 'connected'

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
  language: 'auto' | 'en' | 'fa'
  showUptime: boolean
  showLatency: boolean
  showBanner: boolean
  domain: string
  barDays: number
  hide: HideKey[]
  createdAt: number
  updatedAt: number
  groups: StatusPageGroup[]
}

export interface DomainStatus {
  domain: string
  kind: 'panel' | 'status'
  pageId?: number
  title?: string
  ips: string[]
  pointsHere: boolean
  issued: boolean
  notAfter?: number
  issuer?: string
  issuing: boolean
  error?: string
  checkedAt?: number
}

export interface HttpsStatus {
  enabled: boolean
  running: boolean
  httpAddr: string
  httpsAddr: string
  listenError?: string
  httpError?: string
  serverIps: string[]
  domains: DomainStatus[]
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
  isOwner: boolean
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

export interface AgentSystem {
  hostname: string
  os: string
  kernel: string
  arch: string
  uptimeSecs: number
  cpuPercent: number
  cpuCores: number
  load1: number
  load5: number
  load15: number
  memTotal: number
  memUsed: number
  swapTotal: number
  swapUsed: number
  diskTotal: number
  diskUsed: number
  netRxBytes: number
  netTxBytes: number
  netRxRate: number
  netTxRate: number
  tcpConns: number
}

export interface SourceSummary { installed: boolean; mode?: string; version?: string; error?: string; tunnels: number; online: number }

export interface AgentInfo {
  system: AgentSystem
  receivedAt: number
  version: string
  ip: string
  backpack?: SourceSummary
  kariz?: SourceSummary
  neotunnel?: SourceSummary
}

export interface Agent {
  /** The last speed test of the server's own connection. */
  speed?: SpeedResult
  id: number
  name: string
  tokenPrefix: string
  country: string
  interval: number
  autoImport: boolean
  groupMonitorId: number | null
  createdAt: number
  lastSeen: number
  lastIp: string
  version: string
  hostname: string
  online: boolean
  info?: AgentInfo
}

/** The ways an agent can reach this panel. */
export interface AgentAddresses { direct: string; public: string; port: number; ip: string }

export interface AgentSource { installed: boolean; mode?: string; version?: string; error?: string; tunnels: Tunnel[] }

export interface StatusApiKey {
  id: number
  name: string
  prefix: string
  statusPageId: number
  allowedOrigins: string
  createdAt: number
  lastUsed: number
  active: boolean
}
