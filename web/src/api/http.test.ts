import { describe, expect, it } from 'vitest'

import { ApiError, createHttpClient } from './http'

describe('HTTP API errors', () => {
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
})
