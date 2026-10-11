import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { CircleCheck, CircleDashed, ExternalLink, Globe, LockKeyhole, RefreshCw, Save, TriangleAlert } from 'lucide-react'
import { api } from '../../lib/api'
import { clsx, fmtDate } from '../../lib/format'
import { useT } from '../../lib/i18n'
import type { HttpsStatus } from '../../lib/types'
import { useApp } from '../../lib/store'
import { domainURL } from '../StatusPages'
import { Alert, Badge, Button, Card, CardHeader, CopyButton, Empty, Field, Input, Skeleton, Spinner, Switch, toast } from '../../components/ui'

type S = Record<string, string>

export default function HTTPS() {
  const t = useT()
  const [s, setS] = useState<S | null>(null)
  const [st, setSt] = useState<HttpsStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [checking, setChecking] = useState(false)

  const loadStatus = () => {
    setChecking(true)
    return api.get<HttpsStatus>('/api/https').then(setSt).catch(() => {}).finally(() => setChecking(false))
  }
  useEffect(() => {
    api.get<S>('/api/settings').then(setS).catch(() => setS({}))
    loadStatus()
  }, [])
  // While a certificate is being requested, keep checking.
  const issuing = st?.domains.some((d) => d.issuing)
  useEffect(() => {
    if (!issuing) return
    const h = setInterval(loadStatus, 4000)
    return () => clearInterval(h)
  }, [issuing])

  if (!s) return <Skeleton className="h-96 rounded-[18px]" />

  const save = async () => {
    setBusy(true)
    try {
      const r = await api.put<S>('/api/settings', { httpsEnabled: s.httpsEnabled, acmeEmail: s.acmeEmail, panelDomain: s.panelDomain })
      setS(r)
      toast.success(t('Settings saved'))
      // The listeners restart in the background.
      setTimeout(loadStatus, 1500)
    } catch (e) {
      toast.error(t('Could not save'), (e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const issue = async (domain: string) => {
    try {
      await api.post('/api/https/issue', { domain, enable: true })
      toast.info(t('Requesting a certificate for {d}…', { d: domain }))
      setTimeout(loadStatus, 800)
    } catch (e) {
      toast.error(t('That did not work'), (e as Error).message)
    }
  }
  const ip = st?.serverIps[0]
  const panel = st?.domains.find((d) => d.kind === 'panel' && d.domain === s.panelDomain)
  const panelURL = panel?.issued && st?.running ? domainURL(panel.domain, st) : ''
  const usePanelURL = async () => {
    try {
      setS(await api.put<S>('/api/settings', { primaryBaseURL: panelURL }))
      await useApp.getState().loadSettings()
      toast.success(t('Settings saved'))
    } catch (e) {
      toast.error(t('Could not save'), (e as Error).message)
    }
  }

  return (
    <>
      <Card>
        <CardHeader title={t('Automatic HTTPS')} subtitle={t('Free certificates from Let’s Encrypt for the panel’s and the status pages’ own domains — requested and renewed on their own.')} icon={<LockKeyhole className="size-4" />}
          actions={st?.running ? <Badge tone="up">{t('On')}</Badge> : <Badge>{t('Off')}</Badge>} />
        <div className="px-5 pb-5 space-y-5">
          <Switch checked={s.httpsEnabled === 'true'} onChange={(v) => setS({ ...s, httpsEnabled: String(v) })} label={t('Get certificates automatically')}
            description={t('Neo Uptime then also listens on ports 80 and 443. Another web server on those ports (nginx, Caddy, a panel) has to move, or keep using it as your proxy instead.')} />
          <div className="grid sm:grid-cols-2 gap-4">
            <Field label={t('Panel domain (optional)')} htmlFor="pd" help={t('Open this admin panel at its own address with HTTPS, e.g. uptime.example.com.')}>
              <Input id="pd" dir="ltr" className="font-mono" value={s.panelDomain} onChange={(e) => setS({ ...s, panelDomain: e.target.value.trim().toLowerCase() })} placeholder="uptime.example.com" />
            </Field>
            <Field label={t('E-mail (optional)')} htmlFor="am" help={t('Let’s Encrypt may write here about your certificates.')}>
              <Input id="am" dir="ltr" type="email" value={s.acmeEmail} onChange={(e) => setS({ ...s, acmeEmail: e.target.value })} placeholder="you@example.com" />
            </Field>
          </div>
          {st?.listenError && <Alert tone="danger">{st.listenError}</Alert>}
          {st?.httpError && <Alert tone="warning">{st.httpError}</Alert>}
          {st?.running && !st.listenError && <p className="flex items-center gap-2 text-[13px] text-up"><CircleCheck className="size-4" />{t('Listening on {a} and {b}.', { a: st.httpsAddr, b: st.httpAddr })}</p>}
          {panelURL && s.primaryBaseURL !== panelURL && (
            <Alert tone="success" title={t('The panel has HTTPS on {d}', { d: s.panelDomain })}>
              {t('Use it as the public address, so alert links, push URLs and agent commands have no port.')}{' '}
              <button type="button" className="text-accent-ink underline" onClick={usePanelURL}>{t('Use {u}', { u: panelURL })}</button>
            </Alert>
          )}
          <div className="flex justify-end"><Button variant="primary" loading={busy} icon={<Save className="size-4" />} onClick={save}>{t('Save')}</Button></div>
        </div>
      </Card>

      <Card>
        <CardHeader title={t('Domains')} subtitle={t('Set a status page’s domain in its editor; set the panel’s domain above.')} icon={<Globe className="size-4" />}
          actions={<Button size="sm" loading={checking} icon={<RefreshCw className="size-3.5" />} onClick={loadStatus}>{t('Check again')}</Button>} />
        {ip && (
          <div className="px-5 pb-3 text-[13px] text-muted flex flex-wrap items-center gap-2">
            {t('Point each domain to this server with an A record:')}
            <code className="rounded-lg bg-surface-2 border border-line px-2 py-1 font-mono text-fg" dir="ltr">{ip}</code><CopyButton text={ip} />
          </div>
        )}
        {!st ? <div className="px-5 pb-5"><Skeleton className="h-24" /></div> : st.domains.length === 0 ? (
          <Empty icon={<Globe className="size-6" />} title={t('No domains yet')} body={t('Open a status page in the editor and enter its own domain, such as status.example.com.')} action={<Link to="/status-pages"><Button size="sm">{t('Status pages')}</Button></Link>} />
        ) : (
          <div className="px-3 pb-3">
            {st.domains.map((d) => (
              <div key={d.domain} className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl px-3 py-3 hover:bg-surface-2">
                <div className="min-w-0 flex-1 basis-56">
                  <p className="font-medium font-mono truncate" dir="ltr">{d.domain}</p>
                  <p className="text-[12.5px] text-muted truncate">{d.kind === 'panel' ? t('Admin panel') : t('Status page: {name}', { name: d.title ?? '' })}</p>
                </div>
                <span className={clsx('inline-flex items-center gap-1.5 text-[13px]', d.pointsHere ? 'text-up' : 'text-pending')}>
                  {d.pointsHere ? <CircleCheck className="size-4" /> : <TriangleAlert className="size-4" />}
                  <span dir="auto">{d.pointsHere ? t('DNS points here') : d.ips.length ? t('DNS points to {ip}', { ip: d.ips.join(', ') }) : t('No DNS record yet')}</span>
                </span>
                <span className={clsx('inline-flex items-center gap-1.5 text-[13px]', d.issued ? 'text-up' : 'text-muted')}>
                  {d.issued ? <LockKeyhole className="size-4" /> : d.issuing ? <Spinner className="size-3.5" /> : <CircleDashed className="size-4" />}
                  {d.issued ? t('HTTPS until {d}', { d: fmtDate((d.notAfter ?? 0) * 1000) }) : d.issuing ? t('Getting a certificate…') : t('No certificate yet')}
                </span>
                <div className="flex gap-2 ms-auto">
                  {!d.issued && <Button size="sm" variant="soft" loading={d.issuing} onClick={() => issue(d.domain)}>{t('Get SSL now')}</Button>}
                  <a href={domainURL(d.domain, st)} target="_blank" rel="noreferrer"><Button size="sm" icon={<ExternalLink className="size-3.5" />}>{t('Open')}</Button></a>
                </div>
                {d.error && d.pointsHere && !d.issued && <p className="basis-full text-[12.5px] text-down" dir="auto">{d.error}</p>}
              </div>
            ))}
          </div>
        )}
        {st && !st.enabled && st.domains.length > 0 && <div className="px-5 pb-5"><Alert tone="info">{t('Domains already work over plain HTTP through a proxy you run. Turn on automatic HTTPS above to have Neo Uptime serve them with certificates.')}</Alert></div>}
      </Card>
    </>
  )
}
