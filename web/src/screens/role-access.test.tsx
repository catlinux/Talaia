import type { ReactNode } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import type { AptApi } from '../api/apt'
import type { FleetClient } from '../api/client'
import type { ContainerList, ContainersApi } from '../api/containers'
import type { JournalApi } from '../api/journal'
import type { MeApi, MePermissions, MeResponse, Permission } from '../api/me'
import type { ServiceUnit, ServicesApi } from '../api/services'
import type { ServerSummary, ServersApi } from '../api/servers'
import type { Server } from '../api/types'
import { PermissionsProvider } from '../auth/permissions'
import { App } from '../App'
import i18n from '../i18n'
import { Fleet } from './Fleet'
import { ServerScreen } from './ServerScreen'

const allPermissions: Permission[] = [
  'container.view',
  'container.logs',
  'container.control',
  'container.terminal',
  'container.remove',
  'stack.view',
  'stack.deploy',
  'stack.edit',
  'server.view',
  'server.remove',
  'server.enroll',
  'server.agent_update',
  'resources.manage',
  'host.services.view',
  'host.services.control',
  'host.journal',
  'host.apt.view',
  'host.apt.upgrade',
  'host.reboot',
  'host.terminal',
  'users.manage',
  'settings.manage',
  'audit.view',
  'recordings.view',
  'apikeys.manage',
]

function meApi(permissions: Partial<MePermissions>): MeApi {
  const body: MeResponse = {
    user: { id: 'u1', username: 'alice', display_name: 'Alice', email: '' },
    bindings: { bindings: [] },
    permissions: {
      organization: [],
      servers: {},
      stacks: {},
      containers: {},
      ...permissions,
    },
  }
  return { get: () => Promise.resolve(body) }
}

function mount(me: MeApi, child: ReactNode) {
  return render(<PermissionsProvider meApi={me}>{child}</PermissionsProvider>)
}

function serverSummary(overrides: Partial<ServerSummary> = {}): ServerSummary {
  return {
    id: 'srv-1',
    name: 'vps-web-01',
    connected: true,
    revoked: false,
    agentVersion: '0.2.0',
    agentLatestVersion: '0.2.8',
    agentUpdateAvailable: true,
    agentUpdateManual: false,
    agentUpgradeCommand: '',
    hostTerminal: true,
    ...overrides,
  }
}

function fleetServer(limited = false): Server {
  return {
    id: 'srv-1',
    name: 'vps-web-01',
    os: 'Debian GNU/Linux 13',
    location: '',
    vcpu: 4,
    memoryGb: 8,
    status: 'online',
    cpuPercent: 12,
    memPercent: 20,
    diskPercent: 10,
    containers: 1,
    stacks: 1,
    tags: [],
    lastSeen: '2026-10-19T12:00:00Z',
    agentVersion: '0.2.0',
    agentLatestVersion: '0.2.8',
    agentUpdateAvailable: true,
    agentUpdateManual: false,
    agentUpgradeCommand: '',
    hostTerminal: true,
    ...(limited ? { limited: true } : {}),
  }
}

function fleetClient(server: Server): FleetClient {
  return {
    listServers: () => Promise.resolve([server]),
    listActivity: () => Promise.resolve([]),
    createEnrollment: () => Promise.reject(new Error('unexpected enrollment')),
  }
}

function containersApi(): ContainersApi {
  const list: ContainerList = {
    connected: true,
    dockerAvailable: true,
    containers: [
      {
        id: 'ct-1',
        name: 'web-1',
        image: 'nginx:1.27',
        state: 'running',
        status: 'Up 2 hours',
        composeProject: 'web',
        composeService: 'web',
      },
    ],
  }
  const unexpected = (): Promise<never> => Promise.reject(new Error('unexpected call'))
  return {
    list: () => Promise.resolve(list),
    run: unexpected,
    stacks: () =>
      Promise.resolve({ connected: true, stacks: ['web'], adopted: [], broken: [], adoptable: [] }),
    writeStack: unexpected,
    readStack: unexpected,
    listResources: unexpected,
    removeResources: unexpected,
    adoptStack: unexpected,
    runStack: unexpected,
    logs: unexpected,
    job: unexpected,
  }
}

function serversApi(): ServersApi {
  const unexpected = (): Promise<never> => Promise.reject(new Error('unexpected call'))
  return {
    listServers: () => Promise.resolve([serverSummary()]),
    removeServer: unexpected,
    updateAgent: unexpected,
    updateAllAgents: unexpected,
  }
}

const pendingServices: ServicesApi = {
  list: () => new Promise<ServiceUnit[]>(() => undefined),
  action: () => Promise.reject(new Error('unexpected service action')),
  job: () => Promise.reject(new Error('unexpected service job')),
}

const pendingJournal: JournalApi = {
  read: () => new Promise<string>(() => undefined),
  job: () => Promise.reject(new Error('unexpected journal job')),
}

const pendingApt: AptApi = {
  status: () => new Promise<string>(() => undefined),
  upgrade: () => Promise.reject(new Error('unexpected apt upgrade')),
  reboot: () => Promise.reject(new Error('unexpected reboot')),
  job: () => Promise.reject(new Error('unexpected apt job')),
}

function serverScreen(me: MeApi, limited = false) {
  return mount(
    me,
    <ServerScreen
      server={{ id: 'srv-1', name: 'vps-web-01', limited }}
      onBack={() => undefined}
      api={containersApi()}
      serversApi={serversApi()}
      servicesApi={pendingServices}
      journalApi={pendingJournal}
      aptApi={pendingApt}
    />,
  )
}

describe('role-based panel controls', () => {
  beforeEach(async () => {
    sessionStorage.clear()
    await i18n.changeLanguage('en')
  })

  afterEach(() => cleanup())

  it('admin sees every tab and action', async () => {
    serverScreen(meApi({ organization: allPermissions }))

    for (const tab of ['Overview', 'Containers', 'Services', 'Logs', 'Updates']) {
      expect(await screen.findByRole('tab', { name: tab })).toBeTruthy()
    }
    expect(screen.getAllByRole('button', { name: 'Terminal' }).length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: 'Update agent' })).toBeTruthy()
  })

  it('viewer on a server sees no change actions', async () => {
    serverScreen(
      meApi({
        servers: {
          'srv-1': [
            'server.view',
            'container.view',
            'container.logs',
            'stack.view',
            'host.services.view',
            'host.journal',
            'host.apt.view',
          ],
        },
      }),
    )

    expect(await screen.findByRole('tab', { name: 'Overview' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Updates' })).toBeTruthy()
    fireEvent.click(screen.getByRole('tab', { name: 'Containers' }))
    await screen.findByText('web-1')
    expect(screen.queryByRole('button', { name: 'Terminal' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Update agent' })).toBeNull()
    for (const action of ['Start', 'Stop', 'Restart', 'Remove']) {
      expect(screen.queryByRole('button', { name: action })).toBeNull()
    }
    expect(screen.getByRole('button', { name: 'Logs' })).toBeTruthy()
  })

  it('app operator on one stack sees the limited card and only deploy actions', async () => {
    const me = meApi({
      stacks: {
        'srv-1': {
          web: [
            'stack.view',
            'stack.deploy',
            'container.view',
            'container.logs',
            'container.control',
          ],
        },
      },
      containers: {
        'srv-1': {
          'web-1': ['container.view', 'container.logs', 'container.control'],
        },
      },
      servers: { 'srv-1': [] },
    })

    mount(me, <Fleet client={fleetClient(fleetServer(true))} />)
    expect(await screen.findByText('Limited access')).toBeTruthy()
    expect(screen.queryByRole('meter')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Add server' })).toBeNull()
    cleanup()

    serverScreen(me, true)
    expect(await screen.findByRole('tab', { name: 'Containers' })).toBeTruthy()
    for (const hidden of ['Overview', 'Services', 'Logs', 'Updates']) {
      expect(screen.queryByRole('tab', { name: hidden })).toBeNull()
    }
    await screen.findByText('web-1')
    expect(screen.queryByRole('button', { name: 'Terminal' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Stop managing' })).toBeNull()
    expect(await screen.findByRole('button', { name: 'Pull images' })).toBeTruthy()
  })

  it('metrics_only sees the read-only overview and containers tabs', async () => {
    serverScreen(
      meApi({
        servers: {
          'srv-1': ['server.view', 'stack.view', 'container.view'],
        },
      }),
    )

    expect(await screen.findByRole('tab', { name: 'Overview' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Containers' })).toBeTruthy()
    for (const hidden of ['Services', 'Logs', 'Updates']) {
      expect(screen.queryByRole('tab', { name: hidden })).toBeNull()
    }
    fireEvent.click(screen.getByRole('tab', { name: 'Containers' }))
    await screen.findByText('web-1')
    expect(screen.queryByRole('button', { name: 'Logs' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Terminal' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Start' })).toBeNull()
  })

  it('hides the audit entry from users without audit.view', async () => {
    const adminMe = meApi({ organization: allPermissions })
    const viewerMe = meApi({ servers: { 'srv-1': ['server.view'] } })
    const client = fleetClient(fleetServer())

    const admin = mount(adminMe, <App client={client} />)
    expect(await screen.findByRole('button', { name: 'Audit' })).toBeTruthy()
    admin.unmount()

    mount(viewerMe, <App client={client} />)
    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Audit' })).toBeNull())
  })
})
