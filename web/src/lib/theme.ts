import { useEffect, useState } from 'react'

export type AdminTheme = 'system' | 'dark' | 'light'

function resolve(t: AdminTheme): 'dark' | 'light' {
  if (t === 'system') return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
  return t
}

export function getTheme(): AdminTheme {
  try {
    return (localStorage.getItem('neo-theme') as AdminTheme) || 'system'
  } catch {
    return 'system'
  }
}

export function applyTheme(t: AdminTheme) {
  const r = resolve(t)
  document.documentElement.setAttribute('data-theme', r)
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', r === 'dark' ? '#070908' : '#f3f4f1')
}

export function useAdminTheme() {
  const [theme, setThemeState] = useState<AdminTheme>(getTheme)
  useEffect(() => {
    applyTheme(theme)
    if (theme !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: light)')
    const on = () => applyTheme('system')
    mq.addEventListener('change', on)
    return () => mq.removeEventListener('change', on)
  }, [theme])
  const setTheme = (t: AdminTheme) => {
    try {
      localStorage.setItem('neo-theme', t)
    } catch {
      /* storage unavailable: the choice lasts for this visit */
    }
    setThemeState(t)
  }
  return [theme, setTheme] as const
}
