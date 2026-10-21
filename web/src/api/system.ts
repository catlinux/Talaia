import { createHttpClient } from './http'

/** PendingCommit is one commit between the running build and the newest green one. */
export interface PendingCommit {
  /** sha is already shortened by the core. */
  sha: string
  subject: string
  author: string
}

/** UpdateNotes are the changelog bullets added in the offered version. */
export interface UpdateNotes {
  en: string[]
  es: string[]
}

/** UpdateInfo is what the core knows about newer commits. */
export interface UpdateInfo {
  /** current is the commit of the running binary; empty when it is unknown. */
  current: string
  /** latest is the newest commit whose CI is green; empty when there is none. */
  latest: string
  pending: PendingCommit[]
  /** notes are the changelog bullets added since the running commit. */
  notes: UpdateNotes
  /** ci is "green" when the offered commit passed its CI checks. */
  ci: 'green' | 'red'
  /** checkedAt is the RFC 3339 time of the last check, successful or not. */
  checkedAt: string
  /** error is a stable code of the last failed check, or '' when it worked. */
  error: string
  updateAvailable: boolean
}

/** UpdateState is the state the host helper reports for the last update. */
export type UpdateState = 'idle' | 'running' | 'succeeded' | 'failed' | 'rolled_back' | 'unknown'

/** UpdateStatus is the progress of the update the host helper runs. */
export interface UpdateStatus {
  state: UpdateState
  from: string
  to: string
  startedAt: string
  finishedAt: string
  log: string[]
}

/** SystemVersion is the answer of GET /api/v1/system/version. */
export interface SystemVersion {
  version: string
  commit: string
  update: UpdateInfo
  status: UpdateStatus
}

/** SystemApi is the version and update API of the core. */
export interface SystemApi {
  version(): Promise<SystemVersion>
  /** check asks GitHub now and returns the refreshed version. */
  check(): Promise<SystemVersion>
  /** update queues target for the host helper; it must be the offered commit. */
  update(target: string): Promise<{ target: string }>
}

interface apiPendingCommit {
  sha: string
  subject: string
  author: string
}

interface apiUpdateInfo {
  current: string
  latest: string
  pending?: apiPendingCommit[]
  notes?: {
    en?: string[]
    es?: string[]
  }
  ci: string
  checked_at: string
  error?: string
  update_available: boolean
}

interface apiUpdateStatus {
  state: string
  from?: string
  to?: string
  started_at?: string
  finished_at?: string
  log?: string[]
}

interface apiSystemVersion {
  version: string
  commit: string
  update: apiUpdateInfo
  status: apiUpdateStatus
}

const updateStates: readonly UpdateState[] = [
  'idle',
  'running',
  'succeeded',
  'failed',
  'rolled_back',
  'unknown',
]

/** updateState narrows the state of the helper, falling back to unknown. */
function updateState(value: string): UpdateState {
  return (updateStates as readonly string[]).includes(value) ? (value as UpdateState) : 'unknown'
}

/** mapSystem converts the wire answer to the panel types. */
function mapSystem(body: apiSystemVersion): SystemVersion {
  return {
    version: body.version,
    commit: body.commit,
    update: {
      current: body.update.current,
      latest: body.update.latest,
      pending: (body.update.pending ?? []).map((commit) => ({
        sha: commit.sha,
        subject: commit.subject,
        author: commit.author,
      })),
      notes: {
        en: body.update.notes?.en ?? [],
        es: body.update.notes?.es ?? [],
      },
      ci: body.update.ci === 'green' ? 'green' : 'red',
      checkedAt: body.update.checked_at,
      error: body.update.error ?? '',
      updateAvailable: body.update.update_available,
    },
    status: {
      state: updateState(body.status.state),
      from: body.status.from ?? '',
      to: body.status.to ?? '',
      startedAt: body.status.started_at ?? '',
      finishedAt: body.status.finished_at ?? '',
      log: body.status.log ?? [],
    },
  }
}

/** createSystemApi returns the update API, optionally over an injected fetch. */
export function createSystemApi(fetchImpl?: typeof fetch): SystemApi {
  const http = createHttpClient(fetchImpl)

  return {
    version: async () => mapSystem(await http.get<apiSystemVersion>('/api/v1/system/version')),
    check: async () => mapSystem(await http.post<apiSystemVersion>('/api/v1/system/check', {})),
    update: async (target) => {
      const body = await http.post<{ target: string }>('/api/v1/system/update', {
        confirm: true,
        target,
      })
      return { target: body.target }
    },
  }
}
