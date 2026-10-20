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
}

/** ContainerList is the answer of the containers endpoint. */
export interface ContainerList {
  /** connected is false while the agent of the server has no open stream. */
  connected: boolean
  dockerAvailable: boolean
  containers: Container[]
}

export type ContainerAction = 'start' | 'stop' | 'restart' | 'remove'

export type JobState = 'pending' | 'running' | 'succeeded' | 'failed'

/** Job is the progress of a command sent to an agent. */
export interface Job {
  id: string
  state: JobState
  /** message carries the reason of a failure. */
  message: string
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
      return { id: body.id, state: body.state, message: body.message }
    },
  }
}
