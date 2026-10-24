import { describe, expect, it, vi } from 'vitest'

import { createAlertsApi } from './alerts'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api alerts client', () => {
  it('maps alerts and query parameters', async () => {
    const fetchImpl = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) => {
      void _input
      void _init
      return Promise.resolve(
        json([
          {
            id: 'a1',
            rule_id: 'rule-1',
            rule_type: 'cpu_high',
            severity: 'critical',
            state: 'firing',
            server_id: 's1',
            server_name: 'vps-web-01',
            subject_kind: 'server',
            subject_name: 's1',
            stack: '',
            started_at: '2026-10-24T10:00:00Z',
            resolved_at: null,
            acked_at: null,
            acked_by: '',
            silenced: true,
            summary: { key: 'alerts.cpu_high', params: { value: 91 } },
          },
        ]),
      )
    })
    const api = createAlertsApi(fetchImpl as unknown as typeof fetch)

    await expect(api.list('active')).resolves.toEqual([
      {
        id: 'a1',
        ruleId: 'rule-1',
        ruleType: 'cpu_high',
        severity: 'critical',
        state: 'firing',
        serverId: 's1',
        serverName: 'vps-web-01',
        subjectKind: 'server',
        subjectName: 's1',
        stack: '',
        startedAt: '2026-10-24T10:00:00Z',
        resolvedAt: '',
        ackedAt: '',
        ackedBy: '',
        silenced: true,
        summary: { key: 'alerts.cpu_high', params: { value: 91 } },
      },
    ])
    expect(String(fetchImpl.mock.calls[0]?.[0])).toBe('/api/v1/alerts?state=active&limit=50')

    await api.list('history', 50, 's1')
    expect(String(fetchImpl.mock.calls[1]?.[0])).toBe(
      '/api/v1/alerts?state=history&limit=50&offset=50&server_id=s1',
    )
  })

  it('maps the summary and silences', async () => {
    const fetchImpl = vi.fn((input: RequestInfo | URL) => {
      if (String(input) === '/api/v1/alerts/summary') {
        return Promise.resolve(
          json({ counts: { warning: 2, critical: 1, info: 3 }, servers: { s1: 'critical' } }),
        )
      }
      return Promise.resolve(
        json([
          {
            id: 'sil-1',
            scope_type: 'server',
            server_id: 's1',
            group_id: null,
            name: null,
            rule_type: null,
            reason: 'maintenance',
            created_by: 'admin',
            created_at: '2026-10-24T10:00:00Z',
            ends_at: '2026-10-24T11:00:00Z',
          },
        ]),
      )
    })
    const api = createAlertsApi(fetchImpl as unknown as typeof fetch)

    await expect(api.summary()).resolves.toEqual({
      counts: { warning: 2, critical: 1, info: 3 },
      servers: { s1: 'critical' },
    })
    await expect(api.silences()).resolves.toEqual([
      {
        id: 'sil-1',
        scopeType: 'server',
        serverId: 's1',
        groupId: null,
        name: null,
        ruleType: null,
        reason: 'maintenance',
        createdBy: 'admin',
        createdAt: '2026-10-24T10:00:00Z',
        endsAt: '2026-10-24T11:00:00Z',
      },
    ])
  })

  it('acknowledges, unwatches and creates/deletes silences', async () => {
    const fetchImpl = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === '/api/v1/silences' && init?.method === 'POST') {
        return Promise.resolve(
          json(
            {
              id: 'sil-2',
              scope_type: 'container',
              server_id: 's1',
              name: 'web',
              reason: 'deploy',
              created_by: 'admin',
              created_at: '2026-10-24T10:00:00Z',
              ends_at: '2026-10-24T11:00:00Z',
            },
            201,
          ),
        )
      }
      return Promise.resolve(new Response(null, { status: 200 }))
    })
    const api = createAlertsApi(fetchImpl as unknown as typeof fetch)

    await api.acknowledge('a1')
    await api.unwatch('a1')
    const silence = await api.createSilence({
      scopeType: 'container',
      serverId: 's1',
      name: 'web',
      ruleType: 'container_down',
      durationMinutes: 30,
      reason: 'deploy',
    })
    await api.deleteSilence('sil-1')

    expect(silence.id).toBe('sil-2')
    const paths = fetchImpl.mock.calls.map(([input]) => String(input))
    expect(paths).toContain('/api/v1/alerts/a1/ack')
    expect(paths).toContain('/api/v1/alerts/a1/unwatch')
    expect(paths).toContain('/api/v1/silences/sil-1')
    const post = fetchImpl.mock.calls.find(
      ([input, init]) => String(input) === '/api/v1/silences' && init?.method === 'POST',
    )
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({
      scope_type: 'container',
      server_id: 's1',
      group_id: '',
      name: 'web',
      rule_type: 'container_down',
      duration_minutes: 30,
      reason: 'deploy',
    })
  })
})
