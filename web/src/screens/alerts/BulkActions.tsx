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
        const denied = state.denied.length
        const note = denied > 0 ? t('bulk.notAllowed', { count: denied }) : undefined
        return (
          <button
            key={action.id}
            type="button"
            className={action.id === 'unwatch' ? 'ui-btn ui-btn--danger' : 'ui-btn'}
            disabled={disabled || blocked}
            aria-label={action.label}
            title={note}
            onClick={() => action.run(state.allowed)}
          >
            {action.label}
            {note !== undefined ? (
              <span className="alerts-inbox__bulk-note" aria-hidden="true">
                {note}
              </span>
            ) : null}
          </button>
        )
      })}
    </BulkBar>
  )
}
