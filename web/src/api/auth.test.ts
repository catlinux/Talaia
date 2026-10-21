import { describe, expect, it, vi } from 'vitest'

import { createAuthApi, PasskeysUnavailableError } from './auth'
import type { AssertionCredentialJSON, RegistrationCredentialJSON } from '../auth/webauthn'
import { ApiError, isSignedOut, onSignedOut } from './http'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function noContent(): Response {
  return new Response(null, { status: 204 })
}

interface RecordedCall {
  input: RequestInfo | URL
  init?: RequestInit
}

/** fetchSequence answers the calls in order and records them. */
function fetchSequence(...responses: Response[]) {
  const calls: RecordedCall[] = []
  const fetchImpl = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ input, init })
    const response = responses.shift()
    return response === undefined
      ? Promise.reject(new Error('unexpected request'))
      : Promise.resolve(response)
  })
  return { fetchImpl: fetchImpl as unknown as typeof fetch, calls }
}

describe('auth api', () => {
  it('reads the session with the cookie', async () => {
    const { fetchImpl, calls } = fetchSequence(
      jsonResponse(200, { username: 'admin', stage: 'full' }),
    )

    const session = await createAuthApi(fetchImpl).session()

    expect(session).toEqual({ username: 'admin', stage: 'full' })
    expect(calls[0]?.input).toBe('/api/v1/auth/session')
    expect(calls[0]?.init?.method).toBe('GET')
    expect(calls[0]?.init?.credentials).toBe('same-origin')
  })

  it('sends the credentials as JSON on writes', async () => {
    const { fetchImpl, calls } = fetchSequence(jsonResponse(200, { next: 'totp' }))

    await createAuthApi(fetchImpl).login('admin', 'correct horse')

    const init = calls[0]?.init
    expect(calls[0]?.input).toBe('/api/v1/auth/login')
    expect(init?.method).toBe('POST')
    expect(init?.credentials).toBe('same-origin')
    expect(init?.headers).toMatchObject({ 'Content-Type': 'application/json' })
    expect(init?.body).toBe(JSON.stringify({ username: 'admin', password: 'correct horse' }))
  })

  it('returns the recovery codes of the confirmation', async () => {
    const { fetchImpl } = fetchSequence(
      jsonResponse(200, { recovery_codes: ['ABCDE-FGHIJ', 'KLMNO-PQRST'] }),
    )

    await expect(createAuthApi(fetchImpl).confirmTotpSetup('123456')).resolves.toEqual([
      'ABCDE-FGHIJ',
      'KLMNO-PQRST',
    ])
  })

  it('sends a recovery code when asked for one', async () => {
    const { fetchImpl, calls } = fetchSequence(noContent())

    await createAuthApi(fetchImpl).verifyTotp({ recovery_code: 'ABCDE-FGHIJ' })

    expect(calls[0]?.init?.body).toBe(JSON.stringify({ recovery_code: 'ABCDE-FGHIJ' }))
  })

  it('accepts a 204 without a body', async () => {
    const { fetchImpl, calls } = fetchSequence(noContent())

    await expect(createAuthApi(fetchImpl).logout()).resolves.toBeUndefined()
    expect(calls[0]?.input).toBe('/api/v1/auth/logout')
    expect(calls[0]?.init?.body).toBe(JSON.stringify({}))
  })

  it('turns the error envelope into an ApiError with the code', async () => {
    for (const [status, code] of [
      [401, 'invalid_credentials'],
      [429, 'rate_limited'],
      [403, 'forbidden_origin'],
      [400, 'weak_password'],
    ] as const) {
      const { fetchImpl } = fetchSequence(jsonResponse(status, { error: { code } }))

      const error = await createAuthApi(fetchImpl)
        .login('admin', 'wrong')
        .catch((cause: unknown) => cause)

      expect(error).toBeInstanceOf(ApiError)
      expect((error as ApiError).code).toBe(code)
      expect((error as ApiError).status).toBe(status)
    }
  })

  it('uses a generic code for an unknown error', async () => {
    const { fetchImpl } = fetchSequence(jsonResponse(500, { error: { code: 'surprise' } }))

    const error = await createAuthApi(fetchImpl)
      .session()
      .catch((cause: unknown) => cause)

    expect((error as ApiError).code).toBe('internal')
  })

  it('reports a failed connection as a network error', async () => {
    const fetchImpl = vi.fn(() => Promise.reject(new TypeError('failed to fetch')))

    const error = await createAuthApi(fetchImpl as unknown as typeof fetch)
      .session()
      .catch((cause: unknown) => cause)

    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).code).toBe('network')
  })

  it('tells a signed-out answer apart from a wrong password', async () => {
    const { fetchImpl } = fetchSequence(
      jsonResponse(401, { error: { code: 'session_required' } }),
      jsonResponse(401, { error: { code: 'invalid_credentials' } }),
    )
    const api = createAuthApi(fetchImpl)

    const signedOut = await api.session().catch((cause: unknown) => cause)
    const wrongPassword = await api.login('admin', 'wrong').catch((cause: unknown) => cause)

    expect(isSignedOut(signedOut)).toBe(true)
    expect(isSignedOut(wrongPassword)).toBe(false)
  })

  it('notifies the listeners when the session is gone', async () => {
    const { fetchImpl } = fetchSequence(jsonResponse(401, { error: { code: 'mfa_required' } }))
    const listener = vi.fn()
    const stop = onSignedOut(listener)

    try {
      await createAuthApi(fetchImpl)
        .session()
        .catch(() => undefined)
      expect(listener).toHaveBeenCalledTimes(1)

      stop()
      const again = fetchSequence(jsonResponse(401, { error: { code: 'session_required' } }))
      await createAuthApi(again.fetchImpl)
        .session()
        .catch(() => undefined)
      expect(listener).toHaveBeenCalledTimes(1)
    } finally {
      stop()
    }
  })

  it('signs in with a PIN without treating mfa_required as a lost session', async () => {
    const { fetchImpl, calls } = fetchSequence(
      noContent(),
      jsonResponse(401, { error: { code: 'mfa_required' } }),
      jsonResponse(401, { error: { code: 'invalid_credentials' } }),
    )
    const api = createAuthApi(fetchImpl)
    const listener = vi.fn()
    const stop = onSignedOut(listener)

    try {
      await expect(api.pinLogin('123456')).resolves.toBe('full')
      expect(calls[0]?.input).toBe('/api/v1/auth/pin/login')
      expect(calls[0]?.init?.method).toBe('POST')
      expect(calls[0]?.init?.headers).toMatchObject({ 'Content-Type': 'application/json' })
      expect(calls[0]?.init?.body).toBe(JSON.stringify({ pin: '123456' }))

      await expect(api.pinLogin('123456')).resolves.toBe('totp')
      expect(listener).not.toHaveBeenCalled()

      const wrong = await api.pinLogin('000000').catch((cause: unknown) => cause)
      expect(wrong).toBeInstanceOf(ApiError)
      expect((wrong as ApiError).code).toBe('invalid_credentials')
    } finally {
      stop()
    }
  })

  it('lists, creates and removes trusted devices', async () => {
    const { fetchImpl, calls } = fetchSequence(
      jsonResponse(200, {
        devices: [
          {
            id: 'device-1',
            name: 'laptop',
            created_at: '2026-10-21T08:00:00Z',
            last_used_at: null,
            current: true,
          },
        ],
      }),
      jsonResponse(201, {
        id: 'device-2',
        name: 'desktop',
        created_at: '2026-10-21T09:00:00Z',
        last_used_at: null,
        current: true,
      }),
      noContent(),
    )
    const api = createAuthApi(fetchImpl)

    await expect(api.listDevices()).resolves.toEqual([
      {
        id: 'device-1',
        name: 'laptop',
        createdAt: '2026-10-21T08:00:00Z',
        lastUsedAt: null,
        current: true,
      },
    ])
    await expect(api.createDevice('password', '123456', 'desktop')).resolves.toMatchObject({
      id: 'device-2',
      name: 'desktop',
      current: true,
    })
    expect(calls[1]?.init?.body).toBe(
      JSON.stringify({ password: 'password', pin: '123456', name: 'desktop' }),
    )

    await api.removeDevice('device 2')
    expect(calls[2]?.input).toBe('/api/v1/auth/devices/device%202')
    expect(calls[2]?.init?.method).toBe('DELETE')
  })

  it('lists, begins and removes passkeys', async () => {
    const { fetchImpl, calls } = fetchSequence(
      jsonResponse(200, {
        available: true,
        passkeys: [
          {
            id: 'passkey-1',
            name: 'laptop',
            created_at: '2026-10-21T08:00:00Z',
            last_used_at: '2026-10-21T09:00:00Z',
            backup_state: true,
          },
        ],
      }),
      jsonResponse(200, {
        publicKey: {
          challenge: 'AQID',
          rp: { id: 'panel.example.com', name: 'Talaia' },
          user: { id: 'BAUG', name: 'admin', displayName: 'admin' },
          pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
        },
      }),
      jsonResponse(201, {
        id: 'passkey-2',
        name: 'desktop',
        created_at: '2026-10-21T10:00:00Z',
        last_used_at: null,
        backup_state: false,
      }),
      noContent(),
    )
    const api = createAuthApi(fetchImpl)

    await expect(api.listPasskeys()).resolves.toEqual({
      available: true,
      passkeys: [
        {
          id: 'passkey-1',
          name: 'laptop',
          createdAt: '2026-10-21T08:00:00Z',
          lastUsedAt: '2026-10-21T09:00:00Z',
          backupState: true,
        },
      ],
    })
    const options = await api.beginPasskeyRegistration('password')
    expect(options.publicKey.challenge).toBe('AQID')
    expect(calls[1]?.init?.body).toBe(JSON.stringify({ password: 'password' }))

    const credential: RegistrationCredentialJSON = {
      id: 'credential',
      rawId: 'credential',
      type: 'public-key',
      response: {
        clientDataJSON: 'client-data',
        attestationObject: 'attestation',
        transports: ['internal'],
      },
      clientExtensionResults: {},
    }
    await expect(api.finishPasskeyRegistration('desktop', credential)).resolves.toMatchObject({
      id: 'passkey-2',
      name: 'desktop',
      backupState: false,
    })
    await api.removePasskey('passkey 2')
    expect(calls[3]?.input).toBe('/api/v1/auth/passkeys/passkey%202')
    expect(calls[3]?.init?.method).toBe('DELETE')
  })

  it('begins and finishes a passkey login, hiding an unavailable deployment', async () => {
    const { fetchImpl, calls } = fetchSequence(
      jsonResponse(200, {
        ceremony_id: 'ceremony-1',
        options: { publicKey: { challenge: 'AQID', rpId: 'panel.example.com' } },
      }),
      noContent(),
      jsonResponse(503, { error: { code: 'passkeys_unavailable' } }),
    )
    const api = createAuthApi(fetchImpl)

    await expect(api.beginPasskeyLogin()).resolves.toEqual({
      ceremonyID: 'ceremony-1',
      options: { publicKey: { challenge: 'AQID', rpId: 'panel.example.com' } },
    })
    const assertion: AssertionCredentialJSON = {
      id: 'credential',
      rawId: 'credential',
      type: 'public-key',
      response: {
        clientDataJSON: 'client-data',
        authenticatorData: 'authenticator-data',
        signature: 'signature',
      },
      clientExtensionResults: {},
    }
    await api.finishPasskeyLogin('ceremony-1', assertion)
    expect(calls[1]?.init?.body).toBe(
      JSON.stringify({ ceremony_id: 'ceremony-1', credential: assertion }),
    )

    await expect(api.beginPasskeyLogin()).rejects.toBeInstanceOf(PasskeysUnavailableError)
  })
})
