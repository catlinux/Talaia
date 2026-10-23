import { createHttpClient } from './http'
import type { Binding, Role, Scope } from './users'

/** Team is a named group of users. */
export interface Team {
  id: string
  name: string
}

/** TeamDetail includes the members and assignments of a team. */
export interface TeamDetail extends Team {
  members: string[]
  bindings: Binding[]
}

/** TeamsApi manages teams, their members and their assignments. */
export interface TeamsApi {
  list(): Promise<Team[]>
  create(name: string): Promise<Team>
  get(teamId: string): Promise<TeamDetail>
  rename(teamId: string, name: string): Promise<void>
  remove(teamId: string): Promise<void>
  addMember(teamId: string, userId: string): Promise<void>
  removeMember(teamId: string, userId: string): Promise<void>
  listBindings(teamId: string): Promise<Binding[]>
  addBinding(teamId: string, role: Role, scope: Scope): Promise<Binding>
}

interface apiTeam {
  id: string
  name: string
}

interface teamsResponse {
  teams: apiTeam[]
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

interface teamDetailResponse extends apiTeam {
  members: string[]
  bindings: apiBinding[]
}

interface bindingsResponse {
  bindings: apiBinding[]
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

/** createTeamsApi returns the teams API of the core. */
export function createTeamsApi(fetchImpl?: typeof fetch): TeamsApi {
  const http = createHttpClient(fetchImpl)
  return {
    list: async () => {
      const body = await http.get<teamsResponse>('/api/v1/teams')
      return body.teams
    },
    create: (name) => http.post<apiTeam>('/api/v1/teams', { name }),
    get: async (teamId) => {
      const body = await http.get<teamDetailResponse>(`/api/v1/teams/${encodeURIComponent(teamId)}`)
      return {
        id: body.id,
        name: body.name,
        members: body.members ?? [],
        bindings: (body.bindings ?? []).map(toBinding),
      }
    },
    rename: (teamId, name) =>
      http.patch<void>(`/api/v1/teams/${encodeURIComponent(teamId)}`, { name }),
    remove: (teamId) => http.delete<void>(`/api/v1/teams/${encodeURIComponent(teamId)}`),
    addMember: (teamId, userId) =>
      http.put<void>(
        `/api/v1/teams/${encodeURIComponent(teamId)}/members/${encodeURIComponent(userId)}`,
      ),
    removeMember: (teamId, userId) =>
      http.delete<void>(
        `/api/v1/teams/${encodeURIComponent(teamId)}/members/${encodeURIComponent(userId)}`,
      ),
    listBindings: async (teamId) => {
      const body = await http.get<bindingsResponse>(
        `/api/v1/teams/${encodeURIComponent(teamId)}/bindings`,
      )
      return (body.bindings ?? []).map(toBinding)
    },
    addBinding: async (teamId, role, scope) => {
      const body = await http.post<apiBinding>(
        `/api/v1/teams/${encodeURIComponent(teamId)}/bindings`,
        { role, scope: apiScope(scope) },
      )
      return toBinding(body)
    },
  }
}
