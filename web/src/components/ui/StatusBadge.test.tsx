import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import { SeverityTag, StatusBadge, type Status } from './StatusBadge'

beforeEach(async () => {
  await i18n.changeLanguage('en')
})
afterEach(cleanup)

describe('StatusBadge', () => {
  it('renders a visible text label for every status', () => {
    const labels: Record<Status, string> = {
      ok: 'Correct',
      warn: 'Warning',
      crit: 'Critical',
      offline: 'Offline',
      unknown: 'Unknown',
    }
    for (const [status, label] of Object.entries(labels) as [Status, string][]) {
      const { unmount } = render(<StatusBadge status={status} />)
      expect(screen.getByText(label)).toBeTruthy()
      expect(
        screen.getByText(label).closest('.ui-status')?.classList.contains(`ui-status--${status}`),
      ).toBe(true)
      unmount()
    }
  })

  it('accepts a translated label from its caller', () => {
    render(<StatusBadge status="ok" label="All good" />)
    expect(screen.getByText('All good')).toBeTruthy()
  })

  it('renders the severity tag text and tone', () => {
    render(
      <>
        <SeverityTag severity="critical" />
        <SeverityTag severity="warning" />
        <SeverityTag severity="info" />
      </>,
    )
    expect(screen.getByText('Critical').closest('.ui-severity--critical')).toBeTruthy()
    expect(screen.getByText('Warning').closest('.ui-severity--warning')).toBeTruthy()
    expect(screen.getByText('Info').closest('.ui-severity--info')).toBeTruthy()
  })
})
