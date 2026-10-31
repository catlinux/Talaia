import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type {
  App,
  AppsApi,
  Deployment,
  DeploymentDetail,
  Variable,
  VariableInput,
} from '../api/apps'
import type { FleetClient } from '../api/client'
import type { MeApi, MePermissions, Permission } from '../api/me'
import { PermissionsProvider } from '../auth/permissions'
import { ToastProvider } from '../components/ui'
import i18n from '../i18n'
import { RouterProvider } from '../router'
import { Apps } from './Apps'
import { AppDetail } from './apps/AppDetail'

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
    createdAt: '2026-10-30T10:00:00Z',
    updatedAt: '2026-10-31T10:00:00Z',
    updatedBy: 'alice',
    ...overrides,
  }
}

function deployment(overrides: Partial<Deployment> = {}): Deployment {
  return {
    id: 'dep-2',
    appId: 'app-1',
    number: 2,
    trigger: 'push',
    commitSha: 'abcdef1234567890',
    commitSubject: 'Ship the shop',
    imageRef: 'talaia/shop:abcdef123456',
    state: 'succeeded',
    startedAt: '2026-10-31T10:00:00Z',
    finishedAt: '2026-10-31T10:00:12Z',
    error: '',
    ...overrides,
  }
}

function variable(overrides: Partial<Variable> = {}): Variable {
  return {
    key: 'TOKEN',
    secret: true,
    buildTime: false,
    ...overrides,
  }
}

function fakeApi(overrides: Partial<AppsApi> = {}): AppsApi {
  const deployments = [
    deployment(),
    deployment({
      id: 'dep-1',
      number: 1,
      trigger: 'manual',
      state: 'failed',
      commitSha: 'fedcba9876543210',
      commitSubject: 'First try',
      error: 'build failed',
    }),
  ]
  const variables = [
    variable({ value: 'hidden-secret' }),
    variable({ key: 'PUBLIC_PORT', value: '3000', secret: false, buildTime: true }),
  ]
  return {
    list: vi.fn(() => Promise.resolve([app()])),
    listByServer: vi.fn(() => Promise.resolve([app()])),
    get: vi.fn(() => Promise.resolve(app())),
    variables: vi.fn(() => Promise.resolve(variables)),
    setVariable: vi.fn(() => Promise.resolve()),
    deleteVariable: vi.fn(() => Promise.resolve()),
    deployments: vi.fn(() => Promise.resolve(deployments)),
    deployment: vi.fn((_appId, deploymentId) =>
      Promise.resolve({
        ...(deployments.find((item) => item.id === deploymentId) ?? deployments[0]!),
        log: 'step 1\nstep 2',
      } as DeploymentDetail),
    ),
    deploy: vi.fn(() => Promise.resolve()),
    rollback: vi.fn(() => Promise.resolve()),
    ...overrides,
  }
}

function fleet(): FleetClient {
  return {
    listServers: () =>
      Promise.resolve([
        {
          id: 'srv-1',
          name: 'vps-web-01',
          os: 'Debian 12',
          location: '',
          vcpu: 2,
          memoryGb: 4,
          status: 'online' as const,
          cpuPercent: 10,
          memPercent: 20,
          diskPercent: 30,
          containers: 2,
          stacks: 1,
          tags: [],
          lastSeen: '2026-10-31T10:00:00Z',
          agentVersion: '0.2.0',
          agentLatestVersion: '0.2.8',
          agentUpdateAvailable: false,
          agentUpdateManual: false,
          agentUpgradeCommand: '',
        },
      ]),
    listActivity: () => Promise.resolve([]),
    createEnrollment: () => Promise.reject(new Error('unexpected enrollment')),
  }
}

function meApi(permissions: Partial<MePermissions> = {}): MeApi {
  return {
    get: () =>
      Promise.resolve({
        user: { id: 'u1', username: 'admin', display_name: 'Admin', email: '' },
        bindings: { bindings: [] },
        permissions: {
          organization: [],
          servers: {},
          stacks: {},
          containers: {},
          ...permissions,
        },
      }),
  }
}

function organization(...permissions: Permission[]): Partial<MePermissions> {
  return { organization: permissions }
}

function stackPermissions(permissions: Permission[]): Partial<MePermissions> {
  return { stacks: { 'srv-1': { shop: permissions } } }
}

function renderWithPermissions(me: MeApi, content: ReactNode) {
  return render(
    <ToastProvider>
      <RouterProvider>
        <PermissionsProvider meApi={me}>{content}</PermissionsProvider>
      </RouterProvider>
    </ToastProvider>,
  )
}

beforeEach(async () => {
  localStorage.clear()
  window.history.replaceState({}, '', '/apps')
  await i18n.changeLanguage('en')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('apps list', () => {
  it('lists apps with their server and source and filters them', async () => {
    const api = fakeApi()
    renderWithPermissions(
      meApi(organization('stack.view')),
      <Apps api={api} client={fleet()} refreshMs={0} />,
    )

    expect(await screen.findByRole('link', { name: 'shop' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'vps-web-01' })).toBeTruthy()
    expect(screen.getAllByText('Git').length).toBeGreaterThan(0)
    expect(screen.getByText('127.0.0.1:23456 → app:3000')).toBeTruthy()

    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'nothing' } })
    expect(await screen.findByText('No apps match these filters')).toBeTruthy()
  })

  it('hides the screen data from users without stack.view anywhere', async () => {
    const api = fakeApi()
    renderWithPermissions(
      meApi(organization('server.view')),
      <Apps api={api} client={fleet()} refreshMs={0} />,
    )
    await waitFor(() => expect(api.list).toHaveBeenCalled())
    expect(screen.queryByRole('link', { name: 'shop' })).toBeNull()
  })
})

describe('app detail roles', () => {
  it('viewer sees deployments, logs and masked variables but no actions', async () => {
    const api = fakeApi()
    renderWithPermissions(
      meApi(organization('stack.view')),
      <AppDetail appId="app-1" api={api} client={fleet()} refreshMs={0} logPollMs={0} />,
    )

    expect(await screen.findByRole('heading', { name: 'shop' })).toBeTruthy()
    expect(await screen.findByText('Succeeded')).toBeTruthy()
    expect(await screen.findByText('Failed')).toBeTruthy()
    expect(await screen.findByText('TOKEN')).toBeTruthy()
    expect(await screen.findByText('••••••••')).toBeTruthy()
    expect(screen.queryByText('hidden-secret')).toBeNull()
    expect(
      await screen.findByText(
        'Not available behind your own Apache: each deployment has a cut of a few seconds.',
      ),
    ).toBeTruthy()
    expect(
      screen
        .getByRole('link', { name: 'https://github.com/example/shop.git' })
        .getAttribute('title'),
    ).toBe('https://github.com/example/shop.git')
    expect(screen.queryByRole('link', { name: 'Apps' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Deploy now' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Roll back' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Logs' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Add variable' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
  })

  it('app operator can deploy and roll back but cannot edit variables', async () => {
    const api = fakeApi()
    renderWithPermissions(
      meApi(stackPermissions(['stack.view', 'stack.deploy'])),
      <AppDetail appId="app-1" api={api} client={fleet()} refreshMs={0} logPollMs={0} />,
    )

    expect(await screen.findByRole('button', { name: 'Deploy now' })).toBeTruthy()
    expect(await screen.findByText('Succeeded')).toBeTruthy()
    expect(screen.getAllByRole('button', { name: 'Roll back' }).length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Add variable' })).toBeNull()
    const logsButton = screen.getAllByRole('button', { name: 'Logs' })[0]!
    expect(logsButton.className).toBe('ui-btn')

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(api.deploy).toHaveBeenCalledWith('app-1', 'fedcba9876543210'))

    fireEvent.click(screen.getAllByRole('button', { name: 'Logs' })[0]!)
    expect(await screen.findByText('Deployment #2')).toBeTruthy()
    expect(await screen.findByText(/step 1/)).toBeTruthy()
    fireEvent.click(
      within(screen.getByRole('dialog')).getAllByRole('button', { name: 'Close' })[0]!,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Deploy now' }))
    await waitFor(() => expect(api.deploy).toHaveBeenCalledWith('app-1'))
    expect(await screen.findByText('Deployment queued.')).toBeTruthy()

    fireEvent.click(screen.getAllByRole('button', { name: 'Roll back' })[0]!)
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Roll back' }))
    await waitFor(() => expect(api.rollback).toHaveBeenCalledWith('app-1', 'dep-2'))
  })

  it('explains why Deploy now is disabled while a deployment is active', async () => {
    const api = fakeApi({
      deployments: vi.fn(() =>
        Promise.resolve([deployment({ state: 'building', commitSha: 'abcdef1234567890' })]),
      ),
    })
    renderWithPermissions(
      meApi(stackPermissions(['stack.view', 'stack.deploy'])),
      <AppDetail appId="app-1" api={api} client={fleet()} refreshMs={0} logPollMs={0} />,
    )

    const button = await screen.findByRole('button', { name: 'Deploy now' })
    await waitFor(() => expect(button.hasAttribute('disabled')).toBe(true))
    expect(button.getAttribute('title')).toBe('A deployment is already running.')
  })

  it('app editor can change variables but cannot deploy', async () => {
    const api = fakeApi()
    renderWithPermissions(
      meApi(stackPermissions(['stack.view', 'stack.edit'])),
      <AppDetail appId="app-1" api={api} client={fleet()} refreshMs={0} logPollMs={0} />,
    )

    expect(await screen.findByRole('button', { name: 'Add variable' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Deploy now' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Roll back' })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Add variable' }))
    fireEvent.change(screen.getByPlaceholderText('DATABASE_URL'), {
      target: { value: 'NEW_KEY' },
    })
    const form = screen.getByPlaceholderText('DATABASE_URL').closest('form')!
    fireEvent.change(within(form).getAllByRole('textbox')[1]!, {
      target: { value: 'new value' },
    })
    fireEvent.click(within(form).getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(api.setVariable).toHaveBeenCalledWith(
        'app-1',
        'NEW_KEY',
        expect.objectContaining<VariableInput>({
          value: 'new value',
          secret: false,
          buildTime: false,
        }),
      ),
    )

    fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0]!)
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(api.deleteVariable).toHaveBeenCalled())
  })
})
