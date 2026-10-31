import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { App, CreatedApp, Domain, TemplateSummary, WizardApi } from '../../../api/apps'
import type { FleetClient } from '../../../api/client'
import type { MeApi } from '../../../api/me'
import type { Server } from '../../../api/types'
import { PermissionsProvider } from '../../../auth/permissions'
import i18n from '../../../i18n'
import { RouterProvider } from '../../../router'
import { NewAppWizard } from './NewAppWizard'

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: 'srv-1',
    name: 'vps-web-01',
    os: 'Debian 12',
    location: '',
    vcpu: 4,
    memoryGb: 8,
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

function createdApp(overrides: Partial<CreatedApp> = {}): CreatedApp {
  return {
    ...app(),
    deployKeyPublic: 'ssh-ed25519 AAAA example',
    webhookSecret: 'hook-secret',
    webhookUrl: 'https://panel.example.com/api/v1/hooks/git/app-1',
    ...overrides,
  }
}

const templates: TemplateSummary[] = [
  {
    id: 'uptime-kuma',
    version: '1.0.0',
    names: { en: 'Uptime Kuma', es: 'Uptime Kuma', ca: 'Uptime Kuma' },
    descriptions: { en: 'Uptime monitor', es: 'Monitor', ca: 'Monitor' },
    image: 'louislam/uptime-kuma:1.23.16',
    service: 'app',
    port: 3001,
    ramMB: 512,
  },
]

function fleet(): FleetClient {
  return {
    listServers: () => Promise.resolve([server()]),
    listActivity: () => Promise.resolve([]),
    createEnrollment: () => Promise.reject(new Error('unexpected enrollment')),
  }
}

function meApi(): MeApi {
  return {
    get: () =>
      Promise.resolve({
        user: { id: 'u1', username: 'admin', display_name: 'Admin', email: '' },
        bindings: { bindings: [] },
        permissions: {
          organization: ['stack.view', 'stack.edit', 'stack.deploy', 'server.proxy', 'server.view'],
          servers: {},
          stacks: {},
          containers: {},
        },
      }),
  }
}

function fakeApi(overrides: Partial<WizardApi> = {}): WizardApi {
  const domain: Domain = {
    id: 'domain-1',
    appId: 'app-1',
    hostname: 'shop.example.com',
    service: 'app',
    port: 3000,
    createdAt: '2026-10-31T10:00:00Z',
    warning: '',
  }
  return {
    list: () => Promise.resolve([]),
    listByServer: () => Promise.resolve([]),
    get: () => Promise.reject(new Error('unexpected get')),
    delete: () => Promise.reject(new Error('unexpected delete')),
    variables: () => Promise.resolve([]),
    setVariable: () => Promise.reject(new Error('unexpected set')),
    deleteVariable: () => Promise.reject(new Error('unexpected delete')),
    deployments: () => Promise.resolve([]),
    deployment: () => Promise.reject(new Error('unexpected deployment')),
    deploy: vi.fn(() => Promise.resolve()),
    rollback: () => Promise.reject(new Error('unexpected rollback')),
    templates: vi.fn(() => Promise.resolve(templates)),
    create: vi.fn(() => Promise.resolve(createdApp())),
    appDomains: vi.fn(() => Promise.resolve([domain])),
    addDomain: vi.fn(() => Promise.resolve(domain)),
    proxy: vi.fn(() =>
      Promise.resolve({
        mode: 'external' as const,
        domains: [
          {
            hostname: 'shop.example.com',
            appId: 'app-1',
            hostPort: 23456,
            warning: '',
            apache: '<VirtualHost *:80>\n  ProxyPass / http://127.0.0.1:23456/\n</VirtualHost>',
            nginx: '',
            certbot: 'sudo certbot --apache -d shop.example.com',
          },
        ],
      }),
    ),
    proxyAction: vi.fn(() => Promise.resolve()),
    ...overrides,
  }
}

function renderWizard(api: WizardApi, path = '/apps/new') {
  window.history.replaceState({}, '', path)
  return render(
    <RouterProvider>
      <PermissionsProvider meApi={meApi()}>
        <NewAppWizard api={api} client={fleet()} />
      </PermissionsProvider>
    </RouterProvider>,
  )
}

async function chooseServerAndName(name = 'shop') {
  fireEvent.click(await screen.findByRole('radio', { name: /vps-web-01/ }))
  fireEvent.change(screen.getByLabelText('App name'), { target: { value: name } })
}

beforeEach(async () => {
  localStorage.clear()
  await i18n.changeLanguage('en')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('new app wizard', () => {
  it('creates a Git app and shows the one-time secrets', async () => {
    const api = fakeApi()
    renderWizard(api)

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('heading', { name: 'Build and runtime' })).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/example/shop.git' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))

    expect(await screen.findByRole('heading', { name: 'Where should it run?' })).toBeTruthy()
    expect((screen.getByLabelText('App name') as HTMLInputElement).value).toBe('shop')
    await chooseServerAndName()
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))

    expect(await screen.findByRole('heading', { name: 'Domain and proxy' })).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Hostname'), {
      target: { value: 'shop.example.com' },
    })
    expect(await screen.findByText(/ProxyPass \/ http:\/\/127\.0\.0\.1:23456\//)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))

    expect(await screen.findByRole('heading', { name: 'Review and create' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Create app' }))

    expect(await screen.findByRole('heading', { name: 'App created' })).toBeTruthy()
    expect(api.create).toHaveBeenCalledWith(
      'srv-1',
      expect.objectContaining({
        stackName: 'shop',
        source: 'git',
        repoUrl: 'https://github.com/example/shop.git',
        branch: 'main',
        service: 'app',
        port: 3000,
      }),
    )
    expect(api.addDomain).toHaveBeenCalledWith('app-1', {
      hostname: 'shop.example.com',
      service: 'app',
      port: 3000,
    })
    expect(await screen.findByText('ssh-ed25519 AAAA example')).toBeTruthy()
    expect(screen.getByText('https://panel.example.com/api/v1/hooks/git/app-1')).toBeTruthy()
    expect(screen.getByText('hook-secret')).toBeTruthy()
    expect(screen.getByText('Copy these values now. They will not be shown again.')).toBeTruthy()
  })

  it('creates an app from an existing image', async () => {
    const api = fakeApi({
      create: vi.fn(() =>
        Promise.resolve(createdApp({ source: 'image', image: 'nginx:1.27', hostPort: 0 })),
      ),
    })
    renderWizard(api)

    fireEvent.click(screen.getByRole('radio', { name: /Existing image/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('heading', { name: 'Build and runtime' })).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Container image'), {
      target: { value: 'nginx:1.27' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))

    expect(await screen.findByRole('heading', { name: 'Where should it run?' })).toBeTruthy()
    expect((screen.getByLabelText('App name') as HTMLInputElement).value).toBe('nginx')
    await chooseServerAndName('shop-image')
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('heading', { name: 'Domain and proxy' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('heading', { name: 'Review and create' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Create app' }))

    expect(await screen.findByRole('heading', { name: 'App created' })).toBeTruthy()
    expect(api.create).toHaveBeenCalledWith(
      'srv-1',
      expect.objectContaining({ source: 'image', image: 'nginx:1.27', templateId: '' }),
    )
    expect(api.addDomain).not.toHaveBeenCalled()
    expect(api.deploy).not.toHaveBeenCalled()
  })

  it('creates a template app and queues its first deployment', async () => {
    const api = fakeApi({
      create: vi.fn(() =>
        Promise.resolve(
          createdApp({
            source: 'template',
            templateId: 'uptime-kuma',
            templateVersion: '1.0.0',
            image: 'louislam/uptime-kuma:1.23.16',
            stackName: 'uptime',
            hostPort: 0,
          }),
        ),
      ),
    })
    renderWizard(api)

    fireEvent.click(screen.getByRole('radio', { name: /Template/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('heading', { name: 'Build and runtime' })).toBeTruthy()
    fireEvent.click(await screen.findByRole('radio', { name: /Uptime Kuma/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))

    expect(await screen.findByRole('heading', { name: 'Where should it run?' })).toBeTruthy()
    expect((screen.getByLabelText('App name') as HTMLInputElement).value).toBe('uptime-kuma')
    await chooseServerAndName('uptime')
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('heading', { name: 'Domain and proxy' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('heading', { name: 'Review and create' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Create app' }))

    expect(await screen.findByRole('heading', { name: 'App created' })).toBeTruthy()
    expect(api.create).toHaveBeenCalledWith(
      'srv-1',
      expect.objectContaining({
        stackName: 'uptime',
        source: 'template',
        templateId: 'uptime-kuma',
        templateVersion: '1.0.0',
      }),
    )
    await waitFor(() => expect(api.deploy).toHaveBeenCalledWith('app-1'))
    expect(screen.getByText(/image and template sources/)).toBeTruthy()
  })

  it('prefills the template selected by the gallery', async () => {
    const api = fakeApi()
    renderWizard(api, '/apps/new?template=uptime-kuma')

    expect(await screen.findByRole('heading', { name: 'Build and runtime' })).toBeTruthy()
    const radio = (await screen.findByRole('radio', {
      name: /Uptime Kuma/,
    })) as HTMLInputElement
    expect(radio.checked).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('heading', { name: 'Where should it run?' })).toBeTruthy()
    expect((screen.getByLabelText('App name') as HTMLInputElement).value).toBe('uptime-kuma')
  })

  it('blocks each step until its fields are valid', async () => {
    const api = fakeApi()
    renderWizard(api)

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('heading', { name: 'Build and runtime' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByText('Use a github.com URL over HTTPS or SSH.')).toBeTruthy()

    fireEvent.change(screen.getByLabelText('Repository URL'), {
      target: { value: 'https://github.com/example/shop.git' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('heading', { name: 'Where should it run?' })).toBeTruthy()
    fireEvent.click(await screen.findByRole('radio', { name: /vps-web-01/ }))
    fireEvent.change(screen.getByLabelText('App name'), { target: { value: 'Bad Name' } })
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(
      await screen.findByText(
        'The app name must use lowercase letters, numbers, hyphens and underscores.',
      ),
    ).toBeTruthy()

    fireEvent.change(screen.getByLabelText('App name'), { target: { value: 'shop' } })
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('heading', { name: 'Domain and proxy' })).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Hostname'), { target: { value: 'bad host' } })
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByText('Enter a valid hostname.')).toBeTruthy()
  })
})
