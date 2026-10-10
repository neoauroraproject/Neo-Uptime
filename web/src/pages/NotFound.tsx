import { useNavigate } from 'react-router-dom'
import { Compass } from 'lucide-react'
import { Button, Empty } from '../components/ui'

export default function NotFound() {
  const nav = useNavigate()
  return (
    <div className="p-10">
      <Empty icon={<Compass className="size-6" />} title="Page not found" body="That address does not exist in this panel." action={<Button onClick={() => nav('/dashboard')}>Go to dashboard</Button>} />
    </div>
  )
}
