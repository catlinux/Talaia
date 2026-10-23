import { describe, expect, it, vi } from 'vitest'

import { createRecordingsApi } from './recordings'

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api recordings client', () => {
  it('maps container and host sessions and builds the download URL', async () => {
    const fetchImpl = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) => {
      void _input
      void _init
      return Promise.resolve(
        json([
          {
            id: 'r1',
            server_id: 'srv1',
            server_name: 'vps-web-01',
            container: 'web',
            kind: 'container',
            shell: 'bash',
            user: 'admin',
            started_at: '2026-10-20T10:00:00Z',
            ended_at: '2026-10-20T10:01:00Z',
            duration_seconds: 60,
            close_reason: 'exited',
            privileged: false,
            bytes: 1024,
          },
          {
            id: 'r2',
            server_id: 'srv1',
            server_name: 'vps-web-01',
            container: '',
            kind: 'host',
            shell: 'sh',
            user: 'admin',
            started_at: '2026-10-20T11:00:00Z',
            ended_at: null,
            duration_seconds: 0,
            close_reason: '',
            privileged: true,
            bytes: 0,
          },
        ]),
      )
    })
    const api = createRecordingsApi(fetchImpl as unknown as typeof fetch)

    await expect(api.list()).resolves.toEqual([
      {
        id: 'r1',
        serverId: 'srv1',
        serverName: 'vps-web-01',
        container: 'web',
        kind: 'container',
        shell: 'bash',
        user: 'admin',
        startedAt: '2026-10-20T10:00:00Z',
        endedAt: '2026-10-20T10:01:00Z',
        durationSeconds: 60,
        closeReason: 'exited',
        privileged: false,
        bytes: 1024,
      },
      {
        id: 'r2',
        serverId: 'srv1',
        serverName: 'vps-web-01',
        container: '',
        kind: 'host',
        shell: 'sh',
        user: 'admin',
        startedAt: '2026-10-20T11:00:00Z',
        endedAt: null,
        durationSeconds: 0,
        closeReason: '',
        privileged: true,
        bytes: 0,
      },
    ])
    expect(String(fetchImpl.mock.calls[0]?.[0])).toBe('/api/v1/recordings?limit=100')
    expect(api.downloadUrl('r1')).toBe('/api/v1/recordings/r1/download')
  })
})
