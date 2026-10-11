import { useT } from '../lib/i18n'
import { clsx } from '../lib/format'

export const REPO_URL = 'https://github.com/neoauroraproject/Neo-Uptime'
export const CHANNEL_URL = 'https://t.me/neoaurora'

// Brand marks are drawn inline: the icon set has no logos.
export function GitHubMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden className={className}>
      <path d="M12 .5a11.5 11.5 0 0 0-3.64 22.41c.58.1.79-.25.79-.56v-2c-3.2.7-3.88-1.37-3.88-1.37-.52-1.33-1.28-1.69-1.28-1.69-1.05-.71.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.7 1.26 3.36.96.1-.75.4-1.26.73-1.55-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.28 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 0 1 5.77 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.81 1.19 1.83 1.19 3.09 0 4.42-2.7 5.39-5.26 5.68.41.36.78 1.06.78 2.14v3.17c0 .31.21.67.8.56A11.5 11.5 0 0 0 12 .5Z" />
    </svg>
  )
}

export function TelegramMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden className={className}>
      <path d="M12 0a12 12 0 1 0 0 24 12 12 0 0 0 0-24Zm5.56 8.16-1.82 8.6c-.14.61-.5.76-1.01.47l-2.8-2.06-1.35 1.3c-.15.15-.27.27-.56.27l.2-2.85 5.18-4.68c.23-.2-.05-.31-.35-.11l-6.4 4.03-2.76-.86c-.6-.19-.61-.6.13-.89l10.78-4.16c.5-.18.94.12.76.94Z" />
    </svg>
  )
}

/** The project's GitHub and Telegram channel, as two small links. */
export function ProjectLinks({ className, labels = false }: { className?: string; labels?: boolean }) {
  const t = useT()
  const item = 'inline-flex items-center gap-2 rounded-lg text-muted hover:text-fg transition-colors'
  return (
    <div className={clsx('flex items-center', labels ? 'flex-wrap gap-x-5 gap-y-2' : 'gap-1', className)}>
      <a href={REPO_URL} target="_blank" rel="noreferrer" className={clsx(item, !labels && 'p-1.5 hover:bg-surface-2')} title="GitHub" aria-label={t('Neo Uptime on GitHub')}>
        <GitHubMark className="size-4" />{labels && <span className="text-sm" dir="ltr">github.com/neoauroraproject/Neo-Uptime</span>}
      </a>
      <a href={CHANNEL_URL} target="_blank" rel="noreferrer" className={clsx(item, !labels && 'p-1.5 hover:bg-surface-2')} title={t('Telegram channel')} aria-label={t('Neo Aurora on Telegram')}>
        <TelegramMark className="size-4" />{labels && <span className="text-sm" dir="ltr">t.me/neoaurora</span>}
      </a>
    </div>
  )
}
