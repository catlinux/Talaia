import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Alert, AlertsApi } from '../../api/alerts'
import type { MeApi } from '../../api/me'
import { PermissionsProvider } from '../../auth/permissions'
import i18n from '../../i18n'
import { RouterProvider } from '../../router'
import { AlertBell } from './AlertBell'

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

function api(alerts: Alert[]): AlertsApi {
  const unexpected = () => Promise.reject(new Error('unexpected call'))
  return {
    list: vi.fn(() => Promise.resolve(alerts)),
    summary: () =>
      Promise.resolve({ counts: { warning: 0, critical: alerts.length, info: 0 }, servers: {} }),
    silences: () => Promise.resolve([]),
    acknowledge: unexpected,
    unwatch: unexpected,
    createSilence: unexpected,
    deleteSilence: unexpected,
  }
}

function viewer(): MeApi {
  return {
    get: () =>
      Promise.resolve({
        user: { id: 'u1', username: 'viewer', display_name: 'Viewer', email: '' },
        bindings: { bindings: [] },
        permissions: { organization: [], servers: {}, stacks: {}, containers: {} },
      }),
  }
}

const summary = {
  counts: { warning: 2, critical: 1, info: 0 },
  servers: {},
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.history.replaceState({}, '', '/')
})

afterEach(() => {
  cleanup()
  window.history.replaceState({}, '', '/')
})

describe('AlertBell', () => {
  it('lists the latest new or critical alerts and links to their detail', async () => {
    const list = vi.fn(() =>
      Promise.resolve([
        alert({ id: 'seen', ackedBy: 'ana', severity: 'warning' }),
        alert({ id: 'new', startedAt: '2026-10-24T11:00:00Z' }),
        alert({ id: 'critical', startedAt: '2026-10-24T12:00:00Z' }),
      ]),
    )
    render(
      <RouterProvider>
        <AlertBell summary={summary} api={{ ...api([]), list }} />
      </RouterProvider>,
    )

    const bell = screen.getByRole('button', { name: 'Alerts: 3 open' })
    expect(bell.querySelector('.shell__bell-dot--crit')).toBeTruthy()
    fireEvent.click(bell)

    await waitFor(() => expect(list).toHaveBeenCalledWith('active'))
    const links = await screen.findAllByRole('link')
    const focusLinks = links.filter((link) =>
      (link.getAttribute('href') ?? '').startsWith('/alerts?focus='),
    )
    expect(focusLinks).toHaveLength(2)
    expect(focusLinks[0]?.getAttribute('href')).toBe('/alerts?focus=critical')
    expect(focusLinks[1]?.getAttribute('href')).toBe('/alerts?focus=new')
    expect(screen.getByRole('link', { name: 'View all alerts' }).getAttribute('href')).toBe(
      '/alerts',
    )
  })

  it('hides the bell at zero and without alerts.view', () => {
    const { rerender } = render(
      <RouterProvider>
        <AlertBell summary={{ ...summary, counts: { warning: 0, critical: 0, info: 0 } }} />
      </RouterProvider>,
    )
    expect(screen.queryByRole('button', { name: /Alerts:/ })).toBeNull()

    rerender(
      <RouterProvider>
        <PermissionsProvider meApi={viewer()}>
          <AlertBell summary={summary} />
        </PermissionsProvider>
      </RouterProvider>,
    )
    expect(screen.queryByRole('button', { name: /Alerts:/ })).toBeNull()
  })

  it('closes on Escape and outside clicks', async () => {
    render(
      <RouterProvider>
        <AlertBell summary={summary} api={api([])} />
      </RouterProvider>,
    )
    const bell = screen.getByRole('button', { name: 'Alerts: 3 open' })
    fireEvent.click(bell)
    expect(await screen.findByRole('menu')).toBeTruthy()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('menu')).toBeNull()

    fireEvent.click(bell)
    expect(await screen.findByRole('menu')).toBeTruthy()
    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('menu')).toBeNull()
  })
})
