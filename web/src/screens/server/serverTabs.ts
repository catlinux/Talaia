import type { ServerTab } from './OverviewTab'

export const serverTabs: ServerTab[] = [
  'overview',
  'containers',
  'services',
  'logs',
  'updates',
  'alerts',
]

export interface ParsedServerPath {
  /** tab is null when the path is not a known server route. */
  tab: ServerTab | null
  /** stackName is the managed stack in a /servers/:id/stacks/:name path. */
  stackName: string | null
}

function decode(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

function segments(path: string): string[] {
  const clean = path.split(/[?#]/, 1)[0] ?? '/'
  return clean.replace(/\/+$/, '').split('/').filter(Boolean)
}

/** parseServerPath reads the tab or managed stack of a server URL. */
export function parseServerPath(path: string, serverId: string): ParsedServerPath {
  const parts = segments(path)
  if (parts[0] !== 'servers' || parts[1] === undefined || decode(parts[1]) !== serverId) {
    return { tab: null, stackName: null }
  }
  if (parts.length === 2) {
    return { tab: 'overview', stackName: null }
  }
  if (parts.length === 4 && parts[2] === 'stacks') {
    const name = decode(parts[3] ?? '')
    return name === '' ? { tab: null, stackName: null } : { tab: 'containers', stackName: name }
  }
  if (parts.length !== 3) {
    return { tab: null, stackName: null }
  }
  const tab = decode(parts[2] ?? '')
  return serverTabs.includes(tab as ServerTab)
    ? { tab: tab as ServerTab, stackName: null }
    : { tab: null, stackName: null }
}

/** firstAllowedTab returns the tab to open when the URL has none or a forbidden one. */
export function firstAllowedTab(availableTabs: ServerTab[], limited = false): ServerTab {
  if (limited && availableTabs.includes('containers')) return 'containers'
  return availableTabs[0] ?? 'containers'
}

/** tabPath returns the canonical URL of a tab; Overview has no extra segment. */
export function tabPath(serverId: string, tab: ServerTab): string {
  const base = `/servers/${encodeURIComponent(serverId)}`
  return tab === 'overview' ? base : `${base}/${tab}`
}

/** stackPath returns the URL of a managed stack screen. */
export function stackPath(serverId: string, name: string): string {
  return `/servers/${encodeURIComponent(serverId)}/stacks/${encodeURIComponent(name)}`
}
