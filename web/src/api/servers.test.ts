import { describe, expect, it, vi } from 'vitest'

import { AgentUpdateError, createServersApi } from './servers'

/** jsonResponse builds a JSON response with the given status. */
function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api servers client', () => {
  it('maps the agent version and the update flag', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        jsonResponse(200, {
          agent_latest_version: '0.2.8',
          agent_upgrade_command: '',
          servers: [
            {
              id: 'srv-1',
              name: 'vps-web-01',
              connected: true,
              revoked: false,
              agent_update_available: true,
              agent_update_manual: false,
              live: { agent_version: '0.2.0' },
            },
          ],
        }),
      ),
    )
    const api = createServersApi(fetchImpl as unknown as typeof fetch)

    await expect(api.listServers()).resolves.toEqual([
      {
        id: 'srv-1',
        name: 'vps-web-01',
        connected: true,
        revoked: false,
        agentVersion: '0.2.0',
        agentLatestVersion: '0.2.8',
        agentUpdateAvailable: true,
        agentUpdateManual: false,
        agentUpgradeCommand: '',
        hostTerminal: false,
      },
    ])
  })

  it('maps the host terminal capability', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        jsonResponse(200, {
          servers: [
            {
              id: 'srv-4',
              name: 'vps-host-01',
              connected: true,
              revoked: false,
              agent_update_available: false,
              agent_update_manual: false,
              live: { agent_version: '0.3.0', host_terminal: true },
            },
          ],
        }),
      ),
    )
    const api = createServersApi(fetchImpl as unknown as typeof fetch)

    const [server] = await api.listServers()
    expect(server?.hostTerminal).toBe(true)
  })

  it('carries the manual upgrade flag and command', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        jsonResponse(200, {
          agent_latest_version: '0.2.8',
          agent_upgrade_command: 'sudo bash install.sh --upgrade',
          servers: [
            {
              id: 'srv-3',
              name: 'vps-old-01',
              connected: true,
              revoked: false,
              agent_update_available: false,
              agent_update_manual: true,
              live: { agent_version: '0.2.7' },
            },
          ],
        }),
      ),
    )
    const api = createServersApi(fetchImpl as unknown as typeof fetch)

    const [server] = await api.listServers()
    expect(server?.agentUpdateManual).toBe(true)
    expect(server?.agentUpgradeCommand).toBe('sudo bash install.sh --upgrade')
    expect(server?.agentLatestVersion).toBe('0.2.8')
  })

  it('leaves the agent version empty when the server is offline', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        jsonResponse(200, {
          servers: [
            {
              id: 'srv-2',
              name: 'vps-db-01',
              connected: false,
              revoked: false,
              agent_update_available: false,
            },
          ],
        }),
      ),
    )
    const api = createServersApi(fetchImpl as unknown as typeof fetch)

    const [server] = await api.listServers()
    expect(server?.agentVersion).toBe('')
    expect(server?.agentUpdateAvailable).toBe(false)
  })

  it('asks the core to update one agent', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(jsonResponse(202, { job_id: 'job-1' })))
    const api = createServersApi(fetchImpl as unknown as typeof fetch)

    await expect(api.updateAgent('srv-1')).resolves.toBeUndefined()

    expect(fetchImpl).toHaveBeenCalledWith('/api/v1/servers/srv-1/agent/update', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ confirm: true }),
    })
  })

  it('keeps the stable code of a refused update', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(jsonResponse(409, { error: { code: 'no_update' } })),
    )
    const api = createServersApi(fetchImpl as unknown as typeof fetch)

    await expect(api.updateAgent('srv-1')).rejects.toMatchObject({
      name: 'AgentUpdateError',
      code: 'no_update',
      status: 409,
    })
  })

  it('maps an unknown status to a generic update error', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(jsonResponse(500, { error: { code: 'internal' } })),
    )
    const api = createServersApi(fetchImpl as unknown as typeof fetch)

    await expect(api.updateAgent('srv-1')).rejects.toBeInstanceOf(AgentUpdateError)
  })

  it('queues the update of every available agent', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        jsonResponse(202, {
          queued: ['srv-1'],
          skipped: [{ id: 'srv-2', reason: 'agent_offline' }],
        }),
      ),
    )
    const api = createServersApi(fetchImpl as unknown as typeof fetch)

    await expect(api.updateAllAgents()).resolves.toEqual({
      queued: ['srv-1'],
      skipped: [{ id: 'srv-2', reason: 'agent_offline' }],
    })
    expect(fetchImpl).toHaveBeenCalledWith('/api/v1/agents/update-all', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ confirm: true }),
    })
  })
})
