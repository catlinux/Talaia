import { ApiError, createHttpClient } from './http'

/** ServerSummary is one enrolled server as the Settings screen needs it. */
export interface ServerSummary {
  id: string
  name: string
  connected: boolean
  revoked: boolean
  /** agentVersion is the version the connected agent reports; empty when offline. */
  agentVersion: string
  /** agentLatestVersion is the newest agent package the core can offer. */
  agentLatestVersion: string
  /** agentUpdateAvailable is true when the core offers a newer agent package. */
  agentUpdateAvailable: boolean
  /** agentUpdateManual is true when the agent is too old to update itself. */
  agentUpdateManual: boolean
  /** agentUpgradeCommand upgrades such an old agent once, by hand. */
  agentUpgradeCommand: string
}

/** AgentUpdateSkip is one server the bulk update could not queue. */
export interface AgentUpdateSkip {
  id: string
  reason: string
}

/** AgentUpdateAllResult is the answer of the bulk update endpoint. */
export interface AgentUpdateAllResult {
  queued: string[]
  skipped: AgentUpdateSkip[]
}

/** AgentUpdateErrorCode is the reason a single agent update was refused. */
export type AgentUpdateErrorCode =
  | 'agent_offline'
  | 'no_update'
  | 'update_in_progress'
  | 'confirmation_required'
  | 'session_required'
  | 'not_found'
  | 'internal'

/** AgentUpdateError is a refused agent update, with the stable code of the core. */
export class AgentUpdateError extends Error {
  readonly code: AgentUpdateErrorCode
  readonly status: number

  constructor(code: AgentUpdateErrorCode, status: number) {
    super(`${code} (${status})`)
    this.name = 'AgentUpdateError'
    this.code = code
    this.status = status
  }
}

/** ServersApi lists the enrolled servers, removes one and updates its agent. */
export interface ServersApi {
  listServers(): Promise<ServerSummary[]>
  /** removeServer soft-removes the server and revokes its certificate. */
  removeServer(id: string): Promise<void>
  /** updateAgent asks the core to send the newer package to one server. */
  updateAgent(id: string): Promise<void>
  /** updateAllAgents queues the update of every connected server that has one. */
  updateAllAgents(): Promise<AgentUpdateAllResult>
}

interface apiServerSummary {
  id: string
  name: string
  connected: boolean
  revoked: boolean
  agent_update_available: boolean
  agent_update_manual: boolean
  live?: {
    agent_version: string
  }
}

interface apiAgentUpdateAll {
  queued: string[]
  skipped: AgentUpdateSkip[]
}

interface serversResponse {
  servers: apiServerSummary[]
  agent_latest_version: string
  agent_upgrade_command: string
}

/** defaultFetch keeps the browser's fetch bound to the window. */
function defaultFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return globalThis.fetch(input, init)
}

/** errorCodeOfResponse reads {error:{code}} from a failed response. */
async function errorCodeOfResponse(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as { error?: { code?: unknown } }
    return typeof body.error?.code === 'string' ? body.error.code : ''
  } catch {
    return ''
  }
}

/** agentUpdateCode maps the core code or the status to a stable client code. */
function agentUpdateCode(code: string, status: number): AgentUpdateErrorCode {
  switch (code) {
    case 'agent_offline':
      return 'agent_offline'
    case 'no_update':
      return 'no_update'
    case 'update_in_progress':
      return 'update_in_progress'
    case 'confirmation_required':
      return 'confirmation_required'
    case 'session_required':
      return 'session_required'
    case 'not_found':
      return 'not_found'
    default:
      break
  }
  switch (status) {
    case 400:
      return 'confirmation_required'
    case 401:
      return 'session_required'
    case 404:
      return 'not_found'
    default:
      return 'internal'
  }
}

/** createServersApi returns the server list and removal API of the core. */
export function createServersApi(fetchImpl: typeof fetch = defaultFetch): ServersApi {
  const http = createHttpClient(fetchImpl)

  return {
    listServers: async () => {
      const body = await http.get<serversResponse>('/api/v1/servers')
      const latestVersion = body.agent_latest_version ?? ''
      const upgradeCommand = body.agent_upgrade_command ?? ''
      return body.servers.map((server) => ({
        id: server.id,
        name: server.name,
        connected: server.connected,
        revoked: server.revoked,
        agentVersion: server.live?.agent_version ?? '',
        agentLatestVersion: latestVersion,
        agentUpdateAvailable: server.agent_update_available ?? false,
        agentUpdateManual: server.agent_update_manual ?? false,
        agentUpgradeCommand: upgradeCommand,
      }))
    },
    updateAgent: async (id) => {
      let response: Response
      try {
        response = await fetchImpl(`/api/v1/servers/${encodeURIComponent(id)}/agent/update`, {
          method: 'POST',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ confirm: true }),
        })
      } catch {
        throw new AgentUpdateError('internal', 0)
      }
      if (!response.ok) {
        const code = await errorCodeOfResponse(response)
        throw new AgentUpdateError(agentUpdateCode(code, response.status), response.status)
      }
    },
    updateAllAgents: async () => {
      const body = await http.post<apiAgentUpdateAll>('/api/v1/agents/update-all', {
        confirm: true,
      })
      return {
        queued: body.queued ?? [],
        skipped: body.skipped ?? [],
      }
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
