import { useTranslation } from 'react-i18next'

export type Status = 'ok' | 'warn' | 'crit' | 'offline' | 'unknown'
export type Severity = 'critical' | 'warning' | 'info'

interface StatusBadgeProps {
  status: Status
  label?: string
}

const statusLabelKeys: Record<Status, string> = {
  ok: 'status.ok',
  warn: 'status.warn',
  crit: 'status.crit',
  offline: 'status.offline',
  unknown: 'status.unknown',
}

/** StatusBadge always shows a coloured dot and its text label. */
export function StatusBadge({ status, label }: StatusBadgeProps) {
  const { t } = useTranslation('ui')
  return (
    <span className={`ui-status ui-status--${status}`}>
      <span className="ui-status__dot" aria-hidden="true" />
      <span>{label ?? t(statusLabelKeys[status])}</span>
    </span>
  )
}

interface SeverityTagProps {
  severity: Severity
}

/** SeverityTag is the compact severity pill of the alert tables. */
export function SeverityTag({ severity }: SeverityTagProps) {
  const { t } = useTranslation('ui')
  return <span className={`ui-severity ui-severity--${severity}`}>{t(`severity.${severity}`)}</span>
}
