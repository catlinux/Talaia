import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Job } from '../../api/containers'
import { ApiError } from '../../api/http'
import type { ServiceUnit, ServicesApi } from '../../api/services'
import i18n from '../../i18n'
import { ServicesSection } from './ServicesSection'

const serverId = 'srv-1'

function unit(
  name: string,
  description: string,
  activeState: string,
  subState: string,
  unitFileState: string,
): ServiceUnit {
  return { name, description, loadState: 'loaded', activeState, subState, unitFileState }
}

const initialServices: ServiceUnit[] = [
  unit('ssh.service', 'OpenSSH server', 'active', 'running', 'enabled'),
  unit('docker.service', 'Docker', 'failed', 'failed', 'enabled'),
  unit('nginx.service', 'Web server', 'inactive', 'dead', 'disabled'),
  unit('static.service', 'Static helper', 'inactive', 'dead', 'static'),
  unit('masked.service', 'Masked helper', 'active', 'running', 'masked'),
  unit('talaia-agent.service', 'Talaia agent', 'active', 'running', 'enabled'),
]

function updateUnit(
  services: ServiceUnit[],
  name: string,
  changes: Partial<ServiceUnit>,
): ServiceUnit[] {
  return services.map((service) => (service.name === name ? { ...service, ...changes } : service))
}

function job(output: string, extra: Partial<Job> = {}): Job {
  return { id: 'job-1', state: 'succeeded', message: '', output, ...extra }
}

function stateOutput(
  unitName: string,
  activeState: string,
  subState: string,
  unitFileState: string,
): string {
  return JSON.stringify({
    unit: unitName,
    active_state: activeState,
    sub_state: subState,
    unit_file_state: unitFileState,
    load_state: 'loaded',
  })
}

interface FakeOptions {
  initial?: ServiceUnit[]
  next?: ServiceUnit[]
  job?: Job | (() => Promise<Job>)
}

/** fakeServices returns the current list and applies next when an action runs. */
function fakeServices(options: FakeOptions = {}) {
  let current = options.initial ?? initialServices
  const action = vi.fn(() => {
    if (options.next) current = options.next
    return Promise.resolve('job-1')
  })
  const list = vi.fn(() => Promise.resolve(current))
  const jobCall = vi.fn(() => {
    const result = typeof options.job === 'function' ? options.job() : options.job
    return Promise.resolve(
      result ?? job(stateOutput('ssh.service', 'active', 'running', 'enabled')),
    )
  })
  return {
    api: { list, action, job: jobCall } satisfies ServicesApi,
    list,
    action,
    job: jobCall,
  }
}

function renderSection(api: ServicesApi) {
  render(<ServicesSection api={api} serverId={serverId} pollMs={1} />)
}

function rowFor(name: string): HTMLElement {
  return screen.getByRole('heading', { level: 3, name }).closest('li') as HTMLElement
}

function listNames(): string[] {
  return within(screen.getByRole('list'))
    .getAllByRole('listitem')
    .map((item) => within(item).getByRole('heading', { level: 3 }).textContent ?? '')
}

describe('systemd services section', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('lists failed services first and shows state, enabled state and counts', async () => {
    const { api } = fakeServices()
    renderSection(api)

    expect(await screen.findByText('6 services')).toBeTruthy()
    expect(listNames()).toEqual([
      'docker.service',
      'masked.service',
      'nginx.service',
      'ssh.service',
      'static.service',
      'talaia-agent.service',
    ])

    const docker = rowFor('docker.service')
    expect(within(docker).getByText('Failed')).toBeTruthy()
    expect(within(docker).getByText('Enabled')).toBeTruthy()
    expect(within(docker).getByText('Docker')).toBeTruthy()

    expect(screen.getByRole('button', { name: 'All (6)' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Active (3)' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Failed (1)' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Inactive (2)' })).toBeTruthy()
  })

  it('filters by state and searches name or description', async () => {
    const { api } = fakeServices()
    renderSection(api)
    await screen.findByText('6 services')

    fireEvent.click(screen.getByRole('button', { name: 'Active (3)' }))
    expect(listNames()).toEqual(['masked.service', 'ssh.service', 'talaia-agent.service'])

    const search = screen.getByLabelText('Search services')
    fireEvent.change(search, { target: { value: 'masked' } })
    expect(listNames()).toEqual(['masked.service'])

    fireEvent.click(screen.getByRole('button', { name: 'All (6)' }))
    fireEvent.change(search, { target: { value: 'web' } })
    expect(listNames()).toEqual(['nginx.service'])
  })

  it('asks before stopping, restarting or disabling and warns for access-critical services', async () => {
    const next = updateUnit(initialServices, 'docker.service', {
      activeState: 'inactive',
      subState: 'dead',
    })
    const { api } = fakeServices({
      next,
      job: job(stateOutput('docker.service', 'inactive', 'dead', 'enabled')),
    })
    renderSection(api)
    await screen.findByText('6 services')

    for (const [action, title] of [
      ['Stop', 'Stop docker.service?'],
      ['Restart', 'Restart docker.service?'],
      ['Disable', 'Disable docker.service?'],
    ] as const) {
      fireEvent.click(within(rowFor('docker.service')).getByRole('button', { name: action }))
      const dialog = screen.getByRole('dialog')
      expect(within(dialog).getByText(title)).toBeTruthy()
      expect(within(dialog).getByText('You may lose access to this server.')).toBeTruthy()
      expect(api.action).not.toHaveBeenCalled()
      fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    }

    fireEvent.click(within(rowFor('docker.service')).getByRole('button', { name: 'Stop' }))
    fireEvent.click(screen.getByRole('button', { name: 'Stop the service' }))

    await waitFor(() =>
      expect(api.action).toHaveBeenCalledWith(serverId, 'docker.service', 'stop', true),
    )
    expect(await screen.findByText('Done')).toBeTruthy()
    expect(within(rowFor('docker.service')).getByText('Inactive')).toBeTruthy()
    await waitFor(() => expect(api.list).toHaveBeenCalledTimes(2))
  })

  it('disables the row and shows a spinner while an action runs', async () => {
    let finishAction!: (job: Job) => void
    const pending = new Promise<Job>((resolve) => {
      finishAction = resolve
    })
    const next = updateUnit(initialServices, 'nginx.service', {
      activeState: 'active',
      subState: 'running',
    })
    const { api } = fakeServices({
      next,
      job: () => pending,
    })
    renderSection(api)
    await screen.findByText('6 services')

    fireEvent.click(within(rowFor('nginx.service')).getByRole('button', { name: 'Start' }))

    await waitFor(() =>
      expect(api.action).toHaveBeenCalledWith(serverId, 'nginx.service', 'start', false),
    )
    const row = rowFor('nginx.service')
    expect(within(row).getByText('Working…')).toBeTruthy()
    expect((within(row).getByRole('button', { name: 'Start' }) as HTMLButtonElement).disabled).toBe(
      true,
    )

    finishAction(job(stateOutput('nginx.service', 'active', 'running', 'disabled')))
    expect(await screen.findByText('Done')).toBeTruthy()
    expect(within(rowFor('nginx.service')).getByText('Active')).toBeTruthy()
    await waitFor(() => expect(api.list).toHaveBeenCalledTimes(2))
  })

  it('shows the job message when an action fails', async () => {
    const { api } = fakeServices({
      job: job('', { state: 'failed', message: 'Job for docker.service failed' }),
    })
    renderSection(api)
    await screen.findByText('6 services')

    fireEvent.click(within(rowFor('docker.service')).getByRole('button', { name: 'Restart' }))
    fireEvent.click(screen.getByRole('button', { name: 'Restart the service' }))

    expect((await screen.findByRole('alert')).textContent).toBe(
      'Failed: Job for docker.service failed',
    )
    await waitFor(() => expect(api.list).toHaveBeenCalledTimes(2))
  })

  it('protects the agent service and hides enable/disable for static states', async () => {
    const { api } = fakeServices()
    renderSection(api)
    await screen.findByText('6 services')

    const agent = rowFor('talaia-agent.service')
    expect(within(agent).queryByRole('button', { name: 'Stop' })).toBeNull()
    expect(within(agent).queryByRole('button', { name: 'Restart' })).toBeNull()
    expect(within(agent).queryByRole('button', { name: 'Disable' })).toBeNull()
    expect(within(agent).getByRole('button', { name: 'Reload' })).toBeTruthy()
    expect(
      within(agent).getByText('Talaia updates this service with the agent update.'),
    ).toBeTruthy()

    for (const name of ['static.service', 'masked.service']) {
      const row = rowFor(name)
      expect(within(row).queryByRole('button', { name: 'Enable' })).toBeNull()
      expect(within(row).queryByRole('button', { name: 'Disable' })).toBeNull()
    }
    expect(within(rowFor('nginx.service')).getByRole('button', { name: 'Enable' })).toBeTruthy()
    expect(within(rowFor('ssh.service')).getByRole('button', { name: 'Disable' })).toBeTruthy()
  })

  it('shows a readable error when the action is refused', async () => {
    const { api } = fakeServices()
    api.action.mockRejectedValueOnce(new ApiError('self_management', 400))
    renderSection(api)
    await screen.findByText('6 services')

    fireEvent.click(within(rowFor('nginx.service')).getByRole('button', { name: 'Start' }))

    expect((await screen.findByRole('alert')).textContent).toBe(
      'Talaia cannot manage its own agent service. Update the agent instead.',
    )
  })

  it('shows a readable error when the list cannot be loaded', async () => {
    const { api } = fakeServices()
    api.list.mockRejectedValueOnce(new ApiError('agent_offline', 409))
    renderSection(api)

    expect((await screen.findByRole('alert')).textContent).toBe(
      'The services could not be loaded: The agent of this server is not connected.',
    )
  })

  it('shows a loading state while the list arrives', () => {
    const api: ServicesApi = {
      list: () => new Promise<ServiceUnit[]>(() => undefined),
      action: () => Promise.reject(new Error('unexpected action')),
      job: () => Promise.reject(new Error('unexpected job')),
    }
    renderSection(api)

    expect(screen.getByRole('status').textContent).toBe('Reading the services…')
  })
})
