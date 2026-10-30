import { describe, expect, it, vi } from 'vitest'

import type { Container } from '../../api/containers'
import { containerBulkAvailability, stackBulkAvailability } from './bulk'

function container(overrides: Partial<Container> = {}): Container {
  return {
    id: 'c1',
    name: 'web',
    image: 'nginx',
    state: 'running',
    status: 'Up',
    composeProject: 'shop',
    composeService: 'web',
    ...overrides,
  }
}

describe('containerBulkAvailability', () => {
  it('allows every action when all selected containers are permitted', () => {
    const can = vi.fn(() => true)
    const availability = containerBulkAvailability(
      [
        { serverId: 'srv-1', container: container() },
        { serverId: 'srv-2', container: container({ id: 'c2', composeProject: '' }) },
      ],
      can,
    )

    expect(availability.start).toEqual({ allowed: true, denied: 0 })
    expect(availability.stop).toEqual({ allowed: true, denied: 0 })
    expect(availability.restart).toEqual({ allowed: true, denied: 0 })
    expect(can).toHaveBeenCalledWith('container.control', {
      serverId: 'srv-1',
      stack: 'shop',
      container: 'web',
    })
  })

  it('disables an action when any selected container is not permitted', () => {
    const can = vi.fn((_permission, scope) => scope?.serverId !== 'srv-2')
    const availability = containerBulkAvailability(
      [
        { serverId: 'srv-1', container: container() },
        { serverId: 'srv-2', container: container({ id: 'c2' }) },
      ],
      can,
    )

    expect(availability.start).toEqual({ allowed: false, denied: 1 })
    expect(availability.stop.allowed).toBe(false)
  })

  it('does not allow actions on an empty selection', () => {
    const availability = containerBulkAvailability([], () => true)
    expect(availability.start).toEqual({ allowed: false, denied: 0 })
  })
})

describe('stackBulkAvailability', () => {
  it('checks stack.deploy on every selected stack', () => {
    const can = vi.fn((_permission, scope) => scope?.serverId === 'srv-1')
    const availability = stackBulkAvailability(
      [
        { serverId: 'srv-1', stack: 'shop' },
        { serverId: 'srv-2', stack: 'blog' },
      ],
      can,
    )

    expect(availability.restart).toEqual({ allowed: false, denied: 1 })
    expect(availability.pull.denied).toBe(1)
    expect(can).toHaveBeenCalledWith('stack.deploy', { serverId: 'srv-1', stack: 'shop' })
  })
})
