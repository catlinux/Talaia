import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { ContainersApi, Job } from '../../api/containers'
import i18n from '../../i18n'
import { LogsPanel, followEvery, maxFollowLines } from './LogsPanel'

function job(output: string, state: Job['state'] = 'succeeded', message = ''): Job {
  return { id: 'job', state, message, output }
}

/**
 * apiWithJobs answers each log read with the next job. Every job finishes at
 * once, so one api.job call per read is enough.
 */
function apiWithJobs(jobs: Job[]): ContainersApi & {
  logs: ReturnType<typeof vi.fn>
  job: ReturnType<typeof vi.fn>
} {
  let next = 0
  return {
    logs: vi.fn(() => Promise.resolve(`job-${next}`)),
    job: vi.fn(() => {
      const answer = jobs[next] ?? jobs[jobs.length - 1]
      next += 1
      return Promise.resolve(answer as Job)
    }),
    list: vi.fn(() => Promise.reject(new Error('unexpected list'))),
    run: vi.fn(() => Promise.reject(new Error('unexpected run'))),
    stacks: vi.fn(() => Promise.reject(new Error('unexpected stacks'))),
    adoptStack: vi.fn(() => Promise.reject(new Error('unexpected adopt'))),
    listResources: vi.fn(() => Promise.reject(new Error('unexpected resources'))),
    removeResources: vi.fn(() => Promise.reject(new Error('unexpected removal'))),
    writeStack: vi.fn(() => Promise.reject(new Error('unexpected write'))),
    readStack: vi.fn(() => Promise.reject(new Error('unexpected read'))),
    runStack: vi.fn(() => Promise.reject(new Error('unexpected stack action'))),
  }
}

function renderPanel(api: ContainersApi) {
  render(<LogsPanel api={api} serverId="srv-1" container="web" pollMs={1} onClose={vi.fn()} />)
}

/** line builds one Docker log line at 10:00:0{second}. */
function line(second: number, message: string): string {
  return `2026-10-20T10:00:0${second}.123456789Z ${message}`
}

/** viewer returns the text of the log box. */
function viewer(): string {
  return screen.getByLabelText('Log lines').textContent ?? ''
}

describe('LogsPanel follow mode', () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18n.changeLanguage('en')
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    cleanup()
  })

  it('shows the last lines and appends only the new ones', async () => {
    const api = apiWithJobs([
      job([line(0, 'first'), line(1, 'second')].join('\n')),
      job([line(1, 'second'), line(2, 'third')].join('\n')),
      job([line(2, 'third'), line(3, 'fourth')].join('\n')),
    ])
    renderPanel(api)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(viewer()).toContain('first')
    expect(viewer()).toContain('second')

    fireEvent.click(screen.getByRole('button', { name: 'Follow' }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    // The boundary line is not repeated.
    expect(viewer().match(/second/g)).toHaveLength(1)
    expect(viewer()).toContain('third')
    expect(screen.getByText('Live')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(followEvery)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(viewer()).toContain('fourth')

    expect(api.logs).toHaveBeenCalledTimes(3)
    expect(api.logs.mock.calls[1]).toEqual([
      'srv-1',
      'web',
      1000,
      { since: '2026-10-20T10:00:01.123456789Z', timestamps: true },
    ])
    expect(api.logs.mock.calls[2]).toEqual([
      'srv-1',
      'web',
      1000,
      { since: '2026-10-20T10:00:02.123456789Z', timestamps: true },
    ])
  })

  it('stops when follow is turned off', async () => {
    const api = apiWithJobs([job(line(0, 'first')), job(line(1, 'second'))])
    renderPanel(api)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    fireEvent.click(screen.getByRole('button', { name: 'Follow' }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(api.logs).toHaveBeenCalledTimes(2)

    fireEvent.click(screen.getByRole('button', { name: 'Follow' }))
    expect(screen.queryByText('Live')).toBeNull()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(followEvery * 2)
    })
    expect(api.logs).toHaveBeenCalledTimes(2)
  })

  it('stops when it unmounts', async () => {
    const api = apiWithJobs([job(line(0, 'first')), job(line(1, 'second'))])
    const { unmount } = render(
      <LogsPanel api={api} serverId="srv-1" container="web" pollMs={1} onClose={vi.fn()} />,
    )
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    fireEvent.click(screen.getByRole('button', { name: 'Follow' }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    const calls = api.logs.mock.calls.length

    unmount()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(followEvery * 2)
    })
    expect(api.logs).toHaveBeenCalledTimes(calls)
  })

  it('stops with the usual message when a read fails', async () => {
    const api = apiWithJobs([job(line(0, 'first')), job('', 'failed', 'docker: No such container')])
    renderPanel(api)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    fireEvent.click(screen.getByRole('button', { name: 'Follow' }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    expect(screen.getByRole('alert').textContent).toBe('Failed: docker: No such container')
    expect(screen.getByRole('button', { name: 'Follow' }).getAttribute('aria-pressed')).toBe(
      'false',
    )
    await act(async () => {
      await vi.advanceTimersByTimeAsync(followEvery * 2)
    })
    expect(api.logs).toHaveBeenCalledTimes(2)
  })

  it('never overlaps two follow requests', async () => {
    const api = apiWithJobs([job(line(0, 'first')), job(line(1, 'second'))])
    renderPanel(api)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    api.logs.mockImplementationOnce(() => new Promise<never>(() => undefined))

    fireEvent.click(screen.getByRole('button', { name: 'Follow' }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(followEvery * 2)
    })

    expect(api.logs).toHaveBeenCalledTimes(2)
  })

  it('keeps at most 2000 lines in memory', async () => {
    const base = Date.parse('2026-10-20T10:00:00Z')
    const initial = `${new Date(base).toISOString()} line 0`
    const burst = Array.from({ length: 2500 }, (_, index) => {
      const at = new Date(base + (index + 1) * 1000).toISOString()
      return `${at} line ${index + 1}`
    }).join('\n')
    const api = apiWithJobs([job(initial), job(burst)])
    renderPanel(api)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    fireEvent.click(screen.getByRole('button', { name: 'Follow' }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    const text = viewer()
    const messages = text.split('\n')
    expect(messages).toHaveLength(maxFollowLines)
    expect(messages[0]).toMatch(/line 501$/)
    expect(messages[messages.length - 1]).toMatch(/line 2500$/)
  })
})
