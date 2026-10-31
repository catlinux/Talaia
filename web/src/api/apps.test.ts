import { describe, expect, it, vi } from 'vitest'

import { createAppsApi } from './apps'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const app = {
  id: 'app-1',
  server_id: 'srv-1',
  stack_name: 'shop',
  source: 'git' as const,
  repo_url: 'https://github.com/example/shop.git',
  branch: 'main',
  build: 'dockerfile',
  dockerfile_path: 'Dockerfile',
  build_context: '.',
  image: 'talaia/shop:abcdef123456',
  service: 'app',
  port: 3000,
  host_port: 23456,
  auto_deploy: true,
  poll_enabled: false,
  zero_downtime: true,
  template_id: '',
  template_version: '',
  created_at: '2026-10-31T12:00:00Z',
  updated_at: '2026-10-31T12:00:00Z',
  updated_by: 'alice',
}

describe('api apps client', () => {
  it('lists and maps apps', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(json([app])))
    const api = createAppsApi(fetchImpl as unknown as typeof fetch)
    await expect(api.list()).resolves.toEqual([
      expect.objectContaining({
        id: 'app-1',
        serverId: 'srv-1',
        stackName: 'shop',
        hostPort: 23456,
        zeroDowntime: true,
      }),
    ])
    expect(fetchImpl).toHaveBeenCalledWith('/api/v1/apps', expect.any(Object))
  })

  it('maps secret variables without a value', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        json([
          { key: 'TOKEN', secret: true, build_time: false },
          { key: 'PUBLIC', value: 'yes', secret: false, build_time: true },
        ]),
      ),
    )
    const api = createAppsApi(fetchImpl as unknown as typeof fetch)
    await expect(api.variables('app/1')).resolves.toEqual([
      { key: 'TOKEN', secret: true, buildTime: false },
      { key: 'PUBLIC', value: 'yes', secret: false, buildTime: true },
    ])
    expect(fetchImpl).toHaveBeenCalledWith('/api/v1/apps/app%2F1/env', expect.any(Object))
  })

  it('sends variables, deployments and rollbacks to the right paths', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(new Response(null, { status: 204 })))
    const api = createAppsApi(fetchImpl as unknown as typeof fetch)
    await api.setVariable('app-1', 'TOKEN', { value: 'a b', secret: true, buildTime: false })
    expect(fetchImpl).toHaveBeenLastCalledWith(
      '/api/v1/apps/app-1/env/TOKEN',
      expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({ value: 'a b', secret: true, build_time: false }),
      }),
    )
    await api.deploy('app-1', 'abcdef1234567890abcdef1234567890abcdef12')
    expect(fetchImpl).toHaveBeenLastCalledWith(
      '/api/v1/apps/app-1/deployments',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ commit: 'abcdef1234567890abcdef1234567890abcdef12' }),
      }),
    )
    await api.rollback('app-1', 'dep-1')
    expect(fetchImpl).toHaveBeenLastCalledWith(
      '/api/v1/apps/app-1/deployments/dep-1/rollback',
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('maps deployment logs', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        json({
          id: 'dep-1',
          app_id: 'app-1',
          number: 1,
          trigger: 'push',
          commit_sha: 'abcdef',
          commit_subject: 'first',
          image_ref: 'talaia/shop:abcdef123456',
          state: 'succeeded',
          started_at: '',
          finished_at: '',
          error: '',
          log: 'build output',
        }),
      ),
    )
    const api = createAppsApi(fetchImpl as unknown as typeof fetch)
    await expect(api.deployment('app-1', 'dep-1')).resolves.toEqual(
      expect.objectContaining({ id: 'dep-1', log: 'build output', state: 'succeeded' }),
    )
  })
})
