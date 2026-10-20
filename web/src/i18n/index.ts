import i18n from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import caAbout from '../locales/ca/about.json'
import caAuth from '../locales/ca/auth.json'
import caCommon from '../locales/ca/common.json'
import caErrors from '../locales/ca/errors.json'
import caFleet from '../locales/ca/fleet.json'
import caServer from '../locales/ca/server.json'
import caSettings from '../locales/ca/settings.json'
import enAbout from '../locales/en/about.json'
import enAuth from '../locales/en/auth.json'
import enCommon from '../locales/en/common.json'
import enErrors from '../locales/en/errors.json'
import enFleet from '../locales/en/fleet.json'
import enServer from '../locales/en/server.json'
import enSettings from '../locales/en/settings.json'
import esAbout from '../locales/es/about.json'
import esAuth from '../locales/es/auth.json'
import esCommon from '../locales/es/common.json'
import esErrors from '../locales/es/errors.json'
import esFleet from '../locales/es/fleet.json'
import esServer from '../locales/es/server.json'
import esSettings from '../locales/es/settings.json'

export const supportedLanguages = ['ca', 'es', 'en'] as const

export type SupportedLanguage = (typeof supportedLanguages)[number]

/** Browser storage key for the language chosen in the panel. */
export const LANGUAGE_STORAGE_KEY = 'talaia.lang'

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      ca: {
        common: caCommon,
        about: caAbout,
        fleet: caFleet,
        server: caServer,
        settings: caSettings,
        auth: caAuth,
        errors: caErrors,
      },
      es: {
        common: esCommon,
        about: esAbout,
        fleet: esFleet,
        server: esServer,
        settings: esSettings,
        auth: esAuth,
        errors: esErrors,
      },
      en: {
        common: enCommon,
        about: enAbout,
        fleet: enFleet,
        server: enServer,
        settings: enSettings,
        auth: enAuth,
        errors: enErrors,
      },
    },
    supportedLngs: [...supportedLanguages],
    fallbackLng: 'en',
    ns: ['common', 'about', 'fleet', 'server', 'settings', 'auth', 'errors'],
    defaultNS: 'common',
    load: 'languageOnly',
    initAsync: false,
    interpolation: { escapeValue: false },
    // Order: language saved in the panel, then the browser language, then the
    // English fallback (configured above).
    detection: {
      order: ['localStorage', 'navigator'],
      lookupLocalStorage: LANGUAGE_STORAGE_KEY,
      caches: ['localStorage'],
    },
  })

function syncDocumentLanguage(language: string): void {
  document.documentElement.lang = language
}

i18n.on('languageChanged', syncDocumentLanguage)
syncDocumentLanguage(i18n.resolvedLanguage ?? i18n.language)

export default i18n
