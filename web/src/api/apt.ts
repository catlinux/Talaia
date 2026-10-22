import type { Job } from './containers'
import { createHttpClient } from './http'

/** AptPackage is one pending package update. */
export interface AptPackage {
  name: string
  /** current is empty for a package the upgrade would install. */
  current: string
  candidate: string
  origin: string
  security: boolean
}

/** AptUpgradeMode is the kind of upgrade the core can ask for. */
export type AptUpgradeMode = 'security' | 'all'

/** AptStatus is the output of an apt status job. */
export interface AptStatus {
  packages: AptPackage[]
  held: string[]
  rebootRequired: boolean
  rebootReasons: string[]
  /** listsUpdatedAt is empty when the agent does not know it. */
  listsUpdatedAt: string
  checkedAt: string
}

/** AptApi queues an apt status job and reads its progress. */
export interface AptApi {
  /** status returns the id of the job that answers with AptStatus. */
  status(serverId: string, refresh?: boolean): Promise<string>
  /** upgrade starts an upgrade; the agent computes the package list. */
  upgrade(serverId: string, mode: AptUpgradeMode): Promise<string>
  /** reboot asks the agent to reboot; the hostname is its confirmation. */
  reboot(serverId: string, hostname: string): Promise<string>
  job(id: string): Promise<Job>
}

interface apiAptStatus {
  packages?: Array<{
    name?: string
    current?: string
    candidate?: string
    origin?: string
    security?: boolean
  }>
  held?: string[]
  reboot_required?: boolean
  reboot_reasons?: string[]
  lists_updated_at?: string
  checked_at?: string
}

interface apiJob {
  id: string
  state: Job['state']
  message: string
  output?: string
}

/**
 * parseAptStatus reads the JSON output of an apt status job. The agent always
 * sends the full shape; the defaults keep a slightly older answer readable.
 */
export function parseAptStatus(output: string): AptStatus {
  const body = JSON.parse(output) as apiAptStatus
  return {
    packages: (body.packages ?? []).map((pkg) => ({
      name: pkg.name ?? '',
      current: pkg.current ?? '',
      candidate: pkg.candidate ?? '',
      origin: pkg.origin ?? '',
      security: pkg.security ?? false,
    })),
    held: body.held ?? [],
    rebootRequired: body.reboot_required ?? false,
    rebootReasons: body.reboot_reasons ?? [],
    listsUpdatedAt: body.lists_updated_at ?? '',
    checkedAt: body.checked_at ?? '',
  }
}

/** createAptApi returns the read-only apt API, optionally over an injected fetch. */
export function createAptApi(fetchImpl?: typeof fetch): AptApi {
  const http = createHttpClient(fetchImpl)

  return {
    status: async (serverId, refresh = false) => {
      const body = await http.post<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/apt/status`,
        { refresh },
      )
      return body.job_id
    },
    upgrade: async (serverId, mode) => {
      const body = await http.post<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/apt/upgrade`,
        { mode, confirm: true },
      )
      return body.job_id
    },
    reboot: async (serverId, hostname) => {
      const body = await http.post<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/reboot`,
        { confirm: hostname },
      )
      return body.job_id
    },
    job: async (id) => {
      const body = await http.get<apiJob>(`/api/v1/jobs/${encodeURIComponent(id)}`)
      return { id: body.id, state: body.state, message: body.message, output: body.output ?? '' }
    },
  }
}
