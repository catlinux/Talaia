import type { Alert } from '../../api/alerts'
import type { Permission } from '../../api/me'
import type { PermissionScope } from '../../auth/permissions'
import { alertScope } from './inboxModel'

export type AlertCauseActionID =
  | 'open-server'
  | 'start-container'
  | 'restart-container'
  | 'logs'
  | 'terminal'
  | 'open-stack'
  | 'open-services'
  | 'view-updates'
  | 'cleanup-resources'

export interface AlertCauseAction {
  id: AlertCauseActionID
  kind: 'link' | 'container'
  href?: string
  containerAction?: 'start' | 'restart'
}

export type AlertActionPermission = (permission: Permission, scope: PermissionScope) => boolean

function serverPath(alert: Alert, suffix = ''): string | undefined {
  if (!alert.serverId) return undefined
  return `/servers/${encodeURIComponent(alert.serverId)}${suffix}`
}

/** alertActions decides the direct actions on the cause of an alert. */
export function alertActions(alert: Alert, can: AlertActionPermission): AlertCauseAction[] {
  const scope = alertScope(alert)
  const actions: AlertCauseAction[] = []

  if (alert.subjectKind === 'container' && alert.serverId) {
    if (can('container.control', scope)) {
      if (alert.ruleType === 'container_down') {
        actions.push({ id: 'start-container', kind: 'container', containerAction: 'start' })
      } else if (alert.ruleType === 'container_unhealthy') {
        actions.push({ id: 'restart-container', kind: 'container', containerAction: 'restart' })
      }
    }
    if (can('container.logs', scope)) {
      actions.push({
        id: 'logs',
        kind: 'link',
        href: serverPath(alert, '/containers'),
      })
    }
    if (can('container.terminal', scope)) {
      actions.push({
        id: 'terminal',
        kind: 'link',
        href: serverPath(alert, '/containers'),
      })
    }
  }

  const server = serverPath(alert)
  if (alert.ruleType === 'server_offline') {
    if (server) actions.push({ id: 'open-server', kind: 'link', href: server })
    return actions
  }

  if (alert.ruleType === 'check_failed') {
    const services = serverPath(alert, '/services')
    if (services) actions.push({ id: 'open-services', kind: 'link', href: services })
  }

  if (alert.subjectKind === 'stack' && alert.stack !== '') {
    const stack = serverPath(alert, `/stacks/${encodeURIComponent(alert.stack)}`)
    if (stack) actions.push({ id: 'open-stack', kind: 'link', href: stack })
  }

  if (alert.ruleType === 'disk_high') {
    if (server) actions.push({ id: 'open-server', kind: 'link', href: server })
    const cleanup = serverPath(alert, '/containers')
    if (cleanup) actions.push({ id: 'cleanup-resources', kind: 'link', href: cleanup })
    return actions
  }

  if (alert.ruleType === 'cpu_high' || alert.ruleType === 'mem_high') {
    if (server) actions.push({ id: 'open-server', kind: 'link', href: server })
    return actions
  }

  if (alert.ruleType === 'cert_expiry' && server) {
    actions.push({ id: 'open-server', kind: 'link', href: server })
  }

  return actions
}
