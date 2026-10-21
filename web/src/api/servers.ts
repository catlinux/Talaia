import { ApiError, createHttpClient } from './http'

/** ServerSummary is one enrolled server as the Settings screen needs it. */
export interface ServerSummary {
  id: string
  name: string
  connected: boolean
  revoked: boolean
}

/** ServersApi lists the enrolled servers and removes one for good. */
export interface ServersApi {
  listServers(): Promise<ServerSummary[]>
  /** removeServer soft-removes the server and revokes its certificate. */
  removeServer(id: string): Promise<void>
}

interface apiServerSummary {
  id: string
  name: string
  connected: boolean
  revoked: boolean
}

interface serversResponse {
  servers: apiServerSummary[]
}

/** defaultFetch keeps the browser's fetch bound to the window. */
function defaultFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return globalThis.fetch(input, init)
}

/** createServersApi returns the server list and removal API of the core. */
export function createServersApi(fetchImpl: typeof fetch = defaultFetch): ServersApi {
  const http = createHttpClient(fetchImpl)

  return {
    listServers: async () => {
      const body = await http.get<serversResponse>('/api/v1/servers')
      return body.servers.map((server) => ({
        id: server.id,
        name: server.name,
        connected: server.connected,
        revoked: server.revoked,
      }))
    },
    removeServer: async (id) => {
      let response: Response
      try {
        response = await fetchImpl(`/api/v1/servers/${encodeURIComponent(id)}`, {
          method: 'DELETE',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ confirm: true }),
        })
      } catch {
        throw new ApiError('network', 0)
      }
      if (!response.ok) {
        const code =
          response.status === 401
            ? 'session_required'
            : response.status === 404
              ? 'not_found'
              : response.status === 400
                ? 'confirmation_required'
                : 'internal'
        throw new ApiError(code, response.status)
      }
    },
  }
}
