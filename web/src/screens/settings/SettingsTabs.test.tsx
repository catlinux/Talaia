import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import type { APIKeysApi } from '../../api/apikeys'
import type { AccessApi } from '../../api/auth'
import type { MeApi, MePermissions, MeResponse } from '../../api/me'
import type { OIDCApi } from '../../api/oidc'
import type { SystemVersion } from '../../api/system'
import { PermissionsProvider } from '../../auth/permissions'
import { ToastProvider } from '../../components/ui'
import i18n from '../../i18n'
import { RouterProvider, useRoute } from '../../router'
import { Settings, type SettingsApi } from '../Settings'
import {
  SettingsTabs,
  settingsTabFromPath,
  settingsTabPath,
  type SettingsTab,
} from './SettingsTabs'

const unexpected = () => Promise.reject(new Error('unexpected call'))

const system: SystemVersion = {
  version: '1.2.3',
  commit: 'a'.repeat(40),
  update: {
    current: 'a'.repeat(40),
    latest: '',
    pending: [],
    notes: { en: [], es: [] },
    ci: 'green',
    checkedAt: '2026-10-28T08:00:00Z',
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

function meApi(permissions: Partial<MePermissions> = {}): MeApi {
  const body: MeResponse = {
    user: { id: 'u1', username: 'alice', display_name: 'Alice', email: '' },
    bindings: { bindings: [] },
    permissions: {
      organization: [],
      servers: {},
      stacks: {},
      containers: {},
      ...permissions,
    },
  }
  return { get: () => Promise.resolve(body) }
}

function accessApi(): AccessApi {
  return {
    listDevices: () => Promise.resolve([]),
    createDevice: unexpected,
    removeDevice: unexpected,
    listPasskeys: () => Promise.resolve({ available: false, passkeys: [] }),
    removePasskey: unexpected,
    beginPasskeyRegistration: unexpected,
    finishPasskeyRegistration: unexpected,
    beginPasskeyLogin: unexpected,
    finishPasskeyLogin: unexpected,
    pinLogin: unexpected,
  }
}

function keysApi(): APIKeysApi {
  return { list: () => Promise.resolve([]), create: unexpected, revoke: unexpected }
}

function oidcApi(): OIDCApi {
  return {
    loginConfig: () => Promise.resolve({ enabled: false, label: '' }),
    settings: () =>
      Promise.resolve({
        enabled: false,
        issuer: '',
        clientId: '',
        hasSecret: false,
        buttonLabel: '',
        redirectUrl: '',
      }),
    update: unexpected,
  }
}

function settingsApi(withServers = false, data: SystemVersion = system): SettingsApi {
  return {
    version: () => Promise.resolve(data),
    check: unexpected,
    update: unexpected,
    ...(withServers
      ? {
          listServers: () => Promise.resolve([]),
          removeServer: unexpected,
          updateAgent: unexpected,
          updateAllAgents: unexpected,
        }
      : {}),
  }
}

function mountSettings(
  permissions: Partial<MePermissions>,
  props: Parameters<typeof Settings>[0],
  path = '/settings',
) {
  window.history.replaceState({}, '', path)
  return render(
    <ToastProvider>
      <RouterProvider>
        <PermissionsProvider meApi={meApi(permissions)}>
          <Settings {...props} />
        </PermissionsProvider>
      </RouterProvider>
    </ToastProvider>,
  )
}

function Harness() {
  const { path } = useRoute()
  const active = settingsTabFromPath(path) ?? 'account'
  const tabs: SettingsTab[] = ['account', 'servers', 'system']
  return <SettingsTabs tabs={tabs} active={active} />
}

describe('SettingsTabs', () => {
  beforeEach(async () => {
    window.history.replaceState({}, '', '/settings')
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
    window.history.replaceState({}, '', '/')
  })

  it('renders an accessible tablist and moves with the keyboard', async () => {
    render(
      <RouterProvider>
        <Harness />
      </RouterProvider>,
    )

    const account = screen.getByRole('tab', { name: 'Account' })
    expect(account.getAttribute('aria-selected')).toBe('true')
    expect(account.getAttribute('aria-controls')).toBe('settings-panel-account')
    expect(screen.getByRole('tablist', { name: 'Settings sections' })).toBeTruthy()

    fireEvent.keyDown(account, { key: 'ArrowRight' })
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: 'Servers' }).getAttribute('aria-selected')).toBe(
        'true',
      )
    })
    expect(window.location.pathname).toBe('/settings/servers')

    fireEvent.keyDown(screen.getByRole('tab', { name: 'Servers' }), { key: 'End' })
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: 'System' }).getAttribute('aria-selected')).toBe('true')
    })

    fireEvent.keyDown(screen.getByRole('tab', { name: 'System' }), { key: 'Home' })
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: 'Account' }).getAttribute('aria-selected')).toBe(
        'true',
      )
    })
    expect(window.location.pathname).toBe('/settings')
  })

  it('maps every settings tab to its own address', () => {
    expect(settingsTabPath('account')).toBe('/settings')
    expect(settingsTabPath('system')).toBe('/settings/system')
    expect(settingsTabFromPath('/settings')).toBe('account')
    expect(settingsTabFromPath('/settings/integrations')).toBe('integrations')
    expect(settingsTabFromPath('/settings/alerts')).toBeNull()
  })

  it('shows only the account tab to a user without management permissions', async () => {
    mountSettings({}, { api: settingsApi(), accessApi: accessApi() })

    expect(await screen.findByRole('heading', { name: 'Access' })).toBeTruthy()
    expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual(['Account'])
  })

  it('sends the old /settings/alerts address to the alerts rules tab', async () => {
    mountSettings(
      { organization: ['settings.manage', 'alerts.manage'] },
      { api: settingsApi(), accessApi: accessApi() },
      '/settings/alerts',
    )

    await waitFor(() => expect(window.location.pathname).toBe('/alerts/rules'))
    expect(screen.queryByRole('tab', { name: 'Alerts' })).toBeNull()
  })

  it('keeps the update progress above the tabs whatever tab is selected', async () => {
    const running: SystemVersion = {
      ...system,
      status: { ...system.status, state: 'running', log: ['building'] },
    }
    mountSettings(
      { organization: ['settings.manage'] },
      { api: settingsApi(false, running), accessApi: accessApi() },
    )

    const progress = await screen.findByRole('heading', { name: 'Update progress' })
    const section = progress.closest('section')
    expect(section).not.toBeNull()
    expect(
      section!.compareDocumentPosition(screen.getByRole('tablist')) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Access' })).toBeTruthy()

    fireEvent.click(screen.getByRole('tab', { name: 'System' }))
    expect(await screen.findByRole('heading', { name: 'Version' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Update progress' })).toBeTruthy()
  })

  it('opens the system tab directly from its address once the data is ready', async () => {
    mountSettings(
      {
        organization: ['settings.manage', 'users.manage', 'server.agent_update', 'server.remove'],
      },
      {
        api: settingsApi(true),
        accessApi: accessApi(),
        keysApi: keysApi(),
        oidcApi: oidcApi(),
      },
      '/settings/system',
    )

    expect(await screen.findByRole('heading', { name: 'Version' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'System' }).getAttribute('aria-selected')).toBe('true')
  })

  it('lets an administrator open each of the four cards', async () => {
    mountSettings(
      {
        organization: [
          'settings.manage',
          'users.manage',
          'alerts.manage',
          'server.agent_update',
          'server.remove',
        ],
      },
      {
        api: settingsApi(true),
        accessApi: accessApi(),
        keysApi: keysApi(),
        oidcApi: oidcApi(),
      },
    )

    await waitFor(() => {
      expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual([
        'Account',
        'Servers',
        'Integrations',
        'System',
      ])
    })
    expect(await screen.findByRole('heading', { name: 'Access' })).toBeTruthy()

    fireEvent.click(screen.getByRole('tab', { name: 'Servers' }))
    expect(await screen.findByRole('heading', { name: 'Servers' })).toBeTruthy()
    expect(window.location.pathname).toBe('/settings/servers')

    fireEvent.click(screen.getByRole('tab', { name: 'Integrations' }))
    expect(await screen.findByRole('heading', { name: 'API keys' })).toBeTruthy()
    expect(await screen.findByRole('heading', { name: 'Single sign-on' })).toBeTruthy()

    fireEvent.click(screen.getByRole('tab', { name: 'System' }))
    expect(await screen.findByRole('heading', { name: 'Version' })).toBeTruthy()
    expect(window.location.pathname).toBe('/settings/system')
  })
})
