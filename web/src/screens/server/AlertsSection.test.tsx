import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AlertRule, AlertRuleInput, AlertsAdminApi } from '../../api/alertsAdmin'
import type { MeApi, MePermissions, Permission } from '../../api/me'
import { PermissionsProvider } from '../../auth/permissions'
import i18n from '../../i18n'
import { AlertsSection } from './AlertsSection'

function rule(overrides: Partial<AlertRule> = {}): AlertRule {
  return {
    id: 'rule-1',
    name: 'CPU usage high',
    type: 'cpu_high',
    enabled: true,
    severity: 'warning',
    params: { threshold: 90, for_seconds: 600 },
    scopeType: 'organization',
    serverId: null,
    groupId: null,
    scopeName: null,
    channelIds: ['channel-1'],
    repeatIntervalSeconds: 300,
    builtin: false,
    ...overrides,
  }
}

function toRule(input: AlertRuleInput, id: string): AlertRule {
  return rule({
    id,
    name: input.name,
    type: input.type,
    enabled: input.enabled,
    severity: input.severity,
    params: input.params,
    scopeType: input.scopeType,
    serverId: input.serverId ?? null,
    groupId: input.groupId ?? null,
    scopeName: input.scopeName ?? null,
    channelIds: input.channelIds,
    repeatIntervalSeconds: input.repeatIntervalSeconds,
  })
}

function fakeApi(initial: AlertRule[]) {
  let current = [...initial]
  const listRules = vi.fn(() => Promise.resolve([...current]))
  const createRule = vi.fn((input: AlertRuleInput) => {
    const created = toRule(input, `created-${current.length + 1}`)
    current = [...current, created]
    return Promise.resolve(created)
  })
  const updateRule = vi.fn((id: string, input: AlertRuleInput) => {
    const updated = toRule(input, id)
    current = current.map((candidate) => (candidate.id === id ? updated : candidate))
    return Promise.resolve(updated)
  })
  const deleteRule = vi.fn((id: string) => {
    current = current.filter((candidate) => candidate.id !== id)
    return Promise.resolve()
  })
  const unexpected = () => Promise.reject(new Error('unexpected API call'))
  const api: AlertsAdminApi = {
    listRules,
    createRule,
    updateRule,
    deleteRule,
    listChecks: () => Promise.resolve([]),
    createCheck: unexpected,
    updateCheck: unexpected,
    deleteCheck: () => Promise.resolve(),
    runCheck: unexpected,
    listChannels: () => Promise.resolve([]),
    createChannel: unexpected,
    updateChannel: unexpected,
    deleteChannel: () => Promise.resolve(),
    testChannel: unexpected,
    getSettings: () => Promise.resolve({ allowPrivateTargets: false }),
    updateSettings: (allowPrivateTargets) => Promise.resolve({ allowPrivateTargets }),
  }
  return { api, listRules, createRule, updateRule, deleteRule }
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

function renderSection(
  api: AlertsAdminApi,
  permissions: string[] = ['alerts.view', 'alerts.manage'],
  groupId = 'group-1',
) {
  render(
    <PermissionsProvider meApi={me(permissions)}>
      <AlertsSection api={api} serverId="srv-1" serverName="vps-web-01" groupId={groupId} />
    </PermissionsProvider>,
  )
}

function rowFor(name: string): HTMLElement {
  return screen.getByText(name).closest('li') as HTMLElement
}

describe('server alerts section', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('shows inherited organization and group rules with effective parameters', async () => {
    const orgCPU = rule({ id: 'org-cpu' })
    const groupCPU = rule({
      id: 'group-cpu',
      name: 'Group CPU',
      scopeType: 'group',
      groupId: 'group-1',
      params: { threshold: 91, for_seconds: 300 },
    })
    const disk = rule({
      id: 'org-disk',
      name: 'Disk warning',
      type: 'disk_high',
      params: { threshold: 80, for_seconds: 0 },
    })
    renderSection(fakeApi([orgCPU, groupCPU, disk]).api)

    expect(await screen.findByText('Group CPU')).toBeTruthy()
    expect(screen.queryByText('CPU usage high')).toBeNull()
    expect(screen.getByText('Disk warning')).toBeTruthy()
    expect(screen.getAllByText('Inherited')).toHaveLength(2)
    expect(screen.getByText(/Threshold \(%\): 91/)).toBeTruthy()
    expect(screen.getByText(/Threshold \(%\): 80/)).toBeTruthy()
    expect(screen.getByText(/Duration \(seconds\): 300/)).toBeTruthy()
  })

  it('customises an inherited rule with the inherited scope settings', async () => {
    const { api, createRule } = fakeApi([rule()])
    renderSection(api)

    fireEvent.click(
      within(
        await screen.findByText('CPU usage high').then((node) => node.closest('li')!),
      ).getByRole('button', { name: 'Customise' }),
    )
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText(/Threshold/), {
      target: { value: '85' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(createRule).toHaveBeenCalledTimes(1))
    expect(createRule.mock.calls[0]?.[0]).toMatchObject({
      name: 'CPU usage high',
      type: 'cpu_high',
      enabled: true,
      severity: 'warning',
      params: { threshold: 85, for_seconds: 600 },
      scopeType: 'server',
      serverId: 'srv-1',
      channelIds: ['channel-1'],
      repeatIntervalSeconds: 300,
    })
    expect(await screen.findByText('Customised for this server')).toBeTruthy()
  })

  it('turns a rule off for one server and restores the inherited rule', async () => {
    const { api, createRule, deleteRule } = fakeApi([rule()])
    renderSection(api)

    await screen.findByText('CPU usage high')
    fireEvent.click(
      within(rowFor('CPU usage high')).getByRole('button', {
        name: 'Turn off for this server',
      }),
    )
    await waitFor(() => expect(createRule).toHaveBeenCalledTimes(1))
    expect(createRule.mock.calls[0]?.[0]).toMatchObject({
      type: 'cpu_high',
      severity: 'warning',
      enabled: false,
      scopeType: 'server',
      serverId: 'srv-1',
    })
    expect(await screen.findByText('Off for this server')).toBeTruthy()
    expect(screen.getByText('No alert will be sent for this rule on this server.')).toBeTruthy()

    const created = await createRule.mock.results[0]!.value
    fireEvent.click(
      within(rowFor('CPU usage high')).getByRole('button', { name: 'Restore default' }),
    )
    await waitFor(() => expect(deleteRule).toHaveBeenCalledWith(created.id))
    expect(await screen.findByText('Inherited')).toBeTruthy()
    expect(screen.queryByText('Off for this server')).toBeNull()
  })

  it('edits an existing server override', async () => {
    const override = rule({
      id: 'override-1',
      name: 'Server CPU',
      scopeType: 'server',
      serverId: 'srv-1',
      params: { threshold: 95, for_seconds: 600 },
    })
    const { api, updateRule } = fakeApi([rule(), override])
    renderSection(api)

    expect(await screen.findByText('Customised for this server')).toBeTruthy()
    fireEvent.click(within(rowFor('Server CPU')).getByRole('button', { name: 'Edit' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText(/Threshold/), {
      target: { value: '96' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(updateRule).toHaveBeenCalledTimes(1))
    expect(updateRule.mock.calls[0]?.[0]).toBe('override-1')
    expect(updateRule.mock.calls[0]?.[1]).toMatchObject({
      name: 'Server CPU',
      enabled: true,
      severity: 'warning',
      params: { threshold: 96, for_seconds: 600 },
      scopeType: 'server',
      serverId: 'srv-1',
    })
  })

  it('hides actions from readers without the manage permission', async () => {
    const { api } = fakeApi([rule()])
    renderSection(api, ['alerts.view'])

    expect(await screen.findByText('CPU usage high')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Customise' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Turn off for this server' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull()
  })

  it('lists container, stack and server-only rules as read-only', async () => {
    const container = rule({
      id: 'container-1',
      name: 'Container CPU',
      type: 'cpu_high',
      severity: 'critical',
      scopeType: 'container',
      serverId: 'srv-1',
      scopeName: 'web',
    })
    const serverOnly = rule({
      id: 'server-only',
      name: 'Server memory',
      type: 'mem_high',
      scopeType: 'server',
      serverId: 'srv-1',
    })
    renderSection(fakeApi([rule(), container, serverOnly]).api)

    expect(await screen.findByText('Other rules for this server')).toBeTruthy()
    expect(screen.getByText('Container CPU')).toBeTruthy()
    expect(screen.getByText('Server memory')).toBeTruthy()
    expect(screen.getAllByText('Read-only')).toHaveLength(2)
    expect(within(rowFor('Container CPU')).queryByRole('button')).toBeNull()
    expect(within(rowFor('Server memory')).queryByRole('button')).toBeNull()
  })
})
