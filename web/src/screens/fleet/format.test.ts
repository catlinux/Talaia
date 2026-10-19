import { describe, expect, it } from 'vitest'

import { formatBytes, formatNumber, formatPercent, formatRelativeTime, formatTime } from './format'

describe('formatPercent', () => {
  it('formats a percentage with the active language', () => {
    expect(formatPercent(23, 'en')).toBe('23%')
    expect(formatPercent(91, 'en')).toBe('91%')
    expect(formatPercent(23, 'ca')).toMatch(/^23\s%$/)
    expect(formatPercent(23, 'es')).toMatch(/^23\s%$/)
  })
})

describe('formatBytes', () => {
  it('formats a size with a decimal unit', () => {
    expect(formatBytes(3_200_000_000, 'en')).toBe('3.2 GB')
    expect(formatBytes(3_200_000_000, 'ca')).toBe('3,2 GB')
    expect(formatBytes(512, 'en')).toBe('512 B')
    expect(formatBytes(-1, 'en')).toBe('')
  })
})

describe('formatRelativeTime', () => {
  const now = new Date('2026-01-01T12:00:00Z')

  it('formats a relative time with the active language', () => {
    const sixMinutesAgo = new Date(now.getTime() - 6 * 60_000)
    expect(formatRelativeTime(sixMinutesAgo, now, 'en')).toBe('6 minutes ago')
    expect(formatRelativeTime(sixMinutesAgo, now, 'ca')).toBe('fa 6 minuts')
    expect(formatRelativeTime(sixMinutesAgo, now, 'es')).toBe('hace 6 minutos')
    expect(formatRelativeTime(now, now, 'en')).toBe('now')
  })

  it('uses the largest unit that fits', () => {
    expect(formatRelativeTime(new Date(now.getTime() - 2 * 3_600_000), now, 'en')).toBe(
      '2 hours ago',
    )
    expect(formatRelativeTime(new Date(now.getTime() - 26 * 3_600_000), now, 'en')).toBe(
      'yesterday',
    )
  })
})

describe('formatNumber and formatTime', () => {
  it('formats counts and clock times with Intl', () => {
    expect(formatNumber(1234, 'en')).toBe('1,234')
    expect(formatNumber(1234, 'ca')).toBe('1.234')
    expect(formatTime(new Date(2026, 0, 1, 3, 12), 'en')).toMatch(/03:12/)
  })
})
