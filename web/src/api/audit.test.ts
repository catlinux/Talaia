import { describe, expect, it, vi } from 'vitest'

import { createAuditApi } from './audit'

/** jsonResponse builds a JSON response. */
function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('createAuditApi', () => {
  it('reads a page with every filter and maps the entries', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          entries: [
            {
              id: 'event-1',
              at: '2026-10-22T10:00:00Z',
              actor: 'admin',
              action: 'service_start',
              ip: '192.0.2.10',
              result: 'success',
              details: { server_id: 'srv-1', unit: 'docker.service' },
            },
          ],
          next: 'cursor-2',
        }),
      ),
    )
    const api = createAuditApi(fetchImpl as unknown as typeof fetch)

    const page = await api.list({
      limit: 25,
      before: 'cursor-1',
      action: 'service_*',
      serverId: 'srv-1',
      actor: 'admin',
    })

    expect(page.entries).toEqual([
      {
        id: 'event-1',
        at: '2026-10-22T10:00:00Z',
        actor: 'admin',
        action: 'service_start',
        ip: '192.0.2.10',
        result: 'success',
        details: { server_id: 'srv-1', unit: 'docker.service' },
      },
    ])
    expect(page.next).toBe('cursor-2')
    expect(fetchImpl).toHaveBeenCalledWith(
      '/api/v1/audit?limit=25&before=cursor-1&action=service_*&server_id=srv-1&actor=admin',
      expect.objectContaining({ credentials: 'same-origin' }),
    )
  })

  it('omits empty filters and answers an empty page', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(jsonResponse({ entries: [], next: '' })))
    const api = createAuditApi(fetchImpl as unknown as typeof fetch)

    const page = await api.list()

    expect(page).toEqual({ entries: [], next: '' })
    expect(fetchImpl).toHaveBeenCalledWith(
      '/api/v1/audit',
      expect.objectContaining({ credentials: 'same-origin' }),
    )
  })

  it('keeps a missing details object as null', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          entries: [{ id: 'event-2', at: '2026-10-22T10:00:00Z', action: 'login_success' }],
        }),
      ),
    )
    const api = createAuditApi(fetchImpl as unknown as typeof fetch)

    const page = await api.list()

    expect(page.entries[0]?.details).toBeNull()
    expect(page.next).toBe('')
  })
})
