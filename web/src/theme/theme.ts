export const themeModes = ['light', 'dark', 'system'] as const

export type ThemeMode = (typeof themeModes)[number]

export const THEME_STORAGE_KEY = 'talaia.theme'

function isThemeMode(value: string | null): value is ThemeMode {
  return value !== null && (themeModes as readonly string[]).includes(value)
}

/**
 * Reads the saved theme. Storage can be unavailable (private mode, disabled
 * cookies), in which case the default is used.
 */
export function getStoredTheme(): ThemeMode | null {
  try {
    const value = localStorage.getItem(THEME_STORAGE_KEY)
    return isThemeMode(value) ? value : null
  } catch {
    return null
  }
}

export function storeTheme(mode: ThemeMode): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, mode)
  } catch {
    // Ignore: the theme is still applied for the current session.
  }
}

export function getInitialTheme(): ThemeMode {
  return getStoredTheme() ?? 'system'
}

/**
 * Applies the theme by setting or removing the data-theme attribute on the
 * root element. "system" removes it so the prefers-color-scheme media query
 * takes over.
 */
export function applyTheme(mode: ThemeMode): void {
  const root = document.documentElement
  if (mode === 'system') {
    root.removeAttribute('data-theme')
    return
  }
  root.setAttribute('data-theme', mode)
}
