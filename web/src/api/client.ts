import { sampleActivity, sampleServers } from './fixtures'
import type { ActivityEvent, Server } from './types'

/**
 * Data source of the fleet screen. A later task implements it against the real
 * REST API; the components do not change when that happens.
 */
export interface FleetClient {
  listServers(): Promise<Server[]>
  listActivity(): Promise<ActivityEvent[]>
}

/** Mock client backed by the sample data in fixtures.ts. */
export function createMockFleetClient(): FleetClient {
  return {
    listServers: () => Promise.resolve(sampleServers),
    listActivity: () => Promise.resolve(sampleActivity),
  }
}
