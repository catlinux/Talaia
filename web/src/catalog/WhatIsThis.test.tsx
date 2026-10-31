import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import type { Container } from '../api/containers'
import type { ServiceUnit } from '../api/services'
import i18n from '../i18n'
import { CatalogDrawerProvider } from './CatalogDrawer'
import {
  describeContainer,
  describeEntry,
  describeService,
  describeStack,
  WhatIsThis,
} from './WhatIsThis'

function service(overrides: Partial<ServiceUnit> = {}): ServiceUnit {
  return {
    name: 'ssh.service',
    description: 'OpenBSD Secure Shell server',
    loadState: 'loaded',
    activeState: 'active',
    subState: 'running',
    unitFileState: 'enabled',
    canReload: true,
    ...overrides,
  }
}

function container(overrides: Partial<Container> = {}): Container {
  return {
    id: 'c1',
    name: 'web',
    image: 'docker.io/library/postgres:16-alpine',
    state: 'running',
    status: 'Up',
    composeProject: 'shop',
    composeService: 'db',
    ...overrides,
  }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

afterEach(cleanup)

describe('describeService', () => {
  it('uses the catalogue entry for a known unit', () => {
    const description = describeService(service())

    expect(description.known).toBe(true)
    expect(description.name).toBe('OpenSSH server')
    expect(description.category).toBe('access')
    expect(description.importance).toBe('essential')
    expect(description.what).toContain('secure remote shell')
    expect(description.details).toContain('door you use to administer')
    expect(description.stop).toContain('remote door to the server')
    expect(description.ifFails).toContain('lose remote access')
    expect(description.problems).toHaveLength(3)
    expect(description.commands).toEqual([
      'systemctl status ssh.service',
      'journalctl -u ssh.service -n 50 --no-pager',
    ])
    expect(description.docs).toBe('https://www.openssh.com/manual.html')
    expect(description.origin).toBe('systemd unit ssh.service')
  })

  it('adds the entry checks and details for Certbot', () => {
    const description = describeService(service({ name: 'certbot.timer' }))

    expect(description.known).toBe(true)
    expect(description.name).toBe('Certbot')
    expect(description.commands).toEqual([
      'systemctl status certbot.timer',
      'journalctl -u certbot.timer -n 50 --no-pager',
      'certbot certificates',
      'certbot renew --dry-run',
    ])
    expect(description.problems?.[0]).toContain('domain no longer points')
    expect(description.docs).toBe('https://eff-certbot.readthedocs.io/')
  })

  it('shows the systemd description for an unknown unit', () => {
    const description = describeService(
      service({ name: 'custom.service', description: 'Custom company daemon' }),
    )

    expect(description.known).toBe(false)
    expect(description.importance).toBe('unknown')
    expect(description.what).toBe('systemd says: Custom company daemon')
  })
})

describe('describeEntry', () => {
  it('uses the first unit and appends the entry checks', () => {
    const description = describeEntry('certbot')

    expect(description.known).toBe(true)
    expect(description.name).toBe('Certbot')
    expect(description.commands).toEqual([
      'systemctl status certbot.service',
      'journalctl -u certbot.service -n 50 --no-pager',
      'certbot certificates',
      'certbot renew --dry-run',
    ])
    expect(description.origin).toBe(
      'systemd units: certbot.service · container images: certbot/certbot',
    )
    expect(description.docs).toBe('https://eff-certbot.readthedocs.io/')
  })

  it('has no service command for an entry recognised only by a pattern', () => {
    const description = describeEntry('php-fpm')

    expect(description.commands).toEqual([])
    expect(description.origin).toContain('systemd name pattern')
  })
})

describe('describeContainer', () => {
  it('uses the image catalogue and keeps the stack origin', () => {
    const description = describeContainer(container())

    expect(description.known).toBe(true)
    expect(description.name).toBe('PostgreSQL')
    expect(description.category).toBe('database')
    expect(description.commands).toEqual(['docker logs --tail 50 web', 'docker inspect web'])
    expect(description.origin).toBe('Part of stack shop (service db) · image postgres')
  })

  it('adds the details but not the host checks for a Certbot container', () => {
    const description = describeContainer(
      container({ name: 'certbot', image: 'certbot/certbot:latest', composeProject: '' }),
    )

    expect(description.known).toBe(true)
    expect(description.details).toContain('Certbot asks')
    expect(description.commands).toEqual([
      'docker logs --tail 50 certbot',
      'docker inspect certbot',
    ])
  })

  it('describes the origin of an unknown image', () => {
    const description = describeContainer(
      container({ name: 'custom', image: 'example.com/team/custom:1', composeProject: '' }),
    )

    expect(description.known).toBe(false)
    expect(description.importance).toBe('unknown')
    expect(description.what).toBe('Standalone container · image team/custom')
  })
})

describe('describeStack', () => {
  it('uses the image only when the whole stack shares one known image', () => {
    const description = describeStack('shop', [container(), container({ id: 'c2', name: 'db2' })])

    expect(description.known).toBe(true)
    expect(description.name).toBe('PostgreSQL')
    expect(description.origin).toBe('Part of stack shop · image postgres')
  })

  it('only shows the origin when the stack has several images', () => {
    const description = describeStack('shop', [
      container(),
      container({ id: 'c2', name: 'cache', image: 'docker.io/library/redis:7' }),
    ])

    expect(description.known).toBe(false)
    expect(description.importance).toBe('unknown')
    expect(description.what).toBe('Part of stack shop · images postgres, redis')
  })
})

describe('WhatIsThis', () => {
  it('renders the compact card for a known service', () => {
    render(<WhatIsThis target={{ kind: 'service', unit: service() }} />)

    expect(screen.getByText('OpenSSH server')).toBeTruthy()
    expect(screen.getByText('Remote access')).toBeTruthy()
    expect(screen.getByText('Essential')).toBeTruthy()
    expect(screen.getByText(/secure remote shell/)).toBeTruthy()
    expect(screen.getByText(/If it fails/)).toBeTruthy()
    expect(screen.getByText(/systemd unit ssh.service/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'View full card' })).toBeNull()
  })

  it('offers the full card only when the drawer provider is present', () => {
    render(
      <CatalogDrawerProvider>
        <WhatIsThis target={{ kind: 'service', unit: service() }} />
      </CatalogDrawerProvider>,
    )

    expect(screen.getByRole('button', { name: 'View full card' })).toBeTruthy()
  })

  it('omits the optional sections for a service without a catalogue entry', () => {
    render(
      <WhatIsThis
        target={{
          kind: 'service',
          unit: service({ name: 'custom-app.service', description: 'Custom app' }),
        }}
        variant="full"
      />,
    )

    expect(screen.queryByRole('heading', { name: 'If you stop it' })).toBeNull()
    expect(screen.queryByRole('heading', { name: 'Common problems' })).toBeNull()
    expect(screen.getByRole('heading', { name: 'How to check it' })).toBeTruthy()
  })

  it('renders the full variant with help texts', () => {
    render(<WhatIsThis target={{ kind: 'service', unit: service() }} variant="full" />)

    expect(screen.getByText(/Services that let you reach or log in/)).toBeTruthy()
    expect(screen.getByText(/The server or your access breaks without it/)).toBeTruthy()
  })

  it('never assigns an importance to an unknown service', () => {
    render(
      <WhatIsThis
        target={{
          kind: 'service',
          unit: service({ name: 'custom.service', description: 'Company daemon' }),
        }}
      />,
    )

    expect(screen.getByText('Unknown')).toBeTruthy()
    expect(screen.getByText('systemd says: Company daemon')).toBeTruthy()
  })
})
