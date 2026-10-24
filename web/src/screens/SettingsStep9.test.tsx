import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { APIKey, APIKeysApi } from '../api/apikeys'
import type { Group, GroupsApi } from '../api/groups'
import { ApiError } from '../api/http'
import type { OIDCApi, OIDCSettings } from '../api/oidc'
import type { SystemVersion } from '../api/system'
import i18n from '../i18n'
import { Settings, type SettingsApi } from './Settings'

const system: SystemVersion = {
  version: '1.2.3',
  commit: 'a'.repeat(40),
  update: {
    current: 'a'.repeat(40),
    latest: 'a'.repeat(40),
    pending: [],
    notes: { en: [], es: [] },
    ci: 'green',
    checkedAt: '2026-10-21T08:00:00Z',
    error: '',
    updateAvailable: false,
  },
  status: {
    state: 'idle',
    from: '',
    to: '',
    startedAt: '',
    finishedAt: '',
    log: [],
  },
}

const systemApi: SettingsApi = {
  version: () => Promise.resolve(system),
  check: () => Promise.reject(new Error('unexpected check')),
  update: () => Promise.reject(new Error('unexpected update')),
}

function fakeGroups(): GroupsApi {
  return {
    list: () => Promise.resolve([] as Group[]),
    create: () => Promise.reject(new Error('unexpected')),
    rename: () => Promise.reject(new Error('unexpected')),
    remove: () => Promise.reject(new Error('unexpected')),
    setServerGroup: () => Promise.reject(new Error('unexpected')),
  }
}

function fakeKeys(): APIKeysApi {
  return {
    list: () => Promise.resolve([] as APIKey[]),
    create: () =>
      Promise.resolve({
        id: 'k1',
        key: 'tlk_abcdefghijkl_secret',
        name: 'deploy',
        expiresAt: '2026-12-01T10:00:00Z',
      }),
    revoke: () => Promise.resolve(),
  }
}

function oidcSettings(overrides: Partial<OIDCSettings> = {}): OIDCSettings {
  return {
    enabled: true,
    issuer: 'https://id.example.com',
    clientId: 'client',
    hasSecret: true,
    buttonLabel: 'Company SSO',
    redirectUrl: 'https://panel.example.com/api/v1/auth/oidc/callback',
    ...overrides,
  }
}

function fakeOIDC(overrides: Partial<OIDCApi> = {}): OIDCApi {
  return {
    loginConfig: () => Promise.resolve({ enabled: true, label: 'Company SSO' }),
    settings: () => Promise.resolve(oidcSettings()),
    update: (input) => Promise.resolve(oidcSettings({ enabled: input.enabled })),
    ...overrides,
  }
}

describe('Settings API keys and single sign-on', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => cleanup())

  it('shows a new API key once and forgets it when the dialog closes', async () => {
    const keys = fakeKeys()
    const create = vi.spyOn(keys, 'create')
    render(<Settings api={systemApi} keysApi={keys} groupsApi={fakeGroups()} />)

    expect(await screen.findByRole('heading', { name: 'API keys' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))

    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText(/^Name/), { target: { value: 'deploy' } })
    fireEvent.change(within(dialog).getByLabelText(/Authenticator code/), {
      target: { value: '123456' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create key' }))

    expect(await screen.findByText('tlk_abcdefghijkl_secret')).toBeTruthy()
    expect(screen.getByText(/Copy this key now/)).toBeTruthy()
    const origin = window.location.origin
    const unixCommand = `curl -H "Authorization: Bearer tlk_abcdefghijkl_secret" ${origin}/api/v1/servers`
    const powershellCommand = `Invoke-RestMethod -Uri "${origin}/api/v1/servers" -Headers @{ Authorization = "Bearer tlk_abcdefghijkl_secret" }`
    expect(screen.getByText(unixCommand)).toBeTruthy()
    const tabs = screen.getAllByRole('tab')
    const unixTab = tabs.find((tab) => tab.textContent === 'Linux / macOS')!
    const powershellTab = tabs.find((tab) => tab.textContent === 'PowerShell')!
    expect(unixTab.getAttribute('aria-selected')).toBe('true')
    fireEvent.click(powershellTab)
    expect(screen.getByText(powershellCommand)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Copy command' })).toBeTruthy()
    fireEvent.click(unixTab)
    expect(screen.getByText(unixCommand)).toBeTruthy()
    expect(screen.queryByText(/<key>/)).toBeNull()
    expect(create).toHaveBeenCalledWith({
      name: 'deploy',
      expiresInDays: 90,
      bindings: [{ role: 'viewer', scope: { type: 'organization' } }],
      totpCode: '123456',
    })

    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(screen.queryByText('tlk_abcdefghijkl_secret')).toBeNull())

    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))
    expect(screen.queryByText('tlk_abcdefghijkl_secret')).toBeNull()
    expect(screen.getByLabelText(/Authenticator code/)).toBeTruthy()
  })

  it('never prefills the OIDC secret and shows the discovery error', async () => {
    const update = vi.fn(() => Promise.reject(new ApiError('oidc_discovery_failed', 422)))
    const oidc = fakeOIDC({ update })
    render(<Settings api={systemApi} oidcApi={oidc} />)

    expect(await screen.findByRole('heading', { name: 'Single sign-on' })).toBeTruthy()
    const secret = (await screen.findByLabelText(/Client secret/)) as HTMLInputElement
    expect(secret.value).toBe('')
    expect(screen.getByText(/A client secret is set/)).toBeTruthy()
    expect(screen.queryByDisplayValue('stored-secret')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))

    expect((await screen.findByRole('alert')).textContent).toBe(
      'The OpenID Connect provider could not be reached.',
    )
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        clientSecret: '',
        issuer: 'https://id.example.com',
        clientId: 'client',
      }),
    )
  })
})
