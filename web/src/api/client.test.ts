import { describe, expect, it, vi } from 'vitest'

import { createApiFleetClient } from './client'

interface LiveOverrides {
  agent_version?: string
  cpu_count?: number
  cpu_percent?: number
  mem_used_bytes?: number
  mem_total_bytes?: number
  disk_used_bytes?: number
  disk_total_bytes?: number
  containers?: number
  compose_projects?: number
  host_terminal?: boolean
  docker_version?: string
  uptime_seconds?: number
  seen_at?: string
  apt?: {
    upgradable: number
    security: number
    reboot_required: boolean
    checked_at?: string
    lists_updated_at?: string
  }
}

/** live builds the API live object of a connected agent. */
function live(overrides: LiveOverrides = {}) {
  return {
    hostname: 'vps-web-01',
    agent_version: '0.2.0',
    os_name: 'Debian GNU/Linux',
    os_version: '13',
    kernel: '6.12.0-amd64',
    arch: 'amd64',
    cpu_count: 4,
    cpu_percent: 12.5,
    mem_used_bytes: 2 * 1024 ** 3,
    mem_total_bytes: 8 * 1024 ** 3,
    disk_used_bytes: 20 * 1024 ** 3,
    disk_total_bytes: 100 * 1024 ** 3,
    net_rx_bytes: 11,
    net_tx_bytes: 22,
    docker_available: true,
    containers: 3,
    running_containers: 2,
    compose_projects: 1,
    services: 5,
    seen_at: '2026-10-19T12:00:00Z',
    ...overrides,
  }
}

/** serverEntry builds one entry of the API response. */
function serverEntry(overrides: Record<string, unknown> = {}) {
  return {
    id: '11111111-1111-1111-1111-111111111111',
    name: 'vps-web-01',
    created_at: '2026-10-01T10:00:00Z',
    last_seen: '2026-10-19T11:59:00Z',
    revoked: false,
    cert_not_after: '2027-01-01T10:00:00Z',
    connected: true,
    agent_update_available: false,
    agent_update_manual: false,
    live: live(),
    ...overrides,
  }
}

/** clientFor answers GET /api/v1/servers with the given entries. */
function clientFor(entries: unknown[], upgradeCommand = '', latestVersion = '') {
  const calls: string[] = []
  const fetchImpl = vi.fn((input: RequestInfo | URL) => {
    calls.push(String(input))
    return Promise.resolve(
      new Response(
        JSON.stringify({
          servers: entries,
          agent_latest_version: latestVersion,
          agent_upgrade_command: upgradeCommand,
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )
  })
  return { client: createApiFleetClient(fetchImpl as unknown as typeof fetch), calls }
}

describe('api fleet client', () => {
  it('maps a healthy connected server', async () => {
    const { client, calls } = clientFor([serverEntry()])

    const servers = await client.listServers()

    expect(calls).toEqual(['/api/v1/servers'])
    expect(servers).toHaveLength(1)
    expect(servers[0]).toEqual({
      id: '11111111-1111-1111-1111-111111111111',
      name: 'vps-web-01',
      hostname: 'vps-web-01',
      os: 'Debian GNU/Linux 13',
      location: '',
      vcpu: 4,
      memoryGb: 8,
      status: 'online',
      statusDetailKey: undefined,
      cpuPercent: 12.5,
      memPercent: 25,
      diskPercent: 20,
      containers: 3,
      stacks: 1,
      tags: [],
      lastSeen: '2026-10-19T12:00:00Z',
      agentVersion: '0.2.0',
      agentLatestVersion: '',
      agentUpdateAvailable: false,
      agentUpdateManual: false,
      agentUpgradeCommand: '',
      hostTerminal: false,
      kernel: '6.12.0-amd64',
      arch: 'amd64',
      runningContainers: 2,
      services: 5,
      memUsedBytes: 2 * 1024 ** 3,
      memTotalBytes: 8 * 1024 ** 3,
      diskUsedBytes: 20 * 1024 ** 3,
      diskTotalBytes: 100 * 1024 ** 3,
      netRxBytes: 11,
      netTxBytes: 22,
      dockerVersion: '',
      uptimeSeconds: 0,
    })
  })

  it('maps the host terminal capability the agent reports', async () => {
    const { client } = clientFor([serverEntry({ live: live({ host_terminal: true }) })])

    const [server] = await client.listServers()

    expect(server?.hostTerminal).toBe(true)
  })

  it('maps the manual upgrade flag and the command', async () => {
    const { client } = clientFor(
      [serverEntry({ agent_update_available: false, agent_update_manual: true })],
      'sudo bash install.sh --upgrade',
      '0.2.8',
    )

    const [server] = await client.listServers()

    expect(server?.agentUpdateManual).toBe(true)
    expect(server?.agentUpgradeCommand).toBe('sudo bash install.sh --upgrade')
    expect(server?.agentLatestVersion).toBe('0.2.8')
  })

  it('maps the agent version and the update flag', async () => {
    const { client } = clientFor([
      serverEntry({ agent_update_available: true, live: live({ agent_version: '0.2.0' }) }),
    ])

    const [server] = await client.listServers()

    expect(server?.agentVersion).toBe('0.2.0')
    expect(server?.agentUpdateAvailable).toBe(true)
  })

  it('maps the apt summary the agent reports', async () => {
    const { client } = clientFor([
      serverEntry({
        live: live({
          apt: {
            upgradable: 4,
            security: 2,
            reboot_required: true,
            checked_at: '2026-10-19T11:30:00Z',
            lists_updated_at: '2026-10-19T09:00:00Z',
          },
        }),
      }),
    ])

    const [server] = await client.listServers()

    expect(server?.apt).toEqual({
      upgradable: 4,
      security: 2,
      rebootRequired: true,
      checkedAt: '2026-10-19T11:30:00Z',
      listsUpdatedAt: '2026-10-19T09:00:00Z',
    })
  })

  it('leaves apt absent when the agent did not report it', async () => {
    const { client } = clientFor([serverEntry()])

    const [server] = await client.listServers()

    expect(server?.apt).toBeUndefined()
  })

  it('marks a server over 75 percent as warning and over 90 as critical', async () => {
    const warning = clientFor([serverEntry({ live: live({ disk_used_bytes: 80 * 1024 ** 3 }) })])
    const critical = clientFor([serverEntry({ live: live({ mem_used_bytes: 7.5 * 1024 ** 3 }) })])

    expect((await warning.client.listServers())[0]?.status).toBe('warning')
    expect((await critical.client.listServers())[0]?.status).toBe('critical')
  })

  it('maps an offline server without live data', async () => {
    const { client } = clientFor([
      serverEntry({ connected: false, live: undefined, last_seen: '2026-10-18T09:00:00Z' }),
    ])

    const [server] = await client.listServers()

    expect(server).toMatchObject({
      status: 'offline',
      os: '',
      vcpu: 0,
      memoryGb: 0,
      cpuPercent: null,
      memPercent: null,
      diskPercent: null,
      containers: 0,
      stacks: 0,
      lastSeen: '2026-10-18T09:00:00Z',
    })
  })

  it('maps a revoked server as offline with its own detail', async () => {
    const { client } = clientFor([
      serverEntry({ revoked: true, connected: false, live: undefined }),
    ])

    const [server] = await client.listServers()

    expect(server?.status).toBe('offline')
    expect(server?.statusDetailKey).toBe('status.revoked')
  })

  it('leaves the percentages unknown when a total is zero', async () => {
    const { client } = clientFor([
      serverEntry({ live: live({ mem_total_bytes: 0, disk_total_bytes: 0 }) }),
    ])

    const [server] = await client.listServers()

    expect(server?.memPercent).toBeNull()
    expect(server?.diskPercent).toBeNull()
    // A server with nothing to measure is not over any threshold.
    expect(server?.status).toBe('online')
  })

  it('reports no activity yet', async () => {
    const { client } = clientFor([])

    await expect(client.listActivity()).resolves.toEqual([])
    await expect(client.listServers()).resolves.toEqual([])
  })

  it('creates an enrollment token for a server name', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            token: 'tl_enr_abc',
            ca_fingerprint: 'ab'.repeat(32),
            expires_in: 900,
            command: 'curl …',
          }),
          { status: 201, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    )
    const client = createApiFleetClient(fetchImpl)

    await expect(client.createEnrollment('vps-new')).resolves.toEqual({
      token: 'tl_enr_abc',
      caFingerprint: 'ab'.repeat(32),
      expiresIn: 900,
      command: 'curl …',
    })

    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/servers/enrollment-tokens')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ name: 'vps-new' }))
  })
})
