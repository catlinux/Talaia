import type { Container, ContainersApi } from '../../api/containers'
import type { FleetClient } from '../../api/client'
import type { Server } from '../../api/types'

export interface FleetContainerRow {
  serverId: string
  serverName: string
  container: Container
}

export interface FleetStackRow {
  serverId: string
  serverName: string
  stack: string
  containers: Container[]
  adopted: boolean
  broken: boolean
}

export interface FleetAdoptableRow {
  serverId: string
  serverName: string
  name: string
}

export interface FleetServerError {
  serverId: string
  serverName: string
  message: string
}

export interface FleetLoad {
  servers: Server[]
  containers: FleetContainerRow[]
  stacks: FleetStackRow[]
  adoptable: FleetAdoptableRow[]
  failed: FleetServerError[]
  notConnected: string[]
}

interface LoadFleetOptions {
  /** canLoad skips servers where the user has no permission to read the containers. */
  canLoad?: (server: Server) => boolean
  /** canLoadStacks skips the stack list on servers where the user cannot read it. */
  canLoadStacks?: (server: Server) => boolean
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : ''
}

function attempt<T>(call: () => Promise<T>): Promise<PromiseSettledResult<T>> {
  return Promise.resolve()
    .then(call)
    .then(
      (value) => ({ status: 'fulfilled' as const, value }),
      (reason: unknown) => ({ status: 'rejected' as const, reason }),
    )
}

async function mapLimit<T>(
  items: T[],
  limit: number,
  task: (item: T) => Promise<void>,
): Promise<void> {
  let next = 0
  const workers = Array.from({ length: Math.min(limit, items.length) }, async () => {
    for (;;) {
      const index = next
      next += 1
      if (index >= items.length) return
      await task(items[index]!)
    }
  })
  await Promise.all(workers)
}

/** loadFleet loads every server's containers and stacks, in small parallel batches. */
export async function loadFleet(
  client: FleetClient,
  containersApi: ContainersApi,
  options: LoadFleetOptions = {},
): Promise<FleetLoad> {
  const servers = await client.listServers()
  const result: FleetLoad = {
    servers,
    containers: [],
    stacks: [],
    adoptable: [],
    failed: [],
    notConnected: [],
  }

  await mapLimit(servers, 4, async (server) => {
    if (options.canLoad !== undefined && !options.canLoad(server)) return
    if (server.limited === true || server.status === 'offline') {
      result.notConnected.push(server.name)
      return
    }

    const listResult = await attempt(() => containersApi.list(server.id))
    if (listResult.status === 'rejected') {
      result.failed.push({
        serverId: server.id,
        serverName: server.name,
        message: errorText(listResult.reason),
      })
      return
    }
    if (!listResult.value.connected) {
      result.notConnected.push(server.name)
      return
    }

    const containers = listResult.value.containers
    for (const container of containers) {
      result.containers.push({
        serverId: server.id,
        serverName: server.name,
        container,
      })
    }

    if (!(options.canLoadStacks?.(server) ?? true)) return

    const stacksResult = await attempt(() => containersApi.stacks(server.id))
    if (stacksResult.status === 'rejected') {
      result.failed.push({
        serverId: server.id,
        serverName: server.name,
        message: errorText(stacksResult.reason),
      })
      return
    }
    if (!stacksResult.value.connected) return

    const adopted = new Set(stacksResult.value.adopted)
    const broken = new Set(stacksResult.value.broken)
    for (const stack of stacksResult.value.stacks) {
      result.stacks.push({
        serverId: server.id,
        serverName: server.name,
        stack,
        containers: containers.filter((container) => container.composeProject === stack),
        adopted: adopted.has(stack),
        broken: broken.has(stack),
      })
    }
    for (const name of stacksResult.value.adoptable) {
      result.adoptable.push({ serverId: server.id, serverName: server.name, name })
    }
  })

  return result
}
