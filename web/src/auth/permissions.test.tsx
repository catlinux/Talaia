import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import type { MeApi, MePermissions, MeResponse, Permission } from '../api/me'
import { PermissionsProvider, usePermissions } from './permissions'

function meApi(permissions: Partial<MePermissions>): MeApi {
  const body: MeResponse = {
    user: { id: 'u1', username: 'alice', display_name: 'Alice', email: '' },
    bindings: { bindings: [] },
    permissions: {
      organization: [],
      servers: {},
      stacks: {},
      containers: {},
      ...permissions,
    },
  }
  return { get: () => Promise.resolve(body) }
}

function Probe() {
  const { loading, can, canAnyOnServer, canSeeStacks, canSeeContainers } = usePermissions()
  const checks: Permission[] = [
    'server.enroll',
    'server.view',
    'stack.deploy',
    'container.control',
    'audit.view',
  ]
  return (
    <ul>
      <li data-testid="loading">{String(loading)}</li>
      {checks.map((permission) => (
        <li key={permission} data-testid={`org-${permission}`}>
          {String(can(permission))}
        </li>
      ))}
      <li data-testid="server-view">{String(can('server.view', { serverId: 'srv1' }))}</li>
      <li data-testid="stack-deploy">
        {String(can('stack.deploy', { serverId: 'srv1', stack: 'web' }))}
      </li>
      <li data-testid="container-control">
        {String(can('container.control', { serverId: 'srv1', stack: 'web', container: 'web-1' }))}
      </li>
      <li data-testid="any">{String(canAnyOnServer('srv1'))}</li>
      <li data-testid="stacks">{String(canSeeStacks('srv1'))}</li>
      <li data-testid="containers">{String(canSeeContainers('srv1'))}</li>
    </ul>
  )
}

describe('permissions', () => {
  afterEach(() => cleanup())

  it('resolves organization, server, stack and container scopes', async () => {
    render(
      <PermissionsProvider
        meApi={meApi({
          organization: ['server.enroll', 'audit.view'],
          servers: { srv1: ['server.view'] },
          stacks: { srv1: { web: ['stack.view', 'stack.deploy'] } },
          containers: {
            srv1: {
              'web-1': ['container.view', 'container.control'],
              'db-1': ['container.view'],
            },
          },
        })}
      >
        <Probe />
      </PermissionsProvider>,
    )

    await waitFor(() => expect(screen.getByTestId('loading').textContent).toBe('false'))
    expect(screen.getByTestId('org-server.enroll').textContent).toBe('true')
    expect(screen.getByTestId('org-server.view').textContent).toBe('false')
    expect(screen.getByTestId('server-view').textContent).toBe('true')
    expect(screen.getByTestId('stack-deploy').textContent).toBe('true')
    expect(screen.getByTestId('container-control').textContent).toBe('true')
    expect(screen.getByTestId('any').textContent).toBe('true')
    expect(screen.getByTestId('stacks').textContent).toBe('true')
    expect(screen.getByTestId('containers').textContent).toBe('true')
  })

  it('denies everything while loading and after a failed /me', async () => {
    let reject: (error: Error) => void = () => undefined
    const failed: MeApi = {
      get: () =>
        new Promise((_, rejectPromise) => {
          reject = rejectPromise
        }),
    }

    render(
      <PermissionsProvider meApi={failed}>
        <Probe />
      </PermissionsProvider>,
    )

    expect(screen.getByTestId('loading').textContent).toBe('true')
    expect(screen.getByTestId('org-server.enroll').textContent).toBe('false')
    expect(screen.getByTestId('any').textContent).toBe('false')

    reject(new Error('me failed'))
    await waitFor(() => expect(screen.getByTestId('loading').textContent).toBe('false'))
    expect(screen.getByTestId('server-view').textContent).toBe('false')
    expect(screen.getByTestId('stack-deploy').textContent).toBe('false')
    expect(screen.getByTestId('any').textContent).toBe('false')
  })

  it('lets isolated screens keep their historical permissive behaviour', () => {
    render(<Probe />)
    expect(screen.getByTestId('loading').textContent).toBe('false')
    expect(screen.getByTestId('org-server.enroll').textContent).toBe('true')
    expect(screen.getByTestId('container-control').textContent).toBe('true')
  })
})
