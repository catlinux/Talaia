import { describe, expect, it, vi } from 'vitest'

import { createServicesApi, parseServiceState } from './services'

describe('services API', () => {
  it('lists and maps the reported services', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            services: [
              {
                name: 'docker.service',
                description: 'Docker',
                load_state: 'loaded',
                active_state: 'active',
                sub_state: 'running',
                unit_file_state: 'enabled',
                can_reload: true,
              },
            ],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    )
    const api = createServicesApi(fetchImpl as unknown as typeof fetch)

    await expect(api.list('srv-1')).resolves.toEqual([
      {
        name: 'docker.service',
        description: 'Docker',
        loadState: 'loaded',
        activeState: 'active',
        subState: 'running',
        unitFileState: 'enabled',
        canReload: true,
      },
    ])

    const [path] = fetchImpl.mock.calls[0] as unknown as [string]
    expect(path).toBe('/api/v1/servers/srv-1/services')
  })

  it('submits an action with its confirmation', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ job_id: 'job-1' }), {
          status: 202,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    const api = createServicesApi(fetchImpl as unknown as typeof fetch)

    await expect(api.action('srv-1', 'docker.service', 'stop', true)).resolves.toBe('job-1')

    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/servers/srv-1/services/docker.service/actions')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ action: 'stop', confirm: true }))
  })

  it('reads the job and parses its state output', async () => {
    const output = {
      unit: 'docker.service',
      active_state: 'inactive',
      sub_state: 'dead',
      unit_file_state: 'disabled',
      load_state: 'loaded',
    }
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            id: 'job-2',
            state: 'succeeded',
            message: '',
            output: JSON.stringify(output),
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    )
    const api = createServicesApi(fetchImpl as unknown as typeof fetch)

    const job = await api.job('job-2')

    expect(job.state).toBe('succeeded')
    expect(parseServiceState(job.output)).toEqual({
      unit: 'docker.service',
      activeState: 'inactive',
      subState: 'dead',
      unitFileState: 'disabled',
      loadState: 'loaded',
    })
  })
})
