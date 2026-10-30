import { useEffect, useId, useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

export interface FilterSearch {
  value: string
  onChange: (value: string) => void
  placeholder?: string
}

interface FilterBarProps {
  search?: FilterSearch
  children?: ReactNode
  right?: ReactNode
}

/** FilterBar holds the search field and the filter chips of a table screen. */
export function FilterBar({ search, children, right }: FilterBarProps) {
  const { t } = useTranslation('ui')
  const input = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (search === undefined) return
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key !== '/' || event.ctrlKey || event.metaKey || event.altKey) return
      const target = event.target
      if (
        target instanceof HTMLElement &&
        (target.isContentEditable ||
          target.closest('input, textarea, select, [contenteditable="true"]') !== null)
      ) {
        return
      }
      event.preventDefault()
      input.current?.focus()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [search])

  return (
    <div className="ui-filter-bar">
      {search !== undefined ? (
        <div className="ui-filter-bar__search">
          <input
            ref={input}
            type="search"
            className="ui-filter-bar__input"
            value={search.value}
            placeholder={search.placeholder}
            aria-label={t('search')}
            onChange={(event) => search.onChange(event.target.value)}
          />
        </div>
      ) : null}
      {children !== undefined ? <div className="ui-filter-bar__chips">{children}</div> : null}
      {right !== undefined ? <div className="ui-filter-bar__right">{right}</div> : null}
    </div>
  )
}

export interface SelectOption {
  value: string
  label: string
}

interface SelectProps {
  label: string
  value: string
  options: SelectOption[]
  onChange: (value: string) => void
}

/** Select is the small labelled select used by the filter bars and forms. */
export function Select({ label, value, options, onChange }: SelectProps) {
  const id = useId()
  return (
    <label className="ui-field" htmlFor={id}>
      <span className="ui-field__label">{label}</span>
      <select
        id={id}
        className="ui-select"
        value={value}
        onChange={(event) => onChange(event.target.value)}
      >
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  )
}

interface BulkBarProps {
  count: number
  onClear: () => void
  children?: ReactNode
}

/** BulkBar shows the actions available for the current selection. */
export function BulkBar({ count, onClear, children }: BulkBarProps) {
  const { t } = useTranslation('ui')
  if (count <= 0) return null
  return (
    <div className="ui-bulk" role="status">
      <strong>{t('selected', { count })}</strong>
      {children !== undefined ? <div className="ui-bulk__actions">{children}</div> : null}
      <button type="button" className="ui-btn ui-btn--ghost" onClick={onClear}>
        {t('clearSelection')}
      </button>
    </div>
  )
}
