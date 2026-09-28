import { Link } from 'react-router-dom'
import { Compass } from 'lucide-react'
import { buttonClass } from '../components/ui/Button'
import { EmptyState } from '../components/ui/EmptyState'

export default function NotFoundPage() {
  return (
    <EmptyState
      icon={Compass}
      title="Page not found"
      description="That address does not match anything in Tailwatch."
      size="lg"
      action={
        <Link to="/" className={buttonClass({ variant: 'primary' })}>
          Back to overview
        </Link>
      }
    />
  )
}
