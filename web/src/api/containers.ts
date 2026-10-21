import { createHttpClient } from './http'

/** Container is one container of a server, as its agent reports it. */
export interface Container {
  id: string
  name: string
  image: string
  /** running | exited | paused | restarting | created | dead */
  state: string
  /** Human-readable status, as Docker words it. */
  status: string
  composeProject: string
  composeService: string
  /** use is the last sample of a running container, when the agent has reported one. */
  use?: ContainerUse
}

/** ContainerUse is the resource use of one running container. */
export interface ContainerUse {
  /** cpuPercent is of one core: 100 means one full core. */
  cpuPercent: number
  memUsedBytes: number
  /** memLimitBytes is 0 when the container has no memory limit. */
  memLimitBytes: number
  netRxBytes: number
  netTxBytes: number
}

/** ContainerList is the answer of the containers endpoint. */
export interface ContainerList {
  /** connected is false while the agent of the server has no open stream. */
  connected: boolean
  dockerAvailable: boolean
  containers: Container[]
}

export type ContainerAction = 'start' | 'stop' | 'restart' | 'remove'

/** ResourceKind is a kind of Docker resource that can be cleaned up. */
export type ResourceKind = 'image' | 'volume' | 'network'

/** ResourceItem is one image, volume or network of a server. */
export interface ResourceItem {
  /** id is what a removal needs: an image id, a volume name or a network id. */
  id: string
  name?: string
  tags?: string[]
  driver?: string
  size?: number
  created?: string
  in_use: boolean
  /** system marks the networks Docker needs and never removes. */
  system?: boolean
}

/** ResourceListing is the output of a listing job. */
export interface ResourceListing {
  kind: ResourceKind
  items: ResourceItem[]
}

/** RemovalResult is the outcome of removing one resource. */
export interface RemovalResult {
  kind: ResourceKind
  id: string
  ok: boolean
  message?: string
}

/** StackAction is a docker compose operation on a managed stack. */
export type StackAction = 'up' | 'down' | 'pull' | 'restart' | 'remove' | 'forget'

/** StackList is the answer of the stacks endpoint. */
export interface StackList {
  connected: boolean
  /** stacks are the names of the stacks Talaia manages on the server. */
  stacks: string[]
  /** adopted are the managed stacks that were adopted from a running project. */
  adopted: string[]
  /** adoptable are the running Compose projects Talaia does not manage yet. */
  adoptable: string[]
}

/** StackFiles are the files of a managed stack. */
export interface StackFiles {
  compose: string
  env: string
}

export type JobState = 'pending' | 'running' | 'succeeded' | 'failed'

/** Job is the progress of a command sent to an agent. */
export interface Job {
  id: string
  state: JobState
  /** message carries the reason of a failure. */
  message: string
  /** output is the text the job produced, for example the lines of a log. */
  output: string
}

/** LogOptions asks for an incremental log read, used by the live follow. */
export interface LogOptions {
  /** since is an RFC 3339 timestamp: only lines after it are returned. */
  since?: string
  /** timestamps prefixes each line with the time Docker saw it. */
  timestamps?: boolean
}

/** ContainersApi is the typed container API of the core. */
export interface ContainersApi {
  list(serverId: string): Promise<ContainerList>
  /** run sends the action and returns the id of the job. */
  run(
    serverId: string,
    container: string,
    action: ContainerAction,
    confirm?: boolean,
  ): Promise<string>
  stacks(serverId: string): Promise<StackList>
  /** writeStack validates and stores a stack; it returns the id of the job. */
  writeStack(serverId: string, name: string, compose: string, env: string): Promise<string>
  /** readStack asks for the files of a stack; the job output holds them as JSON. */
  readStack(serverId: string, name: string): Promise<string>
  /** listResources asks for the resources of one kind; the job output holds them as JSON. */
  listResources(serverId: string, kind: ResourceKind): Promise<string>
  /** removeResources removes the given resources (never forced); it returns the id of the job. */
  removeResources(serverId: string, items: { kind: ResourceKind; id: string }[]): Promise<string>
  /** adoptStack makes Talaia manage a running project; it returns the id of the job. */
  adoptStack(serverId: string, name: string): Promise<string>
  /** runStack sends an operation on a stack and returns the id of the job. */
  runStack(serverId: string, name: string, action: StackAction, confirm?: boolean): Promise<string>
  /** logs asks for the last lines of a container and returns the id of the job. */
  logs(serverId: string, container: string, tail?: number, options?: LogOptions): Promise<string>
  job(id: string): Promise<Job>
}

interface apiContainer {
  id: string
  name: string
  image: string
  state: string
  status: string
  compose_project: string
  compose_service: string
  use?: {
    cpu_percent: number
    mem_used_bytes: number
    mem_limit_bytes: number
    net_rx_bytes: number
    net_tx_bytes: number
  }
}

interface apiContainerList {
  connected: boolean
  docker_available: boolean
  containers: apiContainer[]
}

interface apiJob {
  id: string
  state: JobState
  message: string
  output?: string
}

/** createContainersApi returns the container API, optionally over an injected fetch. */
export function createContainersApi(fetchImpl?: typeof fetch): ContainersApi {
  const http = createHttpClient(fetchImpl)

  return {
    list: async (serverId) => {
      const body = await http.get<apiContainerList>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/containers`,
      )
      return {
        connected: body.connected,
        dockerAvailable: body.docker_available,
        containers: body.containers.map((container) => ({
          id: container.id,
          name: container.name,
          image: container.image,
          state: container.state,
          status: container.status,
          composeProject: container.compose_project,
          composeService: container.compose_service,
          ...(container.use
            ? {
                use: {
                  cpuPercent: container.use.cpu_percent,
                  memUsedBytes: container.use.mem_used_bytes,
                  memLimitBytes: container.use.mem_limit_bytes,
                  netRxBytes: container.use.net_rx_bytes,
                  netTxBytes: container.use.net_tx_bytes,
                },
              }
            : {}),
        })),
      }
    },
    run: async (serverId, container, action, confirm = false) => {
      const body = await http.post<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/containers/${encodeURIComponent(container)}/actions`,
        { action, confirm },
      )
      return body.job_id
    },
    job: async (id) => {
      const body = await http.get<apiJob>(`/api/v1/jobs/${encodeURIComponent(id)}`)
      return { id: body.id, state: body.state, message: body.message, output: body.output ?? '' }
    },
    stacks: async (serverId) => {
      const body = await http.get<StackList>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/stacks`,
      )
      return {
        connected: body.connected,
        stacks: body.stacks,
        adopted: body.adopted ?? [],
        adoptable: body.adoptable ?? [],
      }
    },
    writeStack: async (serverId, name, compose, env) => {
      const body = await http.put<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/stacks/${encodeURIComponent(name)}`,
        { compose, env },
      )
      return body.job_id
    },
    readStack: async (serverId, name) => {
      const body = await http.post<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/stacks/${encodeURIComponent(name)}/read`,
        {},
      )
      return body.job_id
    },
    listResources: async (serverId, kind) => {
      const body = await http.post<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/resources/list`,
        { kind },
      )
      return body.job_id
    },
    removeResources: async (serverId, items) => {
      const body = await http.post<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/resources/remove`,
        { items, confirm: true },
      )
      return body.job_id
    },
    adoptStack: async (serverId, name) => {
      const body = await http.post<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/stacks/${encodeURIComponent(name)}/adopt`,
        {},
      )
      return body.job_id
    },
    runStack: async (serverId, name, action, confirm = false) => {
      const body = await http.post<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/stacks/${encodeURIComponent(name)}/actions`,
        { action, confirm },
      )
      return body.job_id
    },
    logs: async (serverId, container, tail = 0, options = {}) => {
      const body = await http.post<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/containers/${encodeURIComponent(container)}/logs`,
        { tail, ...options },
      )
      return body.job_id
    },
  }
}
