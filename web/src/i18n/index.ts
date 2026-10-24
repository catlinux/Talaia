import i18n from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import caAbout from '../locales/ca/about.json'
import caAudit from '../locales/ca/audit.json'
import caAuth from '../locales/ca/auth.json'
import caCommon from '../locales/ca/common.json'
import caErrors from '../locales/ca/errors.json'
import caFleet from '../locales/ca/fleet.json'
import caMetrics from '../locales/ca/metrics.json'
import caServer from '../locales/ca/server.json'
import caAlerts from '../locales/ca/alerts.json'
import caSettings from '../locales/ca/settings.json'
import caUsers from '../locales/ca/users.json'
import caValidation from '../locales/ca/validation.json'
import enAbout from '../locales/en/about.json'
import enAudit from '../locales/en/audit.json'
import enAuth from '../locales/en/auth.json'
import enCommon from '../locales/en/common.json'
import enErrors from '../locales/en/errors.json'
import enFleet from '../locales/en/fleet.json'
import enMetrics from '../locales/en/metrics.json'
import enServer from '../locales/en/server.json'
import enAlerts from '../locales/en/alerts.json'
import enSettings from '../locales/en/settings.json'
import enUsers from '../locales/en/users.json'
import enValidation from '../locales/en/validation.json'
import esAbout from '../locales/es/about.json'
import esAudit from '../locales/es/audit.json'
import esAuth from '../locales/es/auth.json'
import esCommon from '../locales/es/common.json'
import esErrors from '../locales/es/errors.json'
import esFleet from '../locales/es/fleet.json'
import esMetrics from '../locales/es/metrics.json'
import esServer from '../locales/es/server.json'
import esAlerts from '../locales/es/alerts.json'
import esSettings from '../locales/es/settings.json'
import esUsers from '../locales/es/users.json'
import esValidation from '../locales/es/validation.json'

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
        audit: caAudit,
        fleet: caFleet,
        metrics: caMetrics,
        server: caServer,
        settings: caSettings,
        alerts: caAlerts,
        users: caUsers,
        auth: caAuth,
        errors: caErrors,
        validation: caValidation,
      },
      es: {
        common: esCommon,
        about: esAbout,
        audit: esAudit,
        fleet: esFleet,
        metrics: esMetrics,
        server: esServer,
        settings: esSettings,
        alerts: esAlerts,
        users: esUsers,
        auth: esAuth,
        errors: esErrors,
        validation: esValidation,
      },
      en: {
        common: enCommon,
        about: enAbout,
        audit: enAudit,
        fleet: enFleet,
        metrics: enMetrics,
        server: enServer,
        settings: enSettings,
        alerts: enAlerts,
        users: enUsers,
        auth: enAuth,
        errors: enErrors,
        validation: enValidation,
      },
    },
    supportedLngs: [...supportedLanguages],
    fallbackLng: 'en',
    ns: [
      'common',
      'about',
      'audit',
      'fleet',
      'metrics',
      'server',
      'settings',
      'users',
      'auth',
      'errors',
      'validation',
    ],
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
