import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  isHiddenMetricsError,
  type MetricsApi,
  type MetricsRange,
  type MetricsResponse,
} from '../../api/metrics'
import { RangeSelector } from '../../components/chart/RangeSelector'
import { TimeSeriesChart, type ChartSeries } from '../../components/chart/TimeSeriesChart'
import { formatBytes, formatPercent } from '../fleet/format'

export function HistorySection({ api, serverId }: { api: MetricsApi; serverId: string }) {
  const { t, i18n } = useTranslation('metrics')
  const [range, setRange] = useState<MetricsRange>('24h')
  const [data, setData] = useState<MetricsResponse | null>(null)
  const [loadedRange, setLoadedRange] = useState<MetricsRange | null>(null)
  const [hidden, setHidden] = useState(false)
  const [failed, setFailed] = useState(false)
  const refresh = useCallback(async () => {
    try {
      const result = await api.server(serverId, range)
      setData(result)
      setFailed(false)
    } catch (error) {
      if (isHiddenMetricsError(error)) setHidden(true)
      else setFailed(true)
    } finally {
      setLoadedRange(range)
    }
  }, [api, range, serverId])
  useEffect(() => {
    const first = window.setTimeout(() => void refresh(), 0)
    const timer = window.setInterval(() => void refresh(), 30_000)
    return () => {
      window.clearTimeout(first)
      window.clearInterval(timer)
    }
  }, [refresh])
  const loading = loadedRange !== range
  if (hidden) return null
  const points = data?.points ?? []
  const values = (get: (p: MetricsResponse['points'][number]) => number | null | undefined) =>
    points
      .map((point) => ({ t: Date.parse(point.time), v: get(point) ?? null }))
      .filter((point) => Number.isFinite(point.t))
  const cpu: ChartSeries[] = [
    { id: 'cpu', label: t('series.cpu'), color: 'accent', points: values((p) => p.cpu) },
  ]
  const memory: ChartSeries[] = [
    {
      id: 'used',
      label: t('series.used'),
      color: 'ok',
      points: values((p) =>
        p.mem_used !== null && p.mem_total ? (p.mem_used / p.mem_total) * 100 : null,
      ),
    },
  ]
  const disk: ChartSeries[] = [
    {
      id: 'disk',
      label: t('series.used'),
      color: 'warn',
      points: values((p) =>
        p.disk_used != null && p.disk_total ? (p.disk_used / p.disk_total) * 100 : null,
      ),
    },
  ]
  const network: ChartSeries[] = [
    { id: 'rx', label: t('series.receive'), color: 'accent', points: values((p) => p.net_rx_bps) },
    { id: 'tx', label: t('series.transmit'), color: 'ok', points: values((p) => p.net_tx_bps) },
  ]
  const language = i18n.resolvedLanguage ?? i18n.language
  const latest = points.at(-1)
  const latestPercent = (value: number | null | undefined) =>
    value == null ? '—' : formatPercent(value, language)
  const latestRate = (value: number | null | undefined) =>
    value == null ? '—' : `${formatBytes(value, language)}/s`
  const memorySubtitle =
    latest?.mem_used != null && latest.mem_total != null
      ? t('chart.memoryTotal', {
          used: formatBytes(latest.mem_used, language),
          total: formatBytes(latest.mem_total, language),
        })
      : null
  return (
    <section className="history" aria-labelledby="history-title">
      <header className="history__head">
        <h2 className="overview__title" id="history-title">
          {t('history.title')}
        </h2>
        <RangeSelector value={range} onChange={setRange} />
      </header>
      {failed ? (
        <p className="chart-error" role="alert">
          {t('chart.error')}
        </p>
      ) : null}
      <div className="history__grid">
        <article className="history__chart">
          <h3 className="history__chart-head">
            <span>{t('chart.cpu')}</span>
            <strong>{latestPercent(latest?.cpu)}</strong>
          </h3>
          <TimeSeriesChart
            label={t('chart.cpu')}
            series={cpu}
            yDomain={[0, 100]}
            format="percent"
            range={range}
            height={170}
            loading={loading}
          />
        </article>
        <article className="history__chart">
          <h3 className="history__chart-head">
            <span>{t('chart.memory')}</span>
            <strong>
              {latestPercent(
                latest?.mem_used != null && latest.mem_total
                  ? (latest.mem_used / latest.mem_total) * 100
                  : null,
              )}
            </strong>
          </h3>
          {memorySubtitle ? <p className="history__subtitle">{memorySubtitle}</p> : null}
          <TimeSeriesChart
            label={t('chart.memory')}
            series={memory}
            yDomain={[0, 100]}
            format="percent"
            range={range}
            height={170}
            loading={loading}
          />
        </article>
        <article className="history__chart">
          <h3 className="history__chart-head">
            <span>{t('chart.disk')}</span>
            <strong>
              {latestPercent(
                latest?.disk_used != null && latest.disk_total
                  ? (latest.disk_used / latest.disk_total) * 100
                  : null,
              )}
            </strong>
          </h3>
          <TimeSeriesChart
            label={t('chart.disk')}
            series={disk}
            yDomain={[0, 100]}
            format="percent"
            range={range}
            height={170}
            loading={loading}
          />
        </article>
        <article className="history__chart">
          <h3 className="history__chart-head">
            <span>{t('chart.network')}</span>
            <strong>
              {t('chart.networkLatest', {
                rx: latestRate(latest?.net_rx_bps),
                tx: latestRate(latest?.net_tx_bps),
              })}
            </strong>
          </h3>
          <TimeSeriesChart
            label={t('chart.network')}
            series={network}
            format="bytes-per-second"
            range={range}
            height={170}
            loading={loading}
          />
        </article>
      </div>
    </section>
  )
}
