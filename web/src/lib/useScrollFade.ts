import { useEffect, useState, type CSSProperties, type RefObject } from 'react'

const FADE = 40

/**
 * Horizontal scroll affordance for `overflow-x-auto` strips that hide their
 * scrollbar (tab lists, section navs): returns a `mask-image` style that fades
 * whichever edge still has content beyond it, and nothing when it all fits.
 */
export function useScrollFade<T extends HTMLElement>(ref: RefObject<T | null>): CSSProperties | undefined {
  const [edges, setEdges] = useState<{ left: boolean; right: boolean }>({ left: false, right: false })
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const update = () => {
      const left = el.scrollLeft > 1
      const right = el.scrollLeft + el.clientWidth < el.scrollWidth - 1
      setEdges((prev) => (prev.left === left && prev.right === right ? prev : { left, right }))
    }
    update()
    el.addEventListener('scroll', update, { passive: true })
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(update) : null
    ro?.observe(el)
    for (const child of Array.from(el.children)) ro?.observe(child)
    return () => {
      el.removeEventListener('scroll', update)
      ro?.disconnect()
    }
  }, [ref])
  if (!edges.left && !edges.right) return undefined
  const from = edges.left ? `transparent 0, black ${FADE}px` : 'black 0'
  const to = edges.right ? `black calc(100% - ${FADE}px), transparent 100%` : 'black 100%'
  const mask = `linear-gradient(to right, ${from}, ${to})`
  return { maskImage: mask, WebkitMaskImage: mask }
}
