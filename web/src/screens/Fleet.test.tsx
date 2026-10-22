import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { createMockFleetClient, type FleetClient } from '../api/client'
import { ApiError } from '../api/http'
import type { ActivityEvent, EnrollmentToken, Server } from '../api/types'
import i18n from '../i18n'
import { Fleet, refreshInterval } from './Fleet'

const serverNames = ['vps-web-01', 'vps-db-01', 'vps-mail', 'vps-dev', 'vps-backup', 'rpi-casa']

const mockClient = createMockFleetClient()

/** server builds a mapped server with sensible defaults. */
function server(overrides: Partial<Server> = {}): Server {
  return {
    id: 'server-1',
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

/** clientReturning serves the given lists once per call. */
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

describe('Fleet', () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    vi.useRealTimers()
    cleanup()
  })

  it('renders the KPI row and the six servers of the mock client', async () => {
    render(<Fleet client={mockClient} />)

    expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('Fleet')

    expect(await screen.findByText('vps-web-01')).toBeTruthy()

    expect(screen.getByText('Servers online')).toBeTruthy()
    expect(screen.getByText('Running containers')).toBeTruthy()
    expect(screen.getByText('Security updates')).toBeTruthy()
    expect(screen.getByText('Active alerts')).toBeTruthy()
    expect(screen.getByText('Last backup')).toBeTruthy()
    expect(screen.getByText('5 / 6')).toBeTruthy()

    for (const name of serverNames) {
      expect(screen.getByText(name)).toBeTruthy()
    }

    // Three meters (CPU, RAM, disk) per server.
    expect(screen.getAllByRole('meter')).toHaveLength(serverNames.length * 3)
  })

  it('shows the text status of a critical server', async () => {
    render(<Fleet client={mockClient} />)

    expect(await screen.findByText('vps-db-01')).toBeTruthy()
    expect(screen.getByText('Disk at 91%')).toBeTruthy()
  })

  it('shows the activity feed with translated messages', async () => {
    render(<Fleet client={mockClient} />)

    expect(await screen.findByText(/Disk above 90% on vps-db-01/)).toBeTruthy()
    expect(screen.getByRole('heading', { name: /Activity/ })).toBeTruthy()
  })

  it('shows the error state when the first load rejects', async () => {
    const client: FleetClient = {
      listServers: () => Promise.reject(new Error('no connection')),
      listActivity: () => Promise.resolve([]),
      createEnrollment: () => Promise.reject(new Error('unexpected enrollment')),
    }

    render(<Fleet client={client} />)

    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(screen.getByText('The fleet could not be loaded')).toBeTruthy()
  })

  it('shows the servers of the injected client', async () => {
    render(<Fleet client={clientReturning(() => Promise.resolve([server()]))} />)

    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    // No location yet: the specification line leaves it out.
    expect(screen.getByText('Debian GNU/Linux 13 · 4 vCPU / 8 GB')).toBeTruthy()
    expect(screen.getByText('Online')).toBeTruthy()
    expect(screen.getByText('1 / 1')).toBeTruthy()
  })

  it('shows the agent version and the update badge', async () => {
    render(
      <Fleet
        client={clientReturning(() =>
          Promise.resolve([server({ agentVersion: '0.2.0', agentUpdateAvailable: true })]),
        )}
      />,
    )

    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    expect(screen.getByText('Agent 0.2.0')).toBeTruthy()
    expect(screen.getByText('Update available')).toBeTruthy()
  })

  it('shows a manual update badge for an agent that is too old', async () => {
    render(
      <Fleet
        client={clientReturning(() =>
          Promise.resolve([server({ agentUpdateManual: true, agentUpdateAvailable: false })]),
        )}
      />,
    )

    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    expect(screen.getByText('Manual update')).toBeTruthy()
    expect(screen.queryByText('Update available')).toBeNull()
  })

  it('shows the apt update, security and reboot badges', async () => {
    render(
      <Fleet
        client={clientReturning(() =>
          Promise.resolve([
            server({
              apt: {
                upgradable: 4,
                security: 2,
                rebootRequired: true,
                checkedAt: '2026-10-19T11:30:00Z',
                listsUpdatedAt: '2026-10-19T09:00:00Z',
              },
            }),
          ]),
        )}
      />,
    )

    const updates = await screen.findByText('4 updates')
    expect(updates.className).toContain('server__apt--security')
    expect(screen.getByText('2 security updates')).toBeTruthy()
    expect(screen.getByText('Reboot required')).toBeTruthy()
  })

  it('shows no apt badges when the agent did not report apt data', async () => {
    render(<Fleet client={clientReturning(() => Promise.resolve([server()]))} />)

    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    expect(screen.queryByText('4 updates')).toBeNull()
    expect(screen.queryByText('Reboot required')).toBeNull()
  })

  it('explains how to add a server when the fleet is empty', async () => {
    render(<Fleet client={clientReturning(() => Promise.resolve([]))} />)

    expect(await screen.findByText('No servers yet')).toBeTruthy()
    expect(screen.getByText(/Press "Add server"/)).toBeTruthy()
    expect(screen.queryByText('Servers online')).toBeNull()
  })

  it('keeps the KPIs the API cannot fill as not available', async () => {
    render(<Fleet client={clientReturning(() => Promise.resolve([server()]))} />)

    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    // Security updates, active alerts and the last backup have no source yet.
    expect(screen.getAllByText('Not available yet')).toHaveLength(3)
  })

  it('keeps the last data on screen when a refresh fails', async () => {
    vi.useFakeTimers()
    const listServers = vi
      .fn<() => Promise<Server[]>>()
      .mockResolvedValueOnce([server()])
      .mockRejectedValueOnce(new Error('down'))
    render(<Fleet client={clientReturning(listServers)} />)

    await act(async () => {})
    expect(screen.getByText('vps-web-01')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(refreshInterval)
    })

    // The screen is not wiped and the notice explains the stale data.
    expect(screen.getByText('vps-web-01')).toBeTruthy()
    expect(
      screen.getByText('The fleet could not be refreshed: showing the last data.'),
    ).toBeTruthy()
    expect(screen.queryByText('The fleet could not be loaded')).toBeNull()
  })

  it('stops polling when it unmounts', async () => {
    vi.useFakeTimers()
    const listServers = vi.fn<() => Promise<Server[]>>().mockResolvedValue([])
    const { unmount } = render(<Fleet client={clientReturning(listServers)} />)

    await act(async () => {})
    expect(listServers).toHaveBeenCalledTimes(1)

    unmount()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(refreshInterval * 3)
    })

    expect(listServers).toHaveBeenCalledTimes(1)
  })

  it('opens the screen of a server from its name', async () => {
    const onOpenServer = vi.fn()
    render(
      <Fleet
        client={clientReturning(() => Promise.resolve([server({ id: 'srv-9', name: 'vps-9' })]))}
        onOpenServer={onOpenServer}
      />,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'vps-9' }))

    expect(onOpenServer).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'srv-9', name: 'vps-9' }),
    )
  })

  describe('add server', () => {
    const created: EnrollmentToken = {
      token: 'tl_enr_abc',
      caFingerprint: 'ab'.repeat(32),
      expiresIn: 900,
      command: 'curl -fsSk https://192.0.2.10:8443/install.sh | sudo bash -s -- --token tl_enr_abc',
    }

    function addServerClient(createEnrollment: FleetClient['createEnrollment']): FleetClient {
      return { ...clientReturning(() => Promise.resolve([server()])), createEnrollment }
    }

    it('creates a token and shows the command to run on the server', async () => {
      const createEnrollment = vi.fn(() => Promise.resolve(created))
      render(<Fleet client={addServerClient(createEnrollment)} />)

      fireEvent.click(await screen.findByRole('button', { name: 'Add server' }))
      fireEvent.change(screen.getByLabelText('Server name'), { target: { value: ' vps-new ' } })
      fireEvent.submit(screen.getByRole('button', { name: 'Create token' }).closest('form')!)

      expect(await screen.findByText(created.command)).toBeTruthy()
      expect(createEnrollment).toHaveBeenCalledWith('vps-new')
      expect(screen.getByText(/within the next 15 minutes/)).toBeTruthy()
      expect(screen.getByText('The token works once and will not be shown again.')).toBeTruthy()

      fireEvent.click(screen.getByRole('button', { name: 'Done' }))
      expect(screen.getByRole('button', { name: 'Add server' })).toBeTruthy()
      expect(screen.queryByText(created.command)).toBeNull()
    })

    it('copies the command to the clipboard', async () => {
      const writeText = vi.fn(() => Promise.resolve())
      Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
      render(<Fleet client={addServerClient(() => Promise.resolve(created))} />)

      fireEvent.click(await screen.findByRole('button', { name: 'Add server' }))
      fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'vps-new' } })
      fireEvent.submit(screen.getByRole('button', { name: 'Create token' }).closest('form')!)
      fireEvent.click(await screen.findByRole('button', { name: 'Copy the command' }))

      await waitFor(() => expect(writeText).toHaveBeenCalledWith(created.command))
      expect(await screen.findByRole('button', { name: 'Copied' })).toBeTruthy()
    })

    it('says when the name is already taken', async () => {
      const createEnrollment = () => Promise.reject(new ApiError('name_taken', 409))
      render(<Fleet client={addServerClient(createEnrollment)} />)

      fireEvent.click(await screen.findByRole('button', { name: 'Add server' }))
      fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'vps-web-01' } })
      fireEvent.submit(screen.getByRole('button', { name: 'Create token' }).closest('form')!)

      expect((await screen.findByRole('alert')).textContent).toBe(
        'A server or a pending token with that name already exists.',
      )
    })

    it('shows the raw values when the core has no public address', async () => {
      const createEnrollment = () => Promise.resolve({ ...created, command: '' })
      render(<Fleet client={addServerClient(createEnrollment)} />)

      fireEvent.click(await screen.findByRole('button', { name: 'Add server' }))
      fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'vps-new' } })
      fireEvent.submit(screen.getByRole('button', { name: 'Create token' }).closest('form')!)

      expect(await screen.findByText('tl_enr_abc')).toBeTruthy()
      expect(screen.getByText('ab'.repeat(32))).toBeTruthy()
    })
  })
})
