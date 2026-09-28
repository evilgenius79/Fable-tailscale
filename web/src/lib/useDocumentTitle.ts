import { useEffect } from 'react'

export const APP_NAME = 'Tailwatch'

/** Set `document.title` to "<title> · Tailwatch" (or just "Tailwatch" when empty) while mounted. */
export function useDocumentTitle(title: string | null | undefined): void {
  useEffect(() => {
    const prev = document.title
    document.title = title ? `${title} · ${APP_NAME}` : APP_NAME
    return () => {
      document.title = prev
    }
  }, [title])
}
