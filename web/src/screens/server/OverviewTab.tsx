import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'

import type { AptSummary } from '../../api/types'
import { formatBytes, formatNumber, formatPercent } from '../fleet/format'
import { Meter } from '../fleet/Meter'
import { UseGauge } from './UseGauge'
import type { MetricsApi } from '../../api/metrics'
import { HistorySection } from './HistorySection'

/** ServerTab is one tab of the server screen. */
export type ServerTab = 'overview' | 'containers' | 'services' | 'logs' | 'updates'

/** OverviewServer is the live data the overview can show. */
export interface OverviewServer {
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
  apt?: AptSummary
  agentVersion?: string
  agentUpdateAvailable?: boolean
  agentUpdateManual?: boolean
}

interface OverviewTabProps {
  server: OverviewServer
  serverId?: string
  metricsApi?: MetricsApi
  /** The separate props win over the same fields on server. */
  agentVersion?: string
  agentUpdateAvailable?: boolean
  agentUpdateManual?: boolean
  /** failedServices is unknown until the services list has been read. */
  failedServices?: number | null
  /** tabs are the tabs the user may open; attention that points elsewhere is hidden. */
  tabs?: ServerTab[]
  onGoToTab: (tab: ServerTab) => void
}

interface AttentionItem {
  id: string
  text: string
  tab: ServerTab
  action: string
}

/** formatUptime writes a duration in the largest useful units. */
function formatUptime(seconds: number, language: string, t: TFunction): string {
  const days = Math.floor(seconds / 86_400)
  const hours = Math.floor((seconds % 86_400) / 3_600)
  if (days > 0) {
    return t('overview.uptimeDays', {
      days: formatNumber(days, language),
      hours: formatNumber(hours, language),
    })
  }
  const minutes = Math.floor((seconds % 3_600) / 60)
  if (hours > 0) {
    return t('overview.uptimeHours', {
      hours: formatNumber(hours, language),
      minutes: formatNumber(minutes, language),
    })
  }
  return t('overview.uptimeMinutes', { minutes: formatNumber(minutes, language) })
}

/**
 * OverviewTab is the landing summary of a server: its use, its facts and the
 * things that need attention, with a button to the tab that fixes each one.
 */
export function OverviewTab({
  server,
  serverId,
  metricsApi,
  agentVersion,
  agentUpdateAvailable,
  agentUpdateManual,
  failedServices = null,
  tabs = ['overview', 'containers', 'services', 'logs', 'updates'],
  onGoToTab,
}: OverviewTabProps) {
  const { t, i18n } = useTranslation(['server', 'fleet'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const unknown = t('overview.unknown')

  const shownAgentVersion = agentVersion ?? server.agentVersion ?? ''
  const shownUpdateAvailable = agentUpdateAvailable ?? server.agentUpdateAvailable ?? false
  const shownUpdateManual = agentUpdateManual ?? server.agentUpdateManual ?? false
  const cpu = server.cpuPercent ?? null
  const memoryPercentValue = server.memPercent ?? null
  const memoryTotal = server.memTotalBytes ?? (server.memoryGb ?? 0) * 1024 ** 3
  const memoryUsed =
    server.memUsedBytes ??
    (memoryPercentValue !== null && memoryTotal > 0 ? (memoryTotal * memoryPercentValue) / 100 : 0)
  const memoryPercent =
    memoryPercentValue ?? (memoryTotal > 0 ? (memoryUsed / memoryTotal) * 100 : null)
  const memoryValue =
    memoryTotal > 0
      ? `${formatBytes(memoryUsed, language)} / ${formatBytes(memoryTotal, language)}`
      : unknown

  const diskTotal = server.diskTotalBytes ?? 0
  const diskPercentValue = server.diskPercent ?? null
  const diskUsed =
    server.diskUsedBytes ??
    (diskPercentValue !== null && diskTotal > 0 ? (diskTotal * diskPercentValue) / 100 : 0)
  const diskPercent = diskPercentValue ?? (diskTotal > 0 ? (diskUsed / diskTotal) * 100 : null)
  const diskValue =
    diskTotal > 0
      ? `${formatBytes(diskUsed, language)} / ${formatBytes(diskTotal, language)}`
      : unknown

  const containers = server.containers ?? 0
  const runningContainers = server.runningContainers ?? 0
  const services = server.services ?? 0
  const netRx = server.netRxBytes ?? 0
  const netTx = server.netTxBytes ?? 0

  const facts: Array<{ key: string; label: string; value: string }> = [
    { key: 'os', label: t('overview.facts.os'), value: server.os || unknown },
    { key: 'kernel', label: t('overview.facts.kernel'), value: server.kernel || unknown },
    { key: 'arch', label: t('overview.facts.arch'), value: server.arch || unknown },
    {
      key: 'cpus',
      label: t('overview.facts.cpus'),
      value: server.vcpu ? formatNumber(server.vcpu, language) : unknown,
    },
    {
      key: 'memory',
      label: t('overview.facts.memory'),
      value: memoryPercent === null ? unknown : memoryValue,
    },
    {
      key: 'network',
      label: t('overview.facts.network'),
      value: t('overview.networkValue', {
        rx: formatBytes(netRx, language),
        tx: formatBytes(netTx, language),
      }),
    },
    {
      key: 'containers',
      label: t('overview.facts.containers'),
      value: t('overview.containersValue', {
        running: formatNumber(runningContainers, language),
        total: formatNumber(containers, language),
      }),
    },
    {
      key: 'services',
      label: t('overview.facts.services'),
      value: formatNumber(services, language),
    },
    { key: 'agent', label: t('overview.facts.agent'), value: shownAgentVersion || unknown },
    {
      key: 'docker',
      label: t('overview.facts.docker'),
      value: server.dockerVersion || unknown,
    },
  ]
  if ((server.uptimeSeconds ?? 0) > 0) {
    facts.push({
      key: 'uptime',
      label: t('overview.facts.uptime'),
      value: formatUptime(server.uptimeSeconds ?? 0, language, t),
    })
  }

  const attention: AttentionItem[] = []
  if (server.apt?.rebootRequired) {
    attention.push({
      id: 'reboot',
      text: t('overview.attention.reboot'),
      tab: 'updates',
      action: t('overview.attention.goUpdates'),
    })
  }
  if ((server.apt?.security ?? 0) > 0) {
    attention.push({
      id: 'security',
      text: t('overview.attention.security', { count: server.apt?.security ?? 0 }),
      tab: 'updates',
      action: t('overview.attention.goUpdates'),
    })
  }
  if (diskPercent !== null && diskPercent > 85) {
    attention.push({
      id: 'disk',
      text: t('overview.attention.disk', {
        percent: formatPercent(diskPercent, language),
      }),
      tab: 'containers',
      action: t('overview.attention.goContainers'),
    })
  }
  if (failedServices !== null && failedServices > 0) {
    attention.push({
      id: 'services',
      text: t('overview.attention.services', { count: failedServices }),
      tab: 'services',
      action: t('overview.attention.goServices'),
    })
  }
  if (shownUpdateAvailable || shownUpdateManual) {
    attention.push({
      id: 'agent',
      text: t('overview.attention.agent'),
      tab: 'updates',
      action: t('overview.attention.goUpdates'),
    })
  }
  const visibleAttention = attention.filter((item) => tabs.includes(item.tab))

  return (
    <div className="overview">
      <div className="overview__gauges" aria-label={t('overview.gaugesLabel')}>
        <Meter label={t('fleet:metric.cpu')} percent={cpu} />
        <UseGauge
          label={t('fleet:metric.ram')}
          percent={memoryPercent ?? 0}
          value={memoryPercent === null ? unknown : memoryValue}
        />
        <UseGauge
          label={t('fleet:metric.disk')}
          percent={diskPercent ?? 0}
          value={diskPercent === null ? unknown : diskValue}
        />
      </div>

      {metricsApi && serverId ? <HistorySection api={metricsApi} serverId={serverId} /> : null}

      {visibleAttention.length > 0 ? (
        <section className="overview__attention" aria-labelledby="overview-attention-title">
          <h2 className="overview__title" id="overview-attention-title">
            {t('overview.attention.title')}
          </h2>
          <ul className="overview__attention-list">
            {visibleAttention.map((item) => (
              <li key={item.id} className="overview__attention-item">
                <span>{item.text}</span>
                <button type="button" className="cbtn" onClick={() => onGoToTab(item.tab)}>
                  {item.action}
                </button>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      <section className="overview__facts" aria-labelledby="overview-facts-title">
        <h2 className="overview__title" id="overview-facts-title">
          {t('overview.facts.title')}
        </h2>
        <dl className="overview__fact-list">
          {facts.map((fact) => (
            <div key={fact.key} className="overview__fact">
              <dt>{fact.label}</dt>
              <dd>{fact.value}</dd>
            </div>
          ))}
        </dl>
      </section>
    </div>
  )
}
