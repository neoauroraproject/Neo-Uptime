import { useMemo, useState, type FormEvent, type ReactNode } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Cable, Eye, EyeOff, KeyRound, ShieldCheck, Zap } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import { useApp } from '../lib/store'
import type { User } from '../lib/types'
import { Alert, Button, Checkbox, Field, Input } from '../components/ui'
import { Logo } from '../components/meta'

function AuthLayout({ children }: { children: ReactNode }) {
  // A decorative heartbeat strip: random but stable for the visit.
  const beats = useMemo(() => Array.from({ length: 48 }, (_, i) => (i === 31 || i === 32 ? 0 : i === 17 ? 2 : 1)), [])
  return (
    <div className="app-bg min-h-dvh grid lg:grid-cols-[1.1fr_1fr]">
      <div className="hidden lg:flex flex-col justify-between p-12 border-r border-line relative overflow-hidden">
        <div className="absolute inset-0 dot-grid opacity-60 [mask-image:radial-gradient(ellipse_at_center,black,transparent_75%)]" aria-hidden />
        <Logo />
        <div className="relative max-w-lg">
          <h1 className="font-display text-5xl font-semibold leading-[1.05] tracking-tight">
            Know the moment<br />something <span className="text-accent-ink">goes dark.</span>
          </h1>
          <p className="mt-5 text-[15px] text-muted leading-relaxed max-w-md">
            Websites, ports, DNS, databases — and every BackPack tunnel — watched around the clock, with alerts where you already are.
          </p>
          <div className="mt-10 card p-5">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-[12px] uppercase tracking-wider text-subtle font-semibold">DE2 · WSSMUX</p>
                <p className="font-display text-4xl font-semibold tnum mt-1">92<span className="text-lg text-muted ml-1">ms</span></p>
              </div>
              <span className="inline-flex items-center gap-1.5 h-7 px-3 rounded-full bg-up/12 text-up border border-up/25 text-[12.5px] font-semibold">
                <span className="size-2 rounded-full bg-up live-dot" />Online
              </span>
            </div>
            <div className="mt-5 flex items-end gap-[3px] h-8" aria-hidden>
              {beats.map((b, i) => (
                <span key={i} className="flex-1 rounded-full h-full" style={{ background: b === 1 ? 'var(--up)' : b === 0 ? 'var(--down)' : 'var(--pending)', opacity: 0.35 + (i / beats.length) * 0.65 }} />
              ))}
            </div>
          </div>
        </div>
        <div className="relative flex gap-6 text-[13px] text-muted">
          <span className="inline-flex items-center gap-2"><Cable className="size-4 text-accent-ink" />BackPack tunnels</span>
          <span className="inline-flex items-center gap-2"><Zap className="size-4 text-accent-ink" />Live updates</span>
          <span className="inline-flex items-center gap-2"><ShieldCheck className="size-4 text-accent-ink" />2FA & roles</span>
        </div>
      </div>
      <div className="flex items-center justify-center p-6">
        <div className="w-full max-w-sm">
          <div className="lg:hidden mb-10 flex justify-center"><Logo /></div>
          {children}
        </div>
      </div>
    </div>
  )
}

function PasswordInput(props: React.InputHTMLAttributes<HTMLInputElement>) {
  const [show, setShow] = useState(false)
  return (
    <div className="relative">
      <Input type={show ? 'text' : 'password'} className="pr-11" {...props} />
      <button type="button" onClick={() => setShow((s) => !s)} aria-label={show ? 'Hide password' : 'Show password'} className="absolute right-1 top-1/2 -translate-y-1/2 grid place-items-center size-8 rounded-lg text-muted hover:text-fg">
        {show ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
      </button>
    </div>
  )
}

export function Login() {
  const nav = useNavigate()
  const [params] = useSearchParams()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [totp, setTotp] = useState('')
  const [needTotp, setNeedTotp] = useState(false)
  const [remember, setRemember] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      const r = await api.post<{ user: User }>('/api/auth/login', { username, password, totp, remember })
      useApp.getState().setMe(r.user)
      const next = params.get('next')
      nav(next && next.startsWith('/') && !next.startsWith('//') ? next : '/dashboard', { replace: true })
    } catch (err) {
      const e2 = err as ApiError
      if (e2.code === 'totp_required') {
        setNeedTotp(true)
      } else {
        setError(e2.message)
        if (e2.code === 'totp_invalid') setTotp('')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout>
      <h2 className="font-display text-3xl font-semibold tracking-tight">{needTotp ? 'Two-factor check' : 'Welcome back'}</h2>
      <p className="mt-2 text-sm text-muted">{needTotp ? 'Enter the 6-digit code from your authenticator app.' : 'Sign in to your monitoring panel.'}</p>
      <form onSubmit={submit} className="mt-8 space-y-4">
        {error && <Alert tone="danger">{error}</Alert>}
        {!needTotp ? (
          <>
            <Field label="Username" htmlFor="u">
              <Input id="u" autoComplete="username" autoFocus value={username} onChange={(e) => setUsername(e.target.value)} required />
            </Field>
            <Field label="Password" htmlFor="p">
              <PasswordInput id="p" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
            </Field>
            <Checkbox checked={remember} onChange={setRemember} label={<span className="text-muted">Keep me signed in for 30 days</span>} />
          </>
        ) : (
          <Field label="Authentication code" htmlFor="t">
            <Input id="t" autoFocus inputMode="numeric" autoComplete="one-time-code" maxLength={6} placeholder="123456" className="font-mono text-lg tracking-[0.4em] text-center h-12" value={totp} onChange={(e) => setTotp(e.target.value.replace(/\D/g, ''))} />
          </Field>
        )}
        <Button type="submit" variant="primary" size="lg" className="w-full" loading={busy} icon={<KeyRound className="size-4" />}>
          {needTotp ? 'Verify' : 'Sign in'}
        </Button>
        {needTotp && (
          <button type="button" className="w-full text-[13px] text-muted hover:text-fg" onClick={() => { setNeedTotp(false); setTotp('') }}>
            Use a different account
          </button>
        )}
      </form>
      <p className="mt-10 text-[12.5px] text-subtle leading-relaxed">
        Locked out? On the server run <code className="font-mono text-muted">neo-uptime reset-password &lt;user&gt;</code>.
      </p>
    </AuthLayout>
  )
}

export function Setup({ onDone }: { onDone: () => void }) {
  const nav = useNavigate()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [repeat, setRepeat] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const mismatch = repeat.length > 0 && repeat !== password

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (password !== repeat) return setError('The passwords do not match.')
    setBusy(true)
    setError('')
    try {
      const r = await api.post<{ user: User }>('/api/auth/setup', { username, password })
      useApp.getState().setMe(r.user)
      onDone()
      nav('/dashboard', { replace: true })
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout>
      <span className="inline-flex items-center h-6 px-2.5 rounded-full bg-accent-soft text-accent-ink text-[12px] font-semibold">First run</span>
      <h2 className="mt-3 font-display text-3xl font-semibold tracking-tight">Create the admin account</h2>
      <p className="mt-2 text-sm text-muted">This account can add servers, tunnels, users and everything else. You can invite more people later.</p>
      <form onSubmit={submit} className="mt-8 space-y-4">
        {error && <Alert tone="danger">{error}</Alert>}
        <Field label="Username" htmlFor="u" help="Letters, digits, dot, dash and underscore.">
          <Input id="u" autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required minLength={3} />
        </Field>
        <Field label="Password" htmlFor="p" help="At least 8 characters.">
          <PasswordInput id="p" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
        </Field>
        <Field label="Repeat password" htmlFor="r" error={mismatch ? 'Does not match the password above.' : undefined}>
          <PasswordInput id="r" autoComplete="new-password" value={repeat} onChange={(e) => setRepeat(e.target.value)} required />
        </Field>
        <Button type="submit" variant="primary" size="lg" className="w-full" loading={busy} disabled={mismatch}>
          Create account & continue
        </Button>
      </form>
    </AuthLayout>
  )
}
