import { describe, expect, it, vi } from 'vitest'

import { createInvitesApi } from './invites'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api invites client', () => {
  it('creates an invitation and keeps the one-time token', async () => {
    const expires = '2026-10-30T10:00:00Z'
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        json(
          {
            id: 'i1',
            token: 'secret-token',
            link_path: '/invite#secret-token',
            expires_at: expires,
          },
          201,
        ),
      ),
    )
    const api = createInvitesApi(fetchImpl as unknown as typeof fetch)

    await expect(
      api.create({
        username: 'alice',
        email: 'alice@example.com',
        role: 'viewer',
        scope: { type: 'organization' },
      }),
    ).resolves.toEqual({
      id: 'i1',
      token: 'secret-token',
      linkPath: '/invite#secret-token',
      expiresAt: expires,
    })
  })

  it('reads and accepts a public invitation', async () => {
    const fetchImpl = vi.fn((input: RequestInfo | URL) => {
      if (String(input).endsWith('/accept')) {
        return Promise.resolve(json({ secret: 'ABCDEF', uri: 'otpauth://totp/Talaia:alice' }))
      }
      return Promise.resolve(json({ username: 'alice', expires_at: '2026-10-30T10:00:00Z' }))
    })
    const api = createInvitesApi(fetchImpl as unknown as typeof fetch)

    await expect(api.info('token')).resolves.toEqual({
      username: 'alice',
      expiresAt: '2026-10-30T10:00:00Z',
    })
    await expect(api.accept('token', 'a long password')).resolves.toEqual({
      secret: 'ABCDEF',
      uri: 'otpauth://totp/Talaia:alice',
    })
  })
})
