import { describe, expect, it, vi } from 'vitest'

import { ApiError, createHttpClient, isSignedOut, onVersionMismatch } from './http'

describe('HTTP API errors', () => {
  it.each(['account_disabled', 'session_revoked'] as const)('treats %s as signed out', (code) => {
    expect(isSignedOut(new ApiError(code, 401))).toBe(true)
  })

  it('preserves optional field and reason strings from invalid request bodies', async () => {
    const client = createHttpClient(
      async () =>
        new Response(
          JSON.stringify({
            error: { code: 'invalid_request', field: 'scope.name', reason: 'required' },
          }),
          { status: 400 },
        ),
    )

    await expect(client.post('/api/v1/invites', {})).rejects.toMatchObject({
      code: 'invalid_request',
      field: 'scope.name',
      reason: 'required',
    })
  })

  it('ignores non-string validation details and keeps the two-argument constructor', async () => {
    const client = createHttpClient(
      async () =>
        new Response(
          JSON.stringify({ error: { code: 'invalid_request', field: 3, reason: null } }),
          {
            status: 400,
          },
        ),
    )

    await expect(client.post('/api/v1/invites', {})).rejects.toSatisfy(
      (error: unknown) =>
        error instanceof ApiError &&
        error.field === undefined &&
        error.reason === undefined &&
        error.status === 400,
    )
    expect(new ApiError('invalid_request', 400).field).toBeUndefined()
  })

  it('notifies once when the core version differs from the panel', async () => {
    const mismatch = '999.0.0'
    const nextMismatch = '999.0.1'
    let next: () => Response = () => new Response(null, { status: 204 })
    const client = createHttpClient(async () => next())
    const listener = vi.fn()
    const off = onVersionMismatch(listener)

    next = () => new Response(null, { status: 204 })
    await client.get('/api/v1/one')
    expect(listener).not.toHaveBeenCalled()

    next = () =>
      new Response(null, {
        status: 204,
        headers: { 'X-Talaia-Version': __APP_VERSION__ },
      })
    await client.get('/api/v1/two')
    expect(listener).not.toHaveBeenCalled()

    next = () =>
      new Response(null, {
        status: 204,
        headers: { 'X-Talaia-Version': mismatch },
      })
    await client.get('/api/v1/three')
    expect(listener).toHaveBeenCalledTimes(1)
    expect(listener).toHaveBeenCalledWith(mismatch)

    await client.get('/api/v1/four')
    expect(listener).toHaveBeenCalledTimes(1)

    next = () => new Response(null, { status: 204 })
    await client.get('/api/v1/five')
    expect(listener).toHaveBeenCalledTimes(1)

    const failed = createHttpClient(
      async () =>
        new Response(JSON.stringify({ error: { code: 'invalid_request' } }), {
          status: 400,
          headers: { 'X-Talaia-Version': nextMismatch },
        }),
    )
    await expect(failed.post('/api/v1/six', {})).rejects.toBeInstanceOf(ApiError)
    expect(listener).toHaveBeenCalledTimes(2)
    expect(listener).toHaveBeenLastCalledWith(nextMismatch)

    off()
  })
})
