import { describe, expect, it, vi } from 'vitest'

import { createNotesApi } from './notes'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api notes client', () => {
  it('lists and maps the notes of a kind', async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(
        json({
          notes: [
            {
              name: 'web',
              note: 'do not touch',
              updated_by: 'alice',
              updated_at: '2026-10-31T12:00:00Z',
            },
          ],
        }),
      ),
    )
    const api = createNotesApi(fetchImpl as unknown as typeof fetch)

    await expect(api.list('s/1', 'containers')).resolves.toEqual([
      {
        name: 'web',
        note: 'do not touch',
        updatedBy: 'alice',
        updatedAt: '2026-10-31T12:00:00Z',
      },
    ])
    expect(fetchImpl).toHaveBeenCalledWith('/api/v1/servers/s%2F1/notes/containers', {
      method: 'GET',
      credentials: 'same-origin',
      headers: {},
      body: undefined,
    })
  })

  it('saves a note with the resource name encoded', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve(new Response(null, { status: 204 })))
    const api = createNotesApi(fetchImpl as unknown as typeof fetch)

    await api.save('s1', 'services', 'getty@tty1.service', 'keep it')
    expect(fetchImpl).toHaveBeenCalledWith(
      '/api/v1/servers/s1/notes/services/getty%40tty1.service',
      {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ note: 'keep it' }),
      },
    )
  })
})
