import { describe, expect, it, vi } from 'vitest'
import { ApiError } from './http'
import { createMetricsApi, isHiddenMetricsError } from './metrics'

describe('metrics API', () => {
  it('maps nullable server measurements and encodes the range', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            resolution: '5m',
            from: 'a',
            to: 'b',
            points: [
              {
                time: '2026-10-24T10:00:00Z',
                cpu: null,
                mem_used: 12,
                mem_total: 100,
                net_rx_bps: 4,
              },
            ],
          }),
          { status: 200 },
        ),
      ),
    )
    const api = createMetricsApi(fetchImpl as unknown as typeof fetch)
    await expect(api.server('srv 1', '6h')).resolves.toMatchObject({
      points: [{ cpu: null, mem_used: 12, net_rx_bps: 4 }],
    })
    expect((fetchImpl.mock.calls[0] as unknown as [string])[0]).toBe(
      '/api/v1/servers/srv%201/metrics?range=6h',
    )
  })
  it('handles forbidden responses as hidden metrics', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ error: { code: 'forbidden' } }), { status: 403 }),
      ),
    )
    await expect(
      createMetricsApi(fetchImpl as unknown as typeof fetch).server('srv', '1h'),
    ).rejects.toBeInstanceOf(ApiError)
    expect(isHiddenMetricsError(new ApiError('forbidden', 403))).toBe(true)
  })
})
