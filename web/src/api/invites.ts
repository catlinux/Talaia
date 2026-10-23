import type { TotpSetup } from './auth'
import { createHttpClient } from './http'
import type { Role, Scope } from './users'

/** Invite is one pending account invitation. */
export interface Invite {
  id: string
  username: string
  email: string
  role: Role
  scope: Scope
  createdAt: string
  expiresAt: string
}

/** CreatedInvite is the one-time answer of creating an invitation. */
export interface CreatedInvite {
  id: string
  token: string
  linkPath: string
  expiresAt: string
}

/** InviteInput is what the Users screen sends to create an invitation. */
export interface InviteInput {
  username: string
  email: string
  role: Role
  scope: Scope
}

/** InviteInfo is the public description of a pending invitation. */
export interface InviteInfo {
  username: string
  expiresAt: string
}

/** InvitesApi creates, revokes and accepts invitations. */
export interface InvitesApi {
  list(): Promise<Invite[]>
  create(input: InviteInput): Promise<CreatedInvite>
  revoke(inviteId: string): Promise<void>
  info(token: string): Promise<InviteInfo>
  accept(token: string, password: string): Promise<TotpSetup>
}

interface apiBindingScope {
  type: string
  server_id?: string
  group_id?: string
  name?: string
}

interface apiInvite {
  id: string
  username: string
  email: string
  role: string
  scope: apiBindingScope
  created_at: string
  expires_at: string
}

interface invitesResponse {
  invites: apiInvite[]
}

interface apiCreatedInvite {
  id: string
  token: string
  link_path: string
  expires_at: string
}

interface apiInviteInfo {
  username: string
  expires_at: string
}

function toScope(scope: apiBindingScope): Scope {
  return {
    type: scope.type as Scope['type'],
    ...(scope.server_id ? { serverId: scope.server_id } : {}),
    ...(scope.group_id ? { groupId: scope.group_id } : {}),
    ...(scope.name ? { name: scope.name } : {}),
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

/** createInvitesApi returns the invitation API of the core. */
export function createInvitesApi(fetchImpl?: typeof fetch): InvitesApi {
  const http = createHttpClient(fetchImpl)
  return {
    list: async () => {
      const body = await http.get<invitesResponse>('/api/v1/users/invites')
      return (body.invites ?? []).map((invite) => ({
        id: invite.id,
        username: invite.username,
        email: invite.email,
        role: invite.role as Role,
        scope: toScope(invite.scope),
        createdAt: invite.created_at,
        expiresAt: invite.expires_at,
      }))
    },
    create: async (input) => {
      const body = await http.post<apiCreatedInvite>('/api/v1/users/invites', {
        username: input.username,
        email: input.email,
        role: input.role,
        scope: apiScope(input.scope),
      })
      return {
        id: body.id,
        token: body.token,
        linkPath: body.link_path,
        expiresAt: body.expires_at,
      }
    },
    revoke: (inviteId) =>
      http.delete<void>(`/api/v1/users/invites/${encodeURIComponent(inviteId)}`),
    info: async (token) => {
      const body = await http.get<apiInviteInfo>(`/api/v1/auth/invite/${encodeURIComponent(token)}`)
      return { username: body.username, expiresAt: body.expires_at }
    },
    accept: (token, password) =>
      http.post<TotpSetup>(`/api/v1/auth/invite/${encodeURIComponent(token)}/accept`, {
        password,
      }),
  }
}
