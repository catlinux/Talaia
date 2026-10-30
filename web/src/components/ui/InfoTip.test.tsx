import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import { InfoTip } from './InfoTip'

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

afterEach(cleanup)

describe('InfoTip', () => {
  it('shows a tooltip on hover and links it with aria-describedby', () => {
    render(
      <InfoTip>
        <p>Useful details</p>
      </InfoTip>,
    )

    const button = screen.getByRole('button', { name: 'More information' })
    fireEvent.mouseEnter(button)

    const tooltip = screen.getByRole('tooltip')
    expect(screen.getByText('Useful details')).toBeTruthy()
    expect(button.getAttribute('aria-describedby')).toBe(tooltip.id)
    expect(button.getAttribute('aria-expanded')).toBe('true')
  })

  it('stays open after a click and closes on the next click', () => {
    render(
      <InfoTip>
        <p>Useful details</p>
      </InfoTip>,
    )

    const button = screen.getByRole('button', { name: 'More information' })
    const wrapper = button.parentElement!
    fireEvent.mouseEnter(button)
    fireEvent.mouseLeave(wrapper)
    expect(screen.queryByRole('tooltip')).toBeNull()

    fireEvent.click(button)
    fireEvent.mouseLeave(wrapper)
    expect(screen.getByRole('tooltip')).toBeTruthy()

    fireEvent.click(button)
    expect(screen.queryByRole('tooltip')).toBeNull()
  })

  it('opens on focus and closes with Escape', () => {
    render(
      <InfoTip>
        <p>Useful details</p>
      </InfoTip>,
    )

    const button = screen.getByRole('button', { name: 'More information' })
    fireEvent.focus(button)
    expect(screen.getByRole('tooltip')).toBeTruthy()

    button.focus()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('tooltip')).toBeNull()
    expect(document.activeElement).toBe(button)
  })

  it('closes when the user clicks outside', () => {
    render(
      <InfoTip>
        <p>Useful details</p>
      </InfoTip>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'More information' }))
    expect(screen.getByRole('tooltip')).toBeTruthy()

    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('tooltip')).toBeNull()
  })
})
