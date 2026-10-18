import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { applyTheme, getInitialTheme, storeTheme, themeModes, type ThemeMode } from './theme'

export function ThemeToggle() {
  const { t } = useTranslation('common')
  const [mode, setMode] = useState<ThemeMode>(getInitialTheme)

  useEffect(() => {
    applyTheme(mode)
    storeTheme(mode)
  }, [mode])

  return (
    <label className="field">
      <span className="field__label">{t('theme.label')}</span>
      <select
        className="field__input"
        value={mode}
        onChange={(event) => setMode(event.target.value as ThemeMode)}
      >
        {themeModes.map((themeMode) => (
          <option key={themeMode} value={themeMode}>
            {t(`theme.${themeMode}`)}
          </option>
        ))}
      </select>
    </label>
  )
}
