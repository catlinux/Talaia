import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { formatBytes, formatPercent } from '../../screens/fleet/format'

export interface ChartPoint {
  t: number
  v: number | null
}
export interface ChartSeries {
  id: string
  label: string
  points: ChartPoint[]
  color: 'accent' | 'ok' | 'warn' | 'crit'
}
export type ChartFormat = 'percent' | 'bytes' | 'bytes-per-second'
interface Props {
  series: ChartSeries[]
  yDomain?: [number, number]
  format: ChartFormat
  height?: number
  loading?: boolean
  range?: string
  label: string
}
export function TimeSeriesChart({
  series,
  yDomain,
  format,
  height = 170,
  loading = false,
  range = '24h',
  label,
}: Props) {
  const { t, i18n } = useTranslation('metrics')
  const language = i18n.resolvedLanguage ?? i18n.language
  const [active, setActive] = useState<number | null>(null)
  const [width, setWidth] = useState(640)
  const container = useRef<HTMLDivElement>(null)
  const values = series.flatMap((item) =>
    item.points
      .map((point) => point.v)
      .filter((v): v is number => v !== null && Number.isFinite(v)),
  )
  const requestedDomain = yDomain ?? [Math.min(0, ...values), Math.max(1, ...values)]
  const domain: [number, number] = format === 'percent' ? [0, 100] : requestedDomain
  const min = values.length ? Math.min(...values) : 0
  const max = values.length ? Math.max(...values) : 0
  const avg = values.length ? values.reduce((sum, v) => sum + v, 0) / values.length : 0
  const summary = t('chart.summary', {
    min: formatValue(min, format, language),
    avg: formatValue(avg, format, language),
    max: formatValue(max, format, language),
  })
  const allPoints = useMemo(() => series.flatMap((s) => s.points.map((p) => p.t)), [series])
  const bounds = [Math.min(...allPoints), Math.max(...allPoints)]
  useEffect(() => {
    const element = container.current
    if (!element) return
    const update = () => {
      if (element.clientWidth > 0) setWidth(element.clientWidth)
    }
    update()
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(update)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  const left = 52,
    right = 14,
    top = 8,
    bottom = 25
  const plotW = width - left - right,
    plotH = height - top - bottom
  const x = (time: number) =>
    left + (bounds[1] === bounds[0] ? 0 : (time - bounds[0]) / (bounds[1] - bounds[0])) * plotW
  const y = (value: number) =>
    top + (1 - (value - domain[0]) / (domain[1] - domain[0] || 1)) * plotH
  const yTicks =
    format === 'percent'
      ? [0, 25, 50, 75, 100]
      : [0, 1, 2, 3].map((i) => domain[0] + ((domain[1] - domain[0]) * i) / 3)
  const activeTime = active === null ? null : allPoints[active]
  const dateFormat = new Intl.DateTimeFormat(
    language,
    range === '1h' || range === '6h'
      ? { hour: '2-digit', minute: '2-digit' }
      : { month: 'short', day: 'numeric', hour: '2-digit' },
  )
  const exactDateFormat = new Intl.DateTimeFormat(language, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
  if (loading)
    return (
      <div className="chart-state" role="status">
        {t('chart.loading')}
      </div>
    )
  if (!values.length) return <div className="chart-state">{t('chart.empty')}</div>
  return (
    <div
      ref={container}
      className="timeseries"
      role="img"
      aria-label={`${label}. ${series.map((item) => item.label).join(', ')}. ${summary}`}
    >
      <span className="timeseries__summary">{summary}</span>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        width="100%"
        height={height}
        onMouseLeave={() => setActive(null)}
      >
        {yTicks.map((val, i) => {
          const yy = y(val)
          return (
            <g key={i}>
              <line x1={left} x2={width - right} y1={yy} y2={yy} className="chart-grid" />
              <text x={left - 7} y={yy + 4} textAnchor="end" className="chart-axis">
                {formatValue(val, format, language)}
              </text>
            </g>
          )
        })}
        {series.map((item) => {
          const groups: ChartPoint[][] = []
          for (const [pointIndex, point] of item.points.entries()) {
            if (point.v === null) continue
            const last = groups[groups.length - 1]
            if (!last || item.points[pointIndex - 1]?.v === null) groups.push([point])
            else last.push(point)
          }
          return groups.map((group, index) => {
            const d = group.map((p, i) => `${i ? 'L' : 'M'}${x(p.t)},${y(p.v!)}`).join(' ')
            const area =
              series.length === 1 && group.length > 1
                ? `${d} L${x(group[group.length - 1]!.t)},${top + plotH} L${x(group[0]!.t)},${top + plotH} Z`
                : ''
            return (
              <g key={`${item.id}-${index}`}>
                <path d={area} className={`chart-area chart-color--${item.color}`} />
                <path
                  d={d}
                  fill="none"
                  className={`chart-line chart-color--${item.color}`}
                  strokeWidth="1.5"
                  vectorEffect="non-scaling-stroke"
                />
              </g>
            )
          })
        })}
        {activeTime !== null ? (
          <line
            x1={x(activeTime)}
            x2={x(activeTime)}
            y1={top}
            y2={top + plotH}
            className="chart-crosshair"
          />
        ) : null}
        {allPoints.map((time, index) => (
          <rect
            key={`${time}-${index}`}
            x={x(time) - Math.max(2, plotW / allPoints.length / 2)}
            y={top}
            width={Math.max(4, plotW / allPoints.length)}
            height={plotH}
            fill="transparent"
            tabIndex={0}
            onFocus={() => setActive(index)}
            onMouseEnter={() => setActive(index)}
            aria-label={dateFormat.format(time)}
          />
        ))}
        <text x={left} y={height - 6} className="chart-axis">
          {dateFormat.format(bounds[0])}
        </text>
        <text x={width - right} y={height - 6} textAnchor="end" className="chart-axis">
          {dateFormat.format(bounds[1])}
        </text>
      </svg>
      <div className="chart-legend" aria-hidden="true">
        {series.map((item) => (
          <span key={item.id}>
            <i className={`chart-dot--${item.color}`} />
            {item.label}
          </span>
        ))}
      </div>
      {activeTime !== null ? (
        <div className="chart-tooltip" role="status">
          <strong>{exactDateFormat.format(activeTime)}</strong>
          {series.map((item) => {
            const p = item.points.find((point) => point.t === activeTime)
            return (
              <span key={item.id}>
                <i className={`chart-dot--${item.color}`} />
                {item.label}:{' '}
                {p?.v === null || p === undefined
                  ? t('chart.empty')
                  : formatValue(p.v, format, language)}
              </span>
            )
          })}
        </div>
      ) : null}
    </div>
  )
}
function formatValue(value: number, format: ChartFormat, language: string): string {
  if (format === 'percent') return formatPercent(value, language)
  if (format === 'bytes') return formatBytes(value, language)
  return `${formatBytes(value, language)}/s`
}
