import { describe, expect, it } from 'vitest'

import caAbout from './ca/about.json'
import caAlerts from './ca/alerts.json'
import caCommon from './ca/common.json'
import caErrors from './ca/errors.json'
import caMetrics from './ca/metrics.json'
import caSettings from './ca/settings.json'
import enAbout from './en/about.json'
import enAlerts from './en/alerts.json'
import enCommon from './en/common.json'
import enErrors from './en/errors.json'
import enMetrics from './en/metrics.json'
import enSettings from './en/settings.json'
import esAbout from './es/about.json'
import esAlerts from './es/alerts.json'
import esCommon from './es/common.json'
import esErrors from './es/errors.json'
import esMetrics from './es/metrics.json'
import esSettings from './es/settings.json'

const languages = ['ca', 'es', 'en'] as const
const namespaces = ['common', 'about', 'alerts', 'settings', 'errors', 'metrics'] as const

type Language = (typeof languages)[number]
type Namespace = (typeof namespaces)[number]

const resources: Record<Language, Record<Namespace, unknown>> = {
  ca: {
    common: caCommon,
    about: caAbout,
    alerts: caAlerts,
    settings: caSettings,
    errors: caErrors,
    metrics: caMetrics,
  },
  es: {
    common: esCommon,
    about: esAbout,
    alerts: esAlerts,
    settings: esSettings,
    errors: esErrors,
    metrics: esMetrics,
  },
  en: {
    common: enCommon,
    about: enAbout,
    alerts: enAlerts,
    settings: enSettings,
    errors: enErrors,
    metrics: enMetrics,
  },
}

function keyPaths(value: unknown, prefix = ''): string[] {
  if (typeof value !== 'object' || value === null) {
    return [prefix]
  }
  return Object.entries(value).flatMap(([key, child]) =>
    keyPaths(child, prefix === '' ? key : `${prefix}.${key}`),
  )
}

describe('locale files', () => {
  for (const namespace of namespaces) {
    it(`has the same keys in every language for "${namespace}"`, () => {
      const reference = keyPaths(resources.ca[namespace]).sort()

      for (const language of languages) {
        expect(keyPaths(resources[language][namespace]).sort()).toEqual(reference)
      }
    })
  }
})
