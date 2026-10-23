import { describe, expect, it, vi } from 'vitest'

import { createAPIKeysApi } from './apikeys'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api api keys client', () => {
  it('lists own and all keys with their bindings', async () => {
    const fetchImpl = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) => {
      void _input
      void _init
      return Promise.resolve(
        json({
          keys: [
            {
              id: 'k1',
              key_id: 'abcdefghijkl',
              name: 'deploy',
              expires_at: '2026-12-01T10:00:00Z',
              last_used_at: null,
              revoked: false,
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
      )
    })
    const api = createAPIKeysApi(fetchImpl as unknown as typeof fetch)

    await expect(api.list()).resolves.toEqual([
      {
        id: 'k1',
        keyId: 'abcdefghijkl',
        name: 'deploy',
        expiresAt: '2026-12-01T10:00:00Z',
        lastUsedAt: null,
        revoked: false,
        bindings: [
          {
            id: 'b1',
            role: 'server_operator',
            scope: { type: 'server', serverId: 'srv1' },
          },
        ],
      },
    ])
    await api.list(true)
    expect(String(fetchImpl.mock.calls.at(-1)?.[0])).toBe('/api/v1/apikeys?all=1')
  })

  it('creates a key with one fresh TOTP code and revokes it by id', async () => {
    const fetchImpl = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      void init
      if (String(input) === '/api/v1/apikeys') {
        return Promise.resolve(
          json(
            {
              id: 'k1',
              key: 'tlk_abcdefghijkl_secret',
              name: 'deploy',
              expires_at: '2026-12-01T10:00:00Z',
            },
            201,
          ),
        )
      }
      return Promise.resolve(new Response(null, { status: 204 }))
    })
    const api = createAPIKeysApi(fetchImpl as unknown as typeof fetch)

    await expect(
      api.create({
        name: 'deploy',
        expiresInDays: 90,
        bindings: [{ role: 'viewer', scope: { type: 'organization' } }],
        totpCode: '123456',
      }),
    ).resolves.toEqual({
      id: 'k1',
      key: 'tlk_abcdefghijkl_secret',
      name: 'deploy',
      expiresAt: '2026-12-01T10:00:00Z',
    })
    const post = fetchImpl.mock.calls.find(([, init]) => init?.method === 'POST')
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({
      name: 'deploy',
      expires_in_days: 90,
      bindings: [
        {
          role: 'viewer',
          scope: { type: 'organization', server_id: '', group_id: '', name: '' },
        },
      ],
      totp_code: '123456',
    })

    await api.revoke('k1')
    expect(String(fetchImpl.mock.calls.at(-1)?.[0])).toBe('/api/v1/apikeys/k1')
    expect(fetchImpl.mock.calls.at(-1)?.[1]?.method).toBe('DELETE')
  })
})
