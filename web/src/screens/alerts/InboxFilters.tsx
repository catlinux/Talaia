import { useTranslation } from 'react-i18next'

import { FilterBar, Select, type SelectOption } from '../../components/ui'
import type { InboxFilters } from './inboxModel'

interface InboxFiltersProps {
  filters: InboxFilters
  servers: SelectOption[]
  rules: SelectOption[]
  changed: boolean
  onChange: (key: keyof InboxFilters, value: string) => void
  onClear: () => void
}

/** InboxFilters is the filter bar of the alerts inbox. */
export function InboxFilters({
  filters,
  servers,
  rules,
  changed,
  onChange,
  onClear,
}: InboxFiltersProps) {
  const { t } = useTranslation('alerts')

  return (
    <FilterBar
      search={{
        value: filters.q,
        onChange: (value) => onChange('q', value),
        placeholder: t('filters.search'),
      }}
      right={
        <>
          <Select
            label={t('filters.group')}
            value={filters.group}
            options={[
              { value: 'none', label: t('filters.groupNone') },
              { value: 'server', label: t('filters.groupServer') },
              { value: 'rule', label: t('filters.groupRule') },
            ]}
            onChange={(value) => onChange('group', value)}
          />
          {changed ? (
            <button type="button" className="ui-btn ui-btn--ghost" onClick={onClear}>
              {t('filters.clear')}
            </button>
          ) : null}
        </>
      }
    >
      <Select
        label={t('filters.severity')}
        value={filters.severity}
        options={[
          { value: 'all', label: t('filters.allSeverities') },
          { value: 'critical', label: t('severity.critical') },
          { value: 'warning', label: t('severity.warning') },
          { value: 'info', label: t('severity.info') },
        ]}
        onChange={(value) => onChange('severity', value)}
      />
      <Select
        label={t('filters.state')}
        value={filters.state}
        options={[
          { value: 'all', label: t('filters.allStates') },
          { value: 'new', label: t('state.new') },
          { value: 'seen', label: t('state.seen') },
          { value: 'silenced', label: t('state.silenced') },
        ]}
        onChange={(value) => onChange('state', value)}
      />
      <Select
        label={t('filters.server')}
        value={filters.server}
        options={servers}
        onChange={(value) => onChange('server', value)}
      />
      <Select
        label={t('filters.rule')}
        value={filters.rule}
        options={rules}
        onChange={(value) => onChange('rule', value)}
      />
    </FilterBar>
  )
}
