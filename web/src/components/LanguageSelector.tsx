import { useTranslation } from 'react-i18next'

import { supportedLanguages, type SupportedLanguage } from '../i18n'

function currentLanguage(resolved: string | undefined, fallback: string): SupportedLanguage {
  const language = resolved ?? fallback
  const match = supportedLanguages.find((candidate) => language.startsWith(candidate))
  return match ?? 'en'
}

export function LanguageSelector() {
  const { t, i18n } = useTranslation('common')
  const value = currentLanguage(i18n.resolvedLanguage, i18n.language)

  return (
    <label className="field">
      <span className="field__label">{t('language.label')}</span>
      <select
        className="field__input"
        value={value}
        onChange={(event) => {
          void i18n.changeLanguage(event.target.value)
        }}
      >
        {supportedLanguages.map((language) => (
          <option key={language} value={language}>
            {t(`language.${language}`)}
          </option>
        ))}
      </select>
    </label>
  )
}
