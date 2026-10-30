import { useCallback, useEffect, useMemo, useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { createGroupsApi, type Group, type GroupsApi } from '../api/groups'
import { createInvitesApi, type Invite, type InvitesApi } from '../api/invites'
import { createServersApi, type ServerSummary, type ServersApi } from '../api/servers'
import { createTeamsApi, type Team, type TeamsApi } from '../api/teams'
import { createUsersApi, type AdminUser, type UsersApi } from '../api/users'
import { errorMessage } from '../auth/AuthGate'
import { usePermissions } from '../auth/permissions'
import { PageHeader } from '../components/ui'
import { Link, useNavigate, useRoute } from '../router'
import { GroupsTab } from './users/GroupsTab'
import { TeamsTab } from './users/TeamsTab'
import { UsersTab } from './users/UsersTab'

type Tab = 'users' | 'teams' | 'groups'

const tabs: Tab[] = ['users', 'teams', 'groups']

/** usersTabPath returns the address of a Users section. */
export function usersTabPath(tab: Tab): string {
  return tab === 'users' ? '/users' : `/users/${tab}`
}

function tabFromPath(path: string): Tab | null {
  if (path === '/users') return 'users'
  if (path === '/users/teams') return 'teams'
  if (path === '/users/groups') return 'groups'
  return null
}

interface UsersProps {
  usersApi?: UsersApi
  teamsApi?: TeamsApi
  groupsApi?: GroupsApi
  invitesApi?: InvitesApi
  serversApi?: ServersApi
}

interface Data {
  users: AdminUser[]
  teams: Team[]
  groups: Group[]
  invites: Invite[]
  servers: ServerSummary[]
}

/** Users is the Users and permissions screen of the panel. */
export function Users({ usersApi, teamsApi, groupsApi, invitesApi, serversApi }: UsersProps) {
  const { t } = useTranslation(['users', 'errors'])
  const { can, loading: permissionsLoading } = usePermissions()
  const usersClient = useMemo(() => usersApi ?? createUsersApi(), [usersApi])
  const teamsClient = useMemo(() => teamsApi ?? createTeamsApi(), [teamsApi])
  const groupsClient = useMemo(() => groupsApi ?? createGroupsApi(), [groupsApi])
  const invitesClient = useMemo(() => invitesApi ?? createInvitesApi(), [invitesApi])
  const serversClient = useMemo(() => serversApi ?? createServersApi(), [serversApi])
  const { path } = useRoute()
  const navigate = useNavigate()
  const [data, setData] = useState<Data | null>(null)
  const [error, setError] = useState<string | null>(null)
  const tab = tabFromPath(path)

  const refresh = useCallback(async () => {
    const [users, teams, groups, invites, servers] = await Promise.all([
      usersClient.list(),
      teamsClient.list(),
      groupsClient.list(),
      invitesClient.list(),
      serversClient.listServers(),
    ])
    setData({ users, teams, groups, invites, servers })
  }, [groupsClient, invitesClient, serversClient, teamsClient, usersClient])

  useEffect(() => {
    if (permissionsLoading || !can('users.manage')) return
    let active = true
    queueMicrotask(() => {
      refresh().then(
        () => {
          if (active) setError(null)
        },
        (cause: unknown) => {
          if (active) setError(errorMessage(t, cause))
        },
      )
    })
    return () => {
      active = false
    }
  }, [can, permissionsLoading, refresh, t])

  const canManage = !permissionsLoading && can('users.manage')
  const activeTab: Tab = tab ?? 'users'

  useEffect(() => {
    if (!permissionsLoading && tab === null) navigate('/users', { replace: true })
  }, [navigate, permissionsLoading, tab])

  const moveTab = (event: KeyboardEvent<HTMLAnchorElement>, index: number) => {
    let next: number
    switch (event.key) {
      case 'ArrowLeft':
        next = (index - 1 + tabs.length) % tabs.length
        break
      case 'ArrowRight':
        next = (index + 1) % tabs.length
        break
      case 'Home':
        next = 0
        break
      case 'End':
        next = tabs.length - 1
        break
      default:
        return
    }
    event.preventDefault()
    const target = tabs[next]!
    navigate(usersTabPath(target))
    document.getElementById(`users-tab-${target}`)?.focus()
  }

  const header = <PageHeader title={t('users:pageTitle')} subtitle={t('users:pageSubtitle')} />

  if (permissionsLoading) {
    return (
      <section className="users" aria-label={t('users:pageTitle')}>
        {header}
        <p className="users__message" role="status">
          {t('users:loading')}
        </p>
      </section>
    )
  }
  if (!canManage) return null
  if (data === null) {
    return (
      <section className="users" aria-label={t('users:pageTitle')}>
        {header}
        {error !== null ? (
          <p className="users__error" role="alert">
            {error}
          </p>
        ) : (
          <p className="users__message" role="status">
            {t('users:loading')}
          </p>
        )}
      </section>
    )
  }

  return (
    <section className="users" aria-label={t('users:pageTitle')}>
      {header}

      {error !== null ? (
        <p className="users__error" role="alert">
          {error}
        </p>
      ) : null}

      <div className="users__tabs" role="tablist" aria-label={t('users:tabs.label')}>
        {tabs.map((candidate, index) => {
          const selected = candidate === activeTab
          return (
            <Link
              key={candidate}
              id={`users-tab-${candidate}`}
              to={usersTabPath(candidate)}
              role="tab"
              aria-selected={selected}
              aria-controls={`users-panel-${candidate}`}
              tabIndex={selected ? 0 : -1}
              className={selected ? 'users__tab users__tab--on' : 'users__tab'}
              onKeyDown={(event) => moveTab(event, index)}
            >
              {t(`users:tabs.${candidate}`)}
            </Link>
          )
        })}
      </div>

      <div
        className="users__panel"
        role="tabpanel"
        id={`users-panel-${activeTab}`}
        aria-labelledby={`users-tab-${activeTab}`}
      >
        {activeTab === 'users' ? (
          <UsersTab
            users={data.users}
            groups={data.groups}
            servers={data.servers}
            invites={data.invites}
            usersApi={usersClient}
            invitesApi={invitesClient}
            onRefresh={refresh}
          />
        ) : activeTab === 'teams' ? (
          <TeamsTab
            teams={data.teams}
            users={data.users}
            groups={data.groups}
            servers={data.servers}
            teamsApi={teamsClient}
            usersApi={usersClient}
            onRefresh={refresh}
          />
        ) : (
          <GroupsTab
            groups={data.groups}
            servers={data.servers}
            groupsApi={groupsClient}
            onRefresh={refresh}
          />
        )}
      </div>
    </section>
  )
}
