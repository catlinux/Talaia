import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Container, ContainersApi, Job } from '../../api/containers'
import type { MetricsApi } from '../../api/metrics'
import i18n from '../../i18n'
import { StackScreen } from './StackScreen'

const server = { id: 'srv-1', name: 'vps-web-01' }

function container(overrides: Partial<Container> = {}): Container {
  return {
    id: 'aaaaaaaaaaaa',
    name: 'shop-web-1',
    image: 'nginx:1.27',
    state: 'running',
    status: 'Up 2 hours',
    composeProject: 'shop',
    composeService: 'web',
    ...overrides,
  }
}

/** apiWith answers every job by its id, so reads and actions do not mix. */
function apiWith(action: Partial<Job> = {}): ContainersApi & {
  runStack: ReturnType<typeof vi.fn>
  readStack: ReturnType<typeof vi.fn>
} {
  const jobs = new Map<string, Job>()
  let actions = 0
  const answer = (id: string, extra: Partial<Job> = {}): Promise<string> => {
    jobs.set(id, { id, state: 'succeeded', message: '', output: '', ...extra })
    return Promise.resolve(id)
  }

  return {
    list: vi.fn(() => Promise.resolve({ connected: true, dockerAvailable: true, containers: [] })),
    run: vi.fn(() => Promise.resolve('job')),
    logs: vi.fn(() => answer('logs', { output: '2026-10-20T10:00:00Z hello\n' })),
    stacks: vi.fn(() =>
      Promise.resolve({ connected: true, stacks: [], adopted: [], broken: [], adoptable: [] }),
    ),
    adoptStack: vi.fn(() => Promise.resolve('job')),
    listResources: vi.fn(() => Promise.resolve('job')),
    removeResources: vi.fn(() => Promise.resolve('job')),
    writeStack: vi.fn(() => answer('write')),
    readStack: vi.fn(() =>
      answer('read', { output: JSON.stringify({ compose: 'services: {}\n', env: '' }) }),
    ),
    runStack: vi.fn(() => {
      actions += 1
      return answer(`run-${actions}`, action)
    }),
    job: vi.fn((id: string): Promise<Job> =>
      Promise.resolve(jobs.get(id) ?? { id, state: 'succeeded', message: '', output: '' }),
    ),
  }
}

interface RenderOptions {
  containers?: Container[]
  adopted?: boolean
  broken?: boolean
  connected?: boolean
  dockerAvailable?: boolean
  onBack?: () => void
  onChanged?: () => void
  metricsApi?: MetricsApi
}

function renderScreen(api: ContainersApi, options: RenderOptions = {}) {
  render(
    <StackScreen
      api={api}
      metricsApi={options.metricsApi}
      server={server}
      name="shop"
      adopted={options.adopted ?? false}
      broken={options.broken ?? false}
      containers={options.containers ?? [container()]}
      connected={options.connected ?? true}
      dockerAvailable={options.dockerAvailable ?? true}
      pollMs={1}
      onBack={options.onBack ?? (() => undefined)}
      onChanged={options.onChanged ?? (() => undefined)}
    />,
  )
}

describe('StackScreen', () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18n.changeLanguage('en')
  })

  it('shows CPU and memory sparklines for each container when metrics are provided', async () => {
    const metricsApi = {
      server: vi.fn(),
      container: vi.fn().mockResolvedValue({
        resolution: 'raw',
        from: '',
        to: '',
        points: [
          { time: '2026-10-24T10:00:00Z', cpu: 10, mem_used: 1_000_000 },
          { time: '2026-10-24T10:01:00Z', cpu: 20, mem_used: 2_000_000 },
        ],
      }),
    } as unknown as MetricsApi
    renderScreen(apiWith(), { metricsApi })
    await waitFor(() =>
      expect(document.querySelectorAll('.stack-metrics .sparkline')).toHaveLength(2),
    )
    expect(metricsApi.container).toHaveBeenCalledWith('srv-1', 'shop-web-1', '1h')
  })

  afterEach(() => {
    cleanup()
  })

  it('shows the breadcrumb, the state of the stack and the adopted tag', async () => {
    renderScreen(apiWith(), {
      adopted: true,
      containers: [
        container({ name: 'shop-web-1' }),
        container({ id: 'bbbbbbbbbbbb', name: 'shop-db-1', composeService: 'db' }),
        container({
          id: 'cccccccccccc',
          name: 'shop-redis-1',
          composeService: 'redis',
          state: 'exited',
        }),
      ],
    })

    expect(await screen.findByRole('heading', { level: 1, name: /shop/ })).toBeTruthy()
    expect(screen.getByText('2 of 3 running')).toBeTruthy()
    expect(screen.getByText('adopted')).toBeTruthy()
    expect(screen.getByRole('navigation', { name: 'Breadcrumb' }).textContent).toContain(
      'vps-web-01/Stacks/shop',
    )
  })

  it('flags a broken adopted stack and only offers to stop managing it', async () => {
    const api = apiWith()
    const onBack = vi.fn()
    renderScreen(api, { adopted: true, broken: true, onBack })
    await screen.findByRole('heading', { level: 1, name: /shop/ })

    expect(screen.getByText('Files missing')).toBeTruthy()
    expect(screen.getByText(/files of this project are gone/)).toBeTruthy()
    for (const name of ['Restart', 'Stop', 'Deploy', 'Pull']) {
      expect(screen.queryByRole('button', { name })).toBeNull()
    }
    expect(screen.queryByLabelText('compose.yaml')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Stop managing' }))
    const confirmation = screen.getByRole('group', { name: 'Stop managing shop?' })
    fireEvent.click(within(confirmation).getByRole('button', { name: 'Stop managing' }))

    expect(api.runStack).toHaveBeenCalledWith('srv-1', 'shop', 'forget', false)
    await waitFor(() => expect(onBack).toHaveBeenCalled())
  })

  it('runs restart, deploy and pull as jobs', async () => {
    const api = apiWith()
    const onChanged = vi.fn()
    renderScreen(api, { onChanged })
    await screen.findByText('Compose')

    fireEvent.click(screen.getByRole('button', { name: 'Restart' }))
    expect(api.runStack).toHaveBeenCalledWith('srv-1', 'shop', 'restart', false)
    expect(await screen.findByText('Done')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Deploy' }))
    expect(api.runStack).toHaveBeenCalledWith('srv-1', 'shop', 'up', false)
    await waitFor(() =>
      expect((screen.getByRole('button', { name: 'Pull' }) as HTMLButtonElement).disabled).toBe(
        false,
      ),
    )

    fireEvent.click(screen.getByRole('button', { name: 'Pull' }))
    expect(api.runStack).toHaveBeenCalledWith('srv-1', 'shop', 'pull', false)
    expect(onChanged).toHaveBeenCalled()
  })

  it('asks before stopping the stack', async () => {
    const api = apiWith()
    renderScreen(api)
    await screen.findByText('Compose')

    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    expect(api.runStack).not.toHaveBeenCalled()
    expect(screen.getByText('Stop shop?')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(api.runStack).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    fireEvent.click(screen.getByRole('button', { name: 'Stop the stack' }))
    expect(api.runStack).toHaveBeenCalledWith('srv-1', 'shop', 'down', true)
    expect(await screen.findByText('Done')).toBeTruthy()
  })

  it('shows why an action failed', async () => {
    renderScreen(apiWith({ state: 'failed', message: 'docker: cannot start the stack' }))
    await screen.findByText('Compose')

    fireEvent.click(screen.getByRole('button', { name: 'Deploy' }))

    expect((await screen.findByRole('alert')).textContent).toBe(
      'Failed: docker: cannot start the stack',
    )
  })

  it('shows the containers with their state, CPU and memory', async () => {
    renderScreen(apiWith(), {
      containers: [
        container({
          use: {
            cpuPercent: 25,
            memUsedBytes: 200_000_000,
            memLimitBytes: 0,
            netRxBytes: 0,
            netTxBytes: 0,
          },
        }),
        container({ id: 'bbbbbbbbbbbb', name: 'shop-db-1', state: 'exited' }),
      ],
    })

    await screen.findByText('Compose')
    const table = screen.getByRole('table')
    expect(within(table).getByText('shop-web-1')).toBeTruthy()
    expect(within(table).getByText('25%')).toBeTruthy()
    expect(within(table).getByText('200 MB')).toBeTruthy()
    expect(within(table).getByText('Stopped')).toBeTruthy()
  })

  it('keeps the compose-as-root note visible in the editor', async () => {
    renderScreen(apiWith())

    expect(
      await screen.findByText(
        'A Compose file is run as root on the server. Anyone who can edit it can control the server.',
      ),
    ).toBeTruthy()
    expect(screen.getByRole('note')).toBeTruthy()
  })

  it('shows the logs of the first container and lets the reader choose another', async () => {
    renderScreen(apiWith(), {
      containers: [
        container({ name: 'shop-web-1' }),
        container({ id: 'bbbbbbbbbbbb', name: 'shop-db-1', composeService: 'db' }),
      ],
    })

    expect(await screen.findByRole('region', { name: 'Logs of shop-web-1' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Follow' })).toBeTruthy()

    fireEvent.change(screen.getByRole('combobox', { name: 'Container' }), {
      target: { value: 'shop-db-1' },
    })
    expect(await screen.findByRole('region', { name: 'Logs of shop-db-1' })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('region', { name: /Logs of/ })).toBeNull()
    expect(screen.getByRole('combobox', { name: 'Container' })).toBeTruthy()
  })

  it('explains an offline agent and disables the actions', async () => {
    renderScreen(apiWith(), { connected: false })

    expect(
      await screen.findByText(
        'The agent of this server is not connected, so its containers cannot be shown or managed.',
      ),
    ).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Deploy' }) as HTMLButtonElement).disabled).toBe(
      true,
    )
    expect(screen.queryByText('Compose')).toBeNull()
  })

  it('explains a server without Docker and an empty stack', async () => {
    renderScreen(apiWith(), { dockerAvailable: false })
    expect(
      (await screen.findAllByText('Docker is not available on this server.')).length,
    ).toBeGreaterThan(0)
    // The compose editor does not need Docker and stays available.
    expect(screen.getByText('Compose')).toBeTruthy()
    cleanup()

    renderScreen(apiWith(), { containers: [] })
    expect(await screen.findByText('0 of 0 running')).toBeTruthy()
    expect(screen.getAllByText('This stack has no containers yet.')).toHaveLength(2)
  })

  it('goes back to the server from the link and the breadcrumb', async () => {
    const onBack = vi.fn()
    renderScreen(apiWith(), { onBack })
    await screen.findByText('Compose')

    fireEvent.click(screen.getByRole('button', { name: 'Back to the server' }))
    expect(onBack).toHaveBeenCalledTimes(1)

    fireEvent.click(within(screen.getByRole('navigation')).getByRole('button', { name: 'Stacks' }))
    expect(onBack).toHaveBeenCalledTimes(2)
  })

  it('waits for the containers to reload after an action', async () => {
    const onChanged = vi.fn()
    renderScreen(apiWith(), { onChanged })
    await screen.findByText('Compose')

    fireEvent.click(screen.getByRole('button', { name: 'Deploy' }))
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1))
  })
})
