import {
  Fragment,
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'

export type SortDirection = 'asc' | 'desc'

export interface DataTableSort {
  id: string
  dir: SortDirection
}

export interface DataTableColumn<T> {
  id: string
  header: string
  cell: (row: T) => ReactNode
  sortValue?: (row: T) => string | number
  align?: 'left' | 'right' | 'center'
  width?: string | number
  hideOnMobile?: boolean
}

export interface DataTableGroup<T> {
  key: (row: T) => string
  label: (key: string, rows: T[]) => ReactNode
}

export interface DataTableProps<T> {
  columns: DataTableColumn<T>[]
  rows: T[]
  rowKey: (row: T) => string
  loading?: boolean
  error?: string
  onRetry?: () => void
  empty?: ReactNode
  sort?: DataTableSort
  onSortChange?: (sort: DataTableSort) => void
  selectable?: boolean
  selected?: Set<string>
  onSelectionChange?: (selected: Set<string>) => void
  onRowClick?: (row: T) => void
  rowClassName?: (row: T) => string | undefined
  groupBy?: DataTableGroup<T>
}

const emptySelection = new Set<string>()

function widthStyle(width: string | number | undefined): CSSProperties | undefined {
  if (width === undefined) return undefined
  return { width: typeof width === 'number' ? `${width}px` : width }
}

function compareValues(a: string | number, b: string | number): number {
  if (typeof a === 'number' && typeof b === 'number') return a - b
  return String(a).localeCompare(String(b))
}

/** DataTable is the shared sortable, selectable table of the panel. */
export function DataTable<T>({
  columns,
  rows,
  rowKey,
  loading = false,
  error,
  onRetry,
  empty,
  sort,
  onSortChange,
  selectable = false,
  selected = emptySelection,
  onSelectionChange,
  onRowClick,
  rowClassName,
  groupBy,
}: DataTableProps<T>) {
  const { t } = useTranslation('ui')
  const [internalSort, setInternalSort] = useState<DataTableSort>()
  const activeSort = sort ?? internalSort
  const headerCheckbox = useRef<HTMLInputElement>(null)
  const visibleIDs = rows.map(rowKey)
  const allSelected = visibleIDs.length > 0 && visibleIDs.every((id) => selected.has(id))
  const someSelected = visibleIDs.some((id) => selected.has(id))

  useEffect(() => {
    if (headerCheckbox.current !== null) {
      headerCheckbox.current.indeterminate = someSelected && !allSelected
    }
  }, [allSelected, someSelected])

  let sortedRows = rows
  if (activeSort !== undefined) {
    const column = columns.find((entry) => entry.id === activeSort.id)
    if (column?.sortValue !== undefined) {
      const direction = activeSort.dir === 'asc' ? 1 : -1
      sortedRows = [...rows].sort(
        (a, b) => compareValues(column.sortValue!(a), column.sortValue!(b)) * direction,
      )
    }
  }

  const toggleSort = (column: DataTableColumn<T>) => {
    const next: DataTableSort = {
      id: column.id,
      dir: activeSort?.id === column.id && activeSort.dir === 'asc' ? 'desc' : 'asc',
    }
    onSortChange?.(next)
    if (sort === undefined) setInternalSort(next)
  }

  const toggleAll = () => {
    const next = new Set(selected)
    if (allSelected) {
      for (const id of visibleIDs) next.delete(id)
    } else {
      for (const id of visibleIDs) next.add(id)
    }
    onSelectionChange?.(next)
  }

  const toggleRow = (id: string) => {
    const next = new Set(selected)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    onSelectionChange?.(next)
  }

  const rowClick = (event: MouseEvent<HTMLTableRowElement>, row: T) => {
    if (onRowClick === undefined) return
    const target = event.target
    if (
      target instanceof Element &&
      target.closest(
        'button, a, input, select, textarea, [role="button"], [role="menuitem"], [role="checkbox"]',
      ) !== null
    ) {
      return
    }
    onRowClick(row)
  }

  if (error !== undefined) {
    return (
      <div className="ui-table__state ui-table__state--error" role="alert">
        <p className="ui-table__message">{error}</p>
        {onRetry !== undefined ? (
          <button type="button" className="ui-btn" onClick={onRetry}>
            {t('retry')}
          </button>
        ) : null}
      </div>
    )
  }

  if (!loading && rows.length === 0) {
    return <div className="ui-table__state">{empty ?? t('empty')}</div>
  }

  const renderRow = (row: T) => {
    const id = rowKey(row)
    const className = [
      'ui-table__row',
      onRowClick !== undefined ? 'ui-table__row--clickable' : '',
      selected.has(id) ? 'ui-table__row--selected' : '',
      rowClassName?.(row) ?? '',
    ]
      .filter(Boolean)
      .join(' ')
    return (
      <tr key={id} className={className} onClick={(event) => rowClick(event, row)}>
        {selectable ? (
          <td className="ui-table__check" data-label={t('selectRow')}>
            <input
              type="checkbox"
              checked={selected.has(id)}
              aria-label={t('selectRow')}
              onClick={(event) => event.stopPropagation()}
              onChange={() => toggleRow(id)}
            />
          </td>
        ) : null}
        {columns.map((column) => (
          <td
            key={column.id}
            data-label={column.header}
            className={[
              column.align === undefined ? '' : `ui-table__align-${column.align}`,
              column.hideOnMobile === true ? 'ui-table__hide-mobile' : '',
            ]
              .filter(Boolean)
              .join(' ')}
          >
            {column.cell(row)}
          </td>
        ))}
      </tr>
    )
  }

  let body: ReactNode
  if (loading) {
    body = Array.from({ length: 5 }, (_, index) => (
      <tr key={`skeleton-${index}`} className="ui-table__row ui-table__skeleton-row">
        {selectable ? <td className="ui-table__check" /> : null}
        {columns.map((column) => (
          <td
            key={column.id}
            data-label={column.header}
            className={column.hideOnMobile === true ? 'ui-table__hide-mobile' : undefined}
          >
            <span className="ui-table__skeleton" />
          </td>
        ))}
      </tr>
    ))
  } else if (groupBy !== undefined) {
    const groups = new Map<string, T[]>()
    for (const row of sortedRows) {
      const key = groupBy.key(row)
      const group = groups.get(key)
      if (group === undefined) groups.set(key, [row])
      else group.push(row)
    }
    body = [...groups.entries()].map(([key, group]) => (
      <Fragment key={`group-${key}`}>
        <tr className="ui-table__group">
          <td colSpan={columns.length + (selectable ? 1 : 0)}>{groupBy.label(key, group)}</td>
        </tr>
        {group.map(renderRow)}
      </Fragment>
    ))
  } else {
    body = sortedRows.map(renderRow)
  }

  return (
    <div className="ui-table-wrap" aria-busy={loading || undefined}>
      {loading ? <span className="ui-sr-only">{t('loading')}</span> : null}
      <table className="ui-table">
        <thead>
          <tr>
            {selectable ? (
              <th className="ui-table__check" scope="col">
                <input
                  ref={headerCheckbox}
                  type="checkbox"
                  checked={allSelected}
                  aria-label={t('selectAll')}
                  onChange={toggleAll}
                />
              </th>
            ) : null}
            {columns.map((column) => {
              const current = activeSort?.id === column.id ? activeSort : undefined
              const sortable = column.sortValue !== undefined
              return (
                <th
                  key={column.id}
                  scope="col"
                  style={widthStyle(column.width)}
                  aria-sort={
                    sortable
                      ? current !== undefined
                        ? current.dir === 'asc'
                          ? 'ascending'
                          : 'descending'
                        : 'none'
                      : undefined
                  }
                  className={[
                    column.align === undefined ? '' : `ui-table__align-${column.align}`,
                    column.hideOnMobile === true ? 'ui-table__hide-mobile' : '',
                  ]
                    .filter(Boolean)
                    .join(' ')}
                >
                  {sortable ? (
                    <button
                      type="button"
                      className="ui-table__sort"
                      title={t(current?.dir === 'asc' ? 'sortDescending' : 'sortAscending')}
                      onClick={() => toggleSort(column)}
                    >
                      {column.header}
                      {current !== undefined ? (
                        <span className="ui-table__sort-mark" aria-hidden="true">
                          {current.dir === 'asc' ? '↑' : '↓'}
                        </span>
                      ) : null}
                    </button>
                  ) : (
                    column.header
                  )}
                </th>
              )
            })}
          </tr>
        </thead>
        <tbody>{body}</tbody>
      </table>
    </div>
  )
}
