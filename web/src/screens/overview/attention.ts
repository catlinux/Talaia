import type { Alert } from '../../api/alerts'
import type { Server } from '../../api/types'

export type AttentionTone = 'crit' | 'warn' | 'info'
export type AttentionKind = 'alert' | 'offline' | 'disk' | 'security' | 'reboot' | 'agent'
export type AttentionActionKind = 'alert' | 'server' | 'updates' | 'agent'

export interface AttentionAction {
  kind: AttentionActionKind
  to: string
}

/** AttentionItem is one actionable row of the Overview list. */
export interface AttentionItem {
  id: string
  priority: number
  tone: AttentionTone
  kind: AttentionKind
  serverId?: string
  serverName: string
  params: Record<string, string | number>
  action: AttentionAction
}

interface Candidate extends AttentionItem {
  /** since is the source timestamp used to put the oldest items first. */
  since: number
}

const severityRank: Record<AttentionTone, number> = { crit: 3, warn: 2, info: 1 }

function timestamp(value: string, now: Date): number {
  const parsed = Date.parse(value)
  if (Number.isNaN(parsed)) return 0
  // A clock that is ahead of the panel must not push an item to the front.
  return Math.min(parsed, now.getTime())
}

function serverPath(server: Server): string {
  return `/servers/${encodeURIComponent(server.id)}`
}

function updatesPath(server: Server): string {
  return `${serverPath(server)}/updates`
}

function alertServerName(alert: Alert): string {
  return alert.serverName || alert.serverId || '—'
}

/** buildAttention derives the actionable list of the Overview. */
export function buildAttention(servers: Server[], alerts: Alert[], now: Date): AttentionItem[] {
  const diskAlerts = new Set(
    alerts
      .filter(
        (alert) =>
          (alert.ruleType === 'disk_usage' || alert.ruleType === 'disk_high') &&
          alert.serverId !== null,
      )
      .map((alert) => alert.serverId as string),
  )
  const offlineAlerts = new Set(
    alerts
      .filter((alert) => alert.ruleType === 'server_offline' && alert.serverId !== null)
      .map((alert) => alert.serverId as string),
  )

  const candidates: Candidate[] = []

  for (const alert of alerts) {
    if (
      alert.silenced ||
      alert.ackedAt !== '' ||
      (alert.severity !== 'critical' && alert.severity !== 'warning')
    ) {
      continue
    }
    const critical = alert.severity === 'critical'
    candidates.push({
      id: alert.id,
      priority: critical ? 1 : 3,
      tone: critical ? 'crit' : 'warn',
      kind: 'alert',
      ...(alert.serverId === null ? {} : { serverId: alert.serverId }),
      serverName: alertServerName(alert),
      params: alert.summary?.params ?? {},
      action: { kind: 'alert', to: `/alerts?focus=${encodeURIComponent(alert.id)}` },
      since: timestamp(alert.startedAt, now),
    })
  }

  for (const server of servers) {
    if (server.limited === true) continue

    if (server.status === 'offline' && !offlineAlerts.has(server.id)) {
      candidates.push({
        id: `offline:${server.id}`,
        priority: 2,
        tone: 'crit',
        kind: 'offline',
        serverId: server.id,
        serverName: server.name,
        params: {},
        action: { kind: 'server', to: serverPath(server) },
        since: timestamp(server.lastSeen ?? '', now),
      })
    }

    const disk = server.diskPercent
    if (disk !== null && disk >= 85 && !diskAlerts.has(server.id)) {
      candidates.push({
        id: `disk:${server.id}`,
        priority: 4,
        tone: disk >= 95 ? 'crit' : 'warn',
        kind: 'disk',
        serverId: server.id,
        serverName: server.name,
        params: { percent: disk },
        action: { kind: 'server', to: serverPath(server) },
        since: 0,
      })
    }

    const security = server.apt?.security ?? 0
    if (security > 0) {
      candidates.push({
        id: `security:${server.id}`,
        priority: 5,
        tone: 'warn',
        kind: 'security',
        serverId: server.id,
        serverName: server.name,
        params: { count: security },
        action: { kind: 'updates', to: updatesPath(server) },
        since: 0,
      })
    }

    if (server.apt?.rebootRequired === true) {
      candidates.push({
        id: `reboot:${server.id}`,
        priority: 6,
        tone: 'info',
        kind: 'reboot',
        serverId: server.id,
        serverName: server.name,
        params: {},
        action: { kind: 'updates', to: updatesPath(server) },
        since: 0,
      })
    }

    if (server.agentUpdateAvailable) {
      candidates.push({
        id: `agent:${server.id}`,
        priority: 7,
        tone: 'info',
        kind: 'agent',
        serverId: server.id,
        serverName: server.name,
        params: { manual: server.agentUpdateManual ? 1 : 0 },
        action: { kind: 'agent', to: serverPath(server) },
        since: 0,
      })
    }
  }

  candidates.sort(
    (left, right) =>
      left.priority - right.priority ||
      severityRank[right.tone] - severityRank[left.tone] ||
      left.since - right.since ||
      left.serverName.localeCompare(right.serverName) ||
      left.id.localeCompare(right.id),
  )

  return candidates.map((candidate) => ({
    id: candidate.id,
    priority: candidate.priority,
    tone: candidate.tone,
    kind: candidate.kind,
    ...(candidate.serverId === undefined ? {} : { serverId: candidate.serverId }),
    serverName: candidate.serverName,
    params: candidate.params,
    action: candidate.action,
  }))
}
