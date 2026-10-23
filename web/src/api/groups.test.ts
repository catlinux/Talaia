import { describe, expect, it, vi } from 'vitest'

import { createGroupsApi } from './groups'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api groups client', () => {
  it('lists groups and assigns a server', async () => {
    const fetchImpl = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      void init
      if (String(input) === '/api/v1/groups') {
        return Promise.resolve(json({ groups: [{ id: 'g1', name: 'Production' }] }))
      }
      return Promise.resolve(new Response(null, { status: 204 }))
    })
    const api = createGroupsApi(fetchImpl as unknown as typeof fetch)

    await expect(api.list()).resolves.toEqual([{ id: 'g1', name: 'Production' }])
    await api.setServerGroup('srv1', 'g1')
    await api.setServerGroup('srv1', null)

    const puts = fetchImpl.mock.calls.filter(([, init]) => init?.method === 'PUT')
    expect(JSON.parse(String(puts[0]?.[1]?.body))).toEqual({ group_id: 'g1' })
    expect(JSON.parse(String(puts[1]?.[1]?.body))).toEqual({ group_id: null })
  })
})
