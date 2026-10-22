import { lazy, Suspense, useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  createContainersApi,
  type Container,
  type ContainerAction,
  type ContainerList,
  type ContainersApi,
} from '../api/containers'
import { isSignedOut } from '../api/http'
import {
  AgentUpdateError,
  createServersApi,
  type ServerSummary,
  type ServersApi,
} from '../api/servers'
import { createTerminalApi, type TerminalApi } from '../api/terminal'
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
import { ManualUpgrade } from './server/ManualUpgrade'
import { ResourcesSection } from './server/ResourcesSection'
import { StackScreen } from './server/StackScreen'
import { StacksSection } from './server/StacksSection'
import { TimedOut, waitForJob } from './server/waitForJob'
import { UseGauge } from './server/UseGauge'

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

interface ServerScreenProps {
  server: {
    id: string
    name: string
    agentUpdateManual?: boolean
    agentUpgradeCommand?: string
  }
  onBack: () => void
  /** Tests inject their own API; the panel uses the real one. */
  api?: ContainersApi
  terminalApi?: TerminalApi
  /** serversApi lists the server and updates its agent; the panel uses the real one. */
  serversApi?: ServersApi
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
  const agentVersion = serverInfo?.agentVersion ?? ''
  const agentUpdateManual = server.agentUpdateManual || serverInfo?.agentUpdateManual || false
  const agentUpgradeCommand = server.agentUpgradeCommand || serverInfo?.agentUpgradeCommand || ''
  const canUpdateAgent =
    serverInfo !== null && serverInfo.connected && serverInfo.agentUpdateAvailable && !updatingAgent

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
        {agentUpdateManual ? <ManualUpgrade command={agentUpgradeCommand} /> : null}
      </header>

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
                      container={container.name}
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
