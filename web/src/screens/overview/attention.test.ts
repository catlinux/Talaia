import { describe, expect, it } from 'vitest'

import type { Alert } from '../../api/alerts'
import type { Server } from '../../api/types'
import { buildAttention } from './attention'

function server(overrides: Partial<Server> = {}): Server {
  return {
    id: 'srv-1',
    name: 'vps-web-01',
    os: 'Debian GNU/Linux 13',
    location: '',
    vcpu: 4,
    memoryGb: 8,
    status: 'online',
    cpuPercent: 12,
    memPercent: 25,
    diskPercent: 20,
    containers: 3,
    stacks: 1,
    tags: [],
    lastSeen: '2026-10-19T12:00:00Z',
    agentVersion: '0.2.0',
    agentLatestVersion: '0.2.8',
    agentUpdateAvailable: false,
    agentUpdateManual: false,
    agentUpgradeCommand: '',
    ...overrides,
  }
}

function alert(overrides: Partial<Alert> = {}): Alert {
  return {
    id: 'a1',
    ruleId: 'r1',
    ruleType: 'cpu_high',
    severity: 'critical',
    state: 'firing',
    serverId: 'srv-1',
    serverName: 'vps-web-01',
    subjectKind: 'server',
    subjectName: 'srv-1',
    stack: '',
    startedAt: '2026-10-19T10:00:00Z',
    resolvedAt: '',
    ackedAt: '',
    ackedBy: '',
    silenced: false,
    summary: null,
    ...overrides,
  }
}

const now = new Date('2026-10-19T12:00:00Z')

describe('buildAttention', () => {
  it('orders by priority, then severity and oldest first', () => {
    const servers = [
      server({ id: 'offline', name: 'offline', status: 'offline' }),
      server({ id: 'disk', name: 'disk', diskPercent: 96 }),
      server({
        id: 'security',
        name: 'security',
        apt: { upgradable: 2, security: 3, rebootRequired: false },
      }),
      server({
        id: 'reboot',
        name: 'reboot',
        apt: { upgradable: 0, security: 0, rebootRequired: true },
      }),
      server({ id: 'agent', name: 'agent', agentUpdateAvailable: true, agentUpdateManual: true }),
    ]
    const alerts = [
      alert({ id: 'new', startedAt: '2026-10-19T11:00:00Z' }),
      alert({ id: 'old', startedAt: '2026-10-19T09:00:00Z' }),
      alert({ id: 'warning', severity: 'warning', startedAt: '2026-10-19T10:30:00Z' }),
    ]

    const items = buildAttention(servers, alerts, now)

    expect(items.map((item) => item.id)).toEqual([
      'old',
      'new',
      'offline:offline',
      'warning',
      'disk:disk',
      'security:security',
      'reboot:reboot',
      'agent:agent',
    ])
    expect(items.map((item) => item.priority)).toEqual([1, 1, 2, 3, 4, 5, 6, 7])
    expect(items[4]?.tone).toBe('crit')
    expect(items[7]?.params.manual).toBe(1)
  })

  it('does not duplicate a disk or offline alert with a derived item', () => {
    const servers = [
      server({ id: 'srv-1', name: 'vps-web-01', status: 'offline', diskPercent: 96 }),
      server({ id: 'srv-2', name: 'vps-db-01', diskPercent: 91 }),
    ]
    const alerts = [
      alert({ id: 'offline', ruleType: 'server_offline', silenced: true }),
      alert({ id: 'disk', ruleType: 'disk_usage', ackedAt: '2026-10-19T11:00:00Z' }),
      alert({
        id: 'disk-real',
        ruleType: 'disk_high',
        serverId: 'srv-2',
        serverName: 'vps-db-01',
        silenced: true,
      }),
    ]

    expect(buildAttention(servers, alerts, now)).toEqual([])
  })

  it('excludes silenced, acknowledged and non-critical/warning alerts', () => {
    const alerts = [
      alert({ id: 'silenced', silenced: true }),
      alert({ id: 'acked', ackedAt: '2026-10-19T11:00:00Z' }),
      alert({ id: 'info', severity: 'info' }),
    ]

    expect(buildAttention([server()], alerts, now)).toEqual([])
  })

  it('uses the disk thresholds and skips limited servers', () => {
    const below = buildAttention([server({ id: 'below', diskPercent: 84 })], [], now)
    const warning = buildAttention([server({ id: 'warning', diskPercent: 85 })], [], now)
    const critical = buildAttention([server({ id: 'critical', diskPercent: 95 })], [], now)
    const limited = buildAttention(
      [
        server({
          id: 'limited',
          limited: true,
          status: 'offline',
          diskPercent: 99,
          apt: { upgradable: 1, security: 1, rebootRequired: true },
          agentUpdateAvailable: true,
        }),
      ],
      [],
      now,
    )

    expect(below).toEqual([])
    expect(warning[0]?.tone).toBe('warn')
    expect(critical[0]?.tone).toBe('crit')
    expect(limited).toEqual([])
  })

  it('builds the expected actions', () => {
    const items = buildAttention(
      [
        server({
          id: 'srv-1',
          name: 'vps-web-01',
          status: 'offline',
          diskPercent: 97,
          apt: { upgradable: 2, security: 1, rebootRequired: true },
          agentUpdateAvailable: true,
        }),
      ],
      [alert({ id: 'a1' })],
      now,
    )

    expect(items.find((item) => item.kind === 'alert')?.action).toEqual({
      kind: 'alert',
      to: '/alerts?focus=a1',
    })
    expect(items.find((item) => item.kind === 'offline')?.action).toEqual({
      kind: 'server',
      to: '/servers/srv-1',
    })
    expect(items.find((item) => item.kind === 'security')?.action).toEqual({
      kind: 'updates',
      to: '/servers/srv-1/updates',
    })
    expect(items.find((item) => item.kind === 'agent')?.action).toEqual({
      kind: 'agent',
      to: '/servers/srv-1',
    })
  })
})
