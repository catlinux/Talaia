import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { createMockFleetClient, type FleetClient } from '../api/client'
import type { ActivityEvent, Server } from '../api/types'
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

  it('explains how to add a server when the fleet is empty', async () => {
    render(<Fleet client={clientReturning(() => Promise.resolve([]))} />)

    expect(await screen.findByText('No servers yet')).toBeTruthy()
    expect(screen.getByText('talaia-core token create --name <server>').tagName).toBe('CODE')
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
})
