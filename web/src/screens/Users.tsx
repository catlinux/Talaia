import { useCallback, useEffect, useMemo, useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { createGroupsApi, type Group, type GroupsApi } from '../api/groups'
import { createInvitesApi, type Invite, type InvitesApi } from '../api/invites'
import { createServersApi, type ServerSummary, type ServersApi } from '../api/servers'
import { createTeamsApi, type Team, type TeamsApi } from '../api/teams'
import { createUsersApi, type AdminUser, type UsersApi } from '../api/users'
import { errorMessage } from '../auth/AuthGate'
import { usePermissions } from '../auth/permissions'
import { GroupsTab } from './users/GroupsTab'
import { TeamsTab } from './users/TeamsTab'
import { UsersTab } from './users/UsersTab'

type Tab = 'users' | 'teams' | 'groups'

const tabs: Tab[] = ['users', 'teams', 'groups']

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
  const [tab, setTab] = useState<Tab>('users')
  const [data, setData] = useState<Data | null>(null)
  const [error, setError] = useState<string | null>(null)

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
  if (permissionsLoading) {
    return (
      <p className="users__message" role="status">
        {t('users:loading')}
      </p>
    )
  }
  if (!canManage) return null
  if (error !== null && data === null) {
    return (
      <p className="users__error" role="alert">
        {error}
      </p>
    )
  }
  if (data === null) {
    return (
      <p className="users__message" role="status">
        {t('users:loading')}
      </p>
    )
  }

  const moveTab = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
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
    setTab(tabs[next]!)
    document.getElementById(`users-tab-${tabs[next]}`)?.focus()
  }

  return (
    <section className="users" aria-labelledby="users-title">
      <header className="users__head">
        <h1 className="users__title" id="users-title">
          {t('users:pageTitle')}
        </h1>
        <p className="users__subtitle">{t('users:pageSubtitle')}</p>
      </header>

      {error !== null ? (
        <p className="users__error" role="alert">
          {error}
        </p>
      ) : null}

      <div className="users__tabs" role="tablist" aria-label={t('users:tabs.label')}>
        {tabs.map((candidate, index) => {
          const selected = candidate === tab
          return (
            <button
              key={candidate}
              id={`users-tab-${candidate}`}
              type="button"
              role="tab"
              aria-selected={selected}
              aria-controls={`users-panel-${candidate}`}
              tabIndex={selected ? 0 : -1}
              className={selected ? 'users__tab users__tab--on' : 'users__tab'}
              onClick={() => setTab(candidate)}
              onKeyDown={(event) => moveTab(event, index)}
            >
              {t(`users:tabs.${candidate}`)}
            </button>
          )
        })}
      </div>

      <div
        className="users__panel"
        role="tabpanel"
        id={`users-panel-${tab}`}
        aria-labelledby={`users-tab-${tab}`}
      >
        {tab === 'users' ? (
          <UsersTab
            users={data.users}
            groups={data.groups}
            servers={data.servers}
            invites={data.invites}
            usersApi={usersClient}
            invitesApi={invitesClient}
            onRefresh={refresh}
          />
        ) : tab === 'teams' ? (
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
