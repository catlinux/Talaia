import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../api/http'
import type { OIDCApi, OIDCSettings } from '../../api/oidc'
import { ToastProvider } from '../../components/ui'
import i18n from '../../i18n'
import { OIDCSettingsCard } from './OIDCSettingsCard'

function settings(overrides: Partial<OIDCSettings> = {}): OIDCSettings {
  return {
    enabled: false,
    issuer: '',
    clientId: '',
    hasSecret: false,
    buttonLabel: '',
    redirectUrl: '',
    ...overrides,
  }
}

function fakeOIDC(overrides: Partial<OIDCApi> = {}): OIDCApi {
  return {
    loginConfig: () => Promise.resolve({ enabled: false, label: '' }),
    settings: () => Promise.resolve(settings()),
    update: (input) => Promise.resolve(settings({ enabled: input.enabled })),
    ...overrides,
  }
}

function renderCard(api: OIDCApi) {
  return render(
    <ToastProvider>
      <OIDCSettingsCard api={api} />
    </ToastProvider>,
  )
}

describe('single sign-on settings card', () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18n.changeLanguage('en')
  })

  afterEach(() => cleanup())

  it('warns and hides the redirect URL when the panel has no public URL', async () => {
    const { container } = renderCard(fakeOIDC())

    expect(
      await screen.findByText(
        'The panel has no public URL configured, so OpenID Connect cannot be enabled. Set the public URL of the core and restart it.',
      ),
    ).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Copy redirect URL' })).toBeNull()
    expect(container.querySelector('code')).toBeNull()
  })

  it('disables enabling when there is no public URL and the stored setting is off', async () => {
    renderCard(fakeOIDC())

    const checkbox = (await screen.findByRole('checkbox', {
      name: 'Enable single sign-on',
    })) as HTMLInputElement
    expect(checkbox.disabled).toBe(true)
  })

  it('keeps an already enabled configuration switchable off without a public URL', async () => {
    renderCard(
      fakeOIDC({
        settings: () =>
          Promise.resolve(
            settings({
              enabled: true,
              issuer: 'https://id.example.com',
              clientId: 'client',
              redirectUrl: '',
            }),
          ),
      }),
    )

    const checkbox = (await screen.findByRole('checkbox', {
      name: 'Enable single sign-on',
    })) as HTMLInputElement
    expect(checkbox.disabled).toBe(false)
    expect(checkbox.checked).toBe(true)
    fireEvent.click(checkbox)
    expect(checkbox.checked).toBe(false)
  })

  it('shows the redirect URL and its copy button when configured', async () => {
    const redirectUrl = 'https://panel.example.com/api/v1/auth/oidc/callback'
    renderCard(
      fakeOIDC({
        settings: () => Promise.resolve(settings({ enabled: true, redirectUrl })),
      }),
    )

    expect(await screen.findByText(redirectUrl)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Copy redirect URL' })).toBeTruthy()
    expect(
      (screen.getByRole('checkbox', { name: 'Enable single sign-on' }) as HTMLInputElement)
        .disabled,
    ).toBe(false)
  })

  it('shows the translated public URL error returned by the core', async () => {
    const update = vi.fn(() => Promise.reject(new ApiError('public_url_required', 422)))
    renderCard(
      fakeOIDC({
        settings: () =>
          Promise.resolve(
            settings({
              enabled: true,
              issuer: 'https://id.example.com',
              clientId: 'client',
              redirectUrl: 'https://panel.example.com/api/v1/auth/oidc/callback',
            }),
          ),
        update,
      }),
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Save settings' }))

    expect((await screen.findByRole('alert')).textContent).toBe(
      'Set the panel public URL in the core configuration before enabling OpenID Connect.',
    )
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        enabled: true,
        issuer: 'https://id.example.com',
        clientId: 'client',
      }),
    )
  })
})
