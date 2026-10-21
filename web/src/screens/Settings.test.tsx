import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../api/http'
import type { SystemApi, SystemVersion, UpdateInfo, UpdateStatus } from '../api/system'
import i18n from '../i18n'
import { Settings, pollEvery } from './Settings'

const current = 'a'.repeat(40)
const latest = 'b'.repeat(40)

const baseUpdate: UpdateInfo = {
  current,
  latest,
  pending: [{ sha: 'bbbbbbb', subject: 'Fix the panel', author: 'Ana' }],
  ci: 'green',
  checkedAt: '2026-10-21T08:00:00Z',
  error: '',
  updateAvailable: true,
}

const baseStatus: UpdateStatus = {
  state: 'idle',
  from: '',
  to: '',
  startedAt: '',
  finishedAt: '',
  log: [],
}

interface SystemOverrides {
  version?: string
  commit?: string
  update?: Partial<UpdateInfo>
  status?: Partial<UpdateStatus>
}

/** makeSystem builds the answer of the API with sensible defaults. */
function makeSystem(overrides: SystemOverrides = {}): SystemVersion {
  return {
    version: overrides.version ?? '1.2.3',
    commit: overrides.commit ?? current,
    update: { ...baseUpdate, ...overrides.update },
    status: { ...baseStatus, ...overrides.status },
  }
}

function apiWith(
  version: () => Promise<SystemVersion>,
  update?: SystemApi['update'],
  check?: SystemApi['check'],
): SystemApi {
  return {
    version,
    check: check ?? vi.fn(() => Promise.reject(new Error('unexpected check'))),
    update: update ?? vi.fn(() => Promise.reject(new Error('unexpected update'))),
  }
}

function renderSettings(api: SystemApi) {
  render(<Settings api={api} />)
}

describe('Settings', () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    vi.useRealTimers()
    cleanup()
  })

  it('shows the version, the commit, the CI badge and the pending commits', async () => {
    renderSettings(apiWith(() => Promise.resolve(makeSystem())))

    expect(await screen.findByText('1.2.3')).toBeTruthy()
    expect(screen.getByText('aaaaaaa')).toBeTruthy()
    expect(screen.getByText('CI green')).toBeTruthy()
    expect(screen.getByText('Latest green commit bbbbbbb')).toBeTruthy()
    expect(screen.getByText('1 commit pending')).toBeTruthy()
    expect(screen.getByText('Fix the panel')).toBeTruthy()
    expect(screen.getByText('Ana')).toBeTruthy()
    expect(screen.getByText('Pending commits')).toBeTruthy()

    const button = screen.getByRole('button', { name: 'Update to bbbbbbb' }) as HTMLButtonElement
    expect(button.disabled).toBe(false)
  })

  it('says the version is unknown when the build has no commit', async () => {
    renderSettings(apiWith(() => Promise.resolve(makeSystem({ commit: '' }))))

    expect(await screen.findByText('unknown version')).toBeTruthy()
  })

  it('lists at most ten pending commits and summarises the rest', async () => {
    const pending = Array.from({ length: 12 }, (_, index) => ({
      sha: index.toString(16).padStart(40, '0'),
      subject: `Commit ${index}`,
      author: 'Ana',
    }))
    renderSettings(apiWith(() => Promise.resolve(makeSystem({ update: { pending } }))))

    expect(await screen.findByText('Commit 9')).toBeTruthy()
    expect(screen.queryByText('Commit 10')).toBeNull()
    expect(screen.getByText('and 2 more')).toBeTruthy()
  })

  it('disables the button when there is no update available', async () => {
    renderSettings(
      apiWith(() =>
        Promise.resolve(
          makeSystem({ update: { latest: current, updateAvailable: false, pending: [] } }),
        ),
      ),
    )

    const button = (await screen.findByRole('button', {
      name: 'Update to aaaaaaa',
    })) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    expect(screen.getByText('Up to date')).toBeTruthy()
  })

  it('explains that there is no green build yet', async () => {
    renderSettings(
      apiWith(() =>
        Promise.resolve(
          makeSystem({ update: { latest: '', updateAvailable: false, pending: [] } }),
        ),
      ),
    )

    expect(await screen.findByText('No green build yet')).toBeTruthy()
    const button = screen.getByRole('button', { name: 'No update available' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
  })

  it('disables the button when the host helper is missing', async () => {
    renderSettings(
      apiWith(() => Promise.resolve(makeSystem({ update: { error: 'host_helper_missing' } }))),
    )

    expect(
      await screen.findByText(/The host update helper is not installed or is not running/),
    ).toBeTruthy()
    const button = screen.getByRole('button', { name: 'Update to bbbbbbb' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
  })

  const friendlyErrors = [
    ['rate_limited', 'GitHub has rate-limited the update checks. The panel will try again later.'],
    ['unreachable', 'The update server could not be reached. The panel will try again later.'],
    ['invalid_response', 'The update check failed. The panel will try again later.'],
  ] as const

  for (const [code, message] of friendlyErrors) {
    it(`shows a friendly message for the "${code}" check error`, async () => {
      renderSettings(apiWith(() => Promise.resolve(makeSystem({ update: { error: code } }))))

      expect(await screen.findByText(message)).toBeTruthy()
    })
  }

  it('checks now and replaces the version card with the new answer', async () => {
    const refreshed = makeSystem({
      update: {
        latest: 'd'.repeat(40),
        pending: [{ sha: 'ddddddd', subject: 'New commit', author: 'Bea' }],
        checkedAt: '2026-10-21T09:00:00Z',
        updateAvailable: true,
      },
    })
    const check = vi.fn(() => Promise.resolve(refreshed))
    renderSettings(apiWith(() => Promise.resolve(makeSystem()), undefined, check))

    fireEvent.click(await screen.findByRole('button', { name: 'Check now' }))

    expect(check).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('New commit')).toBeTruthy()
    expect(screen.getByText('Latest green commit ddddddd')).toBeTruthy()
    const checked = new Intl.DateTimeFormat('en', {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date('2026-10-21T09:00:00Z'))
    expect(screen.getByText(checked)).toBeTruthy()
  })

  it('disables Check now while the check runs', async () => {
    let resolveCheck: (system: SystemVersion) => void = () => {}
    const check = vi.fn(
      () =>
        new Promise<SystemVersion>((resolve) => {
          resolveCheck = resolve
        }),
    )
    renderSettings(apiWith(() => Promise.resolve(makeSystem()), undefined, check))

    const button = (await screen.findByRole('button', { name: 'Check now' })) as HTMLButtonElement
    fireEvent.click(button)
    expect(button.disabled).toBe(true)
    expect(screen.getByRole('button', { name: 'Checking.' })).toBeTruthy()

    await act(async () => {
      resolveCheck(makeSystem())
    })
    expect(await screen.findByRole('button', { name: 'Check now' })).toBeTruthy()
  })

  it('shows the short notice when the check is rate limited', async () => {
    const check = vi.fn(() => Promise.reject(new ApiError('rate_limited', 429)))
    renderSettings(apiWith(() => Promise.resolve(makeSystem()), undefined, check))

    fireEvent.click(await screen.findByRole('button', { name: 'Check now' }))

    expect(await screen.findByText('Checked a moment ago. Try again in a minute.')).toBeTruthy()
    expect(
      screen.queryByText(
        'GitHub has rate-limited the update checks. The panel will try again later.',
      ),
    ).toBeNull()
  })

  it('shows the usual error for another failed check', async () => {
    const check = vi.fn(() => Promise.reject(new ApiError('internal', 500)))
    renderSettings(apiWith(() => Promise.resolve(makeSystem()), undefined, check))

    fireEvent.click(await screen.findByRole('button', { name: 'Check now' }))

    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(screen.getByText('Something went wrong. Try again.')).toBeTruthy()
  })

  it('disables Check now while an update is running', async () => {
    renderSettings(
      apiWith(() =>
        Promise.resolve(makeSystem({ status: { state: 'running', log: ['building'] } })),
      ),
    )

    expect(await screen.findByText('The update is running.')).toBeTruthy()
    const button = screen.getByRole('button', { name: 'Check now' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
  })

  it('disables Check now while an update is queued', async () => {
    const update = vi.fn(() => new Promise<{ target: string }>(() => {}))
    renderSettings(apiWith(() => Promise.resolve(makeSystem()), update))

    fireEvent.click(await screen.findByRole('button', { name: 'Update to bbbbbbb' }))
    fireEvent.click(screen.getByRole('button', { name: 'Update now' }))
    const button = screen.getByRole('button', { name: 'Check now' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
  })

  it('asks for confirmation before queuing the update', async () => {
    const update = vi.fn(() => Promise.resolve({ target: latest }))
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockResolvedValueOnce(makeSystem())
      .mockResolvedValue(makeSystem({ status: { state: 'running', log: ['building'] } }))
    renderSettings(apiWith(version, update))

    fireEvent.click(await screen.findByRole('button', { name: 'Update to bbbbbbb' }))
    expect(screen.getByRole('dialog')).toBeTruthy()
    expect(update).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(update).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Update to bbbbbbb' }))
    fireEvent.click(screen.getByRole('button', { name: 'Update now' }))

    expect(update).toHaveBeenCalledWith(latest)
    expect(await screen.findByText('The update is running.')).toBeTruthy()
    expect(screen.getByText('building')).toBeTruthy()
  })

  it('stops waiting for a queued update when the host helper does not take it', async () => {
    vi.useFakeTimers()
    const update = vi.fn(() => Promise.resolve({ target: latest }))
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockResolvedValueOnce(makeSystem())
      .mockResolvedValueOnce(makeSystem())
      .mockResolvedValue(makeSystem({ update: { error: 'host_helper_missing' } }))
    renderSettings(apiWith(version, update))

    await act(async () => {})
    fireEvent.click(screen.getByRole('button', { name: 'Update to bbbbbbb' }))
    fireEvent.click(screen.getByRole('button', { name: 'Update now' }))
    await act(async () => {})
    expect(update).toHaveBeenCalledWith(latest)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getByText(/is not installed or is not running/)).toBeTruthy()

    const calls = version.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery * 3)
    })
    expect(version).toHaveBeenCalledTimes(calls)
  })

  it('polls a running update until it succeeds and then stops', async () => {
    vi.useFakeTimers()
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockResolvedValueOnce(makeSystem({ status: { state: 'running', log: ['building'] } }))
      .mockResolvedValueOnce(
        makeSystem({ status: { state: 'running', log: ['building', 'restarting'] } }),
      )
      .mockResolvedValue(
        makeSystem({ version: '1.3.0', status: { state: 'succeeded', log: ['done'] } }),
      )
    renderSettings(apiWith(version))

    await act(async () => {})
    expect(screen.getByText('The update is running.')).toBeTruthy()
    expect(screen.getByText('building')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getByText('restarting')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getByText('Updated. The panel is running 1.3.0.')).toBeTruthy()

    const calls = version.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery * 3)
    })
    expect(version).toHaveBeenCalledTimes(calls)
  })

  it('keeps polling while the core restarts and then shows the success', async () => {
    vi.useFakeTimers()
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockResolvedValueOnce(makeSystem({ status: { state: 'running', log: ['building'] } }))
      .mockRejectedValueOnce(new ApiError('network', 0))
      .mockRejectedValueOnce(new ApiError('update_check_pending', 503))
      .mockResolvedValue(
        makeSystem({ version: '1.3.0', status: { state: 'succeeded', log: ['done'] } }),
      )
    renderSettings(apiWith(version))

    await act(async () => {})
    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getAllByText('The core is restarting. Waiting…').length).toBeGreaterThan(0)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getAllByText('The core is restarting. Waiting…').length).toBeGreaterThan(0)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getByText('Updated. The panel is running 1.3.0.')).toBeTruthy()
  })

  it('resumes the progress from the API after a reload', async () => {
    vi.useFakeTimers()
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockResolvedValueOnce(makeSystem({ status: { state: 'running', log: ['building'] } }))
      .mockResolvedValue(makeSystem({ status: { state: 'running', log: ['almost'] } }))
    renderSettings(apiWith(version))

    await act(async () => {})
    expect(screen.getByText('The update is running.')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(version).toHaveBeenCalledTimes(2)
    expect(screen.getByText('almost')).toBeTruthy()
  })

  it('shows a failed update with the log', async () => {
    renderSettings(
      apiWith(() =>
        Promise.resolve(makeSystem({ status: { state: 'failed', log: ['build failed: boom'] } })),
      ),
    )

    expect(await screen.findByText('The update failed.')).toBeTruthy()
    expect(screen.getByText('build failed: boom')).toBeTruthy()
  })

  it('shows a rolled-back update with the log', async () => {
    renderSettings(
      apiWith(() =>
        Promise.resolve(
          makeSystem({ status: { state: 'rolled_back', log: ['health check failed'] } }),
        ),
      ),
    )

    expect(
      await screen.findByText('The update failed and the previous version was restored.'),
    ).toBeTruthy()
    expect(screen.getByText('health check failed')).toBeTruthy()
  })

  it('retries while the first check has not finished', async () => {
    vi.useFakeTimers()
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockRejectedValueOnce(new ApiError('update_check_pending', 503))
      .mockResolvedValue(
        makeSystem({ update: { latest: current, updateAvailable: false, pending: [] } }),
      )
    renderSettings(apiWith(version))

    await act(async () => {})
    expect(screen.getByText('Checking for updates…')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getByText('Up to date')).toBeTruthy()
  })

  it('shows the error when the first load fails', async () => {
    renderSettings(apiWith(() => Promise.reject(new ApiError('internal', 500))))

    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(screen.getByText('Something went wrong. Try again.')).toBeTruthy()
  })

  it('stops polling when it unmounts', async () => {
    vi.useFakeTimers()
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockResolvedValue(makeSystem({ status: { state: 'running' } }))
    const { unmount } = render(<Settings api={apiWith(version)} />)

    await act(async () => {})
    expect(version).toHaveBeenCalledTimes(1)

    unmount()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery * 3)
    })
    expect(version).toHaveBeenCalledTimes(1)
  })
})
