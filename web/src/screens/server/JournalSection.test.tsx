import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Job } from '../../api/containers'
import type { JournalApi, JournalEntry } from '../../api/journal'
import type { ServiceUnit, ServicesApi } from '../../api/services'
import i18n from '../../i18n'
import { JournalSection, followEvery, maxFollowLines } from './JournalSection'

function job(output: string, state: Job['state'] = 'succeeded', message = ''): Job {
  return { id: 'job', state, message, output }
}

function entry(second: number, message: string, extra: Partial<JournalEntry> = {}): JournalEntry {
  return {
    ts: `2026-10-20T10:00:${String(second).padStart(2, '0')}Z`,
    unit: 'docker.service',
    priority: 6,
    pid: '42',
    message,
    ...extra,
  }
}

function page(entries: JournalEntry[], cursor = ''): string {
  return JSON.stringify({ entries, cursor })
}

function apiWithPages(pages: Job[]): JournalApi & {
  read: ReturnType<typeof vi.fn>
  job: ReturnType<typeof vi.fn>
} {
  let next = 0
  return {
    read: vi.fn(() => Promise.resolve(`job-${next}`)),
    job: vi.fn(() => {
      const answer = pages[next] ?? pages[pages.length - 1]
      next += 1
      return Promise.resolve(answer as Job)
    }),
  }
}

function renderSection(api: JournalApi, servicesApi?: ServicesApi) {
  render(<JournalSection api={api} servicesApi={servicesApi} serverId="srv-1" pollMs={1} />)
}

function viewer(): HTMLElement {
  return screen.getByLabelText('Journal entries')
}

describe('journal section', () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18n.changeLanguage('en')
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-22T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
    cleanup()
  })

  it('loads the last 200 entries of all boots on mount', async () => {
    const api = apiWithPages([job(page([entry(0, 'first'), entry(1, 'second')], 'c1'))])
    renderSection(api)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    expect(api.read).toHaveBeenCalledWith('srv-1', {
      unit: '',
      maxPriority: null,
      since: '',
      until: '',
      search: '',
      boot: 'all',
      tail: 200,
      afterCursor: '',
    })
    expect(viewer().textContent).toContain('first')
    expect(viewer().textContent).toContain('second')
    expect(within(viewer()).getAllByRole('listitem')).toHaveLength(2)
  })

  it('builds the request from every control', async () => {
    const api = apiWithPages([
      job(page([entry(0, 'first')], 'c1')),
      job(page([entry(2, 'needle')], 'c2')),
    ])
    renderSection(api)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    fireEvent.change(screen.getByLabelText('Unit'), { target: { value: 'docker.service' } })
    fireEvent.change(screen.getByLabelText('Priority'), { target: { value: '3' } })
    fireEvent.change(screen.getByLabelText('Boot'), { target: { value: 'current' } })
    fireEvent.change(screen.getByLabelText('Time range'), { target: { value: '15m' } })
    fireEvent.change(screen.getByLabelText('Search'), { target: { value: 'needle' } })
    fireEvent.change(screen.getByLabelText('Lines'), { target: { value: '500' } })
    const since = new Date(Date.now() - 15 * 60_000).toISOString()
    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    expect(api.read).toHaveBeenLastCalledWith('srv-1', {
      unit: 'docker.service',
      maxPriority: 3,
      since,
      until: '',
      search: 'needle',
      boot: 'current',
      tail: 500,
      afterCursor: '',
    })
    expect(viewer().textContent).toContain('needle')
  })

  it('loads the service names into the unit datalist', async () => {
    const unit: ServiceUnit = {
      name: 'docker.service',
      description: 'Docker',
      loadState: 'loaded',
      activeState: 'active',
      subState: 'running',
      unitFileState: 'enabled',
      canReload: true,
    }
    const servicesApi: ServicesApi = {
      list: vi.fn(() => Promise.resolve([unit])),
      action: () => Promise.reject(new Error('unexpected service action')),
      job: () => Promise.reject(new Error('unexpected service job')),
    }
    const api = apiWithPages([job(page([entry(0, 'first')]))])
    renderSection(api, servicesApi)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
      await Promise.resolve()
    })

    const options = document.querySelectorAll<HTMLOptionElement>('#journal-units option')
    expect(Array.from(options).map((option) => option.value)).toEqual(['docker.service'])
  })

  it('rejects a custom range whose end is not after its start', async () => {
    const api = apiWithPages([job(page([], '')), job(page([], ''))])
    renderSection(api)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    fireEvent.change(screen.getByLabelText('Time range'), { target: { value: 'custom' } })
    fireEvent.change(screen.getByLabelText('From'), {
      target: { value: '2026-10-22T10:00' },
    })
    fireEvent.change(screen.getByLabelText('To'), {
      target: { value: '2026-10-22T09:00' },
    })
    const calls = api.read.mock.calls.length
    fireEvent.click(screen.getByRole('button', { name: 'Search' }))

    expect(screen.getByRole('alert').textContent).toBe(
      'The end of the range must be after its start.',
    )
    expect(api.read).toHaveBeenCalledTimes(calls)

    fireEvent.change(screen.getByLabelText('From'), {
      target: { value: '2026-10-22T09:00' },
    })
    fireEvent.change(screen.getByLabelText('To'), {
      target: { value: '2026-10-22T10:00' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    expect(api.read).toHaveBeenLastCalledWith('srv-1', {
      unit: '',
      maxPriority: null,
      since: new Date('2026-10-22T09:00').toISOString(),
      until: new Date('2026-10-22T10:00').toISOString(),
      search: '',
      boot: 'all',
      tail: 200,
      afterCursor: '',
    })
    expect((screen.getByRole('button', { name: 'Follow' }) as HTMLButtonElement).disabled).toBe(
      true,
    )
  })

  it('follows from the current cursor and appends new entries', async () => {
    const api = apiWithPages([
      job(page([entry(0, 'first')], 'c1')),
      job(page([entry(1, 'second')], 'c2')),
      job(page([entry(2, 'third')], 'c3')),
    ])
    renderSection(api)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    fireEvent.click(screen.getByRole('button', { name: 'Follow' }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    expect(api.read).toHaveBeenLastCalledWith('srv-1', {
      unit: '',
      maxPriority: null,
      since: '',
      until: '',
      search: '',
      boot: 'all',
      tail: 200,
      afterCursor: 'c1',
    })
    expect(viewer().textContent).toContain('second')
    expect(screen.getByText('Live')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(followEvery)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    expect(api.read).toHaveBeenLastCalledWith('srv-1', {
      unit: '',
      maxPriority: null,
      since: '',
      until: '',
      search: '',
      boot: 'all',
      tail: 200,
      afterCursor: 'c2',
    })
    expect(viewer().textContent).toContain('third')
    expect(api.read).toHaveBeenCalledTimes(3)
  })

  it('keeps at most 2000 entries while following', async () => {
    const base = Date.parse('2026-10-20T10:00:00Z')
    const initial = entry(0, 'line 0')
    const burst = Array.from({ length: 2500 }, (_, index) =>
      entry(0, `line ${index + 1}`, { ts: new Date(base + (index + 1) * 1000).toISOString() }),
    )
    const api = apiWithPages([job(page([initial], 'c1')), job(page(burst, 'c2'))])
    renderSection(api)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    fireEvent.click(screen.getByRole('button', { name: 'Follow' }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    const items = within(viewer()).getAllByRole('listitem')
    expect(items).toHaveLength(maxFollowLines)
    expect(items[0]?.textContent).toContain('line 501')
    expect(items[items.length - 1]?.textContent).toContain('line 2500')
  })

  it('highlights literal search matches case-insensitively', async () => {
    const message = 'Error ERROR error a.b'
    const api = apiWithPages([
      job(page([entry(0, message)], 'c1')),
      job(page([entry(0, message)], 'c2')),
    ])
    renderSection(api)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    fireEvent.change(screen.getByLabelText('Search'), { target: { value: 'error' } })
    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    const marks = viewer().querySelectorAll('mark')
    expect(Array.from(marks).map((mark) => mark.textContent)).toEqual(['Error', 'ERROR', 'error'])
  })

  it('downloads the loaded lines as text', async () => {
    const api = apiWithPages([job(page([entry(0, 'first')], 'c1'))])
    renderSection(api)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    const create = vi.fn((blob: Blob) => {
      void blob
      return 'blob:journal'
    })
    const revoke = vi.fn()
    const previousCreate = URL.createObjectURL
    const previousRevoke = URL.revokeObjectURL
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: create })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revoke })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    try {
      fireEvent.click(screen.getByRole('button', { name: 'Download' }))
      expect(create).toHaveBeenCalledTimes(1)
      const blob = create.mock.calls[0]?.[0] as Blob
      expect(await blob.text()).toContain('first')
      expect(click).toHaveBeenCalledTimes(1)
      expect(revoke).toHaveBeenCalledWith('blob:journal')
    } finally {
      click.mockRestore()
      Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: previousCreate })
      Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: previousRevoke })
    }
  })

  it('shows the empty state and a readable error', async () => {
    const empty = apiWithPages([job(page([], ''))])
    renderSection(empty)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(screen.getByText('No entries for these filters.')).toBeTruthy()
    cleanup()

    const failing = apiWithPages([job('', 'failed', 'journalctl: permission denied')])
    renderSection(failing)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(screen.getByRole('alert').textContent).toBe(
      'The journal could not be read: journalctl: permission denied',
    )
  })
})
