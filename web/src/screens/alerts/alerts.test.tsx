import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Alert, AlertSilence, AlertSummary, AlertsApi } from '../../api/alerts'
import type { MeApi, MePermissions, Permission } from '../../api/me'
import { PermissionsProvider } from '../../auth/permissions'
import i18n from '../../i18n'
import { Alerts, AlertsBadge, alertCount } from '../Alerts'
import { ActiveTab } from './ActiveTab'
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

describe('alerts components', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => cleanup())

  it('shows the three tabs and switches between them', async () => {
    const api = fakeApi({
      list: () => Promise.resolve([alert()]),
      silences: () => Promise.resolve([silence()]),
    })
    render(<Alerts api={api} />)

    expect(await screen.findByRole('tab', { name: 'Active' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'History' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Silences' })).toBeTruthy()

    fireEvent.click(screen.getByRole('tab', { name: 'Silences' }))
    expect(await screen.findByText('maintenance')).toBeTruthy()
    fireEvent.click(screen.getByRole('tab', { name: 'Active' }))
    expect(await screen.findAllByText(/Container web is down/)).toHaveLength(1)
  })

  it('shows the screen with a server-scoped alerts.view permission', async () => {
    render(
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

  it('sorts critical first and hides actions from a viewer', async () => {
    const api = fakeApi({
      list: () =>
        Promise.resolve([
          alert({ id: 'warning', severity: 'warning', startedAt: '2026-10-24T11:00:00Z' }),
          alert({ id: 'critical', severity: 'critical', startedAt: '2026-10-24T10:00:00Z' }),
        ]),
    })
    render(
      <PermissionsProvider meApi={me(['alerts.view'])}>
        <ActiveTab api={api} onRefresh={() => undefined} />
      </PermissionsProvider>,
    )

    expect(await screen.findAllByText(/Container web is down/)).toHaveLength(2)
    const cards = document.querySelectorAll('.alert-card')
    expect(cards).toHaveLength(2)
    expect(cards[0]?.textContent).toContain('Critical')
    expect(screen.queryByRole('button', { name: 'Acknowledge' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Silence' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Stop watching' })).toBeNull()
  })

  it('opens the server from an alert when navigation is available', async () => {
    const onOpenServer = vi.fn()
    render(
      <PermissionsProvider meApi={me(['alerts.view'])}>
        <ActiveTab api={fakeApi()} onRefresh={() => undefined} onOpenServer={onOpenServer} />
      </PermissionsProvider>,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'vps-web-01' }))
    expect(onOpenServer).toHaveBeenCalledWith('s1', 'vps-web-01')
  })

  it('acknowledges an alert and refreshes the list and the summary', async () => {
    const acknowledge = vi.fn(() => Promise.resolve())
    const list = vi
      .fn<() => Promise<Alert[]>>()
      .mockResolvedValueOnce([alert()])
      .mockResolvedValueOnce([alert({ ackedBy: 'admin' })])
    const onRefresh = vi.fn()
    render(
      <PermissionsProvider meApi={me(['alerts.view', 'alerts.silence'])}>
        <ActiveTab api={fakeApi({ acknowledge, list })} onRefresh={onRefresh} />
      </PermissionsProvider>,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Acknowledge' }))
    await waitFor(() => expect(acknowledge).toHaveBeenCalledWith('a1'))
    expect(onRefresh).toHaveBeenCalled()
    expect(await screen.findByText('Acknowledged by admin')).toBeTruthy()
  })

  it('validates and posts a container silence with the alert scope', async () => {
    const createSilence = vi.fn(() => Promise.resolve(silence()))
    const onRefresh = vi.fn()
    render(
      <PermissionsProvider meApi={me(['alerts.view', 'alerts.silence'])}>
        <ActiveTab api={fakeApi({ createSilence })} onRefresh={onRefresh} />
      </PermissionsProvider>,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Silence' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Silence' }))
    expect(await within(dialog).findByRole('alert')).toBeTruthy()
    expect(createSilence).not.toHaveBeenCalled()

    fireEvent.change(within(dialog).getByLabelText(/Reason/), {
      target: { value: 'x'.repeat(201) },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Silence' }))
    expect(createSilence).not.toHaveBeenCalled()

    fireEvent.change(within(dialog).getByLabelText(/Reason/), {
      target: { value: 'deploying' },
    })
    fireEvent.change(within(dialog).getByLabelText(/Duration/), { target: { value: 'custom' } })
    fireEvent.change(within(dialog).getByLabelText(/Custom minutes/), {
      target: { value: '50000' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Silence' }))
    expect(createSilence).not.toHaveBeenCalled()

    fireEvent.change(within(dialog).getByLabelText(/Duration/), {
      target: { value: '30' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Silence' }))

    await waitFor(() =>
      expect(createSilence).toHaveBeenCalledWith({
        scopeType: 'container',
        serverId: 's1',
        name: 'web',
        ruleType: 'container_down',
        durationMinutes: 30,
        reason: 'deploying',
      }),
    )
    expect(onRefresh).toHaveBeenCalled()
  })

  it('asks before stop watching a container', async () => {
    const unwatch = vi.fn(() => Promise.resolve())
    render(
      <PermissionsProvider meApi={me(['alerts.view', 'alerts.silence'])}>
        <ActiveTab api={fakeApi({ unwatch })} onRefresh={() => undefined} />
      </PermissionsProvider>,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Stop watching' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText(/will not be monitored again/)).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Stop watching' }))
    await waitFor(() => expect(unwatch).toHaveBeenCalledWith('a1'))
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
