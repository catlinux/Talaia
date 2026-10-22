import { describe, expect, it, vi } from 'vitest'

import { createContainersApi } from './containers'

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('createContainersApi', () => {
  it('maps the container list', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          connected: true,
          docker_available: true,
          containers: [
            {
              id: 'aaaaaaaaaaaa',
              name: 'web',
              image: 'nginx:1.27',
              state: 'running',
              status: 'Up 2 hours',
              compose_project: 'shop',
              compose_service: 'web',
              use: {
                cpu_percent: 12.5,
                mem_used_bytes: 1000,
                mem_limit_bytes: 4000,
                net_rx_bytes: 7,
                net_tx_bytes: 9,
              },
            },
          ],
        }),
      ),
    )

    const list = await createContainersApi(fetchImpl).list('srv 1')

    expect(list).toEqual({
      connected: true,
      dockerAvailable: true,
      containers: [
        {
          id: 'aaaaaaaaaaaa',
          name: 'web',
          image: 'nginx:1.27',
          state: 'running',
          status: 'Up 2 hours',
          composeProject: 'shop',
          composeService: 'web',
          use: {
            cpuPercent: 12.5,
            memUsedBytes: 1000,
            memLimitBytes: 4000,
            netRxBytes: 7,
            netTxBytes: 9,
          },
        },
      ],
    })
    // The server id is escaped in the path.
    expect((fetchImpl.mock.calls[0] as unknown as [string])[0]).toBe(
      '/api/v1/servers/srv%201/containers',
    )
  })

  it('sends an action and returns the job id', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(jsonResponse({ job_id: 'job-9' }, 202)))

    const jobId = await createContainersApi(fetchImpl).run('srv-1', 'web', 'remove', true)

    expect(jobId).toBe('job-9')
    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/servers/srv-1/containers/web/actions')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ action: 'remove', confirm: true }))
  })

  it('reads a job', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        jsonResponse({ id: 'job-9', state: 'failed', message: 'boom', output: 'tail', extra: 1 }),
      ),
    )

    await expect(createContainersApi(fetchImpl).job('job-9')).resolves.toEqual({
      id: 'job-9',
      state: 'failed',
      message: 'boom',
      output: 'tail',
    })
  })

  it('asks for the logs of a container', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(jsonResponse({ job_id: 'job-3' }, 202)))

    const jobId = await createContainersApi(fetchImpl).logs('srv-1', 'web', 50)

    expect(jobId).toBe('job-3')
    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/servers/srv-1/containers/web/logs')
    expect(init.body).toBe(JSON.stringify({ tail: 50 }))
  })

  it('maps the broken stacks of the stack list', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          connected: true,
          stacks: ['blog', 'wiki'],
          adopted: ['wiki'],
          broken: ['wiki'],
          adoptable: ['shop'],
        }),
      ),
    )

    await expect(createContainersApi(fetchImpl).stacks('srv-1')).resolves.toEqual({
      connected: true,
      stacks: ['blog', 'wiki'],
      adopted: ['wiki'],
      broken: ['wiki'],
      adoptable: ['shop'],
    })
  })

  it('asks for the logs after a timestamp, with timestamps', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(jsonResponse({ job_id: 'job-4' }, 202)))
    const since = '2026-10-20T10:00:00.123456789Z'

    await createContainersApi(fetchImpl).logs('srv-1', 'web', 1000, {
      since,
      timestamps: true,
    })

    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/servers/srv-1/containers/web/logs')
    expect(init.body).toBe(JSON.stringify({ tail: 1000, since, timestamps: true }))
  })
})
