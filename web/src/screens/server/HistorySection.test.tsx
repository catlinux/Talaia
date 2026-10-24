import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/http'
import type { MetricsApi } from '../../api/metrics'
import i18n from '../../i18n'
import { HistorySection } from './HistorySection'

afterEach(cleanup)
describe('HistorySection', () => {
  it('refetches on range change and hides on forbidden', async () => {
    await i18n.changeLanguage('en')
    const server = vi.fn().mockResolvedValue({ resolution: 'raw', from: '', to: '', points: [] })
    const api = { server, container: vi.fn() } as unknown as MetricsApi
    render(<HistorySection api={api} serverId="s1" />)
    await waitFor(() => expect(server).toHaveBeenCalledTimes(1))
    screen.getByRole('radio', { name: '1 h' }).click()
    await waitFor(() => expect(server).toHaveBeenCalledWith('s1', '1h'))
    server.mockRejectedValue(new ApiError('forbidden', 403))
    screen.getByRole('radio', { name: '6 h' }).click()
    await waitFor(() => expect(screen.queryByRole('region', { name: 'History' })).toBeNull())
  })

  it('renders percent charts with used memory only and current values', async () => {
    await i18n.changeLanguage('en')
    const server = vi.fn().mockResolvedValue({
      resolution: 'raw',
      from: '',
      to: '',
      points: [
        {
          time: '2026-01-01T00:00:00Z',
          cpu: 34,
          mem_used: 3_100_000_000,
          mem_total: 8_000_000_000,
          disk_used: 50,
          disk_total: 100,
          net_rx_bps: 10,
          net_tx_bps: 20,
        },
      ],
    })
    const api = { server, container: vi.fn() } as unknown as MetricsApi
    const { container } = render(<HistorySection api={api} serverId="s1" />)
    await waitFor(() => expect(container.querySelectorAll('.chart-line')).toHaveLength(5))
    expect(container.querySelector('.history__chart-head strong')?.textContent).toContain('34')
    expect(
      container.querySelectorAll('.history__chart')[1]?.querySelectorAll('.chart-line'),
    ).toHaveLength(1)
    expect(screen.getByText(/3.1 GB of 8 GB/)).toBeTruthy()
  })
})
