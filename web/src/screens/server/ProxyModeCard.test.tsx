import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { ProxyStatus, WizardApi } from '../../api/apps'
import type { MeApi, Permission } from '../../api/me'
import { PermissionsProvider } from '../../auth/permissions'
import i18n from '../../i18n'
import { ProxyModeCard } from './ProxyModeCard'

function meApi(permissions: Permission[]): MeApi {
  return {
    get: () =>
      Promise.resolve({
        user: { id: 'u1', username: 'admin', display_name: 'Admin', email: '' },
        bindings: { bindings: [] },
        permissions: {
          organization: permissions,
          servers: {},
          stacks: {},
          containers: {},
        },
      }),
  }
}

function fakeApi(status: ProxyStatus | Error): WizardApi {
  const unexpected = () => Promise.reject(new Error('unexpected call'))
  return {
    list: () => Promise.resolve([]),
    listByServer: () => Promise.resolve([]),
    get: unexpected,
    delete: unexpected,
    variables: unexpected,
    setVariable: unexpected,
    deleteVariable: unexpected,
    deployments: unexpected,
    deployment: unexpected,
    deploy: unexpected,
    rollback: unexpected,
    templates: () => Promise.resolve([]),
    create: unexpected,
    appDomains: () => Promise.resolve([]),
    addDomain: unexpected,
    proxy: vi.fn(() =>
      status instanceof Error ? Promise.reject(status) : Promise.resolve(status),
    ),
    proxyAction: vi.fn(() => Promise.resolve()),
  }
}

function renderCard(api: WizardApi, permissions: Permission[] = ['server.proxy']) {
  render(
    <PermissionsProvider meApi={meApi(permissions)}>
      <ProxyModeCard api={api} serverId="srv-1" />
    </PermissionsProvider>,
  )
}

const external: ProxyStatus = {
  mode: 'external',
  domains: [],
  port80Free: true,
  port443Free: true,
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('proxy mode card', () => {
  it('installs the managed proxy only when ports 80 and 443 are free', async () => {
    const api = fakeApi(external)
    renderCard(api)

    expect(await screen.findByText('External proxy')).toBeTruthy()
    fireEvent.click(await screen.findByRole('button', { name: 'Install managed proxy' }))
    await waitFor(() => expect(api.proxyAction).toHaveBeenCalledWith('srv-1', 'install'))
    expect(await screen.findByText(/Proxy action queued/)).toBeTruthy()
  })

  it('hides the managed install when a port is busy or unknown', async () => {
    renderCard(fakeApi({ ...external, port443Free: false }))
    expect(await screen.findByText(/Port 80 or 443 is already in use/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Install managed proxy' })).toBeNull()

    cleanup()
    renderCard(fakeApi({ mode: 'external', domains: [] }))
    expect(await screen.findByText(/has not reported whether ports 80 and 443/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Install managed proxy' })).toBeNull()
  })

  it('applies and removes routes in managed mode', async () => {
    const api = fakeApi({
      mode: 'managed',
      domains: [],
      caddyVersion: '2.8.4',
      certificates: [{ hostname: 'shop.example.com', state: 'valid' }],
    })
    renderCard(api)

    expect(await screen.findByText('Managed proxy (Caddy)')).toBeTruthy()
    expect(screen.getByText('Caddy 2.8.4')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Apply routes' }))
    await waitFor(() => expect(api.proxyAction).toHaveBeenCalledWith('srv-1', 'apply'))

    fireEvent.click(screen.getByRole('button', { name: 'Remove managed proxy' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove the proxy' }))
    await waitFor(() => expect(api.proxyAction).toHaveBeenCalledWith('srv-1', 'remove'))
  })

  it('stays hidden without server.proxy', async () => {
    renderCard(fakeApi(external), [])
    await waitFor(() => expect(screen.queryByText('App publishing')).toBeNull())
  })
})
