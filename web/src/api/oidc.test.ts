import { describe, expect, it, vi } from 'vitest'

import { ApiError } from './http'
import { createOIDCApi } from './oidc'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api oidc client', () => {
  it('reads the public login configuration', async () => {
    const fetchImpl = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) => {
      void _input
      void _init
      return Promise.resolve(json({ enabled: true, label: 'Example SSO' }))
    })
    const api = createOIDCApi(fetchImpl as unknown as typeof fetch)

    await expect(api.loginConfig()).resolves.toEqual({ enabled: true, label: 'Example SSO' })
    expect(String(fetchImpl.mock.calls[0]?.[0])).toBe('/api/v1/auth/oidc/config')
  })

  it('maps settings and never expects the secret back', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        json({
          enabled: true,
          issuer: 'https://id.example.com',
          client_id: 'client',
          has_secret: true,
          button_label: 'Company SSO',
          redirect_url: 'https://panel.example.com/api/v1/auth/oidc/callback',
        }),
      ),
    )
    const api = createOIDCApi(fetchImpl as unknown as typeof fetch)

    await expect(api.settings()).resolves.toEqual({
      enabled: true,
      issuer: 'https://id.example.com',
      clientId: 'client',
      hasSecret: true,
      buttonLabel: 'Company SSO',
      redirectUrl: 'https://panel.example.com/api/v1/auth/oidc/callback',
    })
  })

  it('sends the write-only secret and maps the discovery error', async () => {
    const fetchImpl = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      void init
      return Promise.resolve(json({ error: { code: 'oidc_discovery_failed' } }, 422))
    })
    const api = createOIDCApi(fetchImpl as unknown as typeof fetch)

    await expect(
      api.update({
        enabled: true,
        issuer: 'https://id.example.com',
        clientId: 'client',
        clientSecret: 'new-secret',
        buttonLabel: 'Company SSO',
      }),
    ).rejects.toMatchObject({
      code: 'oidc_discovery_failed',
      status: 422,
    } satisfies Partial<ApiError>)
    expect(JSON.parse(String(fetchImpl.mock.calls[0]?.[1]?.body))).toEqual({
      enabled: true,
      issuer: 'https://id.example.com',
      client_id: 'client',
      client_secret: 'new-secret',
      button_label: 'Company SSO',
    })
  })
})
