import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AptApi } from '../api/apt'
import type { Container, ContainerList, ContainersApi, Job } from '../api/containers'
import { ApiError } from '../api/http'
import type { ServiceUnit, ServicesApi } from '../api/services'
import { AgentUpdateError, type ServerSummary, type ServersApi } from '../api/servers'
import i18n from '../i18n'
import { ServerScreen } from './ServerScreen'

const server = { id: 'srv-1', name: 'vps-web-01' }

// The unrelated tests do not wait for the apt section; leaving its request
// pending keeps them focused on the behavior they check.
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

/** serverSummary builds the server list entry the screen loads. */
function serverSummary(overrides: Partial<ServerSummary> = {}): ServerSummary {
  return {
    id: 'srv-1',
    name: 'vps-web-01',
    connected: true,
    revoked: false,
    agentVersion: '0.2.0',
    agentLatestVersion: '0.2.8',
    agentUpdateAvailable: false,
    agentUpdateManual: false,
    agentUpgradeCommand: '',
    ...overrides,
  }
}

/** serversApiWith builds a fake server API; list is served once per call. */
function serversApiWith(
  list: ServerSummary[] | (() => Promise<ServerSummary[]>),
  updateAgent: ServersApi['updateAgent'] = vi.fn(() => Promise.resolve()),
): ServersApi {
  return {
    listServers: vi.fn(typeof list === 'function' ? list : () => Promise.resolve(list)),
    updateAgent,
    updateAllAgents: vi.fn(() => Promise.resolve({ queued: [], skipped: [] })),
    removeServer: vi.fn(() => Promise.resolve()),
  }
}

function container(overrides: Partial<Container> = {}): Container {
  return {
    id: 'aaaaaaaaaaaa',
    name: 'web',
    image: 'nginx:1.27',
    state: 'running',
    status: 'Up 2 hours',
    composeProject: 'shop',
    composeService: 'web',
    ...overrides,
  }
}

const listOf = (...containers: Container[]): ContainerList => ({
  connected: true,
  dockerAvailable: true,
  containers,
})

/** apiWith builds a fake API; the job answers each poll in order. */
function apiWith(
  list: ContainerList | (() => Promise<ContainerList>),
  jobs: Job[] = [{ id: 'job-1', state: 'succeeded', message: '', output: '' }],
): ContainersApi & {
  run: ReturnType<typeof vi.fn>
  list: ReturnType<typeof vi.fn>
  logs: ReturnType<typeof vi.fn>
  stacks: ReturnType<typeof vi.fn>
  job: ReturnType<typeof vi.fn>
} {
  const queue = [...jobs]
  return {
    list: vi.fn(typeof list === 'function' ? list : () => Promise.resolve(list)),
    run: vi.fn(() => Promise.resolve('job-1')),
    logs: vi.fn(() => Promise.resolve('job-1')),
    stacks: vi.fn(() =>
      Promise.resolve({ connected: true, stacks: [], adopted: [], broken: [], adoptable: [] }),
    ),
    adoptStack: vi.fn(() => Promise.resolve('job-1')),
    listResources: vi.fn(() => Promise.resolve('job-1')),
    removeResources: vi.fn(() => Promise.resolve('job-1')),
    writeStack: vi.fn(() => Promise.resolve('job-1')),
    readStack: vi.fn(() => Promise.resolve('job-1')),
    runStack: vi.fn(() => Promise.resolve('job-1')),
    job: vi.fn(() => Promise.resolve(queue.length > 1 ? (queue.shift() as Job) : queue[0]!)),
  }
}

function renderScreen(
  api: ContainersApi,
  onBack = () => undefined,
  serversApi: ServersApi = serversApiWith([serverSummary()]),
  aptApi: AptApi = pendingAptApi,
  servicesApi: ServicesApi = pendingServicesApi,
) {
  render(
    <ServerScreen
      server={server}
      api={api}
      aptApi={aptApi}
      servicesApi={servicesApi}
      serversApi={serversApi}
      onBack={onBack}
      pollMs={1}
    />,
  )
}

describe('ServerScreen', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('lists the containers of the server, stacks together and running first', async () => {
    renderScreen(
      apiWith(
        listOf(
          container({ id: 'bbbbbbbbbbbb', name: 'db', state: 'exited', composeProject: 'shop' }),
          container({ id: 'cccccccccccc', name: 'solo', composeProject: '' }),
          container(),
        ),
      ),
    )

    expect(await screen.findByRole('heading', { level: 1, name: 'vps-web-01' })).toBeTruthy()
    expect(screen.getByText('3 containers · 2 running')).toBeTruthy()

    const names = within(screen.getByRole('list', { name: 'Containers' }))
      .getAllByRole('heading', { level: 3 })
      .map((heading) => heading.textContent)
    // The stack first (running before stopped), the standalone container last.
    expect(names).toEqual(['web', 'db', 'solo'])
    expect(screen.getAllByText('stack shop')).toHaveLength(2)
  })

  it('renders the updates section of a connected server', async () => {
    const aptApi: AptApi = {
      status: vi.fn(() => Promise.resolve('apt-job')),
      upgrade: vi.fn(() => Promise.resolve('apt-job')),
      reboot: vi.fn(() => Promise.resolve('apt-job')),
      job: vi.fn(() =>
        Promise.resolve({
          id: 'apt-job',
          state: 'succeeded' as const,
          message: '',
          output: JSON.stringify({
            packages: [],
            held: [],
            reboot_required: false,
            reboot_reasons: [],
            lists_updated_at: '',
            checked_at: '2026-10-22T09:00:00Z',
          }),
        }),
      ),
    }
    renderScreen(apiWith(listOf(container())), undefined, serversApiWith([serverSummary()]), aptApi)

    expect(await screen.findByRole('heading', { level: 2, name: 'Updates' })).toBeTruthy()
    expect(await screen.findByText('Everything is up to date.')).toBeTruthy()
    expect(aptApi.status).toHaveBeenCalledWith('srv-1', false)
  })

  it('renders the services section of a connected server', async () => {
    const servicesApi: ServicesApi = {
      list: vi.fn(() =>
        Promise.resolve([
          {
            name: 'docker.service',
            description: 'Docker',
            loadState: 'loaded',
            activeState: 'active',
            subState: 'running',
            unitFileState: 'enabled',
          } satisfies ServiceUnit,
        ]),
      ),
      action: () => Promise.reject(new Error('unexpected service action')),
      job: () => Promise.reject(new Error('unexpected service job')),
    }
    renderScreen(
      apiWith(listOf(container())),
      undefined,
      serversApiWith([serverSummary()]),
      pendingAptApi,
      servicesApi,
    )

    expect(await screen.findByRole('heading', { level: 2, name: 'Services' })).toBeTruthy()
    expect(await screen.findByText('1 service')).toBeTruthy()
    expect(screen.getByRole('heading', { level: 3, name: 'docker.service' })).toBeTruthy()
  })

  it('explains an offline agent, a missing Docker and an empty server', async () => {
    renderScreen(apiWith({ connected: false, dockerAvailable: false, containers: [] }))
    expect(await screen.findByText(/agent of this server is not connected/)).toBeTruthy()
    cleanup()

    renderScreen(apiWith({ connected: true, dockerAvailable: false, containers: [] }))
    expect(await screen.findByText('Docker is not available on this server.')).toBeTruthy()
    cleanup()

    renderScreen(apiWith(listOf()))
    expect(await screen.findByText('This server has no containers yet.')).toBeTruthy()
  })

  it('offers the actions that make sense for the state of each container', async () => {
    renderScreen(
      apiWith(listOf(container(), container({ id: 'bbbbbbbbbbbb', name: 'db', state: 'exited' }))),
    )
    await screen.findByText('web')

    const rows = screen.getAllByRole('listitem')
    expect(
      within(rows[0]!)
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual(['Logs', 'Terminal', 'Restart', 'Stop', 'Remove'])
    expect(
      within(rows[1]!)
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual(['Logs', 'Start', 'Remove'])
  })

  it('runs an action as a job, follows it and reloads the list', async () => {
    const api = apiWith(listOf(container()), [
      { id: 'job-1', state: 'running', message: '', output: '' },
      { id: 'job-1', state: 'succeeded', message: '', output: '' },
    ])
    renderScreen(api)

    fireEvent.click(await screen.findByRole('button', { name: 'Restart' }))

    expect(api.run).toHaveBeenCalledWith('srv-1', 'web', 'restart', false)
    expect(await screen.findByText('Done')).toBeTruthy()
    await waitFor(() => expect(api.list).toHaveBeenCalledTimes(2))
  })

  it('starts and restarts without confirmation', async () => {
    const api = apiWith(
      listOf(container(), container({ id: 'bbbbbbbbbbbb', name: 'db', state: 'exited' })),
    )
    renderScreen(api)

    fireEvent.click(await screen.findByRole('button', { name: 'Restart' }))
    expect(api.run).toHaveBeenCalledWith('srv-1', 'web', 'restart', false)

    fireEvent.click(await screen.findByRole('button', { name: 'Start' }))
    expect(api.run).toHaveBeenCalledWith('srv-1', 'db', 'start', false)
    expect(screen.queryByText(/^Stop /)).toBeNull()
  })

  it('asks before stopping and only then sends the stop', async () => {
    const api = apiWith(listOf(container()))
    renderScreen(api)

    fireEvent.click(await screen.findByRole('button', { name: 'Stop' }))
    expect(api.run).not.toHaveBeenCalled()
    expect(screen.getByText('Stop web?')).toBeTruthy()
    expect(screen.getByText(/services that depend on it will stop answering/)).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(api.run).not.toHaveBeenCalled()
    expect(screen.queryByText('Stop web?')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    fireEvent.click(screen.getByRole('button', { name: 'Stop the container' }))

    expect(api.run).toHaveBeenCalledWith('srv-1', 'web', 'stop', false)
    expect(await screen.findByText('Done')).toBeTruthy()
  })

  it('shows why an action failed', async () => {
    const api = apiWith(listOf(container()), [
      { id: 'job-1', state: 'failed', message: 'docker: cannot stop the container', output: '' },
    ])
    renderScreen(api)

    fireEvent.click(await screen.findByRole('button', { name: 'Restart' }))

    expect((await screen.findByRole('alert')).textContent).toBe(
      'Failed: docker: cannot stop the container',
    )
  })

  it('shows a core error when the action cannot be sent', async () => {
    const api = apiWith(listOf(container()))
    api.run.mockRejectedValueOnce(new ApiError('agent_offline', 409))
    renderScreen(api)

    fireEvent.click(await screen.findByRole('button', { name: 'Restart' }))

    expect((await screen.findByRole('alert')).textContent).toContain(
      'The agent of this server is not connected.',
    )
  })

  it('asks before removing and only then sends the confirmation', async () => {
    const api = apiWith(listOf(container({ state: 'exited' })))
    renderScreen(api)

    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }))
    expect(api.run).not.toHaveBeenCalled()
    expect(screen.getByText('Remove web?')).toBeTruthy()
    expect(screen.getByText('The container is deleted. Its volumes are kept.')).toBeTruthy()

    // Cancelling sends nothing.
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(api.run).not.toHaveBeenCalled()
    expect(screen.queryByText('Remove web?')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Remove' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove the container' }))

    expect(api.run).toHaveBeenCalledWith('srv-1', 'web', 'remove', true)
    expect(await screen.findByText('Done')).toBeTruthy()
  })

  it('shows the agent version and updates it after confirmation', async () => {
    const serversApi = serversApiWith([serverSummary({ agentUpdateAvailable: true })])
    renderScreen(apiWith(listOf(container())), undefined, serversApi)

    expect(await screen.findByText('Agent 0.2.0')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Update agent' }))

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Update the agent?')).toBeTruthy()
    expect(within(dialog).getByText(/downloads the package from the core/)).toBeTruthy()

    fireEvent.click(within(dialog).getByRole('button', { name: 'Update agent' }))

    expect(serversApi.updateAgent).toHaveBeenCalledWith('srv-1')
    expect(await screen.findByText('Updating… the agent will reconnect.')).toBeTruthy()
  })

  it('hides the update button when the agent is offline or up to date', async () => {
    renderScreen(
      apiWith(listOf(container())),
      undefined,
      serversApiWith([serverSummary({ connected: false, agentUpdateAvailable: true })]),
    )

    expect(await screen.findByText('Agent 0.2.0')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Update agent' })).toBeNull()
  })

  it('shows the manual upgrade explanation and copies the command', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const serversApi = serversApiWith([
      serverSummary({
        agentUpdateManual: true,
        agentUpgradeCommand: 'sudo bash install.sh --upgrade',
      }),
    ])
    renderScreen(apiWith(listOf(container())), undefined, serversApi)

    expect(await screen.findByText(/This agent is too old to update itself/)).toBeTruthy()
    expect(screen.getByText('sudo bash install.sh --upgrade')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Update agent' })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Copy the command' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('sudo bash install.sh --upgrade'))
    expect(await screen.findByText('Copied')).toBeTruthy()
  })

  it('points to the docs when the manual upgrade command is empty', async () => {
    const serversApi = serversApiWith([serverSummary({ agentUpdateManual: true })])
    renderScreen(apiWith(listOf(container())), undefined, serversApi)

    expect(await screen.findByText(/This agent is too old to update itself/)).toBeTruthy()
    expect(screen.getByText('See the installation guide for the manual steps.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Copy the command' })).toBeNull()
  })

  const refusedUpdates = [
    ['no_update', 'There is no newer agent package.'],
    ['agent_offline', 'The agent of this server is not connected.'],
    ['update_in_progress', 'An agent update is already in progress.'],
    ['session_required', 'The session ended. Sign in again.'],
  ] as const

  for (const [code, message] of refusedUpdates) {
    it(`shows a clear message when the update is refused with ${code}`, async () => {
      const updateAgent = vi.fn(() => Promise.reject(new AgentUpdateError(code, 409)))
      const serversApi = serversApiWith(
        [serverSummary({ agentUpdateAvailable: true })],
        updateAgent,
      )
      renderScreen(apiWith(listOf(container())), undefined, serversApi)

      fireEvent.click(await screen.findByRole('button', { name: 'Update agent' }))
      fireEvent.click(
        within(screen.getByRole('dialog')).getByRole('button', { name: 'Update agent' }),
      )

      expect((await screen.findByRole('alert')).textContent).toBe(message)
    })
  }

  it('clears the updating line when a refresh reports the new version', async () => {
    const listServers = vi
      .fn<() => Promise<ServerSummary[]>>()
      .mockResolvedValueOnce([serverSummary({ agentUpdateAvailable: true })])
      .mockResolvedValue([serverSummary({ agentVersion: '0.3.0', agentUpdateAvailable: false })])
    const serversApi = serversApiWith(listServers)
    renderScreen(apiWith(listOf(container())), undefined, serversApi)

    fireEvent.click(await screen.findByRole('button', { name: 'Update agent' }))
    fireEvent.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'Update agent' }),
    )

    expect(await screen.findByText('Agent 0.3.0')).toBeTruthy()
    expect(screen.queryByText('Updating… the agent will reconnect.')).toBeNull()
  })

  it('goes back to the fleet', async () => {
    const onBack = vi.fn()
    renderScreen(apiWith(listOf(container())), onBack)

    fireEvent.click(await screen.findByRole('button', { name: 'Back to the fleet' }))

    expect(onBack).toHaveBeenCalledTimes(1)
  })

  it('opens the screen of a managed stack and goes back to the server', async () => {
    const api = apiWith(listOf(container()))
    api.stacks.mockResolvedValue({
      connected: true,
      stacks: ['shop'],
      adopted: [],
      broken: [],
      adoptable: [],
    })
    api.job.mockResolvedValue({
      id: 'job-1',
      state: 'succeeded',
      message: '',
      output: JSON.stringify({ compose: 'services: {}\n', env: '' }),
    })
    renderScreen(api)

    fireEvent.click(await screen.findByRole('button', { name: 'shop' }))

    expect(await screen.findByRole('heading', { level: 1, name: /shop/ })).toBeTruthy()
    expect(screen.getByText('Compose')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Back to the server' }))
    expect(await screen.findByRole('heading', { level: 1, name: 'vps-web-01' })).toBeTruthy()
  })

  it('shows an error when the first load fails', async () => {
    renderScreen(apiWith(() => Promise.reject(new ApiError('internal', 500))))

    expect(await screen.findByRole('alert')).toBeTruthy()
  })

  it('shows the logs of a container and refreshes them', async () => {
    const api = apiWith(listOf(container()), [
      {
        id: 'job-1',
        state: 'succeeded',
        message: '',
        output: '2026-10-20T10:00:00Z hello world\n',
      },
    ])
    renderScreen(api)

    fireEvent.click(await screen.findByRole('button', { name: 'Logs' }))

    expect(await screen.findByText(/hello world/)).toBeTruthy()
    expect(api.logs).toHaveBeenCalledWith('srv-1', 'web')
    expect(screen.getByRole('region', { name: 'Logs of web' })).toBeTruthy()

    fireEvent.click(
      within(screen.getByRole('region', { name: 'Logs of web' })).getByRole('button', {
        name: 'Refresh',
      }),
    )
    await waitFor(() => expect(api.logs).toHaveBeenCalledTimes(2))

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('region', { name: 'Logs of web' })).toBeNull()
  })

  it('says when there are no log lines and why a read failed', async () => {
    const empty = apiWith(listOf(container()), [
      { id: 'job-1', state: 'succeeded', message: '', output: '' },
    ])
    renderScreen(empty)
    fireEvent.click(await screen.findByRole('button', { name: 'Logs' }))
    expect(await screen.findByText('There are no log lines.')).toBeTruthy()
    cleanup()

    const failing = apiWith(listOf(container()), [
      { id: 'job-1', state: 'failed', message: 'docker: No such container', output: '' },
    ])
    renderScreen(failing)
    fireEvent.click(await screen.findByRole('button', { name: 'Logs' }))
    expect((await screen.findByRole('alert')).textContent).toBe('Failed: docker: No such container')
  })

  it('shows the CPU and the memory of the running containers that have a sample', async () => {
    const withUse = container({
      use: {
        cpuPercent: 37.5,
        memUsedBytes: 512 * 1024 ** 2,
        memLimitBytes: 1024 ** 3,
        netRxBytes: 1,
        netTxBytes: 2,
      },
    })
    const stopped = container({
      id: 'bbbbbbbbbbbb',
      name: 'db',
      state: 'exited',
      use: { cpuPercent: 99, memUsedBytes: 1, memLimitBytes: 0, netRxBytes: 0, netTxBytes: 0 },
    })
    renderScreen(apiWith(listOf(withUse, stopped)))

    const row = (await screen.findByRole('heading', { level: 3, name: 'web' })).closest('li')!
    expect(within(row).getByRole('meter', { name: /CPU: / })).toBeTruthy()
    expect(within(row).getByRole('meter', { name: /RAM: / })).toBeTruthy()
    // The memory shows what is used out of the limit.
    expect(
      within(row).getByRole('meter', { name: /RAM: / }).getAttribute('aria-valuetext'),
    ).toContain(' / ')

    // A stopped container never shows gauges, even with a leftover sample.
    const stoppedRow = screen.getByRole('heading', { level: 3, name: 'db' }).closest('li')!
    expect(within(stoppedRow).queryByRole('meter')).toBeNull()
  })
})
