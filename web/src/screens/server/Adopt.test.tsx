import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Container, ContainersApi, Job } from '../../api/containers'
import type { ServersApi } from '../../api/servers'
import i18n from '../../i18n'
import { ServerScreen } from '../ServerScreen'

const server = { id: 'srv-1', name: 'vps-web-01' }

const container = (overrides: Partial<Container> = {}): Container => ({
  id: 'aaaaaaaaaaaa',
  name: 'wiki-app-1',
  image: 'wiki',
  state: 'running',
  status: 'Up',
  composeProject: 'wiki',
  composeService: 'app',
  ...overrides,
})

const succeeded: Job = { id: 'job', state: 'succeeded', message: '', output: '' }

function fakeApi(
  stacks: { stacks: string[]; adopted: string[]; adoptable: string[] },
  jobs: Job[] = [succeeded],
) {
  const queue = [...jobs]
  return {
    list: vi.fn(() =>
      Promise.resolve({ connected: true, dockerAvailable: true, containers: [container()] }),
    ),
    run: vi.fn(() => Promise.resolve('job')),
    logs: vi.fn(() => Promise.resolve('job')),
    stacks: vi.fn(() => Promise.resolve({ connected: true, ...stacks })),
    adoptStack: vi.fn(() => Promise.resolve('job')),
    listResources: vi.fn(() => Promise.resolve('job')),
    removeResources: vi.fn(() => Promise.resolve('job')),
    writeStack: vi.fn(() => Promise.resolve('job')),
    readStack: vi.fn(() => Promise.resolve('job')),
    runStack: vi.fn(() => Promise.resolve('job')),
    job: vi.fn(() => Promise.resolve(queue.length > 1 ? (queue.shift() as Job) : queue[0]!)),
  } satisfies ContainersApi
}

const serversApi: ServersApi = {
  listServers: () => Promise.resolve([]),
  updateAgent: () => Promise.resolve(),
  updateAllAgents: () => Promise.resolve({ queued: [], skipped: [] }),
  removeServer: () => Promise.resolve(),
}

function renderScreen(api: ContainersApi) {
  render(
    <ServerScreen
      server={server}
      api={api}
      serversApi={serversApi}
      onBack={() => undefined}
      pollMs={1}
    />,
  )
}

describe('adopting running projects', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('offers the running projects that are not managed yet and adopts one', async () => {
    const api = fakeApi({ stacks: [], adopted: [], adoptable: ['wiki'] })
    renderScreen(api)

    expect(await screen.findByRole('heading', { level: 3, name: 'Running projects' })).toBeTruthy()
    expect(screen.getByText(/nothing is copied or restarted/)).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Manage with Talaia' }))

    await waitFor(() => expect(api.adoptStack).toHaveBeenCalledWith('srv-1', 'wiki'))
    // The list is read again once the job is done.
    await waitFor(() => expect(api.stacks.mock.calls.length).toBeGreaterThanOrEqual(2))
  })

  it('shows why an adoption failed', async () => {
    const api = fakeApi({ stacks: [], adopted: [], adoptable: ['wiki'] }, [
      {
        id: 'job',
        state: 'failed',
        message: 'only projects with a single compose file can be adopted',
        output: '',
      },
    ])
    renderScreen(api)

    fireEvent.click(await screen.findByRole('button', { name: 'Manage with Talaia' }))

    expect((await screen.findByRole('alert')).textContent).toBe(
      'Failed: only projects with a single compose file can be adopted',
    )
  })

  it('marks an adopted stack and offers to stop managing it instead of removing it', async () => {
    const api = fakeApi({ stacks: ['wiki'], adopted: ['wiki'], adoptable: [] })
    renderScreen(api)

    await screen.findByText('Stacks')
    const card = await waitFor(() => {
      const found = document.querySelector('.stacklist li')
      if (found === null) throw new Error('the stack card is not there yet')
      return found as HTMLElement
    })
    expect(within(card).getByText('adopted')).toBeTruthy()
    expect(within(card).queryByRole('button', { name: 'Remove' })).toBeNull()

    fireEvent.click(within(card).getByRole('button', { name: 'Stop managing' }))
    expect(api.runStack).not.toHaveBeenCalled()
    expect(within(card).getByText('Stop managing wiki?')).toBeTruthy()
    expect(
      within(card).getByText(
        'Talaia forgets this stack. The containers and the files stay as they are.',
      ),
    ).toBeTruthy()

    fireEvent.click(within(card).getAllByRole('button', { name: 'Stop managing' })[0]!)
    await waitFor(() => expect(api.runStack).toHaveBeenCalledWith('srv-1', 'wiki', 'forget', false))
  })

  it('keeps removing as the way out of a stack that Talaia created', async () => {
    const api = fakeApi({ stacks: ['blog'], adopted: [], adoptable: [] })
    renderScreen(api)

    const card = (await screen.findByRole('heading', { level: 3, name: 'blog' })).closest('li')!

    expect(within(card).getByRole('button', { name: 'Remove' })).toBeTruthy()
    expect(within(card).queryByText('adopted')).toBeNull()
  })
})
