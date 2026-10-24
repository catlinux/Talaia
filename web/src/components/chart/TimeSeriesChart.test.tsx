import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '../../i18n'
import { TimeSeriesChart } from './TimeSeriesChart'

afterEach(cleanup)
beforeEach(async () => {
  await i18n.changeLanguage('en')
})
describe('TimeSeriesChart', () => {
  it('draws paths and separates null gaps', () => {
    const { container } = render(
      <TimeSeriesChart
        label="CPU"
        format="percent"
        series={[
          {
            id: 'cpu',
            label: 'CPU',
            color: 'accent',
            points: [
              { t: 1, v: 2 },
              { t: 2, v: null },
              { t: 3, v: 4 },
            ],
          },
        ]}
      />,
    )
    expect(container.querySelectorAll('path[fill="none"]')).toHaveLength(2)
    expect(screen.getByRole('img', { name: /CPU/ })).toBeTruthy()
    expect(container.querySelector('.timeseries__summary')).toBeTruthy()
    expect(container.querySelector('svg')?.getAttribute('viewBox')).toContain('0 0 640')
    expect(container.querySelectorAll('.chart-axis')).toHaveLength(7)
  })
  it('shows the empty state and keyboard tooltip', () => {
    const { rerender } = render(<TimeSeriesChart label="CPU" format="percent" series={[]} />)
    expect(screen.getByText('No data yet')).toBeTruthy()
    rerender(
      <TimeSeriesChart
        label="CPU"
        format="percent"
        series={[
          {
            id: 'cpu',
            label: 'CPU',
            color: 'accent',
            points: [
              { t: 1, v: 5 },
              { t: 2, v: 8 },
            ],
          },
        ]}
      />,
    )
    const target = document.querySelector('svg rect[tabindex="0"]')
    if (!target) throw new Error('keyboard target missing')
    fireEvent.focus(target)
    expect(screen.getByRole('status').textContent).toContain('CPU')
  })
})
