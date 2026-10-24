import type { ChartPoint } from './TimeSeriesChart'

export function Sparkline({ points, color }: { points: ChartPoint[]; color: 'accent' | 'ok' }) {
  const valid = points.filter((point) => point.v !== null)
  if (valid.length < 2) return <span className="sparkline sparkline--empty">—</span>
  const min = Math.min(...valid.map((p) => p.v!)),
    max = Math.max(...valid.map((p) => p.v!))
  const d = valid
    .map(
      (p, i) =>
        `${i ? 'L' : 'M'}${(i * 100) / (valid.length - 1)},${24 - ((p.v! - min) / (max - min || 1)) * 20}`,
    )
    .join(' ')
  return (
    <svg className="sparkline" viewBox="0 0 100 26" aria-hidden="true">
      <path
        d={d}
        fill="none"
        stroke={color === 'accent' ? 'var(--accent)' : 'var(--ok)'}
        strokeWidth="1.5"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}
