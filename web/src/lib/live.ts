import { useEffect } from 'react'
import { useApp } from './store'
import { toast } from '../components/ui'
import { t } from './i18n'

// useLiveEvents keeps the store current from the server's event stream and
// refreshes the slower numbers (uptime percentages) once a minute.
export function useLiveEvents() {
  useEffect(() => {
    const st = useApp.getState()
    let timer: ReturnType<typeof setTimeout> | undefined
    const reload = () => {
      clearTimeout(timer)
      timer = setTimeout(() => {
        useApp.getState().loadDashboard().catch(() => {})
      }, 300)
    }
    const es = new EventSource('/api/events')
    st.setLive('connecting')
    es.onopen = () => useApp.getState().setLive('open')
    es.onerror = () => useApp.getState().setLive('closed')
    es.addEventListener('hello', () => {
      useApp.getState().setLive('open')
      reload()
    })
    es.addEventListener('heartbeat', (e) => {
      try {
        useApp.getState().applyHeartbeat(JSON.parse((e as MessageEvent).data))
      } catch {
        /* ignore malformed event */
      }
    })
    es.addEventListener('monitors', reload)
    let agentTimer: ReturnType<typeof setTimeout> | undefined
    es.addEventListener('agent', () => {
      clearTimeout(agentTimer)
      agentTimer = setTimeout(() => useApp.getState().loadAgents().catch(() => {}), 1500)
    })
    es.addEventListener('maintenance', reload)
    let subTimer: ReturnType<typeof setTimeout> | undefined
    es.addEventListener('subscriptions', () => {
      clearTimeout(subTimer)
      subTimer = setTimeout(() => useApp.getState().loadSubscriptions().catch(() => {}), 500)
    })
    es.addEventListener('notification-error', (e) => {
      try {
        const d = JSON.parse((e as MessageEvent).data)
        toast.error(t('Notification "{name}" failed', { name: d.notification }), d.error)
      } catch {
        /* ignore */
      }
    })
    es.addEventListener('latency', (e) => {
      try {
        const d = JSON.parse((e as MessageEvent).data)
        const name = useApp.getState().monitors[d.monitorId]?.name ?? `#${d.monitorId}`
        const ping = d.ping != null ? Math.round(d.ping) : '?'
        if (d.kind === 'latency') toast.error(t('{name} is slow', { name }), t('{ms} ms — above the {limit} ms limit', { ms: ping, limit: d.limit }))
        else toast.success(t('{name} is fast again', { name }), t('{ms} ms', { ms: ping }))
      } catch {
        /* ignore */
      }
    })
    const minute = setInterval(reload, 60_000)
    return () => {
      es.close()
      clearInterval(minute)
      clearTimeout(timer)
    }
  }, [])
}
