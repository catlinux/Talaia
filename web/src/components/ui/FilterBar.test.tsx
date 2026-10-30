import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { BulkBar, FilterBar, Select } from './FilterBar'

beforeEach(async () => {
  await i18n.changeLanguage('en')
})
afterEach(cleanup)

describe('FilterBar', () => {
  it('focuses the search field when / is pressed outside an input', () => {
    render(
      <FilterBar search={{ value: '', onChange: () => undefined, placeholder: 'Search servers' }}>
        <span>All</span>
      </FilterBar>,
    )
    const input = screen.getByRole('searchbox', { name: 'Search' })
    fireEvent.keyDown(document.body, { key: '/' })
    expect(document.activeElement).toBe(input)
  })

  it('does not steal / while typing in another field', () => {
    render(
      <FilterBar search={{ value: '', onChange: () => undefined }}>
        <input aria-label="Other field" />
      </FilterBar>,
    )
    const input = screen.getByRole('searchbox', { name: 'Search' })
    const other = screen.getByRole('textbox', { name: 'Other field' })
    other.focus()
    fireEvent.keyDown(other, { key: '/' })
    expect(document.activeElement).toBe(other)
    expect(document.activeElement).not.toBe(input)
  })

  it('renders a labelled select and reports changes', () => {
    const onChange = vi.fn()
    render(
      <Select
        label="Severity"
        value="warning"
        options={[
          { value: 'warning', label: 'Warning' },
          { value: 'critical', label: 'Critical' },
        ]}
        onChange={onChange}
      />,
    )
    const select = screen.getByRole('combobox', { name: 'Severity' })
    fireEvent.change(select, { target: { value: 'critical' } })
    expect(onChange).toHaveBeenCalledWith('critical')
  })

  it('shows the bulk bar only with a selection and clears it', () => {
    const onClear = vi.fn()
    const { rerender } = render(<BulkBar count={0} onClear={onClear} />)
    expect(screen.queryByRole('status')).toBeNull()

    rerender(
      <BulkBar count={2} onClear={onClear}>
        <button type="button">Acknowledge</button>
      </BulkBar>,
    )
    expect(screen.getByText('2 selected')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Acknowledge' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Clear selection' }))
    expect(onClear).toHaveBeenCalledTimes(1)
  })
})
