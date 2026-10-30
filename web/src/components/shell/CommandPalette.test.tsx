import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { FleetClient } from '../../api/client'
import type { ContainersApi } from '../../api/containers'
import type { MeApi, MePermissions, Permission } from '../../api/me'
import type { Server } from '../../api/types'
import { PermissionsProvider } from '../../auth/permissions'
import i18n from '../../i18n'
import { RouterProvider } from '../../router'
import { CommandPalette } from './CommandPalette'
import { Topbar } from './Topbar'

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: 's1',
    name: 'vps-web-01',
    os: 'Debian 12',
    location: 'example.com',
    vcpu: 2,
    memoryGb: 4,
    status: 'online',
    cpuPercent: 10,
    memPercent: 20,
    diskPercent: 30,
    containers: 1,
    stacks: 1,
    tags: [],
    lastSeen: null,
    agentVersion: '1.0.0',
    agentLatestVersion: '1.0.0',
    agentUpdateAvailable: false,
    agentUpdateManual: false,
    agentUpgradeCommand: '',
    ...overrides,
  }
}

function me(permissions: string[] = ['server.view', 'container.view', 'stack.view']): MeApi {
  const scopes: MePermissions = {
    organization: permissions as Permission[],
    servers: {},
    stacks: {},
    containers: {},
  }
  return {
    get: () =>
      Promise.resolve({
        user: { id: 'u1', username: 'alice', display_name: 'Alice', email: '' },
        bindings: { bindings: [] },
        permissions: scopes,
      }),
  }
}

function fakeClient(listServers = () => Promise.resolve([server()])): FleetClient {
  return {
    listServers,
    listActivity: () => Promise.resolve([]),
    createEnrollment: () => Promise.reject(new Error('unexpected enrollment')),
  }
}

function fakeContainers(): ContainersApi {
  return {
    list: () =>
      Promise.resolve({
        connected: true,
        dockerAvailable: true,
        containers: [
          {
            id: 'c1',
            name: 'db',
            image: 'postgres:16',
            state: 'running',
            status: 'Up',
            composeProject: 'shop',
            composeService: 'db',
          },
        ],
      }),
    stacks: () =>
      Promise.resolve({
        connected: true,
        stacks: ['shop'],
        adopted: [],
        broken: [],
        adoptable: [],
      }),
  } as unknown as ContainersApi
}

function Harness({
  fleetClient = fakeClient(),
  containersApi = fakeContainers(),
  meApi = me(),
}: {
  fleetClient?: FleetClient
  containersApi?: ContainersApi
  meApi?: MeApi
}) {
  const [open, setOpen] = useState(false)
  return (
    <RouterProvider>
      <PermissionsProvider meApi={meApi}>
        <button type="button" onClick={() => setOpen(true)}>
          Open palette
        </button>
        <CommandPalette
          open={open}
          onOpen={() => setOpen(true)}
          onClose={() => setOpen(false)}
          client={fleetClient}
          containersApi={containersApi}
        />
      </PermissionsProvider>
    </RouterProvider>
  )
}

beforeEach(async () => {
  localStorage.clear()
  document.documentElement.removeAttribute('data-theme')
  window.history.replaceState({}, '', '/')
  await i18n.changeLanguage('en')
})

afterEach(() => {
  cleanup()
  window.history.replaceState({}, '', '/')
})

describe('CommandPalette', () => {
  it('opens with Ctrl+K, filters, navigates with Enter and returns focus on Escape', async () => {
    render(<Harness />)
    const opener = screen.getByRole('button', { name: 'Open palette' })
    opener.focus()
    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })

    expect(await screen.findByRole('dialog')).toBeTruthy()
    const input = screen.getByRole('combobox')
    expect(input.getAttribute('aria-expanded')).toBe('true')
    expect(input.getAttribute('aria-controls')).toBe('command-palette-list')

    fireEvent.change(input, { target: { value: 'web' } })
    expect(await screen.findByRole('option', { name: /^vps-web-01/ })).toBeTruthy()
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    fireEvent.keyDown(input, { key: 'ArrowUp' })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(window.location.pathname).toBe('/servers/s1'))
    expect(screen.queryByRole('dialog')).toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(opener))

    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    expect(await screen.findByRole('dialog')).toBeTruthy()
    fireEvent.keyDown(screen.getByRole('combobox'), { key: 'Escape' })
    await waitFor(() => expect(document.activeElement).toBe(opener))
  })

  it('finds containers and stacks from the background fleet load', async () => {
    const containersApi = fakeContainers()
    render(<Harness containersApi={containersApi} />)
    fireEvent.click(screen.getByRole('button', { name: 'Open palette' }))
    const input = await screen.findByRole('combobox')

    fireEvent.change(input, { target: { value: 'db' } })
    expect(await screen.findByRole('option', { name: /^db/ })).toBeTruthy()
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(window.location.pathname).toBe('/servers/s1/containers'))

    fireEvent.click(screen.getByRole('button', { name: 'Open palette' }))
    fireEvent.change(await screen.findByRole('combobox'), { target: { value: 'shop' } })
    expect(await screen.findByRole('option', { name: /shop/ })).toBeTruthy()
  })

  it('hides screens and actions that the user may not use', async () => {
    render(<Harness meApi={me([])} />)
    fireEvent.click(screen.getByRole('button', { name: 'Open palette' }))
    await screen.findByRole('dialog')

    expect(screen.queryByRole('option', { name: 'Containers' })).toBeNull()
    expect(screen.queryByRole('option', { name: 'Stacks' })).toBeNull()
    expect(screen.queryByRole('option', { name: 'Users' })).toBeNull()
    expect(screen.getByRole('option', { name: 'Servers' })).toBeTruthy()
    expect(screen.getByRole('option', { name: 'Settings' })).toBeTruthy()
  })

  it('runs the theme action and remembers recent picks', async () => {
    render(<Harness />)
    fireEvent.click(screen.getByRole('button', { name: 'Open palette' }))
    const input = await screen.findByRole('combobox')
    fireEvent.change(input, { target: { value: 'theme' } })
    expect(await screen.findByRole('option', { name: 'Switch theme' })).toBeTruthy()
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(document.documentElement.getAttribute('data-theme')).toBe('dark'))
    expect(screen.queryByRole('dialog')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Open palette' }))
    expect(await screen.findByRole('option', { name: 'Switch theme' })).toBeTruthy()
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'web' } })
    fireEvent.keyDown(screen.getByRole('combobox'), { key: 'Enter' })
    await waitFor(() => expect(window.location.pathname).toBe('/servers/s1'))

    fireEvent.click(screen.getByRole('button', { name: 'Open palette' }))
    expect(await screen.findByText('Recent')).toBeTruthy()
    expect(screen.getByRole('option', { name: /^vps-web-01/ })).toBeTruthy()
  })

  it('offers Add server to users who may enroll and opens its address', async () => {
    render(<Harness meApi={me(['server.view', 'server.enroll'])} />)
    fireEvent.click(screen.getByRole('button', { name: 'Open palette' }))
    const input = await screen.findByRole('combobox')
    fireEvent.change(input, { target: { value: 'add server' } })
    expect(await screen.findByRole('option', { name: 'Add server' })).toBeTruthy()
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(window.location.pathname).toBe('/servers'))
    expect(window.location.search).toBe('?add=1')
  })

  it('renders the top bar search button and opens the palette from it', async () => {
    render(
      <RouterProvider>
        <PermissionsProvider meApi={me()}>
          <Topbar
            alertSummary={null}
            client={fakeClient()}
            containersApi={fakeContainers()}
            onOpenMenu={vi.fn()}
          />
        </PermissionsProvider>
      </RouterProvider>,
    )

    const search = screen.getByRole('button', { name: 'Open search' })
    expect(search.textContent).toContain('Search')
    fireEvent.click(search)
    expect(await screen.findByRole('dialog')).toBeTruthy()
  })

  it('does not open while another modal dialog is open', async () => {
    render(
      <>
        <div role="dialog" aria-modal="true" aria-label="Other dialog" />
        <Harness />
      </>,
    )
    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    expect(screen.queryByRole('dialog', { name: 'Command palette' })).toBeNull()
  })
})
