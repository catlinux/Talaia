import { describe, expect, it } from 'vitest'

import caAbout from './ca/about.json'
import caCommon from './ca/common.json'
import enAbout from './en/about.json'
import enCommon from './en/common.json'
import esAbout from './es/about.json'
import esCommon from './es/common.json'

const languages = ['ca', 'es', 'en'] as const
const namespaces = ['common', 'about'] as const

type Language = (typeof languages)[number]
type Namespace = (typeof namespaces)[number]

const resources: Record<Language, Record<Namespace, unknown>> = {
  ca: { common: caCommon, about: caAbout },
  es: { common: esCommon, about: esAbout },
  en: { common: enCommon, about: enAbout },
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
