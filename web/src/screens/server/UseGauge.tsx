interface UseGaugeProps {
  label: string
  /** percent fills the bar; it is clamped to 0..100. */
  percent: number
  /** value is the text shown next to the bar. */
  value: string
}

/** UseGauge is a small segmented gauge, the same instrument the fleet cards use. */
export function UseGauge({ label, percent, value }: UseGaugeProps) {
  const clamped = Math.max(0, Math.min(100, percent))
  const tone = clamped >= 90 ? 'crit' : clamped >= 75 ? 'warn' : 'ok'

  return (
    <div className={`meter meter--${tone}`}>
      <span className="meter__label">{label}</span>
      <div
        className="meter__track"
        role="meter"
        aria-label={`${label}: ${value}`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(clamped)}
        aria-valuetext={value}
      >
        <span className="meter__fill" style={{ width: `${clamped}%` }} />
      </div>
      <span className="meter__value">{value}</span>
    </div>
  )
}
