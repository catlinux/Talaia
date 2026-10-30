export const PALETTE_RECENT_KEY = 'talaia.paletteRecent'

export type PaletteSection = 'screens' | 'servers' | 'containers' | 'stacks' | 'actions' | 'recent'

export type PaletteAction =
  { kind: 'theme' } | { kind: 'language'; language: string } | { kind: 'signout' }

export interface PaletteItem {
  id: string
  section: PaletteSection
  label: string
  detail?: string
  to?: string
  action?: PaletteAction
}

const wordBoundary = /[\s\-_/.:·>]/

/** normalizeSearch lowercases and removes the accents of a search string. */
export function normalizeSearch(value: string): string {
  return value
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .trim()
}

function scoreText(text: string, query: string): number {
  const normalized = normalizeSearch(text)
  const index = normalized.indexOf(query)
  if (index < 0) return 0
  if (index === 0) return 1000
  return wordBoundary.test(normalized[index - 1] ?? '') ? 800 : 600
}

/** scorePaletteItem scores a label first and falls back to its secondary text. */
export function scorePaletteItem(item: PaletteItem, query: string): number {
  const normalized = normalizeSearch(query)
  if (normalized === '') return 0
  const labelScore = scoreText(item.label, normalized)
  if (labelScore > 0) return labelScore * 2
  return item.detail === undefined ? 0 : scoreText(item.detail, normalized)
}

/** readPaletteRecent returns the last picked ids, newest first. */
export function readPaletteRecent(): string[] {
  try {
    const raw = localStorage.getItem(PALETTE_RECENT_KEY)
    if (raw === null) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((value): value is string => typeof value === 'string').slice(0, 5)
  } catch {
    return []
  }
}

/** rememberPalettePick stores one id at the front of the recent list. */
export function rememberPalettePick(id: string): void {
  const next = [id, ...readPaletteRecent().filter((candidate) => candidate !== id)].slice(0, 5)
  try {
    localStorage.setItem(PALETTE_RECENT_KEY, JSON.stringify(next))
  } catch {
    // Searching still works when browser storage is unavailable.
  }
}

/**
 * searchPalette filters and scores the entries. An empty query shows the
 * screens and the recent picks, with recent entries marked as such.
 */
export function searchPalette(
  items: PaletteItem[],
  query: string,
  recentIds: string[] = [],
): PaletteItem[] {
  const normalized = normalizeSearch(query)
  if (normalized === '') {
    const byId = new Map(items.map((item) => [item.id, item]))
    const screens = items.filter((item) => item.section === 'screens')
    const seen = new Set(screens.map((item) => item.id))
    const recent: PaletteItem[] = []
    for (const id of recentIds) {
      const item = byId.get(id)
      if (item === undefined || seen.has(item.id)) continue
      seen.add(item.id)
      recent.push({ ...item, section: 'recent' })
    }
    return [...screens, ...recent]
  }

  return items
    .map((item, index) => ({ item, index, score: scorePaletteItem(item, normalized) }))
    .filter(({ score }) => score > 0)
    .sort((left, right) => right.score - left.score || left.index - right.index)
    .map(({ item }) => item)
}
