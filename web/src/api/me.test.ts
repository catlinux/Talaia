import { describe, expect, it, vi } from 'vitest'

import { createMeApi, type MeResponse } from './me'

function response(body: MeResponse, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api me client', () => {
  it('reads the user and the permission tree', async () => {
    const body: MeResponse = {
      user: { id: 'u1', username: 'alice', display_name: 'Alice', email: 'alice@example.com' },
      bindings: { bindings: [] },
      permissions: {
        organization: ['audit.view'],
        servers: { srv1: ['server.view'] },
        stacks: { srv1: { web: ['stack.view', 'stack.deploy'] } },
        containers: { srv1: { web1: ['container.view', 'container.control'] } },
      },
    }
    const fetchImpl = vi.fn(() => Promise.resolve(response(body)))
    const api = createMeApi(fetchImpl as unknown as typeof fetch)

    await expect(api.get()).resolves.toEqual(body)
    expect(fetchImpl).toHaveBeenCalledWith(
      '/api/v1/me',
      expect.objectContaining({ method: 'GET', credentials: 'same-origin' }),
    )
  })

  it('turns an API error into an ApiError', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(response({} as MeResponse, 403)))
    const api = createMeApi(fetchImpl as unknown as typeof fetch)
    await expect(api.get()).rejects.toMatchObject({ code: 'internal', status: 403 })
  })
})
