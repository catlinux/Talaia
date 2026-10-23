import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Group, GroupsApi } from '../api/groups'
import { ApiError } from '../api/http'
import type { Invite, InvitesApi } from '../api/invites'
import type { ServerSummary, ServersApi } from '../api/servers'
import type { Team, TeamsApi } from '../api/teams'
import type { AdminUser, Binding, UsersApi } from '../api/users'
import i18n from '../i18n'
import { Users } from './Users'

const binding: Binding = {
  id: 'b1',
  role: 'viewer',
  scope: { type: 'organization' },
}

function account(overrides: Partial<AdminUser> = {}): AdminUser {
  return {
    id: 'u1',
    username: 'admin',
    displayName: 'Ada',
    email: 'ada@example.com',
    disabled: false,
    totpEnabled: true,
    bindings: [binding],
    ...overrides,
  }
}

function fakeUsers(users: AdminUser[], disable = vi.fn(() => Promise.resolve())): UsersApi {
  return {
    list: vi.fn(() => Promise.resolve(users)),
    update: vi.fn(() => Promise.resolve()),
    disable,
    enable: vi.fn(() => Promise.resolve()),
    listBindings: vi.fn(() => Promise.resolve(binding ? [binding] : [])),
    addBinding: vi.fn((_userId, role, scope) => Promise.resolve({ id: 'b2', role, scope })),
    updateBinding: vi.fn(() => Promise.resolve()),
    removeBinding: vi.fn(() => Promise.resolve()),
  }
}

function fakeTeams(): TeamsApi {
  const unexpected = () => Promise.reject(new Error('unexpected team call'))
  return {
    list: () => Promise.resolve([] as Team[]),
    create: unexpected,
    get: unexpected,
    rename: unexpected,
    remove: unexpected,
    addMember: unexpected,
    removeMember: unexpected,
    listBindings: () => Promise.resolve([]),
    addBinding: unexpected,
  }
}

function fakeGroups(): GroupsApi {
  const unexpected = () => Promise.reject(new Error('unexpected group call'))
  return {
    list: () => Promise.resolve([] as Group[]),
    create: unexpected,
    rename: unexpected,
    remove: unexpected,
    setServerGroup: unexpected,
  }
}

function fakeInvites(): InvitesApi {
  const invites: Invite[] = []
  return {
    list: () => Promise.resolve(invites),
    create: vi.fn(() =>
      Promise.resolve({
        id: 'i1',
        token: 'secret',
        linkPath: '/invite#secret',
        expiresAt: '2026-10-30T10:00:00Z',
      }),
    ),
    revoke: vi.fn(() => Promise.resolve()),
    info: () => Promise.resolve({ username: 'invitee', expiresAt: '2026-10-30T10:00:00Z' }),
    accept: () => Promise.resolve({ secret: 'ABCDEF', uri: 'otpauth://totp/Talaia:invitee' }),
  }
}

const server: ServerSummary = {
  id: 'srv1',
  name: 'vps-web-01',
  connected: true,
  revoked: false,
  agentVersion: '0.2.0',
  agentLatestVersion: '0.2.8',
  agentUpdateAvailable: false,
  agentUpdateManual: false,
  agentUpgradeCommand: '',
  hostTerminal: false,
}

function fakeServers(): ServersApi {
  const unexpected = () => Promise.reject(new Error('unexpected server call'))
  return {
    listServers: () => Promise.resolve([server]),
    removeServer: unexpected,
    updateAgent: unexpected,
    updateAllAgents: unexpected,
  }
}

function renderUsers(usersApi: UsersApi, invitesApi = fakeInvites()) {
  render(
    <Users
      usersApi={usersApi}
      teamsApi={fakeTeams()}
      groupsApi={fakeGroups()}
      invitesApi={invitesApi}
      serversApi={fakeServers()}
    />,
  )
}

describe('Users screen', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => cleanup())

  it('lists users, roles and status', async () => {
    renderUsers(fakeUsers([account()]))

    expect(await screen.findByText('Ada')).toBeTruthy()
    expect(screen.getByText('admin')).toBeTruthy()
    expect(screen.getByText('Viewer · Whole organization')).toBeTruthy()
    expect(screen.getByText('Active')).toBeTruthy()
    expect(screen.getByText('TOTP on')).toBeTruthy()
  })

  it('shows the invitation link once and forgets it when the dialog closes', async () => {
    renderUsers(fakeUsers([account()]))

    fireEvent.click(await screen.findByRole('button', { name: 'Invite user' }))
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'new-user' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create invitation' }))

    expect(await screen.findByText(/\/invite#secret$/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(screen.queryByText(/\/invite#secret$/)).toBeNull())

    fireEvent.click(screen.getByRole('button', { name: 'Invite user' }))
    expect(screen.queryByText(/\/invite#secret$/)).toBeNull()
  })

  it('shows the explicit last administrator error', async () => {
    const usersApi = fakeUsers(
      [account()],
      vi.fn(() => Promise.reject(new ApiError('last_admin', 409))),
    )
    renderUsers(usersApi)

    fireEvent.click((await screen.findAllByRole('button', { name: 'Disable' }))[0]!)
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Disable' }))

    expect(
      await screen.findByText('You cannot disable the last active administrator.'),
    ).toBeTruthy()
  })

  it('adds and removes access', async () => {
    const usersApi = fakeUsers([account()])
    renderUsers(usersApi)

    fireEvent.click((await screen.findAllByRole('button', { name: 'Access' }))[0]!)
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove assignment for Viewer' }))
    await waitFor(() => expect(usersApi.removeBinding).toHaveBeenCalledWith('b1'))

    fireEvent.click(within(dialog).getByRole('button', { name: 'Add access' }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add access' }))
    await waitFor(() =>
      expect(usersApi.addBinding).toHaveBeenCalledWith(
        'u1',
        'viewer',
        expect.objectContaining({ type: 'organization' }),
      ),
    )
  })
})
