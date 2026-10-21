/** Data model of the fleet screen. */

export type ServerStatus = 'online' | 'offline' | 'warning' | 'critical'

export interface Server {
  id: string
  name: string
  os: string
  location: string
  vcpu: number
  memoryGb: number
  status: ServerStatus
  /** Translation key describing what needs attention, when it is not the status itself. */
  statusDetailKey?: string
  cpuPercent: number | null
  memPercent: number | null
  diskPercent: number | null
  containers: number
  stacks: number
  tags: string[]
  lastSeen: string | null
  /** agentVersion is the version the connected agent reports; empty when offline. */
  agentVersion: string
  /** agentLatestVersion is the newest agent package the core can offer. */
  agentLatestVersion: string
  /** agentUpdateAvailable is true when the core offers a newer agent package. */
  agentUpdateAvailable: boolean
  /** agentUpdateManual is true when the agent is too old to update itself. */
  agentUpdateManual: boolean
  /** agentUpgradeCommand upgrades such an old agent once, by hand. */
  agentUpgradeCommand: string
}

/**
 * KPI values for the summary row. They are derived from the servers and the
 * activity feed, because the client only exposes those two lists.
 */
export interface FleetSummary {
  serversOnline: number
  serversTotal: number
  runningContainers: number
  /** Not reported by the API yet: null means "not available". */
  securityUpdates: number | null
  activeAlerts: number | null
  lastBackup: string | null
}

export type ActivitySeverity = 'info' | 'ok' | 'warning' | 'critical'

export interface ActivityEvent {
  id: string
  severity: ActivitySeverity
  /** Translation key of the message, for example event.diskHigh. */
  messageKey: string
  params: Record<string, string | number>
  at: string
  /** Translation key of the origin of the event, for example source.apt. */
  source: string
}

/** EnrollmentToken is a one-time token to add a server, with its install command. */
export interface EnrollmentToken {
  token: string
  caFingerprint: string
  /** expiresIn is how many seconds the token stays valid. */
  expiresIn: number
  /** command is the line to run on the new server; empty when the core has no public address. */
  command: string
}
