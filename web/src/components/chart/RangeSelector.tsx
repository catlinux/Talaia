import type { MetricsRange } from '../../api/metrics'
import { useTranslation } from 'react-i18next'
import { useRef } from 'react'

const ranges: MetricsRange[] = ['1h', '6h', '24h', '7d', '30d']
export function RangeSelector({
  value,
  onChange,
}: {
  value: MetricsRange
  onChange: (range: MetricsRange) => void
}) {
  const { t } = useTranslation('metrics')
  const buttons = useRef<Array<HTMLButtonElement | null>>([])
  return (
    <div className="chart-range" role="radiogroup" aria-label={t('range.label')}>
      {ranges.map((range, buttonIndex) => (
        <button
          key={range}
          type="button"
          role="radio"
          aria-checked={value === range}
          tabIndex={value === range ? 0 : -1}
          className={value === range ? 'chart-range__button is-active' : 'chart-range__button'}
          onClick={() => onChange(range)}
          onKeyDown={(event) => {
            const index = ranges.indexOf(value)
            const next =
              event.key === 'ArrowRight'
                ? (index + 1) % ranges.length
                : event.key === 'ArrowLeft'
                  ? (index - 1 + ranges.length) % ranges.length
                  : -1
            if (next >= 0) {
              event.preventDefault()
              onChange(ranges[next]!)
              buttons.current[next]?.focus()
            }
          }}
          ref={(element) => {
            buttons.current[buttonIndex] = element
          }}
        >
          {t(`range.${range}`)}
        </button>
      ))}
    </div>
  )
}
