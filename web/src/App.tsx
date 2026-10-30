import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import type { AptApi } from './api/apt'
import type { AuditApi } from './api/audit'
import { createAlertsApi, type AlertsApi, type AlertSummary } from './api/alerts'
import { createAlertsAdminApi, type AlertsAdminApi } from './api/alertsAdmin'
import { createApiFleetClient, type FleetClient } from './api/client'
import { createContainersApi, type ContainersApi } from './api/containers'
import { createMetricsApi, type MetricsApi } from './api/metrics'
import type { JournalApi } from './api/journal'
import type { UsersApi } from './api/users'
import type { TeamsApi } from './api/teams'
import { createGroupsApi, type GroupsApi } from './api/groups'
import type { InvitesApi } from './api/invites'
import { createServersApi, type ServersApi } from './api/servers'
import type { ServicesApi } from './api/services'
import type { SystemApi } from './api/system'
import type { Server } from './api/types'
import { usePermissions } from './auth/permissions'
import { Sidebar } from './components/shell/Sidebar'
import { Topbar } from './components/shell/Topbar'
import { ToastProvider } from './components/ui'
import { Link, matchRoute, RouterProvider, useNavigate, useRoute } from './router'
import { About } from './screens/About'
import { Audit } from './screens/Audit'
import { Alerts } from './screens/Alerts'
import { Containers } from './screens/Containers'
import { Overview } from './screens/Overview'
import { ServerScreen } from './screens/ServerScreen'
import { Servers } from './screens/Servers'
import { Stacks } from './screens/Stacks'
import { Settings } from './screens/Settings'
import { Users } from './screens/Users'

interface AppProps {
  /** Data source of the fleet screen. Tests inject their own client. */
  client?: FleetClient
  /** Container API of the server screen. Tests inject their own. */
  containersApi?: ContainersApi
  /** Apt API of the server screen. Tests inject their own. */
  aptApi?: AptApi
  /** Services API of the server screen. Tests inject their own. */
  servicesApi?: ServicesApi
  /** Journal API of the server screen. Tests inject their own. */
  journalApi?: JournalApi
  /** Version and update API of the settings screen. Tests inject their own. */
  systemApi?: SystemApi
  /** Audit API of the audit screen. Tests inject their own. */
  auditApi?: AuditApi
  alertsApi?: AlertsApi
  alertsAdminApi?: AlertsAdminApi
  metricsApi?: MetricsApi
  usersApi?: UsersApi
  teamsApi?: TeamsApi
  groupsApi?: GroupsApi
  invitesApi?: InvitesApi
  serversApi?: ServersApi
}

/** App mounts the router, the toast stack and the panel shell. */
export function App(props: AppProps) {
  return (
    <ToastProvider>
      <RouterProvider>
        <PanelShell {...props} />
      </RouterProvider>
    </ToastProvider>
  )
}

function PanelShell({
  client,
  containersApi,
  aptApi,
  servicesApi,
  journalApi,
  systemApi,
  auditApi,
  alertsApi,
  alertsAdminApi,
  metricsApi,
  usersApi,
  teamsApi,
  groupsApi,
  invitesApi,
  serversApi,
}: AppProps) {
  const { t } = useTranslation('common')
  const { can, canAnywhere } = usePermissions()
  const navigate = useNavigate()
  const { path } = useRoute()
  const fleetClient = useMemo(() => client ?? createApiFleetClient(), [client])

  const canAudit = can('audit.view') || can('recordings.view')
  const canUsers = can('users.manage')
  const canAlerts = canAnywhere('alerts.view')
  const canContainers = canAnywhere('container.view')
  const canStacks = canAnywhere('stack.view')

  const alerts = alertsApi ?? defaultAlertsApi
  const alertsAdmin = alertsAdminApi ?? defaultAlertsAdminApi
  const metrics = metricsApi ?? defaultMetricsApi
  const containers = containersApi ?? defaultContainersApi
  const groups = groupsApi ?? defaultGroupsApi
  const servers = serversApi ?? defaultServersApi
  const [alertSummary, setAlertSummary] = useState<AlertSummary | null>(null)
  const refreshAlertSummary = useCallback(() => {
    if (!canAlerts) return
    void alerts.summary().then(setAlertSummary, () => setAlertSummary(null))
  }, [alerts, canAlerts])
  useEffect(() => {
    if (!canAlerts) return
    let active = true
    const load = () => {
      alerts.summary().then(
        (summary) => {
          if (active) setAlertSummary(summary)
        },
        () => {
          if (active) setAlertSummary(null)
        },
      )
    }
    load()
    const timer = window.setInterval(load, 30_000)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [alerts, canAlerts])

  const [mobileOpen, setMobileOpen] = useState(false)
  const [serverNames, setServerNames] = useState<Record<string, string>>({})
  const [serverDetails, setServerDetails] = useState<Record<string, Server>>({})
  const serverMatch = matchRoute('/servers/:id/*', path)
  const serverID = serverMatch?.params.id ?? ''
  const serverName = serverID === '' ? undefined : serverNames[serverID]
  const serverDetail = serverID === '' ? undefined : serverDetails[serverID]

  useEffect(() => {
    if (serverID === '' || serverDetail !== undefined) return
    let active = true
    fleetClient.listServers().then(
      (servers) => {
        if (!active) return
        const found = servers.find((server) => server.id === serverID)
        if (found === undefined) return
        setServerNames((current) =>
          current[serverID] === undefined ? { ...current, [serverID]: found.name } : current,
        )
        setServerDetails((current) =>
          current[serverID] === undefined ? { ...current, [serverID]: found } : current,
        )
      },
      () => undefined,
    )
    return () => {
      active = false
    }
  }, [fleetClient, serverDetail, serverID])

  const openServer = useCallback(
    (id: string, name: string) => {
      setServerNames((current) => ({ ...current, [id]: name }))
      navigate(`/servers/${encodeURIComponent(id)}`)
    },
    [navigate],
  )

  const isServers = matchRoute('/servers', path) !== null
  const isAlerts = matchRoute('/alerts/*', path) !== null
  const isAudit = matchRoute('/audit', path) !== null
  const isUsers = matchRoute('/users/*', path) !== null
  const isSettings = matchRoute('/settings/*', path) !== null
  const isAbout = matchRoute('/about', path) !== null
  const isContainers = matchRoute('/containers', path) !== null
  const isStacks = matchRoute('/stacks', path) !== null

  // Tabs and stacks of one server share the screen state; the URL only changes the panel.
  const pageKey = serverID === '' ? path : `server:${serverID}`

  let page: ReactNode
  if (path === '/') {
    page = <Overview client={fleetClient} alertsApi={canAlerts ? alerts : undefined} />
  } else if (isServers) {
    page = <Servers client={fleetClient} alertsApi={alerts} />
  } else if (serverMatch !== null) {
    page = (
      <ServerScreen
        server={
          serverDetail
            ? { ...serverDetail, name: serverName ?? serverDetail.name }
            : { id: serverID, name: serverName ?? serverID }
        }
        api={containersApi}
        aptApi={aptApi}
        servicesApi={servicesApi}
        journalApi={journalApi}
        serversApi={serversApi}
      />
    )
  } else if (isAlerts) {
    page = canAlerts ? (
      <Alerts
        api={alerts}
        onRefresh={refreshAlertSummary}
        onOpenServer={(id, name) => openServer(id, name)}
        alertsAdminApi={alertsAdmin}
        metricsApi={metrics}
        containersApi={containers}
        serversApi={servers}
        groupsApi={groups}
      />
    ) : (
      <PanelMessage title={t('shell.noAccess')} />
    )
  } else if (isAudit) {
    page = canAudit ? <Audit api={auditApi} /> : <PanelMessage title={t('shell.noAccess')} />
  } else if (isUsers) {
    page = canUsers ? (
      <Users
        usersApi={usersApi}
        teamsApi={teamsApi}
        groupsApi={groupsApi}
        invitesApi={invitesApi}
        serversApi={serversApi}
      />
    ) : (
      <PanelMessage title={t('shell.noAccess')} />
    )
  } else if (isSettings) {
    page = <Settings api={systemApi} />
  } else if (isAbout) {
    page = <About />
  } else if (isContainers) {
    page = canContainers ? (
      <Containers client={fleetClient} containersApi={containersApi} />
    ) : (
      <PanelMessage title={t('shell.noAccess')} />
    )
  } else if (isStacks) {
    page = canStacks ? (
      <Stacks client={fleetClient} containersApi={containersApi} />
    ) : (
      <PanelMessage title={t('shell.noAccess')} />
    )
  } else {
    page = (
      <PanelMessage
        title={t('shell.notFound')}
        body={t('shell.notFoundBody')}
        action={<Link to="/">{t('shell.backHome')}</Link>}
      />
    )
  }

  return (
    <div className="shell">
      <Sidebar
        alertSummary={alertSummary}
        mobileOpen={mobileOpen}
        onClose={() => setMobileOpen(false)}
      />
      <div className="shell__content">
        <Topbar
          alertSummary={alertSummary}
          alertsApi={alerts}
          client={fleetClient}
          containersApi={containers}
          serverName={serverID === '' ? undefined : (serverName ?? serverDetail?.name ?? serverID)}
          onOpenMenu={() => setMobileOpen(true)}
        />
        <main className="shell__main">
          <div className="shell__page" key={pageKey}>
            {page}
          </div>
        </main>
      </div>
    </div>
  )
}

function PanelMessage({
  title,
  body,
  action,
}: {
  title: string
  body?: string
  action?: ReactNode
}) {
  return (
    <section className="shell__message">
      <h1 className="shell__message-title">{title}</h1>
      {body !== undefined ? <p className="shell__message-body">{body}</p> : null}
      {action}
    </section>
  )
}

const defaultAlertsApi = createAlertsApi()
const defaultAlertsAdminApi = createAlertsAdminApi()
const defaultMetricsApi = createMetricsApi()
const defaultContainersApi = createContainersApi()
const defaultGroupsApi = createGroupsApi()
const defaultServersApi = createServersApi()
