import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from './App'
import { AuthGate } from './auth/AuthGate'
import type { AptApi } from './api/apt'
import type { AlertsApi } from './api/alerts'
import type { AuditApi } from './api/audit'
import type { AuthApi } from './api/auth'
import { createMockFleetClient, type FleetClient } from './api/client'
import type { ContainersApi } from './api/containers'
import type { Group, GroupsApi } from './api/groups'
import type { Invite, InvitesApi } from './api/invites'
import type { JournalApi } from './api/journal'
import type { MeApi, Permission } from './api/me'
import type { ServersApi } from './api/servers'
import type { ServiceUnit, ServicesApi } from './api/services'
import type { SystemApi } from './api/system'
import type { Team, TeamsApi } from './api/teams'
import type { AdminUser, UsersApi } from './api/users'
import i18n from './i18n'
import { ATTRIBUTION } from './screens/About'

const overviewTitles = { ca: 'Resum', es: 'Resumen', en: 'Overview' } as const
const serversTitles = { ca: 'Servidors', es: 'Servidores', en: 'Servers' } as const
const settingsTitles = { ca: 'Configuració', es: 'Ajustes', en: 'Settings' } as const
const aboutTitles = { ca: 'Sobre Talaia', es: 'Acerca de Talaia', en: 'About Talaia' } as const
const navOverview = { ca: 'Resum', es: 'Resumen', en: 'Overview' } as const
const navServers = { ca: 'Servidors', es: 'Servidores', en: 'Servers' } as const
const navContainers = { ca: 'Contenidors', es: 'Contenedores', en: 'Containers' } as const
const navStacks = { ca: 'Stacks', es: 'Stacks', en: 'Stacks' } as const
const navSettings = { ca: 'Configuració', es: 'Ajustes', en: 'Settings' } as const
const navAudit = { ca: 'Auditoria', es: 'Auditoría', en: 'Audit' } as const
const navAbout = { ca: 'Sobre', es: 'Acerca de', en: 'About' } as const
const navUsers = { ca: 'Usuaris', es: 'Usuarios', en: 'Users' } as const
const navAlerts = { ca: 'Alertes', es: 'Alertas', en: 'Alerts' } as const
const usersHeading = 'Users and permissions'

const adminPermissions: Permission[] = [
  'container.view',
  'container.logs',
  'container.control',
  'container.terminal',
  'container.remove',
  'stack.view',
  'stack.deploy',
  'stack.edit',
  'server.view',
  'server.remove',
  'server.enroll',
  'server.agent_update',
  'resources.manage',
  'host.services.view',
  'host.services.control',
  'host.journal',
  'host.apt.view',
  'host.apt.upgrade',
  'host.reboot',
  'host.terminal',
  'users.manage',
  'settings.manage',
  'audit.view',
  'recordings.view',
  'apikeys.manage',
  'alerts.view',
  'alerts.silence',
  'alerts.manage',
]

const adminAlertsApi: AlertsApi = {
  list: () => Promise.resolve([]),
  summary: () => Promise.resolve({ counts: { warning: 0, critical: 0, info: 0 }, servers: {} }),
  silences: () => Promise.resolve([]),
  acknowledge: () => Promise.resolve(),
  unwatch: () => Promise.resolve(),
  createSilence: () => Promise.reject(new Error('unexpected silence')),
  deleteSilence: () => Promise.resolve(),
}

const adminMe: MeApi = {
  get: () =>
    Promise.resolve({
      user: { id: 'admin', username: 'admin', display_name: 'Admin', email: '' },
      bindings: { bindings: [] },
      permissions: {
        organization: adminPermissions,
        servers: {},
        stacks: {},
        containers: {},
      },
    }),
}

const viewerMe: MeApi = {
  get: () =>
    Promise.resolve({
      user: { id: 'u1', username: 'alice', display_name: 'Alice', email: '' },
      bindings: { bindings: [] },
      permissions: {
        organization: [],
        servers: { 'vps-web-01': ['server.view'] },
        stacks: {},
        containers: {},
      },
    }),
}

/** signedInApi is the authentication API of an already verified session. */
const signedInApi: AuthApi = {
  session: () => Promise.resolve({ username: 'admin', stage: 'full' }),
  login: () => Promise.reject(new Error('the panel is already signed in')),
  startTotpSetup: () => Promise.reject(new Error('the panel is already signed in')),
  confirmTotpSetup: () => Promise.reject(new Error('the panel is already signed in')),
  verifyTotp: () => Promise.reject(new Error('the panel is already signed in')),
  logout: () => Promise.resolve(),
}

const pendingAptApi: AptApi = {
  status: () => new Promise<string>(() => undefined),
  upgrade: () => Promise.reject(new Error('unexpected apt upgrade')),
  reboot: () => Promise.reject(new Error('unexpected reboot')),
  job: () => Promise.reject(new Error('unexpected apt job')),
}

const pendingServicesApi: ServicesApi = {
  list: () => new Promise<ServiceUnit[]>(() => undefined),
  action: () => Promise.reject(new Error('unexpected service action')),
  job: () => Promise.reject(new Error('unexpected service job')),
}

const pendingJournalApi: JournalApi = {
  read: () => new Promise<string>(() => undefined),
  job: () => Promise.reject(new Error('unexpected journal job')),
}

function systemApi(): SystemApi {
  const version = {
    version: '1.2.3',
    commit: 'a'.repeat(40),
    update: {
      current: 'a'.repeat(40),
      latest: 'a'.repeat(40),
      pending: [],
      notes: { en: [], es: [] },
      ci: 'green' as const,
      checkedAt: '2026-10-21T08:00:00Z',
      error: '',
      updateAvailable: false,
    },
    status: {
      state: 'idle' as const,
      from: '',
      to: '',
      startedAt: '',
      finishedAt: '',
      log: [],
    },
  }
  return {
    version: () => Promise.resolve(version),
    check: () => Promise.resolve(version),
    update: () => Promise.reject(new Error('unexpected update')),
  }
}

function usersApi(): UsersApi {
  const unexpected = () => Promise.reject(new Error('unexpected users call'))
  return {
    list: () => Promise.resolve([] as AdminUser[]),
    update: unexpected,
    disable: unexpected,
    enable: unexpected,
    delete: unexpected,
    listBindings: () => Promise.resolve([]),
    addBinding: unexpected,
    updateBinding: unexpected,
    removeBinding: unexpected,
  }
}

function teamsApi(): TeamsApi {
  const unexpected = () => Promise.reject(new Error('unexpected team call'))
  return {
    list: () => Promise.resolve([] as Team[]),
    create: unexpected,
    get: unexpected,
    rename: unexpected,
    remove: unexpected,
    addMember: unexpected,
    removeMember: unexpected,
    listBindings: () => Promise.resolve([]),
    addBinding: unexpected,
  }
}

function groupsApi(): GroupsApi {
  const unexpected = () => Promise.reject(new Error('unexpected group call'))
  return {
    list: () => Promise.resolve([] as Group[]),
    create: unexpected,
    rename: unexpected,
    remove: unexpected,
    setServerGroup: unexpected,
  }
}

function invitesApi(): InvitesApi {
  const unexpected = () => Promise.reject(new Error('unexpected invite call'))
  return {
    list: () => Promise.resolve([] as Invite[]),
    create: unexpected,
    revoke: unexpected,
    info: unexpected,
    accept: unexpected,
  }
}

function serversApi(): ServersApi {
  const unexpected = () => Promise.reject(new Error('unexpected server call'))
  return {
    listServers: () => Promise.resolve([]),
    removeServer: unexpected,
    updateAgent: unexpected,
    updateAllAgents: unexpected,
  }
}

function containersApi(): ContainersApi {
  return {
    list: () =>
      Promise.resolve({
        connected: true,
        dockerAvailable: true,
        containers: [
          {
            id: 'aaaaaaaaaaaa',
            name: 'shop-web-1',
            image: 'nginx',
            state: 'running',
            status: 'Up',
            composeProject: 'shop',
            composeService: 'web',
          },
        ],
      }),
    run: () => Promise.reject(new Error('unexpected')),
    logs: () => Promise.reject(new Error('unexpected')),
    stacks: () =>
      Promise.resolve({ connected: true, stacks: [], adopted: [], broken: [], adoptable: [] }),
    adoptStack: () => Promise.reject(new Error('unexpected')),
    listResources: () => Promise.reject(new Error('unexpected')),
    removeResources: () => Promise.reject(new Error('unexpected')),
    writeStack: () => Promise.reject(new Error('unexpected')),
    readStack: () => Promise.reject(new Error('unexpected')),
    runStack: () => Promise.reject(new Error('unexpected')),
    job: () => Promise.reject(new Error('unexpected')),
  }
}

interface RenderOptions {
  me?: MeApi
  client?: FleetClient
  containersApi?: ContainersApi
  aptApi?: AptApi
  servicesApi?: ServicesApi
  journalApi?: JournalApi
  systemApi?: SystemApi
  auditApi?: AuditApi
  alertsApi?: AlertsApi
  usersApi?: UsersApi
  teamsApi?: TeamsApi
  groupsApi?: GroupsApi
  invitesApi?: InvitesApi
  serversApi?: ServersApi
}

/** renderApp mounts the panel behind an authenticated gate. */
function renderApp(options: RenderOptions = {}) {
  return render(
    <AuthGate api={signedInApi} meApi={options.me ?? adminMe}>
      <App
        client={options.client ?? createMockFleetClient()}
        containersApi={options.containersApi}
        aptApi={options.aptApi}
        servicesApi={options.servicesApi}
        journalApi={options.journalApi}
        systemApi={options.systemApi}
        auditApi={options.auditApi}
        alertsApi={options.alertsApi ?? adminAlertsApi}
        usersApi={options.usersApi}
        teamsApi={options.teamsApi}
        groupsApi={options.groupsApi}
        invitesApi={options.invitesApi}
        serversApi={options.serversApi}
      />
    </AuthGate>,
  )
}

function currentTitle(): string {
  return screen.getByRole('heading', { level: 1 }).textContent ?? ''
}

function mainNav(): HTMLElement {
  return screen.getByRole('navigation', { name: i18n.t('nav.label') })
}

async function openSidebarLink(name: string) {
  const nav = mainNav()
  await within(nav).findByRole('link', { name })
  fireEvent.click(within(nav).getByRole('link', { name }))
}

function serverApis() {
  return {
    containersApi: containersApi(),
    aptApi: pendingAptApi,
    servicesApi: pendingServicesApi,
    journalApi: pendingJournalApi,
  }
}

describe('App', () => {
  beforeEach(async () => {
    localStorage.clear()
    sessionStorage.clear()
    document.documentElement.removeAttribute('data-theme')
    window.history.replaceState({}, '', '/')
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
    window.history.replaceState({}, '', '/')
  })

  it('shows the Overview screen by default', async () => {
    renderApp()

    expect(i18n.resolvedLanguage).toBe('en')
    expect((await screen.findAllByText('vps-web-01')).length).toBeGreaterThan(0)
    expect(currentTitle()).toBe(overviewTitles.en)
    expect(window.location.pathname).toBe('/')
  })

  it('updates the URL and the screen when a sidebar link is clicked', async () => {
    renderApp({ systemApi: systemApi() })
    await screen.findAllByText('vps-web-01')

    await openSidebarLink(navSettings.en)

    expect(window.location.pathname).toBe('/settings')
    expect(currentTitle()).toBe(settingsTitles.en)
    expect(await screen.findByText('1.2.3')).toBeTruthy()
  })

  it('opens every screen from the sidebar', async () => {
    const auditApi: AuditApi = { list: vi.fn(() => Promise.resolve({ entries: [], next: '' })) }
    renderApp({
      systemApi: systemApi(),
      auditApi,
      usersApi: usersApi(),
      teamsApi: teamsApi(),
      groupsApi: groupsApi(),
      invitesApi: invitesApi(),
      serversApi: serversApi(),
    })
    await screen.findAllByText('vps-web-01')

    await openSidebarLink(navServers.en)
    expect(window.location.pathname).toBe('/servers')
    expect(await screen.findByRole('heading', { level: 1, name: serversTitles.en })).toBeTruthy()

    await openSidebarLink(navContainers.en)
    expect(window.location.pathname).toBe('/containers')
    expect(currentTitle()).toBe(navContainers.en)
    expect(screen.getByText('Coming in the next update')).toBeTruthy()

    await openSidebarLink(navStacks.en)
    expect(window.location.pathname).toBe('/stacks')
    expect(currentTitle()).toBe(navStacks.en)

    await openSidebarLink(navAlerts.en)
    expect(window.location.pathname).toBe('/alerts')
    expect(currentTitle()).toBe(navAlerts.en)

    await openSidebarLink(navAudit.en)
    expect(window.location.pathname).toBe('/audit')
    expect(await screen.findByRole('heading', { level: 1, name: navAudit.en })).toBeTruthy()
    expect(auditApi.list).toHaveBeenCalled()

    await openSidebarLink(navUsers.en)
    expect(window.location.pathname).toBe('/users')
    expect(await screen.findByRole('heading', { level: 1, name: usersHeading })).toBeTruthy()

    await openSidebarLink(navSettings.en)
    expect(window.location.pathname).toBe('/settings')
    expect(await screen.findByText('1.2.3')).toBeTruthy()

    await openSidebarLink(navOverview.en)
    expect(window.location.pathname).toBe('/')
    expect(await screen.findByRole('heading', { level: 1, name: overviewTitles.en })).toBeTruthy()
  })

  it('shows the grouped navigation in order', async () => {
    renderApp()
    await screen.findAllByText('vps-web-01')

    const nav = mainNav()
    await within(nav).findByRole('link', { name: navUsers.en })

    expect(
      within(nav)
        .getAllByRole('link')
        .map((link) => link.textContent),
    ).toEqual([
      navOverview.en,
      navServers.en,
      navContainers.en,
      navStacks.en,
      navAlerts.en,
      navAudit.en,
      navUsers.en,
      navSettings.en,
    ])
  })

  it('navigates to About and back to the overview', async () => {
    renderApp()
    await screen.findAllByText('vps-web-01')

    fireEvent.click(screen.getByRole('button', { name: 'admin' }))
    fireEvent.click(screen.getByRole('menuitem', { name: navAbout.en }))

    expect(window.location.pathname).toBe('/about')
    expect(currentTitle()).toBe(aboutTitles.en)
    expect(screen.getByText(ATTRIBUTION)).toBeTruthy()
    expect(screen.getByRole('link', { name: 'WNCL-TP-1.0' }).getAttribute('href')).toBe(
      'https://github.com/catlinux/Talaia',
    )

    await openSidebarLink(navOverview.en)
    expect(window.location.pathname).toBe('/')
    expect(currentTitle()).toBe(overviewTitles.en)
  })

  it('keeps the attribution text in every language', async () => {
    renderApp()
    await screen.findAllByText('vps-web-01')

    for (const language of ['ca', 'es', 'en'] as const) {
      await i18n.changeLanguage(language)
      fireEvent.click(screen.getByRole('button', { name: 'admin' }))
      fireEvent.click(screen.getByRole('menuitem', { name: navAbout[language] }))
      expect(currentTitle()).toBe(aboutTitles[language])
      expect(screen.getByText(ATTRIBUTION)).toBeTruthy()
    }
  })

  it('translates the navigation and the Overview title with the language', async () => {
    renderApp()
    await screen.findAllByText('vps-web-01')

    await i18n.changeLanguage('ca')

    expect(currentTitle()).toBe(overviewTitles.ca)
    const nav = mainNav()
    expect(within(nav).getByRole('link', { name: navOverview.ca })).toBeTruthy()
    expect(within(nav).getByRole('link', { name: navServers.ca })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'admin' }))
    expect(screen.getByRole('menuitem', { name: navAbout.ca })).toBeTruthy()
  })

  it('changes language and theme from the user menu', async () => {
    renderApp()
    await screen.findAllByText('vps-web-01')

    const account = screen.getByRole('button', { name: 'admin' })
    fireEvent.click(account)
    fireEvent.click(screen.getByRole('menuitem', { name: 'Català' }))
    await waitFor(() => expect(i18n.resolvedLanguage).toBe('ca'))
    expect(currentTitle()).toBe(overviewTitles.ca)

    fireEvent.click(account)
    fireEvent.click(screen.getByRole('menuitem', { name: 'Fosc' }))
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    expect(localStorage.getItem('talaia.theme')).toBe('dark')
  })

  it('opens the containers of a server and goes back to the fleet', async () => {
    renderApp(serverApis())
    await screen.findAllByText('vps-web-01')
    await openSidebarLink(navServers.en)
    sessionStorage.setItem('talaia.serverTab.vps-web-01', 'containers')

    fireEvent.click(await screen.findByRole('link', { name: 'vps-web-01' }))

    expect(await screen.findByText('shop-web-1')).toBeTruthy()
    expect(window.location.pathname).toBe('/servers/vps-web-01')
    expect(currentTitle()).toBe('vps-web-01')

    fireEvent.click(screen.getByRole('button', { name: 'Back to the fleet' }))
    expect(window.location.pathname).toBe('/servers')
    expect(currentTitle()).toBe(serversTitles.en)
  })

  it('loads a server from a deep link', async () => {
    window.history.replaceState({}, '', '/servers/abc')
    renderApp(serverApis())

    expect(await screen.findByRole('heading', { level: 1, name: 'abc' })).toBeTruthy()
    expect(window.location.pathname).toBe('/servers/abc')

    const crumbs = screen.getByRole('navigation', { name: 'Breadcrumb' })
    expect(within(crumbs).getByText('abc')).toBeTruthy()
  })

  it('returns to the servers on popstate', async () => {
    window.history.replaceState({}, '', '/servers/vps-web-01')
    renderApp(serverApis())
    await screen.findByRole('heading', { level: 1, name: 'vps-web-01' })

    window.history.replaceState({}, '', '/servers')
    window.dispatchEvent(new PopStateEvent('popstate'))

    expect(await screen.findByRole('heading', { level: 1, name: serversTitles.en })).toBeTruthy()
    expect(window.location.pathname).toBe('/servers')
  })

  it('shows a not-found page for unknown routes', async () => {
    window.history.replaceState({}, '', '/does-not-exist')
    renderApp()

    expect(await screen.findByRole('heading', { level: 1, name: 'Page not found' })).toBeTruthy()
    fireEvent.click(screen.getByRole('link', { name: 'Back to overview' }))

    expect(window.location.pathname).toBe('/')
    expect(await screen.findByRole('heading', { level: 1, name: overviewTitles.en })).toBeTruthy()
  })

  it('hides entries and guards routes the user cannot access', async () => {
    const view = renderApp({ me: viewerMe })
    await screen.findAllByText('vps-web-01')

    const nav = mainNav()
    await within(nav).findByRole('link', { name: navServers.en })
    for (const name of [navUsers.en, navAudit.en, navAlerts.en]) {
      expect(within(nav).queryByRole('link', { name })).toBeNull()
    }

    view.unmount()
    window.history.replaceState({}, '', '/users')
    renderApp({ me: viewerMe })

    expect(
      await screen.findByRole('heading', {
        level: 1,
        name: 'You do not have access to this page.',
      }),
    ).toBeTruthy()
    expect(window.location.pathname).toBe('/users')
  })

  it('opens and closes the mobile drawer with the hamburger and Escape', async () => {
    renderApp()
    await screen.findAllByText('vps-web-01')

    const sidebar = screen.getByRole('complementary')
    fireEvent.click(screen.getByRole('button', { name: 'Open navigation' }))
    expect(sidebar.classList.contains('side--open')).toBe(true)

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(sidebar.classList.contains('side--open')).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'Open navigation' }))
    await openSidebarLink(navOverview.en)
    expect(sidebar.classList.contains('side--open')).toBe(false)
  })
})
