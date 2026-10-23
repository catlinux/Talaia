import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AuthApi } from '../api/auth'
import { ApiError } from '../api/http'
import type { InvitesApi } from '../api/invites'
import i18n from '../i18n'
import { AuthGate } from './AuthGate'

const signedOut = new ApiError('session_required', 401)

function authApi(): AuthApi {
  const unexpected = () => Promise.reject(new Error('unexpected auth call'))
  return {
    session: () => Promise.reject(signedOut),
    login: unexpected,
    startTotpSetup: () =>
      Promise.resolve({ secret: 'JBSWY3DPEHPK3PXP', uri: 'otpauth://totp/Talaia:invitee' }),
    confirmTotpSetup: () => Promise.resolve(['ABCDE-FGHIJ']),
    verifyTotp: () => Promise.resolve(),
    logout: () => Promise.resolve(),
  }
}

function inviteApi(overrides: Partial<InvitesApi> = {}): InvitesApi {
  return {
    list: () => Promise.resolve([]),
    create: () => Promise.reject(new Error('unexpected create')),
    revoke: () => Promise.resolve(),
    info: () => Promise.resolve({ username: 'invitee', expiresAt: '2026-10-30T10:00:00Z' }),
    accept: () =>
      Promise.resolve({ secret: 'JBSWY3DPEHPK3PXP', uri: 'otpauth://totp/Talaia:invitee' }),
    ...overrides,
  }
}

describe('invitation acceptance', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    window.history.replaceState(null, '', '/invite#invite-token')
  })

  afterEach(() => {
    cleanup()
    window.history.replaceState(null, '', '/')
  })

  it('accepts the invitation and continues with the TOTP setup', async () => {
    const accept = vi.fn(() =>
      Promise.resolve({ secret: 'JBSWY3DPEHPK3PXP', uri: 'otpauth://totp/Talaia:invitee' }),
    )
    render(
      <AuthGate api={authApi()} inviteApi={inviteApi({ accept })}>
        <p>the panel</p>
      </AuthGate>,
    )

    expect(await screen.findByRole('heading', { name: 'Accept the invitation' })).toBeTruthy()
    expect(screen.getByText(/Choose the password for invitee/)).toBeTruthy()

    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'a long password' } })
    fireEvent.change(screen.getByLabelText('Repeat the password'), {
      target: { value: 'another password' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }))
    expect(await screen.findByText('The passwords do not match.')).toBeTruthy()
    expect(accept).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('Repeat the password'), {
      target: { value: 'a long password' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }))

    await waitFor(() => expect(accept).toHaveBeenCalledWith('invite-token', 'a long password'))
    expect(await screen.findByRole('heading', { name: 'Set up the second factor' })).toBeTruthy()
    expect(window.location.pathname).toBe('/')
    expect(window.location.hash).toBe('')
  })

  it('shows a clear invalid-token message with a way back to sign in', async () => {
    render(
      <AuthGate
        api={authApi()}
        inviteApi={inviteApi({
          info: () => Promise.reject(new ApiError('invalid_invite', 404)),
        })}
      >
        <p>the panel</p>
      </AuthGate>,
    )

    expect(await screen.findByText('This invitation is not valid or has expired.')).toBeTruthy()
    fireEvent.click(screen.getByRole('link', { name: 'Go to sign in' }))

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeTruthy()
    expect(window.location.pathname).toBe('/')
  })
})
