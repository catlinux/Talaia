import { useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { createAlertsApi, type AlertsApi, type AlertSummary } from '../api/alerts'
import { usePermissions } from '../auth/permissions'
import { ActiveTab } from './alerts/ActiveTab'
import { HistoryTab } from './alerts/HistoryTab'
import { SilencesTab } from './alerts/SilencesTab'

const defaultApi = createAlertsApi()

/** alertCount is the number shown in the navigation badge. */
export function alertCount(summary: AlertSummary): number {
  return (summary.counts.warning ?? 0) + (summary.counts.critical ?? 0)
}

/** AlertsBadge shows the active alert count, critical in red and warning in amber. */
export function AlertsBadge({ summary }: { summary: AlertSummary | null }) {
  if (summary === null) return null
  const count = alertCount(summary)
  if (count === 0) return null
  return (
    <span
      className={`side__badge ${(summary.counts.critical ?? 0) > 0 ? 'side__badge--critical' : 'side__badge--warning'}`}
    >
      {count}
    </span>
  )
}
const tabs = ['active', 'history', 'silences'] as const
type Tab = (typeof tabs)[number]

interface AlertsProps {
  api?: AlertsApi
  onRefresh?: () => void
  onOpenServer?: (serverId: string, serverName: string) => void
}

/** Alerts is the active, history and silence screen. */
export function Alerts({
  api = defaultApi,
  onRefresh = () => undefined,
  onOpenServer,
}: AlertsProps) {
  const { t } = useTranslation('alerts')
  const { canAnywhere } = usePermissions()
  const [tab, setTab] = useState<Tab>('active')
  const canView = canAnywhere('alerts.view' as never)
  if (!canView) return null

  const moveTab = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    let next: number
    switch (event.key) {
      case 'ArrowLeft':
        next = (index - 1 + tabs.length) % tabs.length
        break
      case 'ArrowRight':
        next = (index + 1) % tabs.length
        break
      case 'Home':
        next = 0
        break
      case 'End':
        next = tabs.length - 1
        break
      default:
        return
    }
    event.preventDefault()
    setTab(tabs[next]!)
    document.getElementById(`alerts-tab-${tabs[next]}`)?.focus()
  }

  return (
    <section className="alerts" aria-labelledby="alerts-title">
      <header className="alerts__head">
        <p className="alerts__eyebrow">{t('eyebrow')}</p>
        <h1 className="alerts__title" id="alerts-title">
          {t('title')}
        </h1>
        <p className="alerts__intro">{t('intro')}</p>
      </header>

      <div className="alerts__tabs" role="tablist" aria-label={t('tabs.label')}>
        {tabs.map((name, index) => (
          <button
            key={name}
            id={`alerts-tab-${name}`}
            type="button"
            role="tab"
            aria-selected={tab === name}
            aria-controls={`alerts-panel-${name}`}
            tabIndex={tab === name ? 0 : -1}
            className={tab === name ? 'alerts__tab alerts__tab--on' : 'alerts__tab'}
            onClick={() => setTab(name)}
            onKeyDown={(event) => moveTab(event, index)}
          >
            {t(`tabs.${name}`)}
          </button>
        ))}
      </div>

      <div
        className="alerts__panel"
        id={`alerts-panel-${tab}`}
        role="tabpanel"
        aria-labelledby={`alerts-tab-${tab}`}
      >
        {tab === 'active' ? (
          <ActiveTab api={api} onRefresh={onRefresh} onOpenServer={onOpenServer} />
        ) : tab === 'history' ? (
          <HistoryTab api={api} />
        ) : (
          <SilencesTab api={api} onRefresh={onRefresh} />
        )}
      </div>
    </section>
  )
}
