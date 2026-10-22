import { describe, expect, it, vi } from 'vitest'

import { ApiError } from './http'
import { createTerminalApi, parseTerminalMessage, socketUrl } from './terminal'

describe('socketUrl', () => {
  it('uses wss on https and ws on http, on the origin of the panel', () => {
    expect(socketUrl('abc', { protocol: 'https:', host: 'core.example.com:8443' })).toBe(
      'wss://core.example.com:8443/api/v1/terminal/abc/ws',
    )
    expect(socketUrl('abc', { protocol: 'http:', host: 'localhost:5173' })).toBe(
      'ws://localhost:5173/api/v1/terminal/abc/ws',
    )
  })

  it('escapes the session id', () => {
    expect(socketUrl('a/b', { protocol: 'https:', host: 'x' })).toBe(
      'wss://x/api/v1/terminal/a%2Fb/ws',
    )
  })
})

describe('parseTerminalMessage', () => {
  it('reads the closing message of the core', () => {
    expect(parseTerminalMessage('{"type":"closed","reason":"exited","exit_code":2}')).toEqual({
      type: 'closed',
      reason: 'exited',
      exitCode: 2,
    })
  })

  it('ignores anything else', () => {
    expect(parseTerminalMessage('not json')).toBeNull()
    expect(parseTerminalMessage('{"type":"other"}')).toBeNull()
    expect(parseTerminalMessage('[]')).toBeNull()
  })

  it('tolerates a closing message with missing fields', () => {
    expect(parseTerminalMessage('{"type":"closed"}')).toEqual({
      type: 'closed',
      reason: '',
      exitCode: 0,
    })
  })
})

describe('createTerminalApi', () => {
  it('asks for the terminal with the confirmation and the size', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ session_id: 's1', ticket: 't1', privileged: true }), {
          status: 201,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    const api = createTerminalApi(fetchImpl as unknown as typeof fetch)

    const session = await api.open('srv 1', 'web', 'bash', 100, 30)

    expect(session).toEqual({ sessionId: 's1', ticket: 't1', privileged: true })
    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/servers/srv%201/containers/web/terminal')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({
      confirm: true,
      shell: 'bash',
      cols: 100,
      rows: 30,
    })
  })

  it('asks for a host terminal with the fresh code', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ session_id: 'h1', ticket: 't1', privileged: true }), {
          status: 201,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    const api = createTerminalApi(fetchImpl as unknown as typeof fetch)

    const session = await api.openHost('srv 1', 'bash', '123456', 100, 30)

    expect(session).toEqual({ sessionId: 'h1', ticket: 't1', privileged: true })
    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/v1/servers/srv%201/terminal')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({
      confirm: true,
      code: '123456',
      shell: 'bash',
      cols: 100,
      rows: 30,
    })
  })

  it('turns the codes of the core into typed errors', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ error: { code: 'terminal_exists' } }), {
          status: 409,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    const api = createTerminalApi(fetchImpl as unknown as typeof fetch)

    await expect(api.open('s', 'web', 'sh', 80, 24)).rejects.toMatchObject({
      code: 'terminal_exists',
      status: 409,
    })
    await expect(api.open('s', 'web', 'sh', 80, 24)).rejects.toBeInstanceOf(ApiError)
  })
})
