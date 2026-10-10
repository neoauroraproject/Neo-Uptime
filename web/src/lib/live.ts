import { useEffect } from 'react'
import { useApp } from './store'
import { toast } from '../components/ui'

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
    es.addEventListener('maintenance', reload)
    es.addEventListener('notification-error', (e) => {
      try {
        const d = JSON.parse((e as MessageEvent).data)
        toast.error(`Notification "${d.notification}" failed`, d.error)
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
