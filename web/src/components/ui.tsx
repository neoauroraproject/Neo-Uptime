import { create } from 'zustand'
import {
  forwardRef, useCallback, useEffect, useId, useLayoutEffect, useRef, useState,
  type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes,
} from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown, CircleCheck, CircleX, Copy, Info, LoaderCircle, TriangleAlert, X } from 'lucide-react'
import { clsx, n, statusKey } from '../lib/format'
import { t as tr, useT } from '../lib/i18n'
import type { Status } from '../lib/types'

/* ---------- Button ---------- */

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'soft'
type Size = 'sm' | 'md' | 'lg'

const variants: Record<Variant, string> = {
  primary: 'bg-accent text-on-accent hover:brightness-105 active:brightness-95 shadow-[0_0_0_1px_rgb(0_0_0/0.06)_inset]',
  secondary: 'bg-surface-2 text-fg border border-line-strong hover:bg-surface-3',
  ghost: 'text-muted hover:text-fg hover:bg-surface-2',
  danger: 'bg-down/12 text-down border border-down/30 hover:bg-down/20',
  soft: 'bg-accent-soft text-accent-ink hover:brightness-110',
}
const sizes: Record<Size, string> = {
  sm: 'h-8 px-3 text-[13px] gap-1.5 rounded-lg',
  md: 'h-10 px-4 text-sm gap-2 rounded-xl',
  lg: 'h-12 px-5 text-[15px] gap-2 rounded-xl',
}

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant
  size?: Size
  loading?: boolean
  icon?: ReactNode
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'secondary', size = 'md', loading, icon, className, children, disabled, ...rest }, ref,
) {
  return (
    <button
      ref={ref}
      className={clsx(
        'inline-flex items-center justify-center font-medium whitespace-nowrap select-none transition-[background,filter,color,border-color] duration-150 disabled:opacity-50',
        variants[variant], sizes[size], className,
      )}
      disabled={disabled || loading}
      {...rest}
    >
      {loading ? <LoaderCircle className="size-4 animate-spin" aria-hidden /> : icon}
      {children}
    </button>
  )
})

export function IconButton({ label, className, children, size = 'md', ...rest }: ButtonHTMLAttributes<HTMLButtonElement> & { label: string; size?: 'sm' | 'md' }) {
  return (
    <button
      aria-label={label}
      title={label}
      className={clsx(
        'inline-flex items-center justify-center rounded-xl text-muted hover:text-fg hover:bg-surface-2 border border-transparent hover:border-line transition-colors duration-150 disabled:opacity-40',
        size === 'sm' ? 'size-8' : 'size-10',
        className,
      )}
      {...rest}
    >
      {children}
    </button>
  )
}

/* ---------- form controls ---------- */

const control =
  'w-full bg-surface-2 border border-line-strong rounded-xl px-3 text-[15px] sm:text-sm text-fg placeholder:text-subtle transition-[border-color,box-shadow] duration-150 focus:outline-none focus:border-accent focus:ring-4 focus:ring-accent/15 disabled:opacity-60'

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement> & { invalid?: boolean }>(
  function Input({ className, invalid, ...rest }, ref) {
    return <input ref={ref} className={clsx(control, 'h-10', invalid && 'border-down focus:border-down focus:ring-down/15', className)} aria-invalid={invalid || undefined} {...rest} />
  },
)

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(function Textarea({ className, ...rest }, ref) {
  return <textarea ref={ref} className={clsx(control, 'py-2.5 min-h-24 leading-relaxed', className)} {...rest} />
})

export function Select({ className, children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <div className={clsx('relative', className)}>
      <select className={clsx(control, 'h-10 appearance-none pe-9 cursor-pointer')} {...rest}>
        {children}
      </select>
      <ChevronDown className="pointer-events-none absolute end-3 top-1/2 -translate-y-1/2 size-4 text-muted" aria-hidden />
    </div>
  )
}

export function Switch({ checked, onChange, label, description, disabled, id }: {
  checked: boolean; onChange: (v: boolean) => void; label?: ReactNode; description?: ReactNode; disabled?: boolean; id?: string
}) {
  const auto = useId()
  const sid = id ?? auto
  return (
    <div className="flex items-start gap-3">
      <button
        id={sid}
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={clsx(
          'relative mt-0.5 inline-flex h-6 w-10 shrink-0 items-center rounded-full border transition-colors duration-200',
          checked ? 'bg-accent border-accent' : 'bg-surface-3 border-line-strong',
          disabled && 'opacity-50',
        )}
      >
        <span className={clsx('inline-block size-4.5 rounded-full shadow transition-transform duration-200', checked ? 'translate-x-[18px] rtl:-translate-x-[18px] bg-on-accent' : 'translate-x-[3px] rtl:-translate-x-[3px] bg-muted')} />
      </button>
      {(label || description) && (
        <label htmlFor={sid} className="min-w-0 cursor-pointer select-none">
          {label && <span className="block text-sm text-fg font-medium">{label}</span>}
          {description && <span className="block text-[13px] text-muted mt-0.5">{description}</span>}
        </label>
      )}
    </div>
  )
}

export function Checkbox({ checked, onChange, label, className }: { checked: boolean; onChange: (v: boolean) => void; label?: ReactNode; className?: string }) {
  return (
    <label className={clsx('inline-flex items-center gap-2.5 cursor-pointer select-none text-sm', className)}>
      <span
        className={clsx(
          'relative inline-flex size-[18px] shrink-0 items-center justify-center rounded-md border transition-colors',
          checked ? 'bg-accent border-accent text-on-accent' : 'bg-surface-2 border-line-strong',
        )}
      >
        <input type="checkbox" className="absolute inset-0 opacity-0 cursor-pointer" checked={checked} onChange={(e) => onChange(e.target.checked)} />
        {checked && <Check className="size-3.5" strokeWidth={3} aria-hidden />}
      </span>
      {label}
    </label>
  )
}

export function Field({ label, help, error, children, required, htmlFor, className, aside }: {
  label?: ReactNode; help?: ReactNode; error?: string; children: ReactNode; required?: boolean; htmlFor?: string; className?: string; aside?: ReactNode
}) {
  return (
    <div className={clsx('space-y-1.5', className)}>
      {label && (
        <div className="flex items-center justify-between gap-2">
          <label htmlFor={htmlFor} className="text-[13px] font-medium text-fg">
            {label}
            {required && <span className="text-down ms-0.5" aria-hidden>*</span>}
          </label>
          {aside}
        </div>
      )}
      {children}
      {error ? <p className="text-[13px] text-down" role="alert">{error}</p> : help ? <p className="text-[12.5px] text-muted leading-relaxed">{help}</p> : null}
    </div>
  )
}

export function Segmented<T extends string>({ value, onChange, options, size = 'md', className, ariaLabel }: {
  value: T; onChange: (v: T) => void; options: { value: T; label: ReactNode }[]; size?: 'sm' | 'md'; className?: string; ariaLabel?: string
}) {
  return (
    <div role="radiogroup" aria-label={ariaLabel} className={clsx('inline-flex p-1 rounded-xl bg-surface-2 border border-line gap-1', className)}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          onClick={() => onChange(o.value)}
          className={clsx(
            'rounded-lg font-medium transition-colors duration-150 whitespace-nowrap',
            size === 'sm' ? 'px-2.5 h-7 text-[12.5px]' : 'px-3.5 h-8 text-[13px]',
            value === o.value ? 'bg-surface text-fg shadow-sm border border-line-strong' : 'text-muted hover:text-fg border border-transparent',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

/* ---------- surfaces ---------- */

export function Card({ className, children, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={clsx('card', className)} {...rest}>{children}</div>
}

export function CardHeader({ title, subtitle, actions, icon }: { title: ReactNode; subtitle?: ReactNode; actions?: ReactNode; icon?: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 px-5 pt-4 pb-3">
      <div className="flex items-start gap-3 min-w-0">
        {icon && <div className="mt-0.5 text-muted">{icon}</div>}
        <div className="min-w-0">
          <h3 className="font-display text-[15px] font-semibold text-fg">{title}</h3>
          {subtitle && <p className="text-[13px] text-muted mt-0.5">{subtitle}</p>}
        </div>
      </div>
      {actions && <div className="flex items-center gap-2 shrink-0">{actions}</div>}
    </div>
  )
}

export function Badge({ children, tone = 'neutral', className, mono }: { children: ReactNode; tone?: 'neutral' | 'accent' | 'up' | 'down' | 'pending' | 'maint' | 'outline'; className?: string; mono?: boolean }) {
  const tones = {
    neutral: 'bg-surface-3 text-muted border-line',
    outline: 'bg-transparent text-muted border-line-strong',
    accent: 'bg-accent-soft text-accent-ink border-accent/25',
    up: 'bg-up/12 text-up border-up/25',
    down: 'bg-down/12 text-down border-down/25',
    pending: 'bg-pending/12 text-pending border-pending/25',
    maint: 'bg-maint/12 text-maint border-maint/25',
  }
  return (
    <span className={clsx('inline-flex items-center gap-1 h-6 px-2 rounded-md border text-[11.5px] font-semibold uppercase tracking-wide whitespace-nowrap', mono && 'font-mono normal-case tracking-normal', tones[tone], className)}>
      {children}
    </span>
  )
}

export function TagChip({ name, color, value }: { name: string; color: string; value?: string }) {
  return (
    <span
      className="inline-flex items-center h-5.5 px-2 rounded-full text-[11.5px] font-medium border whitespace-nowrap"
      style={{ color, borderColor: `color-mix(in srgb, ${color} 40%, transparent)`, background: `color-mix(in srgb, ${color} 12%, transparent)` }}
    >
      {name}
      {value && <span className="opacity-75">: {value}</span>}
    </span>
  )
}

const statusText = () => ({ up: tr('Up'), down: tr('Down'), pending: tr('Pending'), maint: tr('Maintenance'), paused: tr('Paused'), empty: tr('No data') })
const statusBg = { up: 'bg-up', down: 'bg-down', pending: 'bg-pending', maint: 'bg-maint', paused: 'bg-paused', empty: 'bg-subtle' }
const statusFg = { up: 'text-up', down: 'text-down', pending: 'text-pending', maint: 'text-maint', paused: 'text-muted', empty: 'text-muted' }

export function StatusDot({ status, active = true, live, className }: { status: Status | null | undefined; active?: boolean; live?: boolean; className?: string }) {
  const k = statusKey(status, active)
  return (
    <span className={clsx('relative inline-block size-2 rounded-full shrink-0', statusBg[k], statusFg[k], live && k === 'up' && 'live-dot', className)} aria-hidden />
  )
}

export function StatusPill({ status, active = true, size = 'md' }: { status: Status | null | undefined; active?: boolean; size?: 'sm' | 'md' | 'lg' }) {
  useT()
  const k = statusKey(status, active)
  return (
    <span
      className={clsx(
        'inline-flex items-center gap-1.5 rounded-full font-semibold whitespace-nowrap border',
        size === 'lg' ? 'h-8 px-3.5 text-sm' : size === 'sm' ? 'h-5.5 px-2 text-[11.5px]' : 'h-6.5 px-2.5 text-[12.5px]',
        {
          up: 'bg-up/12 text-up border-up/25', down: 'bg-down/12 text-down border-down/25', pending: 'bg-pending/12 text-pending border-pending/25',
          maint: 'bg-maint/12 text-maint border-maint/25', paused: 'bg-surface-3 text-muted border-line', empty: 'bg-surface-3 text-muted border-line',
        }[k],
      )}
    >
      <StatusDot status={status} active={active} live={size !== 'sm'} />
      {statusText()[k]}
    </span>
  )
}

export function Spinner({ className }: { className?: string }) {
  return <LoaderCircle className={clsx('animate-spin text-muted', className ?? 'size-5')} aria-label={tr('Loading')} />
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={clsx('skeleton', className)} aria-hidden />
}

export function Empty({ icon, title, body, action }: { icon?: ReactNode; title: string; body?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center text-center px-6 py-14">
      {icon && <div className="mb-4 grid place-items-center size-14 rounded-2xl bg-surface-2 border border-line text-muted">{icon}</div>}
      <h3 className="font-display text-lg font-semibold">{title}</h3>
      {body && <p className="mt-1.5 text-sm text-muted max-w-md leading-relaxed">{body}</p>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  )
}

export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="inline-flex items-center h-5 px-1.5 rounded-md border border-line-strong bg-surface-2 text-[11px] font-mono text-muted">{children}</kbd>
}

export function CopyButton({ text, label, className, variant = 'secondary' }: { text: string; label?: string; className?: string; variant?: Variant }) {
  const t = useT()
  const [done, setDone] = useState(false)
  return (
    <Button
      type="button"
      size="sm"
      variant={variant}
      className={className}
      icon={done ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
        } catch {
          const ta = document.createElement('textarea')
          ta.value = text
          document.body.appendChild(ta)
          ta.select()
          document.execCommand('copy')
          ta.remove()
        }
        setDone(true)
        setTimeout(() => setDone(false), 1600)
      }}
    >
      {done ? t('Copied') : label ?? t('Copy')}
    </Button>
  )
}

export function Pagination({ page, pages, onPage }: { page: number; pages: number; onPage: (p: number) => void }) {
  if (pages <= 1) return null
  const items: (number | '…')[] = []
  for (let i = 1; i <= pages; i++) {
    if (i === 1 || i === pages || Math.abs(i - page) <= 1) items.push(i)
    else if (items[items.length - 1] !== '…') items.push('…')
  }
  return (
    <nav aria-label={tr('Pagination')} className="flex items-center gap-1" dir="ltr">
      {items.map((it, i) =>
        it === '…' ? (
          <span key={`e${i}`} className="px-1 text-subtle">…</span>
        ) : (
          <button
            key={it}
            onClick={() => onPage(it)}
            aria-current={it === page ? 'page' : undefined}
            className={clsx('min-w-8 h-8 px-2 rounded-lg text-[13px] tnum font-medium transition-colors', it === page ? 'bg-accent text-on-accent' : 'text-muted hover:bg-surface-2 hover:text-fg')}
          >
            {n(it)}
          </button>
        ),
      )}
    </nav>
  )
}

/* ---------- modal ---------- */

export function Modal({ open, onClose, title, description, children, footer, size = 'md' }: {
  open: boolean; onClose: () => void; title: ReactNode; description?: ReactNode; children: ReactNode; footer?: ReactNode; size?: 'sm' | 'md' | 'lg' | 'xl'
}) {
  const ref = useRef<HTMLDivElement>(null)
  const titleId = useId()
  // Callers pass a new onClose on every render. Keeping it out of the
  // effect below means focus is set once, when the dialog opens: re-running
  // it moved focus away and back on each render, which on phones closes the
  // keyboard after every key typed.
  const close = useRef(onClose)
  close.current = onClose
  useEffect(() => {
    if (!open) return
    const prev = document.activeElement as HTMLElement | null
    const el = ref.current
    const focusables = () => Array.from(el?.querySelectorAll<HTMLElement>('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])') ?? []).filter((x) => !x.hasAttribute('disabled'))
    const first = el?.querySelector<HTMLElement>('[data-autofocus]') ?? focusables()[1] ?? focusables()[0]
    const timer = setTimeout(() => first?.focus(), 20)
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        close.current()
      }
      if (e.key === 'Tab') {
        const f = focusables()
        if (!f.length) return
        const a = f[0], b = f[f.length - 1]
        if (e.shiftKey && document.activeElement === a) { e.preventDefault(); b.focus() }
        else if (!e.shiftKey && document.activeElement === b) { e.preventDefault(); a.focus() }
      }
    }
    document.addEventListener('keydown', onKey)
    const overflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      clearTimeout(timer)
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = overflow
      prev?.focus?.()
    }
  }, [open])
  if (!open) return null
  const widths = { sm: 'max-w-md', md: 'max-w-xl', lg: 'max-w-3xl', xl: 'max-w-5xl' }
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-end sm:items-center justify-center sm:p-6">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-[2px] fade-up" onClick={onClose} aria-hidden />
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={clsx('relative w-full bg-surface border border-line-strong shadow-[var(--shadow-pop)] rounded-t-3xl sm:rounded-3xl max-h-[92vh] flex flex-col fade-up', widths[size])}
      >
        <div className="flex items-start justify-between gap-4 px-6 pt-5 pb-4 border-b border-line">
          <div className="min-w-0">
            <h2 id={titleId} className="font-display text-lg font-semibold">{title}</h2>
            {description && <p className="text-[13px] text-muted mt-1">{description}</p>}
          </div>
          <IconButton label={tr('Close')} size="sm" onClick={onClose} className="-me-2 -mt-1">
            <X className="size-4" />
          </IconButton>
        </div>
        <div className="px-6 py-5 overflow-y-auto">{children}</div>
        {footer && <div className="flex flex-wrap items-center justify-end gap-2 px-6 py-4 border-t border-line bg-surface-2/50 rounded-b-3xl">{footer}</div>}
      </div>
    </div>,
    document.body,
  )
}

/* ---------- confirm ---------- */

interface ConfirmReq { title: string; body?: ReactNode; confirmLabel?: string; danger?: boolean; resolve: (v: boolean) => void }
const useConfirmStore = create<{ req: ConfirmReq | null; set: (r: ConfirmReq | null) => void }>((set) => ({ req: null, set: (req) => set({ req }) }))

export function confirm(opts: Omit<ConfirmReq, 'resolve'>): Promise<boolean> {
  return new Promise((resolve) => useConfirmStore.getState().set({ ...opts, resolve }))
}

export function ConfirmHost() {
  const { req, set } = useConfirmStore()
  const done = useCallback((v: boolean) => {
    req?.resolve(v)
    set(null)
  }, [req, set])
  return (
    <Modal
      open={!!req}
      onClose={() => done(false)}
      title={req?.title ?? ''}
      size="sm"
      footer={
        <>
          <Button variant="ghost" onClick={() => done(false)}>{tr('Cancel')}</Button>
          <Button data-autofocus variant={req?.danger ? 'danger' : 'primary'} onClick={() => done(true)}>{req?.confirmLabel ?? tr('Confirm')}</Button>
        </>
      }
    >
      <div className="text-sm text-muted leading-relaxed">{req?.body}</div>
    </Modal>
  )
}

/* ---------- toasts ---------- */

interface Toast { id: number; kind: 'success' | 'error' | 'info'; title: string; body?: string }
const useToasts = create<{ list: Toast[]; push: (t: Omit<Toast, 'id'>) => void; drop: (id: number) => void }>((set, get) => ({
  list: [],
  push: (t) => {
    const id = Date.now() + Math.random()
    set({ list: [...get().list.slice(-3), { ...t, id }] })
    setTimeout(() => get().drop(id), t.kind === 'error' ? 7000 : 3800)
  },
  drop: (id) => set({ list: get().list.filter((x) => x.id !== id) }),
}))

export const toast = {
  success: (title: string, body?: string) => useToasts.getState().push({ kind: 'success', title, body }),
  error: (title: string, body?: string) => useToasts.getState().push({ kind: 'error', title, body }),
  info: (title: string, body?: string) => useToasts.getState().push({ kind: 'info', title, body }),
}

export function Toaster() {
  const { list, drop } = useToasts()
  return createPortal(
    <div className="fixed z-[60] bottom-4 end-4 start-4 sm:start-auto flex flex-col gap-2 sm:w-96" aria-live="polite">
      {list.map((t) => (
        <div key={t.id} className="fade-up flex items-start gap-3 rounded-2xl border border-line-strong bg-surface px-4 py-3 shadow-[var(--shadow-pop)]">
          {t.kind === 'success' ? <CircleCheck className="size-5 text-up shrink-0 mt-0.5" /> : t.kind === 'error' ? <CircleX className="size-5 text-down shrink-0 mt-0.5" /> : <Info className="size-5 text-maint shrink-0 mt-0.5" />}
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium">{t.title}</p>
            {t.body && <p className="text-[13px] text-muted mt-0.5 break-words">{t.body}</p>}
          </div>
          <button aria-label={tr('Dismiss')} onClick={() => drop(t.id)} className="text-subtle hover:text-fg">
            <X className="size-4" />
          </button>
        </div>
      ))}
    </div>,
    document.body,
  )
}

/* ---------- dropdown menu ---------- */

export interface MenuItem { label: string; icon?: ReactNode; onClick?: () => void; danger?: boolean; href?: string; disabled?: boolean; divider?: boolean }

export function Menu({ trigger, items, align = 'end' }: { trigger: (p: { onClick: () => void; 'aria-expanded': boolean }) => ReactNode; items: MenuItem[]; align?: 'start' | 'end' }) {
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)
  const ref = useRef<HTMLDivElement>(null)
  const menu = useRef<HTMLDivElement>(null)

  // The list is drawn on top of the page (a portal), so a card that clips its
  // content cannot cut it off. It opens upwards when there is no room below.
  useLayoutEffect(() => {
    if (!open) {
      setPos(null)
      return
    }
    const place = () => {
      const tr = ref.current?.getBoundingClientRect()
      const m = menu.current
      if (!tr || !m) return
      const w = m.offsetWidth
      const h = m.offsetHeight
      const rtl = getComputedStyle(ref.current!).direction === 'rtl'
      const alignRight = (align === 'end') !== rtl
      let left = alignRight ? tr.right - w : tr.left
      left = Math.max(8, Math.min(left, window.innerWidth - w - 8))
      let top = tr.bottom + 8
      if (top + h > window.innerHeight - 8 && tr.top - 8 - h >= 8) top = tr.top - 8 - h
      top = Math.max(8, Math.min(top, window.innerHeight - h - 8))
      setPos({ top, left })
    }
    place()
    window.addEventListener('resize', place)
    return () => window.removeEventListener('resize', place)
  }, [open, align])

  useEffect(() => {
    if (!open) return
    const inside = (e: Event) => ref.current?.contains(e.target as Node) || menu.current?.contains(e.target as Node)
    const onDoc = (e: MouseEvent) => { if (!inside(e)) setOpen(false) }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    const onScroll = (e: Event) => { if (!menu.current?.contains(e.target as Node)) setOpen(false) }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    window.addEventListener('scroll', onScroll, true)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
      window.removeEventListener('scroll', onScroll, true)
    }
  }, [open])

  return (
    <div ref={ref} className="relative inline-block">
      {trigger({ onClick: () => setOpen((o) => !o), 'aria-expanded': open })}
      {open && createPortal(
        <div ref={menu} role="menu" style={{ top: pos?.top ?? 0, left: pos?.left ?? 0, visibility: pos ? 'visible' : 'hidden' }}
          dir={ref.current ? getComputedStyle(ref.current).direction : undefined}
          className="fixed z-[65] min-w-48 max-w-[calc(100vw-16px)] rounded-2xl border border-line-strong bg-surface p-1.5 shadow-[var(--shadow-pop)] fade-up">
          {items.map((it, i) =>
            it.divider ? (
              <div key={i} className="my-1 h-px bg-line" />
            ) : (
              <button
                key={i}
                role="menuitem"
                disabled={it.disabled}
                onClick={() => {
                  setOpen(false)
                  it.onClick?.()
                }}
                className={clsx(
                  'flex w-full items-center gap-2.5 rounded-xl px-3 h-9 text-sm text-start whitespace-nowrap transition-colors disabled:opacity-40',
                  it.danger ? 'text-down hover:bg-down/10' : 'text-fg hover:bg-surface-2',
                )}
              >
                {it.icon && <span className="text-muted [&>svg]:size-4">{it.icon}</span>}
                {it.label}
              </button>
            ),
          )}
        </div>,
        document.body,
      )}
    </div>
  )
}

/* ---------- misc ---------- */

export function Alert({ tone = 'info', title, children, className }: { tone?: 'info' | 'warning' | 'danger' | 'success'; title?: ReactNode; children?: ReactNode; className?: string }) {
  const map = {
    info: ['border-maint/30 bg-maint/8', <Info key="i" className="size-4.5 text-maint" />],
    warning: ['border-pending/30 bg-pending/8', <TriangleAlert key="w" className="size-4.5 text-pending" />],
    danger: ['border-down/30 bg-down/8', <CircleX key="d" className="size-4.5 text-down" />],
    success: ['border-up/30 bg-up/8', <CircleCheck key="s" className="size-4.5 text-up" />],
  } as const
  const [cls, icon] = map[tone]
  return (
    <div className={clsx('flex gap-3 rounded-2xl border px-4 py-3', cls, className)} role={tone === 'danger' ? 'alert' : undefined}>
      <div className="shrink-0 mt-0.5">{icon}</div>
      <div className="min-w-0 text-[13.5px] leading-relaxed">
        {title && <p className="font-semibold text-fg">{title}</p>}
        {children && <div className="text-muted">{children}</div>}
      </div>
    </div>
  )
}

export function useMeasure<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  const [w, setW] = useState(0)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    setW(el.clientWidth)
    const ro = new ResizeObserver(([e]) => setW(e.contentRect.width))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, w] as const
}

export function useNow(intervalMs = 1000) {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(t)
  }, [intervalMs])
  return now
}
