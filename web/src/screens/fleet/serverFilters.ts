import type { Server, ServerStatus } from '../../api/types'

export type SortDirection = 'asc' | 'desc'
export type ServerSortColumn =
  'status' | 'name' | 'os' | 'cpu' | 'ram' | 'disk' | 'containers' | 'alerts' | 'updates' | 'agent'

export interface ServerSort {
  id: ServerSortColumn
  dir: SortDirection
}

export interface ServerFilters {
  q: string
  status: '' | ServerStatus
  tag: string
  securityOnly: boolean
  rebootOnly: boolean
  sort: ServerSort
}

export const serverStatuses: ServerStatus[] = ['online', 'warning', 'critical', 'offline']

export const serverSortColumns: ServerSortColumn[] = [
  'status',
  'name',
  'os',
  'cpu',
  'ram',
  'disk',
  'containers',
  'alerts',
  'updates',
  'agent',
]

export const defaultServerSort: ServerSort = { id: 'status', dir: 'asc' }

export const serverStatusOrder: Record<ServerStatus, number> = {
  critical: 0,
  warning: 1,
  offline: 2,
  online: 3,
}

function parseStatus(value: string | null): '' | ServerStatus {
  if (value === null || value === '') return ''
  if (value === 'correct' || value === 'ok') return 'online'
  return serverStatuses.includes(value as ServerStatus) ? (value as ServerStatus) : ''
}

function parseSort(value: string | null): ServerSort {
  if (value === null) return defaultServerSort
  const [id, dir] = value.split(':')
  if (
    id === undefined ||
    dir === undefined ||
    !serverSortColumns.includes(id as ServerSortColumn) ||
    (dir !== 'asc' && dir !== 'desc')
  ) {
    return defaultServerSort
  }
  return { id: id as ServerSortColumn, dir }
}

/** parseServerFilters reads the URL query of the Servers screen. */
export function parseServerFilters(params: URLSearchParams): ServerFilters {
  const reboot = params.get('reboot')
  return {
    q: params.get('q') ?? '',
    status: parseStatus(params.get('status')),
    tag: params.get('tag') ?? '',
    securityOnly: params.get('updates') === 'security',
    rebootOnly: reboot === '1' || reboot === 'true',
    sort: parseSort(params.get('sort')),
  }
}

function matchesQuery(server: Server, query: string): boolean {
  const needle = query.trim().toLocaleLowerCase()
  if (needle === '') return true
  const fields = [server.name, server.hostname ?? '', server.os, server.location, ...server.tags]
  return fields.some((field) => field.toLocaleLowerCase().includes(needle))
}

/** filterServers applies the search and filters of the URL. */
export function filterServers(servers: Server[], filters: ServerFilters): Server[] {
  return servers.filter((server) => {
    if (filters.status !== '' && server.status !== filters.status) return false
    if (filters.tag !== '' && !server.tags.includes(filters.tag)) return false
    if (filters.securityOnly && (server.apt?.security ?? 0) <= 0) return false
    if (filters.rebootOnly && server.apt?.rebootRequired !== true) return false
    return matchesQuery(server, filters.q)
  })
}

function compareNullableNumber(
  left: number | null,
  right: number | null,
  direction: number,
): number {
  if (left === null && right === null) return 0
  if (left === null) return 1
  if (right === null) return -1
  return (left - right) * direction
}

function updateCount(server: Server): number {
  const security = server.apt?.security ?? 0
  return security > 0 ? security : (server.apt?.upgradable ?? 0)
}

function compareByColumn(
  left: Server,
  right: Server,
  column: ServerSortColumn,
  direction: number,
  alertCounts: ReadonlyMap<string, number>,
): number {
  switch (column) {
    case 'status':
      return (serverStatusOrder[left.status] - serverStatusOrder[right.status]) * direction
    case 'name':
      return left.name.localeCompare(right.name) * direction
    case 'os':
      return (
        `${left.os} ${left.location}`.localeCompare(`${right.os} ${right.location}`) * direction
      )
    case 'cpu':
      return compareNullableNumber(left.cpuPercent, right.cpuPercent, direction)
    case 'ram':
      return compareNullableNumber(left.memPercent, right.memPercent, direction)
    case 'disk':
      return compareNullableNumber(left.diskPercent, right.diskPercent, direction)
    case 'containers':
      return (left.containers - right.containers) * direction
    case 'alerts':
      return ((alertCounts.get(left.id) ?? 0) - (alertCounts.get(right.id) ?? 0)) * direction
    case 'updates':
      return (updateCount(left) - updateCount(right)) * direction
    case 'agent':
      return left.agentVersion.localeCompare(right.agentVersion) * direction
  }
}

/** sortServers applies the current sort and keeps name as the tie-breaker. */
export function sortServers(
  servers: Server[],
  sort: ServerSort,
  alertCounts: ReadonlyMap<string, number> = new Map(),
): Server[] {
  const direction = sort.dir === 'asc' ? 1 : -1
  return [...servers].sort((left, right) => {
    const byColumn = compareByColumn(left, right, sort.id, direction, alertCounts)
    if (byColumn !== 0) return byColumn
    return (
      left.name.localeCompare(right.name) ||
      serverStatusOrder[left.status] - serverStatusOrder[right.status] ||
      left.id.localeCompare(right.id)
    )
  })
}

/** applyServerFilters filters and sorts the fleet for the URL state. */
export function applyServerFilters(
  servers: Server[],
  filters: ServerFilters,
  alertCounts: ReadonlyMap<string, number> = new Map(),
): Server[] {
  return sortServers(filterServers(servers, filters), filters.sort, alertCounts)
}

/** serverQuery returns the replaceable URL query of a filter state. */
export function serverQuery(filters: Partial<ServerFilters>): Record<string, string | null> {
  return {
    ...(filters.q === undefined ? {} : { q: filters.q === '' ? null : filters.q }),
    ...(filters.status === undefined
      ? {}
      : { status: filters.status === '' ? null : filters.status }),
    ...(filters.tag === undefined ? {} : { tag: filters.tag === '' ? null : filters.tag }),
    ...(filters.securityOnly === undefined
      ? {}
      : { updates: filters.securityOnly ? 'security' : null }),
    ...(filters.rebootOnly === undefined ? {} : { reboot: filters.rebootOnly ? '1' : null }),
    ...(filters.sort === undefined ? {} : { sort: `${filters.sort.id}:${filters.sort.dir}` }),
  }
}
