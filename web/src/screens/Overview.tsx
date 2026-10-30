import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Alert, AlertSummary, AlertsApi } from '../api/alerts'
import type { FleetClient } from '../api/client'
import { isSignedOut } from '../api/http'
import type { ActivityEvent, Server } from '../api/types'
import { usePermissions } from '../auth/permissions'
import { PageHeader } from '../components/ui'
import { AddServer } from './fleet/AddServer'
import { ActivityFeed } from './fleet/ActivityFeed'
import { formatTime } from './fleet/format'
import { AttentionList } from './overview/AttentionList'
import { buildAttention } from './overview/attention'
import { ServersMini } from './overview/ServersMini'
import { StatusStrip } from './overview/StatusStrip'

export const refreshInterval = 15_000
const alertsRefreshInterval = 30_000

type OverviewState =
  | { status: 'loading' }
  | { status: 'error' }
  | {
      status: 'ready'
      servers: Server[]
      activity: ActivityEvent[]
      loadedAt: Date
    }

interface OverviewProps {
  client: FleetClient
  alertsApi?: AlertsApi
}

/** Overview is the home screen: what is wrong, what to do and the fleet at a glance. */
export function Overview({ client, alertsApi }: OverviewProps) {
  const { t, i18n } = useTranslation('overview')
  const language = i18n.resolvedLanguage ?? i18n.language
  const { can, canAnywhere } = usePermissions()
  const canAlerts = canAnywhere('alerts.view')
  const canEnroll = can('server.enroll')

  const [state, setState] = useState<OverviewState>({ status: 'loading' })
  const [refreshFailed, setRefreshFailed] = useState(false)
  const [alerts, setAlerts] = useState<Alert[]>([])
  const [alertSummary, setAlertSummary] = useState<AlertSummary | null>(null)

  const loadFleet = useCallback(async () => {
    try {
      const [servers, activity] = await Promise.all([client.listServers(), client.listActivity()])
      setState({ status: 'ready', servers, activity, loadedAt: new Date() })
      setRefreshFailed(false)
    } catch (error) {
      if (isSignedOut(error)) return
      setState((current) => (current.status === 'ready' ? current : { status: 'error' }))
      setRefreshFailed(true)
    }
  }, [client])

  const loadAlerts = useCallback(async () => {
    if (!canAlerts || alertsApi === undefined) return
    try {
      const [active, summary] = await Promise.all([alertsApi.list('active'), alertsApi.summary()])
      setAlerts(active)
      setAlertSummary(summary)
    } catch (error) {
      if (isSignedOut(error)) return
      setAlerts([])
      setAlertSummary(null)
    }
  }, [alertsApi, canAlerts])

  useEffect(() => {
    queueMicrotask(() => {
      void loadFleet()
    })
    const timer = window.setInterval(() => {
      if (!document.hidden) void loadFleet()
    }, refreshInterval)
    return () => window.clearInterval(timer)
  }, [loadFleet])

  useEffect(() => {
    if (!canAlerts || alertsApi === undefined) return
    queueMicrotask(() => {
      void loadAlerts()
    })
    const timer = window.setInterval(() => {
      if (!document.hidden) void loadAlerts()
    }, alertsRefreshInterval)
    return () => window.clearInterval(timer)
  }, [alertsApi, canAlerts, loadAlerts])

  const retry = useCallback(() => {
    setState({ status: 'loading' })
    setRefreshFailed(false)
    void loadFleet()
    void loadAlerts()
  }, [loadAlerts, loadFleet])

  const alertsAvailable = canAlerts && alertsApi !== undefined
  const visibleAlerts = alertsAvailable ? alerts : []
  const visibleAlertSummary = alertsAvailable ? alertSummary : null
  const actions = canEnroll ? <AddServer create={client.createEnrollment} /> : undefined
  const subtitle =
    state.status === 'ready'
      ? t('subtitle', { time: formatTime(state.loadedAt, language) })
      : undefined

  if (state.status === 'loading') {
    return (
      <section className="overview">
        <PageHeader title={t('title')} actions={actions} />
        <p className="ui-sr-only" role="status">
          {t('loading')}
        </p>
        <StatusStrip servers={[]} alertSummary={null} canAlerts={alertsAvailable} loading />
        <div className="overview__cols">
          <div className="overview__main">
            <AttentionList items={[]} alerts={[]} servers={[]} loading />
            <ServersMini servers={[]} alerts={[]} loading />
          </div>
          <div className="overview-panel overview-skeleton-panel" aria-hidden="true">
            <span className="overview-skeleton__line overview-skeleton__line--short" />
            <span className="overview-skeleton__line" />
            <span className="overview-skeleton__line" />
            <span className="overview-skeleton__line overview-skeleton__line--short" />
          </div>
        </div>
      </section>
    )
  }

  if (state.status === 'error') {
    return (
      <section className="overview">
        <PageHeader title={t('title')} actions={actions} />
        <div className="overview-error" role="alert">
          <p>{t('error')}</p>
          <button type="button" className="ui-btn" onClick={retry}>
            {t('retry')}
          </button>
        </div>
      </section>
    )
  }

  const attention = buildAttention(state.servers, visibleAlerts, new Date())

  return (
    <section className="overview">
      <PageHeader title={t('title')} subtitle={subtitle} actions={actions} />
      <StatusStrip
        servers={state.servers}
        alertSummary={visibleAlertSummary}
        canAlerts={alertsAvailable}
      />
      {refreshFailed ? (
        <p className="overview__notice" role="status">
          {t('refreshFailed')}{' '}
          <button type="button" className="overview__notice-retry" onClick={retry}>
            {t('retry')}
          </button>
        </p>
      ) : null}
      <div className="overview__cols">
        <div className="overview__main">
          <AttentionList items={attention} alerts={visibleAlerts} servers={state.servers} />
          <ServersMini servers={state.servers} alerts={visibleAlerts} />
        </div>
        <ActivityFeed events={state.activity} />
      </div>
    </section>
  )
}
