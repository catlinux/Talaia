import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { createHttpClient } from '../../api/http'
import i18n from '../../i18n'
import { UpdateBanner } from './UpdateBanner'

describe('update banner', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('shows the new core version and reloads only when asked', async () => {
    const reload = vi.fn()
    render(<UpdateBanner reload={reload} />)
    expect(screen.queryByRole('status')).toBeNull()

    const version = '999.0.0'
    const client = createHttpClient(
      async () =>
        new Response(null, {
          status: 204,
          headers: { 'X-Talaia-Version': version },
        }),
    )
    await act(async () => {
      await client.get('/api/v1/version')
    })

    const banner = screen.getByRole('status')
    expect(
      within(banner).getByText(
        'Talaia was updated to version 999.0.0. Reload to use the new version.',
      ),
    ).toBeTruthy()
    expect(reload).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
    expect(reload).toHaveBeenCalledTimes(1)
  })
})
