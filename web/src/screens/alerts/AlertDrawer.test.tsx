import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Alert, AlertsApi } from '../../api/alerts'
import type { AlertsAdminApi, AlertRule } from '../../api/alertsAdmin'
import type { ContainersApi, Job } from '../../api/containers'
import type { MetricsApi, MetricsResponse } from '../../api/metrics'
import type { MeApi, Permission } from '../../api/me'
import { PermissionsProvider } from '../../auth/permissions'
import { ToastProvider } from '../../components/ui'
import i18n from '../../i18n'
import { RouterProvider } from '../../router'
import { AlertDrawer } from './AlertDrawer'

function alert(overrides: Partial<Alert> = {}): Alert {
  return {
    id: 'a1',
    ruleId: 'r1',
    ruleType: 'cpu_high',
    severity: 'critical',
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

function rule(overrides: Partial<AlertRule> = {}): AlertRule {
  return {
    id: 'r1',
    name: 'CPU rule',
    type: 'cpu_high',
    enabled: true,
    severity: 'critical',
    params: { threshold: 90, for_seconds: 600 },
    scopeType: 'organization',
    serverId: null,
    groupId: null,
    scopeName: null,
    channelIds: [],
    repeatIntervalSeconds: 300,
    builtin: true,
    ...overrides,
  }
}

function api(): AlertsApi {
  const unexpected = () => Promise.reject(new Error('unexpected'))
  return {
    list: () => Promise.resolve([]),
    summary: () => Promise.resolve({ counts: { warning: 0, critical: 0, info: 0 }, servers: {} }),
    silences: () => Promise.resolve([]),
    acknowledge: vi.fn(() => Promise.resolve()),
    unwatch: vi.fn(() => Promise.resolve()),
    createSilence: unexpected,
    deleteSilence: unexpected,
  }
}

function me(
  permissions: string[] = [
    'alerts.view',
    'alerts.silence',
    'alerts.manage',
    'container.control',
    'container.logs',
    'container.terminal',
  ],
): MeApi {
  return {
    get: () =>
      Promise.resolve({
        user: { id: 'u1', username: 'admin', display_name: 'Admin', email: '' },
        bindings: { bindings: [] },
        permissions: {
          organization: permissions as Permission[],
          servers: {},
          stacks: {},
          containers: {},
        },
      }),
  }
}

function metrics(): MetricsApi {
  const response: MetricsResponse = {
    resolution: 'raw',
    from: '2026-10-24T09:00:00Z',
    to: '2026-10-24T10:00:00Z',
    points: [
      {
        time: '2026-10-24T09:00:00Z',
        cpu: 40,
        mem_used: 50,
        mem_total: 100,
        disk_used: 40,
        disk_total: 100,
      },
      {
        time: '2026-10-24T10:00:00Z',
        cpu: 91,
        mem_used: 60,
        mem_total: 100,
        disk_used: 50,
        disk_total: 100,
      },
    ],
  }
  return {
    server: vi.fn(() => Promise.resolve(response)),
    container: vi.fn(() => Promise.resolve(response)),
  }
}

function admin(rules: AlertRule[] = [rule()]): AlertsAdminApi {
  return { listRules: vi.fn(() => Promise.resolve(rules)) } as unknown as AlertsAdminApi
}

function containers(
  run = vi.fn(() => Promise.resolve('job')),
  job = vi.fn((): Promise<Job> =>
    Promise.resolve({ id: 'job', state: 'succeeded', message: '', output: '' }),
  ),
) {
  return {
    run,
    job,
  } as unknown as ContainersApi
}

function renderDrawer(
  target: Alert | null,
  options: {
    missing?: boolean
    alertsApi?: AlertsApi
    adminApi?: AlertsAdminApi
    metricsApi?: MetricsApi
    containerApi?: ContainersApi
  } = {},
) {
  const onRefresh = vi.fn()
  const onClose = vi.fn()
  const view = render(
    <ToastProvider>
      <RouterProvider>
        <PermissionsProvider meApi={me()}>
          <AlertDrawer
            alert={target}
            missing={options.missing}
            api={options.alertsApi ?? api()}
            alertsAdminApi={options.adminApi ?? admin()}
            metricsApi={options.metricsApi ?? metrics()}
            containersApi={options.containerApi}
            onClose={onClose}
            onRefresh={onRefresh}
            pollMs={1}
          />
        </PermissionsProvider>
      </RouterProvider>
    </ToastProvider>,
  )
  return { ...view, onRefresh, onClose }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.history.replaceState({}, '', '/alerts')
})

afterEach(() => {
  cleanup()
  window.history.replaceState({}, '', '/')
})

describe('AlertDrawer', () => {
  it('shows the context, timeline, rule and metric chart', async () => {
    renderDrawer(
      alert({
        ackedAt: '2026-10-24T10:05:00Z',
        ackedBy: 'ana',
        resolvedAt: '2026-10-24T11:00:00Z',
      }),
    )

    expect(await screen.findByText('CPU at 91% on vps-web-01')).toBeTruthy()
    expect(screen.getAllByText('Resolved').length).toBeGreaterThan(0)
    expect(screen.getByText('Started')).toBeTruthy()
    expect(screen.getByText('Acknowledged by ana')).toBeTruthy()
    expect(await screen.findByText('CPU rule')).toBeTruthy()
    expect(screen.getAllByText('CPU high').length).toBeGreaterThan(0)
    expect(screen.getByText(/> 90 % for 10 min/)).toBeTruthy()
    expect(screen.getByRole('link', { name: 'vps-web-01' }).getAttribute('href')).toBe(
      '/servers/s1',
    )
    expect(screen.getByRole('link', { name: 'Edit rule' }).getAttribute('href')).toBe(
      '/alerts/rules?rule=r1',
    )
    expect(screen.getByRole('img', { name: /CPU/ })).toBeTruthy()
  })

  it('asks before restarting a container and reports the job', async () => {
    const run = vi.fn(() => Promise.resolve('job'))
    const job = vi.fn(() =>
      Promise.resolve<Job>({ id: 'job', state: 'succeeded', message: '', output: '' }),
    )
    renderDrawer(
      alert({
        ruleType: 'container_unhealthy',
        subjectKind: 'container',
        subjectName: 'web',
        stack: 'shop',
        summary: { key: 'alerts.container_unhealthy', params: {} },
      }),
      { containerApi: containers(run, job) },
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Restart container' }))
    const dialog = screen.getByRole('dialog', { name: /Restart web/ })
    expect(within(dialog).getByText('Restart web?')).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Restart container' }))

    await waitFor(() => expect(run).toHaveBeenCalledWith('s1', 'web', 'restart'))
    expect(await screen.findByText('Restart container on web finished.')).toBeTruthy()
  })

  it('shows a missing message when the alert is gone', () => {
    renderDrawer(null, { missing: true })
    expect(screen.getByText('This alert no longer exists.')).toBeTruthy()
  })
})
