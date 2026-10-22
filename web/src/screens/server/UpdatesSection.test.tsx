import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AptApi, AptPackage } from '../../api/apt'
import type { Job } from '../../api/containers'
import i18n from '../../i18n'
import { UpdatesSection } from './UpdatesSection'

const serverId = 'srv-1'

function jestJob(output: string, extra: Partial<Job> = {}): Job {
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

/** fakeApi answers the initial and the refresh jobs by id. */
function fakeApi(initial: Job, refreshed?: Job) {
  const api = {
    status: vi.fn((_serverId: string, refresh = false) =>
      Promise.resolve(refresh ? 'refresh' : 'initial'),
    ),
    job: vi.fn((id: string) => {
      if (id === 'refresh' && refreshed) return Promise.resolve(refreshed)
      return Promise.resolve(initial)
    }),
  }
  return api satisfies AptApi
}

function renderSection(api: AptApi) {
  render(<UpdatesSection aptApi={api} serverId={serverId} pollMs={1} />)
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
      job: () => Promise.reject(new Error('unexpected job')),
    }
    renderSection(api)

    expect(screen.getByRole('status').textContent).toBe('Reading the updates…')
  })

  it('lists security packages first and shows times, held packages and reboot reasons', async () => {
    const api = fakeApi(
      jestJob(
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
    const api = fakeApi(jestJob(aptOutput([])))
    renderSection(api)

    expect(await screen.findByText('Everything is up to date.')).toBeTruthy()
    expect(screen.getByText('0 updates, 0 security')).toBeTruthy()
  })

  it('shows the message of a failed job', async () => {
    const api = fakeApi(
      jestJob('', {
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
      job: vi.fn((id: string) =>
        id === 'refresh' ? refreshJob : Promise.resolve(jestJob(aptOutput([security]))),
      ),
    }
    renderSection(api)

    expect(await screen.findByText('1 update, 1 security')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Check now' }))

    await waitFor(() => expect(api.status).toHaveBeenCalledWith(serverId, true))
    const checking = screen.getByRole('button', { name: 'Checking for updates…' })
    expect((checking as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getAllByText('Checking for updates…').length).toBeGreaterThan(1)

    finishRefresh(jestJob(aptOutput([normal, security])))
    expect(await screen.findByText('2 updates, 1 security')).toBeTruthy()
    await waitFor(() =>
      expect(
        (screen.getByRole('button', { name: 'Check now' }) as HTMLButtonElement).disabled,
      ).toBe(false),
    )
  })
})
