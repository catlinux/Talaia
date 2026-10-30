import { useTranslation } from 'react-i18next'

import type { Alert } from '../../api/alerts'
import type { Server, ServerStatus } from '../../api/types'
import { DataTable, StatusBadge, type DataTableColumn, type Status } from '../../components/ui'
import { Link, useNavigate } from '../../router'
import { formatNumber, formatPercent } from '../fleet/format'

interface ServersMiniProps {
  servers: Server[]
  alerts: Alert[]
  loading?: boolean
}

const statusByServer: Record<ServerStatus, Status> = {
  online: 'ok',
  warning: 'warn',
  critical: 'crit',
  offline: 'offline',
}

const severityByServer: Record<ServerStatus, number> = {
  critical: 4,
  warning: 3,
  online: 2,
  offline: 1,
}

function usageTone(value: number): string {
  if (value >= 90) return 'overview-usage--crit'
  if (value >= 80) return 'overview-usage--warn'
  return ''
}

function UsageBar({ label, value }: { label: string; value: number | null }) {
  const { t, i18n } = useTranslation('overview')
  const language = i18n.resolvedLanguage ?? i18n.language
  const known = value !== null
  const text = known ? formatPercent(value, language) : t('servers.unknown')

  return (
    <span className={`overview-usage ${known ? usageTone(value) : ''}`}>
      <span
        className="overview-usage__track"
        role="meter"
        aria-label={t('servers.usage', { label, value: text })}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={known ? Math.round(value) : undefined}
        aria-valuetext={text}
      >
        <span className="overview-usage__fill" style={{ width: `${known ? value : 0}%` }} />
      </span>
      <span className="overview-usage__value">{text}</span>
    </span>
  )
}

/** ServersMini is the compact server table of the Overview. */
export function ServersMini({ servers, alerts, loading = false }: ServersMiniProps) {
  const { t, i18n } = useTranslation('overview')
  const language = i18n.resolvedLanguage ?? i18n.language
  const navigate = useNavigate()

  const sorted = [...servers].sort(
    (left, right) =>
      severityByServer[right.status] - severityByServer[left.status] ||
      left.name.localeCompare(right.name),
  )
  const alertCounts = new Map<string, number>()
  for (const alert of alerts) {
    if (alert.serverId !== null) {
      alertCounts.set(alert.serverId, (alertCounts.get(alert.serverId) ?? 0) + 1)
    }
  }

  const columns: DataTableColumn<Server>[] = [
    {
      id: 'status',
      header: t('servers.status'),
      cell: (server) => <StatusBadge status={statusByServer[server.status]} />,
    },
    {
      id: 'name',
      header: t('servers.name'),
      cell: (server) => <Link to={`/servers/${encodeURIComponent(server.id)}`}>{server.name}</Link>,
    },
    {
      id: 'cpu',
      header: t('servers.cpu'),
      cell: (server) => <UsageBar label={t('servers.cpu')} value={server.cpuPercent} />,
    },
    {
      id: 'ram',
      header: t('servers.ram'),
      cell: (server) => <UsageBar label={t('servers.ram')} value={server.memPercent} />,
    },
    {
      id: 'disk',
      header: t('servers.disk'),
      cell: (server) => <UsageBar label={t('servers.disk')} value={server.diskPercent} />,
    },
    {
      id: 'containers',
      header: t('servers.containers'),
      cell: (server) => formatNumber(server.containers, language),
      align: 'right',
    },
    {
      id: 'alerts',
      header: t('servers.alerts'),
      cell: (server) => formatNumber(alertCounts.get(server.id) ?? 0, language),
      align: 'right',
    },
  ]

  return (
    <section className="overview-panel overview-servers" aria-labelledby="mini-servers-title">
      <header className="overview-panel__head">
        <h2 className="overview-panel__title" id="mini-servers-title">
          {t('servers.title')}
        </h2>
        <Link className="overview-panel__link" to="/servers">
          {t('servers.all')}
        </Link>
      </header>
      <DataTable
        columns={columns}
        rows={sorted}
        rowKey={(server) => server.id}
        loading={loading}
        empty={t('servers.empty')}
        onRowClick={(server) => navigate(`/servers/${encodeURIComponent(server.id)}`)}
      />
    </section>
  )
}
