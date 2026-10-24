import { createHttpClient } from './http'

/** Alert is one firing or historical rule match. */
export interface Alert {
  id: string
  ruleId: string | null
  ruleType: string
  severity: string
  state: string
  serverId: string | null
  serverName: string
  subjectKind: string
  subjectName: string
  stack: string
  startedAt: string
  resolvedAt: string
  ackedAt: string
  ackedBy: string
  silenced: boolean
  summary: { key: string; params: Record<string, string | number> } | null
}

/** AlertSilence is one active silence. */
export interface AlertSilence {
  id: string
  scopeType: string
  serverId: string | null
  groupId: string | null
  name: string | null
  ruleType: string | null
  reason: string
  createdBy: string
  createdAt: string
  endsAt: string
}

/** AlertSummary is the badge and fleet summary returned by the core. */
export interface AlertSummary {
  counts: { warning: number; critical: number; info?: number }
  servers: Record<string, string>
}

/** SilenceInput is the body of a silence request. */
export interface SilenceInput {
  scopeType: string
  serverId?: string
  groupId?: string
  name?: string
  ruleType?: string
  durationMinutes: number
  reason: string
}

/** AlertsApi is the alert and silence API of the core. */
export interface AlertsApi {
  list(state?: 'active' | 'history', offset?: number, serverId?: string): Promise<Alert[]>
  summary(): Promise<AlertSummary>
  silences(): Promise<AlertSilence[]>
  acknowledge(id: string): Promise<void>
  unwatch(id: string): Promise<void>
  createSilence(input: SilenceInput): Promise<AlertSilence>
  deleteSilence(id: string): Promise<void>
}

type raw = Record<string, unknown>

function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function nullableText(value: unknown): string | null {
  return typeof value === 'string' && value !== '' ? value : null
}

function summaryOf(value: unknown): Alert['summary'] {
  if (typeof value !== 'object' || value === null) return null
  const record = value as Record<string, unknown>
  if (typeof record.key !== 'string') return null
  const params: Record<string, string | number> = {}
  if (typeof record.params === 'object' && record.params !== null) {
    for (const [key, item] of Object.entries(record.params as Record<string, unknown>)) {
      if (typeof item === 'string' || typeof item === 'number') params[key] = item
    }
  }
  return { key: record.key, params }
}

function mapAlert(value: raw): Alert {
  return {
    id: text(value.id),
    ruleId: nullableText(value.rule_id),
    ruleType: text(value.rule_type),
    severity: text(value.severity),
    state: text(value.state),
    serverId: nullableText(value.server_id),
    serverName: text(value.server_name),
    subjectKind: text(value.subject_kind),
    subjectName: text(value.subject_name),
    stack: text(value.stack),
    startedAt: text(value.started_at),
    resolvedAt: text(value.resolved_at),
    ackedAt: text(value.acked_at),
    ackedBy: text(value.acked_by),
    silenced: value.silenced === true,
    summary: summaryOf(value.summary),
  }
}

function mapSilence(value: raw): AlertSilence {
  return {
    id: text(value.id),
    scopeType: text(value.scope_type),
    serverId: nullableText(value.server_id),
    groupId: nullableText(value.group_id),
    name: nullableText(value.name),
    ruleType: nullableText(value.rule_type),
    reason: text(value.reason),
    createdBy: text(value.created_by),
    createdAt: text(value.created_at),
    endsAt: text(value.ends_at),
  }
}

function mapSummary(value: {
  counts?: Record<string, unknown>
  servers?: Record<string, unknown>
}): AlertSummary {
  const counts = { warning: 0, critical: 0, info: 0 }
  for (const key of ['warning', 'critical', 'info'] as const) {
    const count = value.counts?.[key]
    if (typeof count === 'number') counts[key] = count
  }
  const servers: Record<string, string> = {}
  for (const [id, severity] of Object.entries(value.servers ?? {})) {
    if (typeof severity === 'string') servers[id] = severity
  }
  return { counts, servers }
}

/** createAlertsApi returns the alert API, optionally over an injected fetch. */
export function createAlertsApi(fetchImpl?: typeof fetch): AlertsApi {
  const http = createHttpClient(fetchImpl)
  return {
    list: async (state = 'active', offset = 0, serverId) => {
      const query = new URLSearchParams({ state, limit: '50' })
      if (state === 'history') query.set('offset', String(offset))
      if (serverId) query.set('server_id', serverId)
      const rows = await http.get<raw[]>(`/api/v1/alerts?${query.toString()}`)
      return (rows ?? []).map(mapAlert)
    },
    summary: async () => {
      const body = await http.get<{
        counts?: Record<string, unknown>
        servers?: Record<string, unknown>
      }>('/api/v1/alerts/summary')
      return mapSummary(body)
    },
    silences: async () => {
      const rows = await http.get<raw[]>('/api/v1/silences')
      return (rows ?? []).map(mapSilence)
    },
    acknowledge: async (id) => {
      await http.post<void>(`/api/v1/alerts/${encodeURIComponent(id)}/ack`)
    },
    unwatch: async (id) => {
      await http.post<void>(`/api/v1/alerts/${encodeURIComponent(id)}/unwatch`)
    },
    createSilence: async (input) => {
      const body = await http.post<raw>('/api/v1/silences', {
        scope_type: input.scopeType,
        server_id: input.serverId ?? '',
        group_id: input.groupId ?? '',
        name: input.name ?? '',
        rule_type: input.ruleType ?? null,
        duration_minutes: input.durationMinutes,
        reason: input.reason,
      })
      return mapSilence(body)
    },
    deleteSilence: async (id) => {
      await http.delete<void>(`/api/v1/silences/${encodeURIComponent(id)}`)
    },
  }
}
