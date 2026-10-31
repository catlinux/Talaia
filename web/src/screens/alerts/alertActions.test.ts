import { describe, expect, it, vi } from 'vitest'

import type { Alert } from '../../api/alerts'
import { alertActions } from './alertActions'

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
    summary: null,
    ...overrides,
  }
}

const all = vi.fn(() => true)
const none = vi.fn(() => false)

describe('alertActions', () => {
  it('offers a start and logs for a down container with permission', () => {
    const actions = alertActions(alert(), all)
    expect(actions.map((action) => action.id)).toEqual(['start-container', 'logs', 'terminal'])
    expect(actions[0]).toMatchObject({ kind: 'container', containerAction: 'start' })
  })

  it('offers a restart for an unhealthy container and respects log permissions', () => {
    const actions = alertActions(
      alert({ ruleType: 'container_unhealthy' }),
      (permission) => permission === 'container.control',
    )
    expect(actions.map((action) => action.id)).toEqual(['restart-container'])
  })

  it('does not offer container controls without permission', () => {
    expect(alertActions(alert(), none).map((action) => action.id)).toEqual([])
  })

  it('opens the server and the services tab for a failed check', () => {
    const actions = alertActions(
      alert({ ruleType: 'check_failed', subjectKind: 'check', subjectName: 'api', stack: '' }),
      all,
    )
    expect(actions.map((action) => action.id)).toEqual(['open-services'])
    expect(actions[0]?.href).toBe('/servers/s1/services')
  })

  it('opens the server and cleanup for a full disk', () => {
    const actions = alertActions(
      alert({ ruleType: 'disk_high', subjectKind: 'server', subjectName: 'vps-web-01', stack: '' }),
      all,
    )
    expect(actions.map((action) => action.id)).toEqual(['open-server', 'cleanup-resources'])
    expect(actions[1]?.href).toBe('/servers/s1/containers')
  })

  it('opens the server for CPU and memory', () => {
    for (const ruleType of ['cpu_high', 'mem_high']) {
      expect(
        alertActions(alert({ ruleType, subjectKind: 'server', stack: '' }), all).map(
          (action) => action.id,
        ),
      ).toEqual(['open-server'])
    }
  })

  it('opens the server for an offline agent and the stack for a stack alert', () => {
    expect(
      alertActions(
        alert({ ruleType: 'server_offline', subjectKind: 'server', stack: '' }),
        all,
      ).map((action) => action.id),
    ).toEqual(['open-server'])
    expect(
      alertActions(alert({ ruleType: 'cert_expiry', subjectKind: 'stack' }), all),
    ).toContainEqual({ id: 'open-stack', kind: 'link', href: '/servers/s1/stacks/shop' })
  })
})
