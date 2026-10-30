import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ToastProvider, useToast } from './Toast'

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.useFakeTimers()
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

function Pusher() {
  const toast = useToast()
  return (
    <>
      <button type="button" onClick={() => toast.success('Saved')}>
        Save
      </button>
      <button type="button" onClick={() => toast.error('Failed')}>
        Fail
      </button>
    </>
  )
}

function renderToasts() {
  render(
    <ToastProvider>
      <Pusher />
    </ToastProvider>,
  )
}

describe('Toast', () => {
  it('auto-dismisses success after five seconds and errors after eight', () => {
    renderToasts()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    fireEvent.click(screen.getByRole('button', { name: 'Fail' }))
    expect(screen.getByText('Saved')).toBeTruthy()
    expect(screen.getByRole('alert')).toBeTruthy()
    expect(screen.getByText('Saved').closest('.ui-toast-region')?.getAttribute('aria-live')).toBe(
      'polite',
    )

    act(() => {
      vi.advanceTimersByTime(5000)
    })
    expect(screen.queryByText('Saved')).toBeNull()
    expect(screen.getByText('Failed')).toBeTruthy()

    act(() => {
      vi.advanceTimersByTime(3000)
    })
    expect(screen.queryByText('Failed')).toBeNull()
  })

  it('closes a toast with its button', () => {
    renderToasts()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    const toast = screen.getByText('Saved').closest<HTMLElement>('.ui-toast')
    if (toast === null) throw new Error('toast not found')
    fireEvent.click(within(toast).getByRole('button', { name: 'Close' }))
    expect(screen.queryByText('Saved')).toBeNull()
  })
})
