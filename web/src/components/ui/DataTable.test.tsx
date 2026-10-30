import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { DataTable, type DataTableColumn, type DataTableProps } from './DataTable'

interface Row {
  id: string
  name: string
  count: number
  group: string
}

const rows: Row[] = [
  { id: 'b', name: 'Bravo', count: 10, group: 'web' },
  { id: 'a', name: 'Alpha', count: 2, group: 'web' },
  { id: 'c', name: 'Charlie', count: 1, group: 'db' },
]

const columns: DataTableColumn<Row>[] = [
  { id: 'name', header: 'Name', cell: (row) => row.name, sortValue: (row) => row.name },
  {
    id: 'count',
    header: 'Count',
    cell: (row) => String(row.count),
    sortValue: (row) => row.count,
    align: 'right',
  },
]

function renderTable(overrides: Partial<DataTableProps<Row>> = {}) {
  return render(<DataTable columns={columns} rows={rows} rowKey={(row) => row.id} {...overrides} />)
}

function bodyRows(): HTMLElement[] {
  const body = screen.getByRole('table').querySelector('tbody')
  if (body === null) throw new Error('table body not found')
  return within(body).getAllByRole('row')
}

function firstCells(): string[] {
  return bodyRows().map((row) => within(row).getAllByRole('cell')[0]?.textContent ?? '')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})
afterEach(cleanup)

describe('DataTable', () => {
  it('sorts internally and toggles aria-sort', () => {
    renderTable()
    const header = screen.getByRole('columnheader', { name: 'Name' })
    expect(header.getAttribute('aria-sort')).toBe('none')

    fireEvent.click(within(header).getByRole('button', { name: 'Name' }))
    expect(header.getAttribute('aria-sort')).toBe('ascending')
    expect(firstCells()).toEqual(['Alpha', 'Bravo', 'Charlie'])

    fireEvent.click(within(header).getByRole('button', { name: 'Name' }))
    expect(header.getAttribute('aria-sort')).toBe('descending')
    expect(firstCells()).toEqual(['Charlie', 'Bravo', 'Alpha'])
  })

  it('sorts numeric sortValue values numerically', () => {
    renderTable()
    fireEvent.click(screen.getByRole('button', { name: 'Count' }))
    expect(firstCells()).toEqual(['Charlie', 'Alpha', 'Bravo'])
  })

  it('supports a controlled sort callback', () => {
    const onSortChange = vi.fn()
    renderTable({ sort: { id: 'name', dir: 'asc' }, onSortChange })
    const header = screen.getByRole('columnheader', { name: 'Name' })
    expect(header.getAttribute('aria-sort')).toBe('ascending')
    fireEvent.click(within(header).getByRole('button', { name: 'Name' }))
    expect(onSortChange).toHaveBeenCalledWith({ id: 'name', dir: 'desc' })
    expect(header.getAttribute('aria-sort')).toBe('ascending')
  })

  it('selects every visible row from the header checkbox', () => {
    const onSelectionChange = vi.fn()
    const selected = new Set(['a'])
    const { rerender } = renderTable({ selectable: true, selected, onSelectionChange })
    const checkboxes = screen.getAllByRole('checkbox')
    const headerCheckbox = screen.getByRole('checkbox', { name: 'Select all' }) as HTMLInputElement
    expect(checkboxes).toHaveLength(rows.length + 1)
    expect(headerCheckbox.checked).toBe(false)
    expect(headerCheckbox.indeterminate).toBe(true)

    fireEvent.click(headerCheckbox)
    const all = onSelectionChange.mock.calls[0]?.[0] as Set<string>
    expect([...all].sort()).toEqual(['a', 'b', 'c'])

    rerender(
      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(row) => row.id}
        selectable
        selected={all}
        onSelectionChange={onSelectionChange}
      />,
    )
    expect((screen.getByRole('checkbox', { name: 'Select all' }) as HTMLInputElement).checked).toBe(
      true,
    )
    expect(
      (screen.getByRole('checkbox', { name: 'Select all' }) as HTMLInputElement).indeterminate,
    ).toBe(false)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
    const cleared = onSelectionChange.mock.calls[1]?.[0] as Set<string>
    expect(cleared.size).toBe(0)
  })

  it('does not fire a row click from a checkbox or a button in the row', () => {
    const onRowClick = vi.fn()
    const onSelectionChange = vi.fn()
    const columnsWithAction: DataTableColumn<Row>[] = [
      ...columns,
      { id: 'action', header: 'Actions', cell: () => <button type="button">Open</button> },
    ]
    render(
      <DataTable
        columns={columnsWithAction}
        rows={rows}
        rowKey={(row) => row.id}
        selectable
        selected={new Set()}
        onSelectionChange={onSelectionChange}
        onRowClick={onRowClick}
      />,
    )

    const rowCheckboxes = screen.getAllByRole('checkbox').slice(1)
    fireEvent.click(rowCheckboxes[0]!)
    expect(onRowClick).not.toHaveBeenCalled()
    expect(onSelectionChange).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getAllByRole('button', { name: 'Open' })[0]!)
    expect(onRowClick).not.toHaveBeenCalled()

    fireEvent.click(within(bodyRows()[0]!).getAllByRole('cell')[1]!)
    expect(onRowClick).toHaveBeenCalledTimes(1)
  })

  it('shows five skeleton rows while loading', () => {
    const { container } = renderTable({ loading: true })
    expect(container.querySelectorAll('.ui-table__skeleton-row')).toHaveLength(5)
    expect(screen.getByText('Loading')).toBeTruthy()
  })

  it('shows the error and calls retry', () => {
    const onRetry = vi.fn()
    renderTable({ error: 'Could not load', onRetry })
    expect(screen.getByRole('alert')).toBeTruthy()
    expect(screen.getByText('Could not load')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('shows the empty node when there are no rows', () => {
    renderTable({ rows: [], empty: <p>Nothing to show</p> })
    expect(screen.getByText('Nothing to show')).toBeTruthy()
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('marks cells with their header label and hides mobile-only columns', () => {
    const mobileColumns: DataTableColumn<Row>[] = [
      columns[0]!,
      { ...columns[1]!, hideOnMobile: true },
    ]
    renderTable({ columns: mobileColumns })
    const header = screen.getByRole('columnheader', { name: 'Count' })
    expect(header.classList.contains('ui-table__hide-mobile')).toBe(true)
    const cells = within(bodyRows()[0]!).getAllByRole('cell')
    expect(cells[0]?.getAttribute('data-label')).toBe('Name')
    expect(cells[1]?.getAttribute('data-label')).toBe('Count')
    expect(cells[1]?.classList.contains('ui-table__hide-mobile')).toBe(true)
  })

  it('renders group labels before their rows', () => {
    renderTable({
      groupBy: {
        key: (row) => row.group,
        label: (key, groupRows) => `${key} · ${groupRows.length}`,
      },
    })
    expect(screen.getByText('web · 2')).toBeTruthy()
    expect(screen.getByText('db · 1')).toBeTruthy()
    expect(bodyRows()).toHaveLength(5)
  })
})
