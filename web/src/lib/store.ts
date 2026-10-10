import { create } from 'zustand'
import { api } from './api'
import { setTimezone } from './format'
import type { Agent, AgentInfo, BackpackPanel, CertInfo, Heartbeat, MonitorView, Notification, PanelSystem, Tag, Tunnel, User } from './types'

const MAX_BEATS = 120

export interface AppSettings {
  siteTitle: string
  timezone: string
  primaryBaseURL: string
  entryPage: string
}

interface AppState {
  me: User | null
  role: 'admin' | 'editor' | 'viewer'
  version: string
  settings: AppSettings
  monitors: Record<number, MonitorView>
  beats: Record<number, Heartbeat[]>
  loaded: boolean
  live: 'connecting' | 'open' | 'closed'
  lastBeatAt: Record<number, number>
  tags: Tag[]
  notifications: Notification[]
  panels: BackpackPanel[]
  agents: Agent[]

  setMe: (u: User | null, version?: string) => void
  loadDashboard: () => Promise<void>
  loadSettings: () => Promise<void>
  loadTags: () => Promise<void>
  loadNotifications: () => Promise<void>
  loadPanels: () => Promise<void>
  loadAgents: () => Promise<void>
  applyHeartbeat: (e: { monitorId: number; heartbeat: Heartbeat; cert?: CertInfo; tunnel?: Tunnel; system?: PanelSystem; agent?: AgentInfo }) => void
  setLive: (s: AppState['live']) => void
  removeMonitor: (id: number) => void
}

export const useApp = create<AppState>((set, get) => ({
  me: null,
  role: 'viewer',
  version: '',
  settings: { siteTitle: 'Neo Uptime', timezone: 'auto', primaryBaseURL: '', entryPage: 'dashboard' },
  monitors: {},
  beats: {},
  loaded: false,
  live: 'connecting',
  lastBeatAt: {},
  tags: [],
  notifications: [],
  panels: [],
  agents: [],

  setMe: (u, version) => set({ me: u, role: u?.role ?? 'viewer', ...(version ? { version } : {}) }),

  loadDashboard: async () => {
    const d = await api.get<{ monitors: MonitorView[]; heartbeats: Record<string, Heartbeat[]> }>('/api/dashboard?beats=100')
    const monitors: Record<number, MonitorView> = {}
    for (const m of d.monitors) monitors[m.id] = m
    const beats: Record<number, Heartbeat[]> = {}
    for (const [k, v] of Object.entries(d.heartbeats ?? {})) beats[Number(k)] = v
    set({ monitors, beats, loaded: true })
  },

  loadSettings: async () => {
    const s = await api.get<AppSettings>('/api/settings/public')
    setTimezone(s.timezone)
    set({ settings: s })
    document.title = s.siteTitle
  },

  loadTags: async () => set({ tags: await api.get<Tag[]>('/api/tags') }),
  loadNotifications: async () => set({ notifications: await api.get<Notification[]>('/api/notifications') }),
  loadPanels: async () => set({ panels: await api.get<BackpackPanel[]>('/api/backpack/panels') }),
  loadAgents: async () => set({ agents: await api.get<Agent[]>('/api/agents') }),

  applyHeartbeat: ({ monitorId, heartbeat, cert, tunnel, system, agent }) => {
    const { monitors, beats, lastBeatAt } = get()
    const m = monitors[monitorId]
    const list = (beats[monitorId] ?? []).concat(heartbeat)
    if (list.length > MAX_BEATS) list.splice(0, list.length - MAX_BEATS)
    set({
      beats: { ...beats, [monitorId]: list },
      lastBeatAt: { ...lastBeatAt, [monitorId]: Date.now() },
      monitors: m
        ? {
            ...monitors,
            [monitorId]: {
              ...m,
              last: heartbeat,
              cert: cert ?? m.cert,
              tunnel: tunnel ?? m.tunnel,
              system: system ?? m.system,
              agent: agent ?? m.agent,
              maintenance: heartbeat.status === 3,
            },
          }
        : monitors,
    })
  },

  setLive: (live) => set({ live }),

  removeMonitor: (id) => {
    const monitors = { ...get().monitors }
    delete monitors[id]
    set({ monitors })
  },
}))

export const canEdit = (role: string) => role === 'admin' || role === 'editor'
