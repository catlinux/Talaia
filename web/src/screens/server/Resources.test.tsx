import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AptApi } from '../../api/apt'
import type { ContainersApi, Job, ResourceItem, ResourceKind } from '../../api/containers'
import type { ServersApi } from '../../api/servers'
import type { ServiceUnit, ServicesApi } from '../../api/services'
import i18n from '../../i18n'
import { ServerScreen } from '../ServerScreen'

const server = { id: 'srv-1', name: 'vps-web-01' }

// The apt section is covered by its own tests; leave its request pending here.
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

const images: ResourceItem[] = [
  { id: 'sha256:' + 'b'.repeat(64), tags: ['old:1.0'], size: 300_000_000, in_use: false },
  { id: 'sha256:' + 'c'.repeat(64), size: 100_000_000, in_use: false },
  { id: 'sha256:' + 'a'.repeat(64), tags: ['nginx:1.27'], size: 50_000_000, in_use: true },
]

const volumes: ResourceItem[] = [
  { id: 'old_cache', name: 'old_cache', driver: 'local', in_use: false },
  { id: 'app_data', name: 'app_data', driver: 'local', in_use: true },
]

const networks: ResourceItem[] = [
  { id: 'a'.repeat(12), name: 'orphan', driver: 'bridge', in_use: false },
  { id: 'b'.repeat(12), name: 'bridge', driver: 'bridge', in_use: false, system: true },
]

const job = (output: string, extra: Partial<Job> = {}): Job => ({
  id: 'job',
  state: 'succeeded',
  message: '',
  output,
  ...extra,
})

/** fakeApi answers listings per kind and removals with the given results. */
function fakeApi(removalOutput = '{"results":[]}') {
  let pending = ''
  const lists: Record<ResourceKind, ResourceItem[]> = {
    image: images,
    volume: volumes,
    network: networks,
  }
  const api = {
    list: vi.fn(() => Promise.resolve({ connected: true, dockerAvailable: true, containers: [] })),
    run: vi.fn(() => Promise.resolve('job')),
    logs: vi.fn(() => Promise.resolve('job')),
    stacks: vi.fn(() =>
      Promise.resolve({ connected: true, stacks: [], adopted: [], broken: [], adoptable: [] }),
    ),
    adoptStack: vi.fn(() => Promise.resolve('job')),
    writeStack: vi.fn(() => Promise.resolve('job')),
    readStack: vi.fn(() => Promise.resolve('job')),
    runStack: vi.fn(() => Promise.resolve('job')),
    listResources: vi.fn((_serverId: string, kind: ResourceKind) => {
      pending = JSON.stringify({ kind, items: lists[kind] })
      return Promise.resolve('job')
    }),
    removeResources: vi.fn(() => {
      pending = removalOutput
      return Promise.resolve('job')
    }),
    job: vi.fn(() => Promise.resolve(job(pending))),
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
  render(
    <ServerScreen
      server={server}
      api={api}
      aptApi={pendingAptApi}
      servicesApi={pendingServicesApi}
      serversApi={serversApi}
      onBack={() => undefined}
      pollMs={1}
    />,
  )
}

async function openResources() {
  fireEvent.click(await screen.findByRole('button', { name: 'View and clean up' }))
}

describe('Docker resources of a server', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('stays closed until it is opened and then lists the images', async () => {
    const api = fakeApi()
    renderScreen(api)

    await screen.findByRole('heading', { level: 2, name: 'Images, volumes and networks' })
    expect(api.listResources).not.toHaveBeenCalled()

    await openResources()

    expect(await screen.findByText('old:1.0')).toBeTruthy()
    expect(api.listResources).toHaveBeenCalledWith('srv-1', 'image')
    // Two of the three images are free: 400 MB can be freed.
    expect(screen.getByText(/2 unused of 3/)).toBeTruthy()
  })

  it('does not let a resource in use be selected', async () => {
    renderScreen(fakeApi())
    await openResources()
    await screen.findByText('old:1.0')

    const used = screen.getByRole('checkbox', { name: 'Select nginx:1.27' }) as HTMLInputElement
    const free = screen.getByRole('checkbox', { name: 'Select old:1.0' }) as HTMLInputElement

    expect(used.disabled).toBe(true)
    expect(free.disabled).toBe(false)
    expect(screen.getByText('In use')).toBeTruthy()
  })

  it('selects what is unused, asks, and removes only the selection', async () => {
    const api = fakeApi(
      '{"results":[{"kind":"image","id":"x","ok":true},{"kind":"image","id":"y","ok":true}]}',
    )
    renderScreen(api)
    await openResources()
    await screen.findByText('old:1.0')

    fireEvent.click(screen.getByRole('button', { name: 'Select what is unused' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove the selection (2)' }))
    // Nothing is sent before the confirmation.
    expect(api.removeResources).not.toHaveBeenCalled()
    expect(screen.getByText('Remove 2 items?')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Remove' }))

    await waitFor(() => expect(api.removeResources).toHaveBeenCalledTimes(1))
    expect(api.removeResources).toHaveBeenCalledWith('srv-1', [
      { kind: 'image', id: images[0]!.id },
      { kind: 'image', id: images[1]!.id },
    ])
    expect(await screen.findByText('2 of 2 removed.')).toBeTruthy()
    // The list is read again afterwards.
    await waitFor(() => expect(api.listResources.mock.calls.length).toBeGreaterThanOrEqual(2))
  })

  it('can cancel the confirmation', async () => {
    const api = fakeApi()
    renderScreen(api)
    await openResources()
    await screen.findByText('old:1.0')

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select old:1.0' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove the selection (1)' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(api.removeResources).not.toHaveBeenCalled()
    expect(screen.queryByText('Remove 1 item?')).toBeNull()
  })

  it('warns about data when volumes are removed', async () => {
    renderScreen(fakeApi())
    await openResources()
    fireEvent.click(await screen.findByRole('tab', { name: 'Volumes' }))
    await screen.findByText('old_cache')

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select old_cache' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove the selection (1)' }))

    expect(screen.getByText(/may hold data that cannot be recovered/)).toBeTruthy()
  })

  it('never offers the system networks', async () => {
    renderScreen(fakeApi())
    await openResources()
    fireEvent.click(await screen.findByRole('tab', { name: 'Networks' }))
    await screen.findByText('orphan')

    const bridge = screen.getByRole('checkbox', { name: 'Select bridge' }) as HTMLInputElement
    expect(bridge.disabled).toBe(true)
    expect(screen.getByText('System')).toBeTruthy()
  })

  it('lists the resources Docker refused to remove', async () => {
    const api = fakeApi(
      '{"results":[{"kind":"volume","id":"old_cache","ok":false,"message":"docker: volume is in use"}]}',
    )
    renderScreen(api)
    await openResources()
    fireEvent.click(await screen.findByRole('tab', { name: 'Volumes' }))
    await screen.findByText('old_cache')

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select old_cache' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove the selection (1)' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }))

    const report = await screen.findByText('0 of 1 removed.')
    expect(report).toBeTruthy()
    expect(
      within(report.closest('.resources__report') as HTMLElement).getByText(/volume is in use/),
    ).toBeTruthy()
  })
})
