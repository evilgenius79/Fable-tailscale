// Optimistic rule saves: patch the rules cache immediately, roll back on
// failure (the global mutation toast reports the error), and let the hook's
// own onSuccess settle the cache with the server's copy.

import { useCallback, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useSaveRule } from '../../api/hooks'
import { queryKeys } from '../../api/queryKeys'
import type { AlertRule } from '../../api/types'

export function useRuleSave() {
  const qc = useQueryClient()
  const save = useSaveRule()
  const [pendingId, setPendingId] = useState<string | null>(null)

  const saveRule = useCallback(
    (rule: AlertRule, opts?: { onSuccess?: (saved: AlertRule) => void; onError?: (err: Error) => void }) => {
      const previous = qc.getQueryData<AlertRule[]>(queryKeys.rules)
      qc.setQueryData<AlertRule[]>(queryKeys.rules, (list) => (list ? list.map((r) => (r.id === rule.id ? { ...r, ...rule } : r)) : list))
      setPendingId(rule.id)
      save.mutate(rule, {
        onSuccess: (saved) => opts?.onSuccess?.(saved),
        onError: (err) => {
          if (previous) qc.setQueryData<AlertRule[]>(queryKeys.rules, previous)
          opts?.onError?.(err)
        },
        onSettled: () => setPendingId((cur) => (cur === rule.id ? null : cur)),
      })
    },
    [qc, save],
  )

  return { saveRule, pendingId, isPending: save.isPending }
}
