import { describe, expect, it } from 'vitest'

import i18n from '../i18n'
import { catalog, imageRepository, lookupImage, lookupService } from './catalog'

describe('lookupService', () => {
  it('normalizes the service suffix, case and template instances', () => {
    expect(lookupService('ssh.service')?.id).toBe('ssh')
    expect(lookupService('SSHD.SERVICE')?.id).toBe('ssh')
    expect(lookupService('getty@tty1.service')?.id).toBe('getty')
    expect(lookupService('getty@.service')?.id).toBe('getty')
    expect(lookupService('postgresql@16-main.service')?.id).toBe('postgresql')
    expect(lookupService('php8.2-fpm.service')?.id).toBe('php-fpm')
    expect(lookupService('wg-quick@wg0.service')?.id).toBe('wireguard')
    expect(lookupService('apt-daily.timer')?.id).toBe('apt-daily')
    expect(lookupService('certbot.service')?.id).toBe('certbot')
    expect(lookupService('certbot.timer')?.id).toBe('certbot')
  })

  it('prefers an exact unit over a pattern', () => {
    expect(lookupService('mysql.service')?.id).toBe('mariadb')
    expect(lookupService('docker.service')?.id).toBe('docker')
  })

  it('returns undefined for an unknown unit', () => {
    expect(lookupService('not-a-real-service.service')).toBeUndefined()
  })
})

describe('lookupImage', () => {
  it('ignores registries, library, tags and digests', () => {
    expect(lookupImage('docker.io/library/postgres:16-alpine')?.id).toBe('postgresql')
    expect(lookupImage('POSTGRES@sha256:abcd')?.id).toBe('postgresql')
    expect(lookupImage('registry.example.com:5000/library/redis:7')?.id).toBe('redis')
    expect(lookupImage('quay.io/library/mariadb')?.id).toBe('mariadb')
    expect(lookupImage('gcr.io/cadvisor:0.49.1')?.id).toBe('cadvisor')
    expect(lookupImage('gcr.io/cadvisor/cadvisor:0.49.1')?.id).toBe('cadvisor')
    expect(lookupImage('portainer/portainer-ce:latest')?.id).toBe('portainer')
  })

  it('returns undefined for unknown images', () => {
    expect(lookupImage('example.com/team/unknown:1')).toBeUndefined()
  })
})

describe('imageRepository', () => {
  it('keeps a repository without registry, tag or digest', () => {
    expect(imageRepository('docker.io/library/postgres:16')).toBe('postgres')
    expect(imageRepository('localhost:5000/team/app@sha256:abc')).toBe('team/app')
    expect(imageRepository('NGINX:latest')).toBe('nginx')
  })
})

describe('catalog translations', () => {
  it('has name, what and ifFails for every entry in ca, es and en', () => {
    for (const language of ['ca', 'es', 'en']) {
      const t = i18n.getFixedT(language, 'catalog')
      for (const entry of catalog) {
        for (const field of ['name', 'what', 'ifFails']) {
          const key = `entries.${entry.id}.${field}`
          expect(t(key), `${language} ${key}`).not.toBe(key)
        }
      }
      for (const category of new Set(catalog.map((entry) => entry.category))) {
        expect(t(`category.${category}`), `${language} category ${category}`).not.toBe(
          `category.${category}`,
        )
        expect(t(`categoryHelp.${category}`), `${language} category help ${category}`).not.toBe(
          `categoryHelp.${category}`,
        )
      }
      for (const importance of ['essential', 'important', 'optional', 'unknown']) {
        expect(t(`importance.${importance}`), `${language} importance ${importance}`).not.toBe(
          `importance.${importance}`,
        )
        expect(
          t(`importanceHelp.${importance}`),
          `${language} importance help ${importance}`,
        ).not.toBe(`importanceHelp.${importance}`)
      }
    }
  })

  it('has unique ids and non-overlapping exact units and images', () => {
    expect(new Set(catalog.map((entry) => entry.id)).size).toBe(catalog.length)
    const units = catalog.flatMap((entry) => entry.units ?? [])
    expect(new Set(units).size).toBe(units.length)
    const images = catalog.flatMap((entry) => entry.images ?? [])
    expect(new Set(images).size).toBe(images.length)
  })

  it('uses https documentation links and the Certbot diagnostics', () => {
    for (const entry of catalog) {
      if (entry.docs !== undefined) expect(entry.docs.startsWith('https://')).toBe(true)
    }
    const certbot = catalog.find((entry) => entry.id === 'certbot')
    expect(certbot?.kind).toBe('both')
    expect(certbot?.units).toEqual(['certbot'])
    expect(certbot?.checks).toEqual(['certbot certificates', 'certbot renew --dry-run'])
  })
})
