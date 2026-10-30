import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { PageHeader } from './PageHeader'

afterEach(cleanup)

describe('PageHeader', () => {
  it('renders the title, subtitle and actions', () => {
    render(
      <PageHeader
        title="Servers"
        subtitle="6 servers in 2 groups"
        actions={<button type="button">Add server</button>}
      />,
    )

    expect(screen.getByRole('heading', { level: 1, name: 'Servers' })).toBeTruthy()
    expect(screen.getByText('6 servers in 2 groups')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Add server' })).toBeTruthy()
  })
})
