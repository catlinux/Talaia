import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import type { AlertsAdminApi } from '../../api/alertsAdmin'
import type { APIKeysApi } from '../../api/apikeys'
import type { AccessApi } from '../../api/auth'
import type { MeApi, MePermissions, MeResponse } from '../../api/me'
import type { OIDCApi } from '../../api/oidc'
import type { SystemVersion } from '../../api/system'
import { PermissionsProvider } from '../../auth/permissions'
import i18n from '../../i18n'
import { Settings, type SettingsApi } from '../Settings'
import { SettingsTabs, readSettingsTab, storeSettingsTab, type SettingsTab } from './SettingsTabs'

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

function alertsApi(): AlertsAdminApi {
  const settings = { allowPrivateTargets: false }
  return {
    listRules: () => Promise.resolve([]),
    createRule: unexpected,
    updateRule: unexpected,
    deleteRule: unexpected,
    listChecks: () => Promise.resolve([]),
    createCheck: unexpected,
    updateCheck: unexpected,
    deleteCheck: unexpected,
    runCheck: unexpected,
    listChannels: () => Promise.resolve([]),
    createChannel: unexpected,
    updateChannel: unexpected,
    deleteChannel: unexpected,
    testChannel: unexpected,
    getSettings: () => Promise.resolve(settings),
    updateSettings: () => Promise.resolve(settings),
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

function mountSettings(permissions: Partial<MePermissions>, props: Parameters<typeof Settings>[0]) {
  return render(
    <PermissionsProvider meApi={meApi(permissions)}>
      <Settings {...props} />
    </PermissionsProvider>,
  )
}

function Harness({ initial = 'account' }: { initial?: SettingsTab }) {
  const [active, setActive] = useState<SettingsTab>(initial)
  const tabs: SettingsTab[] = ['account', 'servers', 'system']
  return <SettingsTabs tabs={tabs} active={active} onChange={setActive} />
}

describe('SettingsTabs', () => {
  beforeEach(async () => {
    sessionStorage.clear()
    await i18n.changeLanguage('en')
  })

  afterEach(() => cleanup())

  it('renders an accessible tablist and moves with the keyboard', async () => {
    render(<Harness />)

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
  })

  it('remembers the selected tab for the browser session', () => {
    storeSettingsTab('system')
    expect(readSettingsTab()).toBe('system')

    sessionStorage.setItem('talaia.settingsTab', 'unknown')
    expect(readSettingsTab()).toBeNull()
  })

  it('shows only the account tab to a user without management permissions', async () => {
    mountSettings({}, { api: settingsApi(), accessApi: accessApi() })

    expect(await screen.findByRole('heading', { name: 'Access' })).toBeTruthy()
    expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual(['Account'])
  })

  it('shows the account and alerts tabs to an alerts manager', async () => {
    mountSettings(
      { organization: ['alerts.manage'] },
      { api: settingsApi(), accessApi: accessApi(), alertsApi: alertsApi() },
    )

    await waitFor(() => {
      expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual([
        'Account',
        'Alerts',
      ])
    })
    fireEvent.click(screen.getByRole('tab', { name: 'Alerts' }))
    expect(await screen.findByRole('heading', { name: 'Alerts' })).toBeTruthy()
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

  it('restores the stored tab once permissions and the system data are ready', async () => {
    storeSettingsTab('system')
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
        alertsApi: alertsApi(),
      },
    )

    expect(await screen.findByRole('heading', { name: 'Version' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'System' }).getAttribute('aria-selected')).toBe('true')
    expect(readSettingsTab()).toBe('system')
  })

  it('lets an administrator open each of the five cards', async () => {
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
        alertsApi: alertsApi(),
      },
    )

    await waitFor(() => {
      expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual([
        'Account',
        'Servers',
        'Alerts',
        'Integrations',
        'System',
      ])
    })
    expect(await screen.findByRole('heading', { name: 'Access' })).toBeTruthy()

    fireEvent.click(screen.getByRole('tab', { name: 'Servers' }))
    expect(await screen.findByRole('heading', { name: 'Servers' })).toBeTruthy()

    fireEvent.click(screen.getByRole('tab', { name: 'Alerts' }))
    expect(await screen.findByRole('heading', { name: 'Alerts' })).toBeTruthy()

    fireEvent.click(screen.getByRole('tab', { name: 'Integrations' }))
    expect(await screen.findByRole('heading', { name: 'API keys' })).toBeTruthy()
    expect(await screen.findByRole('heading', { name: 'Single sign-on' })).toBeTruthy()

    fireEvent.click(screen.getByRole('tab', { name: 'System' }))
    expect(await screen.findByRole('heading', { name: 'Version' })).toBeTruthy()
  })
})
