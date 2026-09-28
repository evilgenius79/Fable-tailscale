// @vitest-environment jsdom
import { act, cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Dialog } from './Dialog'

afterEach(cleanup)

function DialogWithInput({ onClose }: { onClose: () => void }) {
  return (
    <Dialog open onClose={onClose} title="Rename">
      <input aria-label="Name" defaultValue="x" />
    </Dialog>
  )
}

describe('Dialog', () => {
  it('keeps focus in the dialog when the parent re-renders with a new onClose identity', () => {
    vi.useFakeTimers()
    try {
      const { rerender, getByLabelText } = render(<DialogWithInput onClose={() => {}} />)
      act(() => {
        vi.runOnlyPendingTimers()
      })
      const input = getByLabelText('Name') as HTMLInputElement
      input.focus()
      expect(document.activeElement).toBe(input)
      const blur = vi.fn()
      input.addEventListener('blur', blur)
      // Every SSE tick re-renders the page, and call sites pass inline closures.
      for (let i = 0; i < 3; i++) {
        rerender(<DialogWithInput onClose={() => {}} />)
        act(() => {
          vi.runOnlyPendingTimers()
        })
      }
      expect(document.activeElement).toBe(input)
      expect(blur).not.toHaveBeenCalled()
      expect(document.body.style.overflow).toBe('hidden')
    } finally {
      vi.useRealTimers()
    }
  })

  it('calls the latest onClose on Escape', () => {
    const first = vi.fn()
    const second = vi.fn()
    const { rerender } = render(<DialogWithInput onClose={first} />)
    rerender(<DialogWithInput onClose={second} />)
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledTimes(1)
  })

  it('cycles Tab inside the panel', () => {
    // jsdom has no layout: make the trap treat every node as visible.
    const desc = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'offsetParent')
    Object.defineProperty(HTMLElement.prototype, 'offsetParent', { configurable: true, get: () => document.body })
    try {
      const { getByLabelText } = render(
        <Dialog open onClose={() => {}} title="Two fields">
          <input aria-label="A" />
          <input aria-label="B" />
        </Dialog>,
      )
      const close = getByLabelText('Close dialog')
      const b = getByLabelText('B')
      const tab = (shiftKey: boolean) =>
        act(() => {
          document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey, bubbles: true, cancelable: true }))
        })
      // DOM order inside the panel: close button, A, B.
      b.focus()
      tab(false)
      expect(document.activeElement).toBe(close)
      tab(true)
      expect(document.activeElement).toBe(b)
    } finally {
      if (desc) Object.defineProperty(HTMLElement.prototype, 'offsetParent', desc)
    }
  })
})
