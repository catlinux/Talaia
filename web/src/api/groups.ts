import { createHttpClient } from './http'

/** Group is a server group used as an assignment scope. */
export interface Group {
  id: string
  name: string
}

/** GroupsApi manages server groups and their server assignments. */
export interface GroupsApi {
  list(): Promise<Group[]>
  create(name: string): Promise<Group>
  rename(groupId: string, name: string): Promise<void>
  remove(groupId: string): Promise<void>
  setServerGroup(serverId: string, groupId: string | null): Promise<void>
}

interface apiGroup {
  id: string
  name: string
}

interface groupsResponse {
  groups: apiGroup[]
}

/** createGroupsApi returns the server-group API of the core. */
export function createGroupsApi(fetchImpl?: typeof fetch): GroupsApi {
  const http = createHttpClient(fetchImpl)
  return {
    list: async () => {
      const body = await http.get<groupsResponse>('/api/v1/groups')
      return body.groups
    },
    create: (name) => http.post<apiGroup>('/api/v1/groups', { name }),
    rename: (groupId, name) =>
      http.patch<void>(`/api/v1/groups/${encodeURIComponent(groupId)}`, { name }),
    remove: (groupId) => http.delete<void>(`/api/v1/groups/${encodeURIComponent(groupId)}`),
    setServerGroup: (serverId, groupId) =>
      http.put<void>(`/api/v1/servers/${encodeURIComponent(serverId)}/group`, {
        group_id: groupId,
      }),
  }
}
