import type { Alert } from '../../api/alerts'
import type { Permission } from '../../api/me'
import type { PermissionScope } from '../../auth/permissions'

export type AlertState = 'new' | 'seen' | 'silenced'
export type SeverityFilter = 'all' | 'critical' | 'warning' | 'info'
export type StateFilter = 'all' | AlertState
export type InboxGroup = 'none' | 'server' | 'rule'
export type BulkAction = 'acknowledge' | 'silence' | 'unwatch'

export interface InboxFilters {
  severity: SeverityFilter
  state: StateFilter
  server: string
  rule: string
  q: string
  group: InboxGroup
}

export type AlertPermissionCheck = (permission: Permission, scope: PermissionScope) => boolean

export interface BulkEligibility {
  allowed: Alert[]
  denied: Alert[]
}

export interface SilenceScope {
  scopeType: string
  serverId?: string
  name?: string
  ruleType?: string
}

export interface InboxCounts {
  critical: number
  warning: number
  new: number
}

const severityRank: Record<string, number> = { critical: 3, warning: 2, info: 1 }
const stateRank: Record<AlertState, number> = { new: 0, seen: 1, silenced: 2 }

/** alertState distinguishes an unseen alert from one already acknowledged or silenced. */
export function alertState(alert: Alert): AlertState {
  if (alert.silenced) return 'silenced'
  if (alert.ackedAt !== '' || alert.ackedBy !== '') return 'seen'
  return 'new'
}

/** inboxCounts returns the numbers shown above the inbox. */
export function inboxCounts(alerts: Alert[]): InboxCounts {
  return {
    critical: alerts.filter((alert) => alert.severity === 'critical').length,
    warning: alerts.filter((alert) => alert.severity === 'warning').length,
    new: alerts.filter((alert) => alertState(alert) === 'new').length,
  }
}

/** alertScope is the permission scope of one alert. */
export function alertScope(alert: Alert): PermissionScope {
  return {
    serverId: alert.serverId ?? undefined,
    stack: alert.stack || undefined,
    container: alert.subjectKind === 'container' ? alert.subjectName : undefined,
  }
}

/** filterAlerts applies the inbox filters; messageOf lets the UI search translated text. */
export function filterAlerts(
  alerts: Alert[],
  filters: InboxFilters,
  messageOf?: (alert: Alert) => string,
): Alert[] {
  const query = filters.q.trim().toLocaleLowerCase()
  return alerts.filter((alert) => {
    if (filters.severity !== 'all' && alert.severity !== filters.severity) return false
    if (filters.state !== 'all' && alertState(alert) !== filters.state) return false
    if (filters.server !== '' && alert.serverId !== filters.server) return false
    if (filters.rule !== '' && alert.ruleType !== filters.rule) return false
    if (query === '') return true
    const summary = alert.summary
    const text = [
      messageOf?.(alert) ?? '',
      alert.serverName,
      alert.serverId ?? '',
      alert.stack,
      alert.subjectName,
      alert.ruleType,
      summary?.key ?? '',
      ...Object.values(summary?.params ?? {}).map(String),
    ]
      .join(' ')
      .toLocaleLowerCase()
    return text.includes(query)
  })
}

/** defaultSort puts new alerts first, then severity and finally the oldest alert. */
export function defaultSort(left: Alert, right: Alert): number {
  const state = stateRank[alertState(left)] - stateRank[alertState(right)]
  if (state !== 0) return state
  const severity = (severityRank[right.severity] ?? 0) - (severityRank[left.severity] ?? 0)
  if (severity !== 0) return severity
  const started = left.startedAt.localeCompare(right.startedAt)
  if (started !== 0) return started
  return left.id.localeCompare(right.id)
}

/** groupKey returns the key used by the table grouping. */
export function groupKey(alert: Alert, group: InboxGroup): string {
  if (group === 'server') return alert.serverId ?? alert.serverName
  if (group === 'rule') return alert.ruleType
  return ''
}

/** eligibility calculates which selected alerts each bulk action may touch. */
export function eligibility(
  alerts: Alert[],
  can: AlertPermissionCheck,
): Record<BulkAction, BulkEligibility> {
  const partition = (applicable: (alert: Alert) => boolean): BulkEligibility => {
    const allowed: Alert[] = []
    const denied: Alert[] = []
    for (const alert of alerts) {
      if (applicable(alert) && can('alerts.silence', alertScope(alert))) allowed.push(alert)
      else denied.push(alert)
    }
    return { allowed, denied }
  }

  return {
    acknowledge: partition((alert) => alertState(alert) !== 'seen'),
    silence: partition(() => true),
    unwatch: partition((alert) => alert.subjectKind === 'container'),
  }
}

/** silenceScope returns the server, stack or container scope of one alert. */
export function silenceScope(alert: Alert): {
  scopeType: string
  serverId?: string
  name?: string
} {
  if (!alert.serverId) return { scopeType: 'organization' }
  if (alert.subjectKind === 'container') {
    return { scopeType: 'container', serverId: alert.serverId, name: alert.subjectName }
  }
  if (alert.stack !== '') {
    return { scopeType: 'stack', serverId: alert.serverId, name: alert.stack }
  }
  return { scopeType: 'server', serverId: alert.serverId }
}

/** distinctSilenceScopes removes repeated scopes from a bulk silence selection. */
export function distinctSilenceScopes(alerts: Alert[]): SilenceScope[] {
  const scopes = new Map<string, SilenceScope>()
  for (const alert of alerts) {
    const scope = { ...silenceScope(alert), ruleType: alert.ruleType || undefined }
    const key = JSON.stringify([
      scope.scopeType,
      scope.serverId ?? '',
      scope.name ?? '',
      scope.ruleType ?? '',
    ])
    if (!scopes.has(key)) scopes.set(key, scope)
  }
  return [...scopes.values()]
}
