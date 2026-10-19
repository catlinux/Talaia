import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import type { AuthApi } from '../api/auth'
import { ApiError } from '../api/http'
import i18n from '../i18n'
import { AuthGate } from './AuthGate'

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
})
