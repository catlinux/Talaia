import { createHttpClient } from './http'

/** TerminalShell is a shell the agent can start; the list is closed on the server. */
export type TerminalShell = 'sh' | 'bash'

/** TerminalSession is a shell the core started, waiting for the browser to attach. */
export interface TerminalSession {
  sessionId: string
  /** ticket is shown once to the WebSocket and lives 30 seconds. */
  ticket: string
  /** privileged says the container is as good as root on the server. */
  privileged: boolean
}

/** SocketLike is the part of WebSocket the terminal uses, so tests can replace it. */
export interface SocketLike {
  /** readyState is that of a WebSocket; a test double may leave it out. */
  readyState?: number
  binaryType: BinaryType
  onopen: ((event: Event) => void) | null
  onmessage: ((event: MessageEvent) => void) | null
  onclose: ((event: CloseEvent) => void) | null
  onerror: ((event: Event) => void) | null
  send(data: string | Blob | BufferSource): void
  close(code?: number, reason?: string): void
}

/** TerminalApi opens terminals and connects to them. */
export interface TerminalApi {
  open(
    serverId: string,
    container: string,
    shell: TerminalShell,
    cols: number,
    rows: number,
  ): Promise<TerminalSession>
  /** connect opens the WebSocket of a session; the caller sends the ticket first. */
  connect(sessionId: string): SocketLike
}

/** What the core sends as text on the WebSocket. */
export interface TerminalClosedMessage {
  type: 'closed'
  reason: string
  exitCode: number
}

/** Close codes of the core, besides the standard ones. */
export const closeBadTicket = 4401
export const closeSessionRevoked = 4403

/** parseTerminalMessage reads a text message of the core, or null if it is not one we know. */
export function parseTerminalMessage(text: string): TerminalClosedMessage | null {
  try {
    const body = JSON.parse(text) as { type?: unknown; reason?: unknown; exit_code?: unknown }
    if (body.type !== 'closed') return null
    return {
      type: 'closed',
      reason: typeof body.reason === 'string' ? body.reason : '',
      exitCode: typeof body.exit_code === 'number' ? body.exit_code : 0,
    }
  } catch {
    return null
  }
}

/** socketUrl is the WebSocket address of a session on the origin of the panel. */
export function socketUrl(
  sessionId: string,
  location: Pick<Location, 'protocol' | 'host'>,
): string {
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${scheme}//${location.host}/api/v1/terminal/${encodeURIComponent(sessionId)}/ws`
}

interface apiTerminalSession {
  session_id: string
  ticket: string
  privileged: boolean
}

/** createTerminalApi returns the terminal API, optionally over an injected fetch. */
export function createTerminalApi(fetchImpl?: typeof fetch): TerminalApi {
  const http = createHttpClient(fetchImpl)

  return {
    open: async (serverId, container, shell, cols, rows) => {
      const body = await http.post<apiTerminalSession>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/containers/${encodeURIComponent(container)}/terminal`,
        { confirm: true, shell, cols, rows },
      )
      return { sessionId: body.session_id, ticket: body.ticket, privileged: body.privileged }
    },
    connect: (sessionId) => new WebSocket(socketUrl(sessionId, window.location)),
  }
}
