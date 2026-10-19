import { useTranslation } from 'react-i18next'

import type { FleetSummary } from '../../api/types'
import { formatNumber, formatTime } from './format'

interface KpiRowProps {
  summary: FleetSummary
}

export function KpiRow({ summary }: KpiRowProps) {
  const { t, i18n } = useTranslation('fleet')
  const language = i18n.resolvedLanguage ?? i18n.language

  const kpis = [
    {
      id: 'servers-online',
      label: t('kpi.serversOnline'),
      value: t('kpi.serversOnlineValue', {
        online: formatNumber(summary.serversOnline, language),
        total: formatNumber(summary.serversTotal, language),
      }),
      tone: '',
    },
    {
      id: 'containers',
      label: t('kpi.containers'),
      value: formatNumber(summary.runningContainers, language),
      tone: '',
    },
    {
      id: 'security-updates',
      label: t('kpi.securityUpdates'),
      value: formatNumber(summary.securityUpdates, language),
      tone: 'warn',
    },
    {
      id: 'alerts',
      label: t('kpi.alerts'),
      value: formatNumber(summary.activeAlerts, language),
      tone: 'crit',
    },
    {
      id: 'last-backup',
      label: t('kpi.lastBackup'),
      value: summary.lastBackup
        ? formatTime(new Date(summary.lastBackup), language)
        : t('kpi.unknown'),
      tone: '',
    },
  ]

  return (
    <dl className="kpis">
      {kpis.map((kpi) => (
        <div key={kpi.id} className={kpi.tone === '' ? 'kpi' : `kpi kpi--${kpi.tone}`}>
          <dt className="kpi__label">{kpi.label}</dt>
          <dd className="kpi__value">{kpi.value}</dd>
        </div>
      ))}
    </dl>
  )
}
