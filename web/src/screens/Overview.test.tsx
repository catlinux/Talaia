import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Alert, AlertsApi } from '../api/alerts'
import type { FleetClient } from '../api/client'
import type { MeApi, MePermissions, MeResponse, Permission } from '../api/me'
import type { ActivityEvent, Server } from '../api/types'
import { PermissionsProvider } from '../auth/permissions'
import i18n from '../i18n'
import { RouterProvider } from '../router'
import { Overview } from './Overview'

const allPermissions: Permission[] = ['server.view', 'server.enroll', 'alerts.view']

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: 'srv-1',
    name: 'vps-web-01',
    os: 'Debian GNU/Linux 13',
    location: '',
    vcpu: 4,
    memoryGb: 8,
    status: 'online',
    cpuPercent: 12,
    memPercent: 25,
    diskPercent: 20,
    containers: 3,
    stacks: 1,
    tags: [],
    lastSeen: '2026-10-19T12:00:00Z',
    agentVersion: '0.2.0',
    agentLatestVersion: '0.2.8',
    agentUpdateAvailable: false,
    agentUpdateManual: false,
    agentUpgradeCommand: '',
    ...overrides,
  }
}

function alert(overrides: Partial<Alert> = {}): Alert {
  return {
    id: 'a1',
    ruleId: 'r1',
    ruleType: 'cpu_high',
    severity: 'critical',
    state: 'firing',
    serverId: 'srv-1',
    serverName: 'vps-web-01',
    subjectKind: 'server',
    subjectName: 'srv-1',
    stack: '',
    startedAt: '2026-10-19T10:00:00Z',
    resolvedAt: '',
    ackedAt: '',
    ackedBy: '',
    silenced: false,
    summary: { key: 'alerts.cpu_high', params: { value: 91 } },
    ...overrides,
  }
}

function meApi(permissions: Partial<MePermissions> = { organization: allPermissions }): MeApi {
  const body: MeResponse = {
    user: { id: 'u1', username: 'admin', display_name: 'Admin', email: '' },
    bindings: { bindings: [] },
    permissions: {
      organization: allPermissions,
      servers: {},
      stacks: {},
      containers: {},
      ...permissions,
    },
  }
  return { get: () => Promise.resolve(body) }
}

function fleetClient(servers: Server[], activity: ActivityEvent[] = []): FleetClient {
  return {
    listServers: () => Promise.resolve(servers),
    listActivity: () => Promise.resolve(activity),
    createEnrollment: () => Promise.reject(new Error('unexpected enrollment')),
  }
}

function alertsApi(alerts: Alert[] = []): AlertsApi {
  return {
    list: vi.fn(() => Promise.resolve(alerts)),
    summary: vi.fn(() =>
      Promise.resolve({
        counts: { warning: 0, critical: 0, info: 0 },
        servers: {},
      }),
    ),
    silences: () => Promise.resolve([]),
    acknowledge: () => Promise.resolve(),
    unwatch: () => Promise.resolve(),
    createSilence: () => Promise.reject(new Error('unexpected silence')),
    deleteSilence: () => Promise.resolve(),
  }
}

function renderOverview(client: FleetClient, alerts: AlertsApi = alertsApi(), me = meApi()) {
  return render(
    <RouterProvider>
      <PermissionsProvider meApi={me}>
        <Overview client={client} alertsApi={alerts} />
      </PermissionsProvider>
    </RouterProvider>,
  )
}

beforeEach(async () => {
  window.history.replaceState({}, '', '/')
  await i18n.changeLanguage('en')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('Overview', () => {
  it('shows loading skeletons while the fleet is being loaded', () => {
    const pending = new Promise<Server[]>(() => undefined)
    renderOverview({
      listServers: () => pending,
      listActivity: () => new Promise<ActivityEvent[]>(() => undefined),
      createEnrollment: () => Promise.reject(new Error('unexpected')),
    })

    expect(screen.getByText('Loading the overview…')).toBeTruthy()
    expect(screen.getByRole('status')).toBeTruthy()
  })

  it('shows an error and retries the load', async () => {
    const listServers = vi
      .fn<() => Promise<Server[]>>()
      .mockRejectedValueOnce(new Error('network'))
      .mockResolvedValue([server()])
    const client: FleetClient = {
      listServers,
      listActivity: () => Promise.resolve([]),
      createEnrollment: () => Promise.reject(new Error('unexpected')),
    }

    renderOverview(client)

    expect(await screen.findByText('The overview could not be loaded.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))

    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    expect(listServers).toHaveBeenCalledTimes(2)
  })

  it('shows the empty attention state when nothing needs action', async () => {
    renderOverview(fleetClient([server()]))

    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    expect(await screen.findByText('Nothing needs your attention')).toBeTruthy()

    const hrefs = screen.getAllByRole('link').map((link) => link.getAttribute('href'))
    for (const href of [
      '/servers?status=offline',
      '/alerts',
      '/servers?updates=security',
      '/servers?reboot=1',
    ]) {
      expect(hrefs).toContain(href)
    }
  })

  it('links every attention item to its action', async () => {
    const data = [
      server({
        status: 'offline',
        diskPercent: 97,
        apt: { upgradable: 3, security: 1, rebootRequired: true },
        agentUpdateAvailable: true,
      }),
    ]
    renderOverview(fleetClient(data), alertsApi([alert()]))

    const openAlert = await screen.findByRole('link', { name: 'Open alert' })
    expect(openAlert.getAttribute('href')).toBe('/alerts?focus=a1')

    const openServer = await screen.findAllByRole('link', { name: 'Open server' })
    expect(openServer.map((link) => link.getAttribute('href'))).toEqual([
      '/servers/srv-1',
      '/servers/srv-1',
    ])

    const updates = await screen.findAllByRole('link', { name: 'View updates' })
    expect(updates.map((link) => link.getAttribute('href'))).toEqual([
      '/servers/srv-1/updates',
      '/servers/srv-1/updates',
    ])

    expect((await screen.findByRole('link', { name: 'Update agent' })).getAttribute('href')).toBe(
      '/servers/srv-1',
    )
  })

  it('does not call the alerts API without alerts.view', async () => {
    const alerts = alertsApi([alert()])
    renderOverview(fleetClient([server()]), alerts, meApi({ organization: ['server.view'] }))

    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    expect(await screen.findByText('Unavailable')).toBeTruthy()
    expect(alerts.list).not.toHaveBeenCalled()
    expect(alerts.summary).not.toHaveBeenCalled()
    expect(screen.queryByRole('link', { name: 'Open alert' })).toBeNull()
  })
})
