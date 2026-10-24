import { describe, expect, it, vi } from 'vitest'

import { createAlertsAdminApi, type AlertChannelInput } from './alertsAdmin'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const rule = {
  id: 'r1',
  name: 'CPU high',
  type: 'cpu_high',
  enabled: true,
  severity: 'warning',
  params: { threshold: 90, for_seconds: 60 },
  scope_type: 'server',
  server_id: 's1',
  group_id: null,
  scope_name: null,
  channel_ids: ['c1'],
  repeat_interval_seconds: 3600,
  builtin: true,
}

const check = {
  id: 'k1',
  name: 'Health',
  kind: 'http',
  target: 'https://example.com/health',
  config: { method: 'GET', expected_codes: ['200-299'], timeout_seconds: 5 },
  interval_seconds: 60,
  enabled: true,
  server_id: null,
  latest_result: {
    ok: false,
    latency_ms: 32,
    status_code: 503,
    error: 'HTTP 503',
    cert_not_after: '2027-01-01T00:00:00Z',
    time: '2026-10-24T10:00:00Z',
  },
}

const channel = {
  id: 'c1',
  name: 'Ops',
  type: 'telegram',
  enabled: true,
  language: 'en',
  config: { chat_id: '123' },
  has_secret: true,
  last_error: 'blocked address',
  last_ok_at: null,
}

describe('api alerts administration client', () => {
  it('maps rules, checks, channels and settings', async () => {
    const fetchImpl = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/v1/alert-rules') return Promise.resolve(json([rule]))
      if (path === '/api/v1/checks') return Promise.resolve(json([check]))
      if (path === '/api/v1/channels') return Promise.resolve(json([channel]))
      if (path === '/api/v1/settings/alerts') {
        return Promise.resolve(json({ allow_private_targets: true }))
      }
      return Promise.resolve(json({}))
    })
    const api = createAlertsAdminApi(fetchImpl as unknown as typeof fetch)

    await expect(api.listRules()).resolves.toEqual([
      {
        id: 'r1',
        name: 'CPU high',
        type: 'cpu_high',
        enabled: true,
        severity: 'warning',
        params: { threshold: 90, for_seconds: 60 },
        scopeType: 'server',
        serverId: 's1',
        groupId: null,
        scopeName: null,
        channelIds: ['c1'],
        repeatIntervalSeconds: 3600,
        builtin: true,
      },
    ])
    await expect(api.listChecks()).resolves.toEqual([
      {
        id: 'k1',
        name: 'Health',
        kind: 'http',
        target: 'https://example.com/health',
        config: { method: 'GET', expectedCodes: ['200-299'], timeoutSeconds: 5 },
        intervalSeconds: 60,
        enabled: true,
        serverId: null,
        latestResult: {
          ok: false,
          latencyMs: 32,
          statusCode: 503,
          error: 'HTTP 503',
          certNotAfter: '2027-01-01T00:00:00Z',
          time: '2026-10-24T10:00:00Z',
        },
      },
    ])
    await expect(api.listChannels()).resolves.toEqual([
      {
        id: 'c1',
        name: 'Ops',
        type: 'telegram',
        enabled: true,
        language: 'en',
        config: {
          chatId: '123',
          server: '',
          priority: null,
          host: '',
          port: null,
          from: '',
          to: [],
          tlsMode: '',
        },
        hasSecret: true,
        lastError: 'blocked address',
        lastOkAt: null,
      },
    ])
    await expect(api.getSettings()).resolves.toEqual({ allowPrivateTargets: true })
  })

  it('posts rule and check bodies and omits an untouched channel secret', async () => {
    const seen: { path: string; method: string; body: unknown }[] = []
    const fetchImpl = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      const body = init?.body ? JSON.parse(String(init.body)) : null
      seen.push({ path, method: init?.method ?? 'GET', body })
      if (path === '/api/v1/channels/c1/test') {
        return Promise.resolve(json({ ok: false, error: 'redacted failure' }))
      }
      if (path === '/api/v1/settings/alerts') {
        return Promise.resolve(json({ allow_private_targets: true }))
      }
      if (path.startsWith('/api/v1/alert-rules')) return Promise.resolve(json(rule))
      if (path.startsWith('/api/v1/checks')) {
        return Promise.resolve(
          json({
            ...check,
            latest_result: null,
          }),
        )
      }
      return Promise.resolve(json(channel))
    })
    const api = createAlertsAdminApi(fetchImpl as unknown as typeof fetch)

    await api.createRule({
      name: 'CPU high',
      type: 'cpu_high',
      enabled: true,
      severity: 'warning',
      params: { threshold: 80, for_seconds: 60 },
      scopeType: 'organization',
      channelIds: [],
      repeatIntervalSeconds: 300,
    })
    await api.createCheck({
      name: 'Health',
      kind: 'http',
      target: 'https://example.com',
      config: { method: 'HEAD', expectedCodes: ['200'], timeoutSeconds: 5 },
      intervalSeconds: 60,
      enabled: true,
    })
    const input: AlertChannelInput = {
      name: 'Ops',
      type: 'telegram',
      enabled: true,
      language: 'en',
      config: {
        chatId: '123',
        server: '',
        priority: null,
        host: '',
        port: null,
        from: '',
        to: [],
        tlsMode: '',
      },
    }
    await api.updateChannel('c1', input)
    await api.updateChannel('c1', { ...input, secret: { token: 'new-token' } })
    await expect(api.testChannel('c1')).resolves.toEqual({
      ok: false,
      error: 'redacted failure',
    })
    await api.updateSettings(true)

    const ruleCall = seen.find(
      (call) => call.path === '/api/v1/alert-rules' && call.method === 'POST',
    )
    expect(ruleCall?.body).toMatchObject({
      type: 'cpu_high',
      scope_type: 'organization',
      server_id: '',
      group_id: '',
      scope_name: '',
      repeat_interval_seconds: 300,
    })
    const checkCall = seen.find((call) => call.path === '/api/v1/checks' && call.method === 'POST')
    expect(checkCall?.body).toMatchObject({
      kind: 'http',
      config: { method: 'HEAD', expected_codes: ['200'], timeout_seconds: 5 },
      server_id: '',
    })
    const patches = seen.filter(
      (call) => call.path === '/api/v1/channels/c1' && call.method === 'PATCH',
    )
    expect(patches).toHaveLength(2)
    expect(patches[0]?.body).not.toHaveProperty('secret')
    expect(patches[1]?.body).toMatchObject({ secret: { token: 'new-token' } })
    const settingsCall = seen.find(
      (call) => call.path === '/api/v1/settings/alerts' && call.method === 'PUT',
    )
    expect(settingsCall?.body).toEqual({ allow_private_targets: true })
  })
})
