import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AptApi, AptPackage } from '../../api/apt'
import type { Job } from '../../api/containers'
import { ApiError } from '../../api/http'
import i18n from '../../i18n'
import { UpdatesSection } from './UpdatesSection'

const serverId = 'srv-1'

function jobFor(output: string, extra: Partial<Job> = {}): Job {
  return { id: 'apt-job', state: 'succeeded', message: '', output, ...extra }
}

interface OutputOptions {
  held?: string[]
  rebootRequired?: boolean
  rebootReasons?: string[]
  listsUpdatedAt?: string
  checkedAt?: string
}

/** aptOutput builds the JSON the agent returns from an apt status job. */
function aptOutput(packages: AptPackage[], options: OutputOptions = {}): string {
  return JSON.stringify({
    packages,
    held: options.held ?? [],
    reboot_required: options.rebootRequired ?? false,
    reboot_reasons: options.rebootReasons ?? [],
    lists_updated_at: options.listsUpdatedAt ?? '2026-10-22T08:00:00Z',
    checked_at: options.checkedAt ?? '2026-10-22T09:00:00Z',
  })
}

const normal: AptPackage = {
  name: 'curl',
  current: '8.10.1-1',
  candidate: '8.12.1-1',
  origin: 'Debian:13/stable',
  security: false,
}

const security: AptPackage = {
  name: 'openssl',
  current: '3.0.15-1',
  candidate: '3.0.16-1',
  origin: 'Debian-Security:13/stable-security',
  security: true,
}

const docker: AptPackage = {
  name: 'docker-ce',
  current: '5:27.0.0-1',
  candidate: '5:27.0.1-1',
  origin: 'Docker:13/stable',
  security: false,
}

/** fakeApi answers the initial and the refresh jobs by id. */
function fakeApi(initial: Job, refreshed?: Job) {
  const api = {
    status: vi.fn((_serverId: string, refresh = false) =>
      Promise.resolve(refresh ? 'refresh' : 'initial'),
    ),
    upgrade: vi.fn(() => Promise.resolve('upgrade')),
    reboot: vi.fn(() => Promise.resolve('reboot')),
    job: vi.fn((id: string) => {
      if (id === 'refresh' && refreshed) return Promise.resolve(refreshed)
      return Promise.resolve(initial)
    }),
  }
  return api satisfies AptApi
}

function renderSection(api: AptApi, hostname = 'vps-web-01') {
  render(<UpdatesSection aptApi={api} serverId={serverId} hostname={hostname} pollMs={1} />)
}

describe('apt updates section', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('shows a loading state while the agent answers', () => {
    const api: AptApi = {
      status: () => new Promise<string>(() => undefined),
      upgrade: () => Promise.reject(new Error('unexpected apt upgrade')),
      reboot: () => Promise.reject(new Error('unexpected reboot')),
      job: () => Promise.reject(new Error('unexpected job')),
    }
    renderSection(api)

    expect(screen.getByRole('status').textContent).toBe('Reading the updates…')
  })

  it('lists security packages first and shows times, held packages and reboot reasons', async () => {
    const api = fakeApi(
      jobFor(
        aptOutput([normal, security], {
          held: ['linux-image-amd64'],
          rebootRequired: true,
          rebootReasons: ['A newer kernel is installed.'],
        }),
      ),
    )
    renderSection(api)

    expect(await screen.findByText('2 updates, 1 security')).toBeTruthy()
    expect(screen.getByText('Last checked')).toBeTruthy()
    expect(screen.getByText('Package lists updated')).toBeTruthy()

    const rows = screen.getAllByRole('row')
    expect(within(rows[0]!).getByText('Package')).toBeTruthy()
    expect(within(rows[1]!).getByText('openssl')).toBeTruthy()
    expect(within(rows[1]!).getByText('Security')).toBeTruthy()
    expect(within(rows[2]!).getByText('curl')).toBeTruthy()
    expect(rows[1]!.textContent).toContain('3.0.15-1')
    expect(rows[1]!.textContent).toContain('3.0.16-1')

    expect(screen.getByText('A reboot is required')).toBeTruthy()
    expect(screen.getByText('A newer kernel is installed.')).toBeTruthy()

    const held = screen.getByText('Held packages (1)').closest('details')
    expect(held?.open).toBe(false)
    fireEvent.click(screen.getByText('Held packages (1)'))
    expect(held?.open).toBe(true)
    expect(screen.getByText('linux-image-amd64')).toBeTruthy()
  })

  it('shows the empty state when everything is up to date', async () => {
    const api = fakeApi(jobFor(aptOutput([])))
    renderSection(api)

    expect(await screen.findByText('Everything is up to date.')).toBeTruthy()
    expect(screen.getByText('0 updates, 0 security')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Update security/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /Update all/ })).toBeNull()
    expect(screen.getByRole('button', { name: 'Reboot' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Reboot now' })).toBeNull()
  })

  it('shows the message of a failed job', async () => {
    const api = fakeApi(
      jobFor('', {
        state: 'failed',
        message: 'apt: another apt process holds the lock',
      }),
    )
    renderSection(api)

    expect((await screen.findByRole('alert')).textContent).toBe(
      'The updates could not be read: apt: another apt process holds the lock',
    )
  })

  it('checks again with refresh=true and keeps a progress state while it waits', async () => {
    let finishRefresh!: (job: Job) => void
    const refreshJob = new Promise<Job>((resolve) => {
      finishRefresh = resolve
    })
    const api: AptApi = {
      status: vi.fn((_serverId: string, refresh = false) =>
        Promise.resolve(refresh ? 'refresh' : 'initial'),
      ),
      upgrade: () => Promise.reject(new Error('unexpected apt upgrade')),
      reboot: () => Promise.reject(new Error('unexpected reboot')),
      job: vi.fn((id: string) =>
        id === 'refresh' ? refreshJob : Promise.resolve(jobFor(aptOutput([security]))),
      ),
    }
    renderSection(api)

    expect(await screen.findByText('1 update, 1 security')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Check now' }))

    await waitFor(() => expect(api.status).toHaveBeenCalledWith(serverId, true))
    const checking = screen.getByRole('button', { name: 'Checking for updates…' })
    expect((checking as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getAllByText('Checking for updates…').length).toBeGreaterThan(1)

    finishRefresh(jobFor(aptOutput([normal, security])))
    expect(await screen.findByText('2 updates, 1 security')).toBeTruthy()
    await waitFor(() =>
      expect(
        (screen.getByRole('button', { name: 'Check now' }) as HTMLButtonElement).disabled,
      ).toBe(false),
    )
  })

  it('asks before upgrading security and shows the live log until the list reloads', async () => {
    const initialJob = jobFor(aptOutput([normal, security]))
    let pendingCalls = 0
    let resolveRunning!: (job: Job) => void
    let resolveFinal!: (job: Job) => void
    const api: AptApi = {
      status: vi.fn(() => Promise.resolve('initial')),
      upgrade: vi.fn(() => Promise.resolve('upgrade-job')),
      reboot: () => Promise.reject(new Error('unexpected reboot')),
      job: vi.fn((id: string) => {
        if (id === 'initial') return Promise.resolve(initialJob)
        if (id !== 'upgrade-job') return Promise.reject(new Error('unexpected job'))
        pendingCalls += 1
        if (pendingCalls === 1) {
          return new Promise<Job>((resolve) => {
            resolveRunning = resolve
          })
        }
        return new Promise<Job>((resolve) => {
          resolveFinal = resolve
        })
      }),
    }
    renderSection(api)

    expect(await screen.findByText('2 updates, 1 security')).toBeTruthy()
    const updateSecurity = screen.getByRole('button', { name: 'Update security (1)' })
    fireEvent.click(updateSecurity)
    expect(screen.getByRole('dialog')).toBeTruthy()
    expect(screen.getByText('Update the security packages?')).toBeTruthy()
    expect(api.upgrade).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(api.upgrade).not.toHaveBeenCalled()

    fireEvent.click(updateSecurity)
    fireEvent.click(screen.getByRole('button', { name: 'Start the upgrade' }))
    await waitFor(() => expect(api.upgrade).toHaveBeenCalledWith(serverId, 'security'))
    await waitFor(() => expect(api.job).toHaveBeenCalledWith('upgrade-job'))

    resolveRunning({
      id: 'upgrade-job',
      state: 'running',
      message: '',
      output: 'Reading package lists...\n',
    })
    expect(await screen.findByText(/Reading package lists/)).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Check now' }) as HTMLButtonElement).disabled).toBe(
      true,
    )
    expect(
      (screen.getByRole('button', { name: 'Update security (1)' }) as HTMLButtonElement).disabled,
    ).toBe(true)
    expect((screen.getByRole('button', { name: 'Reboot' }) as HTMLButtonElement).disabled).toBe(
      true,
    )

    resolveFinal({
      id: 'upgrade-job',
      state: 'succeeded',
      message: '',
      output: 'Reading package lists...\nDone\n',
    })
    expect(await screen.findByText('The upgrade finished.')).toBeTruthy()
    expect(screen.getByRole('log').textContent).toContain('Done')
    await waitFor(() => expect(api.status).toHaveBeenCalledTimes(2))
    await waitFor(() =>
      expect(
        (screen.getByRole('button', { name: 'Update security (1)' }) as HTMLButtonElement).disabled,
      ).toBe(false),
    )
  })

  it('warns about Docker and held packages in the all dialog', async () => {
    const api = fakeApi(jobFor(aptOutput([security, docker], { held: ['linux-image-amd64'] })))
    renderSection(api)

    expect(await screen.findByText('2 updates, 1 security')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Update all (2)' }))

    expect(screen.getByText('Update all packages?')).toBeTruthy()
    expect(
      screen.getByText(/Docker will restart and so will your containers: docker-ce/),
    ).toBeTruthy()
    expect(screen.getByText('1 held package stays at its current version.')).toBeTruthy()
    expect(api.upgrade).not.toHaveBeenCalled()
  })

  it('shows a failed upgrade with its message and log', async () => {
    const initialJob = jobFor(aptOutput([security]))
    const api: AptApi = {
      status: vi.fn(() => Promise.resolve('initial')),
      upgrade: vi.fn(() => Promise.resolve('upgrade-job')),
      reboot: () => Promise.reject(new Error('unexpected reboot')),
      job: vi.fn((id: string) =>
        id === 'upgrade-job'
          ? Promise.resolve(
              jobFor('E: Could not get lock /var/lib/dpkg/lock-frontend\n', {
                id: 'upgrade-job',
                state: 'failed',
                message: 'apt-get exited with status 100',
              }),
            )
          : Promise.resolve(initialJob),
      ),
    }
    renderSection(api)

    expect(await screen.findByText('1 update, 1 security')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Update security (1)' }))
    fireEvent.click(screen.getByRole('button', { name: 'Start the upgrade' }))

    expect(
      await screen.findByText('The upgrade failed: apt-get exited with status 100'),
    ).toBeTruthy()
    expect(screen.getByRole('log').textContent).toContain('Could not get lock')
  })

  it('requires the exact hostname before enabling reboot', async () => {
    const initialJob = jobFor(
      aptOutput([security], {
        rebootRequired: true,
        rebootReasons: ['A newer kernel is installed.'],
      }),
    )
    const api: AptApi = {
      status: vi.fn(() => Promise.resolve('initial')),
      upgrade: () => Promise.reject(new Error('unexpected upgrade')),
      reboot: vi.fn(() => Promise.resolve('reboot-job')),
      job: vi.fn((id: string) =>
        id === 'reboot-job'
          ? Promise.resolve(jobFor('', { id: 'reboot-job', state: 'succeeded' }))
          : Promise.resolve(initialJob),
      ),
    }
    renderSection(api)

    expect(await screen.findByText('1 update, 1 security')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Reboot now' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Reboot' }))

    expect(screen.getByText('Reboot the server?')).toBeTruthy()
    expect(
      within(screen.getByRole('dialog')).getByText('A newer kernel is installed.'),
    ).toBeTruthy()
    const confirm = screen.getByRole('button', { name: 'Reboot the server' }) as HTMLButtonElement
    expect(confirm.disabled).toBe(true)

    const input = screen.getByLabelText('Type the hostname to confirm')
    fireEvent.change(input, { target: { value: 'other-server' } })
    expect(confirm.disabled).toBe(true)
    expect(api.reboot).not.toHaveBeenCalled()

    fireEvent.change(input, { target: { value: '  VPS-WEB-01 ' } })
    expect(confirm.disabled).toBe(false)
    fireEvent.click(confirm)

    await waitFor(() => expect(api.reboot).toHaveBeenCalledWith(serverId, 'vps-web-01'))
    expect(await screen.findByText('Rebooting… the server will reconnect by itself.')).toBeTruthy()
  })

  it('shows a readable error when another apt operation is running', async () => {
    const api = fakeApi(jobFor(aptOutput([security])))
    api.upgrade.mockRejectedValueOnce(new ApiError('operation_in_progress', 409))
    renderSection(api)

    expect(await screen.findByText('1 update, 1 security')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Update security (1)' }))
    fireEvent.click(screen.getByRole('button', { name: 'Start the upgrade' }))

    expect((await screen.findByRole('alert')).textContent).toBe(
      'Another apt operation or reboot is already running for this server.',
    )
  })
})
