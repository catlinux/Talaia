import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Alert, AlertsApi } from '../api/alerts'
import { createMockFleetClient, type FleetClient } from '../api/client'
import { ApiError } from '../api/http'
import type { MeApi, MePermissions, MeResponse } from '../api/me'
import type { ActivityEvent, EnrollmentToken, Server } from '../api/types'
import { PermissionsProvider } from '../auth/permissions'
import i18n from '../i18n'
import { RouterProvider } from '../router'
import { Servers, refreshInterval } from './Servers'

const serverNames = ['vps-web-01', 'vps-db-01', 'vps-mail', 'vps-dev', 'vps-backup', 'rpi-casa']
const mockClient = createMockFleetClient()

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: 'server-1',
    name: 'vps-web-01',
    hostname: 'vps-web-01',
    os: 'Debian GNU/Linux 13',
    location: 'Paris',
    vcpu: 4,
    memoryGb: 8,
    status: 'online',
    cpuPercent: 12,
    memPercent: 25,
    diskPercent: 20,
    containers: 3,
    runningContainers: 3,
    stacks: 1,
    tags: ['web'],
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
    serverId: 'server-1',
    serverName: 'vps-web-01',
    subjectKind: 'server',
    subjectName: 'server-1',
    stack: '',
    startedAt: '2026-10-19T10:00:00Z',
    resolvedAt: '',
    ackedAt: '',
    ackedBy: '',
    silenced: false,
    summary: null,
    ...overrides,
  }
}

function clientReturning(
  servers: () => Promise<Server[]>,
  activity: ActivityEvent[] = [],
): FleetClient {
  return {
    listServers: servers,
    listActivity: () => Promise.resolve(activity),
    createEnrollment: () => Promise.reject(new Error('unexpected enrollment')),
  }
}

function alertsApi(alerts: Alert[] = []): AlertsApi {
  const counts = new Map<string, { critical: number; warning: number; info: number }>()
  const servers: Record<string, string> = {}
  for (const item of alerts) {
    if (item.serverId === null) continue
    const current = counts.get(item.serverId) ?? { critical: 0, warning: 0, info: 0 }
    if (item.severity === 'critical') current.critical += 1
    else if (item.severity === 'warning') current.warning += 1
    else current.info += 1
    counts.set(item.serverId, current)
    if (item.severity === 'critical' || servers[item.serverId] === undefined) {
      servers[item.serverId] = item.severity
    } else if (item.severity === 'warning' && servers[item.serverId] !== 'critical') {
      servers[item.serverId] = 'warning'
    }
  }
  return {
    list: vi.fn(() => Promise.resolve(alerts)),
    summary: vi.fn(() =>
      Promise.resolve({
        counts: {
          critical: [...counts.values()].reduce((total, item) => total + item.critical, 0),
          warning: [...counts.values()].reduce((total, item) => total + item.warning, 0),
          info: [...counts.values()].reduce((total, item) => total + item.info, 0),
        },
        servers,
      }),
    ),
    silences: () => Promise.resolve([]),
    acknowledge: () => Promise.resolve(),
    unwatch: () => Promise.resolve(),
    createSilence: () => Promise.reject(new Error('unexpected silence')),
    deleteSilence: () => Promise.resolve(),
  }
}

function meApi(permissions: Partial<MePermissions> = {}): MeApi {
  const body: MeResponse = {
    user: { id: 'u1', username: 'admin', display_name: 'Admin', email: '' },
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

function renderServers(client: FleetClient, alerts?: AlertsApi, me?: MeApi) {
  const content = <Servers client={client} alertsApi={alerts} />
  return render(
    <RouterProvider>
      {me === undefined ? content : <PermissionsProvider meApi={me}>{content}</PermissionsProvider>}
    </RouterProvider>,
  )
}

function searchParam(name: string): string | null {
  return new URLSearchParams(window.location.search).get(name)
}

beforeEach(async () => {
  localStorage.clear()
  window.history.replaceState({}, '', '/servers')
  await i18n.changeLanguage('en')
})

afterEach(() => {
  vi.useRealTimers()
  cleanup()
  vi.restoreAllMocks()
  window.history.replaceState({}, '', '/servers')
})

describe('Servers', () => {
  it('renders the table with the six servers and their meters', async () => {
    renderServers(mockClient)

    expect(await screen.findByRole('heading', { name: 'Servers' })).toBeTruthy()
    for (const name of serverNames) {
      expect(await screen.findByRole('link', { name })).toBeTruthy()
    }
    expect(screen.getAllByRole('meter')).toHaveLength(serverNames.length * 3)
    expect(screen.getByText('Disk at 91%')).toBeTruthy()
  })

  it('shows loading skeletons', () => {
    renderServers({
      listServers: () => new Promise<Server[]>(() => undefined),
      listActivity: () => Promise.resolve([]),
      createEnrollment: () => Promise.reject(new Error('unexpected')),
    })

    expect(screen.getByText('Loading the servers…')).toBeTruthy()
    expect(document.querySelector('.ui-table__skeleton')).toBeTruthy()
  })

  it('shows the first-load error and retries', async () => {
    const listServers = vi
      .fn<() => Promise<Server[]>>()
      .mockRejectedValueOnce(new Error('down'))
      .mockResolvedValue([server()])
    renderServers({ ...clientReturning(listServers) })

    expect(await screen.findByText('The servers could not be loaded.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))

    expect(await screen.findByRole('link', { name: 'vps-web-01' })).toBeTruthy()
    expect(listServers).toHaveBeenCalledTimes(2)
  })

  it('keeps the last data on screen when a refresh fails', async () => {
    vi.useFakeTimers()
    const listServers = vi
      .fn<() => Promise<Server[]>>()
      .mockResolvedValueOnce([server()])
      .mockRejectedValueOnce(new Error('down'))
    renderServers({ ...clientReturning(listServers) })

    await act(async () => {})
    expect(screen.getByRole('link', { name: 'vps-web-01' })).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(refreshInterval)
    })

    expect(screen.getByRole('link', { name: 'vps-web-01' })).toBeTruthy()
    expect(
      screen.getByText('The servers could not be refreshed: showing the last data.'),
    ).toBeTruthy()
    expect(screen.queryByText('The servers could not be loaded.')).toBeNull()
  })

  it('stops polling when it unmounts', async () => {
    vi.useFakeTimers()
    const listServers = vi.fn<() => Promise<Server[]>>().mockResolvedValue([])
    const view = renderServers({ ...clientReturning(listServers) })

    await act(async () => {})
    expect(listServers).toHaveBeenCalledTimes(1)

    view.unmount()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(refreshInterval * 3)
    })
    expect(listServers).toHaveBeenCalledTimes(1)
  })

  it('applies the URL filters on load', async () => {
    renderServers(mockClient)
    await screen.findByRole('link', { name: 'vps-web-01' })

    for (const [query, visible, hidden] of [
      ['?status=offline', 'rpi-casa', 'vps-web-01'],
      ['?updates=security', 'vps-web-01', 'vps-db-01'],
      ['?reboot=1', 'vps-backup', 'vps-web-01'],
    ] as const) {
      cleanup()
      window.history.replaceState({}, '', `/servers${query}`)
      renderServers(mockClient)
      expect(await screen.findByRole('link', { name: visible })).toBeTruthy()
      expect(screen.queryByRole('link', { name: hidden })).toBeNull()
    }
  })

  it('searches and writes the query to the URL', async () => {
    renderServers(mockClient)
    await screen.findByRole('link', { name: 'vps-web-01' })

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search' }), {
      target: { value: 'vps-db' },
    })

    expect(await screen.findByRole('link', { name: 'vps-db-01' })).toBeTruthy()
    expect(searchParam('q')).toBe('vps-db')
    expect(screen.queryByRole('link', { name: 'vps-web-01' })).toBeNull()
  })

  it('writes the sort to the URL when a column is toggled', async () => {
    renderServers(mockClient)
    await screen.findByRole('link', { name: 'vps-web-01' })

    fireEvent.click(screen.getByRole('button', { name: 'Server' }))
    await waitFor(() => expect(searchParam('sort')).toBe('name:asc'))

    fireEvent.click(screen.getByRole('button', { name: 'Server' }))
    await waitFor(() => expect(searchParam('sort')).toBe('name:desc'))
  })

  it('persists the card view in localStorage', async () => {
    const view = renderServers(mockClient)
    await screen.findByRole('link', { name: 'vps-web-01' })

    fireEvent.click(screen.getByRole('button', { name: 'Cards' }))

    expect(localStorage.getItem('talaia.serversView')).toBe('cards')
    expect(await screen.findByRole('button', { name: 'vps-web-01' })).toBeTruthy()

    view.unmount()
    renderServers(mockClient)
    expect(await screen.findByRole('button', { name: 'vps-web-01' })).toBeTruthy()
  })

  it('shows no matches and clears the filters', async () => {
    renderServers(clientReturning(() => Promise.resolve([server()])))
    await screen.findByRole('link', { name: 'vps-web-01' })

    window.history.replaceState({}, '', '/servers?status=offline')
    window.dispatchEvent(new PopStateEvent('popstate'))

    expect(await screen.findByText('No servers match these filters')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))

    expect(await screen.findByRole('link', { name: 'vps-web-01' })).toBeTruthy()
    expect(window.location.search).toBe('')
  })

  it('shows the empty fleet explanation', async () => {
    renderServers(clientReturning(() => Promise.resolve([])))

    expect(await screen.findByText('No servers yet')).toBeTruthy()
    expect(screen.getByText(/Press "Add server"/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Add server' })).toBeTruthy()
  })

  it('hides Add server without server.enroll', async () => {
    renderServers(mockClient, undefined, meApi({ organization: ['server.view'] }))

    expect(await screen.findByRole('link', { name: 'vps-web-01' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Add server' })).toBeNull()
  })

  it('does not call the alerts API without alerts.view', async () => {
    const alerts = alertsApi([alert()])
    renderServers(mockClient, alerts, meApi({ organization: ['server.view'] }))

    expect(await screen.findByRole('link', { name: 'vps-web-01' })).toBeTruthy()
    expect(alerts.list).not.toHaveBeenCalled()
    expect(alerts.summary).not.toHaveBeenCalled()
    expect(screen.queryByRole('link', { name: '1' })).toBeNull()
  })

  it('hides the alert count when the alerts API fails', async () => {
    const alerts: AlertsApi = {
      ...alertsApi(),
      list: () => Promise.reject(new Error('forbidden')),
      summary: () => Promise.reject(new Error('forbidden')),
    }
    renderServers(
      clientReturning(() => Promise.resolve([server()])),
      alerts,
    )

    expect(await screen.findByRole('link', { name: 'vps-web-01' })).toBeTruthy()
    await waitFor(() => expect(document.querySelector('.servers__alerts')).toBeNull())
  })

  it('shows only status and name for a limited server', async () => {
    renderServers(
      clientReturning(() =>
        Promise.resolve([
          server({
            limited: true,
            cpuPercent: 99,
            memPercent: 99,
            diskPercent: 99,
            containers: 9,
            agentUpdateAvailable: true,
          }),
        ]),
      ),
    )

    expect(await screen.findByRole('link', { name: 'vps-web-01' })).toBeTruthy()
    expect(screen.queryByRole('meter')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Actions for vps-web-01' })).toBeNull()
    expect(screen.getAllByText('—').length).toBeGreaterThan(0)
  })

  it('shows the alert count and links it to the filtered alerts list', async () => {
    const alerts = alertsApi([alert()])
    renderServers(
      clientReturning(() => Promise.resolve([server()])),
      alerts,
    )

    const link = await screen.findByRole('link', { name: '1' })
    expect(link.getAttribute('href')).toBe('/alerts?server=server-1')
    expect(link.className).toContain('servers__alerts--critical')
  })

  it('shows the agent update and manual update badges', async () => {
    renderServers(
      clientReturning(() =>
        Promise.resolve([
          server({ agentVersion: '0.2.0', agentUpdateAvailable: true }),
          server({
            id: 'server-2',
            name: 'vps-db-01',
            hostname: 'vps-db-01',
            agentUpdateAvailable: false,
            agentUpdateManual: true,
          }),
        ]),
      ),
    )

    expect(await screen.findByText('Update available')).toBeTruthy()
    expect(screen.getByText('Manual update')).toBeTruthy()
  })

  it('shows the apt security and reboot badges', async () => {
    renderServers(
      clientReturning(() =>
        Promise.resolve([
          server({
            apt: { upgradable: 4, security: 2, rebootRequired: true },
          }),
        ]),
      ),
    )

    expect(await screen.findByText('2 security updates')).toBeTruthy()
    expect(screen.getByText('Reboot')).toBeTruthy()
  })

  it('opens the server from its name and from the row menu', async () => {
    renderServers(clientReturning(() => Promise.resolve([server({ id: 'srv-9', name: 'vps-9' })])))

    fireEvent.click(await screen.findByRole('link', { name: 'vps-9' }))
    expect(window.location.pathname).toBe('/servers/srv-9')

    window.history.replaceState({}, '', '/servers')
    window.dispatchEvent(new PopStateEvent('popstate'))
    await screen.findByRole('link', { name: 'vps-9' })
    fireEvent.click(screen.getByRole('button', { name: 'Actions for vps-9' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Containers' }))

    expect(window.location.pathname).toBe('/servers/srv-9/containers')
  })

  it('creates a token and shows the command to run on the server', async () => {
    const created: EnrollmentToken = {
      token: 'tl_enr_abc',
      caFingerprint: 'ab'.repeat(32),
      expiresIn: 900,
      command: 'curl -fsSk https://192.0.2.10:8443/install.sh | sudo bash -s -- --token tl_enr_abc',
    }
    const createEnrollment = vi.fn(() => Promise.resolve(created))
    renderServers({ ...clientReturning(() => Promise.resolve([server()])), createEnrollment })

    fireEvent.click(await screen.findByRole('button', { name: 'Add server' }))
    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: ' vps-new ' } })
    fireEvent.submit(screen.getByRole('button', { name: 'Create token' }).closest('form')!)

    expect(await screen.findByText(created.command)).toBeTruthy()
    expect(createEnrollment).toHaveBeenCalledWith('vps-new')
    expect(screen.getByText('The token works once and will not be shown again.')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    expect(screen.getByRole('button', { name: 'Add server' })).toBeTruthy()
  })

  it('says when the name is already taken', async () => {
    const createEnrollment = () => Promise.reject(new ApiError('name_taken', 409))
    renderServers({ ...clientReturning(() => Promise.resolve([server()])), createEnrollment })

    fireEvent.click(await screen.findByRole('button', { name: 'Add server' }))
    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'vps-web-01' } })
    fireEvent.submit(screen.getByRole('button', { name: 'Create token' }).closest('form')!)

    expect((await screen.findByRole('alert')).textContent).toBe(
      'A server or a pending token with that name already exists.',
    )
  })
})
