import { beforeEach, describe, expect, it } from 'vitest'

import type { Alert } from '../../api/alerts'
import i18n from '../../i18n'
import { alertMessageText } from './AlertMessage'

function alert(overrides: Partial<Alert> = {}): Alert {
  return {
    id: 'a1',
    ruleId: 'r1',
    ruleType: 'cpu_high',
    severity: 'warning',
    state: 'firing',
    serverId: 's1',
    serverName: 'vps-web-01',
    subjectKind: 'server',
    subjectName: 'vps-web-01',
    stack: '',
    startedAt: '2026-10-24T10:00:00Z',
    resolvedAt: '',
    ackedAt: '',
    ackedBy: '',
    silenced: false,
    summary: { key: 'alerts.cpu_high', params: { value: 91 } },
    ...overrides,
  }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

function text(value: Alert): string {
  return alertMessageText(i18n.getFixedT('en', 'alerts'), value)
}

describe('alertMessageText', () => {
  it('renders the normal templates with their measured value', () => {
    expect(text(alert())).toBe('CPU at 91% on vps-web-01')
  })

  it('uses the rule label when a measured template has no value', () => {
    expect(text(alert({ summary: { key: 'alerts.cpu_high', params: {} } }))).toBe(
      'CPU high on vps-web-01',
    )
    expect(text(alert({ summary: null, ruleType: 'mem_high' }))).toBe('Memory high on vps-web-01')
  })

  it('falls back to the rule label for unknown rule types', () => {
    expect(
      text(
        alert({ ruleType: 'mystery_rule', summary: { key: 'alerts.mystery_rule', params: {} } }),
      ),
    ).toBe('mystery_rule')
    expect(text(alert({ ruleType: 'other_rule', summary: null }))).toBe('other_rule')
  })
})
