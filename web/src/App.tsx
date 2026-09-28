import { MutationCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from 'react-router-dom'
import { errorMessage, errorTitle, isApiError } from './api/client'
import { useLiveStream } from './api/sse'
import { Toaster, toast } from './components/ui/Toast'
import { router } from './router'

/** Shared QueryClient: no retries on 4xx, 15s default staleTime, global mutation error toasts. */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      gcTime: 5 * 60_000,
      refetchOnWindowFocus: true,
      retry: (count, err) => {
        if (isApiError(err) && err.status >= 400 && err.status < 500) return false
        return count < 2
      },
      retryDelay: (attempt) => Math.min(1000 * 2 ** attempt, 8000),
    },
    mutations: { retry: false },
  },
  mutationCache: new MutationCache({
    onError: (err, _vars, _ctx, mutation) => {
      // Mutations that pass their own onError still get this global toast unless they set meta.silent.
      if ((mutation.meta as { silent?: boolean } | undefined)?.silent) return
      toast.error(errorTitle(err), errorMessage(err))
    },
  }),
})

function LiveStream() {
  useLiveStream()
  return null
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <LiveStream />
      <RouterProvider router={router} />
      <Toaster />
    </QueryClientProvider>
  )
}
