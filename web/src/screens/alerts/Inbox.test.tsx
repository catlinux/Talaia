import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Alert, AlertSilence, AlertsApi } from '../../api/alerts'
import type { MeApi, MePermissions, Permission } from '../../api/me'
import { PermissionsProvider } from '../../auth/permissions'
import { ToastProvider } from '../../components/ui'
import i18n from '../../i18n'
import { RouterProvider } from '../../router'
import { Inbox } from './Inbox'

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
    scopeType: 'container',
    serverId: 's1',
    groupId: null,
    name: 'web',
    ruleType: 'container_down',
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

function fakeApi(overrides: Partial<AlertsApi> = {}) {
  const list = vi.fn(overrides.list ?? (() => Promise.resolve([alert()])))
  const acknowledge = vi.fn(() => Promise.resolve())
  const unwatch = vi.fn(() => Promise.resolve())
  const createSilence = vi.fn(() => Promise.resolve(silence()))
  const api = {
    summary: () =>
      Promise.resolve({
        counts: { warning: 0, critical: 1, info: 0 },
        servers: { s1: 'critical' },
      }),
    silences: () => Promise.resolve([]),
    acknowledge,
    unwatch,
    createSilence,
    deleteSilence: () => Promise.resolve(),
    ...overrides,
    list,
  } satisfies AlertsApi
  return { api, list, acknowledge, unwatch, createSilence }
}

interface RenderOptions {
  me?: MeApi
  onRefresh?: () => void
  onOpenServer?: (serverId: string, serverName: string) => void
}

function renderInbox(api: AlertsApi, options: RenderOptions = {}) {
  return render(
    <ToastProvider>
      <RouterProvider>
        <PermissionsProvider meApi={options.me ?? me(['alerts.view', 'alerts.silence'])}>
          <Inbox
            api={api}
            onRefresh={options.onRefresh ?? (() => undefined)}
            onOpenServer={options.onOpenServer}
            refreshMs={30_000}
          />
        </PermissionsProvider>
      </RouterProvider>
    </ToastProvider>,
  )
}

function rows(): HTMLElement[] {
  return Array.from(document.querySelectorAll<HTMLElement>('tbody tr.ui-table__row'))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.history.replaceState({}, '', '/alerts')
})

afterEach(() => {
  cleanup()
  window.history.replaceState({}, '', '/')
})

describe('Inbox', () => {
  it('renders, sorts new alerts first and links the alert target', async () => {
    const { api } = fakeApi({
      list: () =>
        Promise.resolve([
          alert({
            id: 'seen',
            subjectName: 'seen-web',
            severity: 'warning',
            ackedBy: 'ana',
            startedAt: '2026-10-24T09:00:00Z',
          }),
          alert({
            id: 'new-warning',
            subjectName: 'warning-web',
            severity: 'warning',
            startedAt: '2026-10-24T08:00:00Z',
          }),
          alert({
            id: 'new-critical',
            subjectName: 'critical-web',
            severity: 'critical',
            startedAt: '2026-10-24T10:00:00Z',
          }),
        ]),
    })
    renderInbox(api)

    await screen.findAllByText(/Container .* is down/)
    expect(rows()).toHaveLength(3)
    expect(rows()[0]?.textContent).toContain('critical-web')
    expect(rows()[1]?.textContent).toContain('warning-web')
    expect(rows()[2]?.textContent).toContain('seen-web')
    expect(screen.getByText('Seen by ana')).toBeTruthy()
    expect(screen.getAllByRole('link', { name: 'vps-web-01' })[0]?.getAttribute('href')).toBe(
      '/servers/s1',
    )
    expect(screen.getAllByRole('link', { name: 'shop' })[0]?.getAttribute('href')).toBe(
      '/servers/s1/stacks/shop',
    )
  })

  it('filters through the URL query', async () => {
    window.history.replaceState({}, '', '/alerts?severity=critical&state=new')
    const { api } = fakeApi({
      list: () =>
        Promise.resolve([
          alert({ id: 'critical', severity: 'critical' }),
          alert({ id: 'warning', severity: 'warning' }),
          alert({ id: 'seen', ackedBy: 'ana' }),
        ]),
    })
    renderInbox(api)

    await screen.findAllByText(/Container web is down/)
    expect(rows()).toHaveLength(1)
    expect(rows()[0]?.textContent).toContain('Critical')

    fireEvent.change(screen.getByLabelText('Severity'), { target: { value: 'warning' } })
    await waitFor(() => expect(window.location.search).toContain('severity=warning'))
    expect(rows()).toHaveLength(1)
    expect(rows()[0]?.textContent).toContain('Warning')

    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
    await waitFor(() => expect(window.location.search).toBe(''))
    expect(rows()).toHaveLength(3)
  })

  it('groups by server with severity counts', async () => {
    const { api } = fakeApi({
      list: () =>
        Promise.resolve([
          alert(),
          alert({ id: 'w', severity: 'warning', serverId: 's2', serverName: 'vps-db-01' }),
        ]),
    })
    renderInbox(api)
    await screen.findAllByText(/Container web is down/)

    fireEvent.change(screen.getByLabelText('Group by'), { target: { value: 'server' } })
    expect(await screen.findByText('vps-web-01 · 1 Critical')).toBeTruthy()
    expect(screen.getByText('vps-db-01 · 1 Warning')).toBeTruthy()
  })

  it('acknowledges every allowed selected alert and reloads once', async () => {
    const { api, list, acknowledge } = fakeApi({
      list: () => Promise.resolve([alert({ id: 'a1' }), alert({ id: 'a2', subjectName: 'db' })]),
    })
    const onRefresh = vi.fn()
    renderInbox(api, { onRefresh })
    await screen.findAllByText(/Container web is down/)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
    fireEvent.click(screen.getByRole('button', { name: 'Acknowledge' }))

    await waitFor(() => expect(acknowledge).toHaveBeenCalledTimes(2))
    expect(acknowledge).toHaveBeenCalledWith('a1')
    expect(acknowledge).toHaveBeenCalledWith('a2')
    expect(list).toHaveBeenCalledTimes(2)
    expect(onRefresh).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('2 done · 0 failed · 0 not allowed')).toBeTruthy()
  })

  it('reports and skips alerts outside the permission scope', async () => {
    const { api, acknowledge } = fakeApi({
      list: () =>
        Promise.resolve([
          alert({ id: 'allowed' }),
          alert({ id: 'outside', serverId: 's2', serverName: 'vps-db-01' }),
        ]),
    })
    renderInbox(api, {
      me: me(['alerts.view'], { servers: { s1: ['alerts.silence' as Permission] } }),
    })
    await screen.findAllByText(/Container web is down/)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
    expect(
      within(screen.getByRole('button', { name: 'Acknowledge' })).getByText('1 not allowed'),
    ).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Acknowledge' }))

    await waitFor(() => expect(acknowledge).toHaveBeenCalledTimes(1))
    expect(acknowledge).toHaveBeenCalledWith('allowed')
    expect(await screen.findByText('1 done · 0 failed · 1 not allowed')).toBeTruthy()
  })

  it('creates one bulk silence for a repeated scope', async () => {
    const { api, createSilence, list } = fakeApi({
      list: () => Promise.resolve([alert({ id: 'a1' }), alert({ id: 'a2' })]),
    })
    renderInbox(api)
    await screen.findAllByText(/Container web is down/)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
    fireEvent.click(screen.getByRole('button', { name: 'Silence' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText(/Reason/), {
      target: { value: 'maintenance' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Silence' }))

    await waitFor(() => expect(createSilence).toHaveBeenCalledTimes(1))
    expect(createSilence).toHaveBeenCalledWith({
      scopeType: 'container',
      serverId: 's1',
      name: 'web',
      ruleType: 'container_down',
      durationMinutes: 60,
      reason: 'maintenance',
    })
    expect(list).toHaveBeenCalledTimes(2)
    expect(await screen.findByText('1 done · 0 failed · 0 not allowed')).toBeTruthy()
  })

  it('asks before bulk stop watching and confirms the count', async () => {
    const { api, unwatch } = fakeApi()
    renderInbox(api)
    await screen.findAllByText(/Container web is down/)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
    fireEvent.click(screen.getByRole('button', { name: 'Stop watching' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText(/will not be monitored again/)).toBeTruthy()
    fireEvent.change(within(dialog).getByLabelText('Type 1 to confirm'), {
      target: { value: '1' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Stop watching' }))

    await waitFor(() => expect(unwatch).toHaveBeenCalledWith('a1'))
  })

  it('highlights and focuses the alert named in the query', async () => {
    window.history.replaceState({}, '', '/alerts?focus=a2')
    const { api } = fakeApi({
      list: () => Promise.resolve([alert({ id: 'a1' }), alert({ id: 'a2', subjectName: 'db' })]),
    })
    renderInbox(api)
    await screen.findAllByText(/Container web is down/)

    const focused = document.querySelectorAll('.alerts-inbox__row--focus')
    expect(focused).toHaveLength(1)
    expect(focused[0]!.textContent).toContain('Container db is down')
    fireEvent.click(
      within(rows().find((row) => row !== focused[0])!).getByText(/Container web is down/),
    )
    await waitFor(() => expect(window.location.search).toContain('focus=a1'))
  })

  it('hides every action from a viewer', async () => {
    const { api } = fakeApi()
    renderInbox(api, { me: me(['alerts.view']) })
    await screen.findAllByText(/Container web is down/)

    expect(screen.queryByRole('checkbox', { name: 'Select all' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Actions for web' })).toBeNull()
  })

  it('acknowledges one alert from its menu and refreshes the list and the summary', async () => {
    const list = vi
      .fn<() => Promise<Alert[]>>()
      .mockResolvedValueOnce([alert()])
      .mockResolvedValue([alert({ ackedAt: '2026-10-24T12:00:00Z', ackedBy: 'admin' })])
    const { api, acknowledge } = fakeApi({ list })
    const onRefresh = vi.fn()
    renderInbox(api, { onRefresh })

    fireEvent.click(await screen.findByRole('button', { name: 'Actions for web' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Acknowledge' }))

    await waitFor(() => expect(acknowledge).toHaveBeenCalledWith('a1'))
    expect(await screen.findByText('Seen by admin')).toBeTruthy()
    expect(onRefresh).toHaveBeenCalled()
  })

  it('validates and posts a container silence from the row menu', async () => {
    const { api, createSilence } = fakeApi()
    const onRefresh = vi.fn()
    renderInbox(api, { onRefresh })

    fireEvent.click(await screen.findByRole('button', { name: 'Actions for web' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Silence…' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Silence' }))
    expect(await within(dialog).findByRole('alert')).toBeTruthy()
    expect(createSilence).not.toHaveBeenCalled()

    fireEvent.change(within(dialog).getByLabelText(/Reason/), {
      target: { value: 'x'.repeat(201) },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Silence' }))
    expect(createSilence).not.toHaveBeenCalled()

    fireEvent.change(within(dialog).getByLabelText(/Reason/), { target: { value: 'deploying' } })
    fireEvent.change(within(dialog).getByLabelText(/Duration/), { target: { value: '30' } })
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

  it('asks before stop watching one container', async () => {
    const { api, unwatch } = fakeApi()
    renderInbox(api)

    fireEvent.click(await screen.findByRole('button', { name: 'Actions for web' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Stop watching' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText(/will not be monitored again/)).toBeTruthy()
    expect(unwatch).not.toHaveBeenCalled()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Stop watching' }))
    await waitFor(() => expect(unwatch).toHaveBeenCalledWith('a1'))
  })
})
