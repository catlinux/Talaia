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
})
