import { ApiError, createHttpClient } from './http'

export type MetricsRange = '1h' | '6h' | '24h' | '7d' | '30d'
export interface MetricPoint {
  time: string
  cpu: number | null
  mem_used: number | null
  mem_total?: number | null
  mem_limit?: number | null
  disk_used?: number | null
  disk_total?: number | null
  net_rx_bps?: number | null
  net_tx_bps?: number | null
}
export interface MetricsResponse {
  resolution: 'raw' | '5m' | '1h' | string
  from: string
  to: string
  points: MetricPoint[]
}
export interface MetricsApi {
  server(serverId: string, range: MetricsRange): Promise<MetricsResponse>
  container(serverId: string, name: string, range: MetricsRange): Promise<MetricsResponse>
}
interface ApiMetricsResponse {
  resolution?: string
  from?: string
  to?: string
  points?: Array<Record<string, unknown>>
}

function numberOrNull(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}
function mapResponse(body: ApiMetricsResponse): MetricsResponse {
  return {
    resolution: body.resolution ?? 'raw',
    from: body.from ?? '',
    to: body.to ?? '',
    points: (body.points ?? []).map((p) => ({
      time: typeof p.time === 'string' ? p.time : '',
      cpu: numberOrNull(p.cpu),
      mem_used: numberOrNull(p.mem_used),
      mem_total: numberOrNull(p.mem_total),
      mem_limit: numberOrNull(p.mem_limit),
      disk_used: numberOrNull(p.disk_used),
      disk_total: numberOrNull(p.disk_total),
      net_rx_bps: numberOrNull(p.net_rx_bps),
      net_tx_bps: numberOrNull(p.net_tx_bps),
    })),
  }
}

export function createMetricsApi(fetchImpl?: typeof fetch): MetricsApi {
  const http = createHttpClient(fetchImpl)
  return {
    server: async (serverId, range) =>
      mapResponse(
        await http.get<ApiMetricsResponse>(
          `/api/v1/servers/${encodeURIComponent(serverId)}/metrics?range=${range}`,
        ),
      ),
    container: async (serverId, name, range) =>
      mapResponse(
        await http.get<ApiMetricsResponse>(
          `/api/v1/servers/${encodeURIComponent(serverId)}/containers/${encodeURIComponent(name)}/metrics?range=${range}`,
        ),
      ),
  }
}

export function isHiddenMetricsError(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 403 || error.status === 404)
}
