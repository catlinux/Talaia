import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { Menu, type MenuEntry, type MenuTriggerProps } from './Menu'

beforeEach(async () => {
  await i18n.changeLanguage('en')
})
afterEach(cleanup)

function trigger(props: MenuTriggerProps) {
  return (
    <button {...props} aria-label="Row actions">
      ···
    </button>
  )
}

function items(onSelect = vi.fn()): MenuEntry[] {
  return [
    { id: 'edit', label: 'Edit', onSelect },
    { separator: true },
    { id: 'delete', label: 'Delete', danger: true, onSelect },
    { id: 'disabled', label: 'Disabled', disabled: true, onSelect },
  ]
}

describe('Menu', () => {
  it('navigates with the keyboard and selects an item', () => {
    const onSelect = vi.fn()
    render(<Menu trigger={trigger} items={items(onSelect)} />)
    const button = screen.getByRole('button', { name: 'Row actions' })
    expect(button.getAttribute('aria-haspopup')).toBe('menu')
    expect(button.getAttribute('aria-expanded')).toBe('false')

    fireEvent.click(button)
    const menu = screen.getByRole('menu', { name: 'Actions' })
    expect(button.getAttribute('aria-expanded')).toBe('true')
    expect(document.activeElement).toBe(screen.getByRole('menuitem', { name: 'Edit' }))

    fireEvent.keyDown(menu, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(screen.getByRole('menuitem', { name: 'Delete' }))
    fireEvent.keyDown(menu, { key: 'Home' })
    expect(document.activeElement).toBe(screen.getByRole('menuitem', { name: 'Edit' }))
    fireEvent.keyDown(menu, { key: 'End' })
    expect(document.activeElement).toBe(screen.getByRole('menuitem', { name: 'Delete' }))
    fireEvent.keyDown(menu, { key: 'ArrowUp' })
    expect(document.activeElement).toBe(screen.getByRole('menuitem', { name: 'Edit' }))
    fireEvent.keyDown(menu, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(screen.getByRole('menuitem', { name: 'Delete' }))
    fireEvent.keyDown(menu, { key: 'Enter' })

    expect(onSelect).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('menu')).toBeNull()
    expect(document.activeElement).toBe(button)
  })

  it('closes on Escape and on an outside click', () => {
    render(<Menu trigger={trigger} items={items()} />)
    const button = screen.getByRole('button', { name: 'Row actions' })

    fireEvent.click(button)
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
    expect(screen.queryByRole('menu')).toBeNull()
    expect(document.activeElement).toBe(button)

    fireEvent.click(button)
    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('menu')).toBeNull()
    expect(document.activeElement).toBe(button)
  })
})
