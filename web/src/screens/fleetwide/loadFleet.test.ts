import { describe, expect, it } from 'vitest'

import type { Container, ContainersApi, ContainerList, StackList } from '../../api/containers'
import type { FleetClient } from '../../api/client'
import type { Server } from '../../api/types'
import { loadFleet } from './loadFleet'

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: 'srv-1',
    name: 'vps-web-01',
    os: 'Debian 12',
    location: '',
    vcpu: 2,
    memoryGb: 4,
    status: 'online',
    cpuPercent: 10,
    memPercent: 20,
    diskPercent: 30,
    containers: 2,
    stacks: 1,
    tags: [],
    lastSeen: '2026-10-19T12:00:00Z',
    agentVersion: '0.2.0',
    agentLatestVersion: '0.2.8',
    agentUpdateAvailable: false,
    agentUpdateManual: false,
    agentUpgradeCommand: '',
    ...overrides,
  }
}

function container(overrides: Partial<Container> = {}): Container {
  return {
    id: 'c1',
    name: 'web',
    image: 'nginx',
    state: 'running',
    status: 'Up',
    composeProject: 'shop',
    composeService: 'web',
    ...overrides,
  }
}

function list(containers: Container[] = [container()], connected = true): ContainerList {
  return { connected, dockerAvailable: true, containers }
}

function stacks(names: string[] = ['shop'], extra: Partial<StackList> = {}): StackList {
  return {
    connected: true,
    stacks: names,
    adopted: [],
    broken: [],
    adoptable: [],
    ...extra,
  }
}

function fakeApi(
  lists: Record<string, () => Promise<ContainerList>>,
  stackLists: Record<string, () => Promise<StackList>> = {},
): ContainersApi {
  const unexpected = () => Promise.reject(new Error('unexpected call'))
  return {
    list: (serverId) => (lists[serverId] ?? unexpected)(),
    run: unexpected,
    stacks: (serverId) => (stackLists[serverId] ?? (() => Promise.resolve(stacks())))(),
    writeStack: unexpected,
    readStack: unexpected,
    listResources: unexpected,
    removeResources: unexpected,
    adoptStack: unexpected,
    runStack: unexpected,
    logs: unexpected,
    job: unexpected,
  }
}

function client(servers: Server[]): FleetClient {
  return {
    listServers: () => Promise.resolve(servers),
    listActivity: () => Promise.resolve([]),
    createEnrollment: () => Promise.reject(new Error('unexpected enrollment')),
  }
}

describe('loadFleet', () => {
  it('keeps the other servers when one fails', async () => {
    const one = server({ id: 'one', name: 'one' })
    const two = server({ id: 'two', name: 'two' })
    const api = fakeApi({
      one: () => Promise.resolve(list([container()])),
      two: () => Promise.reject(new Error('agent down')),
    })

    const result = await loadFleet(client([one, two]), api)

    expect(result.failed).toEqual([{ serverId: 'two', serverName: 'two', message: 'agent down' }])
    expect(result.containers.map((row) => row.serverId)).toEqual(['one'])
  })

  it('reports offline, limited and disconnected servers without listing them', async () => {
    const servers = [
      server({ id: 'offline', name: 'offline', status: 'offline' }),
      server({ id: 'limited', name: 'limited', limited: true }),
      server({ id: 'down', name: 'down' }),
    ]
    const api = fakeApi({
      down: () => Promise.resolve(list([], false)),
    })

    const result = await loadFleet(client(servers), api)

    expect(result.notConnected).toEqual(['offline', 'limited', 'down'])
    expect(result.containers).toEqual([])
  })

  it('builds stack rows and adoptable projects from the lists', async () => {
    const api = fakeApi(
      { one: () => Promise.resolve(list([container()])) },
      {
        one: () =>
          Promise.resolve(
            stacks(['shop'], {
              adopted: ['shop'],
              broken: ['old'],
              adoptable: ['orphan'],
            }),
          ),
      },
    )

    const result = await loadFleet(client([server({ id: 'one', name: 'one' })]), api)

    expect(result.stacks).toEqual([
      {
        serverId: 'one',
        serverName: 'one',
        stack: 'shop',
        containers: [container()],
        adopted: true,
        broken: false,
      },
    ])
    expect(result.adoptable).toEqual([{ serverId: 'one', serverName: 'one', name: 'orphan' }])
  })

  it('limits concurrent server reads to four', async () => {
    let active = 0
    let maximum = 0
    const servers = Array.from({ length: 8 }, (_, index) =>
      server({ id: `srv-${index}`, name: `srv-${index}` }),
    )
    const api = fakeApi(
      Object.fromEntries(
        servers.map((item) => [
          item.id,
          () => {
            active += 1
            maximum = Math.max(maximum, active)
            return new Promise<ContainerList>((resolve) => {
              setTimeout(() => {
                active -= 1
                resolve(list([]))
              }, 5)
            })
          },
        ]),
      ),
    )

    await loadFleet(client(servers), api)

    expect(maximum).toBeLessThanOrEqual(4)
  })
})
