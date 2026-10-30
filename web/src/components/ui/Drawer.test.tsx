import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import { Drawer } from './Drawer'

beforeEach(async () => {
  await i18n.changeLanguage('en')
})
afterEach(cleanup)

function Harness() {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Open drawer
      </button>
      <Drawer
        open={open}
        title="Details"
        onClose={() => setOpen(false)}
        footer={<button type="button">Save</button>}
      >
        <button type="button">Inside</button>
      </Drawer>
    </>
  )
}

describe('Drawer', () => {
  it('opens as a modal, traps focus, closes on Escape and restores focus', () => {
    render(<Harness />)
    const opener = screen.getByRole('button', { name: 'Open drawer' })
    opener.focus()
    fireEvent.click(opener)

    const dialog = screen.getByRole('dialog', { name: 'Details' })
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(document.body.style.overflow).toBe('hidden')
    expect(dialog.contains(document.activeElement)).toBe(true)

    const buttons = within(dialog).getAllByRole('button')
    const first = buttons[0]!
    const last = buttons[buttons.length - 1]!
    last.focus()
    fireEvent.keyDown(document, { key: 'Tab' })
    expect(document.activeElement).toBe(first)

    first.focus()
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(last)

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(document.activeElement).toBe(opener)
    expect(document.body.style.overflow).toBe('')
  })
})
