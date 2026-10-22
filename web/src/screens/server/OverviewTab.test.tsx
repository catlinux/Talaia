import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { OverviewTab, type OverviewServer } from './OverviewTab'

const gib = 1024 ** 3

function server(overrides: Partial<OverviewServer> = {}): OverviewServer {
  return {
    os: 'Debian GNU/Linux 13',
    kernel: '6.12.0-amd64',
    arch: 'amd64',
    vcpu: 4,
    memoryGb: 8,
    cpuPercent: 23,
    memPercent: 61,
    diskPercent: 91,
    memUsedBytes: 5 * gib,
    memTotalBytes: 8 * gib,
    diskUsedBytes: 146 * gib,
    diskTotalBytes: 160 * gib,
    netRxBytes: 48_000_000_000,
    netTxBytes: 12_000_000_000,
    containers: 14,
    runningContainers: 10,
    services: 38,
    dockerVersion: '27.1.1',
    uptimeSeconds: 3 * 86_400 + 4 * 3_600,
    apt: {
      upgradable: 8,
      security: 3,
      rebootRequired: true,
    },
    ...overrides,
  }
}

describe('OverviewTab', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('shows the three use gauges and the machine facts', () => {
    render(
      <OverviewTab
        server={server()}
        agentVersion="0.2.0"
        agentUpdateAvailable
        failedServices={2}
        onGoToTab={() => undefined}
      />,
    )

    expect(screen.getAllByRole('meter')).toHaveLength(3)
    expect(screen.getByText('Debian GNU/Linux 13')).toBeTruthy()
    expect(screen.getByText('6.12.0-amd64')).toBeTruthy()
    expect(screen.getByText('amd64')).toBeTruthy()
    expect(screen.getByText('27.1.1')).toBeTruthy()
    expect(screen.getByText('10 of 14 running')).toBeTruthy()
    expect(screen.getByText('3 d 4 h')).toBeTruthy()
    expect(screen.getByText(/48 GB in/)).toBeTruthy()
  })

  it('lists the things that need attention and opens their tab', () => {
    const onGoToTab = vi.fn()
    render(
      <OverviewTab
        server={server()}
        agentVersion="0.2.0"
        agentUpdateAvailable
        failedServices={2}
        onGoToTab={onGoToTab}
      />,
    )

    expect(screen.getByRole('heading', { level: 2, name: 'Needs attention' })).toBeTruthy()
    expect(screen.getByText('A reboot is required after the updates.')).toBeTruthy()
    expect(screen.getByText('3 security updates are pending.')).toBeTruthy()
    expect(screen.getByText('Disk is at 91%.')).toBeTruthy()
    expect(screen.getByText(/2 services have failed/)).toBeTruthy()
    expect(screen.getByText('A newer agent is available.')).toBeTruthy()

    fireEvent.click(screen.getAllByRole('button', { name: 'Open updates' })[0]!)
    fireEvent.click(screen.getByRole('button', { name: 'Open containers' }))
    fireEvent.click(screen.getByRole('button', { name: 'Open services' }))

    expect(onGoToTab.mock.calls).toEqual([['updates'], ['containers'], ['services']])
  })

  it('hides the attention section when nothing needs it', () => {
    render(
      <OverviewTab
        server={server({
          diskPercent: 30,
          apt: { upgradable: 0, security: 0, rebootRequired: false },
        })}
        agentVersion="0.3.0"
        agentUpdateAvailable={false}
        failedServices={0}
        onGoToTab={() => undefined}
      />,
    )

    expect(screen.queryByRole('heading', { name: 'Needs attention' })).toBeNull()
    expect(screen.getByRole('heading', { level: 2, name: 'Facts' })).toBeTruthy()
  })

  it('shows unknown values for an offline server', () => {
    render(
      <OverviewTab
        server={server({
          os: '',
          kernel: '',
          arch: '',
          cpuPercent: null,
          memPercent: null,
          diskPercent: null,
          memUsedBytes: 0,
          memTotalBytes: 0,
          diskUsedBytes: 0,
          diskTotalBytes: 0,
          netRxBytes: 0,
          netTxBytes: 0,
          containers: 0,
          runningContainers: 0,
          services: 0,
          dockerVersion: '',
          uptimeSeconds: 0,
          apt: undefined,
        })}
        agentVersion=""
        agentUpdateAvailable={false}
        onGoToTab={() => undefined}
      />,
    )

    expect(screen.getAllByRole('meter')).toHaveLength(3)
    expect(screen.getAllByText('unknown').length).toBeGreaterThan(0)
  })
})
