import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'

import { createAptApi, type AptApi } from '../api/apt'
import {
  createContainersApi,
  type Container,
  type ContainerAction,
  type ContainerList,
  type ContainersApi,
} from '../api/containers'
import { isSignedOut } from '../api/http'
import { createJournalApi, type JournalApi } from '../api/journal'
import {
  AgentUpdateError,
  createServersApi,
  type ServerSummary,
  type ServersApi,
} from '../api/servers'
import { createServicesApi, type ServiceUnit, type ServicesApi } from '../api/services'
import { createTerminalApi, type TerminalApi } from '../api/terminal'
import type { AptSummary } from '../api/types'
import { errorMessage } from '../auth/AuthGate'
import {
  ArrowLeftIcon,
  LogsIcon,
  PlayIcon,
  RestartIcon,
  StopIcon,
  TerminalIcon,
  TrashIcon,
} from '../components/icons'
import { formatBytes, formatPercent } from './fleet/format'
import { LogsPanel } from './server/LogsPanel'
import { JournalSection } from './server/JournalSection'
import { ManualUpgrade } from './server/ManualUpgrade'
import { OverviewTab, type ServerTab } from './server/OverviewTab'
import { ResourcesSection } from './server/ResourcesSection'
import { ServicesSection } from './server/ServicesSection'
import { StackScreen } from './server/StackScreen'
import { StacksSection } from './server/StacksSection'
import { UpdatesSection } from './server/UpdatesSection'
import { TimedOut, waitForJob } from './server/waitForJob'
import { UseGauge } from './server/UseGauge'

/** refreshEvery is how often the list reloads while the screen is open. */
export const refreshEvery = 10_000

/** pollEvery is how often a running job is asked for its progress. */
const pollEvery = 800

/** updateWindowMs is how long the "updating" line stays if no refresh confirms it. */
const updateWindowMs = 3 * 60_000

const serverTabs: ServerTab[] = ['overview', 'containers', 'services', 'logs', 'updates']

/** tabStorageKey remembers the last tab of each server in this session. */
function tabStorageKey(serverId: string): string {
  return `talaia.serverTab.${serverId}`
}

/** readStoredTab returns the last tab of a server, or Overview. */
function readStoredTab(serverId: string): ServerTab {
  try {
    const value = sessionStorage.getItem(tabStorageKey(serverId))
    if (value !== null && serverTabs.includes(value as ServerTab)) {
      return value as ServerTab
    }
  } catch {
    // Storage can be unavailable (private mode, policy); Overview is enough.
  }
  return 'overview'
}

function storeTab(serverId: string, tab: ServerTab): void {
  try {
    sessionStorage.setItem(tabStorageKey(serverId), tab)
  } catch {
    // The tab still works for this render when storage is unavailable.
  }
}

// xterm is a third of the panel: it is only fetched when a terminal is opened.
const TerminalPanel = lazy(() =>
  import('./server/TerminalPanel').then((module) => ({ default: module.TerminalPanel })),
)

const defaultApi = createContainersApi()
const defaultTerminalApi = createTerminalApi()
const defaultServersApi = createServersApi()
const defaultAptApi = createAptApi()
const defaultServicesApi = createServicesApi()
const defaultJournalApi = createJournalApi()

interface ServerScreenProps {
  server: {
    id: string
    name: string
    hostname?: string
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
  onBack: () => void
  /** Tests inject their own API; the panel uses the real one. */
  api?: ContainersApi
  terminalApi?: TerminalApi
  /** serversApi lists the server and updates its agent; the panel uses the real one. */
  serversApi?: ServersApi
  /** aptApi reads the pending package updates; the panel uses the real one. */
  aptApi?: AptApi
  /** servicesApi lists systemd services and runs actions; the panel uses the real one. */
  servicesApi?: ServicesApi
  /** journalApi reads the systemd journal; the panel uses the real one. */
  journalApi?: JournalApi
  /** pollMs overrides the job polling interval, for the tests. */
  pollMs?: number
}

/** agentUpdateErrorKey returns the translation key of a refused update. */
function agentUpdateErrorKey(error: unknown): string {
  if (!(error instanceof AgentUpdateError)) {
    return ''
  }
  switch (error.code) {
    case 'agent_offline':
      return 'agentUpdate.errors.agent_offline'
    case 'no_update':
      return 'agentUpdate.errors.no_update'
    case 'update_in_progress':
      return 'agentUpdate.errors.update_in_progress'
    case 'session_required':
      return 'agentUpdate.errors.session_required'
    case 'not_found':
      return 'agentUpdate.errors.not_found'
    default:
      return 'agentUpdate.errors.generic'
  }
}

/** Outcome is what the screen tells about the last action of a container. */
type Outcome =
  | { kind: 'working'; action: ContainerAction }
  | { kind: 'done'; action: ContainerAction }
  | { kind: 'failed'; message: string }

type ListState =
  | { status: 'loading' }
  | { status: 'ready'; list: ContainerList }
  | { status: 'error'; message: string }

const stateOrder = ['running', 'restarting', 'paused', 'created', 'exited', 'dead']

/** sortContainers puts the stacks together, running containers first. */
function sortContainers(containers: Container[]): Container[] {
  return [...containers].sort((a, b) => {
    if (a.composeProject !== b.composeProject) {
      if (a.composeProject === '') return 1
      if (b.composeProject === '') return -1
      return a.composeProject.localeCompare(b.composeProject)
    }
    const byState = stateOrder.indexOf(a.state) - stateOrder.indexOf(b.state)
    return byState !== 0 ? byState : a.name.localeCompare(b.name)
  })
}

const actionIcons = {
  start: PlayIcon,
  stop: StopIcon,
  restart: RestartIcon,
  remove: TrashIcon,
} as const

/**
 * ServerScreen lists the containers of one server and runs actions on them.
 * Every action is a job: the screen sends it, follows its progress and reloads
 * the list when it ends. Removing asks for a confirmation first.
 */
export function ServerScreen({
  server,
  onBack,
  api = defaultApi,
  terminalApi = defaultTerminalApi,
  serversApi = defaultServersApi,
  aptApi = defaultAptApi,
  servicesApi = defaultServicesApi,
  journalApi = defaultJournalApi,
  pollMs = pollEvery,
}: ServerScreenProps) {
  const { t } = useTranslation(['server', 'errors'])
  const [state, setState] = useState<ListState>({ status: 'loading' })
  const [serverInfo, setServerInfo] = useState<ServerSummary | null>(null)
  const [refreshFailed, setRefreshFailed] = useState(false)
  const [outcomes, setOutcomes] = useState<Record<string, Outcome>>({})
  // The container action that is waiting for confirmation, if any.
  const [confirming, setConfirming] = useState<{
    id: string
    action: 'stop' | 'remove'
  } | null>(null)
  const [confirmingAgentUpdate, setConfirmingAgentUpdate] = useState(false)
  const [updatingAgent, setUpdatingAgent] = useState(false)
  const [updateError, setUpdateError] = useState<string | null>(null)
  // The container whose logs are open, if any.
  const [logsFor, setLogsFor] = useState<string | null>(null)
  // The container whose terminal is open, if any.
  const [terminalFor, setTerminalFor] = useState<string | null>(null)
  // Whether the root terminal on the server itself is open.
  const [hostTerminalOpen, setHostTerminalOpen] = useState(false)
  // The tab is remembered per server for this browser session.
  const [activeTab, setActiveTab] = useState<ServerTab>(() => readStoredTab(server.id))
  // The services list is only read in its tab; keeping the last result lets the
  // tab badge and the overview keep working after switching away.
  const [liveApt, setLiveApt] = useState<AptSummary | null>(null)
  const [knownServices, setKnownServices] = useState<ServiceUnit[] | null>(null)
  // The managed stack whose screen is open, if any.
  const [openStack, setOpenStack] = useState<{
    name: string
    adopted: boolean
    broken: boolean
  } | null>(null)
  const mounted = useRef(true)
  const updatingRef = useRef(false)
  const updateStartedVersionRef = useRef('')

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
      // A refresh with the new version clears the "updating" line.
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

  const selectTab = useCallback(
    (tab: ServerTab, focus = false) => {
      setActiveTab(tab)
      storeTab(server.id, tab)
      if (focus) {
        requestAnimationFrame(() => {
          document.getElementById(`server-tab-${tab}`)?.focus()
        })
      }
    },
    [server.id],
  )

  const handleServicesLoaded = useCallback((services: ServiceUnit[]) => {
    setKnownServices(services)
  }, [])

  useEffect(() => {
    setActiveTab(readStoredTab(server.id))
    setKnownServices(null)
  }, [server.id])

  useEffect(() => {
    mounted.current = true
    void load()
    void loadServer()
    const timer = setInterval(() => {
      if (!document.hidden) {
        void load()
        void loadServer()
      }
    }, refreshEvery)
    return () => {
      mounted.current = false
      clearInterval(timer)
    }
  }, [load, loadServer])

  useEffect(() => {
    if (!updatingAgent) {
      return
    }
    const timer = setTimeout(() => {
      updatingRef.current = false
      setUpdatingAgent(false)
    }, updateWindowMs)
    return () => clearTimeout(timer)
  }, [updatingAgent])

  const setOutcome = useCallback((id: string, outcome: Outcome | null) => {
    setOutcomes((current) => {
      const next = { ...current }
      if (outcome === null) delete next[id]
      else next[id] = outcome
      return next
    })
  }, [])

  const run = useCallback(
    async (container: Container, action: ContainerAction) => {
      setConfirming(null)
      setOutcome(container.id, { kind: 'working', action })
      try {
        const jobId = await api.run(server.id, container.name, action, action === 'remove')
        const job = await waitForJob(api, jobId, pollMs, () => mounted.current)
        if (job === null) return
        setOutcome(
          container.id,
          job.state === 'succeeded'
            ? { kind: 'done', action }
            : { kind: 'failed', message: job.message },
        )
      } catch (error) {
        if (!mounted.current || isSignedOut(error)) return
        const message =
          error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error)
        setOutcome(container.id, { kind: 'failed', message })
      }
      await load()
    },
    [api, load, pollMs, server.id, setOutcome, t],
  )

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

  const list = state.status === 'ready' ? state.list : null
  const containers = list ? sortContainers(list.containers) : []
  const running = containers.filter((container) => container.state === 'running').length
  const agentVersion = serverInfo?.agentVersion || server.agentVersion || ''
  const agentUpdateAvailable =
    serverInfo?.agentUpdateAvailable ?? server.agentUpdateAvailable ?? false
  const agentUpdateManual = server.agentUpdateManual || serverInfo?.agentUpdateManual || false
  const agentUpgradeCommand = server.agentUpgradeCommand || serverInfo?.agentUpgradeCommand || ''
  const hostTerminal = serverInfo?.hostTerminal ?? server.hostTerminal ?? false
  const hostTerminalConnected = serverInfo?.connected ?? list?.connected ?? false
  const canUpdateAgent =
    serverInfo !== null && serverInfo.connected && serverInfo.agentUpdateAvailable && !updatingAgent
  const apt = liveApt ?? server.apt
  const failedServices =
    knownServices === null
      ? null
      : knownServices.filter((service) => service.activeState === 'failed').length
  const overviewServer = {
    ...server,
    cpuPercent: server.cpuPercent ?? null,
    memPercent: server.memPercent ?? null,
    diskPercent: server.diskPercent ?? null,
    containers: server.containers ?? containers.length,
    runningContainers: server.runningContainers ?? running,
    services: server.services ?? 0,
  }

  const moveTab = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    let next: number
    switch (event.key) {
      case 'ArrowLeft':
        next = (index - 1 + serverTabs.length) % serverTabs.length
        break
      case 'ArrowRight':
        next = (index + 1) % serverTabs.length
        break
      case 'Home':
        next = 0
        break
      case 'End':
        next = serverTabs.length - 1
        break
      default:
        return
    }
    event.preventDefault()
    selectTab(serverTabs[next]!, true)
  }

  if (openStack !== null) {
    return (
      <StackScreen
        api={api}
        server={server}
        name={openStack.name}
        adopted={openStack.adopted}
        broken={openStack.broken}
        containers={list?.containers ?? []}
        connected={list?.connected ?? false}
        dockerAvailable={list?.dockerAvailable ?? false}
        pollMs={pollMs}
        onBack={() => setOpenStack(null)}
        onChanged={() => void load()}
      />
    )
  }

  return (
    <section className="serverscreen" aria-labelledby="server-title">
      <button type="button" className="serverscreen__back" onClick={onBack}>
        <ArrowLeftIcon />
        {t('server:back')}
      </button>

      <header className="serverscreen__head">
        <h1 className="serverscreen__title" id="server-title">
          {server.name}
        </h1>
        {list !== null && list.connected && list.dockerAvailable ? (
          <p className="serverscreen__summary">
            {t('server:summary', { count: containers.length, running })}
          </p>
        ) : null}
        {agentVersion !== '' || canUpdateAgent ? (
          <div className="serverscreen__meta">
            {agentVersion !== '' ? (
              <span className="serverscreen__agent">
                {t('server:agentVersion', { version: agentVersion })}
              </span>
            ) : null}
            {canUpdateAgent ? (
              <button
                type="button"
                className="serverscreen__update"
                onClick={() => {
                  setUpdateError(null)
                  setConfirmingAgentUpdate(true)
                }}
              >
                {t('server:agentUpdate.button')}
              </button>
            ) : null}
          </div>
        ) : null}
        {hostTerminalConnected ? (
          <button
            type="button"
            className="cbtn serverscreen__terminal"
            disabled={!hostTerminal}
            title={hostTerminal ? undefined : t('server:terminal.hostUnavailable')}
            onClick={() => setHostTerminalOpen(true)}
          >
            <TerminalIcon />
            {t('server:terminal.hostButton')}
          </button>
        ) : null}
        {agentUpdateManual ? <ManualUpgrade command={agentUpgradeCommand} /> : null}
      </header>

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

      <div className="server-tabs" role="tablist" aria-label={t('server:tabs.label')}>
        {serverTabs.map((tab, index) => {
          const selected = activeTab === tab
          const label = t(`server:tabs.${tab}`)
          const updatesBadge = tab === 'updates' && apt ? `${apt.security}/${apt.upgradable}` : ''
          const failedBadge =
            tab === 'services' && failedServices !== null && failedServices > 0
              ? t('server:tabs.failedBadge', { count: failedServices })
              : ''
          const badge = updatesBadge || failedBadge
          const alert = tab === 'updates' && (apt?.security ?? 0) > 0
          const badgeID = badge === '' ? undefined : `server-tab-${tab}-badge`
          return (
            <button
              key={tab}
              id={`server-tab-${tab}`}
              type="button"
              role="tab"
              className={selected ? 'server-tabs__tab server-tabs__tab--on' : 'server-tabs__tab'}
              aria-label={label}
              aria-selected={selected}
              aria-controls={`server-panel-${tab}`}
              aria-describedby={badgeID}
              tabIndex={selected ? 0 : -1}
              onClick={() => selectTab(tab)}
              onKeyDown={(event) => moveTab(event, index)}
            >
              <span>{label}</span>
              {badge !== '' ? (
                <span
                  id={badgeID}
                  className={
                    alert ? 'server-tabs__badge server-tabs__badge--alert' : 'server-tabs__badge'
                  }
                >
                  {badge}
                </span>
              ) : null}
            </button>
          )
        })}
      </div>

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

      {activeTab === 'overview' ? (
        <div
          className="server-tabpanel"
          role="tabpanel"
          id="server-panel-overview"
          aria-labelledby="server-tab-overview"
          tabIndex={0}
        >
          <OverviewTab
            server={overviewServer}
            agentVersion={agentVersion}
            agentUpdateAvailable={agentUpdateAvailable}
            agentUpdateManual={agentUpdateManual}
            failedServices={failedServices}
            onGoToTab={selectTab}
          />
        </div>
      ) : null}

      {activeTab === 'containers' ? (
        <div
          className="server-tabpanel"
          role="tabpanel"
          id="server-panel-containers"
          aria-labelledby="server-tab-containers"
          tabIndex={0}
        >
          {list !== null && !list.connected ? (
            <p className="serverscreen__message">{t('server:offline')}</p>
          ) : null}
          {list !== null && list.connected && !list.dockerAvailable ? (
            <p className="serverscreen__message">{t('server:noDocker')}</p>
          ) : null}
          {list !== null && list.connected && list.dockerAvailable && containers.length === 0 ? (
            <p className="serverscreen__message">{t('server:empty')}</p>
          ) : null}

          {list !== null && list.connected && list.dockerAvailable ? (
            <StacksSection
              api={api}
              serverId={server.id}
              containers={list.containers}
              pollMs={pollMs}
              onChanged={() => void load()}
              onOpenStack={(name, adopted, broken) => setOpenStack({ name, adopted, broken })}
            />
          ) : null}

          {containers.length > 0 ? (
            <ul className="containers" aria-label={t('server:title')}>
              {containers.map((container) => (
                <ContainerRow
                  key={container.id}
                  container={container}
                  outcome={outcomes[container.id]}
                  confirming={
                    confirming !== null && confirming.id === container.id ? confirming.action : null
                  }
                  onAction={(action) => {
                    if (action === 'stop' || action === 'remove') {
                      setConfirming({ id: container.id, action })
                    } else {
                      void run(container, action)
                    }
                  }}
                  onConfirm={() => {
                    if (confirming !== null) {
                      void run(container, confirming.action)
                    }
                  }}
                  onCancel={() => setConfirming(null)}
                  onToggleLogs={() =>
                    setLogsFor((current) => (current === container.id ? null : container.id))
                  }
                  onToggleTerminal={() =>
                    setTerminalFor((current) => (current === container.id ? null : container.id))
                  }
                  terminal={
                    terminalFor === container.id ? (
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
                          target={{ kind: 'container', container: container.name }}
                          onClose={() => setTerminalFor(null)}
                        />
                      </Suspense>
                    ) : null
                  }
                  logs={
                    logsFor === container.id ? (
                      <LogsPanel
                        api={api}
                        serverId={server.id}
                        container={container.name}
                        pollMs={pollMs}
                        onClose={() => setLogsFor(null)}
                      />
                    ) : null
                  }
                />
              ))}
            </ul>
          ) : null}

          {list !== null && list.connected && list.dockerAvailable ? (
            <ResourcesSection api={api} serverId={server.id} pollMs={pollMs} />
          ) : null}
        </div>
      ) : null}

      {activeTab === 'services' ? (
        <div
          className="server-tabpanel"
          role="tabpanel"
          id="server-panel-services"
          aria-labelledby="server-tab-services"
          tabIndex={0}
        >
          {list !== null && !list.connected ? (
            <p className="serverscreen__message">{t('server:offline')}</p>
          ) : null}
          {list !== null && list.connected ? (
            <ServicesSection
              api={servicesApi}
              serverId={server.id}
              pollMs={pollMs}
              onLoaded={handleServicesLoaded}
            />
          ) : null}
        </div>
      ) : null}

      {activeTab === 'logs' ? (
        <div
          className="server-tabpanel"
          role="tabpanel"
          id="server-panel-logs"
          aria-labelledby="server-tab-logs"
          tabIndex={0}
        >
          {list !== null && !list.connected ? (
            <p className="serverscreen__message">{t('server:offline')}</p>
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

      {activeTab === 'updates' ? (
        <div
          className="server-tabpanel"
          role="tabpanel"
          id="server-panel-updates"
          aria-labelledby="server-tab-updates"
          tabIndex={0}
        >
          {list !== null && !list.connected ? (
            <p className="serverscreen__message">{t('server:offline')}</p>
          ) : null}
          {list !== null && list.connected ? (
            <UpdatesSection
              aptApi={aptApi}
              serverId={server.id}
              hostname={server.hostname ?? ''}
              pollMs={pollMs}
              onSummary={setLiveApt}
            />
          ) : null}
        </div>
      ) : null}

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

interface ContainerRowProps {
  container: Container
  outcome: Outcome | undefined
  /** confirming is the action waiting for confirmation, or null. */
  confirming: 'stop' | 'remove' | null
  onAction: (action: ContainerAction) => void
  onConfirm: () => void
  onCancel: () => void
  onToggleLogs: () => void
  onToggleTerminal: () => void
  /** logs is the open log viewer of this container, if any. */
  logs: ReactNode
  /** terminal is the open terminal of this container, if any. */
  terminal: ReactNode
}

function ContainerRow({
  container,
  outcome,
  confirming,
  onAction,
  onConfirm,
  onCancel,
  onToggleLogs,
  onToggleTerminal,
  logs,
  terminal,
}: ContainerRowProps) {
  const { t } = useTranslation('server')
  const busy = outcome?.kind === 'working'
  const isRunning = container.state === 'running'

  const actions: ContainerAction[] = isRunning ? ['restart', 'stop', 'remove'] : ['start', 'remove']

  return (
    <li className={`container container--${container.state}`}>
      <div className="container__main">
        <div className="container__head">
          <h3 className="container__name">{container.name}</h3>
          <span className={`status status--${isRunning ? 'online' : 'offline'}`}>
            {t(`state.${container.state}`, { defaultValue: container.state })}
          </span>
          {container.composeProject !== '' ? (
            <span className="tag">{t('project', { name: container.composeProject })}</span>
          ) : null}
        </div>
        <p className="container__meta">
          <span className="container__image">{container.image}</span>
          <span className="container__status">{container.status}</span>
        </p>
        {isRunning && container.use ? <ContainerUseGauges container={container} /> : null}
        {outcome?.kind === 'working' ? (
          <p className="container__outcome" role="status">
            {t('progress.working')}
          </p>
        ) : null}
        {outcome?.kind === 'done' ? (
          <p className="container__outcome container__outcome--ok" role="status">
            {t('progress.done')}
          </p>
        ) : null}
        {outcome?.kind === 'failed' ? (
          <p className="container__outcome container__outcome--error" role="alert">
            {t('progress.failed', { message: outcome.message })}
          </p>
        ) : null}
      </div>

      {confirming ? (
        <div
          className="container__confirm"
          role="group"
          aria-label={t(confirming === 'remove' ? 'confirm.removeTitle' : 'confirm.stopTitle', {
            name: container.name,
          })}
        >
          <p className="container__confirm-title">
            {t(confirming === 'remove' ? 'confirm.removeTitle' : 'confirm.stopTitle', {
              name: container.name,
            })}
          </p>
          <p className="container__confirm-body">
            {t(confirming === 'remove' ? 'confirm.removeBody' : 'confirm.stopBody')}
          </p>
          <div className="container__actions">
            <button
              type="button"
              className={confirming === 'remove' ? 'cbtn cbtn--danger' : 'cbtn'}
              onClick={onConfirm}
            >
              {t(confirming === 'remove' ? 'confirm.yes' : 'confirm.stopYes')}
            </button>
            <button type="button" className="cbtn" onClick={onCancel}>
              {t('confirm.no')}
            </button>
          </div>
        </div>
      ) : (
        <div className="container__actions">
          <button
            type="button"
            className="cbtn"
            aria-expanded={logs !== null}
            onClick={onToggleLogs}
          >
            <LogsIcon />
            {t('action.logs')}
          </button>
          {isRunning ? (
            <button
              type="button"
              className="cbtn"
              aria-expanded={terminal !== null}
              onClick={onToggleTerminal}
            >
              <TerminalIcon />
              {t('action.terminal')}
            </button>
          ) : null}
          {actions.map((action) => {
            const ActionIcon = actionIcons[action]
            return (
              <button
                key={action}
                type="button"
                className={action === 'remove' ? 'cbtn cbtn--danger' : 'cbtn'}
                disabled={busy}
                onClick={() => onAction(action)}
              >
                <ActionIcon />
                {t(`action.${action}`)}
              </button>
            )
          })}
        </div>
      )}
      {logs}
      {terminal}
    </li>
  )
}

/** ContainerUseGauges shows the CPU and the memory of a running container. */
function ContainerUseGauges({ container }: { container: Container }) {
  const { t, i18n } = useTranslation(['server', 'fleet'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const use = container.use
  if (!use) return null

  const memoryPercent = use.memLimitBytes > 0 ? (use.memUsedBytes / use.memLimitBytes) * 100 : 0
  const memoryText =
    use.memLimitBytes > 0
      ? `${formatBytes(use.memUsedBytes, language)} / ${formatBytes(use.memLimitBytes, language)}`
      : formatBytes(use.memUsedBytes, language)

  return (
    <div className="container__use" aria-label={t('server:use.title')}>
      <UseGauge
        label={t('fleet:metric.cpu')}
        percent={use.cpuPercent}
        value={formatPercent(use.cpuPercent, language)}
      />
      <UseGauge label={t('fleet:metric.ram')} percent={memoryPercent} value={memoryText} />
    </div>
  )
}
