import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { App, Domain, WizardApi } from '../api/apps'
import type { FleetClient } from '../api/client'
import type { MeApi, Permission } from '../api/me'
import type { Server } from '../api/types'
import { PermissionsProvider } from '../auth/permissions'
import i18n from '../i18n'
import { RouterProvider } from '../router'
import { Domains } from './Domains'

function app(overrides: Partial<App> = {}): App {
  return {
    id: 'app-1',
    serverId: 'srv-1',
    stackName: 'shop',
    source: 'git',
    repoUrl: 'https://github.com/example/shop.git',
    branch: 'main',
    build: 'dockerfile',
    dockerfilePath: 'Dockerfile',
    buildContext: '.',
    image: '',
    service: 'app',
    port: 3000,
    hostPort: 23456,
    autoDeploy: true,
    pollEnabled: false,
    zeroDowntime: true,
    templateId: '',
    templateVersion: '',
    createdAt: '2026-10-31T10:00:00Z',
    updatedAt: '2026-10-31T10:00:00Z',
    updatedBy: 'admin',
    ...overrides,
  }
}

function domain(overrides: Partial<Domain> = {}): Domain {
  return {
    id: 'domain-1',
    appId: 'app-1',
    hostname: 'shop.example.com',
    service: 'app',
    port: 3000,
    createdAt: '2026-10-31T10:00:00Z',
    warning: '',
    ...overrides,
  }
}

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: 'srv-1',
    name: 'vps-web-01',
    os: 'Debian 12',
    location: '',
    vcpu: 2,
    memoryGb: 4,
    status: 'online',
    cpuPercent: 10,
    memPercent: 20,
    diskPercent: 30,
    containers: 2,
    stacks: 1,
    tags: [],
    lastSeen: '2026-10-31T10:00:00Z',
    agentVersion: '0.2.8',
    agentLatestVersion: '0.2.8',
    agentUpdateAvailable: false,
    agentUpdateManual: false,
    agentUpgradeCommand: '',
    ...overrides,
  }
}

function meApi(permissions: Permission[] = ['stack.view', 'server.proxy']): MeApi {
  return {
    get: () =>
      Promise.resolve({
        user: { id: 'u1', username: 'admin', display_name: 'Admin', email: '' },
        bindings: { bindings: [] },
        permissions: {
          organization: permissions,
          servers: {},
          stacks: {},
          containers: {},
        },
      }),
  }
}

function fakeApi(): WizardApi {
  const apps = [app(), app({ id: 'app-2', serverId: 'srv-2', stackName: 'vault' })]
  const unexpected = () => Promise.reject(new Error('unexpected call'))
  return {
    list: vi.fn(() => Promise.resolve(apps)),
    listByServer: vi.fn(() => Promise.resolve(apps)),
    get: unexpected,
    delete: unexpected,
    variables: unexpected,
    setVariable: unexpected,
    deleteVariable: unexpected,
    deployments: unexpected,
    deployment: unexpected,
    deploy: unexpected,
    rollback: unexpected,
    templates: vi.fn(() => Promise.resolve([])),
    create: unexpected,
    appDomains: vi.fn((appId: string) =>
      Promise.resolve(
        appId === 'app-1'
          ? [domain()]
          : [
              domain({
                id: 'domain-2',
                appId: 'app-2',
                hostname: 'vault.example.com',
                warning: 'the hostname does not resolve to the server',
              }),
            ],
      ),
    ),
    addDomain: unexpected,
    proxy: vi.fn((serverId: string) =>
      Promise.resolve(
        serverId === 'srv-1'
          ? {
              mode: 'external' as const,
              domains: [
                {
                  hostname: 'shop.example.com',
                  appId: 'app-1',
                  hostPort: 23456,
                  warning: '',
                  apache:
                    '<VirtualHost *:80>\n  ProxyPass / http://127.0.0.1:23456/\n</VirtualHost>',
                  nginx: 'server {\n  proxy_pass http://127.0.0.1:23456;\n}',
                  certbot: 'sudo certbot --apache -d shop.example.com',
                },
              ],
            }
          : {
              mode: 'managed' as const,
              domains: [],
              certificates: [{ hostname: 'vault.example.com', state: 'valid' }],
            },
      ),
    ),
    proxyAction: unexpected,
  }
}

function fleet(): FleetClient {
  return {
    listServers: () => Promise.resolve([server(), server({ id: 'srv-2', name: 'vps-db' })]),
    listActivity: () => Promise.resolve([]),
    createEnrollment: () => Promise.reject(new Error('unexpected enrollment')),
  }
}

function renderDomains(api: WizardApi, permissions: Permission[] = ['stack.view', 'server.proxy']) {
  window.history.replaceState({}, '', '/domains')
  render(
    <RouterProvider>
      <PermissionsProvider meApi={meApi(permissions)}>
        <Domains api={api} client={fleet()} />
      </PermissionsProvider>
    </RouterProvider>,
  )
}

let writeText: ReturnType<typeof vi.fn>

beforeEach(async () => {
  localStorage.clear()
  writeText = vi.fn(() => Promise.resolve())
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText },
    configurable: true,
  })
  await i18n.changeLanguage('en')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('domains', () => {
  it('lists external and managed domains with their state', async () => {
    const api = fakeApi()
    renderDomains(api)

    expect(await screen.findByText('shop.example.com')).toBeTruthy()
    expect(await screen.findByText('vault.example.com')).toBeTruthy()
    expect(screen.getAllByText('Handled by your Apache').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Certificate valid').length).toBeGreaterThan(0)
    expect(screen.getByText('The hostname does not point to this server.')).toBeTruthy()
    expect(api.proxy).toHaveBeenCalledWith('srv-1')
    expect(api.proxy).toHaveBeenCalledWith('srv-2')
  })

  it('opens the setup drawer for external rows and copies a block', async () => {
    const api = fakeApi()
    renderDomains(api)

    const setupButtons = await screen.findAllByRole('button', { name: 'View setup' })
    expect(setupButtons).toHaveLength(1)
    fireEvent.click(setupButtons[0]!)
    const drawer = await screen.findByRole('dialog')
    expect(within(drawer).getByText('Domain setup')).toBeTruthy()
    expect(within(drawer).getByText('shop.example.com')).toBeTruthy()
    expect(within(drawer).getByText('127.0.0.1:23456')).toBeTruthy()
    expect(within(drawer).getByText(/ProxyPass \/ http:\/\/127\.0\.0\.1:23456\//)).toBeTruthy()
    expect(within(drawer).getByText(/proxy_pass http:\/\/127\.0\.0\.1:23456/)).toBeTruthy()
    expect(within(drawer).getByText(/certbot --apache -d shop.example.com/)).toBeTruthy()
    expect(within(drawer).getAllByRole('button', { name: 'Copy' })).toHaveLength(3)

    fireEvent.click(within(drawer).getAllByRole('button', { name: 'Copy' })[0]!)
    await waitFor(() => expect(writeText).toHaveBeenCalled())
    expect(writeText.mock.calls[0]?.[0]).toContain('<VirtualHost *:80>')
    expect(await within(drawer).findByRole('button', { name: 'Copied' })).toBeTruthy()
  })

  it('keeps the state text and hides setup without server.proxy', async () => {
    const api = fakeApi()
    renderDomains(api, ['stack.view'])

    expect(await screen.findByText('shop.example.com')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'View setup' })).toBeNull()
    expect(screen.getAllByText('Unknown').length).toBeGreaterThan(0)
    expect(api.proxy).not.toHaveBeenCalled()
  })

  it('filters domains by name', async () => {
    const api = fakeApi()
    renderDomains(api)

    expect(await screen.findByText('shop.example.com')).toBeTruthy()
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'vault' } })
    expect(screen.queryByText('shop.example.com')).toBeNull()
    expect(screen.getByText('vault.example.com')).toBeTruthy()
  })
})
