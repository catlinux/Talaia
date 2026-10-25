import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type {
  AlertChannel,
  AlertCheck,
  AlertRule,
  AlertChannelInput,
  AlertRuleInput,
  AlertsAdminApi,
} from '../../api/alertsAdmin'
import type { GroupsApi } from '../../api/groups'
import type { MeApi, MePermissions, Permission } from '../../api/me'
import type { ServerSummary } from '../../api/servers'
import type { SystemVersion } from '../../api/system'
import { PermissionsProvider } from '../../auth/permissions'
import i18n from '../../i18n'
import { Settings } from '../Settings'
import { ChannelForm } from './ChannelForm'
import { ChannelsCard } from './ChannelsCard'
import { ChecksCard } from './ChecksCard'
import { PrivateTargetsCard } from './AlertsSettings'
import { RulesCard } from './RulesCard'

function rule(overrides: Partial<AlertRule> = {}): AlertRule {
  return {
    id: 'r1',
    name: 'CPU high',
    type: 'cpu_high',
    enabled: true,
    severity: 'warning',
    params: { threshold: 90, for_seconds: 0 },
    scopeType: 'organization',
    serverId: null,
    groupId: null,
    scopeName: null,
    channelIds: [],
    repeatIntervalSeconds: 0,
    builtin: false,
    ...overrides,
  }
}

function check(overrides: Partial<AlertCheck> = {}): AlertCheck {
  return {
    id: 'k1',
    name: 'Health',
    kind: 'http',
    target: 'https://example.com/health',
    config: { method: 'GET', expectedCodes: ['200-299'], timeoutSeconds: 5 },
    intervalSeconds: 60,
    enabled: true,
    serverId: null,
    latestResult: null,
    ...overrides,
  }
}

function channel(overrides: Partial<AlertChannel> = {}): AlertChannel {
  return {
    id: 'c1',
    name: 'Ops',
    type: 'telegram',
    enabled: true,
    language: 'en',
    config: {
      chatId: '123',
      server: '',
      priority: null,
      host: '',
      port: null,
      from: '',
      to: [],
      tlsMode: '',
    },
    hasSecret: true,
    lastError: '',
    lastOkAt: null,
    ...overrides,
  }
}

function fakeApi(overrides: Partial<AlertsAdminApi> = {}): AlertsAdminApi {
  return {
    listRules: () => Promise.resolve([]),
    createRule: (input: AlertRuleInput) =>
      Promise.resolve(rule({ ...input, id: 'new', builtin: false })),
    updateRule: (id, input) => Promise.resolve(rule({ ...input, id })),
    deleteRule: () => Promise.resolve(),
    listChecks: () => Promise.resolve([]),
    createCheck: (input) =>
      Promise.resolve(
        check({
          ...input,
          id: 'new',
        }),
      ),
    updateCheck: (id, input) => Promise.resolve(check({ ...input, id })),
    deleteCheck: () => Promise.resolve(),
    runCheck: () =>
      Promise.resolve({
        ok: true,
        latencyMs: 32,
        statusCode: 200,
        error: '',
        certNotAfter: null,
        time: '2026-10-24T10:00:00Z',
      }),
    listChannels: () => Promise.resolve([]),
    createChannel: (input) => Promise.resolve(channel({ ...input, id: 'new' })),
    updateChannel: (id, input) => Promise.resolve(channel({ ...input, id })),
    deleteChannel: () => Promise.resolve(),
    testChannel: () => Promise.resolve({ ok: true, error: '' }),
    getSettings: () => Promise.resolve({ allowPrivateTargets: false }),
    updateSettings: (allowPrivateTargets) => Promise.resolve({ allowPrivateTargets }),
    ...overrides,
  }
}

const system: SystemVersion = {
  version: '1.2.3',
  commit: 'a'.repeat(40),
  update: {
    current: 'a'.repeat(40),
    latest: 'a'.repeat(40),
    pending: [],
    notes: { en: [], es: [] },
    ci: 'green',
    checkedAt: '2026-10-24T10:00:00Z',
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

const systemApi = {
  version: () => Promise.resolve(system),
  check: () => Promise.resolve(system),
  update: () => Promise.resolve({ target: system.commit }),
}

function me(permissions: string[]): MeApi {
  const scopes: MePermissions = {
    organization: permissions as Permission[],
    servers: {},
    stacks: {},
    containers: {},
  }
  return {
    get: () =>
      Promise.resolve({
        user: { id: 'u1', username: 'alice', display_name: 'Alice', email: '' },
        bindings: { bindings: [] },
        permissions: scopes,
      }),
  }
}

const servers: ServerSummary[] = [
  {
    id: 's1',
    name: 'vps-web-01',
    connected: true,
    revoked: false,
    agentVersion: '1.0.0',
    agentLatestVersion: '1.0.0',
    agentUpdateAvailable: false,
    agentUpdateManual: false,
    agentUpgradeCommand: '',
  },
]

const groupsApi: GroupsApi = {
  list: () => Promise.resolve([{ id: 'g1', name: 'Main' }]),
  create: () => Promise.reject(new Error('unexpected')),
  rename: () => Promise.reject(new Error('unexpected')),
  remove: () => Promise.reject(new Error('unexpected')),
  setServerGroup: () => Promise.reject(new Error('unexpected')),
}

describe('alert settings', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => cleanup())

  it('lists built-in rules without delete and creates a validated rule', async () => {
    const builtin = rule({ id: 'builtin', name: 'Server offline', builtin: true })
    const custom = rule({ id: 'custom', name: 'Custom CPU' })
    const createRule = vi.fn((input: AlertRuleInput) =>
      Promise.resolve(rule({ ...input, id: 'created' })),
    )
    render(
      <RulesCard
        api={fakeApi({
          listRules: () => Promise.resolve([builtin, custom]),
          createRule,
        })}
        servers={servers}
        groups={[{ id: 'g1', name: 'Main' }]}
      />,
    )

    expect(await screen.findByText('Server offline')).toBeTruthy()
    const builtinRow = screen.getByText('Server offline').closest('tr')!
    expect(within(builtinRow).queryByRole('button', { name: 'Delete' })).toBeNull()
    const customRow = screen.getByText('Custom CPU').closest('tr')!
    expect(within(customRow).getByRole('button', { name: 'Delete' })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'CPU watch' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(createRule).toHaveBeenCalledTimes(1))
    expect(createRule.mock.calls[0]?.[0]).toMatchObject({
      name: 'CPU watch',
      type: 'cpu_high',
      severity: 'warning',
      scopeType: 'organization',
      params: { threshold: 90, for_seconds: 0 },
      channelIds: [],
      repeatIntervalSeconds: 0,
    })

    fireEvent.click(await screen.findByRole('button', { name: 'Add rule' }))
    const emptyDialog = screen.getByRole('dialog')
    fireEvent.click(within(emptyDialog).getByRole('button', { name: 'Save' }))
    expect(await within(emptyDialog).findByRole('alert')).toBeTruthy()
    expect(createRule).toHaveBeenCalledTimes(1)
  })

  it('runs a check and shows the result in the row', async () => {
    const runCheck = vi.fn(() =>
      Promise.resolve({
        ok: true,
        latencyMs: 32,
        statusCode: 200,
        error: '',
        certNotAfter: null,
        time: '2026-10-24T10:00:00Z',
      }),
    )
    render(
      <ChecksCard
        api={fakeApi({
          listChecks: () => Promise.resolve([check()]),
          runCheck,
        })}
        servers={[]}
      />,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Run now' }))
    await waitFor(() => expect(runCheck).toHaveBeenCalledWith('k1'))
    expect(await screen.findByText('HTTP 200')).toBeTruthy()
    expect(screen.getByText('32 ms')).toBeTruthy()
    expect(screen.getByText('The check Health passed.')).toBeTruthy()
  })

  it('only shows the fields of the chosen channel type and never fills a saved secret', async () => {
    const onSave = vi.fn<(input: AlertChannelInput) => Promise<void>>(() => Promise.resolve())
    const { unmount } = render(
      <ChannelForm channel={channel()} onClose={() => undefined} onSave={onSave} />,
    )

    expect(screen.getByText('Secret saved')).toBeTruthy()
    expect(screen.queryByLabelText('Bot token')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
    expect(onSave.mock.calls[0]?.[0]).not.toHaveProperty('secret')

    fireEvent.click(screen.getByRole('button', { name: 'Replace' }))
    const token = screen.getByLabelText('Bot token') as HTMLInputElement
    expect(token.value).toBe('')
    fireEvent.change(token, { target: { value: 'new-token' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(2))
    expect(onSave.mock.calls[1]?.[0]).toMatchObject({ secret: { token: 'new-token' } })
    unmount()

    render(<ChannelForm channel={null} onClose={() => undefined} onSave={onSave} />)
    expect(screen.getByLabelText('Bot token')).toBeTruthy()
    expect(screen.queryByLabelText('SMTP host')).toBeNull()
    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'email' } })
    expect(screen.getByLabelText('SMTP host')).toBeTruthy()
    expect(screen.queryByLabelText('Bot token')).toBeNull()
  })

  it('lets an email channel use an SMTP relay without login but not half a login', async () => {
    const onSave = vi.fn<(input: AlertChannelInput) => Promise<void>>(() => Promise.resolve())
    render(<ChannelForm channel={null} onClose={() => undefined} onSave={onSave} />)
    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'email' } })
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Relay' } })
    fireEvent.change(screen.getByLabelText('SMTP host'), { target: { value: 'smtp.example.com' } })
    fireEvent.change(screen.getByLabelText('SMTP port'), { target: { value: '25' } })
    fireEvent.change(screen.getByLabelText('From address'), {
      target: { value: 'talaia@example.com' },
    })
    fireEvent.change(screen.getByLabelText(/Recipients/), { target: { value: 'ops@example.com' } })

    fireEvent.change(screen.getByLabelText('Username (optional)'), { target: { value: 'user' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(onSave).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('Username (optional)'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
    expect(onSave.mock.calls[0]?.[0].secret).toBeDefined()
  })

  it('shows a successful and a failed send test', async () => {
    const testChannel = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, error: '' })
      .mockResolvedValueOnce({ ok: false, error: 'redacted failure' })
    render(
      <ChannelsCard
        api={fakeApi({
          listChannels: () => Promise.resolve([channel()]),
          testChannel,
        })}
      />,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Send test' }))
    expect(await screen.findByText('The test notification to Ops was delivered.')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Send test' }))
    expect(
      await screen.findByText('The test notification to Ops failed: redacted failure'),
    ).toBeTruthy()
    expect(testChannel).toHaveBeenCalledTimes(2)
  })

  it('round-trips the private-target switch', async () => {
    const updateSettings = vi.fn((allowPrivateTargets: boolean) =>
      Promise.resolve({ allowPrivateTargets }),
    )
    render(
      <PrivateTargetsCard
        api={fakeApi({
          getSettings: () => Promise.resolve({ allowPrivateTargets: false }),
          updateSettings,
        })}
      />,
    )

    const toggle = (await screen.findByLabelText(
      'Allow checks and notifications to reach private networks',
    )) as HTMLInputElement
    expect(toggle.checked).toBe(false)
    fireEvent.click(toggle)
    await waitFor(() => expect(updateSettings).toHaveBeenCalledWith(true))
    await waitFor(() => expect(toggle.checked).toBe(true))
    expect(screen.getByText('Private-network policy saved.')).toBeTruthy()
  })

  it('shows the section to an admin and hides it from viewers and app operators', async () => {
    const alertsApi = fakeApi()
    const { unmount } = render(
      <PermissionsProvider meApi={me(['settings.manage', 'alerts.manage'])}>
        <Settings api={systemApi} alertsApi={alertsApi} groupsApi={groupsApi} />
      </PermissionsProvider>,
    )
    expect(await screen.findByRole('heading', { name: 'Alerts' })).toBeTruthy()
    unmount()

    const viewer = render(
      <PermissionsProvider meApi={me(['alerts.view'])}>
        <Settings api={systemApi} alertsApi={alertsApi} groupsApi={groupsApi} />
      </PermissionsProvider>,
    )
    await act(async () => {})
    expect(screen.queryByRole('heading', { name: 'Alerts' })).toBeNull()
    viewer.unmount()

    render(
      <PermissionsProvider meApi={me(['alerts.view', 'alerts.silence'])}>
        <Settings api={systemApi} alertsApi={alertsApi} groupsApi={groupsApi} />
      </PermissionsProvider>,
    )
    await act(async () => {})
    expect(screen.queryByRole('heading', { name: 'Alerts' })).toBeNull()
  })
})
