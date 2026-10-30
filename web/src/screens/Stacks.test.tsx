import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Container, ContainerList, ContainersApi, Job, StackList } from '../api/containers'
import type { FleetClient } from '../api/client'
import type { MeApi, MePermissions } from '../api/me'
import type { Server } from '../api/types'
import { PermissionsProvider } from '../auth/permissions'
import { ToastProvider } from '../components/ui'
import i18n from '../i18n'
import { RouterProvider } from '../router'
import { Stacks } from './Stacks'

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
    stacks: 2,
    tags: [],
    lastSeen: '2026-10-19T12:00:00Z',
    agentVersion: '0.2.0',
    agentLatestVersion: '0.2.8',
    agentUpdateAvailable: false,
    agentUpdateManual: false,
    agentUpgradeCommand: '',
    ...overrides,
  }
}

function container(overrides: Partial<Container> = {}): Container {
  return {
    id: 'c1',
    name: 'web',
    image: 'nginx',
    state: 'running',
    status: 'Up',
    composeProject: 'shop',
    composeService: 'web',
    ...overrides,
  }
}

function client(servers: Server[]): FleetClient {
  return {
    listServers: () => Promise.resolve(servers),
    listActivity: () => Promise.resolve([]),
    createEnrollment: () => Promise.reject(new Error('unexpected enrollment')),
  }
}

function succeeded(): Job {
  return { id: 'job', state: 'succeeded', message: '', output: '' }
}

interface FakeOptions {
  lists?: Record<string, ContainerList>
  stacks?: Record<string, StackList>
}

function fakeApi(options: FakeOptions = {}) {
  const list = vi.fn((serverId: string) =>
    Promise.resolve(
      options.lists?.[serverId] ?? {
        connected: true,
        dockerAvailable: true,
        containers: [container()],
      },
    ),
  )
  const stacks = vi.fn((serverId: string) =>
    Promise.resolve(
      options.stacks?.[serverId] ?? {
        connected: true,
        stacks: ['shop'],
        adopted: [],
        broken: [],
        adoptable: [],
      },
    ),
  )
  const runStack = vi.fn(() => Promise.resolve('job'))
  const job = vi.fn(() => Promise.resolve(succeeded()))
  const unexpected = () => Promise.reject(new Error('unexpected call'))
  const api = {
    list,
    run: unexpected,
    stacks,
    writeStack: unexpected,
    readStack: unexpected,
    listResources: unexpected,
    removeResources: unexpected,
    adoptStack: unexpected,
    runStack,
    logs: unexpected,
    job,
  } satisfies ContainersApi
  return { api, list, stacks, runStack, job }
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

const allPermissions: MePermissions = {
  organization: ['stack.view', 'stack.deploy', 'container.view'],
  servers: {},
  stacks: {},
  containers: {},
}

function renderScreen(fleet: FleetClient, api: ContainersApi, me: MeApi = meApi(allPermissions)) {
  return render(
    <ToastProvider>
      <RouterProvider>
        <PermissionsProvider meApi={me}>
          <Stacks client={fleet} containersApi={api} pollMs={1} />
        </PermissionsProvider>
      </RouterProvider>
    </ToastProvider>,
  )
}

beforeEach(async () => {
  localStorage.clear()
  window.history.replaceState({}, '', '/stacks')
  await i18n.changeLanguage('en')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  window.history.replaceState({}, '', '/')
})

describe('Stacks', () => {
  it('lists managed stacks, their health, flags and the not-managed group', async () => {
    const { api } = fakeApi({
      lists: {
        'srv-1': {
          connected: true,
          dockerAvailable: true,
          containers: [
            container(),
            container({ id: 'c2', name: 'old', state: 'exited', composeProject: 'old' }),
            container({ id: 'c3', name: 'old-web', composeProject: 'old' }),
          ],
        },
      },
      stacks: {
        'srv-1': {
          connected: true,
          stacks: ['shop', 'old'],
          adopted: ['shop'],
          broken: ['old'],
          adoptable: ['orphan'],
        },
      },
    })
    renderScreen(client([server()]), api)

    expect(await screen.findByRole('heading', { level: 1, name: 'Stacks' })).toBeTruthy()
    expect(await screen.findByText(/2 stacks/)).toBeTruthy()
    const table = screen.getByRole('table')
    expect(within(table).getByRole('link', { name: 'shop' }).getAttribute('href')).toBe(
      '/servers/srv-1/stacks/shop',
    )
    expect(within(table).getByText('All running')).toBeTruthy()
    expect(within(table).getByText('Some stopped')).toBeTruthy()
    expect(within(table).getByText('Adopted')).toBeTruthy()
    expect(within(table).getByText('Files missing')).toBeTruthy()
    expect(within(table).getAllByRole('button', { name: 'More information' }).length).toBe(2)

    expect(screen.getByRole('heading', { name: 'Not managed' })).toBeTruthy()
    expect(screen.getByText('orphan')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Adopt' }).getAttribute('href')).toBe(
      '/servers/srv-1/containers',
    )
  })

  it('filters by status and server from the URL', async () => {
    const { api } = fakeApi({
      lists: {
        'srv-1': {
          connected: true,
          dockerAvailable: true,
          containers: [
            container(),
            container({ id: 'c2', name: 'old', state: 'exited', composeProject: 'old' }),
            container({ id: 'c3', name: 'old-web', composeProject: 'old' }),
          ],
        },
      },
      stacks: {
        'srv-1': {
          connected: true,
          stacks: ['shop', 'old'],
          adopted: [],
          broken: [],
          adoptable: [],
        },
      },
    })
    renderScreen(client([server()]), api)
    await screen.findByText('shop')

    fireEvent.change(screen.getByLabelText('Status'), { target: { value: 'problems' } })
    await waitFor(() => expect(window.location.search).toContain('status=problems'))
    expect(screen.queryByRole('link', { name: 'shop' })).toBeNull()
    expect(screen.getByRole('link', { name: 'old' })).toBeTruthy()

    fireEvent.change(screen.getByLabelText('Server'), { target: { value: 'srv-1' } })
    expect(await screen.findByRole('link', { name: 'old' })).toBeTruthy()
  })

  it('asks to type the stack name before running Down', async () => {
    const { api, runStack } = fakeApi()
    renderScreen(client([server()]), api)
    await screen.findByText('shop')

    fireEvent.click(screen.getByRole('button', { name: 'Actions for shop' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Down' }))
    expect(await screen.findByText('Down shop?')).toBeTruthy()
    const confirm = screen.getByRole('button', { name: 'Down the stack' }) as HTMLButtonElement
    expect(confirm.disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Type shop to confirm'), {
      target: { value: 'shop' },
    })
    fireEvent.click(confirm)

    await waitFor(() => expect(runStack).toHaveBeenCalledWith('srv-1', 'shop', 'down', true))
  })

  it('confirms bulk Restart and reports the summary', async () => {
    const { api, runStack, job } = fakeApi({
      lists: {
        'srv-1': {
          connected: true,
          dockerAvailable: true,
          containers: [container()],
        },
        'srv-2': {
          connected: true,
          dockerAvailable: true,
          containers: [container({ id: 'c2', name: 'blog-web' })],
        },
      },
      stacks: {
        'srv-1': {
          connected: true,
          stacks: ['shop'],
          adopted: [],
          broken: [],
          adoptable: [],
        },
        'srv-2': {
          connected: true,
          stacks: ['blog'],
          adopted: [],
          broken: [],
          adoptable: [],
        },
      },
    })
    renderScreen(client([server(), server({ id: 'srv-2', name: 'vps-db-01' })]), api)
    await screen.findByText('blog')

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }))
    expect(await screen.findByText('Restart 2 stacks?')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Type 2 to confirm'), {
      target: { value: '2' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))

    await waitFor(() => expect(runStack).toHaveBeenCalledTimes(2))
    expect(runStack).toHaveBeenCalledWith('srv-1', 'shop', 'restart')
    expect(runStack).toHaveBeenCalledWith('srv-2', 'blog', 'restart')
    expect(job).toHaveBeenCalledTimes(2)
    expect(await screen.findByText('2 succeeded, 0 failed.')).toBeTruthy()
  })

  it('does not offer stack actions without stack.deploy', async () => {
    const { api } = fakeApi()
    renderScreen(client([server()]), api, meApi({ organization: ['stack.view'] }))
    await screen.findByText('shop')

    expect(screen.queryByRole('checkbox')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Actions for shop' })).toBeNull()
  })

  it('disables a bulk action when a selected stack is not permitted', async () => {
    const { api, runStack } = fakeApi({
      lists: {
        'srv-1': {
          connected: true,
          dockerAvailable: true,
          containers: [container()],
        },
        'srv-2': {
          connected: true,
          dockerAvailable: true,
          containers: [container({ id: 'c2', name: 'blog-web' })],
        },
      },
      stacks: {
        'srv-1': {
          connected: true,
          stacks: ['shop'],
          adopted: [],
          broken: [],
          adoptable: [],
        },
        'srv-2': {
          connected: true,
          stacks: ['blog'],
          adopted: [],
          broken: [],
          adoptable: [],
        },
      },
    })
    renderScreen(
      client([server(), server({ id: 'srv-2', name: 'vps-db-01' })]),
      api,
      meApi({
        organization: ['stack.view'],
        stacks: { 'srv-1': { shop: ['stack.deploy'] } },
      }),
    )
    await screen.findByText('blog')

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
    const restart = screen.getByRole('button', { name: 'Restart' }) as HTMLButtonElement
    expect(restart.disabled).toBe(true)
    expect(restart.getAttribute('title')).toBe('1 selected stack cannot run this action')
    expect(runStack).not.toHaveBeenCalled()
  })
})
