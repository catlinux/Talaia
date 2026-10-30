import type { Container } from '../../api/containers'
import type { Permission } from '../../api/me'
import type { PermissionScope } from '../../auth/permissions'

export type ContainerBulkAction = 'start' | 'stop' | 'restart'
export type StackBulkAction = 'restart' | 'pull'

export interface BulkAvailability {
  allowed: boolean
  denied: number
}

export type CanRun = (permission: Permission, scope?: PermissionScope) => boolean

export interface ContainerBulkTarget {
  serverId: string
  container: Container
}

export interface StackBulkTarget {
  serverId: string
  stack: string
}

function availability(targets: Array<{ allowed: boolean }>): BulkAvailability {
  const denied = targets.filter((target) => !target.allowed).length
  return { allowed: targets.length > 0 && denied === 0, denied }
}

/** containerBulkAvailability checks who may run each bulk action on the selection. */
export function containerBulkAvailability(
  targets: ContainerBulkTarget[],
  can: CanRun,
): Record<ContainerBulkAction, BulkAvailability> {
  const evaluated = targets.map((target) => ({
    allowed: can('container.control', {
      serverId: target.serverId,
      stack: target.container.composeProject || undefined,
      container: target.container.name,
    }),
  }))
  return {
    start: availability(evaluated),
    stop: availability(evaluated),
    restart: availability(evaluated),
  }
}

/** stackBulkAvailability checks who may run each bulk action on the selected stacks. */
export function stackBulkAvailability(
  targets: StackBulkTarget[],
  can: CanRun,
): Record<StackBulkAction, BulkAvailability> {
  const evaluated = targets.map((target) => ({
    allowed: can('stack.deploy', { serverId: target.serverId, stack: target.stack }),
  }))
  return {
    restart: availability(evaluated),
    pull: availability(evaluated),
  }
}
