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

const authUser = vi.hoisted(() => ({ username: null as string | null }))
vi.mock('../auth/AuthGate', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../auth/AuthGate')>()
  return { ...actual, useAuth: () => (authUser.username ? { username: authUser.username } : null) }
})

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
    delete: vi.fn(() => Promise.resolve()),
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
    authUser.username = null
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

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
    fireEvent.change(screen.getByLabelText(/^Username/), { target: { value: 'new-user' } })
    fireEvent.change(screen.getByLabelText(/^Email/), {
      target: { value: 'new@example.com' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create invitation' }))

    expect(await screen.findByText(/\/invite#secret$/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(screen.queryByText(/\/invite#secret$/)).toBeNull())

    fireEvent.click(screen.getByRole('button', { name: 'Invite user' }))
    expect(screen.queryByText(/\/invite#secret$/)).toBeNull()
  })

  it('creates an invitation without an email and rejects a malformed email', async () => {
    const invites = fakeInvites()
    renderUsers(fakeUsers([account()]), invites)
    fireEvent.click(await screen.findByRole('button', { name: 'Invite user' }))
    fireEvent.change(screen.getByLabelText(/^Username/), { target: { value: 'new-user' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create invitation' }))

    await waitFor(() =>
      expect(invites.create).toHaveBeenCalledWith(
        expect.objectContaining({
          username: 'new-user',
        }),
      ),
    )
    expect(invites.create).toHaveBeenCalledWith(expect.objectContaining({ email: '' }))

    fireEvent.click(await screen.findByRole('button', { name: 'Done' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Invite user' }))
    fireEvent.change(screen.getByLabelText(/^Username/), { target: { value: 'new-user' } })
    fireEvent.change(screen.getByLabelText(/^Email/), { target: { value: 'bad-address' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create invitation' }))
    expect(await screen.findByText('Enter a valid email address.')).toBeTruthy()
    expect(invites.create).toHaveBeenCalledTimes(1)
  })

  it('allows clearing the email when editing a user', async () => {
    const usersApi = fakeUsers([account()])
    renderUsers(usersApi)
    fireEvent.click((await screen.findAllByRole('button', { name: 'Edit' }))[0]!)
    fireEvent.change(screen.getByLabelText(/^Email/), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(usersApi.update).toHaveBeenCalledWith('u1', 'Ada', ''))
  })

  it('blocks an empty stack name and shows the inline message', async () => {
    const invites = fakeInvites()
    renderUsers(fakeUsers([account()]), invites)
    fireEvent.click(await screen.findByRole('button', { name: 'Invite user' }))
    fireEvent.change(screen.getByLabelText(/^Username/), { target: { value: 'new-user' } })
    fireEvent.change(screen.getByLabelText(/^Email/), {
      target: { value: 'new@example.com' },
    })
    fireEvent.change(screen.getByLabelText('Role'), { target: { value: 'app_operator' } })
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: 'stack' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create invitation' }))

    expect(await screen.findByText('Stack or container name is required.')).toBeTruthy()
    expect(invites.create).not.toHaveBeenCalled()
  })

  it('shows a specific server validation message for an invite error', async () => {
    const invites = fakeInvites()
    invites.create = vi.fn(() =>
      Promise.reject(new ApiError('invalid_request', 400, 'scope.name', 'required')),
    )
    renderUsers(fakeUsers([account()]), invites)
    fireEvent.click(await screen.findByRole('button', { name: 'Invite user' }))
    fireEvent.change(screen.getByLabelText(/^Username/), { target: { value: 'new-user' } })
    fireEvent.change(screen.getByLabelText(/^Email/), {
      target: { value: 'new@example.com' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create invitation' }))

    expect((await screen.findByRole('alert')).textContent).toBe(
      'Stack or container name is required.',
    )
  })

  it('offers the server stack list in the invite dialog', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              connected: true,
              stacks: ['web'],
              adopted: [],
              broken: [],
              adoptable: [],
            }),
            { status: 200 },
          ),
        ),
      ),
    )
    renderUsers(fakeUsers([account()]))
    fireEvent.click(await screen.findByRole('button', { name: 'Invite user' }))
    fireEvent.change(screen.getByLabelText('Role'), { target: { value: 'app_operator' } })
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: 'stack' } })
    expect(await screen.findByRole('option', { name: 'web' })).toBeTruthy()
    vi.unstubAllGlobals()
  })

  it('falls back to exact-name text entry when the stack list fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('offline'))),
    )
    renderUsers(fakeUsers([account()]))
    fireEvent.click(await screen.findByRole('button', { name: 'Invite user' }))
    fireEvent.change(screen.getByLabelText('Role'), { target: { value: 'app_operator' } })
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: 'stack' } })
    expect(await screen.findByPlaceholderText('Name')).toBeTruthy()
    expect(screen.getByText('Type the exact name')).toBeTruthy()
    vi.unstubAllGlobals()
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

  it('shows the cannot disable yourself error', async () => {
    const usersApi = fakeUsers(
      [account()],
      vi.fn(() => Promise.reject(new ApiError('cannot_disable_self', 409))),
    )
    renderUsers(usersApi)
    fireEvent.click((await screen.findAllByRole('button', { name: 'Disable' }))[0]!)
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Disable' }))
    expect(await screen.findByText('You cannot disable your own account.')).toBeTruthy()
  })

  it('hides delete for active users and the signed-in user', async () => {
    authUser.username = 'admin'
    renderUsers(
      fakeUsers([
        account({ disabled: true }),
        account({ id: 'u2', username: 'active-user' }),
        account({ id: 'u3', username: 'disabled', disabled: true }),
      ]),
    )
    await screen.findAllByText('Ada')
    const rows = screen.getAllByRole('row')
    expect(within(rows[1]!).queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(within(rows[2]!).queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.getAllByRole('button', { name: 'Delete' })).toHaveLength(1)
    expect(within(rows[2]!).getByRole('button', { name: 'Disable' })).toBeTruthy()
    authUser.username = null
  })

  it('requires the exact username and removes a deleted user', async () => {
    const user = account({ id: 'u2', username: 'old-user', disabled: true })
    const state = [user]
    const usersApi = fakeUsers(state)
    usersApi.list = vi.fn(() => Promise.resolve(state))
    usersApi.delete = vi.fn(async (id) => {
      state.splice(
        state.findIndex((candidate) => candidate.id === id),
        1,
      )
    })
    renderUsers(usersApi)
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    const dialog = screen.getByRole('dialog')
    const confirm = within(dialog).getByRole('button', { name: 'Delete' })
    expect(confirm).toHaveProperty('disabled', true)
    fireEvent.change(within(dialog).getByLabelText('Type old-user to confirm'), {
      target: { value: 'old' },
    })
    expect(confirm).toHaveProperty('disabled', true)
    fireEvent.change(within(dialog).getByLabelText('Type old-user to confirm'), {
      target: { value: 'old-user' },
    })
    expect(confirm).toHaveProperty('disabled', false)
    fireEvent.click(confirm)
    await waitFor(() => expect(usersApi.delete).toHaveBeenCalledWith('u2', 'old-user'))
    await waitFor(() => expect(screen.queryByText('old-user')).toBeNull())
  })

  it.each(['cannot_delete_self', 'user_not_disabled', 'last_admin'] as const)(
    'shows delete error %s',
    async (code) => {
      const usersApi = fakeUsers([account({ id: 'u2', username: 'old-user', disabled: true })])
      usersApi.delete = vi.fn(() => Promise.reject(new ApiError(code, 409)))
      renderUsers(usersApi)
      fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
      const dialog = screen.getByRole('dialog')
      fireEvent.change(within(dialog).getByLabelText('Type old-user to confirm'), {
        target: { value: 'old-user' },
      })
      fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
      expect((await within(dialog).findByRole('alert')).textContent).toBe(
        (
          {
            cannot_delete_self: 'You cannot delete your own account.',
            user_not_disabled: 'Disable this account before deleting it.',
            last_admin: 'You cannot disable the last active administrator.',
          } as const
        )[code],
      )
    },
  )

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

  it('clears the access error when the add form is opened again and after a success', async () => {
    const usersApi = fakeUsers([account()])
    usersApi.addBinding = vi.fn(() => Promise.reject(new Error('This assignment already exists')))
    renderUsers(usersApi)
    fireEvent.click((await screen.findAllByRole('button', { name: 'Access' }))[0]!)
    const dialog = screen.getByRole('dialog')
    const addAccess = () => within(dialog).getByRole('button', { name: 'Add access' })

    fireEvent.click(addAccess()) // opens the form
    fireEvent.click(addAccess()) // submits it and fails
    expect(await within(dialog).findByRole('alert')).toBeTruthy()

    fireEvent.click(addAccess()) // opens the form again
    expect(within(dialog).queryByRole('alert')).toBeNull()

    usersApi.addBinding = vi.fn((_userId, role, scope) =>
      Promise.resolve({ id: 'b3', role, scope }),
    )
    fireEvent.click(addAccess()) // submits it and succeeds
    await waitFor(() => expect(usersApi.addBinding).toHaveBeenCalled())
    expect(within(dialog).queryByRole('alert')).toBeNull()
  })
})
