import { createHttpClient } from './http'

/** Role is one of the fixed roles of the panel. */
export type Role = 'admin' | 'server_operator' | 'app_operator' | 'viewer' | 'metrics_only'

/** ScopeType is the level at which an assignment applies. */
export type ScopeType = 'organization' | 'group' | 'server' | 'stack' | 'container'

/** Scope identifies what an assignment covers. */
export interface Scope {
  type: ScopeType
  serverId?: string
  groupId?: string
  name?: string
}

/** Binding is one role assignment. */
export interface Binding {
  id: string
  role: Role
  scope: Scope
}

/** AdminUser is one account as the Users screen sees it. */
export interface AdminUser {
  id: string
  username: string
  displayName: string
  email: string
  disabled: boolean
  totpEnabled: boolean
  bindings: Binding[]
}

/** UsersApi manages accounts and their assignments. */
export interface UsersApi {
  list(): Promise<AdminUser[]>
  update(userId: string, displayName: string, email: string): Promise<void>
  disable(userId: string): Promise<void>
  enable(userId: string): Promise<void>
  delete(userId: string, username: string): Promise<void>
  listBindings(userId: string): Promise<Binding[]>
  addBinding(userId: string, role: Role, scope: Scope): Promise<Binding>
  updateBinding(bindingId: string, role: Role): Promise<void>
  removeBinding(bindingId: string): Promise<void>
}

interface apiBinding {
  id: string
  role: string
  scope: {
    type: string
    server_id?: string
    group_id?: string
    name?: string
  }
}

interface apiUser {
  id: string
  username: string
  display_name: string
  email: string
  disabled: boolean
  totp_enabled: boolean
  bindings: apiBinding[]
}

interface usersResponse {
  users: apiUser[]
}

interface bindingsResponse {
  bindings: apiBinding[]
}

function toScope(scope: apiBinding['scope']): Scope {
  return {
    type: scope.type as ScopeType,
    ...(scope.server_id ? { serverId: scope.server_id } : {}),
    ...(scope.group_id ? { groupId: scope.group_id } : {}),
    ...(scope.name ? { name: scope.name } : {}),
  }
}

export function toBinding(binding: apiBinding): Binding {
  return { id: binding.id, role: binding.role as Role, scope: toScope(binding.scope) }
}

function apiScope(scope: Scope) {
  return {
    type: scope.type,
    server_id: scope.serverId ?? '',
    group_id: scope.groupId ?? '',
    name: scope.name ?? '',
  }
}

/** createUsersApi returns the users and bindings API of the core. */
export function createUsersApi(fetchImpl?: typeof fetch): UsersApi {
  const http = createHttpClient(fetchImpl)
  return {
    list: async () => {
      const body = await http.get<usersResponse>('/api/v1/users')
      return body.users.map((user) => ({
        id: user.id,
        username: user.username,
        displayName: user.display_name,
        email: user.email,
        disabled: user.disabled,
        totpEnabled: user.totp_enabled,
        bindings: user.bindings.map(toBinding),
      }))
    },
    update: (userId, displayName, email) =>
      http.patch<void>(`/api/v1/users/${encodeURIComponent(userId)}`, {
        display_name: displayName,
        email,
      }),
    disable: (userId) => http.post<void>(`/api/v1/users/${encodeURIComponent(userId)}/disable`),
    enable: (userId) => http.post<void>(`/api/v1/users/${encodeURIComponent(userId)}/enable`),
    delete: (userId, username) =>
      http.delete<void>(`/api/v1/users/${encodeURIComponent(userId)}`, {
        confirm_username: username,
      }),
    listBindings: async (userId) => {
      const body = await http.get<bindingsResponse>(
        `/api/v1/users/${encodeURIComponent(userId)}/bindings`,
      )
      return body.bindings.map(toBinding)
    },
    addBinding: async (userId, role, scope) => {
      const body = await http.post<apiBinding>(
        `/api/v1/users/${encodeURIComponent(userId)}/bindings`,
        { role, scope: apiScope(scope) },
      )
      return toBinding(body)
    },
    updateBinding: (bindingId, role) =>
      http.patch<void>(`/api/v1/bindings/${encodeURIComponent(bindingId)}`, { role }),
    removeBinding: (bindingId) =>
      http.delete<void>(`/api/v1/bindings/${encodeURIComponent(bindingId)}`),
  }
}
