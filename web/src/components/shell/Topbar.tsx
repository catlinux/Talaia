import { useTranslation } from 'react-i18next'

import type { AlertSummary } from '../../api/alerts'
import { usePermissions } from '../../auth/permissions'
import { alertCount } from '../../screens/Alerts'
import { AlertIcon } from '../icons'
import { Link } from '../../router'
import { Breadcrumbs } from './Breadcrumbs'
import { UserMenu } from './UserMenu'

interface TopbarProps {
  alertSummary: AlertSummary | null
  serverName?: string
  onOpenMenu: () => void
}

/** Topbar holds the drawer button, breadcrumbs, alert bell and user menu. */
export function Topbar({ alertSummary, serverName, onOpenMenu }: TopbarProps) {
  const { t } = useTranslation('common')
  const { canAnywhere } = usePermissions()
  const canAlerts = canAnywhere('alerts.view')
  const count = alertSummary === null ? 0 : alertCount(alertSummary)
  const critical = (alertSummary?.counts.critical ?? 0) > 0

  return (
    <header className="shell__topbar">
      <button
        type="button"
        className="shell__menu-button"
        aria-label={t('shell.openMenu')}
        onClick={onOpenMenu}
      >
        <span aria-hidden="true">☰</span>
      </button>
      <Breadcrumbs serverName={serverName} />
      <div className="shell__topbar-spacer" />
      {canAlerts ? (
        <Link
          to="/alerts"
          className="shell__bell"
          aria-label={count > 0 ? t('shell.alertsCount', { count }) : t('nav.alerts')}
        >
          <AlertIcon />
          {count > 0 ? (
            <span
              className={critical ? 'shell__bell-dot shell__bell-dot--crit' : 'shell__bell-dot'}
              aria-hidden="true"
            >
              {count}
            </span>
          ) : null}
        </Link>
      ) : null}
      <UserMenu />
    </header>
  )
}
