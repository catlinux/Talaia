import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'

import type { ServerSummary } from '../../api/servers'
import type { ServerStatus } from '../../api/types'
import { Menu, StatusBadge, type MenuEntry, type Status } from '../../components/ui'
import { TerminalIcon } from '../../components/icons'
import { useNavigate } from '../../router'
import { formatNumber, formatPercent } from '../fleet/format'
import { ManualUpgrade } from './ManualUpgrade'
import type { OverviewServer } from './OverviewTab'

interface HeaderServer extends OverviewServer {
  id: string
  name: string
  status?: ServerStatus
  hostTerminal?: boolean
  agentUpgradeCommand?: string
}

const statusByServer: Record<ServerStatus, Status> = {
  online: 'ok',
  warning: 'warn',
  critical: 'crit',
  offline: 'offline',
}

interface ServerHeaderProps {
  server: HeaderServer
  serverInfo: ServerSummary | null
  agentVersion: string
  containers: number
  runningContainers: number
  canHostTerminal: boolean
  hostTerminal: boolean
  hostTerminalConnected: boolean
  canUpdateAgent: boolean
  canAgentUpdate: boolean
  canAlerts: boolean
  onOpenHostTerminal: () => void
  onRequestAgentUpdate: () => void
}

function uptimeText(seconds: number, language: string, t: TFunction): string {
  const days = Math.floor(seconds / 86_400)
  const hours = Math.floor((seconds % 86_400) / 3_600)
  if (days > 0) {
    return t('server:overview.uptimeDays', {
      days: formatNumber(days, language),
      hours: formatNumber(hours, language),
    })
  }
  const minutes = Math.floor((seconds % 3_600) / 60)
  if (hours > 0) {
    return t('server:overview.uptimeHours', {
      hours: formatNumber(hours, language),
      minutes: formatNumber(minutes, language),
    })
  }
  return t('server:overview.uptimeMinutes', { minutes: formatNumber(minutes, language) })
}

function HeaderMetric({
  label,
  value,
  language,
  metricLabel,
}: {
  label: string
  value: number | null
  language: string
  metricLabel: string
}) {
  const known = value !== null
  const text = known ? formatPercent(value, language) : '—'
  const tone = known ? (value >= 90 ? 'crit' : value >= 80 ? 'warn' : 'ok') : ''

  return (
    <span className={`serverscreen__metric${tone === '' ? '' : ` serverscreen__metric--${tone}`}`}>
      <span className="serverscreen__metric-label">{label}</span>
      <span
        className="serverscreen__metric-track"
        role="meter"
        aria-label={metricLabel}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={known ? Math.round(value) : undefined}
        aria-valuetext={text}
      >
        <span className="serverscreen__metric-fill" style={{ width: `${known ? value : 0}%` }} />
      </span>
      <span className="serverscreen__metric-value">{text}</span>
    </span>
  )
}

/** ServerHeader is the fixed identity, status, metrics and actions of a server. */
export function ServerHeader({
  server,
  serverInfo,
  agentVersion,
  containers,
  runningContainers,
  canHostTerminal,
  hostTerminal,
  hostTerminalConnected,
  canUpdateAgent,
  canAgentUpdate,
  canAlerts,
  onOpenHostTerminal,
  onRequestAgentUpdate,
}: ServerHeaderProps) {
  const { t, i18n } = useTranslation('server')
  const language = i18n.resolvedLanguage ?? i18n.language
  const navigate = useNavigate()
  const [copied, setCopied] = useState(false)

  const status: Status = serverInfo?.revoked
    ? 'offline'
    : serverInfo !== null && !serverInfo.connected
      ? 'offline'
      : server.status
        ? statusByServer[server.status]
        : serverInfo?.connected
          ? 'ok'
          : 'unknown'

  const meta: Array<{ key: string; value: string }> = []
  if (server.os) meta.push({ key: 'os', value: server.os })
  if (server.kernel) meta.push({ key: 'kernel', value: server.kernel })
  if (server.arch) meta.push({ key: 'arch', value: server.arch })
  if (agentVersion !== '') {
    meta.push({ key: 'agent', value: t('agentVersion', { version: agentVersion }) })
  }
  if ((server.uptimeSeconds ?? 0) > 0) {
    meta.push({
      key: 'uptime',
      value: uptimeText(server.uptimeSeconds ?? 0, language, t),
    })
  }

  const items: MenuEntry[] = []
  if (canUpdateAgent) {
    items.push({
      id: 'update-agent',
      label: t('agentUpdate.button'),
      onSelect: onRequestAgentUpdate,
    })
  }
  if (canAlerts) {
    items.push({
      id: 'view-alerts',
      label: t('header.viewAlerts'),
      onSelect: () => navigate(`/alerts?server=${encodeURIComponent(server.id)}`),
    })
  }
  items.push({
    id: 'copy-id',
    label: t('header.copyId'),
    onSelect: () => {
      const clipboard = navigator.clipboard
      if (!clipboard) {
        setCopied(false)
        return
      }
      void clipboard.writeText(server.id).then(
        () => setCopied(true),
        () => setCopied(false),
      )
    },
  })

  const manualUpgrade =
    canAgentUpdate && (server.agentUpdateManual || serverInfo?.agentUpdateManual)
  const manualCommand = server.agentUpgradeCommand || serverInfo?.agentUpgradeCommand || ''

  return (
    <header className="serverscreen__header">
      <div className="serverscreen__identity">
        <div className="serverscreen__title-row">
          <h1 className="serverscreen__title" id="server-title">
            {server.name}
          </h1>
          <StatusBadge status={status} />
        </div>
        {meta.length > 0 ? (
          <p className="serverscreen__meta-line">
            {meta.map((part, index) => (
              <span key={part.key}>
                {index > 0 ? <span aria-hidden="true"> · </span> : null}
                {part.value}
              </span>
            ))}
          </p>
        ) : null}
        <p className="serverscreen__summary">
          {t('summary', { count: containers, running: runningContainers })}
        </p>
        <div className="serverscreen__metrics">
          <HeaderMetric
            label={t('header.cpu')}
            value={server.cpuPercent ?? null}
            language={language}
            metricLabel={t('header.metric', {
              label: t('header.cpu'),
              value:
                server.cpuPercent === null || server.cpuPercent === undefined
                  ? '—'
                  : formatPercent(server.cpuPercent, language),
            })}
          />
          <HeaderMetric
            label={t('header.ram')}
            value={server.memPercent ?? null}
            language={language}
            metricLabel={t('header.metric', {
              label: t('header.ram'),
              value:
                server.memPercent === null || server.memPercent === undefined
                  ? '—'
                  : formatPercent(server.memPercent, language),
            })}
          />
          <HeaderMetric
            label={t('header.disk')}
            value={server.diskPercent ?? null}
            language={language}
            metricLabel={t('header.metric', {
              label: t('header.disk'),
              value:
                server.diskPercent === null || server.diskPercent === undefined
                  ? '—'
                  : formatPercent(server.diskPercent, language),
            })}
          />
        </div>
      </div>

      <div className="serverscreen__actions">
        {canHostTerminal && hostTerminalConnected ? (
          <button
            type="button"
            className="cbtn cbtn--primary serverscreen__terminal"
            disabled={!hostTerminal}
            title={hostTerminal ? undefined : t('terminal.hostUnavailable')}
            onClick={onOpenHostTerminal}
          >
            <TerminalIcon />
            {t('terminal.hostButton')}
          </button>
        ) : null}
        <Menu
          align="end"
          items={items}
          trigger={(props) => (
            <button
              {...props}
              className="iconbtn serverscreen__menu"
              aria-label={t('header.actions')}
            >
              <span aria-hidden="true">⋯</span>
            </button>
          )}
        />
        {copied ? (
          <span className="serverscreen__copy-notice" role="status">
            {t('header.copied')}
          </span>
        ) : null}
      </div>

      {manualUpgrade ? <ManualUpgrade command={manualCommand} /> : null}
    </header>
  )
}
