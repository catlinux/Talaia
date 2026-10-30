import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import type { Container, ContainerAction } from '../../api/containers'
import { usePermissions } from '../../auth/permissions'
import {
  LogsIcon,
  PlayIcon,
  RestartIcon,
  StopIcon,
  TerminalIcon,
  TrashIcon,
} from '../../components/icons'
import { formatBytes, formatPercent } from '../fleet/format'
import { UseGauge } from './UseGauge'

/** ContainerOutcome is what the tab tells about the last action of a container. */
export type ContainerOutcome =
  | { kind: 'working'; action: ContainerAction }
  | { kind: 'done'; action: ContainerAction }
  | { kind: 'failed'; message: string }

const actionIcons = {
  start: PlayIcon,
  stop: StopIcon,
  restart: RestartIcon,
  remove: TrashIcon,
} as const

interface ContainerRowProps {
  serverId: string
  container: Container
  outcome: ContainerOutcome | undefined
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

/** ContainerRow is one container of the server, with its actions and inline panels. */
export function ContainerRow({
  serverId,
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
  const { can } = usePermissions()
  const busy = outcome?.kind === 'working'
  const isRunning = container.state === 'running'
  const scope = {
    serverId,
    stack: container.composeProject || undefined,
    container: container.name,
  }
  const canControl = can('container.control', scope)
  const canRemove = can('container.remove', scope)
  const canLogs = can('container.logs', scope)
  const canTerminal = can('container.terminal', scope)
  const actions: ContainerAction[] = (isRunning ? ['restart', 'stop'] : ['start']).filter(
    () => canControl,
  ) as ContainerAction[]
  if (canRemove) actions.push('remove')
  const hasActions = canLogs || (canTerminal && isRunning) || actions.length > 0

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
      ) : hasActions ? (
        <div className="container__actions">
          {canLogs ? (
            <button
              type="button"
              className="cbtn"
              aria-expanded={logs !== null}
              onClick={onToggleLogs}
            >
              <LogsIcon />
              {t('action.logs')}
            </button>
          ) : null}
          {canTerminal && isRunning ? (
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
      ) : null}
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
