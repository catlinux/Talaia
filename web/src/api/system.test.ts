import { describe, expect, it, vi } from 'vitest'

import { ApiError } from './http'
import { createSystemApi } from './system'

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const versionPayload = {
  version: '1.3.0',
  commit: 'a'.repeat(40),
  update: {
    current: 'a'.repeat(40),
    latest: 'b'.repeat(40),
    pending: [{ sha: 'bbbbbbb', subject: 'Fix the panel', author: 'Ana' }],
    ci: 'green',
    checked_at: '2026-10-21T08:00:00Z',
    error: '',
    update_available: true,
  },
  status: {
    state: 'succeeded',
    from: 'a'.repeat(40),
    to: 'b'.repeat(40),
    started_at: '2026-10-21T08:01:00Z',
    finished_at: '2026-10-21T08:05:00Z',
    log: ['building', 'done'],
  },
}

describe('createSystemApi', () => {
  it('maps the version, the pending commits and the status', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(jsonResponse(versionPayload)))

    const system = await createSystemApi(fetchImpl).version()

    expect(system).toEqual({
      version: '1.3.0',
      commit: 'a'.repeat(40),
      update: {
        current: 'a'.repeat(40),
        latest: 'b'.repeat(40),
        pending: [{ sha: 'bbbbbbb', subject: 'Fix the panel', author: 'Ana' }],
        ci: 'green',
        checkedAt: '2026-10-21T08:00:00Z',
        error: '',
        updateAvailable: true,
      },
      status: {
        state: 'succeeded',
        from: 'a'.repeat(40),
        to: 'b'.repeat(40),
        startedAt: '2026-10-21T08:01:00Z',
        finishedAt: '2026-10-21T08:05:00Z',
        log: ['building', 'done'],
      },
    })
    expect((fetchImpl.mock.calls[0] as unknown as [string])[0]).toBe('/api/v1/system/version')
  })

  it('fills the optional fields and hides an unknown state', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          version: 'dev',
          commit: '',
          update: {
            current: '',
            latest: '',
            ci: 'something',
            checked_at: '',
            update_available: false,
          },
          status: { state: 'exploded' },
        }),
      ),
    )

    const system = await createSystemApi(fetchImpl).version()

    expect(system.update.pending).toEqual([])
    expect(system.update.error).toBe('')
    expect(system.update.ci).toBe('red')
    expect(system.status).toEqual({
      state: 'unknown',
      from: '',
      to: '',
      startedAt: '',
      finishedAt: '',
      log: [],
    })
  })

  it('asks for a check now and maps the refreshed version', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(jsonResponse(versionPayload)))

    const system = await createSystemApi(fetchImpl).check()

    expect(system.update.latest).toBe('b'.repeat(40))
    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/system/check')
    expect(init.method).toBe('POST')
    expect(init.body).toBe('{}')
  })

  it('keeps a rate-limited check typed', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(jsonResponse({ error: { code: 'rate_limited' } }, 429)),
    )

    await expect(createSystemApi(fetchImpl).check()).rejects.toMatchObject({
      name: 'ApiError',
      code: 'rate_limited',
      status: 429,
    })
  })

  it('queues an update with the confirmation the API needs', async () => {
    const target = 'b'.repeat(40)
    const fetchImpl = vi.fn(() => Promise.resolve(jsonResponse({ target }, 202)))

    const result = await createSystemApi(fetchImpl).update(target)

    expect(result).toEqual({ target })
    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/system/update')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ confirm: true, target }))
  })

  it('keeps the update error codes typed', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(jsonResponse({ error: { code: 'update_check_pending' } }, 503)),
    )

    await expect(createSystemApi(fetchImpl).version()).rejects.toMatchObject({
      name: 'ApiError',
      code: 'update_check_pending',
      status: 503,
    })
    await expect(createSystemApi(fetchImpl).version()).rejects.toBeInstanceOf(ApiError)
  })
})
