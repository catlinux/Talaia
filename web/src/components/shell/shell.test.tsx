import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AlertSummary } from '../../api/alerts'
import type { AuthApi } from '../../api/auth'
import type { MeApi, MePermissions, MeResponse, Permission } from '../../api/me'
import { AuthGate } from '../../auth/AuthGate'
import { PermissionsProvider } from '../../auth/permissions'
import i18n from '../../i18n'
import { RouterProvider } from '../../router'
import { Sidebar } from './Sidebar'
import { Topbar } from './Topbar'
import { UserMenu } from './UserMenu'

const allPermissions: Permission[] = [
  'container.view',
  'container.logs',
  'container.control',
  'container.terminal',
  'container.remove',
  'stack.view',
  'stack.deploy',
  'stack.edit',
  'server.view',
  'users.manage',
  'settings.manage',
  'audit.view',
  'recordings.view',
  'alerts.view',
  'alerts.silence',
  'alerts.manage',
]

const signedInApi: AuthApi = {
  session: () => Promise.resolve({ username: 'admin', stage: 'full' }),
  login: () => Promise.reject(new Error('unexpected login')),
  startTotpSetup: () => Promise.reject(new Error('unexpected setup')),
  confirmTotpSetup: () => Promise.reject(new Error('unexpected setup')),
  verifyTotp: () => Promise.reject(new Error('unexpected verify')),
  logout: () => Promise.resolve(),
}

function meApi(permissions: Partial<MePermissions>): MeApi {
  const body: MeResponse = {
    user: { id: 'u1', username: 'admin', display_name: 'Admin', email: '' },
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

function renderSidebar(me: MeApi, summary: AlertSummary | null = null) {
  return render(
    <RouterProvider>
      <PermissionsProvider meApi={me}>
        <Sidebar alertSummary={summary} mobileOpen={false} onClose={vi.fn()} />
      </PermissionsProvider>
    </RouterProvider>,
  )
}

beforeEach(async () => {
  localStorage.clear()
  sessionStorage.clear()
  document.documentElement.removeAttribute('data-theme')
  window.history.replaceState({}, '', '/')
  await i18n.changeLanguage('en')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  window.history.replaceState({}, '', '/')
})

describe('Sidebar', () => {
  it('renders the grouped navigation and marks the active link', async () => {
    renderSidebar(meApi({ organization: allPermissions }))

    const nav = await screen.findByRole('navigation', { name: 'Main navigation' })
    await within(nav).findByRole('link', { name: 'Users' })

    expect(
      within(nav)
        .getAllByRole('link')
        .map((link) => link.textContent),
    ).toEqual([
      'Overview',
      'Servers',
      'Containers',
      'Stacks',
      'Alerts',
      'Audit',
      'Users',
      'Settings',
    ])
    expect(within(nav).getByText('Infrastructure')).toBeTruthy()
    expect(within(nav).getByText('Monitoring')).toBeTruthy()
    expect(within(nav).getByText('Administration')).toBeTruthy()
    expect(within(nav).getByRole('link', { name: 'Overview' }).getAttribute('aria-current')).toBe(
      'page',
    )

    fireEvent.click(within(nav).getByRole('link', { name: 'Servers' }))

    expect(window.location.pathname).toBe('/servers')
    await waitFor(() =>
      expect(within(nav).getByRole('link', { name: 'Servers' }).getAttribute('aria-current')).toBe(
        'page',
      ),
    )
  })

  it('collapses to icons and remembers the choice', async () => {
    renderSidebar(meApi({ organization: allPermissions }))
    await screen.findByRole('link', { name: 'Settings' })

    const sidebar = screen.getByRole('complementary')
    fireEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }))

    expect(sidebar.classList.contains('side--collapsed')).toBe(true)
    expect(localStorage.getItem('talaia.sidebar')).toBe('1')
    expect(screen.getByRole('button', { name: 'Expand sidebar' })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Expand sidebar' }))

    expect(sidebar.classList.contains('side--collapsed')).toBe(false)
    expect(localStorage.getItem('talaia.sidebar')).toBe('0')
  })

  it('hides entries the user cannot access', async () => {
    renderSidebar(meApi({ organization: ['server.view'] }))

    const nav = await screen.findByRole('navigation', { name: 'Main navigation' })
    await within(nav).findByRole('link', { name: 'Servers' })
    for (const name of ['Containers', 'Stacks', 'Alerts', 'Audit', 'Users']) {
      expect(within(nav).queryByRole('link', { name })).toBeNull()
    }
    expect(within(nav).getByRole('link', { name: 'Settings' })).toBeTruthy()
    expect(within(nav).getByRole('link', { name: 'Overview' })).toBeTruthy()
  })
})

describe('Topbar', () => {
  it('shows route breadcrumbs and the alert bell', async () => {
    window.history.replaceState({}, '', '/servers/vps-web-01')
    const summary: AlertSummary = {
      counts: { warning: 2, critical: 1, info: 0 },
      servers: {},
    }
    render(
      <AuthGate api={signedInApi} meApi={meApi({ organization: allPermissions })}>
        <RouterProvider>
          <Topbar alertSummary={summary} serverName="vps-web-01" onOpenMenu={vi.fn()} />
        </RouterProvider>
      </AuthGate>,
    )

    const crumbs = await screen.findByRole('navigation', { name: 'Breadcrumb' })
    expect(within(crumbs).getByText('Infrastructure')).toBeTruthy()
    expect(within(crumbs).getByRole('link', { name: 'Servers' }).getAttribute('href')).toBe(
      '/servers',
    )
    expect(within(crumbs).getByText('vps-web-01')).toBeTruthy()

    const bell = await screen.findByRole('link', { name: '3 open alerts' })
    expect(bell.getAttribute('href')).toBe('/alerts')
    expect(within(bell).getByText('3').classList.contains('shell__bell-dot--crit')).toBe(true)
  })
})

describe('UserMenu', () => {
  it('changes language and theme and marks the current choices', async () => {
    render(
      <AuthGate api={signedInApi} meApi={meApi({ organization: allPermissions })}>
        <RouterProvider>
          <UserMenu />
        </RouterProvider>
      </AuthGate>,
    )

    const button = await screen.findByRole('button', { name: 'admin' })
    fireEvent.click(button)

    expect(screen.getByText('admin')).toBeTruthy()
    expect(screen.queryByRole('menuitem', { name: 'admin' })).toBeNull()

    fireEvent.click(screen.getByRole('menuitem', { name: 'Català' }))
    await waitFor(() => expect(i18n.resolvedLanguage).toBe('ca'))

    fireEvent.click(button)
    fireEvent.click(screen.getByRole('menuitem', { name: 'Fosc' }))

    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    expect(localStorage.getItem('talaia.theme')).toBe('dark')
  })
})
