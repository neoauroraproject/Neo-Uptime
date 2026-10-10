import { useNavigate } from 'react-router-dom'
import { Compass } from 'lucide-react'
import { useT } from '../lib/i18n'
import { Button, Empty } from '../components/ui'

export default function NotFound() {
  const t = useT()
  const nav = useNavigate()
  return (
    <div className="p-10">
      <Empty icon={<Compass className="size-6" />} title={t('Page not found')} body={t('That address does not exist in this panel.')} action={<Button onClick={() => nav('/dashboard')}>{t('Go to dashboard')}</Button>} />
    </div>
  )
}
