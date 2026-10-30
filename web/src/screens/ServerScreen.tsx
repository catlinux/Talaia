import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { createAlertsAdminApi, type AlertsAdminApi } from '../api/alertsAdmin'
import { createAptApi, type AptApi } from '../api/apt'
import { createContainersApi, type ContainerList, type ContainersApi } from '../api/containers'
import { isSignedOut } from '../api/http'
import { createJournalApi, type JournalApi } from '../api/journal'
import { createServersApi, type ServerSummary, type ServersApi } from '../api/servers'
import { createServicesApi, type ServiceUnit, type ServicesApi } from '../api/services'
import { createMetricsApi, type MetricsApi } from '../api/metrics'
import { createTerminalApi, type TerminalApi } from '../api/terminal'
import type { AptSummary, ServerStatus } from '../api/types'
import { errorMessage } from '../auth/AuthGate'
import { usePermissions } from '../auth/permissions'
import { useNavigate, useRoute } from '../router'
import { agentUpdateErrorKey } from './server/agentUpdate'
import { ContainersTab } from './server/ContainersTab'
import type { ServerTab } from './server/OverviewTab'
import { ServerHeader } from './server/ServerHeader'
import { ServerPanels } from './server/ServerPanels'
import { ServerTabs } from './server/ServerTabBar'
import { StackScreen } from './server/StackScreen'
import { firstAllowedTab, parseServerPath, stackPath, tabPath } from './server/serverTabs'

/** refreshEvery is how often the list reloads while the screen is open. */
export const refreshEvery = 10_000

/** pollEvery is how often a running job is asked for its progress. */
const pollEvery = 800

/** updateWindowMs is how long the "updating" line stays if no refresh confirms it. */
const updateWindowMs = 3 * 60_000

// xterm is a third of the panel: it is only fetched when a terminal is opened.
const TerminalPanel = lazy(() =>
  import('./server/TerminalPanel').then((module) => ({ default: module.TerminalPanel })),
)

const defaultApi = createContainersApi()
const defaultTerminalApi = createTerminalApi()
const defaultServersApi = createServersApi()
const defaultAptApi = createAptApi()
const defaultAlertsApi = createAlertsAdminApi()
const defaultServicesApi = createServicesApi()
const defaultMetricsApi = createMetricsApi()
const defaultJournalApi = createJournalApi()

interface ServerScreenProps {
  server: {
    id: string
    name: string
    /** groupId is the server group when the caller knows it. */
    groupId?: string
    limited?: boolean
    hostname?: string
    status?: ServerStatus
    hostTerminal?: boolean
    agentUpdateManual?: boolean
    agentUpgradeCommand?: string
    os?: string
    kernel?: string
    arch?: string
    vcpu?: number
    memoryGb?: number
    cpuPercent?: number | null
    memPercent?: number | null
    diskPercent?: number | null
    memUsedBytes?: number
    memTotalBytes?: number
    diskUsedBytes?: number
    diskTotalBytes?: number
    netRxBytes?: number
    netTxBytes?: number
    containers?: number
    runningContainers?: number
    services?: number
    dockerVersion?: string
    uptimeSeconds?: number
    agentVersion?: string
    agentUpdateAvailable?: boolean
    apt?: AptSummary
  }
  /** Kept for callers that used the old back button. */
  onBack?: () => void
  /** Tests inject their own API; the panel uses the real one. */
  api?: ContainersApi
  terminalApi?: TerminalApi
  /** serversApi lists the server and updates its agent; the panel uses the real one. */
  serversApi?: ServersApi
  /** aptApi reads the pending package updates; the panel uses the real one. */
  aptApi?: AptApi
  /** servicesApi lists systemd services and runs actions; the panel uses the real one. */
  servicesApi?: ServicesApi
  metricsApi?: MetricsApi
  /** journalApi reads the systemd journal; the panel uses the real one. */
  journalApi?: JournalApi
  /** alertsApi reads and changes the alert rules of this server. */
  alertsApi?: AlertsAdminApi
  /** pollMs overrides the job polling interval, for the tests. */
  pollMs?: number
}

type ListState =
  | { status: 'loading' }
  | { status: 'ready'; list: ContainerList }
  | { status: 'error'; message: string }

/** ServerScreen is the server page: header, URL tabs and tab panels. */
export function ServerScreen({
  server,
  api = defaultApi,
  terminalApi = defaultTerminalApi,
  serversApi = defaultServersApi,
  aptApi = defaultAptApi,
  servicesApi = defaultServicesApi,
  metricsApi = defaultMetricsApi,
  journalApi = defaultJournalApi,
  alertsApi = defaultAlertsApi,
  pollMs = pollEvery,
}: ServerScreenProps) {
  const { t } = useTranslation(['server', 'errors'])
  const navigate = useNavigate()
  const { path } = useRoute()
  const permissions = usePermissions()
  const permissionsLoading = permissions.loading
  const serverScope = { serverId: server.id }
  const canOverview = !permissionsLoading && permissions.can('server.view', serverScope)
  const canStacks = !permissionsLoading && permissions.canSeeStacks(server.id)
  const canContainers =
    !permissionsLoading && (permissions.canSeeContainers(server.id) || canStacks)
  const canServices = !permissionsLoading && permissions.can('host.services.view', serverScope)
  const canLogs = !permissionsLoading && permissions.can('host.journal', serverScope)
  const canUpdates = !permissionsLoading && permissions.can('host.apt.view', serverScope)
  const canAlerts = !permissionsLoading && permissions.can('alerts.view' as never)
  const canHostTerminal = !permissionsLoading && permissions.can('host.terminal', serverScope)
  const canAgentUpdate = !permissionsLoading && permissions.can('server.agent_update', serverScope)
  const canResources = !permissionsLoading && permissions.can('resources.manage', serverScope)

  const [state, setState] = useState<ListState>({ status: 'loading' })
  const [serverInfo, setServerInfo] = useState<ServerSummary | null>(null)
  const [refreshFailed, setRefreshFailed] = useState(false)
  const [confirmingAgentUpdate, setConfirmingAgentUpdate] = useState(false)
  const [updatingAgent, setUpdatingAgent] = useState(false)
  const [updateError, setUpdateError] = useState<string | null>(null)
  const [hostTerminalOpen, setHostTerminalOpen] = useState(false)
  const [liveApt, setLiveApt] = useState<AptSummary | null>(null)
  const [knownServices, setKnownServices] = useState<ServiceUnit[] | null>(null)
  const [stackFlags, setStackFlags] = useState<{
    name: string
    adopted: boolean
    broken: boolean
  } | null>(null)
  const mounted = useRef(true)
  const updatingRef = useRef(false)
  const updateStartedVersionRef = useRef('')

  const parsed = parseServerPath(path, server.id)
  const stackName = parsed.stackName

  const availableTabs = useMemo<ServerTab[]>(() => {
    const tabs: ServerTab[] = []
    if (canOverview) tabs.push('overview')
    if (canContainers) tabs.push('containers')
    if (canServices) tabs.push('services')
    if (canLogs) tabs.push('logs')
    if (canUpdates) tabs.push('updates')
    if (canAlerts) tabs.push('alerts')
    return tabs
  }, [canAlerts, canContainers, canLogs, canOverview, canServices, canUpdates])
  const availableTabKey = availableTabs.join(',')
  const fallbackTab = firstAllowedTab(availableTabs, server.limited)
  const parsedAllowed = parsed.tab !== null && availableTabs.includes(parsed.tab)
  const stackAllowed = stackName === null || (parsed.tab === 'containers' && canStacks)
  const activeTab: ServerTab =
    parsedAllowed && parsed.tab !== null && stackAllowed ? parsed.tab : fallbackTab
  const showStack = stackName !== null && parsed.tab === 'containers' && canStacks

  const load = useCallback(async () => {
    try {
      const list = await api.list(server.id)
      if (!mounted.current) return
      setState({ status: 'ready', list })
      setRefreshFailed(false)
    } catch (error) {
      if (!mounted.current || isSignedOut(error)) return
      setState((current) =>
        current.status === 'ready' ? current : { status: 'error', message: errorMessage(t, error) },
      )
      setRefreshFailed(true)
    }
  }, [api, server.id, t])

  const loadServer = useCallback(async () => {
    try {
      const list = await serversApi.listServers()
      if (!mounted.current) return
      const info = list.find((candidate) => candidate.id === server.id) ?? null
      setServerInfo(info)
      if (
        updatingRef.current &&
        info !== null &&
        !info.agentUpdateAvailable &&
        info.agentVersion !== '' &&
        info.agentVersion !== updateStartedVersionRef.current
      ) {
        updatingRef.current = false
        setUpdatingAgent(false)
      }
    } catch (error) {
      if (!mounted.current || isSignedOut(error)) return
    }
  }, [server.id, serversApi])

  useEffect(() => {
    mounted.current = true
    if (!permissionsLoading) {
      if (canContainers) void load()
      void loadServer()
    }
    const timer = setInterval(() => {
      if (!permissionsLoading && !document.hidden) {
        if (canContainers) void load()
        void loadServer()
      }
    }, refreshEvery)
    return () => {
      mounted.current = false
      clearInterval(timer)
    }
  }, [canContainers, load, loadServer, permissionsLoading])

  useEffect(() => {
    setKnownServices(null)
  }, [server.id])

  useEffect(() => {
    if (permissionsLoading || availableTabKey === '') return
    if (parsedAllowed && stackAllowed) return
    navigate(tabPath(server.id, fallbackTab), { replace: true })
  }, [
    availableTabKey,
    fallbackTab,
    navigate,
    parsedAllowed,
    permissionsLoading,
    server.id,
    stackAllowed,
  ])

  useEffect(() => {
    if (stackName === null) return
    let active = true
    api.stacks(server.id).then(
      (list) => {
        if (!active) return
        setStackFlags({
          name: stackName,
          adopted: list.adopted.includes(stackName),
          broken: list.broken.includes(stackName),
        })
      },
      () => {
        if (active) setStackFlags(null)
      },
    )
    return () => {
      active = false
    }
  }, [api, server.id, stackName])

  useEffect(() => {
    if (!updatingAgent) return
    const timer = setTimeout(() => {
      updatingRef.current = false
      setUpdatingAgent(false)
    }, updateWindowMs)
    return () => clearTimeout(timer)
  }, [updatingAgent])

  const startAgentUpdate = useCallback(() => {
    setConfirmingAgentUpdate(false)
    setUpdateError(null)
    setUpdatingAgent(true)
    updatingRef.current = true
    updateStartedVersionRef.current = serverInfo?.agentVersion ?? ''
    serversApi.updateAgent(server.id).then(
      () => {
        void loadServer()
      },
      (error: unknown) => {
        updatingRef.current = false
        setUpdatingAgent(false)
        if (isSignedOut(error)) return
        const key = agentUpdateErrorKey(error)
        setUpdateError(key === '' ? errorMessage(t, error) : t(`server:${key}`))
      },
    )
  }, [loadServer, server.id, serverInfo?.agentVersion, serversApi, t])

  const handleServicesLoaded = useCallback((services: ServiceUnit[]) => {
    setKnownServices(services)
  }, [])

  const list = state.status === 'ready' ? state.list : null
  const containers = list?.containers ?? []
  const running = containers.filter((container) => container.state === 'running').length
  const agentVersion = serverInfo?.agentVersion || server.agentVersion || ''
  const agentUpdateAvailable =
    serverInfo?.agentUpdateAvailable ?? server.agentUpdateAvailable ?? false
  const agentUpdateManual = server.agentUpdateManual || serverInfo?.agentUpdateManual || false
  const hostTerminal = serverInfo?.hostTerminal ?? server.hostTerminal ?? false
  const hostTerminalConnected = serverInfo?.connected ?? list?.connected ?? false
  const canUpdateAgent =
    canAgentUpdate &&
    serverInfo !== null &&
    serverInfo.connected &&
    serverInfo.agentUpdateAvailable &&
    !updatingAgent
  const apt = liveApt ?? server.apt
  const failedServices =
    knownServices === null
      ? null
      : knownServices.filter((service) => service.activeState === 'failed').length
  const containerTotal = list !== null ? containers.length : (server.containers ?? 0)
  const containerRunning = list !== null ? running : (server.runningContainers ?? 0)
  const overviewServer = {
    ...server,
    cpuPercent: server.cpuPercent ?? null,
    memPercent: server.memPercent ?? null,
    diskPercent: server.diskPercent ?? null,
    containers: containerTotal,
    runningContainers: containerRunning,
    services: server.services ?? 0,
  }

  if (showStack && stackName !== null) {
    return (
      <StackScreen
        api={api}
        metricsApi={metricsApi}
        server={server}
        name={stackName}
        adopted={stackFlags?.name === stackName ? stackFlags.adopted : false}
        broken={stackFlags?.name === stackName ? stackFlags.broken : false}
        containers={containers}
        connected={list?.connected ?? false}
        dockerAvailable={list?.dockerAvailable ?? false}
        pollMs={pollMs}
        onBack={() => navigate(tabPath(server.id, 'containers'))}
        onChanged={() => void load()}
      />
    )
  }

  return (
    <section className="serverscreen" aria-labelledby="server-title">
      <div className="serverscreen__sticky">
        <ServerHeader
          server={server}
          serverInfo={serverInfo}
          agentVersion={agentVersion}
          containers={overviewServer.containers ?? 0}
          runningContainers={overviewServer.runningContainers ?? 0}
          canHostTerminal={canHostTerminal}
          hostTerminal={hostTerminal}
          hostTerminalConnected={hostTerminalConnected}
          canUpdateAgent={canUpdateAgent}
          canAgentUpdate={canAgentUpdate}
          canAlerts={canAlerts}
          onOpenHostTerminal={() => setHostTerminalOpen(true)}
          onRequestAgentUpdate={() => {
            setUpdateError(null)
            setConfirmingAgentUpdate(true)
          }}
        />

        <ServerTabs
          serverId={server.id}
          tabs={availableTabs}
          activeTab={activeTab}
          apt={apt}
          failedServices={failedServices}
        />
      </div>

      {hostTerminalOpen && hostTerminalConnected ? (
        <Suspense
          fallback={
            <p className="terminal__message" role="status">
              {t('server:terminal.opening')}
            </p>
          }
        >
          <TerminalPanel
            api={terminalApi}
            serverId={server.id}
            serverName={server.name}
            target={{ kind: 'host' }}
            onClose={() => setHostTerminalOpen(false)}
          />
        </Suspense>
      ) : null}

      {state.status === 'loading' ? (
        <p className="serverscreen__message" role="status">
          {t('server:loading')}
        </p>
      ) : null}

      {state.status === 'error' ? (
        <p className="serverscreen__message serverscreen__message--error" role="alert">
          {state.message}
        </p>
      ) : null}

      {refreshFailed && state.status === 'ready' ? (
        <p className="serverscreen__notice" role="status">
          {t('server:refreshFailed')}
        </p>
      ) : null}

      {updatingAgent ? (
        <p className="serverscreen__notice" role="status">
          {t('server:agentUpdate.running')}
        </p>
      ) : null}

      {updateError !== null ? (
        <p className="serverscreen__message serverscreen__message--error" role="alert">
          {updateError}
        </p>
      ) : null}

      {canContainers && activeTab === 'containers' ? (
        <div
          className="server-tabpanel"
          role="tabpanel"
          id="server-panel-containers"
          aria-labelledby="server-tab-containers"
          tabIndex={0}
        >
          <ContainersTab
            api={api}
            terminalApi={terminalApi}
            server={server}
            list={list}
            pollMs={pollMs}
            canStacks={canStacks}
            canResources={canResources}
            onReload={load}
            onOpenStack={(name) => navigate(stackPath(server.id, name))}
          />
        </div>
      ) : null}

      <ServerPanels
        activeTab={activeTab}
        tabs={availableTabs}
        server={overviewServer}
        list={list}
        pollMs={pollMs}
        metricsApi={metricsApi}
        agentVersion={agentVersion}
        agentUpdateAvailable={agentUpdateAvailable}
        agentUpdateManual={agentUpdateManual}
        failedServices={failedServices}
        canOverview={canOverview}
        canServices={canServices}
        canLogs={canLogs}
        canUpdates={canUpdates}
        canAlerts={canAlerts}
        servicesApi={servicesApi}
        journalApi={journalApi}
        aptApi={aptApi}
        alertsApi={alertsApi}
        onGoToTab={(tab) => navigate(tabPath(server.id, tab))}
        onServicesLoaded={handleServicesLoaded}
        onAptSummary={setLiveApt}
      />

      {confirmingAgentUpdate ? (
        <div className="settings__overlay">
          <div
            className="settings__dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="server-agent-update-title"
          >
            <h2 className="settings__dialog-title" id="server-agent-update-title">
              {t('server:agentUpdate.confirmTitle')}
            </h2>
            <p className="settings__dialog-body">{t('server:agentUpdate.confirmBody')}</p>
            <div className="settings__dialog-actions">
              <button
                type="button"
                className="settings__button settings__button--primary"
                onClick={startAgentUpdate}
              >
                {t('server:agentUpdate.confirm')}
              </button>
              <button
                type="button"
                className="settings__button"
                onClick={() => setConfirmingAgentUpdate(false)}
              >
                {t('server:agentUpdate.cancel')}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </section>
  )
}
