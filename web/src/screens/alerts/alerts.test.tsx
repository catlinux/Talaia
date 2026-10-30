import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'

import type { Alert, AlertSilence, AlertSummary, AlertsApi } from '../../api/alerts'
import type { AlertRule, AlertsAdminApi } from '../../api/alertsAdmin'
import type { Group, GroupsApi } from '../../api/groups'
import { ApiError } from '../../api/http'
import type { MeApi, MePermissions, Permission } from '../../api/me'
import { PermissionsProvider } from '../../auth/permissions'
import { ToastProvider } from '../../components/ui'
import i18n from '../../i18n'
import { RouterProvider } from '../../router'
import { Alerts, AlertsBadge, alertCount } from '../Alerts'
import { AlertMessage } from './AlertMessage'
import { HistoryTab } from './HistoryTab'
import { SilencesTab } from './SilencesTab'

function alert(overrides: Partial<Alert> = {}): Alert {
  return {
    id: 'a1',
    ruleId: 'r1',
    ruleType: 'container_down',
    severity: 'critical',
    state: 'firing',
    serverId: 's1',
    serverName: 'vps-web-01',
    subjectKind: 'container',
    subjectName: 'web',
    stack: 'shop',
    startedAt: '2026-10-24T10:00:00Z',
    resolvedAt: '',
    ackedAt: '',
    ackedBy: '',
    silenced: false,
    summary: { key: 'alerts.container_down', params: {} },
    ...overrides,
  }
}

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

function silence(overrides: Partial<AlertSilence> = {}): AlertSilence {
  return {
    id: 'sil-1',
    scopeType: 'server',
    serverId: 's1',
    groupId: null,
    name: null,
    ruleType: null,
    reason: 'maintenance',
    createdBy: 'admin',
    createdAt: '2026-10-24T10:00:00Z',
    endsAt: '2099-10-24T11:00:00Z',
    ...overrides,
  }
}

function me(permissions: string[], scopes: Partial<MePermissions> = {}): MeApi {
  return {
    get: () =>
      Promise.resolve({
        user: { id: 'u1', username: 'admin', display_name: 'Admin', email: '' },
        bindings: { bindings: [] },
        permissions: {
          organization: permissions as Permission[],
          servers: {},
          stacks: {},
          containers: {},
          ...scopes,
        },
      }),
  }
}

function fakeApi(overrides: Partial<AlertsApi> = {}): AlertsApi {
  return {
    list: () => Promise.resolve([alert()]),
    summary: () =>
      Promise.resolve({
        counts: { warning: 0, critical: 1, info: 0 },
        servers: { s1: 'critical' },
      }),
    silences: () => Promise.resolve([]),
    acknowledge: () => Promise.resolve(),
    unwatch: () => Promise.resolve(),
    createSilence: () => Promise.resolve(silence()),
    deleteSilence: () => Promise.resolve(),
    ...overrides,
  }
}

function adminApi(overrides: Partial<AlertsAdminApi> = {}): AlertsAdminApi {
  return {
    listRules: () => Promise.resolve([]),
    createRule: () => Promise.reject(new Error('unexpected')),
    updateRule: () => Promise.reject(new Error('unexpected')),
    deleteRule: () => Promise.reject(new Error('unexpected')),
    listChecks: () => Promise.resolve([]),
    createCheck: () => Promise.reject(new Error('unexpected')),
    updateCheck: () => Promise.reject(new Error('unexpected')),
    deleteCheck: () => Promise.reject(new Error('unexpected')),
    runCheck: () => Promise.reject(new Error('unexpected')),
    listChannels: () => Promise.resolve([]),
    createChannel: () => Promise.reject(new Error('unexpected')),
    updateChannel: () => Promise.reject(new Error('unexpected')),
    deleteChannel: () => Promise.reject(new Error('unexpected')),
    testChannel: () => Promise.reject(new Error('unexpected')),
    getSettings: () => Promise.resolve({ allowPrivateTargets: false }),
    updateSettings: (allowPrivateTargets) => Promise.resolve({ allowPrivateTargets }),
    ...overrides,
  }
}

function renderAlerts(ui: ReactNode) {
  return render(
    <ToastProvider>
      <RouterProvider>{ui}</RouterProvider>
    </ToastProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.history.replaceState({}, '', '/alerts')
})

afterEach(() => {
  cleanup()
  window.history.replaceState({}, '', '/')
})

describe('alerts screen', () => {
  it('shows the three tabs as routes and switches between them', async () => {
    const list = vi.fn((state?: 'active' | 'history') =>
      Promise.resolve(state === 'active' ? [alert()] : []),
    )
    const api = fakeApi({
      list,
      silences: () => Promise.resolve([silence()]),
    })
    renderAlerts(<Alerts api={api} />)

    expect((await screen.findByRole('tab', { name: 'Active' })).getAttribute('aria-selected')).toBe(
      'true',
    )
    expect(screen.getByRole('tab', { name: 'History' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Silences' })).toBeTruthy()

    fireEvent.click(screen.getByRole('tab', { name: 'Silences' }))
    expect(window.location.pathname).toBe('/alerts/silences')
    expect(await screen.findByText('maintenance')).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Silences' }).getAttribute('aria-selected')).toBe('true')

    fireEvent.click(screen.getByRole('tab', { name: 'Active' }))
    expect(window.location.pathname).toBe('/alerts')
    expect(await screen.findAllByText(/Container web is down/)).toHaveLength(1)
  })

  it('opens history and silences directly from their routes', async () => {
    window.history.replaceState({}, '', '/alerts/history')
    const historyList = vi.fn(() =>
      Promise.resolve([alert({ resolvedAt: '2026-10-24T11:00:00Z' })]),
    )
    renderAlerts(<Alerts api={fakeApi({ list: historyList })} />)
    expect(
      (await screen.findByRole('tab', { name: 'History' })).getAttribute('aria-selected'),
    ).toBe('true')
    await waitFor(() => expect(historyList).toHaveBeenCalledWith('history', 0, undefined))

    cleanup()
    window.history.replaceState({}, '', '/alerts/silences')
    renderAlerts(<Alerts api={fakeApi({ silences: () => Promise.resolve([silence()]) })} />)
    expect(await screen.findByText('maintenance')).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Silences' }).getAttribute('aria-selected')).toBe('true')
  })

  it('opens the management tabs and the linked rule from their addresses', async () => {
    window.history.replaceState({}, '', '/alerts/rules?rule=r1')
    const listServers = vi.fn(() => Promise.resolve([]))
    const listGroups = vi.fn<() => Promise<Group[]>>(() => Promise.resolve([]))
    const listRules = vi.fn(() => Promise.resolve([rule()]))
    const groups: GroupsApi = {
      list: listGroups,
      create: () => Promise.reject(new Error('unexpected')),
      rename: () => Promise.reject(new Error('unexpected')),
      remove: () => Promise.reject(new Error('unexpected')),
      setServerGroup: () => Promise.reject(new Error('unexpected')),
    }
    renderAlerts(
      <PermissionsProvider meApi={me(['alerts.view', 'alerts.manage'])}>
        <Alerts
          api={fakeApi()}
          alertsAdminApi={adminApi({ listRules })}
          serversApi={{ listServers }}
          groupsApi={groups}
        />
      </PermissionsProvider>,
    )

    expect(await screen.findByRole('tab', { name: 'Rules' })).toBeTruthy()
    expect(await screen.findByRole('dialog')).toBeTruthy()
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('CPU high')
    expect(listServers).toHaveBeenCalledTimes(1)
    expect(listGroups).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Channels' }))
    expect(window.location.pathname).toBe('/alerts/channels')
    expect(
      await screen.findByLabelText('Allow checks and notifications to reach private networks'),
    ).toBeTruthy()
  })

  it('redirects a management address to the inbox without alerts.manage', async () => {
    window.history.replaceState({}, '', '/alerts/rules')
    renderAlerts(
      <PermissionsProvider meApi={me(['alerts.view'])}>
        <Alerts api={fakeApi({ list: () => Promise.resolve([]) })} />
      </PermissionsProvider>,
    )

    await waitFor(() => expect(window.location.pathname).toBe('/alerts'))
    expect(screen.queryByRole('tab', { name: 'Rules' })).toBeNull()
    expect(screen.getByRole('tab', { name: 'Active' })).toBeTruthy()
  })

  it('retries loading the servers and groups of a management tab', async () => {
    window.history.replaceState({}, '', '/alerts/checks')
    const listServers = vi
      .fn<() => Promise<never[]>>()
      .mockRejectedValueOnce(new ApiError('internal', 500))
      .mockResolvedValueOnce([])
    const listGroups = vi.fn<() => Promise<Group[]>>(() => Promise.resolve([]))
    renderAlerts(
      <PermissionsProvider meApi={me(['alerts.view', 'alerts.manage'])}>
        <Alerts
          api={fakeApi()}
          alertsAdminApi={adminApi()}
          serversApi={{ listServers }}
          groupsApi={{
            list: listGroups,
            create: () => Promise.reject(new Error('unexpected')),
            rename: () => Promise.reject(new Error('unexpected')),
            remove: () => Promise.reject(new Error('unexpected')),
            setServerGroup: () => Promise.reject(new Error('unexpected')),
          }}
        />
      </PermissionsProvider>,
    )

    expect((await screen.findByRole('alert')).textContent).toContain(
      'Something went wrong. Try again.',
    )
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByRole('button', { name: 'Add check' })).toBeTruthy()
    expect(listServers).toHaveBeenCalledTimes(2)
  })

  it('shows the inbox subtitle with severity and new counts', async () => {
    const api = fakeApi({
      list: () =>
        Promise.resolve([
          alert({ id: 'critical' }),
          alert({ id: 'warning', severity: 'warning' }),
          alert({ id: 'seen', ackedBy: 'ana', severity: 'info' }),
        ]),
    })
    renderAlerts(<Alerts api={api} />)

    expect(await screen.findByText('1 critical · 1 warnings · 2 new')).toBeTruthy()
  })

  it('shows the screen with a server-scoped alerts.view permission', async () => {
    renderAlerts(
      <PermissionsProvider
        meApi={me([], {
          servers: { s1: ['alerts.view' as Permission] },
        })}
      >
        <Alerts api={fakeApi({ list: () => Promise.resolve([]) })} />
      </PermissionsProvider>,
    )

    expect(await screen.findByRole('tab', { name: 'Active' })).toBeTruthy()
  })

  it('translates every known rule without raw keys or undefined', async () => {
    const rules = [
      'cpu_high',
      'mem_high',
      'disk_high',
      'server_offline',
      'container_down',
      'container_unhealthy',
      'check_failed',
      'cert_expiry',
    ]
    for (const language of ['ca', 'es', 'en'] as const) {
      await i18n.changeLanguage(language)
      for (const rule of rules) {
        const { unmount } = render(
          <AlertMessage
            alert={alert({
              ruleType: rule,
              summary: { key: `alerts.${rule}`, params: { value: 91 } },
            })}
          />,
        )
        const text = document.body.textContent ?? ''
        expect(text).not.toContain(`alerts.${rule}`)
        expect(text).not.toContain(rule)
        expect(text.toLowerCase()).not.toContain('undefined')
        expect(text.length).toBeGreaterThan(0)
        unmount()
      }
    }
  })

  it('loads more history with the next offset', async () => {
    const first = Array.from({ length: 50 }, (_, index) =>
      alert({ id: `h-${index}`, state: 'resolved', resolvedAt: '2026-10-24T10:10:00Z' }),
    )
    const list = vi
      .fn<() => Promise<Alert[]>>()
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce([alert({ id: 'h-50', resolvedAt: '2026-10-24T10:10:00Z' })])
    render(<HistoryTab api={fakeApi({ list })} />)

    fireEvent.click(await screen.findByRole('button', { name: 'Load more' }))
    await waitFor(() => expect(list).toHaveBeenLastCalledWith('history', 50, undefined))
  })

  it('lists and deletes silences when the user may', async () => {
    const deleteSilence = vi.fn(() => Promise.resolve())
    render(
      <PermissionsProvider meApi={me(['alerts.view', 'alerts.silence'])}>
        <SilencesTab
          api={fakeApi({ silences: () => Promise.resolve([silence()]), deleteSilence })}
          onRefresh={() => undefined}
        />
      </PermissionsProvider>,
    )

    expect(await screen.findByText('maintenance')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Delete silence' }))
    await waitFor(() => expect(deleteSilence).toHaveBeenCalledWith('sil-1'))
  })

  it('colours and hides the navigation badge', () => {
    const summary: AlertSummary = {
      counts: { warning: 2, critical: 0, info: 0 },
      servers: { s1: 'warning' },
    }
    const { container, rerender } = render(<AlertsBadge summary={summary} />)
    expect(alertCount(summary)).toBe(2)
    expect(container.querySelector('.side__badge--warning')?.textContent).toBe('2')
    rerender(
      <AlertsBadge summary={{ counts: { warning: 0, critical: 1, info: 0 }, servers: {} }} />,
    )
    expect(container.querySelector('.side__badge--critical')?.textContent).toBe('1')
    rerender(
      <AlertsBadge summary={{ counts: { warning: 0, critical: 0, info: 0 }, servers: {} }} />,
    )
    expect(container.querySelector('.side__badge')).toBeNull()
    rerender(<AlertsBadge summary={null} />)
    expect(container.querySelector('.side__badge')).toBeNull()
  })
})
