import { createHttpClient } from './http'

/** AuditEntry is one event of the append-only audit log. */
export interface AuditEntry {
  id: string
  /** at is the RFC 3339 timestamp of the event. */
  at: string
  actor: string
  action: string
  ip: string
  result: string
  details: Record<string, unknown> | null
}

/** AuditPage is one page of the audit log, newest first. */
export interface AuditPage {
  entries: AuditEntry[]
  /** next is the opaque cursor of the next page, or empty. */
  next: string
}

/** AuditQuery are the optional filters of the audit endpoint. */
export interface AuditQuery {
  limit?: number
  before?: string
  action?: string
  serverId?: string
  actor?: string
}

/** AuditApi reads the audit log. */
export interface AuditApi {
  list(query?: AuditQuery): Promise<AuditPage>
}

interface apiAuditEntry {
  id?: string
  at?: string
  actor?: string
  action?: string
  ip?: string
  result?: string
  details?: Record<string, unknown> | null
}

interface apiAuditPage {
  entries?: apiAuditEntry[]
  next?: string
}

function queryString(query: AuditQuery): string {
  const params = new URLSearchParams()
  if (query.limit !== undefined) params.set('limit', String(query.limit))
  if (query.before) params.set('before', query.before)
  if (query.action) params.set('action', query.action)
  if (query.serverId) params.set('server_id', query.serverId)
  if (query.actor) params.set('actor', query.actor)
  const encoded = params.toString()
  return encoded === '' ? '' : `?${encoded}`
}

/** createAuditApi returns the read-only audit API of the core. */
export function createAuditApi(fetchImpl?: typeof fetch): AuditApi {
  const http = createHttpClient(fetchImpl)

  return {
    list: async (query = {}) => {
      const body = await http.get<apiAuditPage>(`/api/v1/audit${queryString(query)}`)
      return {
        entries: (body.entries ?? []).map((entry) => ({
          id: entry.id ?? '',
          at: entry.at ?? '',
          actor: entry.actor ?? '',
          action: entry.action ?? '',
          ip: entry.ip ?? '',
          result: entry.result ?? '',
          details: entry.details ?? null,
        })),
        next: body.next ?? '',
      }
    },
  }
}
