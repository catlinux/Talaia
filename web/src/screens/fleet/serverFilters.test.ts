import { describe, expect, it } from 'vitest'

import type { Server } from '../../api/types'
import {
  applyServerFilters,
  defaultServerSort,
  filterServers,
  parseServerFilters,
  serverQuery,
  sortServers,
} from './serverFilters'

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: 'srv-1',
    name: 'vps-web-01',
    os: 'Debian GNU/Linux 13',
    location: 'Paris',
    vcpu: 4,
    memoryGb: 8,
    status: 'online',
    cpuPercent: 12,
    memPercent: 25,
    diskPercent: 20,
    containers: 3,
    runningContainers: 3,
    stacks: 1,
    tags: ['web'],
    lastSeen: '2026-10-19T12:00:00Z',
    agentVersion: '0.2.0',
    agentLatestVersion: '0.2.8',
    agentUpdateAvailable: false,
    agentUpdateManual: false,
    agentUpgradeCommand: '',
    ...overrides,
  }
}

describe('parseServerFilters', () => {
  it('reads the URL query and defaults the sort to severity then name', () => {
    const filters = parseServerFilters(
      new URLSearchParams('q=db&status=critical&tag=prod&updates=security&reboot=1'),
    )

    expect(filters).toEqual({
      q: 'db',
      status: 'critical',
      tag: 'prod',
      securityOnly: true,
      rebootOnly: true,
      sort: defaultServerSort,
    })
  })

  it('accepts the correct status alias and validates the sort', () => {
    expect(parseServerFilters(new URLSearchParams('status=correct')).status).toBe('online')
    expect(parseServerFilters(new URLSearchParams('sort=name:desc')).sort).toEqual({
      id: 'name',
      dir: 'desc',
    })
    expect(parseServerFilters(new URLSearchParams('sort=bogus:down')).sort).toEqual(
      defaultServerSort,
    )
    expect(parseServerFilters(new URLSearchParams('status=unknown')).status).toBe('')
  })
})

describe('serverQuery', () => {
  it('only writes the provided keys and removes empty values', () => {
    expect(
      serverQuery({
        q: '',
        status: 'offline',
        tag: '',
        securityOnly: true,
        rebootOnly: false,
        sort: { id: 'name', dir: 'asc' },
      }),
    ).toEqual({
      q: null,
      status: 'offline',
      tag: null,
      updates: 'security',
      reboot: null,
      sort: 'name:asc',
    })
  })
})

describe('filterServers', () => {
  const servers = [
    server({ id: 'a', name: 'vps-web-01', hostname: 'web.example.com', tags: ['web', 'prod'] }),
    server({
      id: 'b',
      name: 'vps-db-01',
      os: 'Debian 13',
      location: 'Madrid',
      status: 'critical',
      tags: ['db', 'prod'],
      apt: { upgradable: 3, security: 2, rebootRequired: true },
    }),
    server({ id: 'c', name: 'rpi-casa', status: 'offline', tags: ['home'] }),
  ]

  it('searches name, hostname, OS, location and tags', () => {
    const base = parseServerFilters(new URLSearchParams())
    for (const query of ['web.example', 'madrid', 'debian 13', 'home']) {
      expect(filterServers(servers, { ...base, q: query }).map((entry) => entry.id)).toEqual([
        query === 'web.example' ? 'a' : query === 'home' ? 'c' : 'b',
      ])
    }
  })

  it('combines status, tag, security and reboot filters', () => {
    const filters = parseServerFilters(
      new URLSearchParams('status=critical&tag=prod&updates=security&reboot=1'),
    )
    expect(filterServers(servers, filters).map((entry) => entry.id)).toEqual(['b'])
  })

  it('returns nothing when the filters do not match', () => {
    const filters = parseServerFilters(new URLSearchParams('status=online&tag=home'))
    expect(filterServers(servers, filters)).toEqual([])
  })
})

describe('sortServers', () => {
  const servers = [
    server({ id: 'online', name: 'b-online', status: 'online', cpuPercent: null }),
    server({ id: 'critical', name: 'c-critical', status: 'critical', cpuPercent: 10 }),
    server({ id: 'warning', name: 'a-warning', status: 'warning', cpuPercent: 50 }),
    server({ id: 'offline', name: 'd-offline', status: 'offline', cpuPercent: 20 }),
  ]

  it('sorts by severity by default: critical, warning, offline, online', () => {
    expect(sortServers(servers, defaultServerSort).map((entry) => entry.id)).toEqual([
      'critical',
      'warning',
      'offline',
      'online',
    ])
  })

  it('sorts by name and by CPU with unknown values last', () => {
    expect(sortServers(servers, { id: 'name', dir: 'asc' }).map((entry) => entry.id)).toEqual([
      'warning',
      'online',
      'critical',
      'offline',
    ])
    expect(sortServers(servers, { id: 'cpu', dir: 'asc' }).map((entry) => entry.id)).toEqual([
      'critical',
      'offline',
      'warning',
      'online',
    ])
    expect(sortServers(servers, { id: 'cpu', dir: 'desc' }).map((entry) => entry.id)).toEqual([
      'warning',
      'offline',
      'critical',
      'online',
    ])
  })

  it('sorts alerts by the supplied counts', () => {
    const counts = new Map([
      ['online', 2],
      ['critical', 5],
      ['warning', 0],
      ['offline', 1],
    ])
    expect(
      sortServers(servers, { id: 'alerts', dir: 'desc' }, counts).map((entry) => entry.id),
    ).toEqual(['critical', 'online', 'offline', 'warning'])
  })
})

describe('applyServerFilters', () => {
  it('filters before sorting', () => {
    const filters = parseServerFilters(new URLSearchParams('tag=prod&sort=name:desc'))
    const servers = [
      server({ id: 'a', name: 'a', tags: ['prod'] }),
      server({ id: 'b', name: 'b', tags: ['prod'] }),
      server({ id: 'c', name: 'c', tags: ['home'] }),
    ]
    expect(applyServerFilters(servers, filters).map((entry) => entry.id)).toEqual(['b', 'a'])
  })
})
