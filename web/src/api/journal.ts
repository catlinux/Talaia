import type { Job } from './containers'
import { createHttpClient } from './http'

/** JournalBoot is the boot whose entries are read. */
export type JournalBoot = 'all' | 'current' | 'previous'

/** JournalEntry is one journal line returned by the agent. */
export interface JournalEntry {
  ts: string
  unit: string
  priority: number
  pid: string
  message: string
}

/** JournalPage is the output of a journal read job. */
export interface JournalPage {
  entries: JournalEntry[]
  cursor: string
}

/** JournalQuery is one journal read request. */
export interface JournalQuery {
  unit?: string
  maxPriority?: number | null
  since?: string
  until?: string
  search?: string
  boot?: JournalBoot
  tail?: number
  afterCursor?: string
}

/** JournalApi queues journal reads and reads their progress. */
export interface JournalApi {
  /** read returns the id of the job that answers with a JournalPage. */
  read(serverId: string, query?: JournalQuery): Promise<string>
  job(id: string): Promise<Job>
}

interface apiJournalEntry {
  ts?: unknown
  unit?: unknown
  priority?: unknown
  pid?: unknown
  message?: unknown
}

interface apiJournalPage {
  entries?: unknown
  cursor?: unknown
}

interface apiJob {
  id: string
  state: Job['state']
  message: string
  output?: string
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function priority(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? Math.trunc(value) : 6
}

/**
 * parseJournalPage reads the JSON output of a journal read job. The agent always
 * sends the full shape; the defaults keep a slightly older answer readable.
 */
export function parseJournalPage(output: string): JournalPage {
  const body = JSON.parse(output) as apiJournalPage
  const entries = Array.isArray(body.entries) ? (body.entries as apiJournalEntry[]) : []
  return {
    entries: entries.map((entry) => ({
      ts: text(entry.ts),
      unit: text(entry.unit),
      priority: priority(entry.priority),
      pid: text(entry.pid),
      message: text(entry.message),
    })),
    cursor: text(body.cursor),
  }
}

/** createJournalApi returns the journal API, optionally over an injected fetch. */
export function createJournalApi(fetchImpl?: typeof fetch): JournalApi {
  const http = createHttpClient(fetchImpl)

  return {
    read: async (serverId, query = {}) => {
      const body = await http.post<{ job_id: string }>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/journal`,
        {
          unit: query.unit ?? '',
          max_priority: query.maxPriority ?? null,
          since: query.since ?? '',
          until: query.until ?? '',
          search: query.search ?? '',
          boot: query.boot ?? 'all',
          tail: query.tail ?? 0,
          after_cursor: query.afterCursor ?? '',
        },
      )
      return body.job_id
    },
    job: async (id) => {
      const body = await http.get<apiJob>(`/api/v1/jobs/${encodeURIComponent(id)}`)
      return { id: body.id, state: body.state, message: body.message, output: body.output ?? '' }
    },
  }
}
