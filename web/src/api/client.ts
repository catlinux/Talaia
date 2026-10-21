import { createHttpClient } from './http'
import { sampleActivity, sampleServers } from './fixtures'
import type { ActivityEvent, EnrollmentToken, Server, ServerStatus } from './types'

/**
 * Data source of the fleet screen. The panel uses the API client; the tests
 * inject their own.
 */
export interface FleetClient {
  listServers(): Promise<Server[]>
  listActivity(): Promise<ActivityEvent[]>
  /** createEnrollment creates the one-time token that adds a server with this name. */
  createEnrollment(name: string): Promise<EnrollmentToken>
}

/** Mock client backed by the sample data in fixtures.ts. */
export function createMockFleetClient(): FleetClient {
  return {
    listServers: () => Promise.resolve(sampleServers),
    listActivity: () => Promise.resolve(sampleActivity),
    createEnrollment: () =>
      Promise.resolve({
        token: 'tl_enr_EXAMPLE',
        caFingerprint: '0'.repeat(64),
        expiresIn: 900,
        command:
          'curl -fsSk https://panel.example.com/install.sh | sudo bash -s -- --url https://panel.example.com --token tl_enr_EXAMPLE --ca-fingerprint ' +
          '0'.repeat(64),
      }),
  }
}

/** liveAgent is what a connected agent reports, as the API sends it. */
interface apiLiveAgent {
  hostname: string
  agent_version: string
  os_name: string
  os_version: string
  kernel: string
  arch: string
  cpu_count: number
  cpu_percent: number
  mem_used_bytes: number
  mem_total_bytes: number
  disk_used_bytes: number
  disk_total_bytes: number
  net_rx_bytes: number
  net_tx_bytes: number
  docker_available: boolean
  containers: number
  running_containers: number
  compose_projects: number
  services: number
  seen_at: string
}

/** apiServer is one entry of GET /api/v1/servers. */
interface apiServer {
  id: string
  name: string
  created_at: string | null
  last_seen: string | null
  revoked: boolean
  cert_not_after: string | null
  connected: boolean
  agent_update_available: boolean
  agent_update_manual: boolean
  live?: apiLiveAgent
}

/** apiEnrollment is the answer of POST /api/v1/servers/enrollment-tokens. */
interface apiEnrollment {
  token: string
  ca_fingerprint: string
  expires_in: number
  command: string
}

interface serversResponse {
  servers: apiServer[]
  /** agent_latest_version is the newest agent package the core can offer. */
  agent_latest_version: string
  /** agent_upgrade_command upgrades an agent that is too old to update itself. */
  agent_upgrade_command: string
}

/** Status thresholds, in percent of memory or disk in use. */
const criticalPercent = 90
const warningPercent = 75

const bytesPerGb = 1024 ** 3

/** percent returns the used share of a total, or null when the total is zero. */
function percent(used: number, total: number): number | null {
  if (total <= 0) {
    return null
  }
  return Math.min(100, Math.round((used / total) * 100))
}

/** gigabytes converts bytes to GiB, rounded to one decimal. */
function gigabytes(bytes: number): number {
  return Math.round((bytes / bytesPerGb) * 10) / 10
}

/** statusOf derives the card status from the live data and the revocation. */
function statusOf(server: apiServer): { status: ServerStatus; statusDetailKey?: string } {
  if (server.revoked) {
    return { status: 'offline', statusDetailKey: 'status.revoked' }
  }
  if (!server.connected || !server.live) {
    return { status: 'offline' }
  }

  const load = Math.max(
    percent(server.live.mem_used_bytes, server.live.mem_total_bytes) ?? 0,
    percent(server.live.disk_used_bytes, server.live.disk_total_bytes) ?? 0,
  )
  if (load >= criticalPercent) {
    return { status: 'critical' }
  }
  if (load >= warningPercent) {
    return { status: 'warning' }
  }
  return { status: 'online' }
}

/** toServer maps one API entry to the model of the fleet screen. */
function toServer(server: apiServer, latestVersion: string, upgradeCommand: string): Server {
  const live = server.connected ? server.live : undefined

  return {
    id: server.id,
    name: server.name,
    // The name of the system is only known while the agent is connected.
    os: live ? `${live.os_name} ${live.os_version}`.trim() : '',
    // Locations arrive with a later task: the card hides the field while empty.
    location: '',
    vcpu: live ? live.cpu_count : 0,
    memoryGb: live ? gigabytes(live.mem_total_bytes) : 0,
    ...statusOf(server),
    cpuPercent: live ? live.cpu_percent : null,
    memPercent: live ? percent(live.mem_used_bytes, live.mem_total_bytes) : null,
    diskPercent: live ? percent(live.disk_used_bytes, live.disk_total_bytes) : null,
    containers: live ? live.containers : 0,
    stacks: live ? live.compose_projects : 0,
    tags: [],
    lastSeen: live?.seen_at ?? server.last_seen,
    agentVersion: live?.agent_version ?? '',
    agentLatestVersion: latestVersion,
    agentUpdateAvailable: server.agent_update_available ?? false,
    agentUpdateManual: server.agent_update_manual ?? false,
    agentUpgradeCommand: upgradeCommand,
  }
}

/** createApiFleetClient reads the fleet from the core API. */
export function createApiFleetClient(fetchImpl?: typeof fetch): FleetClient {
  const http = createHttpClient(fetchImpl)

  return {
    listServers: async () => {
      const body = await http.get<serversResponse>('/api/v1/servers')
      const latestVersion = body.agent_latest_version ?? ''
      const upgradeCommand = body.agent_upgrade_command ?? ''
      return body.servers.map((server) => toServer(server, latestVersion, upgradeCommand))
    },
    // There is no activity API yet: the screen shows placeholders instead of
    // inventing numbers.
    listActivity: () => Promise.resolve([]),
    createEnrollment: async (name) => {
      const body = await http.post<apiEnrollment>('/api/v1/servers/enrollment-tokens', { name })
      return {
        token: body.token,
        caFingerprint: body.ca_fingerprint,
        expiresIn: body.expires_in,
        command: body.command,
      }
    },
  }
}
