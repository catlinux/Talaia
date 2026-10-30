import { describe, expect, it, vi } from 'vitest'

import type { Alert } from '../../api/alerts'
import {
  alertScope,
  alertState,
  defaultSort,
  distinctSilenceScopes,
  eligibility,
  filterAlerts,
  groupKey,
  silenceScope,
} from './inboxModel'

function alert(overrides: Partial<Alert> = {}): Alert {
  return {
    id: 'a1',
    ruleId: 'r1',
    ruleType: 'container_down',
    severity: 'critical',
    state: 'firing',
    serverId: 's1',
    serverName: 'vps-web-01',
    subjectKind: 'container',
    subjectName: 'web',
    stack: 'shop',
    startedAt: '2026-10-24T10:00:00Z',
    resolvedAt: '',
    ackedAt: '',
    ackedBy: '',
    silenced: false,
    summary: { key: 'alerts.container_down', params: {} },
    ...overrides,
  }
}

describe('alertState', () => {
  it('distinguishes new, seen and silenced alerts', () => {
    expect(alertState(alert())).toBe('new')
    expect(alertState(alert({ ackedBy: 'admin' }))).toBe('seen')
    expect(alertState(alert({ silenced: true }))).toBe('silenced')
  })
})

describe('filterAlerts', () => {
  const alerts = [
    alert(),
    alert({
      id: 'a2',
      severity: 'warning',
      serverId: 's2',
      serverName: 'vps-db-01',
      ruleType: 'disk_high',
      stack: '',
      subjectKind: 'server',
      subjectName: 'vps-db-01',
      startedAt: '2026-10-24T09:00:00Z',
    }),
    alert({ id: 'a3', silenced: true, ackedBy: 'ana' }),
  ]

  it('filters by severity, state, server and rule', () => {
    const filters = {
      severity: 'warning' as const,
      state: 'all' as const,
      server: 's2',
      rule: 'disk_high',
      q: '',
      group: 'none' as const,
    }
    expect(filterAlerts(alerts, filters).map((row) => row.id)).toEqual(['a2'])
  })

  it('filters by translated message and keeps the original order', () => {
    const filters = {
      severity: 'all' as const,
      state: 'all' as const,
      server: '',
      rule: '',
      q: 'database is full',
      group: 'none' as const,
    }
    expect(
      filterAlerts(alerts, filters, (row) => (row.id === 'a2' ? 'Database is full' : '')),
    ).toHaveLength(1)
    expect(filterAlerts(alerts, filters).length).toBe(0)
  })
})

describe('defaultSort', () => {
  it('puts new alerts first, then critical and oldest', () => {
    const alerts = [
      alert({ id: 'silenced', silenced: true, severity: 'critical' }),
      alert({ id: 'seen', ackedBy: 'ana', severity: 'critical' }),
      alert({ id: 'warning', severity: 'warning', startedAt: '2026-10-24T08:00:00Z' }),
      alert({ id: 'old-critical', severity: 'critical', startedAt: '2026-10-24T09:00:00Z' }),
      alert({ id: 'new-critical', severity: 'critical', startedAt: '2026-10-24T11:00:00Z' }),
    ]
    expect([...alerts].sort(defaultSort).map((row) => row.id)).toEqual([
      'old-critical',
      'new-critical',
      'warning',
      'seen',
      'silenced',
    ])
  })
})

describe('groupKey', () => {
  it('groups by server, rule or nothing', () => {
    const row = alert()
    expect(groupKey(row, 'server')).toBe('s1')
    expect(groupKey(row, 'rule')).toBe('container_down')
    expect(groupKey(row, 'none')).toBe('')
  })
})

describe('permissions and scopes', () => {
  it('builds the permission scope of an alert', () => {
    expect(alertScope(alert())).toEqual({
      serverId: 's1',
      stack: 'shop',
      container: 'web',
    })
    expect(alertScope(alert({ subjectKind: 'server', subjectName: 's1', stack: '' }))).toEqual({
      serverId: 's1',
      stack: undefined,
      container: undefined,
    })
  })

  it('partitions bulk actions by permissions and applicability', () => {
    const can = vi.fn((_permission, scope) => scope?.serverId === 's1')
    const result = eligibility(
      [
        alert(),
        alert({ id: 'seen', ackedBy: 'ana' }),
        alert({ id: 'server', subjectKind: 'server', subjectName: 's1', stack: '' }),
        alert({
          id: 'outside',
          serverId: 's2',
          subjectKind: 'container',
          subjectName: 'db',
        }),
      ],
      can,
    )
    expect(result.acknowledge.allowed.map((row) => row.id)).toEqual(['a1', 'server'])
    expect(result.acknowledge.denied.map((row) => row.id)).toEqual(['seen', 'outside'])
    expect(result.silence.allowed.map((row) => row.id)).toEqual(['a1', 'seen', 'server'])
    expect(result.unwatch.allowed.map((row) => row.id)).toEqual(['a1', 'seen'])
    expect(result.unwatch.denied.map((row) => row.id)).toEqual(['server', 'outside'])
  })

  it('deduplicates bulk silence scopes', () => {
    const scopes = distinctSilenceScopes([
      alert(),
      alert({ id: 'a2' }),
      alert({ id: 'a3', subjectKind: 'server', subjectName: 's1', stack: '' }),
      alert({ id: 'a4', ruleType: 'disk_high' }),
    ])
    expect(scopes).toEqual([
      { scopeType: 'container', serverId: 's1', name: 'web', ruleType: 'container_down' },
      { scopeType: 'server', serverId: 's1', ruleType: 'container_down' },
      { scopeType: 'container', serverId: 's1', name: 'web', ruleType: 'disk_high' },
    ])
  })

  it('uses the stack or organization scope when there is no container', () => {
    expect(silenceScope(alert({ subjectKind: 'stack', subjectName: '', stack: 'shop' }))).toEqual({
      scopeType: 'stack',
      serverId: 's1',
      name: 'shop',
    })
    expect(silenceScope(alert({ serverId: null }))).toEqual({ scopeType: 'organization' })
  })
})
