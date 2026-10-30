import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import type { AlertSummary } from '../../api/alerts'
import type { Server } from '../../api/types'
import { Link } from '../../router'
import { formatNumber } from '../fleet/format'

interface StatusStripProps {
  servers: Server[]
  alertSummary: AlertSummary | null
  canAlerts: boolean
  loading?: boolean
}

function SkeletonCell() {
  return (
    <div className="overview-strip__cell overview-strip__cell--skeleton" aria-hidden="true">
      <span className="overview-skeleton__line overview-skeleton__line--short" />
      <span className="overview-skeleton__line overview-skeleton__line--value" />
    </div>
  )
}

function Cell({
  to,
  label,
  value,
  detail,
}: {
  to: string
  label: string
  value: ReactNode
  detail?: ReactNode
}) {
  return (
    <Link to={to} className="overview-strip__cell">
      <span className="overview-strip__label">{label}</span>
      <span className="overview-strip__value">{value}</span>
      {detail !== undefined ? <span className="overview-strip__detail">{detail}</span> : null}
    </Link>
  )
}

/** StatusStrip is the four-cell summary at the top of the Overview. */
export function StatusStrip({
  servers,
  alertSummary,
  canAlerts,
  loading = false,
}: StatusStripProps) {
  const { t, i18n } = useTranslation('overview')
  const language = i18n.resolvedLanguage ?? i18n.language

  if (loading) {
    return (
      <div className="overview-strip" aria-busy="true">
        <SkeletonCell />
        <SkeletonCell />
        <SkeletonCell />
        <SkeletonCell />
      </div>
    )
  }

  const online = servers.filter((server) => server.status !== 'offline').length
  const offline = servers.length - online
  const security = servers.reduce((total, server) => total + (server.apt?.security ?? 0), 0)
  const securityServers = servers.filter((server) => (server.apt?.security ?? 0) > 0).length
  const reboots = servers.filter((server) => server.apt?.rebootRequired === true).length

  const critical = alertSummary?.counts.critical ?? 0
  const warning = alertSummary?.counts.warning ?? 0

  let alerts: ReactNode
  if (!canAlerts || alertSummary === null) {
    alerts = <span className="overview-strip__muted">{t('strip.alertsUnavailable')}</span>
  } else if (critical === 0 && warning === 0) {
    alerts = <span className="overview-strip__ok">{t('strip.alertsNone')}</span>
  } else {
    alerts = (
      <>
        {critical > 0 ? (
          <span className="overview-strip__badge overview-strip__badge--crit">
            {t('strip.critical', { count: critical })}
          </span>
        ) : null}
        {warning > 0 ? (
          <span className="overview-strip__badge overview-strip__badge--warn">
            {t('strip.warning', { count: warning })}
          </span>
        ) : null}
      </>
    )
  }

  return (
    <div className="overview-strip">
      <Cell
        to="/servers?status=offline"
        label={t('strip.servers')}
        value={t('strip.serversValue', {
          online: formatNumber(online, language),
          total: formatNumber(servers.length, language),
        })}
        detail={
          <span className={offline > 0 ? 'overview-strip__crit' : 'overview-strip__muted'}>
            {t('strip.offline', { count: offline })}
          </span>
        }
      />
      <Cell
        to="/alerts"
        label={t('strip.alerts')}
        value={<span className="overview-strip__badges">{alerts}</span>}
      />
      <Cell
        to="/servers?updates=security"
        label={t('strip.security')}
        value={formatNumber(security, language)}
        detail={
          securityServers > 0 ? t('strip.securityServers', { count: securityServers }) : undefined
        }
      />
      <Cell
        to="/servers?reboot=1"
        label={t('strip.reboots')}
        value={formatNumber(reboots, language)}
      />
    </div>
  )
}
