import { describe, expect, it, vi } from 'vitest'

import { createAppsApi, type AppInput } from './apps'

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

const createInput: AppInput = {
  stackName: 'shop',
  source: 'git',
  repoUrl: 'https://github.com/example/shop.git',
  branch: 'main',
  build: 'dockerfile',
  dockerfilePath: 'Dockerfile',
  buildContext: '.',
  image: '',
  service: 'app',
  port: 3000,
  autoDeploy: true,
  pollEnabled: false,
  zeroDowntime: true,
  templateId: '',
  templateVersion: '',
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

  it('maps the template catalog', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        json([
          {
            id: 'uptime-kuma',
            version: '1.0.0',
            names: { en: 'Uptime Kuma' },
            descriptions: { en: 'Monitor' },
            image: 'louislam/uptime-kuma:1.23.16',
            service: 'app',
            port: 3001,
            ram_mb: 512,
          },
        ]),
      ),
    )
    const api = createAppsApi(fetchImpl as unknown as typeof fetch)
    await expect(api.templates()).resolves.toEqual([
      expect.objectContaining({ id: 'uptime-kuma', port: 3001, ramMB: 512 }),
    ])
    expect(fetchImpl).toHaveBeenCalledWith('/api/v1/templates', expect.any(Object))
  })

  it('creates an app with one-time Git secrets', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        json({
          ...app,
          deploy_key_public: 'ssh-ed25519 AAAA example',
          webhook_secret: 'hook-secret',
          webhook_url: 'https://panel.example.com/api/v1/hooks/git/app-1',
        }),
      ),
    )
    const api = createAppsApi(fetchImpl as unknown as typeof fetch)
    const created = await api.create('srv-1', createInput)
    expect(created).toEqual(
      expect.objectContaining({
        id: 'app-1',
        deployKeyPublic: 'ssh-ed25519 AAAA example',
        webhookSecret: 'hook-secret',
        webhookUrl: 'https://panel.example.com/api/v1/hooks/git/app-1',
        credentialUser: '',
        credentialPassword: '',
      }),
    )
    expect(fetchImpl).toHaveBeenLastCalledWith(
      '/api/v1/servers/srv-1/apps',
      expect.objectContaining({
        method: 'POST',
        body: expect.stringContaining('"stack_name":"shop"'),
      }),
    )
    expect(fetchImpl).toHaveBeenLastCalledWith(
      '/api/v1/servers/srv-1/apps',
      expect.objectContaining({
        body: expect.stringContaining('"repo_url":"https://github.com/example/shop.git"'),
      }),
    )
  })

  it('maps the first-login credential of a template app', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        json({
          ...app,
          source: 'template' as const,
          template_id: 'miniflux',
          template_version: '1.0.0',
          image: 'miniflux/miniflux:2.3.4',
          port: 8080,
          credential_user: 'admin',
          credential_password: 'news-password',
        }),
      ),
    )
    const api = createAppsApi(fetchImpl as unknown as typeof fetch)
    const created = await api.create('srv-1', {
      ...createInput,
      source: 'template',
      templateId: 'miniflux',
      templateVersion: '1.0.0',
    })
    expect(created).toEqual(
      expect.objectContaining({
        credentialUser: 'admin',
        credentialPassword: 'news-password',
      }),
    )
  })

  it('adds and lists app domains', async () => {
    const domain = {
      id: 'domain-1',
      app_id: 'app-1',
      hostname: 'shop.example.com',
      service: 'app',
      port: 3000,
      created_at: '2026-10-31T12:00:00Z',
      warning: 'the hostname does not resolve',
    }
    const fetchImpl = vi.fn((_input: RequestInfo | URL, init?: RequestInit) =>
      Promise.resolve(init?.method === 'POST' ? json(domain) : json([domain])),
    )
    const api = createAppsApi(fetchImpl as unknown as typeof fetch)
    await expect(
      api.addDomain('app-1', { hostname: 'shop.example.com', service: 'app', port: 3000 }),
    ).resolves.toEqual(
      expect.objectContaining({
        hostname: 'shop.example.com',
        warning: 'the hostname does not resolve',
      }),
    )
    await expect(api.appDomains('app-1')).resolves.toEqual(
      expect.arrayContaining([expect.objectContaining({ hostname: 'shop.example.com' })]),
    )
    expect(fetchImpl).toHaveBeenLastCalledWith(
      '/api/v1/apps/app-1/domains',
      expect.objectContaining({ method: 'GET' }),
    )
  })

  it('reads external proxy blocks', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        json({
          mode: 'external',
          domains: [
            {
              hostname: 'shop.example.com',
              app_id: 'app-1',
              host_port: 23456,
              apache: '<VirtualHost *:80>',
              nginx: 'server {}',
              certbot: 'certbot --apache',
            },
          ],
        }),
      ),
    )
    const api = createAppsApi(fetchImpl as unknown as typeof fetch)
    await expect(api.proxy('srv-1')).resolves.toEqual({
      mode: 'external',
      domains: [
        expect.objectContaining({
          hostname: 'shop.example.com',
          hostPort: 23456,
          apache: '<VirtualHost *:80>',
        }),
      ],
    })
    expect(fetchImpl).toHaveBeenLastCalledWith('/api/v1/servers/srv-1/proxy', expect.any(Object))
  })

  it('reads proxy port and certificate state and sends actions', async () => {
    const fetchImpl = vi.fn((input: RequestInfo | URL) => {
      if (String(input) === '/api/v1/servers/srv-1/proxy') {
        return Promise.resolve(
          json({
            mode: 'managed',
            domains: [],
            port_80_free: true,
            port_443_free: true,
            caddy_version: '2.8.4',
            certificates: [{ hostname: 'shop.example.com', state: 'valid' }],
          }),
        )
      }
      return Promise.resolve(new Response(null, { status: 202 }))
    })
    const api = createAppsApi(fetchImpl as unknown as typeof fetch)
    await expect(api.proxy('srv-1')).resolves.toEqual(
      expect.objectContaining({
        mode: 'managed',
        port80Free: true,
        port443Free: true,
        caddyVersion: '2.8.4',
        certificates: [{ hostname: 'shop.example.com', state: 'valid' }],
      }),
    )
    await api.proxyAction('srv-1', 'apply')
    expect(fetchImpl).toHaveBeenLastCalledWith(
      '/api/v1/servers/srv-1/proxy',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ action: 'apply' }) }),
    )
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

  it('deletes an app with the confirmation name', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(json({ output: 'removed image talaia/shop:abc' })),
    )
    const api = createAppsApi(fetchImpl as unknown as typeof fetch)
    await expect(api.delete('app-1', 'shop')).resolves.toEqual({
      output: 'removed image talaia/shop:abc',
    })
    expect(fetchImpl).toHaveBeenLastCalledWith(
      '/api/v1/apps/app-1',
      expect.objectContaining({
        method: 'DELETE',
        body: JSON.stringify({ confirm_name: 'shop' }),
      }),
    )
  })
})
