import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { TemplateSummary, WizardApi } from '../api/apps'
import type { MeApi, Permission } from '../api/me'
import { PermissionsProvider } from '../auth/permissions'
import i18n from '../i18n'
import { RouterProvider } from '../router'
import { Templates } from './Templates'

const templates: TemplateSummary[] = [
  {
    id: 'uptime-kuma',
    version: '1.0.0',
    names: { en: 'Uptime Kuma', es: 'Uptime Kuma', ca: 'Uptime Kuma' },
    descriptions: { en: 'Uptime monitor', es: 'Monitor', ca: 'Monitor' },
    image: 'louislam/uptime-kuma:1.23.16',
    service: 'app',
    port: 3001,
    ramMB: 512,
  },
  {
    id: 'redis',
    version: '1.0.0',
    names: { en: 'Redis', es: 'Redis', ca: 'Redis' },
    descriptions: { en: 'Cache', es: 'Caché', ca: 'Memòria cau' },
    image: 'redis:7.4.2-alpine',
    service: 'app',
    port: 6379,
    ramMB: 128,
  },
]

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

function fakeApi(): WizardApi {
  const unexpected = () => Promise.reject(new Error('unexpected call'))
  return {
    list: () => Promise.resolve([]),
    listByServer: () => Promise.resolve([]),
    get: unexpected,
    variables: unexpected,
    setVariable: unexpected,
    deleteVariable: unexpected,
    deployments: unexpected,
    deployment: unexpected,
    deploy: unexpected,
    rollback: unexpected,
    templates: vi.fn(() => Promise.resolve(templates)),
    create: unexpected,
    appDomains: () => Promise.resolve([]),
    addDomain: unexpected,
    proxy: () => Promise.resolve({ mode: 'external', domains: [] }),
    proxyAction: unexpected,
  }
}

function renderTemplates(api: WizardApi, permissions: Permission[] = ['stack.view', 'stack.edit']) {
  window.history.replaceState({}, '', '/templates')
  render(
    <RouterProvider>
      <PermissionsProvider meApi={meApi(permissions)}>
        <Templates api={api} />
      </PermissionsProvider>
    </RouterProvider>,
  )
}

beforeEach(async () => {
  localStorage.clear()
  await i18n.changeLanguage('en')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('templates gallery', () => {
  it('shows templates and opens the wizard prefilled', async () => {
    renderTemplates(fakeApi())

    expect(await screen.findByRole('heading', { name: 'Uptime Kuma' })).toBeTruthy()
    fireEvent.click(screen.getAllByRole('button', { name: 'Use template' })[0]!)
    expect(window.location.pathname).toBe('/apps/new')
    expect(window.location.search).toBe('?template=uptime-kuma')
  })

  it('filters the gallery', async () => {
    renderTemplates(fakeApi())
    expect(await screen.findByRole('heading', { name: 'Redis' })).toBeTruthy()
    fireEvent.change(screen.getByPlaceholderText('Search by template, image or description'), {
      target: { value: 'redis' },
    })
    expect(screen.queryByRole('heading', { name: 'Uptime Kuma' })).toBeNull()
    expect(screen.getByRole('heading', { name: 'Redis' })).toBeTruthy()
  })

  it('does not offer the wizard without app-edit permission', async () => {
    renderTemplates(fakeApi(), ['stack.view'])
    expect(await screen.findByRole('heading', { name: 'Uptime Kuma' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Use template' })).toBeNull()
    expect(screen.getAllByText(/permission to create apps/).length).toBeGreaterThan(0)
  })
})
