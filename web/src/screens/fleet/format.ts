/**
 * Formatting helpers. Every value is formatted with Intl and the active
 * language, so the numbers, sizes and dates follow the locale.
 */

/** Formats a plain count, for example 1234 as "1,234" in English. */
export function formatNumber(value: number, language: string): string {
  return new Intl.NumberFormat(language).format(value)
}

/** Formats a 0-100 percentage, for example 23 as "23%". */
export function formatPercent(value: number, language: string): string {
  return new Intl.NumberFormat(language, {
    style: 'unit',
    unit: 'percent',
    unitDisplay: 'narrow',
    maximumFractionDigits: 0,
  }).format(value)
}

const sizeUnits = ['B', 'kB', 'MB', 'GB', 'TB', 'PB']

/** Formats a size in bytes with a decimal unit, for example "3.2 GB". */
export function formatBytes(bytes: number, language: string): string {
  if (!Number.isFinite(bytes) || bytes < 0) {
    return ''
  }

  let value = bytes
  let unit = 0
  while (value >= 1000 && unit < sizeUnits.length - 1) {
    value /= 1000
    unit += 1
  }

  const maximumFractionDigits = unit > 0 && value < 10 ? 1 : 0
  const formatted = new Intl.NumberFormat(language, { maximumFractionDigits }).format(value)
  return `${formatted} ${sizeUnits[unit]}`
}

const relativeUnits = [
  ['year', 31_536_000],
  ['month', 2_592_000],
  ['day', 86_400],
  ['hour', 3_600],
  ['minute', 60],
  ['second', 1],
] as const

/** Formats an instant relative to `now`, for example "6 minutes ago". */
export function formatRelativeTime(at: Date, now: Date, language: string): string {
  const seconds = (at.getTime() - now.getTime()) / 1000
  const formatter = new Intl.RelativeTimeFormat(language, { numeric: 'auto' })

  for (const [unit, unitSeconds] of relativeUnits) {
    if (unit === 'second' || Math.abs(seconds) >= unitSeconds) {
      return formatter.format(Math.round(seconds / unitSeconds), unit)
    }
  }

  return formatter.format(0, 'second')
}

/** Formats a clock time for the active language, for example "03:12". */
export function formatTime(at: Date, language: string): string {
  return new Intl.DateTimeFormat(language, { hour: '2-digit', minute: '2-digit' }).format(at)
}
