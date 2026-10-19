import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { createMockFleetClient } from '../api/client'
import type { FleetClient } from '../api/client'
import type { ActivityEvent, FleetSummary, Server } from '../api/types'
import { ActivityFeed } from './fleet/ActivityFeed'
import { KpiRow } from './fleet/KpiRow'
import { ServerCard } from './fleet/ServerCard'
import { formatTime } from './fleet/format'

interface FleetProps {
  /** Data source. Tests inject their own client; the panel uses the mock. */
  client?: FleetClient
}

type FleetState =
  | { status: 'loading' }
  | { status: 'ready'; servers: Server[]; activity: ActivityEvent[]; loadedAt: Date }
  | { status: 'error' }

const defaultClient = createMockFleetClient()

/**
 * Derives the KPI row from the two lists the client returns: the server status,
 * the container count, the security updates reported in the activity feed, the
 * events that need attention and the latest successful backup.
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

  let securityUpdates = 0
  let activeAlerts = 0
  let lastBackup: string | null = null
  for (const event of activity) {
    if (event.messageKey === 'event.securityUpdates') {
      securityUpdates += Number(event.params.count ?? 0)
    }
    if (event.severity === 'warning' || event.severity === 'critical') {
      activeAlerts += 1
    }
    if (event.messageKey === 'event.backupDone' && (lastBackup === null || event.at > lastBackup)) {
      lastBackup = event.at
    }
  }

  return {
    serversOnline,
    serversTotal: servers.length,
    runningContainers,
    securityUpdates,
    activeAlerts,
    lastBackup,
  }
}

export function Fleet({ client = defaultClient }: FleetProps) {
  const { t, i18n } = useTranslation('fleet')
  const language = i18n.resolvedLanguage ?? i18n.language
  const [state, setState] = useState<FleetState>({ status: 'loading' })

  useEffect(() => {
    let cancelled = false

    Promise.all([client.listServers(), client.listActivity()])
      .then(([servers, activity]) => {
        if (!cancelled) {
          setState({ status: 'ready', servers, activity, loadedAt: new Date() })
        }
      })
      .catch(() => {
        if (!cancelled) {
          setState({ status: 'error' })
        }
      })

    return () => {
      cancelled = true
    }
  }, [client])

  return (
    <section className="fleet" aria-labelledby="fleet-title">
      <header className="fleet__head">
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

      {state.status === 'ready' ? (
        <>
          <KpiRow summary={summarizeFleet(state.servers, state.activity)} />
          <div className="fleet__cols">
            <ul className="servers">
              {state.servers.map((server) => (
                <ServerCard key={server.id} server={server} />
              ))}
            </ul>
            <ActivityFeed events={state.activity} />
          </div>
        </>
      ) : null}
    </section>
  )
}
