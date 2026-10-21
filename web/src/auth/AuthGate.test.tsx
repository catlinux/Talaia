import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PasskeysUnavailableError, type AuthApi } from '../api/auth'
import { ApiError } from '../api/http'
import i18n from '../i18n'
import { AuthGate } from './AuthGate'
import { DEVICE_STORAGE_KEY } from './PinLogin'

const signedOut = new ApiError('session_required', 401)
const originalCredentials = navigator.credentials

/** enablePasskeys installs the browser APIs a passkey flow needs. */
function enablePasskeys(
  getImpl: (options: CredentialRequestOptions) => Promise<Credential | null>,
) {
  Object.defineProperty(window, 'PublicKeyCredential', {
    value: class {},
    configurable: true,
  })
  const get = vi.fn(getImpl)
  Object.defineProperty(navigator, 'credentials', {
    value: { get, create: vi.fn() },
    configurable: true,
  })
  return get
}

/** mockCredential builds the subset the passkey flow serializes. */
function mockCredential(): PublicKeyCredential {
  return {
    id: 'credential',
    rawId: new Uint8Array([1, 2]).buffer,
    type: 'public-key',
    response: {
      clientDataJSON: new Uint8Array([3]).buffer,
      authenticatorData: new Uint8Array([4]).buffer,
      signature: new Uint8Array([5]).buffer,
      userHandle: new Uint8Array([6]).buffer,
    },
    getClientExtensionResults: () => ({}),
  } as unknown as PublicKeyCredential
}

/** fakeApi builds an AuthApi; the tests override what they need. */
function fakeApi(overrides: Partial<AuthApi> = {}): AuthApi {
  return {
    session: () => Promise.resolve({ username: 'admin', stage: 'full' }),
    login: () => Promise.reject(new Error('unexpected login')),
    startTotpSetup: () => Promise.reject(new Error('unexpected setup')),
    confirmTotpSetup: () => Promise.reject(new Error('unexpected confirmation')),
    verifyTotp: () => Promise.reject(new Error('unexpected verification')),
    logout: () => Promise.resolve(),
    ...overrides,
  }
}

describe('AuthGate', () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    Object.defineProperty(navigator, 'credentials', {
      value: originalCredentials,
      configurable: true,
    })
    Reflect.deleteProperty(window, 'PublicKeyCredential')
    cleanup()
  })

  it('shows a status while it asks for the session, then the panel', async () => {
    render(
      <AuthGate api={fakeApi()}>
        <p>the panel</p>
      </AuthGate>,
    )

    expect(screen.getByRole('status').textContent).toBe('Checking the session…')
    expect(await screen.findByText('the panel')).toBeTruthy()
    expect(screen.queryByRole('heading', { name: 'Sign in' })).toBeNull()
  })

  it('asks for the login when there is no session', async () => {
    render(
      <AuthGate
        api={fakeApi({ session: () => Promise.reject(new ApiError('session_required', 401)) })}
      >
        <p>the panel</p>
      </AuthGate>,
    )

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeTruthy()
    expect(screen.queryByText('the panel')).toBeNull()
  })

  it('keeps an incomplete session out of the panel', async () => {
    render(
      <AuthGate
        api={fakeApi({
          session: () => Promise.resolve({ username: 'admin', stage: 'mfa_pending' }),
        })}
      >
        <p>the panel</p>
      </AuthGate>,
    )

    expect(await screen.findByRole('heading', { name: 'Verify the second factor' })).toBeTruthy()
    expect(screen.queryByText('the panel')).toBeNull()
  })

  it('shows the panel only with a full session', async () => {
    render(
      <AuthGate
        api={fakeApi({ session: () => Promise.resolve({ username: 'admin', stage: 'full' }) })}
      >
        <p>the panel</p>
      </AuthGate>,
    )

    expect(await screen.findByText('the panel')).toBeTruthy()
  })

  it('uses the remembered device PIN first and then completes MFA', async () => {
    localStorage.setItem(DEVICE_STORAGE_KEY, JSON.stringify({ name: 'laptop' }))
    const session = vi
      .fn<() => Promise<{ username: string; stage: 'mfa_pending' | 'full' }>>()
      .mockRejectedValueOnce(signedOut)
      .mockResolvedValue({ username: 'admin', stage: 'mfa_pending' })
    const pinLogin = vi.fn(() => Promise.resolve('totp' as const))

    render(
      <AuthGate api={fakeApi({ session, pinLogin })}>
        <p>the panel</p>
      </AuthGate>,
    )

    const pin = await screen.findByLabelText('PIN')
    expect(pin.getAttribute('inputmode')).toBe('numeric')
    expect(pin.getAttribute('autocomplete')).toBe('off')
    expect(screen.getByText(/laptop/)).toBeTruthy()

    fireEvent.change(pin, { target: { value: '123456' } })
    fireEvent.submit(screen.getByRole('form'))

    expect(pinLogin).toHaveBeenCalledWith('123456')
    expect(await screen.findByRole('heading', { name: 'Verify the second factor' })).toBeTruthy()
    expect(session).toHaveBeenCalledTimes(2)
    expect(localStorage.getItem(DEVICE_STORAGE_KEY)).not.toBeNull()
  })

  it('opens the panel when the PIN starts a full session', async () => {
    localStorage.setItem(DEVICE_STORAGE_KEY, JSON.stringify({ name: 'laptop' }))
    const session = vi
      .fn<() => Promise<{ username: string; stage: 'mfa_pending' | 'full' }>>()
      .mockRejectedValueOnce(signedOut)
      .mockResolvedValue({ username: 'admin', stage: 'full' })
    const pinLogin = vi.fn(() => Promise.resolve('full' as const))

    render(
      <AuthGate api={fakeApi({ session, pinLogin })}>
        <p>the panel</p>
      </AuthGate>,
    )

    fireEvent.change(await screen.findByLabelText('PIN'), { target: { value: '123456' } })
    fireEvent.submit(screen.getByRole('form'))

    expect(await screen.findByText('the panel')).toBeTruthy()
    expect(pinLogin).toHaveBeenCalledWith('123456')
  })

  it('falls back to the password form from the PIN form', async () => {
    localStorage.setItem(DEVICE_STORAGE_KEY, JSON.stringify({ name: 'laptop' }))
    const pinLogin = vi.fn(() => Promise.reject(new ApiError('invalid_credentials', 401)))

    render(
      <AuthGate api={fakeApi({ session: () => Promise.reject(signedOut), pinLogin })}>
        <p>the panel</p>
      </AuthGate>,
    )

    fireEvent.change(await screen.findByLabelText('PIN'), { target: { value: '123456' } })
    fireEvent.submit(screen.getByRole('form'))

    expect(await screen.findByText('The PIN is not right.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Use password' }))

    expect(await screen.findByLabelText('Username')).toBeTruthy()
    expect(screen.queryByLabelText('PIN')).toBeNull()
  })

  it('forgets the device after five wrong PINs', async () => {
    localStorage.setItem(DEVICE_STORAGE_KEY, JSON.stringify({ name: 'laptop' }))
    const pinLogin = vi.fn(() => Promise.reject(new ApiError('invalid_credentials', 401)))

    render(
      <AuthGate api={fakeApi({ session: () => Promise.reject(signedOut), pinLogin })}>
        <p>the panel</p>
      </AuthGate>,
    )

    for (let attempt = 0; attempt < 5; attempt += 1) {
      fireEvent.change(await screen.findByLabelText('PIN'), { target: { value: '123456' } })
      fireEvent.submit(screen.getByRole('form'))
      if (attempt < 4) {
        await screen.findByText('The PIN is not right.')
      }
    }

    expect(await screen.findByLabelText('Username')).toBeTruthy()
    expect(localStorage.getItem(DEVICE_STORAGE_KEY)).toBeNull()
    expect(pinLogin).toHaveBeenCalledTimes(5)
  })

  it('signs in with a passkey through navigator.credentials', async () => {
    const get = enablePasskeys(() => Promise.resolve(mockCredential()))
    const session = vi
      .fn<() => Promise<{ username: string; stage: 'mfa_pending' | 'full' }>>()
      .mockRejectedValueOnce(signedOut)
      .mockResolvedValue({ username: 'admin', stage: 'full' })
    const beginPasskeyLogin = vi.fn(() =>
      Promise.resolve({
        ceremonyID: 'ceremony-1',
        options: { publicKey: { challenge: 'AQID', rpId: 'panel.example.com' } },
      }),
    )
    const finishPasskeyLogin = vi.fn(() => Promise.resolve())

    render(
      <AuthGate api={fakeApi({ session, beginPasskeyLogin, finishPasskeyLogin })}>
        <p>the panel</p>
      </AuthGate>,
    )

    fireEvent.click(
      await screen.findByRole('button', {
        name: 'Sign in with fingerprint or PIN of this device',
      }),
    )

    await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
    const options = get.mock.calls[0]?.[0]
    const challenge = options?.publicKey?.challenge as ArrayBuffer
    expect([...new Uint8Array(challenge)]).toEqual([1, 2, 3])
    await waitFor(() =>
      expect(finishPasskeyLogin).toHaveBeenCalledWith('ceremony-1', expect.any(Object)),
    )
    expect(await screen.findByText('the panel')).toBeTruthy()
  })

  it('ignores a cancelled passkey prompt', async () => {
    const get = enablePasskeys(() =>
      Promise.reject(new DOMException('cancelled', 'NotAllowedError')),
    )
    const beginPasskeyLogin = vi.fn(() =>
      Promise.resolve({
        ceremonyID: 'ceremony-1',
        options: { publicKey: { challenge: 'AQID', rpId: 'panel.example.com' } },
      }),
    )
    const finishPasskeyLogin = vi.fn(() => Promise.resolve())

    render(
      <AuthGate
        api={fakeApi({
          session: () => Promise.reject(signedOut),
          beginPasskeyLogin,
          finishPasskeyLogin,
        })}
      >
        <p>the panel</p>
      </AuthGate>,
    )

    fireEvent.click(
      await screen.findByRole('button', {
        name: 'Sign in with fingerprint or PIN of this device',
      }),
    )

    await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
    expect(screen.queryByRole('alert')).toBeNull()
    expect(
      screen.getByRole('button', { name: 'Sign in with fingerprint or PIN of this device' }),
    ).toBeTruthy()
    expect(finishPasskeyLogin).not.toHaveBeenCalled()
  })

  it('hides the passkey button when the deployment has no passkeys', async () => {
    enablePasskeys(() => Promise.resolve(null))
    const beginPasskeyLogin = vi.fn(() => Promise.reject(new PasskeysUnavailableError()))

    render(
      <AuthGate
        api={fakeApi({
          session: () => Promise.reject(signedOut),
          beginPasskeyLogin,
          finishPasskeyLogin: vi.fn(() => Promise.resolve()),
        })}
      >
        <p>the panel</p>
      </AuthGate>,
    )

    fireEvent.click(
      await screen.findByRole('button', {
        name: 'Sign in with fingerprint or PIN of this device',
      }),
    )

    await waitFor(() =>
      expect(
        screen.queryByRole('button', {
          name: 'Sign in with fingerprint or PIN of this device',
        }),
      ).toBeNull(),
    )
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('does not show the passkey button without WebAuthn support', async () => {
    render(
      <AuthGate api={fakeApi({ session: () => Promise.reject(signedOut) })}>
        <p>the panel</p>
      </AuthGate>,
    )

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeTruthy()
    expect(
      screen.queryByRole('button', {
        name: 'Sign in with fingerprint or PIN of this device',
      }),
    ).toBeNull()
  })
})
