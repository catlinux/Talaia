import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'

const scriptPath = join(dirname(fileURLToPath(import.meta.url)), 'i18n-check.mjs')

function withFixture(files, assertions) {
  const root = mkdtempSync(join(tmpdir(), 'talaia-i18n-check-'))
  try {
    for (const [relative, content] of Object.entries(files)) {
      const path = join(root, relative)
      mkdirSync(dirname(path), { recursive: true })
      writeFileSync(path, content)
    }

    const result = spawnSync(process.execPath, [scriptPath, root], { encoding: 'utf8' })
    assertions({ result, output: `${result.stdout}${result.stderr}` })
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
}

function json(value) {
  return `${JSON.stringify(value, null, 2)}\n`
}

test('exits 0 when every language has the same keys', () => {
  withFixture(
    {
      'web/src/locales/ca/common.json': json({ appName: 'Talaia', theme: { light: 'Clar' } }),
      'web/src/locales/es/common.json': json({ appName: 'Talaia', theme: { light: 'Claro' } }),
      'web/src/locales/en/common.json': json({ appName: 'Talaia', theme: { light: 'Light' } }),
      'core/i18n/locales/ca.toml': '[app.name]\nother = "Talaia"\n',
      'core/i18n/locales/es.toml': '[app.name]\nother = "Talaia"\n',
      'core/i18n/locales/en.toml': '[app.name]\nother = "Talaia"\n',
      'cli/i18n/locales/ca.toml': '[version.line]\nother = "talaiactl {{.Version}}"\n',
      'cli/i18n/locales/es.toml': '[version.line]\nother = "talaiactl {{.Version}}"\n',
      'cli/i18n/locales/en.toml': '[version.line]\nother = "talaiactl {{.Version}}"\n',
    },
    ({ result }) => {
      assert.equal(result.status, 0)
      assert.match(result.stdout, /i18n-check: OK \(\d+ keys checked\)/)
    },
  )
})

test('fails when a key is missing in a language', () => {
  withFixture(
    {
      'web/src/locales/ca/common.json': json({ appName: 'Talaia', onlyCa: 'només ca' }),
      'web/src/locales/es/common.json': json({ appName: 'Talaia' }),
      'web/src/locales/en/common.json': json({ appName: 'Talaia', onlyCa: 'only ca' }),
    },
    ({ result, output }) => {
      assert.equal(result.status, 1)
      assert.match(output, /web common\.json: key "onlyCa" missing in es/)
    },
  )
})

test('fails when a file is missing in a language', () => {
  withFixture(
    {
      'web/src/locales/ca/common.json': json({ appName: 'Talaia' }),
      'web/src/locales/en/common.json': json({ appName: 'Talaia' }),
    },
    ({ result, output }) => {
      assert.equal(result.status, 1)
      assert.match(output, /web common\.json: file missing in es/)
    },
  )
})

test('flattens nested keys and keeps i18next plural suffixes', () => {
  withFixture(
    {
      'web/src/locales/ca/common.json': json({
        fleet: { server: { status: { online: 'En línia' } } },
        updates_one: '1 actualització',
        updates_other: '{{count}} actualitzacions',
      }),
      'web/src/locales/es/common.json': json({
        fleet: { server: { status: { online: 'En línea' } } },
        updates_one: '1 actualización',
      }),
      'web/src/locales/en/common.json': json({
        fleet: { server: { status: { online: 'Online' } } },
        updates_one: '1 update',
        updates_other: '{{count}} updates',
      }),
    },
    ({ result, output }) => {
      assert.equal(result.status, 1)
      assert.match(output, /web common\.json: key "updates_other" missing in es/)
    },
  )
})

test('passes with nested keys and plural suffixes in every language', () => {
  withFixture(
    {
      'web/src/locales/ca/common.json': json({ fleet: { server: { online: 'En línia' } }, updates_other: 'moltes' }),
      'web/src/locales/es/common.json': json({ fleet: { server: { online: 'En línea' } }, updates_other: 'muchas' }),
      'web/src/locales/en/common.json': json({ fleet: { server: { online: 'Online' } }, updates_other: 'many' }),
    },
    ({ result }) => {
      assert.equal(result.status, 0)
    },
  )
})

test('fails when a Go message id is missing', () => {
  withFixture(
    {
      'cli/i18n/locales/ca.toml':
        '[app.name]\nother = "talaiactl"\n\n[version.line]\nother = "talaiactl {{.Version}}"\n',
      'cli/i18n/locales/es.toml': '[app.name]\nother = "talaiactl"\n',
      'cli/i18n/locales/en.toml':
        '[app.name]\nother = "talaiactl"\n\n[version.line]\nother = "talaiactl {{.Version}}"\n',
    },
    ({ result, output }) => {
      assert.equal(result.status, 1)
      assert.match(output, /cli es\.toml: key "version\.line" missing in es/)
    },
  )
})

test('fails when a Go locale file is missing', () => {
  withFixture(
    {
      'core/i18n/locales/ca.toml': '[app.name]\nother = "Talaia"\n',
      'core/i18n/locales/en.toml': '[app.name]\nother = "Talaia"\n',
    },
    ({ result, output }) => {
      assert.equal(result.status, 1)
      assert.match(output, /core es\.toml: file missing in es/)
    },
  )
})

test('skips components whose locale directory does not exist', () => {
  withFixture(
    {
      'web/src/locales/ca/common.json': json({ appName: 'Talaia' }),
      'web/src/locales/es/common.json': json({ appName: 'Talaia' }),
      'web/src/locales/en/common.json': json({ appName: 'Talaia' }),
    },
    ({ result }) => {
      assert.equal(result.status, 0)
      assert.match(result.stdout, /note: core\/i18n\/locales not found, skipping core/)
      assert.match(result.stdout, /note: cli\/i18n\/locales not found, skipping cli/)
    },
  )
})
