import { useEffect, useMemo, useState } from 'react'
import qrcode from 'qrcode-generator'
import { LogOut, Monitor, ShieldCheck, ShieldOff, Smartphone } from 'lucide-react'
import { api } from '../../lib/api'
import { useApp } from '../../lib/store'
import { fmtDateTime, timeAgo } from '../../lib/format'
import type { User } from '../../lib/types'
import { Alert, Badge, Button, Card, CardHeader, CopyButton, Field, IconButton, Input, Modal, toast } from '../../components/ui'

function QR({ text, size = 200 }: { text: string; size?: number }) {
  const cells = useMemo(() => {
    const qr = qrcode(0, 'M')
    qr.addData(text)
    qr.make()
    const n = qr.getModuleCount()
    const out: [number, number][] = []
    for (let r = 0; r < n; r++) for (let c = 0; c < n; c++) if (qr.isDark(r, c)) out.push([c, r])
    return { n, out }
  }, [text])
  const pad = 2
  return (
    <svg viewBox={`0 0 ${cells.n + pad * 2} ${cells.n + pad * 2}`} width={size} height={size} role="img" aria-label="QR code for your authenticator app" className="rounded-xl" shapeRendering="crispEdges">
      <rect width="100%" height="100%" fill="#fff" />
      {cells.out.map(([x, y]) => <rect key={`${x}-${y}`} x={x + pad} y={y + pad} width={1} height={1} fill="#0b0f0c" />)}
    </svg>
  )
}

interface SessionView { id: string; createdAt: number; lastSeen: number; ip: string; userAgent: string; current: boolean }

function describeUA(ua: string) {
  const b = /Edg\//.test(ua) ? 'Edge' : /Chrome\//.test(ua) ? 'Chrome' : /Firefox\//.test(ua) ? 'Firefox' : /Safari\//.test(ua) ? 'Safari' : 'Browser'
  const os = /Windows/.test(ua) ? 'Windows' : /Android/.test(ua) ? 'Android' : /iPhone|iPad/.test(ua) ? 'iOS' : /Mac OS/.test(ua) ? 'macOS' : /Linux/.test(ua) ? 'Linux' : ''
  return { label: `${b}${os ? ` on ${os}` : ''}`, mobile: /Android|iPhone|iPad|Mobile/.test(ua) }
}

export default function Security() {
  const me = useApp((s) => s.me)
  const [pw, setPw] = useState({ current: '', next: '', repeat: '' })
  const [pwBusy, setPwBusy] = useState(false)
  const [pwErr, setPwErr] = useState('')
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null)
  const [code, setCode] = useState('')
  const [tfErr, setTfErr] = useState('')
  const [disableOpen, setDisableOpen] = useState(false)
  const [disablePw, setDisablePw] = useState('')
  const [sessions, setSessions] = useState<SessionView[]>([])

  const loadSessions = () => api.get<SessionView[]>('/api/me/sessions').then(setSessions).catch(() => {})
  useEffect(() => { loadSessions() }, [])
  const refreshMe = async () => {
    const r = await api.get<{ user: User }>('/api/me')
    useApp.getState().setMe(r.user)
  }

  const changePw = async () => {
    setPwErr('')
    if (pw.next !== pw.repeat) return setPwErr('The new passwords do not match.')
    setPwBusy(true)
    try {
      await api.put('/api/me/password', { current: pw.current, new: pw.next })
      toast.success('Password changed', 'Other devices were signed out.')
      setPw({ current: '', next: '', repeat: '' })
      loadSessions()
    } catch (e) {
      setPwErr((e as Error).message)
    } finally {
      setPwBusy(false)
    }
  }

  return (
    <>
      <Card>
        <CardHeader title="Password" subtitle={`Signed in as ${me?.username}.`} />
        <div className="px-5 pb-5 space-y-4 max-w-md">
          {pwErr && <Alert tone="danger">{pwErr}</Alert>}
          <Field label="Current password" htmlFor="cp"><Input id="cp" type="password" autoComplete="current-password" value={pw.current} onChange={(e) => setPw({ ...pw, current: e.target.value })} /></Field>
          <Field label="New password" htmlFor="np" help="At least 8 characters."><Input id="np" type="password" autoComplete="new-password" value={pw.next} onChange={(e) => setPw({ ...pw, next: e.target.value })} /></Field>
          <Field label="Repeat new password" htmlFor="rp"><Input id="rp" type="password" autoComplete="new-password" value={pw.repeat} onChange={(e) => setPw({ ...pw, repeat: e.target.value })} /></Field>
          <Button variant="primary" loading={pwBusy} disabled={!pw.current || !pw.next} onClick={changePw}>Change password</Button>
        </div>
      </Card>

      <Card>
        <CardHeader title="Two-factor sign-in" subtitle="A 6-digit code from any authenticator app (Google Authenticator, Aegis, 1Password…)." actions={me?.totpEnabled ? <Badge tone="up">On</Badge> : <Badge>Off</Badge>} />
        <div className="px-5 pb-5">
          {me?.totpEnabled ? (
            <div className="flex flex-wrap items-center justify-between gap-3">
              <p className="flex items-center gap-2 text-sm text-muted"><ShieldCheck className="size-4 text-up" />Your account asks for a code at every sign-in.</p>
              <Button variant="danger" size="sm" icon={<ShieldOff className="size-3.5" />} onClick={() => { setDisablePw(''); setDisableOpen(true) }}>Turn off</Button>
            </div>
          ) : setup ? (
            <div className="grid sm:grid-cols-[auto_1fr] gap-6 items-start">
              <QR text={setup.uri} />
              <div className="space-y-4 min-w-0">
                <ol className="text-sm text-muted space-y-1 list-decimal pl-4">
                  <li>Scan the code with your authenticator app.</li>
                  <li>Or type this key: <span className="inline-flex items-center gap-2"><code className="font-mono text-fg break-all">{setup.secret}</code></span></li>
                  <li>Enter the 6 digits it shows to confirm.</li>
                </ol>
                <CopyButton text={setup.secret} label="Copy key" />
                {tfErr && <Alert tone="danger">{tfErr}</Alert>}
                <div className="flex gap-2 max-w-xs">
                  <Input inputMode="numeric" maxLength={6} placeholder="123456" aria-label="Code" className="font-mono tracking-[0.3em] text-center" value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} />
                  <Button variant="primary" disabled={code.length !== 6} onClick={async () => {
                    setTfErr('')
                    try {
                      await api.post('/api/me/totp/enable', { code })
                      await refreshMe()
                      setSetup(null)
                      toast.success('Two-factor sign-in is on', 'If you lose the phone, run `neo-uptime disable-2fa <user>` on the server.')
                    } catch (e) {
                      setTfErr((e as Error).message)
                    }
                  }}>Confirm</Button>
                </div>
              </div>
            </div>
          ) : (
            <Button variant="soft" icon={<ShieldCheck className="size-4" />} onClick={async () => {
              try { setSetup(await api.post('/api/me/totp/setup')) } catch (e) { toast.error('Could not start', (e as Error).message) }
            }}>Set up two-factor sign-in</Button>
          )}
        </div>
      </Card>

      <Card>
        <CardHeader title="Signed-in devices" actions={sessions.length > 1 && <Button size="sm" onClick={async () => { await api.post('/api/me/sessions/revoke-others'); loadSessions(); toast.success('Other devices signed out') }}>Sign out others</Button>} />
        <div className="px-3 pb-3">
          {sessions.map((s) => {
            const ua = describeUA(s.userAgent)
            return (
              <div key={s.id} className="flex items-center gap-3 rounded-xl px-3 py-3 hover:bg-surface-2">
                <span className="grid place-items-center size-9 rounded-xl bg-surface-2 border border-line text-muted">{ua.mobile ? <Smartphone className="size-4" /> : <Monitor className="size-4" />}</span>
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium">{ua.label} {s.current && <Badge tone="accent" className="ml-1">This device</Badge>}</p>
                  <p className="text-[12.5px] text-muted font-mono">{s.ip} · active {timeAgo(s.lastSeen * 1000)} · since {fmtDateTime(s.createdAt * 1000)}</p>
                </div>
                {!s.current && <IconButton size="sm" label="Sign out this device" onClick={async () => { await api.del(`/api/me/sessions/${s.id}`); loadSessions() }}><LogOut className="size-4" /></IconButton>}
              </div>
            )
          })}
        </div>
      </Card>

      <Modal open={disableOpen} onClose={() => setDisableOpen(false)} title="Turn off two-factor sign-in?" size="sm"
        footer={<><Button variant="ghost" onClick={() => setDisableOpen(false)}>Cancel</Button><Button variant="danger" disabled={!disablePw} onClick={async () => {
          try {
            await api.post('/api/me/totp/disable', { password: disablePw })
            await refreshMe()
            setDisableOpen(false)
            toast.success('Two-factor sign-in turned off')
          } catch (e) { toast.error('Not turned off', (e as Error).message) }
        }}>Turn off</Button></>}>
        <Field label="Confirm with your password" htmlFor="dp"><Input id="dp" data-autofocus type="password" value={disablePw} onChange={(e) => setDisablePw(e.target.value)} /></Field>
      </Modal>
    </>
  )
}
