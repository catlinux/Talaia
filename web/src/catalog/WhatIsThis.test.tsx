import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import type { Container } from '../api/containers'
import type { ServiceUnit } from '../api/services'
import i18n from '../i18n'
import { describeContainer, describeService, describeStack, WhatIsThis } from './WhatIsThis'

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
    expect(description.ifFails).toContain('lose remote access')
    expect(description.origin).toBe('systemd unit ssh.service')
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

describe('describeContainer', () => {
  it('uses the image catalogue and keeps the stack origin', () => {
    const description = describeContainer(container())

    expect(description.known).toBe(true)
    expect(description.name).toBe('PostgreSQL')
    expect(description.category).toBe('database')
    expect(description.origin).toBe('Part of stack shop (service db) · image postgres')
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
