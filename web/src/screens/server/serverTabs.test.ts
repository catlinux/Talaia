import { describe, expect, it } from 'vitest'

import { firstAllowedTab, parseServerPath, serverTabs, stackPath, tabPath } from './serverTabs'

describe('parseServerPath', () => {
  it('reads the overview with and without its explicit segment', () => {
    expect(parseServerPath('/servers/srv-1', 'srv-1')).toEqual({
      tab: 'overview',
      stackName: null,
    })
    expect(parseServerPath('/servers/srv-1/overview', 'srv-1')).toEqual({
      tab: 'overview',
      stackName: null,
    })
    expect(parseServerPath('/servers/srv-1/', 'srv-1')).toEqual({
      tab: 'overview',
      stackName: null,
    })
  })

  it('reads valid tabs and decodes the server id', () => {
    expect(parseServerPath('/servers/srv%201/updates', 'srv 1')).toEqual({
      tab: 'updates',
      stackName: null,
    })
    expect(parseServerPath('/servers/srv-1/containers', 'srv-1')).toEqual({
      tab: 'containers',
      stackName: null,
    })
  })

  it('reads a managed stack path and decodes its name', () => {
    expect(parseServerPath('/servers/srv-1/stacks/my%20shop', 'srv-1')).toEqual({
      tab: 'containers',
      stackName: 'my shop',
    })
  })

  it('returns a null tab for unknown or malformed paths', () => {
    expect(parseServerPath('/servers/srv-1/unknown', 'srv-1')).toEqual({
      tab: null,
      stackName: null,
    })
    expect(parseServerPath('/servers/other/updates', 'srv-1')).toEqual({
      tab: null,
      stackName: null,
    })
    expect(parseServerPath('/servers/srv-1/stacks', 'srv-1')).toEqual({
      tab: null,
      stackName: null,
    })
    expect(parseServerPath('/servers/srv-1/stacks/foo/bar', 'srv-1')).toEqual({
      tab: null,
      stackName: null,
    })
  })
})

describe('firstAllowedTab', () => {
  it('uses the first available tab', () => {
    expect(firstAllowedTab(['overview', 'containers'])).toBe('overview')
    expect(firstAllowedTab(['logs', 'updates'])).toBe('logs')
  })

  it('prefers the containers tab for a limited server', () => {
    expect(firstAllowedTab(['overview', 'containers'], true)).toBe('containers')
    expect(firstAllowedTab(['overview'], true)).toBe('overview')
  })

  it('falls back to containers when nothing is permitted', () => {
    expect(firstAllowedTab([])).toBe('containers')
  })
})

describe('paths', () => {
  it('builds canonical tab and stack URLs', () => {
    expect(tabPath('srv 1', 'overview')).toBe('/servers/srv%201')
    expect(tabPath('srv-1', 'updates')).toBe('/servers/srv-1/updates')
    expect(stackPath('srv-1', 'my shop')).toBe('/servers/srv-1/stacks/my%20shop')
    expect(serverTabs).toEqual(['overview', 'containers', 'services', 'logs', 'updates', 'alerts'])
  })
})
