import { useTranslation } from 'react-i18next'

import type { Alert } from '../../api/alerts'
import { BulkBar } from '../../components/ui'
import { eligibility, type AlertPermissionCheck, type BulkAction } from './inboxModel'

interface BulkActionsProps {
  selected: Alert[]
  can: AlertPermissionCheck
  disabled?: boolean
  onClear: () => void
  onAcknowledge: (alerts: Alert[]) => void
  onSilence: (alerts: Alert[]) => void
  onUnwatch: (alerts: Alert[]) => void
}

/** BulkActions is the bulk bar of the alerts inbox. */
export function BulkActions({
  selected,
  can,
  disabled = false,
  onClear,
  onAcknowledge,
  onSilence,
  onUnwatch,
}: BulkActionsProps) {
  const { t } = useTranslation('alerts')
  const available = eligibility(selected, can)
  const notes = [
    available.silence.withoutPermission.length > 0
      ? t('bulk.withoutPermission', { count: available.silence.withoutPermission.length })
      : null,
    available.acknowledge.notApplicable.length > 0
      ? t('bulk.alreadyAcknowledged', { count: available.acknowledge.notApplicable.length })
      : null,
    available.unwatch.notApplicable.length > 0
      ? t('bulk.notApplicable', { count: available.unwatch.notApplicable.length })
      : null,
  ].filter((note): note is string => note !== null)
  const actions: Array<{
    id: BulkAction
    label: string
    run: (alerts: Alert[]) => void
  }> = [
    {
      id: 'acknowledge',
      label: t('actions.acknowledge'),
      run: onAcknowledge,
    },
    {
      id: 'silence',
      label: t('actions.silence'),
      run: onSilence,
    },
    {
      id: 'unwatch',
      label: t('actions.unwatch'),
      run: onUnwatch,
    },
  ]

  return (
    <BulkBar count={selected.length} onClear={onClear}>
      {actions.map((action) => {
        const state = available[action.id]
        const blocked = state.allowed.length === 0
        return (
          <button
            key={action.id}
            type="button"
            className={action.id === 'unwatch' ? 'ui-btn ui-btn--danger' : 'ui-btn'}
            disabled={disabled || blocked}
            aria-label={action.label}
            onClick={() => action.run(state.allowed)}
          >
            {action.label}
          </button>
        )
      })}
      {notes.length > 0 ? (
        <span className="alerts-inbox__bulk-notes">
          {notes.map((note) => (
            <span key={note}>{note}</span>
          ))}
        </span>
      ) : null}
    </BulkBar>
  )
}
