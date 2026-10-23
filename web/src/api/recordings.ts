import { createHttpClient } from './http'

/** Recording is one terminal session as the recordings list exposes it. */
export interface Recording {
  id: string
  serverId: string
  serverName: string
  /** container is empty for a host shell. */
  container: string
  kind: 'container' | 'host'
  shell: string
  user: string
  startedAt: string
  /** endedAt is null while the session is still open. */
  endedAt: string | null
  durationSeconds: number
  closeReason: string
  privileged: boolean
  bytes: number
}

/** RecordingsApi lists finished and open terminal sessions and their downloads. */
export interface RecordingsApi {
  list(limit?: number): Promise<Recording[]>
  downloadUrl(id: string): string
}

interface apiRecording {
  id: string
  server_id: string
  server_name: string
  container: string
  kind: string
  shell: string
  user: string
  started_at: string
  ended_at: string | null
  duration_seconds: number
  close_reason: string
  privileged: boolean
  bytes: number
}

/** createRecordingsApi returns the terminal recordings client of the core. */
export function createRecordingsApi(fetchImpl?: typeof fetch): RecordingsApi {
  const http = createHttpClient(fetchImpl)
  return {
    list: async (limit = 100) => {
      const body = await http.get<apiRecording[]>(`/api/v1/recordings?limit=${limit}`)
      return (body ?? []).map((recording) => ({
        id: recording.id,
        serverId: recording.server_id,
        serverName: recording.server_name,
        container: recording.container,
        kind: recording.kind === 'host' ? 'host' : 'container',
        shell: recording.shell,
        user: recording.user,
        startedAt: recording.started_at,
        endedAt: recording.ended_at,
        durationSeconds: recording.duration_seconds,
        closeReason: recording.close_reason,
        privileged: recording.privileged,
        bytes: recording.bytes,
      }))
    },
    downloadUrl: (id) => `/api/v1/recordings/${encodeURIComponent(id)}/download`,
  }
}
