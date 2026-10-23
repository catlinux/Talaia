import { createHttpClient } from './http'
import type { Binding, Role, Scope } from './users'

/** APIKey is one key of the panel; the secret is never listed again. */
export interface APIKey {
  id: string
  /** keyId is the public prefix used to identify the key. */
  keyId: string
  name: string
  expiresAt: string
  lastUsedAt: string | null
  revoked: boolean
  bindings: Binding[]
}

/** APIKeyBinding is one role assignment of a new key. */
export interface APIKeyBinding {
  role: Role
  scope: Scope
}

/** CreatedAPIKey is the one-time response of creating a key. */
export interface CreatedAPIKey {
  id: string
  /** key is the secret; it must only live in the dialog that shows it. */
  key: string
  name: string
  expiresAt: string
}

/** APIKeyInput is what the create dialog sends to the core. */
export interface APIKeyInput {
  name: string
  expiresInDays: number
  bindings: APIKeyBinding[]
  totpCode: string
}

/** APIKeysApi manages the keys of the signed-in user and, for admins, all keys. */
export interface APIKeysApi {
  list(all?: boolean): Promise<APIKey[]>
  create(input: APIKeyInput): Promise<CreatedAPIKey>
  revoke(id: string): Promise<void>
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

interface apiAPIKey {
  id: string
  key_id: string
  name: string
  expires_at: string
  last_used_at: string | null
  revoked: boolean
  bindings: apiBinding[]
}

interface apiKeysResponse {
  keys: apiAPIKey[]
}

interface apiCreatedKey {
  id: string
  key: string
  name: string
  expires_at: string
}

function toBinding(binding: apiBinding): Binding {
  return {
    id: binding.id,
    role: binding.role as Role,
    scope: {
      type: binding.scope.type as Scope['type'],
      ...(binding.scope.server_id ? { serverId: binding.scope.server_id } : {}),
      ...(binding.scope.group_id ? { groupId: binding.scope.group_id } : {}),
      ...(binding.scope.name ? { name: binding.scope.name } : {}),
    },
  }
}

function apiScope(scope: Scope) {
  return {
    type: scope.type,
    server_id: scope.serverId ?? '',
    group_id: scope.groupId ?? '',
    name: scope.name ?? '',
  }
}

/** createAPIKeysApi returns the API key client of the core. */
export function createAPIKeysApi(fetchImpl?: typeof fetch): APIKeysApi {
  const http = createHttpClient(fetchImpl)
  return {
    list: async (all = false) => {
      const body = await http.get<apiKeysResponse>(`/api/v1/apikeys${all ? '?all=1' : ''}`)
      return (body.keys ?? []).map((key) => ({
        id: key.id,
        keyId: key.key_id,
        name: key.name,
        expiresAt: key.expires_at,
        lastUsedAt: key.last_used_at,
        revoked: key.revoked,
        bindings: (key.bindings ?? []).map(toBinding),
      }))
    },
    create: async (input) => {
      const body = await http.post<apiCreatedKey>('/api/v1/apikeys', {
        name: input.name,
        expires_in_days: input.expiresInDays,
        bindings: input.bindings.map((binding) => ({
          role: binding.role,
          scope: apiScope(binding.scope),
        })),
        totp_code: input.totpCode,
      })
      return {
        id: body.id,
        key: body.key,
        name: body.name,
        expiresAt: body.expires_at,
      }
    },
    revoke: (id) => http.delete<void>(`/api/v1/apikeys/${encodeURIComponent(id)}`),
  }
}
