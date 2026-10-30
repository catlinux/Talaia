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
import { Containers } from './Containers'

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
    status: 'Up 2 hours',
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
  const run = vi.fn(() => Promise.resolve('job'))
  const job = vi.fn(() => Promise.resolve(succeeded()))
  const unexpected = () => Promise.reject(new Error('unexpected call'))
  const api = {
    list,
    run,
    stacks,
    writeStack: unexpected,
    readStack: unexpected,
    listResources: unexpected,
    removeResources: unexpected,
    adoptStack: unexpected,
    runStack: unexpected,
    logs: unexpected,
    job,
  } satisfies ContainersApi
  return { api, list, stacks, run, job }
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
  organization: [
    'container.view',
    'container.control',
    'container.logs',
    'container.terminal',
    'container.remove',
    'stack.view',
    'stack.deploy',
  ],
  servers: {},
  stacks: {},
  containers: {},
}

function renderScreen(fleet: FleetClient, api: ContainersApi, me: MeApi = meApi(allPermissions)) {
  return render(
    <ToastProvider>
      <RouterProvider>
        <PermissionsProvider meApi={me}>
          <Containers client={fleet} containersApi={api} pollMs={1} />
        </PermissionsProvider>
      </RouterProvider>
    </ToastProvider>,
  )
}

beforeEach(async () => {
  localStorage.clear()
  window.history.replaceState({}, '', '/containers')
  await i18n.changeLanguage('en')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  window.history.replaceState({}, '', '/')
})

describe('Containers', () => {
  it('lists every container with links to its stack and server', async () => {
    const { api } = fakeApi({
      lists: {
        'srv-1': {
          connected: true,
          dockerAvailable: true,
          containers: [
            container(),
            container({
              id: 'c2',
              name: 'worker',
              state: 'exited',
              status: 'Exited (0)',
              composeService: 'worker',
            }),
            container({
              id: 'c3',
              name: 'scratch',
              composeProject: '',
              composeService: '',
            }),
          ],
        },
        'srv-2': {
          connected: true,
          dockerAvailable: true,
          containers: [container({ id: 'c4', name: 'blog', composeProject: 'blog' })],
        },
      },
    })
    renderScreen(client([server(), server({ id: 'srv-2', name: 'vps-db-01' })]), api)

    expect(await screen.findByRole('heading', { level: 1, name: 'Containers' })).toBeTruthy()
    expect(await screen.findByText(/4 containers/)).toBeTruthy()
    const table = screen.getByRole('table')
    expect(within(table).getByText('web')).toBeTruthy()
    expect(within(table).getByText('worker')).toBeTruthy()
    expect(within(table).getAllByText('blog').length).toBeGreaterThan(0)
    expect(within(table).getAllByRole('link', { name: 'shop' })[0]?.getAttribute('href')).toBe(
      '/servers/srv-1/stacks/shop',
    )
    expect(
      within(table).getAllByRole('link', { name: 'vps-web-01' })[0]?.getAttribute('href'),
    ).toBe('/servers/srv-1/containers')
    expect(within(table).getAllByText('service web').length).toBeGreaterThan(0)
  })

  it('filters by URL query and clears the filters', async () => {
    const { api } = fakeApi({
      lists: {
        'srv-1': {
          connected: true,
          dockerAvailable: true,
          containers: [container(), container({ id: 'c2', name: 'db', composeProject: 'blog' })],
        },
      },
    })
    renderScreen(client([server()]), api)
    await screen.findByText('db')

    const search = screen.getByRole('searchbox')
    fireEvent.change(search, { target: { value: 'db' } })
    await waitFor(() => expect(window.location.search).toContain('q=db'))
    expect(screen.queryByText('web')).toBeNull()
    expect(screen.getByText('db')).toBeTruthy()

    fireEvent.change(search, { target: { value: 'nothing' } })
    expect(await screen.findByText('No containers match these filters')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
    expect(await screen.findByText('web')).toBeTruthy()
    expect(window.location.search).toBe('')
  })

  it('groups the table by stack or server', async () => {
    const { api } = fakeApi({
      lists: {
        'srv-1': {
          connected: true,
          dockerAvailable: true,
          containers: [container(), container({ id: 'c2', name: 'db', composeProject: 'blog' })],
        },
      },
    })
    renderScreen(client([server()]), api)
    await screen.findByText('db')

    fireEvent.change(screen.getByLabelText('Group by'), { target: { value: 'stack' } })
    expect(await screen.findByText('shop (1)')).toBeTruthy()
    expect(screen.getByText('blog (1)')).toBeTruthy()

    fireEvent.change(screen.getByLabelText('Group by'), { target: { value: 'server' } })
    expect(await screen.findByText('vps-web-01 (2)')).toBeTruthy()
  })

  it('does not offer control actions to a viewer without container.control', async () => {
    const { api } = fakeApi({
      lists: {
        'srv-1': {
          connected: true,
          dockerAvailable: true,
          containers: [container()],
        },
      },
    })
    renderScreen(
      client([server()]),
      api,
      meApi({
        organization: ['container.view'],
        containers: { 'srv-1': { web: ['container.logs'] } },
        stacks: { 'srv-1': { shop: ['container.view'] } },
      }),
    )
    await screen.findByText('web')

    expect(screen.queryByRole('checkbox')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Actions for web' }))
    expect(screen.getByRole('menuitem', { name: 'Logs' })).toBeTruthy()
    expect(screen.queryByRole('menuitem', { name: 'Stop' })).toBeNull()
    expect(screen.queryByRole('menuitem', { name: 'Restart' })).toBeNull()
    expect(screen.queryByRole('menuitem', { name: 'Remove' })).toBeNull()
  })

  it('runs a row action and reports the job result', async () => {
    const { api, run } = fakeApi()
    renderScreen(client([server()]), api)
    await screen.findByText('web')

    fireEvent.click(screen.getByRole('button', { name: 'Actions for web' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Stop' }))

    await waitFor(() => expect(run).toHaveBeenCalledWith('srv-1', 'web', 'stop', false))
    expect(await screen.findByText('Stop on web finished.')).toBeTruthy()
  })

  it('asks to type the name before removing a container', async () => {
    const { api, run } = fakeApi()
    renderScreen(client([server()]), api)
    await screen.findByText('web')

    fireEvent.click(screen.getByRole('button', { name: 'Actions for web' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Remove' }))
    const confirm = screen.getByRole('button', {
      name: 'Remove the container',
    }) as HTMLButtonElement
    expect(confirm.disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Type web to confirm'), {
      target: { value: 'web' },
    })
    fireEvent.click(confirm)

    await waitFor(() => expect(run).toHaveBeenCalledWith('srv-1', 'web', 'remove', true))
  })

  it('confirms and runs bulk Stop sequentially with one summary toast', async () => {
    const { api, run, job } = fakeApi({
      lists: {
        'srv-1': {
          connected: true,
          dockerAvailable: true,
          containers: [container()],
        },
        'srv-2': {
          connected: true,
          dockerAvailable: true,
          containers: [container({ id: 'c2', name: 'blog' })],
        },
      },
    })
    renderScreen(client([server(), server({ id: 'srv-2', name: 'vps-db-01' })]), api)
    await screen.findByText('blog')

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    expect(await screen.findByText('Stop 2 containers?')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Type 2 to confirm'), {
      target: { value: '2' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))

    await waitFor(() => expect(run).toHaveBeenCalledTimes(2))
    expect(job).toHaveBeenCalledTimes(2)
    expect(await screen.findByText('2 succeeded, 0 failed.')).toBeTruthy()
  })

  it('disables a bulk action when part of the selection is not permitted', async () => {
    const { api, run } = fakeApi({
      lists: {
        'srv-1': {
          connected: true,
          dockerAvailable: true,
          containers: [container()],
        },
        'srv-2': {
          connected: true,
          dockerAvailable: true,
          containers: [container({ id: 'c2', name: 'blog' })],
        },
      },
    })
    renderScreen(
      client([server(), server({ id: 'srv-2', name: 'vps-db-01' })]),
      api,
      meApi({
        organization: ['container.view', 'stack.view'],
        containers: { 'srv-1': { web: ['container.control'] } },
      }),
    )
    await screen.findByText('blog')

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
    const start = screen.getByRole('button', { name: 'Start' }) as HTMLButtonElement
    expect(start.disabled).toBe(true)
    expect(start.getAttribute('title')).toBe('1 selected item cannot run this action')
    expect(run).not.toHaveBeenCalled()
  })
})
