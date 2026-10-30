import { useCallback, useEffect, useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { createAlertsApi, type AlertsApi, type AlertSummary } from '../api/alerts'
import { usePermissions } from '../auth/permissions'
import { PageHeader } from '../components/ui'
import { Link, useNavigate, useRoute } from '../router'
import { HistoryTab } from './alerts/HistoryTab'
import { Inbox } from './alerts/Inbox'
import type { InboxCounts } from './alerts/inboxModel'
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

/** alertsTabPath returns the canonical address of an alerts tab. */
export function alertsTabPath(tab: Tab): string {
  return tab === 'active' ? '/alerts' : `/alerts/${tab}`
}

function tabFromPath(path: string): Tab | null {
  if (path === '/alerts') return 'active'
  if (path === '/alerts/history') return 'history'
  if (path === '/alerts/silences') return 'silences'
  return null
}

interface AlertsProps {
  api?: AlertsApi
  onRefresh?: () => void
  onOpenServer?: (serverId: string, serverName: string) => void
}

/** Alerts is the inbox, history and silence screen. */
export function Alerts({
  api = defaultApi,
  onRefresh = () => undefined,
  onOpenServer,
}: AlertsProps) {
  const { t } = useTranslation('alerts')
  const { canAnywhere } = usePermissions()
  const { path } = useRoute()
  const navigate = useNavigate()
  const [counts, setCounts] = useState<InboxCounts | null>(null)
  const canView = canAnywhere('alerts.view' as never)
  const tab = tabFromPath(path)
  const activeTab: Tab = tab ?? 'active'

  useEffect(() => {
    if (tab === null) navigate('/alerts', { replace: true })
  }, [navigate, tab])

  const onCounts = useCallback((next: InboxCounts) => setCounts(next), [])

  if (!canView) return null

  const moveTab = (event: KeyboardEvent<HTMLAnchorElement>, index: number) => {
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
    const target = tabs[next]!
    navigate(alertsTabPath(target))
    document.getElementById(`alerts-tab-${target}`)?.focus()
  }

  return (
    <section className="alerts" aria-label={t('title')}>
      <PageHeader
        title={t('title')}
        subtitle={
          counts === null
            ? undefined
            : t('subtitle', {
                critical: counts.critical,
                warning: counts.warning,
                new: counts.new,
              })
        }
      />

      <div className="alerts__tabs" role="tablist" aria-label={t('tabs.label')}>
        {tabs.map((name, index) => (
          <Link
            key={name}
            id={`alerts-tab-${name}`}
            to={alertsTabPath(name)}
            role="tab"
            aria-selected={activeTab === name}
            aria-controls={`alerts-panel-${name}`}
            tabIndex={activeTab === name ? 0 : -1}
            className={activeTab === name ? 'alerts__tab alerts__tab--on' : 'alerts__tab'}
            onKeyDown={(event) => moveTab(event, index)}
          >
            {t(`tabs.${name}`)}
          </Link>
        ))}
      </div>

      <div
        className="alerts__panel"
        id={`alerts-panel-${activeTab}`}
        role="tabpanel"
        aria-labelledby={`alerts-tab-${activeTab}`}
      >
        {activeTab === 'active' ? (
          <Inbox api={api} onRefresh={onRefresh} onOpenServer={onOpenServer} onCounts={onCounts} />
        ) : activeTab === 'history' ? (
          <HistoryTab api={api} />
        ) : (
          <SilencesTab api={api} onRefresh={onRefresh} />
        )}
      </div>
    </section>
  )
}
