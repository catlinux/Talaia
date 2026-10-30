import { useTranslation } from 'react-i18next'

import type { AlertSummary, AlertsApi } from '../../api/alerts'
import { AlertBell } from './AlertBell'
import { Breadcrumbs } from './Breadcrumbs'
import { UserMenu } from './UserMenu'

interface TopbarProps {
  alertSummary: AlertSummary | null
  alertsApi?: AlertsApi
  serverName?: string
  onOpenMenu: () => void
}

/** Topbar holds the drawer button, breadcrumbs, alert bell and user menu. */
export function Topbar({ alertSummary, alertsApi, serverName, onOpenMenu }: TopbarProps) {
  const { t } = useTranslation('common')

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
      <AlertBell summary={alertSummary} api={alertsApi} />
      <UserMenu />
    </header>
  )
}
