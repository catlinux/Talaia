import { useCallback, useEffect, useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { createAlertsApi, type AlertsApi, type AlertSummary } from '../api/alerts'
import type { AlertsAdminApi } from '../api/alertsAdmin'
import type { ContainersApi } from '../api/containers'
import type { Group, GroupsApi } from '../api/groups'
import type { MetricsApi } from '../api/metrics'
import type { ServerSummary, ServersApi } from '../api/servers'
import { errorMessage } from '../auth/AuthGate'
import { usePermissions } from '../auth/permissions'
import { PageHeader } from '../components/ui'
import { Link, useNavigate, useRoute } from '../router'
import { ChannelsTab } from './alerts/ChannelsTab'
import { ChecksTab } from './alerts/ChecksTab'
import { HistoryTab } from './alerts/HistoryTab'
import { Inbox } from './alerts/Inbox'
import { inboxCounts, type InboxCounts } from './alerts/inboxModel'
import { RulesTab } from './alerts/RulesTab'
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

const baseTabs = ['active', 'history', 'silences'] as const
const adminTabs = ['rules', 'checks', 'channels'] as const
type AlertsTab = (typeof baseTabs)[number] | (typeof adminTabs)[number]

/** alertsTabPath returns the canonical address of an alerts tab. */
export function alertsTabPath(tab: AlertsTab): string {
  return tab === 'active' ? '/alerts' : `/alerts/${tab}`
}

function tabFromPath(path: string): AlertsTab | null {
  if (path === '/alerts') return 'active'
  if (path === '/alerts/history') return 'history'
  if (path === '/alerts/silences') return 'silences'
  if (path === '/alerts/rules') return 'rules'
  if (path === '/alerts/checks') return 'checks'
  if (path === '/alerts/channels') return 'channels'
  return null
}

function isAdminTab(tab: AlertsTab): tab is (typeof adminTabs)[number] {
  return tab === 'rules' || tab === 'checks' || tab === 'channels'
}

interface AlertsProps {
  api?: AlertsApi
  onRefresh?: () => void
  onOpenServer?: (serverId: string, serverName: string) => void
  alertsAdminApi?: AlertsAdminApi
  metricsApi?: MetricsApi
  containersApi?: ContainersApi
  serversApi?: Pick<ServersApi, 'listServers'>
  groupsApi?: GroupsApi
}

/** Alerts is the inbox, history, silences and alert-management screen. */
export function Alerts({
  api = defaultApi,
  onRefresh = () => undefined,
  onOpenServer,
  alertsAdminApi,
  metricsApi,
  containersApi,
  serversApi,
  groupsApi,
}: AlertsProps) {
  const { t } = useTranslation('alerts')
  const { canAnywhere, loading: permissionsLoading } = usePermissions()
  const { path } = useRoute()
  const navigate = useNavigate()
  const [counts, setCounts] = useState<InboxCounts | null>(null)
  const [servers, setServers] = useState<ServerSummary[] | null>(null)
  const [groups, setGroups] = useState<Group[] | null>(null)
  const [adminLoaded, setAdminLoaded] = useState(false)
  const [adminAttempt, setAdminAttempt] = useState(0)
  const [adminError, setAdminError] = useState<string | null>(null)
  const canManage = canAnywhere('alerts.manage' as never)
  const canView = canAnywhere('alerts.view' as never) || canManage
  const tab = tabFromPath(path)
  const activeTab: AlertsTab = tab !== null && (!isAdminTab(tab) || canManage) ? tab : 'active'
  const tabs: AlertsTab[] = canManage ? [...baseTabs, ...adminTabs] : [...baseTabs]
  const adminOpen = isAdminTab(activeTab)

  useEffect(() => {
    if (permissionsLoading) return
    if (tab === null || (isAdminTab(tab) && !canManage)) {
      navigate('/alerts', { replace: true })
    }
  }, [canManage, navigate, permissionsLoading, tab])

  const onCounts = useCallback((next: InboxCounts) => setCounts(next), [])

  useEffect(() => {
    if (!canView || activeTab === 'active' || counts !== null) return
    let active = true
    api.list('active').then(
      (rows) => {
        if (active) setCounts(inboxCounts(rows))
      },
      () => undefined,
    )
    return () => {
      active = false
    }
  }, [activeTab, api, canView, counts])

  useEffect(() => {
    if (!canManage || !adminOpen || adminLoaded) return
    let active = true
    Promise.all([
      serversApi === undefined ? Promise.resolve([]) : serversApi.listServers(),
      groupsApi === undefined ? Promise.resolve([]) : groupsApi.list(),
    ]).then(
      ([nextServers, nextGroups]) => {
        if (!active) return
        setServers(nextServers)
        setGroups(nextGroups)
        setAdminError(null)
        setAdminLoaded(true)
      },
      (cause: unknown) => {
        if (active) setAdminError(errorMessage(t, cause))
      },
    )
    return () => {
      active = false
    }
  }, [adminAttempt, adminLoaded, adminOpen, canManage, groupsApi, serversApi, t])

  const retryAdmin = () => {
    setAdminError(null)
    setAdminAttempt((value) => value + 1)
  }

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
          <Inbox
            api={api}
            onRefresh={onRefresh}
            onOpenServer={onOpenServer}
            onCounts={onCounts}
            alertsAdminApi={alertsAdminApi}
            metricsApi={metricsApi}
            containersApi={containersApi}
          />
        ) : activeTab === 'history' ? (
          <HistoryTab api={api} />
        ) : activeTab === 'silences' ? (
          <SilencesTab api={api} onRefresh={onRefresh} />
        ) : adminError !== null ? (
          <div className="settings__message settings__message--error" role="alert">
            <p>{adminError}</p>
            <button type="button" className="settings__button" onClick={retryAdmin}>
              {t('retry')}
            </button>
          </div>
        ) : servers === null || groups === null ? (
          <p className="settings__message" role="status">
            {t('management.loading')}
          </p>
        ) : activeTab === 'rules' && alertsAdminApi !== undefined ? (
          <RulesTab api={alertsAdminApi} servers={servers} groups={groups} />
        ) : activeTab === 'checks' && alertsAdminApi !== undefined ? (
          <ChecksTab api={alertsAdminApi} servers={servers} />
        ) : activeTab === 'channels' && alertsAdminApi !== undefined ? (
          <ChannelsTab api={alertsAdminApi} />
        ) : null}
      </div>
    </section>
  )
}
