import { useCallback } from 'react'
import { create } from 'zustand'
import { fa } from './fa'

export type Lang = 'en' | 'fa'

const dicts: Record<Lang, Record<string, string>> = { en: {}, fa }

function initial(): Lang {
  try {
    const saved = localStorage.getItem('neo-lang')
    if (saved === 'en' || saved === 'fa') return saved
  } catch {
    /* storage unavailable */
  }
  return navigator.language?.toLowerCase().startsWith('fa') ? 'fa' : 'en'
}

export function applyLang(lang: Lang) {
  const el = document.documentElement
  el.lang = lang
  el.dir = lang === 'fa' ? 'rtl' : 'ltr'
}

interface LangState {
  lang: Lang
  /** Changes the admin's language and remembers it. */
  setLang: (l: Lang) => void
  /** Changes the language for this page view only (public status pages). */
  useForPage: (l: Lang) => void
}

export const useLang = create<LangState>((set) => ({
  lang: initial(),
  setLang: (lang) => {
    try {
      localStorage.setItem('neo-lang', lang)
    } catch {
      /* not remembered */
    }
    applyLang(lang)
    set({ lang })
  },
  useForPage: (lang) => {
    applyLang(lang)
    set({ lang })
  },
}))

applyLang(useLang.getState().lang)

const faDigitMap = '۰۱۲۳۴۵۶۷۸۹'

/** Writes Western digits as Persian ones when the language is Persian. */
export function digits(s: string, lang: Lang = useLang.getState().lang): string {
  if (lang !== 'fa') return s
  return s.replace(/[0-9]/g, (d) => faDigitMap[Number(d)]).replace(/\./g, '٫').replace(/%/g, '٪')
}

export function translate(lang: Lang, key: string, vars?: Record<string, string | number>): string {
  let s = dicts[lang][key] ?? key
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      const val = typeof v === 'number' ? digits(String(v), lang) : v
      s = s.split(`{${k}}`).join(val)
    }
  }
  return s
}

/** Translate outside React (reads the current language). */
export function t(key: string, vars?: Record<string, string | number>): string {
  return translate(useLang.getState().lang, key, vars)
}

/** Translate inside components; re-renders when the language changes. */
export function useT() {
  const lang = useLang((s) => s.lang)
  return useCallback((key: string, vars?: Record<string, string | number>) => translate(lang, key, vars), [lang])
}

export const isRTL = () => useLang.getState().lang === 'fa'
