import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '../../i18n'
import { RangeSelector } from './RangeSelector'

afterEach(cleanup)
beforeEach(async () => {
  await i18n.changeLanguage('en')
})
describe('RangeSelector', () => {
  it('changes range with a button', () => {
    const onChange = vi.fn()
    render(<RangeSelector value="1h" onChange={onChange} />)
    fireEvent.click(screen.getByRole('radio', { name: '24 h' }))
    expect(onChange).toHaveBeenCalledWith('24h')
  })
})
