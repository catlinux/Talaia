import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { catalog } from '../catalog/catalog'
import { CatalogDrawerProvider } from '../catalog/CatalogDrawer'
import i18n from '../i18n'
import { RouterProvider } from '../router'
import { Catalog } from './Catalog'

function renderCatalog() {
  return render(
    <RouterProvider>
      <CatalogDrawerProvider>
        <Catalog />
      </CatalogDrawerProvider>
    </RouterProvider>,
  )
}

function query(name: string): string | null {
  return new URLSearchParams(window.location.search).get(name)
}

beforeEach(async () => {
  window.history.replaceState({}, '', '/catalog')
  await i18n.changeLanguage('en')
})

afterEach(cleanup)

describe('Catalog', () => {
  it('lists every catalogue entry', () => {
    renderCatalog()

    expect(screen.getByRole('heading', { level: 1, name: 'Catalogue' })).toBeTruthy()
    expect(screen.getByText(`${catalog.length} entries`)).toBeTruthy()
    for (const entry of catalog) {
      expect(screen.getByText(i18n.t(`entries.${entry.id}.name`, { ns: 'catalog' }))).toBeTruthy()
    }
  })

  it('searches by text and keeps the query in the address', () => {
    renderCatalog()

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search' }), {
      target: { value: 'certbot' },
    })

    expect(screen.getByText('Certbot')).toBeTruthy()
    expect(screen.getByText('1 entry')).toBeTruthy()
    expect(query('q')).toBe('certbot')
    expect(screen.queryByText('OpenSSH server')).toBeNull()
  })

  it('filters by type and category and writes both to the address', async () => {
    renderCatalog()

    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'image' } })
    expect(query('type')).toBe('image')
    await waitFor(() => expect(screen.queryByText('OpenSSH server')).toBeNull())
    expect(screen.getByText('PostgreSQL')).toBeTruthy()

    fireEvent.change(screen.getByLabelText('Category'), {
      target: { value: 'database' },
    })
    expect(query('category')).toBe('database')
    expect(screen.getByText('PostgreSQL')).toBeTruthy()
    expect(screen.queryByText('Nginx')).toBeNull()
  })

  it('opens the detailed drawer from a row', async () => {
    renderCatalog()

    fireEvent.click(screen.getByText('Certbot'))
    const dialog = await screen.findByRole('dialog')

    expect(within(dialog).getByText('Certbot')).toBeTruthy()
    expect(within(dialog).getByRole('heading', { name: 'What it is' })).toBeTruthy()
  })
})
