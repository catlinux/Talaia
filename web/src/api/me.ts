import { createHttpClient } from './http'

/** Permission names the core exposes in GET /api/v1/me. */
export type Permission =
  | 'container.view'
  | 'container.logs'
  | 'container.control'
  | 'container.terminal'
  | 'container.remove'
  | 'stack.view'
  | 'stack.deploy'
  | 'stack.edit'
  | 'server.view'
  | 'server.remove'
  | 'server.enroll'
  | 'server.agent_update'
  | 'resources.manage'
  | 'host.services.view'
  | 'host.services.control'
  | 'host.journal'
  | 'host.apt.view'
  | 'host.apt.upgrade'
  | 'host.reboot'
  | 'host.terminal'
  | 'users.manage'
  | 'settings.manage'
  | 'audit.view'
  | 'recordings.view'
  | 'apikeys.manage'

/** MeUser is the signed-in account. */
export interface MeUser {
  id: string
  username: string
  display_name: string
  email: string
}

/** MePermissions is the effective permission tree of the core. */
export interface MePermissions {
  organization: Permission[]
  servers: Record<string, Permission[]>
  stacks: Record<string, Record<string, Permission[]>>
  containers: Record<string, Record<string, Permission[]>>
}

/** MeResponse is the body of GET /api/v1/me. */
export interface MeResponse {
  user: MeUser
  bindings: {
    bindings: Array<{
      role: string
      scope: {
        type?: string
        server_id?: string
        group_id?: string
        name?: string
      }
    }>
  }
  permissions: MePermissions
}

/** MeApi answers the effective permissions of the signed-in user. */
export interface MeApi {
  get(): Promise<MeResponse>
}

/** defaultFetch keeps the browser's fetch bound to the window. */
function defaultFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return globalThis.fetch(input, init)
}

/** createMeApi returns the permissions client of the panel. */
export function createMeApi(fetchImpl: typeof fetch = defaultFetch): MeApi {
  const http = createHttpClient(fetchImpl)
  return {
    get: () => http.get<MeResponse>('/api/v1/me'),
  }
}
