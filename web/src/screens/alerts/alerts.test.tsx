import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'

import type { Alert, AlertSilence, AlertSummary, AlertsApi } from '../../api/alerts'
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
