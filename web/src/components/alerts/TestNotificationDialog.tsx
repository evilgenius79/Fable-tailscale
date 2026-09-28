import { useEffect, useRef, useState } from 'react'
import { CircleCheck, CircleX, Send } from 'lucide-react'
import { errorMessage } from '../../api/client'
import { useTestNotification, type TestNotificationResult } from '../../api/hooks'
import { Button } from '../ui/Button'
import { Dialog } from '../ui/Dialog'
import { Field, Textarea } from '../ui/Input'
import { notifierMeta } from '../settings/settings'

export interface TestNotificationDialogProps {
  open: boolean
  onClose: () => void
  /** Configured notifier kinds (from /settings) for the preamble. */
  notifiers?: string[]
}

const DEFAULT_MESSAGE = 'Test notification from Tailwatch — if you can read this, alerts will reach you.'

/** Sends `{ message }` to every configured notifier and shows per-notifier results. */
export function TestNotificationDialog({ open, onClose, notifiers }: TestNotificationDialogProps) {
  const [message, setMessage] = useState(DEFAULT_MESSAGE)
  const [result, setResult] = useState<TestNotificationResult | null>(null)
  const [failure, setFailure] = useState<string | null>(null)
  const textRef = useRef<HTMLTextAreaElement>(null)
  const test = useTestNotification()

  useEffect(() => {
    if (open) {
      setResult(null)
      setFailure(null)
    }
  }, [open])

  const send = () => {
    setResult(null)
    setFailure(null)
    test.mutate(
      { message: message.trim() || DEFAULT_MESSAGE },
      {
        onSuccess: (r) => setResult({ sent: r?.sent ?? [], errors: r?.errors ?? {} }),
        onError: (e) => setFailure(errorMessage(e)),
      },
    )
  }

  const sentCount = result?.sent.length ?? 0
  const errorEntries = Object.entries(result?.errors ?? {})
  const none = notifiers && notifiers.length === 0

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Send a test notification"
      description={none ? 'No notifiers are configured on the hub, so nothing will be delivered.' : `Delivers to ${notifiers?.length ? notifiers.map((n) => notifierMeta(n).label).join(', ') : 'every configured notifier'}.`}
      initialFocus={textRef}
      size="md"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={test.isPending}>
            Close
          </Button>
          <Button variant="primary" leadingIcon={Send} onClick={send} loading={test.isPending}>
            {result ? 'Send again' : 'Send test'}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="Message" hint="Sent as the notification body. Nothing is stored.">
          {({ id, describedBy }) => <Textarea ref={textRef} id={id} rows={3} value={message} onChange={(e) => setMessage(e.target.value)} aria-describedby={describedBy} maxLength={500} disabled={test.isPending} />}
        </Field>
        {result ? (
          <div role="status" className="space-y-2 rounded-lg border border-border bg-surface-inset p-3 text-[13px]" aria-live="polite">
            <p className="font-medium text-fg">
              {sentCount ? `Delivered to ${sentCount} notifier${sentCount === 1 ? '' : 's'}` : 'Nothing was delivered'}
              {errorEntries.length ? ` · ${errorEntries.length} failed` : ''}
            </p>
            <ul className="space-y-1">
              {result.sent.map((k) => (
                <li key={`ok-${k}`} className="flex items-center gap-2 text-fg-secondary">
                  <CircleCheck className="size-4 shrink-0 text-online" aria-hidden="true" />
                  <span className="sr-only">Sent: </span>
                  {notifierMeta(k).label}
                </li>
              ))}
              {errorEntries.map(([k, msg]) => (
                <li key={`err-${k}`} className="flex items-start gap-2 text-fg-secondary">
                  <CircleX className="mt-0.5 size-4 shrink-0 text-critical" aria-hidden="true" />
                  <span className="min-w-0 break-words">
                    <span className="sr-only">Failed: </span>
                    <span className="font-medium text-fg">{notifierMeta(k).label}</span> · {msg}
                  </span>
                </li>
              ))}
              {!result.sent.length && !errorEntries.length ? <li className="text-fg-muted">No notifiers are configured on the hub.</li> : null}
            </ul>
          </div>
        ) : null}
        {failure ? (
          <p role="alert" className="rounded-lg border border-critical/30 bg-critical-soft px-3 py-2 text-[13px] text-fg">
            {failure}
          </p>
        ) : null}
      </div>
    </Dialog>
  )
}
