#!/usr/bin/env node
// Checks that every translation key exists in ca, es and en for the web panel
// and the Go components (core and cli).
//
// Usage: node scripts/i18n-check.mjs [rootDir]
// The default rootDir is the repository root (the parent of scripts/).
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import process from 'node:process'

const languages = ['ca', 'es', 'en']
const scriptDir = dirname(fileURLToPath(import.meta.url))
const rootDir = resolve(process.argv[2] ?? join(scriptDir, '..'))

const problems = []
let keysChecked = 0

function report(problem) {
  problems.push(problem)
}

// flattenKeys returns the dot paths of every leaf of a nested object. Array
// values and i18next plural suffixes (total_one) are kept as they are.
function flattenKeys(value, prefix = '') {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return prefix === '' ? [] : [prefix]
  }
  return Object.entries(value).flatMap(([key, child]) =>
    flattenKeys(child, prefix === '' ? key : `${prefix}.${key}`),
  )
}

function readKeys(path, language, unit) {
  let parsed
  try {
    parsed = JSON.parse(readFileSync(path, 'utf8'))
  } catch (error) {
    report(`web ${unit}: invalid JSON in ${language} (${error.message})`)
    return null
  }
  return new Set(flattenKeys(parsed))
}

function checkLanguages(keysByLanguage, labelFor) {
  const reference = new Set()
  for (const keys of keysByLanguage.values()) {
    for (const key of keys) {
      reference.add(key)
    }
  }
  keysChecked += reference.size

  for (const language of languages) {
    const keys = keysByLanguage.get(language)
    if (!keys) continue
    for (const key of [...reference].sort()) {
      if (!keys.has(key)) {
        report(`${labelFor(language)}: key "${key}" missing in ${language}`)
      }
    }
  }
}

function checkWebPanel() {
  const localesDir = join(rootDir, 'web/src/locales')
  if (!existsSync(localesDir)) {
    console.log('note: web/src/locales not found, skipping the web panel')
    return
  }

  const namespaces = new Set()
  for (const language of languages) {
    const dir = join(localesDir, language)
    if (!existsSync(dir)) continue
    for (const file of readdirSync(dir)) {
      if (file.endsWith('.json')) namespaces.add(file)
    }
  }

  for (const file of [...namespaces].sort()) {
    const keysByLanguage = new Map()
    for (const language of languages) {
      const path = join(localesDir, language, file)
      if (!existsSync(path)) {
        report(`web ${file}: file missing in ${language}`)
        continue
      }
      const keys = readKeys(path, language, file)
      if (keys) keysByLanguage.set(language, keys)
    }
    checkLanguages(keysByLanguage, () => `web ${file}`)
  }
}

// parseMessageIDs reads the message ids of a go-i18n TOML file: the table
// headers [id] and the top-level id = ... lines. Comments are ignored.
function parseMessageIDs(path) {
  const ids = new Set()
  let inTable = false

  for (const rawLine of readFileSync(path, 'utf8').split(/\r?\n/)) {
    const line = rawLine.trim()
    if (line === '' || line.startsWith('#')) continue

    const table = line.match(/^\[([^\]]+)\]/)
    if (table) {
      ids.add(table[1].trim())
      inTable = true
      continue
    }

    if (inTable) continue

    const assignment = line.match(/^([A-Za-z0-9_.-]+)\s*=/)
    if (assignment) ids.add(assignment[1])
  }

  return ids
}

function checkGoComponent(component, relativeDir) {
  const localesDir = join(rootDir, relativeDir)
  if (!existsSync(localesDir)) {
    console.log(`note: ${relativeDir} not found, skipping ${component}`)
    return
  }

  const keysByLanguage = new Map()
  for (const language of languages) {
    const file = `${language}.toml`
    const path = join(localesDir, file)
    if (!existsSync(path)) {
      report(`${component} ${file}: file missing in ${language}`)
      continue
    }
    keysByLanguage.set(language, parseMessageIDs(path))
  }
  checkLanguages(keysByLanguage, (language) => `${component} ${language}.toml`)
}

checkWebPanel()
checkGoComponent('core', 'core/i18n/locales')
checkGoComponent('cli', 'cli/i18n/locales')

if (problems.length > 0) {
  for (const problem of problems) {
    console.log(problem)
  }
  process.exitCode = 1
} else {
  console.log(`i18n-check: OK (${keysChecked} keys checked)`)
}
