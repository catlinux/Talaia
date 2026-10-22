import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AptApi } from '../../api/apt'
import type { Container, ContainersApi, Job } from '../../api/containers'
import type { JournalApi } from '../../api/journal'
import type { ServersApi } from '../../api/servers'
import type { ServiceUnit, ServicesApi } from '../../api/services'
import i18n from '../../i18n'
import { ServerScreen } from '../ServerScreen'

const server = { id: 'srv-1', name: 'vps-web-01' }

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

const container = (overrides: Partial<Container> = {}): Container => ({
  id: 'aaaaaaaaaaaa',
  name: 'shop-web-1',
  image: 'nginx',
  state: 'running',
  status: 'Up',
  composeProject: 'shop',
  composeService: 'web',
  ...overrides,
})

const succeeded = (output = ''): Job => ({ id: 'job', state: 'succeeded', message: '', output })

/** fakeApi answers jobs from a queue; stacks lists the given names. */
function fakeApi(
  stacks: string[],
  jobs: Job[] = [succeeded()],
  extra: { adopted?: string[]; broken?: string[]; adoptable?: string[] } = {},
) {
  const queue = [...jobs]
  const api = {
    list: vi.fn(() =>
      Promise.resolve({ connected: true, dockerAvailable: true, containers: [container()] }),
    ),
    run: vi.fn(() => Promise.resolve('job')),
    logs: vi.fn(() => Promise.resolve('job')),
    stacks: vi.fn(() =>
      Promise.resolve({
        connected: true,
        stacks,
        adopted: extra.adopted ?? [],
        broken: extra.broken ?? [],
        adoptable: extra.adoptable ?? [],
      }),
    ),
    adoptStack: vi.fn(() => Promise.resolve('job')),
    listResources: vi.fn(() => Promise.resolve('job')),
    removeResources: vi.fn(() => Promise.resolve('job')),
    writeStack: vi.fn(() => Promise.resolve('job')),
    readStack: vi.fn(() => Promise.resolve('job')),
    runStack: vi.fn(() => Promise.resolve('job')),
    job: vi.fn(() => Promise.resolve(queue.length > 1 ? (queue.shift() as Job) : queue[0]!)),
  }
  return api satisfies ContainersApi
}

const serversApi: ServersApi = {
  listServers: () => Promise.resolve([]),
  updateAgent: () => Promise.resolve(),
  updateAllAgents: () => Promise.resolve({ queued: [], skipped: [] }),
  removeServer: () => Promise.resolve(),
}

function renderScreen(api: ContainersApi) {
  sessionStorage.setItem(`talaia.serverTab.${server.id}`, 'containers')
  render(
    <ServerScreen
      server={server}
      api={api}
      aptApi={pendingAptApi}
      servicesApi={pendingServicesApi}
      journalApi={pendingJournalApi}
      serversApi={serversApi}
      onBack={() => undefined}
      pollMs={1}
    />,
  )
}

describe('stacks of a server', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('lists the managed stacks with the state of their containers', async () => {
    renderScreen(fakeApi(['shop', 'blog']))

    expect(await screen.findByRole('heading', { level: 3, name: 'shop' })).toBeTruthy()
    expect(screen.getByRole('heading', { level: 3, name: 'blog' })).toBeTruthy()
    // The header of the screen and the card of the stack both say it.
    expect(screen.getAllByText('1 container · 1 running')).toHaveLength(2)
    expect(screen.getByText('No containers (stopped)')).toBeTruthy()
  })

  it('flags a broken adopted stack and only lets it be forgotten', async () => {
    const api = fakeApi(['wiki'], [succeeded()], { adopted: ['wiki'], broken: ['wiki'] })
    renderScreen(api)

    const open = await screen.findByRole('button', { name: 'wiki' })
    const row = open.closest('li')!
    expect(within(row).getByText('Files missing')).toBeTruthy()
    expect(within(row).getByText(/files of this project are gone/)).toBeTruthy()
    for (const name of ['Start', 'Stop', 'Restart', 'Pull images', 'Edit', 'Remove']) {
      expect(within(row).queryByRole('button', { name })).toBeNull()
    }

    fireEvent.click(within(row).getByRole('button', { name: 'Stop managing' }))
    const confirmation = screen.getByRole('group', { name: 'Stop managing wiki?' })
    fireEvent.click(within(confirmation).getByRole('button', { name: 'Stop managing' }))

    await waitFor(() => expect(api.runStack).toHaveBeenCalledWith('srv-1', 'wiki', 'forget', false))
  })

  it('invites to create the first stack when there are none', async () => {
    renderScreen(fakeApi([]))

    expect(await screen.findByText(/no managed stacks yet/)).toBeTruthy()
  })

  it('runs an operation on a stack without asking when it is not destructive', async () => {
    const api = fakeApi(['shop'])
    renderScreen(api)

    const card = (await screen.findByRole('heading', { level: 3, name: 'shop' })).closest('li')!
    fireEvent.click(within(card).getByRole('button', { name: 'Start' }))

    expect(api.runStack).toHaveBeenCalledWith('srv-1', 'shop', 'up', false)
    expect(await within(card).findByText('Done')).toBeTruthy()
  })

  it('asks before stopping a stack and only then sends the confirmation', async () => {
    const api = fakeApi(['shop'])
    renderScreen(api)
    const card = (await screen.findByRole('heading', { level: 3, name: 'shop' })).closest('li')!

    fireEvent.click(within(card).getByRole('button', { name: 'Stop' }))
    expect(api.runStack).not.toHaveBeenCalled()
    expect(within(card).getByText('Stop shop?')).toBeTruthy()

    fireEvent.click(within(card).getByRole('button', { name: 'Cancel' }))
    expect(api.runStack).not.toHaveBeenCalled()

    fireEvent.click(within(card).getByRole('button', { name: 'Stop' }))
    fireEvent.click(within(card).getByRole('button', { name: 'Stop the stack' }))
    expect(api.runStack).toHaveBeenCalledWith('srv-1', 'shop', 'down', true)
  })

  it('asks before removing a stack', async () => {
    const api = fakeApi(['shop'])
    renderScreen(api)
    const card = (await screen.findByRole('heading', { level: 3, name: 'shop' })).closest('li')!

    fireEvent.click(within(card).getByRole('button', { name: 'Remove' }))
    expect(within(card).getByText('Remove shop?')).toBeTruthy()
    fireEvent.click(within(card).getByRole('button', { name: 'Remove the stack' }))

    expect(api.runStack).toHaveBeenCalledWith('srv-1', 'shop', 'remove', true)
  })

  it('creates a stack: validates, saves and closes the editor', async () => {
    const api = fakeApi([])
    renderScreen(api)

    fireEvent.click(await screen.findByRole('button', { name: 'New stack' }))
    fireEvent.change(screen.getByLabelText(/Name/), { target: { value: 'blog' } })
    fireEvent.change(screen.getByLabelText('compose.yaml'), { target: { value: 'services: {}' } })
    fireEvent.change(screen.getByLabelText(/\.env/), { target: { value: 'A=1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Validate and save' }))

    await waitFor(() =>
      expect(api.writeStack).toHaveBeenCalledWith('srv-1', 'blog', 'services: {}', 'A=1'),
    )
    await waitFor(() => expect(screen.queryByRole('region', { name: 'New stack' })).toBeNull())
    expect(api.runStack).not.toHaveBeenCalled()
  })

  it('saves and starts in one go', async () => {
    const api = fakeApi([])
    renderScreen(api)

    fireEvent.click(await screen.findByRole('button', { name: 'New stack' }))
    fireEvent.change(screen.getByLabelText(/Name/), { target: { value: 'blog' } })
    fireEvent.change(screen.getByLabelText('compose.yaml'), { target: { value: 'services: {}' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save and start' }))

    await waitFor(() => expect(api.runStack).toHaveBeenCalledWith('srv-1', 'blog', 'up'))
  })

  it('shows the message of a rejected project and keeps the editor open', async () => {
    const api = fakeApi(
      [],
      [
        {
          id: 'job',
          state: 'failed',
          message: 'compose validation failed: line 3 is not valid',
          output: '',
        },
      ],
    )
    renderScreen(api)

    fireEvent.click(await screen.findByRole('button', { name: 'New stack' }))
    fireEvent.change(screen.getByLabelText(/Name/), { target: { value: 'blog' } })
    fireEvent.change(screen.getByLabelText('compose.yaml'), { target: { value: 'broken: [' } })
    fireEvent.click(screen.getByRole('button', { name: 'Validate and save' }))

    expect((await screen.findByRole('alert')).textContent).toContain('line 3 is not valid')
    expect((screen.getByLabelText('compose.yaml') as HTMLTextAreaElement).value).toBe('broken: [')
  })

  it('edits an existing stack: reads its files first', async () => {
    const api = fakeApi(
      ['shop'],
      [succeeded(JSON.stringify({ compose: 'services: {web: {}}', env: 'TOKEN=x' }))],
    )
    renderScreen(api)
    const card = (await screen.findByRole('heading', { level: 3, name: 'shop' })).closest('li')!

    fireEvent.click(within(card).getByRole('button', { name: 'Edit' }))

    await waitFor(() =>
      expect((screen.getByLabelText('compose.yaml') as HTMLTextAreaElement).value).toBe(
        'services: {web: {}}',
      ),
    )
    expect(api.readStack).toHaveBeenCalledWith('srv-1', 'shop')
    expect((screen.getByLabelText(/\.env/) as HTMLTextAreaElement).value).toBe('TOKEN=x')
    // The name of an existing stack cannot change.
    expect((screen.getByLabelText(/Name/) as HTMLInputElement).disabled).toBe(true)
  })
})
