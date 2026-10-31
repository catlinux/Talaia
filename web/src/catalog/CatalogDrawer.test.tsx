import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { ServiceUnit } from '../api/services'
import i18n from '../i18n'
import { CatalogDrawerProvider } from './CatalogDrawer'
import { WhatIsThis } from './WhatIsThis'

function service(overrides: Partial<ServiceUnit> = {}): ServiceUnit {
  return {
    name: 'ssh.service',
    description: 'OpenBSD Secure Shell server',
    loadState: 'loaded',
    activeState: 'active',
    subState: 'running',
    unitFileState: 'enabled',
    canReload: true,
    ...overrides,
  }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

afterEach(() => {
  cleanup()
  Reflect.deleteProperty(navigator, 'clipboard')
})

describe('CatalogDrawer', () => {
  it('opens the detailed card from the compact tooltip and copies a command', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })

    render(
      <CatalogDrawerProvider>
        <WhatIsThis target={{ kind: 'service', unit: service() }} />
      </CatalogDrawerProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'View full card' }))
    const dialog = await screen.findByRole('dialog')

    expect(within(dialog).getByRole('heading', { name: 'What it is' })).toBeTruthy()
    expect(within(dialog).getByText(/It is the door you use to administer/)).toBeTruthy()
    expect(within(dialog).getByRole('heading', { name: 'If you stop it' })).toBeTruthy()
    expect(within(dialog).getByText(/Stopping or disabling SSH/)).toBeTruthy()
    expect(within(dialog).getByRole('heading', { name: 'If it fails' })).toBeTruthy()
    expect(within(dialog).getByRole('heading', { name: 'Common problems' })).toBeTruthy()
    expect(within(dialog).getByText(/The password or key is rejected/)).toBeTruthy()
    expect(within(dialog).getByRole('heading', { name: 'How to check it' })).toBeTruthy()
    expect(within(dialog).getByText('systemctl status ssh.service')).toBeTruthy()

    fireEvent.click(within(dialog).getAllByRole('button', { name: 'Copy' })[0]!)
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('systemctl status ssh.service'))
    expect(within(dialog).getByRole('button', { name: 'Copied' })).toBeTruthy()

    const docs = within(dialog).getByRole('link', {
      name: 'https://www.openssh.com/manual.html',
    })
    expect(docs.getAttribute('target')).toBe('_blank')
    expect(docs.getAttribute('rel')).toBe('noopener noreferrer')
  })

  it('shows the commands and origin of an unknown service', async () => {
    render(
      <CatalogDrawerProvider>
        <WhatIsThis
          target={{
            kind: 'service',
            unit: service({ name: 'custom.service', description: 'Company daemon' }),
          }}
        />
      </CatalogDrawerProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'View full card' }))
    const dialog = await screen.findByRole('dialog')

    expect(within(dialog).getByText('systemd says: Company daemon')).toBeTruthy()
    expect(within(dialog).getByText('systemctl status custom.service')).toBeTruthy()
    expect(within(dialog).getByText('journalctl -u custom.service -n 50 --no-pager')).toBeTruthy()
    expect(within(dialog).queryByRole('heading', { name: 'Official documentation' })).toBeNull()
  })
})
