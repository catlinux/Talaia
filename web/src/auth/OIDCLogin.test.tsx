import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import type { AuthApi } from '../api/auth'
import { ApiError } from '../api/http'
import type { OIDCApi } from '../api/oidc'
import i18n from '../i18n'
import { AuthGate } from './AuthGate'

const signedOut = new ApiError('session_required', 401)

function authApi(): AuthApi {
  const unexpected = () => Promise.reject(new Error('unexpected auth call'))
  return {
    session: () => Promise.reject(signedOut),
    login: unexpected,
    startTotpSetup: unexpected,
    confirmTotpSetup: unexpected,
    verifyTotp: unexpected,
    logout: () => Promise.resolve(),
  }
}

function oidcApi(overrides: Partial<OIDCApi> = {}): OIDCApi {
  return {
    loginConfig: () => Promise.resolve({ enabled: true, label: 'Company SSO' }),
    settings: () => Promise.reject(new Error('unexpected settings')),
    update: () => Promise.reject(new Error('unexpected update')),
    ...overrides,
  }
}

describe('single sign-on login', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    window.history.replaceState(null, '', '/')
  })

  afterEach(() => {
    cleanup()
    window.history.replaceState(null, '', '/')
  })

  it('shows the configured SSO button next to the password form', async () => {
    render(
      <AuthGate api={authApi()} oidcApi={oidcApi()}>
        <p>panel</p>
      </AuthGate>,
    )

    expect(await screen.findByRole('button', { name: 'Sign in with Company SSO' })).toBeTruthy()
    expect(screen.getByRole('form')).toBeTruthy()
  })

  it('keeps the normal login when the OIDC config fails', async () => {
    render(
      <AuthGate
        api={authApi()}
        oidcApi={oidcApi({ loginConfig: () => Promise.reject(new Error('unavailable')) })}
      >
        <p>panel</p>
      </AuthGate>,
    )

    expect(await screen.findByLabelText('Username')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Sign in with/ })).toBeNull()
  })

  it('shows a neutral line after a failed provider round trip', async () => {
    window.history.replaceState(null, '', '/login?oidc_error=oidc_failed')
    render(
      <AuthGate
        api={authApi()}
        oidcApi={oidcApi({ loginConfig: () => Promise.resolve({ enabled: false, label: '' }) })}
      >
        <p>panel</p>
      </AuthGate>,
    )

    expect(
      await screen.findByText(
        'Single sign-on could not be completed. Try again or use your password.',
      ),
    ).toBeTruthy()
    expect(screen.getByLabelText('Username')).toBeTruthy()
  })
})
