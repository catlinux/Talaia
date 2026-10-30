import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Container, ContainerAction, ContainerList, ContainersApi } from '../../api/containers'
import { isSignedOut } from '../../api/http'
import type { TerminalApi } from '../../api/terminal'
import { errorMessage } from '../../auth/AuthGate'
import { ContainerRow, type ContainerOutcome } from './ContainerRow'
import { LogsPanel } from './LogsPanel'
import { ResourcesSection } from './ResourcesSection'
import { StacksSection } from './StacksSection'
import { TimedOut, waitForJob } from './waitForJob'

// xterm is a third of the panel: it is only fetched when a terminal is opened.
const TerminalPanel = lazy(() =>
  import('./TerminalPanel').then((module) => ({ default: module.TerminalPanel })),
)

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

interface ContainersTabProps {
  api: ContainersApi
  terminalApi: TerminalApi
  server: { id: string; name: string }
  list: ContainerList | null
  pollMs: number
  canStacks: boolean
  canResources: boolean
  /** onReload refreshes the container list after an action. */
  onReload: () => Promise<void>
  /** onOpenStack navigates to the managed stack address. */
  onOpenStack: (name: string) => void
}

/** ContainersTab is the containers panel: list, stack list and Docker resources. */
export function ContainersTab({
  api,
  terminalApi,
  server,
  list,
  pollMs,
  canStacks,
  canResources,
  onReload,
  onOpenStack,
}: ContainersTabProps) {
  const { t } = useTranslation(['server', 'errors'])
  const [outcomes, setOutcomes] = useState<Record<string, ContainerOutcome>>({})
  const [confirming, setConfirming] = useState<{
    id: string
    action: 'stop' | 'remove'
  } | null>(null)
  const [logsFor, setLogsFor] = useState<string | null>(null)
  const [terminalFor, setTerminalFor] = useState<string | null>(null)
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const setOutcome = useCallback((id: string, outcome: ContainerOutcome | null) => {
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
      await onReload()
    },
    [api, onReload, pollMs, server.id, setOutcome, t],
  )

  const containers = list ? sortContainers(list.containers) : []

  return (
    <>
      {list !== null && !list.connected ? (
        <p className="serverscreen__message">{t('server:offline')}</p>
      ) : null}
      {list !== null && list.connected && !list.dockerAvailable ? (
        <p className="serverscreen__message">{t('server:noDocker')}</p>
      ) : null}
      {list !== null && list.connected && list.dockerAvailable && containers.length === 0 ? (
        <p className="serverscreen__message">{t('server:empty')}</p>
      ) : null}

      {canStacks && list !== null && list.connected && list.dockerAvailable ? (
        <StacksSection
          api={api}
          serverId={server.id}
          containers={list.containers}
          pollMs={pollMs}
          onChanged={() => void onReload()}
          onOpenStack={(name) => onOpenStack(name)}
        />
      ) : null}

      {containers.length > 0 ? (
        <ul className="containers" aria-label={t('server:title')}>
          {containers.map((container) => (
            <ContainerRow
              key={container.id}
              serverId={server.id}
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

      {canResources && list !== null && list.connected && list.dockerAvailable ? (
        <ResourcesSection api={api} serverId={server.id} pollMs={pollMs} />
      ) : null}
    </>
  )
}
