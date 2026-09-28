import { useEffect, useMemo, useRef, useState } from 'react'
import { Info, Lock, Radio } from 'lucide-react'
import type { AlertRule, Device, Severity } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatDateTime } from '../../lib/format'
import { severityTone } from '../../lib/status'
import { Badge, TONE_SOFT_BG, TONE_TEXT } from '../ui/Badge'
import { Button } from '../ui/Button'
import { Dialog } from '../ui/Dialog'
import { Field, Input, Textarea } from '../ui/Input'
import { Select } from '../ui/Select'
import { toast } from '../ui/Toast'
import { Toggle } from '../ui/Toggle'
import { DurationInput } from './DurationInput'
import { DeviceListEditor, TagListEditor } from './ScopeEditor'
import { draftFromRule, formatForSeconds, formatThreshold, ruleFromDraft, ruleIcon, ruleMeta, severityOptions, validateRuleDraft, type RuleDraft, type RuleDraftErrors } from './alerts'
import { useRuleSave } from './useRuleSave'

export interface RuleEditorDialogProps {
  rule: AlertRule | null
  open: boolean
  onClose: () => void
  isAdmin: boolean
  devices: ReadonlyArray<Pick<Device, 'id' | 'name' | 'tags'>>
}

/** Edit one rule (admins) or inspect it read-only (viewers). Inputs adapt to the rule type's unit. */
export function RuleEditorDialog({ rule, open, onClose, isAdmin, devices }: RuleEditorDialogProps) {
  const [draft, setDraft] = useState<RuleDraft | null>(null)
  const [errors, setErrors] = useState<RuleDraftErrors>({})
  const [touched, setTouched] = useState(false)
  const nameRef = useRef<HTMLInputElement>(null)
  const { saveRule, isPending } = useRuleSave()

  // Re-seed the draft whenever a different rule opens (or the dialog reopens).
  useEffect(() => {
    if (open && rule) {
      setDraft(draftFromRule(rule))
      setErrors({})
      setTouched(false)
    }
  }, [open, rule])

  const tagSuggestions = useMemo(() => Array.from(new Set(devices.flatMap((d) => d.tags))).sort(), [devices])

  if (!rule || !draft) return null
  const meta = ruleMeta(rule.type)
  const Icon = ruleIcon(rule.type)
  const readOnly = !isAdmin
  const patch = (p: Partial<RuleDraft>) => {
    const next = { ...draft, ...p }
    setDraft(next)
    if (touched) setErrors(validateRuleDraft(next, rule.type))
  }
  const dirty = JSON.stringify(ruleFromDraft(rule, draft)) !== JSON.stringify(ruleFromDraft(rule, draftFromRule(rule)))

  const submit = () => {
    const errs = validateRuleDraft(draft, rule.type)
    setErrors(errs)
    setTouched(true)
    if (Object.keys(errs).length) {
      if (errs.name) nameRef.current?.focus()
      return
    }
    const next = ruleFromDraft(rule, draft)
    saveRule(next, {
      onSuccess: (saved) => {
        toast.success('Rule saved', `${saved.name} · ${saved.enabled ? 'enabled' : 'disabled'}`, { duration: 2500 })
        onClose()
      },
    })
  }

  const previewThreshold = meta.unit !== 'none' ? formatThreshold({ type: rule.type, threshold: Number(draft.threshold) || 0 }) : null
  const previewFor = meta.forLabel ? formatForSeconds((Number(draft.minutes) || 0) * 60 + (Number(draft.seconds) || 0)) : null

  return (
    <Dialog
      open={open}
      onClose={onClose}
      size="xl"
      title={
        <span className="flex items-center gap-2.5">
          <span className={cn('flex size-7 items-center justify-center rounded-md', TONE_SOFT_BG[severityTone(draft.severity)], TONE_TEXT[severityTone(draft.severity)])}>
            <Icon className="size-4" aria-hidden="true" />
          </span>
          <span className="truncate">{readOnly ? rule.name : `Edit: ${rule.name}`}</span>
          {readOnly ? (
            <Badge size="sm" tone="neutral" icon={Lock}>
              Read-only
            </Badge>
          ) : null}
        </span>
      }
      description={`${meta.label} rule · last saved ${formatDateTime(rule.updatedAt)}`}
      footer={
        <>
          {readOnly ? (
            <span className="mr-auto text-xs text-fg-muted">Only admins can change rules.</span>
          ) : (
            <span className="mr-auto text-xs text-fg-muted" aria-live="polite">
              {dirty ? 'Unsaved changes' : 'No changes'}
            </span>
          )}
          <Button variant="ghost" onClick={onClose} disabled={isPending}>
            {readOnly ? 'Close' : 'Cancel'}
          </Button>
          {!readOnly ? (
            <Button variant="primary" onClick={submit} loading={isPending} disabled={!dirty}>
              Save rule
            </Button>
          ) : null}
        </>
      }
    >
      <form
        className="space-y-5"
        onSubmit={(e) => {
          e.preventDefault()
          if (!readOnly) submit()
        }}
        aria-describedby="rule-explanation"
      >
        <div id="rule-explanation" className="flex items-start gap-2.5 rounded-lg border border-border bg-surface-inset px-3 py-2.5 text-[13px] leading-5 text-fg-secondary">
          <Info className="mt-0.5 size-4 shrink-0 text-fg-muted" aria-hidden="true" />
          <div className="min-w-0 space-y-1">
            <p>{meta.explanation}</p>
            {meta.escalation ? <p className="text-warning">{meta.escalation}</p> : null}
            <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-fg-muted">
              <span>
                Defaults: {meta.unit !== 'none' ? `${formatThreshold({ type: rule.type, threshold: meta.defaults.threshold })} · ` : ''}
                {meta.forLabel ? `${formatForSeconds(meta.defaults.forSeconds)} · ` : ''}
                {meta.defaults.severity}
              </span>
              {meta.needsAgent ? (
                <span className="inline-flex items-center gap-1">
                  <Radio className="size-3" aria-hidden="true" />
                  needs tailwatch-agent
                </span>
              ) : null}
            </p>
          </div>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Name" required error={errors.name} className="sm:col-span-2">
            {({ id, describedBy }) => (
              <Input ref={nameRef} id={id} value={draft.name} onChange={(e) => patch({ name: e.target.value })} invalid={!!errors.name} aria-describedby={describedBy} disabled={readOnly} maxLength={80} />
            )}
          </Field>
          <Field label="Description" hint="Shown in the rules table." className="sm:col-span-2">
            {({ id, describedBy }) => <Textarea id={id} rows={2} value={draft.description} onChange={(e) => patch({ description: e.target.value })} aria-describedby={describedBy} disabled={readOnly} maxLength={300} />}
          </Field>
          <Field label="Severity" hint={meta.escalation ? 'Built-in escalation still applies.' : undefined}>
            {({ id, describedBy }) => <Select<Severity> id={id} value={draft.severity} onValueChange={(severity) => patch({ severity })} options={severityOptions()} aria-describedby={describedBy} disabled={readOnly} className="w-full" />}
          </Field>
          {meta.unit !== 'none' ? (
            <Field label={meta.thresholdLabel ?? 'Threshold'} required error={errors.threshold} hint={`${meta.min}–${meta.max} ${meta.unitLabel}`.trim()}>
              {({ id, describedBy }) => (
                <Input
                  id={id}
                  type="number"
                  inputMode="decimal"
                  min={meta.min}
                  max={meta.max}
                  step={meta.step}
                  value={draft.threshold}
                  onChange={(e) => patch({ threshold: e.target.value })}
                  invalid={!!errors.threshold}
                  aria-describedby={describedBy}
                  disabled={readOnly}
                  className="num"
                  trailing={<span className="pr-1.5 text-xs text-fg-muted">{meta.unitLabel}</span>}
                />
              )}
            </Field>
          ) : null}
          {meta.forLabel ? (
            <Field label={meta.forLabel} error={errors.duration} hint="Minutes and seconds; 0 fires as soon as the condition is seen." className={meta.unit === 'none' ? '' : 'sm:col-span-2'}>
              {({ id, describedBy }) => <DurationInput id={id} minutes={draft.minutes} seconds={draft.seconds} onChange={(d) => patch(d)} disabled={readOnly} invalid={!!errors.duration} describedBy={describedBy} />}
            </Field>
          ) : null}
        </div>

        <div className="grid gap-3 rounded-lg border border-border p-3 sm:grid-cols-2 sm:gap-6 sm:p-4">
          <Toggle checked={draft.enabled} onCheckedChange={(enabled) => patch({ enabled })} label="Enabled" description="Disabled rules keep their settings but never open alerts." disabled={readOnly} />
          <Toggle checked={draft.notify} onCheckedChange={(notify) => patch({ notify })} label="Notify" description="Send to the configured notifiers when an alert opens or resolves." disabled={readOnly} />
        </div>

        <fieldset className="space-y-4">
          <legend className="text-[13px] font-semibold text-fg">Scope</legend>
          <p className="-mt-2 text-xs text-fg-muted">Leave everything empty to apply the rule to every device. Include lists narrow it down; exclude lists carve devices out.</p>
          {errors.scope ? (
            <p role="alert" className="text-xs text-critical">
              {errors.scope}
            </p>
          ) : null}
          <div className="grid gap-4 sm:grid-cols-2">
            <TagListEditor idPrefix="inc-tags" label="Include tags" tags={draft.includeTags} onChange={(includeTags) => patch({ includeTags })} suggestions={tagSuggestions} disabled={readOnly} />
            <TagListEditor idPrefix="exc-tags" label="Exclude tags" tags={draft.excludeTags} onChange={(excludeTags) => patch({ excludeTags })} suggestions={tagSuggestions} disabled={readOnly} />
            <DeviceListEditor idPrefix="inc-dev" label="Include devices" ids={draft.includeDevices} onChange={(includeDevices) => patch({ includeDevices })} devices={devices} disabled={readOnly} />
            <DeviceListEditor idPrefix="exc-dev" label="Exclude devices" ids={draft.excludeDevices} onChange={(excludeDevices) => patch({ excludeDevices })} devices={devices} disabled={readOnly} />
          </div>
        </fieldset>

        <p className="rounded-md bg-surface-inset px-3 py-2 text-xs text-fg-secondary" aria-live="polite">
          <span className="font-medium text-fg">Summary:</span> {draft.enabled ? 'Opens' : 'Would open'} a <span className="font-medium">{draft.severity}</span> alert
          {previewThreshold ? (
            <>
              {' '}
              when {meta.thresholdLabel?.toLowerCase()} <span className="num font-medium text-fg">{previewThreshold}</span>
            </>
          ) : null}
          {previewFor ? (
            <>
              {' '}
              {meta.forLabel?.toLowerCase()} <span className="num font-medium text-fg">{previewFor}</span>
            </>
          ) : null}
          {draft.notify ? ', and notifies.' : ', without notifying.'}
        </p>
        {!readOnly ? <button type="submit" className="sr-only">Save rule</button> : null}
      </form>
    </Dialog>
  )
}
