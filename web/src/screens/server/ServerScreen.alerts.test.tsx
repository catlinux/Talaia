import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import type { AlertsAdminApi } from '../../api/alertsAdmin'
import type { AptApi } from '../../api/apt'
import type { ContainerList, ContainersApi, Job } from '../../api/containers'
import type { JournalApi } from '../../api/journal'
import type { MeApi, MePermissions, Permission } from '../../api/me'
import type { ServiceUnit, ServicesApi } from '../../api/services'
import type { ServersApi } from '../../api/servers'
import { PermissionsProvider } from '../../auth/permissions'
import i18n from '../../i18n'
import { RouterProvider } from '../../router'
import { ServerScreen } from '../ServerScreen'

const server = { id: 'srv-1', name: 'vps-web-01', groupId: 'group-1' }
const job: Job = { id: 'job-1', state: 'succeeded', message: '', output: '' }
const list: ContainerList = { connected: true, dockerAvailable: true, containers: [] }

const containersApi: ContainersApi = {
  list: () => Promise.resolve(list),
  run: () => Promise.resolve('job-1'),
  logs: () => Promise.resolve('job-1'),
  stacks: () =>
    Promise.resolve({ connected: true, stacks: [], adopted: [], broken: [], adoptable: [] }),
  adoptStack: () => Promise.resolve('job-1'),
  listResources: () => Promise.resolve('job-1'),
  removeResources: () => Promise.resolve('job-1'),
  writeStack: () => Promise.resolve('job-1'),
  readStack: () => Promise.resolve('job-1'),
  runStack: () => Promise.resolve('job-1'),
  job: () => Promise.resolve(job),
}

const aptApi: AptApi = {
  status: () => new Promise<string>(() => undefined),
  upgrade: () => Promise.reject(new Error('unexpected apt upgrade')),
  reboot: () => Promise.reject(new Error('unexpected reboot')),
  job: () => Promise.reject(new Error('unexpected apt job')),
}

const servicesApi: ServicesApi = {
  list: () => new Promise<ServiceUnit[]>(() => undefined),
  action: () => Promise.reject(new Error('unexpected service action')),
  job: () => Promise.reject(new Error('unexpected service job')),
}

const journalApi: JournalApi = {
  read: () => new Promise<string>(() => undefined),
  job: () => Promise.reject(new Error('unexpected journal job')),
}

const serversApi: ServersApi = {
  listServers: () =>
    Promise.resolve([
      {
        id: server.id,
        name: server.name,
        connected: true,
        revoked: false,
        agentVersion: '0.2.0',
        agentLatestVersion: '0.2.0',
        agentUpdateAvailable: false,
        agentUpdateManual: false,
        agentUpgradeCommand: '',
      },
    ]),
  removeServer: () => Promise.reject(new Error('unexpected server removal')),
  updateAgent: () => Promise.reject(new Error('unexpected agent update')),
  updateAllAgents: () => Promise.reject(new Error('unexpected agent update')),
}

const unexpected = () => Promise.reject(new Error('unexpected alerts call'))
const alertsApi: AlertsAdminApi = {
  listRules: () => Promise.resolve([]),
  createRule: unexpected,
  updateRule: unexpected,
  deleteRule: () => Promise.reject(new Error('unexpected rule delete')),
  listChecks: () => Promise.resolve([]),
  createCheck: unexpected,
  updateCheck: unexpected,
  deleteCheck: () => Promise.reject(new Error('unexpected check delete')),
  runCheck: unexpected,
  listChannels: () => Promise.resolve([]),
  createChannel: unexpected,
  updateChannel: unexpected,
  deleteChannel: () => Promise.reject(new Error('unexpected channel delete')),
  testChannel: unexpected,
  getSettings: () => Promise.resolve({ allowPrivateTargets: false }),
  updateSettings: (allowPrivateTargets) => Promise.resolve({ allowPrivateTargets }),
}

function me(permissions: string[]): MeApi {
  const scopes: MePermissions = {
    organization: permissions as Permission[],
    servers: {},
    stacks: {},
    containers: {},
  }
  return {
    get: () =>
      Promise.resolve({
        user: { id: 'u1', username: 'alice', display_name: 'Alice', email: '' },
        bindings: { bindings: [] },
        permissions: scopes,
      }),
  }
}

function renderScreen(permissions: string[], tab: string) {
  const suffix = tab === 'overview' ? '' : `/${tab}`
  window.history.replaceState({}, '', `/servers/${server.id}${suffix}`)
  render(
    <RouterProvider>
      <PermissionsProvider meApi={me(permissions)}>
        <ServerScreen
          server={server}
          api={containersApi}
          aptApi={aptApi}
          servicesApi={servicesApi}
          journalApi={journalApi}
          serversApi={serversApi}
          alertsApi={alertsApi}
          pollMs={1}
        />
      </PermissionsProvider>
    </RouterProvider>,
  )
}

describe('ServerScreen alerts tab', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('shows the Alerts tab and remembers it as the selected tab', async () => {
    renderScreen(['server.view', 'alerts.view', 'alerts.manage'], 'alerts')

    const tab = await screen.findByRole('tab', { name: 'Alerts' })
    expect(tab.getAttribute('aria-selected')).toBe('true')
    expect(await screen.findByText('No alert rules apply to this server.')).toBeTruthy()
    expect(window.location.pathname).toBe(`/servers/${server.id}/alerts`)
  })

  it('redirects a forbidden tab to the first permitted tab', async () => {
    renderScreen(['server.view'], 'alerts')

    expect(await screen.findByRole('tab', { name: 'Overview' })).toBeTruthy()
    expect(await screen.findByRole('tabpanel', { name: 'Overview' })).toBeTruthy()
    expect(window.location.pathname).toBe('/servers/srv-1')
  })

  it('hides the tab from users who cannot read alert rules', async () => {
    renderScreen(['server.view'], 'overview')

    expect(await screen.findByRole('tab', { name: 'Overview' })).toBeTruthy()
    expect(screen.queryByRole('tab', { name: 'Alerts' })).toBeNull()
  })
})
