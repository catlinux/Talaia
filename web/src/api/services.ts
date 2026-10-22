import type { Job } from './containers'
import { createHttpClient } from './http'

/** ServiceAction is a typed systemd operation. */
export type ServiceAction = 'start' | 'stop' | 'restart' | 'reload' | 'enable' | 'disable'

/** ServiceUnit is one systemd unit the agent reported. */
export interface ServiceUnit {
  name: string
  description: string
  loadState: string
  activeState: string
  subState: string
  unitFileState: string
  canReload: boolean
}

/** ServiceState is the output of a service action job. */
export interface ServiceState {
  unit: string
  activeState: string
  subState: string
  unitFileState: string
  loadState: string
}

/** ServicesApi lists services and runs typed actions. */
export interface ServicesApi {
  list(serverId: string): Promise<ServiceUnit[]>
  /** action returns the id of the job that applies the action. */
  action(serverId: string, unit: string, action: ServiceAction, confirm: boolean): Promise<string>
  job(id: string): Promise<Job>
}

interface apiServiceUnit {
  name?: string
  description?: string
  load_state?: string
  active_state?: string
  sub_state?: string
  unit_file_state?: string
  can_reload?: boolean
}

interface apiServices {
  services?: apiServiceUnit[]
}

interface apiServiceState {
  unit?: string
  active_state?: string
  sub_state?: string
  unit_file_state?: string
  load_state?: string
}

interface apiJob {
  id: string
  state: Job['state']
  message: string
  output?: string
}

/** parseServiceState reads the JSON output of a service action job. */
export function parseServiceState(output: string): ServiceState {
  const body = JSON.parse(output) as apiServiceState
  return {
    unit: body.unit ?? '',
    activeState: body.active_state ?? '',
    subState: body.sub_state ?? '',
    unitFileState: body.unit_file_state ?? '',
    loadState: body.load_state ?? '',
  }
}

/** createServicesApi returns the service API, optionally over an injected fetch. */
export function createServicesApi(fetchImpl?: typeof fetch): ServicesApi {
  const http = createHttpClient(fetchImpl)

  return {
    list: async (serverId) => {
      const body = await http.get<apiServices>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/services`,
      )
      return (body.services ?? []).map((service) => ({
        name: service.name ?? '',
        description: service.description ?? '',
        loadState: service.load_state ?? '',
        activeState: service.active_state ?? '',
        subState: service.sub_state ?? '',
        unitFileState: service.unit_file_state ?? '',
        canReload: service.can_reload ?? false,
      }))
    },
    action: async (serverId, unit, action, confirm) => {
      const body = await http.post<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/services/${encodeURIComponent(unit)}/actions`,
        { action, confirm },
      )
      return body.job_id
    },
    job: async (id) => {
      const body = await http.get<apiJob>(`/api/v1/jobs/${encodeURIComponent(id)}`)
      return { id: body.id, state: body.state, message: body.message, output: body.output ?? '' }
    },
  }
}
