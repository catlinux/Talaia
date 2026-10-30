import { useTranslation } from 'react-i18next'

import type { AlertsAdminApi } from '../../api/alertsAdmin'
import type { AptApi } from '../../api/apt'
import type { ContainerList } from '../../api/containers'
import type { JournalApi } from '../../api/journal'
import type { MetricsApi } from '../../api/metrics'
import type { ServicesApi, ServiceUnit } from '../../api/services'
import type { AptSummary } from '../../api/types'
import { AlertsSection } from './AlertsSection'
import { JournalSection } from './JournalSection'
import type { OverviewServer, ServerTab } from './OverviewTab'
import { OverviewTab } from './OverviewTab'
import { ServicesSection } from './ServicesSection'
import { UpdatesSection } from './UpdatesSection'

interface PanelServer extends OverviewServer {
  id: string
  name: string
  groupId?: string
  hostname?: string
}

interface ServerPanelsProps {
  activeTab: ServerTab
  tabs: ServerTab[]
  server: PanelServer
  list: ContainerList | null
  pollMs: number
  metricsApi?: MetricsApi
  agentVersion: string
  agentUpdateAvailable: boolean
  agentUpdateManual: boolean
  failedServices: number | null
  canOverview: boolean
  canServices: boolean
  canLogs: boolean
  canUpdates: boolean
  canAlerts: boolean
  servicesApi: ServicesApi
  journalApi: JournalApi
  aptApi: AptApi
  alertsApi: AlertsAdminApi
  onGoToTab: (tab: ServerTab) => void
  onServicesLoaded: (services: ServiceUnit[]) => void
  onAptSummary: (apt: AptSummary | null) => void
}

/** ServerPanels renders the non-container tab panels of a server page. */
export function ServerPanels({
  activeTab,
  tabs,
  server,
  list,
  pollMs,
  metricsApi,
  agentVersion,
  agentUpdateAvailable,
  agentUpdateManual,
  failedServices,
  canOverview,
  canServices,
  canLogs,
  canUpdates,
  canAlerts,
  servicesApi,
  journalApi,
  aptApi,
  alertsApi,
  onGoToTab,
  onServicesLoaded,
  onAptSummary,
}: ServerPanelsProps) {
  const { t } = useTranslation('server')
  return (
    <>
      {canOverview && activeTab === 'overview' ? (
        <div
          className="server-tabpanel"
          role="tabpanel"
          id="server-panel-overview"
          aria-labelledby="server-tab-overview"
          tabIndex={0}
        >
          <OverviewTab
            server={server}
            serverId={server.id}
            metricsApi={metricsApi}
            agentVersion={agentVersion}
            agentUpdateAvailable={agentUpdateAvailable}
            agentUpdateManual={agentUpdateManual}
            failedServices={failedServices}
            tabs={tabs}
            onGoToTab={onGoToTab}
          />
        </div>
      ) : null}

      {canServices && activeTab === 'services' ? (
        <div
          className="server-tabpanel"
          role="tabpanel"
          id="server-panel-services"
          aria-labelledby="server-tab-services"
          tabIndex={0}
        >
          {list !== null && !list.connected ? (
            <p className="serverscreen__message">{t('offline')}</p>
          ) : null}
          {list !== null && list.connected ? (
            <ServicesSection
              api={servicesApi}
              serverId={server.id}
              pollMs={pollMs}
              onLoaded={onServicesLoaded}
            />
          ) : null}
        </div>
      ) : null}

      {canLogs && activeTab === 'logs' ? (
        <div
          className="server-tabpanel"
          role="tabpanel"
          id="server-panel-logs"
          aria-labelledby="server-tab-logs"
          tabIndex={0}
        >
          {list !== null && !list.connected ? (
            <p className="serverscreen__message">{t('offline')}</p>
          ) : null}
          {list !== null && list.connected ? (
            <JournalSection
              api={journalApi}
              servicesApi={servicesApi}
              serverId={server.id}
              pollMs={pollMs}
            />
          ) : null}
        </div>
      ) : null}

      {canUpdates && activeTab === 'updates' ? (
        <div
          className="server-tabpanel"
          role="tabpanel"
          id="server-panel-updates"
          aria-labelledby="server-tab-updates"
          tabIndex={0}
        >
          {list !== null && !list.connected ? (
            <p className="serverscreen__message">{t('offline')}</p>
          ) : null}
          {list !== null && list.connected ? (
            <UpdatesSection
              aptApi={aptApi}
              serverId={server.id}
              hostname={server.hostname ?? ''}
              pollMs={pollMs}
              onSummary={onAptSummary}
            />
          ) : null}
        </div>
      ) : null}

      {canAlerts && activeTab === 'alerts' ? (
        <div
          className="server-tabpanel"
          role="tabpanel"
          id="server-panel-alerts"
          aria-labelledby="server-tab-alerts"
          tabIndex={0}
        >
          <AlertsSection
            api={alertsApi}
            serverId={server.id}
            serverName={server.name}
            groupId={server.groupId}
          />
        </div>
      ) : null}
    </>
  )
}
