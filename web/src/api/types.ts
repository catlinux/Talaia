/** Data model of the fleet screen. */

export type ServerStatus = 'online' | 'offline' | 'warning' | 'critical'

/** AptSummary is the pending-update summary a connected agent reports. */
export interface AptSummary {
  upgradable: number
  security: number
  rebootRequired: boolean
  /** checkedAt is when the agent last simulated an upgrade, when known. */
  checkedAt?: string
  /** listsUpdatedAt is when apt last downloaded its package lists, when known. */
  listsUpdatedAt?: string
}

export interface Server {
  id: string
  name: string
  /** hostname is what the connected agent reports; empty when offline. */
  hostname?: string
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
  /** hostTerminal is true when the connected agent accepts root host sessions. */
  hostTerminal?: boolean
  /** The machine facts below are absent while the server is offline. */
  kernel?: string
  arch?: string
  runningContainers?: number
  services?: number
  memUsedBytes?: number
  memTotalBytes?: number
  diskUsedBytes?: number
  diskTotalBytes?: number
  netRxBytes?: number
  netTxBytes?: number
  dockerVersion?: string
  /** uptimeSeconds is absent while the agent does not report it. */
  uptimeSeconds?: number
  /** apt is absent when the agent did not report an apt summary. */
  apt?: AptSummary
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
