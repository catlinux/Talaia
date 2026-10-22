import { describe, expect, it, vi } from 'vitest'

import { createAptApi, parseAptStatus } from './apt'

const status = {
  packages: [
    {
      name: 'openssl',
      current: '3.0.15-1',
      candidate: '3.0.16-1',
      origin: 'Debian-Security:12/stable-security',
      security: true,
    },
  ],
  held: ['linux-image-amd64'],
  reboot_required: true,
  reboot_reasons: ['A newer kernel is installed.'],
  lists_updated_at: '2026-10-22T08:00:00Z',
  checked_at: '2026-10-22T09:00:00Z',
}

describe('apt API', () => {
  it('queues a status job without refreshing the package lists', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ job_id: 'job-1' }), {
          status: 202,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    const api = createAptApi(fetchImpl as unknown as typeof fetch)

    await expect(api.status('srv-1')).resolves.toBe('job-1')

    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/servers/srv-1/apt/status')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ refresh: false }))
  })

  it('asks for fresh package lists when refreshing', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ job_id: 'job-2' }), {
          status: 202,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    const api = createAptApi(fetchImpl as unknown as typeof fetch)

    await expect(api.status('srv-2', true)).resolves.toBe('job-2')

    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/servers/srv-2/apt/status')
    expect(init.body).toBe(JSON.stringify({ refresh: true }))
  })

  it('reads and parses the job output', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            id: 'job-1',
            state: 'succeeded',
            message: '',
            output: JSON.stringify(status),
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    )
    const api = createAptApi(fetchImpl as unknown as typeof fetch)

    const job = await api.job('job-1')

    expect(job.state).toBe('succeeded')
    expect(parseAptStatus(job.output)).toEqual({
      packages: [
        {
          name: 'openssl',
          current: '3.0.15-1',
          candidate: '3.0.16-1',
          origin: 'Debian-Security:12/stable-security',
          security: true,
        },
      ],
      held: ['linux-image-amd64'],
      rebootRequired: true,
      rebootReasons: ['A newer kernel is installed.'],
      listsUpdatedAt: '2026-10-22T08:00:00Z',
      checkedAt: '2026-10-22T09:00:00Z',
    })
  })

  it('accepts a new package without an installed version', () => {
    const parsed = parseAptStatus(
      JSON.stringify({
        packages: [{ name: 'linux-image-6.12', candidate: '6.12.9-1', origin: 'Debian:13/stable' }],
        held: [],
        reboot_required: false,
        reboot_reasons: [],
        lists_updated_at: '',
        checked_at: '2026-10-22T09:00:00Z',
      }),
    )

    expect(parsed.packages[0]).toEqual({
      name: 'linux-image-6.12',
      current: '',
      candidate: '6.12.9-1',
      origin: 'Debian:13/stable',
      security: false,
    })
  })
})
