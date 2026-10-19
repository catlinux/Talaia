import { useTranslation } from 'react-i18next'

import { formatPercent } from './format'

interface MeterProps {
  label: string
  /** null when the value is unknown, for example for an offline server. */
  percent: number | null
}

// meterTone maps a percentage to the colour of the bar. The same information is
// always available as text, never by colour alone.
function meterTone(percent: number): 'ok' | 'warn' | 'crit' {
  if (percent >= 90) {
    return 'crit'
  }
  if (percent >= 75) {
    return 'warn'
  }
  return 'ok'
}

export function Meter({ label, percent }: MeterProps) {
  const { t, i18n } = useTranslation('fleet')
  const language = i18n.resolvedLanguage ?? i18n.language

  const known = percent !== null
  const tone = known ? meterTone(percent) : 'ok'
  const value = known ? formatPercent(percent, language) : t('metric.unknown')

  return (
    <div className={`meter meter--${tone}`}>
      <span className="meter__label">{label}</span>
      <div
        className="meter__track"
        role="meter"
        aria-label={t('metric.aria', { label, value })}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={known ? Math.round(percent) : undefined}
        aria-valuetext={value}
      >
        <span className="meter__fill" style={{ width: `${known ? percent : 0}%` }} />
      </div>
      <span className="meter__value">{value}</span>
    </div>
  )
}
