import { describe, expect, it, vi } from 'vitest'

import { createTeamsApi } from './teams'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api teams client', () => {
  it('reads the team detail with members and assignments', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        json({
          id: 't1',
          name: 'Operations',
          members: ['alice', 'bob'],
          bindings: [{ id: 'b1', role: 'viewer', scope: { type: 'group', group_id: 'g1' } }],
        }),
      ),
    )
    const api = createTeamsApi(fetchImpl as unknown as typeof fetch)

    await expect(api.get('t1')).resolves.toEqual({
      id: 't1',
      name: 'Operations',
      members: ['alice', 'bob'],
      bindings: [{ id: 'b1', role: 'viewer', scope: { type: 'group', groupId: 'g1' } }],
    })
  })

  it('adds members and sends team assignments', async () => {
    const fetchImpl = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      void init
      if (String(input).endsWith('/bindings')) {
        return Promise.resolve(
          json({ id: 'b2', role: 'viewer', scope: { type: 'organization' } }, 201),
        )
      }
      return Promise.resolve(new Response(null, { status: 204 }))
    })
    const api = createTeamsApi(fetchImpl as unknown as typeof fetch)

    await api.addMember('t1', 'u1')
    await api.removeMember('t1', 'u1')
    await api.addBinding('t1', 'viewer', { type: 'organization' })

    const methods = fetchImpl.mock.calls.map(([, init]) => init?.method)
    expect(methods).toContain('PUT')
    expect(methods).toContain('DELETE')
    const post = fetchImpl.mock.calls.find(([, init]) => init?.method === 'POST')
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({
      role: 'viewer',
      scope: { type: 'organization', server_id: '', group_id: '', name: '' },
    })
  })
})
