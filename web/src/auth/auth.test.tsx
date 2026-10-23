import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from '../App'
import { createMockFleetClient } from '../api/client'
import { createAuthApi, type AuthApi } from '../api/auth'
import { ApiError } from '../api/http'
import type { MeApi, Permission } from '../api/me'
import i18n from '../i18n'
import { AuthGate } from './AuthGate'
import { DEVICE_STORAGE_KEY } from './PinLogin'

const signedOut = new ApiError('session_required', 401)

const adminPermissions: Permission[] = [
  'container.view',
  'container.logs',
  'container.control',
  'container.terminal',
  'container.remove',
  'stack.view',
  'stack.deploy',
  'stack.edit',
  'server.view',
  'server.remove',
  'server.enroll',
  'server.agent_update',
  'resources.manage',
  'host.services.view',
  'host.services.control',
  'host.journal',
  'host.apt.view',
  'host.apt.upgrade',
  'host.reboot',
  'host.terminal',
  'users.manage',
  'settings.manage',
  'audit.view',
  'recordings.view',
  'apikeys.manage',
]

const adminMe: MeApi = {
  get: () =>
    Promise.resolve({
      user: { id: 'admin', username: 'admin', display_name: 'Admin', email: '' },
      bindings: { bindings: [] },
      permissions: {
        organization: adminPermissions,
        servers: {},
        stacks: {},
        containers: {},
      },
    }),
}

/** fakeApi builds an AuthApi; the tests override what they need. */
function fakeApi(overrides: Partial<AuthApi> = {}): AuthApi {
  return {
    session: () => Promise.reject(signedOut),
    login: () => Promise.reject(new Error('unexpected login')),
    startTotpSetup: () => Promise.reject(new Error('unexpected setup')),
    confirmTotpSetup: () => Promise.reject(new Error('unexpected confirmation')),
    verifyTotp: () => Promise.reject(new Error('unexpected verification')),
    logout: () => Promise.resolve(),
    ...overrides,
  }
}

/** renderPanel mounts the gate around the real panel. */
function renderPanel(api: AuthApi) {
  render(
    <AuthGate api={api} meApi={adminMe}>
      <App client={createMockFleetClient()} />
    </AuthGate>,
  )
}

/** signIn fills the login form and submits it. */
async function signIn(password = 'correct horse battery staple') {
  fireEvent.change(await screen.findByLabelText('Username'), { target: { value: 'admin' } })
  fireEvent.change(screen.getByLabelText('Password'), { target: { value: password } })
  fireEvent.submit(screen.getByRole('form'))
}

describe('panel authentication', () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('signs in through the first-access flow up to the panel', async () => {
    renderPanel(
      fakeApi({
        login: () => Promise.resolve({ next: 'totp_setup' }),
        startTotpSetup: () =>
          Promise.resolve({
            secret: 'JBSWY3DPEHPK3PXP',
            uri: 'otpauth://totp/Talaia:admin?secret=JBSWY3DPEHPK3PXP',
          }),
        confirmTotpSetup: () => Promise.resolve(['ABCDE-FGHIJ', 'KLMNO-PQRST']),
      }),
    )

    await signIn()
    expect(await screen.findByText('JBSW Y3DP EHPK 3PXP')).toBeTruthy()
    expect(
      screen.getByRole('link', { name: 'Open the authenticator app' }).getAttribute('href'),
    ).toBe('otpauth://totp/Talaia:admin?secret=JBSWY3DPEHPK3PXP')
    expect(screen.getByRole('img', { name: 'QR code for the authenticator app' })).toBeTruthy()

    fireEvent.change(screen.getByLabelText('Six-digit code'), { target: { value: '12a3456' } })
    fireEvent.submit(screen.getByRole('form'))

    expect(await screen.findByText('ABCDE-FGHIJ')).toBeTruthy()
    const next = screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement
    expect(next.disabled).toBe(true)

    fireEvent.click(screen.getByRole('checkbox', { name: 'I have saved the codes somewhere safe' }))
    expect(next.disabled).toBe(false)

    fireEvent.click(next)
    expect(await screen.findByText('vps-web-01')).toBeTruthy()
  })

  it('verifies the code of a returning user', async () => {
    const verifyTotp = vi.fn(() => Promise.resolve())
    renderPanel(fakeApi({ login: () => Promise.resolve({ next: 'totp' }), verifyTotp }))

    await signIn()
    expect(await screen.findByRole('heading', { name: 'Verify the second factor' })).toBeTruthy()

    fireEvent.change(screen.getByLabelText('Six-digit code'), { target: { value: '123456' } })
    fireEvent.submit(screen.getByRole('form'))

    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    expect(verifyTotp).toHaveBeenCalledWith({ code: '123456' })
  })

  it('accepts a recovery code instead of the app code', async () => {
    const verifyTotp = vi.fn(() => Promise.resolve())
    renderPanel(fakeApi({ login: () => Promise.resolve({ next: 'totp' }), verifyTotp }))

    await signIn()
    fireEvent.click(
      await screen.findByRole('button', { name: 'I do not have the app: use a recovery code' }),
    )
    fireEvent.change(screen.getByLabelText('Recovery code'), {
      target: { value: 'abcde fghij' },
    })
    fireEvent.submit(screen.getByRole('form'))

    await waitFor(() => expect(verifyTotp).toHaveBeenCalledWith({ recovery_code: 'ABCDE-FGHIJ' }))
  })

  it('shows a generic error for a wrong password', async () => {
    renderPanel(fakeApi({ login: () => Promise.reject(new ApiError('invalid_credentials', 401)) }))

    await signIn('wrong password')

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toBe('The username or the password is not right.')
  })

  it('shows the rate limit error', async () => {
    renderPanel(fakeApi({ login: () => Promise.reject(new ApiError('rate_limited', 429)) }))

    await signIn()

    expect((await screen.findByRole('alert')).textContent).toContain('Too many attempts')
  })

  it('signs out back to the login screen', async () => {
    const logout = vi.fn(() => Promise.resolve())
    renderPanel(
      fakeApi({ session: () => Promise.resolve({ username: 'admin', stage: 'full' }), logout }),
    )

    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Sign out' }))

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeTruthy()
    expect(logout).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('vps-web-01')).toBeNull()
  })

  it('signs in with the PIN of a remembered device', async () => {
    localStorage.setItem(DEVICE_STORAGE_KEY, JSON.stringify({ name: 'laptop' }))
    const session = vi
      .fn<() => Promise<{ username: string; stage: 'mfa_pending' | 'full' }>>()
      .mockRejectedValueOnce(signedOut)
      .mockResolvedValue({ username: 'admin', stage: 'full' })
    const pinLogin = vi.fn(() => Promise.resolve('full' as const))

    renderPanel(fakeApi({ session, pinLogin }))

    fireEvent.change(await screen.findByLabelText('PIN'), { target: { value: '123456' } })
    fireEvent.submit(screen.getByRole('form'))

    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    expect(pinLogin).toHaveBeenCalledWith('123456')
  })

  it('returns to the login when a later call finds no session', async () => {
    renderPanel(fakeApi({ session: () => Promise.resolve({ username: 'admin', stage: 'full' }) }))
    expect(await screen.findByText('vps-web-01')).toBeTruthy()

    // Any call of the panel that answers "no session" sends the user back.
    const stale = createAuthApi(() =>
      Promise.resolve(
        new Response(JSON.stringify({ error: { code: 'session_required' } }), {
          status: 401,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    await stale.session().catch(() => undefined)

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeTruthy()
    expect(screen.queryByText('vps-web-01')).toBeNull()
    expect((await screen.findByRole('status')).textContent).toBe(
      'The session has expired. Sign in again.',
    )
  })
})
