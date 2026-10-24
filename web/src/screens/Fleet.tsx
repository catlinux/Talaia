import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { AlertsApi, AlertSummary } from '../api/alerts'
import { createApiFleetClient } from '../api/client'
import type { FleetClient } from '../api/client'
import { isSignedOut } from '../api/http'
import { usePermissions } from '../auth/permissions'
import type { ActivityEvent, FleetSummary, Server } from '../api/types'
import { ActivityFeed } from './fleet/ActivityFeed'
import { WatchtowerArt } from '../components/WatchtowerArt'
import { AddServer } from './fleet/AddServer'
import { KpiRow } from './fleet/KpiRow'
import { ServerCard } from './fleet/ServerCard'
import { formatTime } from './fleet/format'

/** refreshInterval is how often the fleet is reloaded while the tab is visible. */
export const refreshInterval = 15_000

interface FleetProps {
  /** Data source. Tests inject their own client; the panel uses the API. */
  client?: FleetClient
  /** onOpenServer opens the screen of a server; the cards are not links without it. */
  onOpenServer?: (server: Server) => void
  /** alertsApi supplies the alert summary and per-server counts when available. */
  alertsApi?: AlertsApi
}

type FleetState =
  | { status: 'loading' }
  | { status: 'ready'; servers: Server[]; activity: ActivityEvent[]; loadedAt: Date }
  | { status: 'error' }

const defaultClient = createApiFleetClient()

/**
 * Derives the KPI row from the two lists the client returns. Security updates
 * and the last backup still come from the activity feed; active alerts come
 * from the alert summary and stay null when it is unavailable.
 */
function summarizeFleet(servers: Server[], activity: ActivityEvent[]): FleetSummary {
  let serversOnline = 0
  let runningContainers = 0
  for (const server of servers) {
    if (server.status !== 'offline') {
      serversOnline += 1
    }
    runningContainers += server.containers
  }

  const summary: FleetSummary = {
    serversOnline,
    serversTotal: servers.length,
    runningContainers,
    securityUpdates: null,
    activeAlerts: null,
    lastBackup: null,
  }
  if (activity.length === 0) {
    return summary
  }

  summary.securityUpdates = 0
  for (const event of activity) {
    if (event.messageKey === 'event.securityUpdates') {
      summary.securityUpdates += Number(event.params.count ?? 0)
    }
    if (
      event.messageKey === 'event.backupDone' &&
      (summary.lastBackup === null || event.at > summary.lastBackup)
    ) {
      summary.lastBackup = event.at
    }
  }
  return summary
}

export function Fleet({ client = defaultClient, onOpenServer, alertsApi }: FleetProps) {
  const { t, i18n } = useTranslation('fleet')
  const language = i18n.resolvedLanguage ?? i18n.language
  const { can } = usePermissions()
  const canEnroll = can('server.enroll')
  const [state, setState] = useState<FleetState>({ status: 'loading' })
  const [refreshFailed, setRefreshFailed] = useState(false)
  const [alertSummary, setAlertSummary] = useState<AlertSummary | null>(null)
  const [alertCounts, setAlertCounts] = useState<Record<string, number>>({})

  useEffect(() => {
    if (!alertsApi) return
    let active = true
    const loadAlerts = () => {
      alertsApi.summary().then(
        (summary) => {
          if (active) setAlertSummary(summary)
        },
        () => {
          if (active) setAlertSummary(null)
        },
      )
      alertsApi.list('active').then(
        (alerts) => {
          if (!active) return
          const counts: Record<string, number> = {}
          for (const alert of alerts) {
            if (alert.serverId) counts[alert.serverId] = (counts[alert.serverId] ?? 0) + 1
          }
          setAlertCounts(counts)
        },
        () => {
          if (active) setAlertCounts({})
        },
      )
    }
    loadAlerts()
    const timer = window.setInterval(loadAlerts, 30_000)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [alertsApi])

  useEffect(() => {
    let cancelled = false

    async function load() {
      try {
        const [servers, activity] = await Promise.all([client.listServers(), client.listActivity()])
        if (cancelled) {
          return
        }
        setState({ status: 'ready', servers, activity, loadedAt: new Date() })
        setRefreshFailed(false)
      } catch (error) {
        if (cancelled) {
          return
        }
        // A signed-out answer is handled by the AuthGate.
        if (isSignedOut(error)) {
          return
        }
        // Keep the last data on screen; only a failed first load replaces it.
        setState((current) => (current.status === 'ready' ? current : { status: 'error' }))
        setRefreshFailed(true)
      }
    }

    void load()

    const timer = setInterval(() => {
      if (!document.hidden) {
        void load()
      }
    }, refreshInterval)

    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [client])

  return (
    <section className="fleet" aria-labelledby="fleet-title">
      <header className="fleet__head">
        <div className="fleet__titles">
          <h1 className="fleet__title" id="fleet-title">
            {t('title')}
          </h1>
          {state.status === 'ready' ? (
            <p className="fleet__subtitle">
              {t('subtitle', {
                count: state.servers.length,
                time: formatTime(state.loadedAt, language),
              })}
            </p>
          ) : null}
        </div>
        {canEnroll ? (
          <div className="fleet__actions">
            <AddServer create={client.createEnrollment} />
          </div>
        ) : null}
      </header>

      {state.status === 'loading' ? (
        <p className="fleet__message" role="status">
          {t('loading')}
        </p>
      ) : null}

      {state.status === 'error' ? (
        <p className="fleet__message fleet__message--error" role="alert">
          {t('error')}
        </p>
      ) : null}

      {state.status === 'ready' && refreshFailed ? (
        <p className="fleet__notice" role="status">
          {t('refreshFailed')}
        </p>
      ) : null}

      {state.status === 'ready' && state.servers.length === 0 ? (
        <div className="fleet__empty">
          <WatchtowerArt className="fleet__empty-art" />
          <p className="fleet__empty-title">{t('empty.title')}</p>
          <p>{t('empty.body')}</p>
        </div>
      ) : null}

      {state.status === 'ready' && state.servers.length > 0 ? (
        <>
          <KpiRow
            summary={{
              ...summarizeFleet(state.servers, state.activity),
              activeAlerts:
                alertSummary === null
                  ? null
                  : (alertSummary.counts.warning ?? 0) + (alertSummary.counts.critical ?? 0),
            }}
            alertSeverity={
              alertSummary === null
                ? undefined
                : (alertSummary.counts.critical ?? 0) > 0
                  ? 'critical'
                  : (alertSummary.counts.warning ?? 0) > 0
                    ? 'warning'
                    : ''
            }
          />
          <div
            className={
              state.activity.length > 0 ? 'fleet__cols' : 'fleet__cols fleet__cols--single'
            }
          >
            <ul className="servers">
              {state.servers.map((server) => (
                <ServerCard
                  key={server.id}
                  server={server}
                  onOpen={onOpenServer}
                  alertSeverity={alertSummary?.servers[server.id]}
                  alertCount={alertSummary ? (alertCounts[server.id] ?? 0) : 0}
                />
              ))}
            </ul>
            {state.activity.length > 0 ? <ActivityFeed events={state.activity} /> : null}
          </div>
        </>
      ) : null}
    </section>
  )
}
