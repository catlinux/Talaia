import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ConfirmDialog } from './ConfirmDialog'

beforeEach(async () => {
  await i18n.changeLanguage('en')
})
afterEach(cleanup)

function Harness({ requireText }: { requireText?: string }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Open confirm
      </button>
      <ConfirmDialog
        open={open}
        title="Delete app"
        body="This cannot be undone."
        confirmLabel="Delete"
        danger
        requireText={requireText}
        onConfirm={() => setOpen(false)}
        onCancel={() => setOpen(false)}
      />
    </>
  )
}

describe('ConfirmDialog', () => {
  it('gates the dangerous action on the required text', () => {
    const onConfirm = vi.fn()
    const onCancel = vi.fn()
    render(
      <ConfirmDialog
        open
        title="Delete app"
        body="This cannot be undone."
        confirmLabel="Delete"
        danger
        requireText="shop"
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    )

    expect(screen.getByRole('dialog', { name: 'Delete app' })).toBeTruthy()
    const confirm = screen.getByRole('button', { name: 'Delete' }) as HTMLButtonElement
    expect(confirm.disabled).toBe(true)
    expect(confirm.classList.contains('ui-btn--danger')).toBe(true)

    fireEvent.change(screen.getByLabelText('Type shop to confirm'), { target: { value: 'shop' } })
    expect(confirm.disabled).toBe(false)
    fireEvent.click(confirm)
    expect(onConfirm).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('restores focus to the opener after Escape', () => {
    render(<Harness />)
    const opener = screen.getByRole('button', { name: 'Open confirm' })
    opener.focus()
    fireEvent.click(opener)
    expect(screen.getByRole('dialog', { name: 'Delete app' })).toBeTruthy()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(document.activeElement).toBe(opener)
  })
})
