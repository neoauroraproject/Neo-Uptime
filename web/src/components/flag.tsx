import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown, Globe, Search, X } from 'lucide-react'
import { clsx } from '../lib/format'
import { useLang, useT } from '../lib/i18n'

// Two-letter codes that have a flag. Flag emoji are not used: Windows desktop
// browsers render them as bare letters, so SVG flags are bundled instead.
const CODES = 'ad,ae,af,ag,ai,al,am,ao,aq,ar,as,at,au,aw,ax,az,ba,bb,bd,be,bf,bg,bh,bi,bj,bl,bm,bn,bo,bq,br,bs,bt,bv,bw,by,bz,ca,cc,cd,cf,cg,ch,ci,ck,cl,cm,cn,co,cr,cu,cv,cw,cx,cy,cz,de,dj,dk,dm,do,dz,ec,ee,eg,eh,er,es,et,eu,fi,fj,fk,fm,fo,fr,ga,gb,gd,ge,gf,gg,gh,gi,gl,gm,gn,gp,gq,gr,gs,gt,gu,gw,gy,hk,hm,hn,hr,ht,hu,id,ie,il,im,in,io,iq,ir,is,it,je,jm,jo,jp,ke,kg,kh,ki,km,kn,kp,kr,kw,ky,kz,la,lb,lc,li,lk,lr,ls,lt,lu,lv,ly,ma,mc,md,me,mf,mg,mh,mk,ml,mm,mn,mo,mp,mq,mr,ms,mt,mu,mv,mw,mx,my,mz,na,nc,ne,nf,ng,ni,nl,no,np,nr,nu,nz,om,pa,pe,pf,pg,ph,pk,pl,pm,pn,pr,ps,pt,pw,py,qa,re,ro,rs,ru,rw,sa,sb,sc,sd,se,sg,sh,si,sj,sk,sl,sm,sn,so,sr,ss,st,sv,sx,sy,sz,tc,td,tf,tg,th,tj,tk,tl,tm,tn,to,tr,tt,tv,tw,tz,ua,ug,um,us,uy,uz,va,vc,ve,vg,vi,vn,vu,wf,ws,xk,ye,yt,za,zm,zw'.split(',')

// Where tunnels and servers usually are, offered first.
const COMMON = ['ir', 'de', 'nl', 'tr', 'ae', 'fi', 'fr', 'gb', 'us', 'se', 'am', 'om', 'qa', 'ru', 'ca', 'pl', 'at', 'ch', 'it', 'es']

const nameCache = new Map<string, Intl.DisplayNames>()
export function countryName(cc: string, lang = useLang.getState().lang): string {
  if (!cc) return ''
  if (cc === 'eu') return lang === 'fa' ? 'اتحادیه اروپا' : 'European Union'
  if (cc === 'xk') return lang === 'fa' ? 'کوزوو' : 'Kosovo'
  let dn = nameCache.get(lang)
  if (!dn) {
    try {
      dn = new Intl.DisplayNames([lang === 'fa' ? 'fa' : 'en'], { type: 'region' })
      nameCache.set(lang, dn)
    } catch {
      return cc.toUpperCase()
    }
  }
  try {
    return dn.of(cc.toUpperCase()) ?? cc.toUpperCase()
  } catch {
    return cc.toUpperCase()
  }
}

export function Flag({ cc, className, size = 20, title, lazy }: { cc?: string | null; className?: string; size?: number; title?: boolean; lazy?: boolean }) {
  const code = cc?.toLowerCase()
  const h = Math.round((size * 3) / 4)
  if (!code || !CODES.includes(code)) {
    return (
      <span className={clsx('inline-grid place-items-center shrink-0 rounded-[4px] bg-surface-3 text-subtle', className)} style={{ width: size, height: h }} aria-hidden>
        <Globe style={{ width: h * 0.75, height: h * 0.75 }} />
      </span>
    )
  }
  return (
    <img
      src={`/flags/${code}.svg`}
      alt={countryName(code)}
      title={title ? countryName(code) : undefined}
      width={size}
      height={h}
      loading={lazy ? 'lazy' : undefined}
      decoding="async"
      className={clsx('inline-block shrink-0 rounded-[4px] object-cover shadow-[0_0_0_1px_rgb(0_0_0/0.12)]', className)}
      style={{ width: size, height: h }}
    />
  )
}

export function CountryPicker({ value, onChange, id, placeholder }: { value: string; onChange: (cc: string) => void; id?: string; placeholder?: string }) {
  const t = useT()
  const lang = useLang((s) => s.lang)
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const [rect, setRect] = useState<DOMRect | null>(null)
  const btn = useRef<HTMLButtonElement>(null)
  const pop = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLInputElement>(null)

  const all = useMemo(
    () => CODES.map((c) => ({ c, name: countryName(c, lang) })).sort((a, b) => a.name.localeCompare(b.name, lang === 'fa' ? 'fa' : 'en')),
    [lang],
  )
  const list = useMemo(() => {
    const s = q.trim().toLowerCase()
    if (!s) {
      const common = COMMON.map((c) => all.find((x) => x.c === c)!).filter(Boolean)
      return [...common, ...all.filter((x) => !COMMON.includes(x.c))]
    }
    return all.filter((x) => x.name.toLowerCase().includes(s) || x.c === s)
  }, [all, q])

  useEffect(() => {
    if (!open) return
    setRect(btn.current?.getBoundingClientRect() ?? null)
    setTimeout(() => input.current?.focus(), 10)
    const onDoc = (e: MouseEvent) => {
      if (!pop.current?.contains(e.target as Node) && !btn.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    const onScroll = () => setRect(btn.current?.getBoundingClientRect() ?? null)
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    window.addEventListener('scroll', onScroll, true)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
      window.removeEventListener('scroll', onScroll, true)
    }
  }, [open])

  const pick = (c: string) => {
    onChange(c)
    setOpen(false)
    setQ('')
  }

  return (
    <>
      <button
        ref={btn}
        id={id}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        className="w-full h-10 flex items-center gap-2.5 rounded-xl border border-line-strong bg-surface-2 px-3 text-sm text-start hover:border-accent/60 transition-colors"
      >
        <Flag cc={value} size={22} />
        <span className={clsx('flex-1 truncate', !value && 'text-subtle')}>{value ? countryName(value, lang) : placeholder ?? t('No flag')}</span>
        {value && (
          <span role="button" tabIndex={-1} aria-label={t('Clear')} onClick={(e) => { e.stopPropagation(); onChange('') }} className="text-subtle hover:text-fg">
            <X className="size-4" />
          </span>
        )}
        <ChevronDown className="size-4 text-muted" />
      </button>
      {open && rect && createPortal(
        <div
          ref={pop}
          className="fixed z-[80] rounded-2xl border border-line-strong bg-surface shadow-[var(--shadow-pop)] fade-up overflow-hidden"
          style={{ top: Math.min(rect.bottom + 6, window.innerHeight - 340), left: Math.max(8, Math.min(rect.left, window.innerWidth - Math.max(rect.width, 280) - 8)), width: Math.max(rect.width, 280) }}
        >
          <div className="p-2 border-b border-line relative">
            <Search className="absolute start-4 top-1/2 -translate-y-1/2 size-4 text-subtle" />
            <input
              ref={input}
              value={q}
              onChange={(e) => setQ(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && list[0]) { e.preventDefault(); pick(list[0].c) } }}
              placeholder={t('Search countries')}
              className="w-full h-9 ps-9 pe-3 rounded-xl bg-surface-2 border border-line text-sm focus:outline-none focus:border-accent"
            />
          </div>
          <ul role="listbox" className="max-h-72 overflow-y-auto p-1.5">
            {!q && (
              <li>
                <button type="button" onClick={() => pick('')} className="w-full flex items-center gap-2.5 rounded-lg px-2.5 h-9 text-sm hover:bg-surface-2 text-muted">
                  <Flag cc="" size={22} />{t('No flag')}
                </button>
              </li>
            )}
            {list.map((x, i) => (
              <li key={x.c}>
                {!q && i === COMMON.length && <div className="my-1 h-px bg-line" />}
                <button
                  type="button"
                  role="option"
                  aria-selected={value === x.c}
                  onClick={() => pick(x.c)}
                  className={clsx('w-full flex items-center gap-2.5 rounded-lg px-2.5 h-9 text-sm text-start hover:bg-surface-2', value === x.c && 'bg-accent-soft')}
                >
                  <Flag cc={x.c} size={22} lazy />
                  <span className="flex-1 truncate">{x.name}</span>
                  <span className="text-[11px] text-subtle uppercase font-mono">{x.c}</span>
                  {value === x.c && <Check className="size-4 text-accent-ink" />}
                </button>
              </li>
            ))}
            {list.length === 0 && <li className="px-3 py-6 text-center text-sm text-muted">{t('No country matches')}</li>}
          </ul>
        </div>,
        document.body,
      )}
    </>
  )
}
