import { describe, expect, it, vi } from 'vitest'

import { ApiError } from './http'
import { createUsersApi } from './users'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api users client', () => {
  it('maps users, bindings and scopes', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        json({
          users: [
            {
              id: 'u1',
              username: 'alice',
              display_name: 'Alice',
              email: 'alice@example.com',
              disabled: false,
              totp_enabled: true,
              bindings: [
                {
                  id: 'b1',
                  role: 'server_operator',
                  scope: { type: 'server', server_id: 'srv1' },
                },
              ],
            },
          ],
        }),
      ),
    )
    const api = createUsersApi(fetchImpl as unknown as typeof fetch)

    await expect(api.list()).resolves.toEqual([
      {
        id: 'u1',
        username: 'alice',
        displayName: 'Alice',
        email: 'alice@example.com',
        disabled: false,
        totpEnabled: true,
        bindings: [
          {
            id: 'b1',
            role: 'server_operator',
            scope: { type: 'server', serverId: 'srv1' },
          },
        ],
      },
    ])
  })

  it('sends assignments and edits through the core', async () => {
    const fetchImpl = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      void init
      const path = String(input)
      if (path === '/api/v1/users/u1/bindings') {
        return Promise.resolve(
          json({ id: 'b2', role: 'viewer', scope: { type: 'organization' } }, 201),
        )
      }
      return Promise.resolve(new Response(null, { status: 204 }))
    })
    const api = createUsersApi(fetchImpl as unknown as typeof fetch)

    await api.update('u1', 'Alice', 'alice@example.com')
    await api.addBinding('u1', 'viewer', { type: 'organization' })
    await api.removeBinding('b1')
    await api.delete('u/1', 'alice')

    const patchCall = fetchImpl.mock.calls.find(([, init]) => init?.method === 'PATCH')
    expect(JSON.parse(String(patchCall?.[1]?.body))).toEqual({
      display_name: 'Alice',
      email: 'alice@example.com',
    })
    const addCall = fetchImpl.mock.calls.find(([, init]) => init?.method === 'POST')
    expect(JSON.parse(String(addCall?.[1]?.body))).toEqual({
      role: 'viewer',
      scope: { type: 'organization', server_id: '', group_id: '', name: '' },
    })
    expect(fetchImpl.mock.calls.some(([, init]) => init?.method === 'DELETE')).toBe(true)
    const deleteCall = fetchImpl.mock.calls.find(([path]) => String(path) === '/api/v1/users/u%2F1')
    expect(deleteCall?.[1]).toEqual(
      expect.objectContaining({
        method: 'DELETE',
        body: JSON.stringify({ confirm_username: 'alice' }),
      }),
    )
  })

  it('maps last_admin to the shared error code', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(json({ error: { code: 'last_admin' } }, 409)))
    const api = createUsersApi(fetchImpl as unknown as typeof fetch)

    await expect(api.disable('u1')).rejects.toMatchObject({
      code: 'last_admin',
      status: 409,
    } satisfies Partial<ApiError>)
  })
})
