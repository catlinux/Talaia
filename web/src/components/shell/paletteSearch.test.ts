import { beforeEach, describe, expect, it } from 'vitest'

import {
  PALETTE_RECENT_KEY,
  normalizeSearch,
  readPaletteRecent,
  rememberPalettePick,
  searchPalette,
  type PaletteItem,
} from './paletteSearch'

function item(overrides: Partial<PaletteItem> = {}): PaletteItem {
  return {
    id: 'item',
    section: 'servers',
    label: 'server',
    ...overrides,
  }
}

beforeEach(() => {
  localStorage.clear()
})

describe('paletteSearch', () => {
  it('normalises case and accents', () => {
    expect(normalizeSearch('  Aplicación  ')).toBe('aplicacion')
    expect(searchPalette([item({ label: 'Aplicación' })], 'aplicacion')).toHaveLength(1)
  })

  it('ranks a prefix above a word start and a substring', () => {
    const items = [
      item({ id: 'substring', label: 'myweb' }),
      item({ id: 'word', label: 'vps-web' }),
      item({ id: 'prefix', label: 'web' }),
    ]
    expect(searchPalette(items, 'web').map((entry) => entry.id)).toEqual([
      'prefix',
      'word',
      'substring',
    ])
  })

  it('matches the secondary text and keeps ties in their original order', () => {
    const items = [
      item({ id: 'detail', label: 'Database', detail: 'vps-web-01' }),
      item({ id: 'first', label: 'Web API' }),
      item({ id: 'second', label: 'Web worker' }),
    ]
    expect(searchPalette(items, 'web').map((entry) => entry.id)).toEqual([
      'first',
      'second',
      'detail',
    ])
  })

  it('shows screens and recent picks for an empty query', () => {
    const items = [
      item({ id: 'screen-overview', section: 'screens', label: 'Overview' }),
      item({ id: 'screen-settings', section: 'screens', label: 'Settings' }),
      item({ id: 'server:s1', label: 'vps-web-01' }),
    ]
    const results = searchPalette(items, '', ['server:s1', 'screen-settings'])
    expect(results.map((entry) => [entry.id, entry.section])).toEqual([
      ['screen-overview', 'screens'],
      ['screen-settings', 'screens'],
      ['server:s1', 'recent'],
    ])
  })

  it('keeps five distinct recent picks and tolerates broken storage', () => {
    for (const id of ['a', 'b', 'c', 'd', 'e', 'f', 'a']) rememberPalettePick(id)
    expect(readPaletteRecent()).toEqual(['a', 'f', 'e', 'd', 'c'])

    localStorage.setItem(PALETTE_RECENT_KEY, '{not json')
    expect(readPaletteRecent()).toEqual([])
  })
})
