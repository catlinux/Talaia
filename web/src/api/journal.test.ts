import { describe, expect, it, vi } from 'vitest'

import { createJournalApi, parseJournalPage } from './journal'

describe('journal API', () => {
  it('queues a journal read with every filter', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ job_id: 'job-1' }), {
          status: 202,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    const api = createJournalApi(fetchImpl as unknown as typeof fetch)

    await expect(
      api.read('srv-1', {
        unit: 'docker.service',
        maxPriority: 3,
        since: '2026-10-20T10:00:00Z',
        until: '2026-10-20T11:00:00Z',
        search: 'needle',
        boot: 'current',
        tail: 500,
        afterCursor: 's=abc;def',
      }),
    ).resolves.toBe('job-1')

    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/servers/srv-1/journal')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(
      JSON.stringify({
        unit: 'docker.service',
        max_priority: 3,
        since: '2026-10-20T10:00:00Z',
        until: '2026-10-20T11:00:00Z',
        search: 'needle',
        boot: 'current',
        tail: 500,
        after_cursor: 's=abc;def',
      }),
    )
  })

  it('uses an empty filter set and a null priority by default', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ job_id: 'job-2' }), {
          status: 202,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    const api = createJournalApi(fetchImpl as unknown as typeof fetch)

    await api.read('srv-2')

    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/servers/srv-2/journal')
    expect(init.body).toBe(
      JSON.stringify({
        unit: '',
        max_priority: null,
        since: '',
        until: '',
        search: '',
        boot: 'all',
        tail: 0,
        after_cursor: '',
      }),
    )
  })

  it('reads the job and parses its page output', async () => {
    const output = {
      entries: [
        {
          ts: '2026-10-20T10:00:00.123456Z',
          unit: 'docker.service',
          priority: 3,
          pid: '42',
          message: 'hello',
        },
        {
          ts: '2026-10-20T10:00:01Z',
          unit: 'nginx.service',
          priority: 4,
          pid: '7',
          message: 'second',
        },
      ],
      cursor: 's=next',
    }
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            id: 'job-3',
            state: 'succeeded',
            message: '',
            output: JSON.stringify(output),
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    )
    const api = createJournalApi(fetchImpl as unknown as typeof fetch)

    const job = await api.job('job-3')

    expect(job.state).toBe('succeeded')
    expect(parseJournalPage(job.output)).toEqual(output)
  })

  it('fills the gaps of a short page', () => {
    expect(parseJournalPage(JSON.stringify({ entries: [{}] }))).toEqual({
      entries: [{ ts: '', unit: '', priority: 6, pid: '', message: '' }],
      cursor: '',
    })
  })
})
