import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AuditApi, AuditPage, AuditQuery } from '../api/audit'
import i18n from '../i18n'
import { RouterProvider } from '../router'
import { Audit } from './Audit'

const at = '2026-10-22T10:00:00Z'

function page(overrides: Partial<AuditPage> = {}): AuditPage {
  return { entries: [], next: '', ...overrides }
}

function renderAudit(ui: ReactNode, path = '/audit') {
  window.history.replaceState({}, '', path)
  return render(<RouterProvider>{ui}</RouterProvider>)
}

/** fakeApi answers from a queue of pages and records the queries. */
function fakeApi(pages: AuditPage[]) {
  const calls: AuditQuery[] = []
  const queue = [...pages]
  const api = {
    list: vi.fn((query: AuditQuery = {}) => {
      calls.push(query)
      return Promise.resolve(
        queue.length > 1 ? (queue.shift() as AuditPage) : (queue[0] as AuditPage),
      )
    }),
  } satisfies AuditApi
  return { api, calls }
}

describe('Audit', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('lists events newest first, translates known actions and renders details as text', async () => {
    const { api } = fakeApi([
      page({
        entries: [
          {
            id: 'event-1',
            at,
            actor: 'admin',
            action: 'terminal_open',
            ip: '192.0.2.10',
            result: 'success',
            details: {
              server_id: 'srv-1',
              container: '<b>web</b>',
              shell: 'bash',
            },
          },
          {
            id: 'event-2',
            at,
            actor: 'cli',
            action: 'custom_action',
            ip: '',
            result: 'failure',
            details: null,
          },
        ],
        next: 'next-page',
      }),
      page({
        entries: [
          {
            id: 'event-3',
            at,
            actor: 'admin',
            action: 'login_success',
            ip: '192.0.2.11',
            result: 'ok',
            details: null,
          },
        ],
      }),
    ])

    renderAudit(<Audit api={api} limit={2} serverNames={{ 'srv-1': 'vps-web-01' }} />)

    expect(await screen.findByRole('heading', { level: 1, name: 'Audit' })).toBeTruthy()
    expect(await screen.findByText('Terminal opened')).toBeTruthy()
    expect(screen.getByText('custom_action')).toBeTruthy()
    expect(screen.getAllByText('Success')).toHaveLength(1)
    expect(screen.getByText('Failure')).toBeTruthy()
    expect(screen.getByText('vps-web-01')).toBeTruthy()
    expect(screen.getByText('No server')).toBeTruthy()
    expect(screen.queryByText('Target: <b>web</b>')).toBeNull()
    expect(screen.getByText('<b>web</b>')).toBeTruthy()

    const detailsButton = screen.getByRole('button', { name: 'Details' })
    expect(detailsButton.className).toBe('ui-btn')
    fireEvent.click(detailsButton)
    expect(within(screen.getByRole('dialog')).getByText('<b>web</b>')).toBeTruthy()
    expect(document.querySelector('b')).toBeNull()
    expect(screen.getByText('bash')).toBeTruthy()
  })

  it('filters by action group and actor and resets the list', async () => {
    const { api, calls } = fakeApi([page()])
    renderAudit(<Audit api={api} />)
    await waitFor(() => expect(calls).toHaveLength(1))

    fireEvent.change(screen.getByLabelText('Action'), { target: { value: 'terminal_*' } })
    await waitFor(() => expect(calls.at(-1)?.action).toBe('terminal_*'))
    expect(new URLSearchParams(window.location.search).get('action')).toBe('terminal_*')

    fireEvent.change(screen.getByLabelText('Actor'), { target: { value: ' admin ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Filter' }))
    await waitFor(() => expect(calls.at(-1)?.actor).toBe('admin'))
    expect(calls.at(-1)?.action).toBe('terminal_*')
    expect(new URLSearchParams(window.location.search).get('user')).toBe('admin')
  })

  it('searches the loaded events and keeps the text filter in the URL', async () => {
    const { api, calls } = fakeApi([
      page({
        entries: [
          {
            id: 'event-1',
            at,
            actor: 'admin',
            action: 'terminal_open',
            ip: '',
            result: 'success',
            details: null,
          },
          {
            id: 'event-2',
            at,
            actor: 'cli',
            action: 'login_success',
            ip: '',
            result: 'success',
            details: null,
          },
        ],
      }),
    ])
    renderAudit(<Audit api={api} />)
    expect(await screen.findByText('Terminal opened')).toBeTruthy()
    const before = calls.length

    fireEvent.change(screen.getByLabelText('Search'), { target: { value: 'cli' } })
    expect(new URLSearchParams(window.location.search).get('q')).toBe('cli')
    expect(screen.queryByText('Terminal opened')).toBeNull()
    expect(screen.getByText('Sign-in succeeded')).toBeTruthy()
    expect(calls).toHaveLength(before)
  })

  it('loads the next page with the cursor and appends it', async () => {
    const { api, calls } = fakeApi([
      page({
        entries: [
          {
            id: 'event-1',
            at,
            actor: 'admin',
            action: 'terminal_open',
            ip: '',
            result: 'success',
            details: null,
          },
        ],
        next: 'cursor-2',
      }),
      page({
        entries: [
          {
            id: 'event-2',
            at,
            actor: 'admin',
            action: 'login_success',
            ip: '',
            result: 'success',
            details: null,
          },
        ],
      }),
    ])

    renderAudit(<Audit api={api} />)

    expect(await screen.findByText('Terminal opened')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }))

    expect(await screen.findByText('Sign-in succeeded')).toBeTruthy()
    expect(calls.at(-1)?.before).toBe('cursor-2')
    expect(screen.getByText('Terminal opened')).toBeTruthy()
  })

  it('shows the empty state and the read error', async () => {
    const empty = fakeApi([page()])
    renderAudit(<Audit api={empty.api} />)
    expect(await screen.findByText('There are no events for these filters.')).toBeTruthy()
    cleanup()

    const failing: AuditApi = {
      list: () => Promise.reject(new Error('database unavailable')),
    }
    renderAudit(<Audit api={failing} />)
    expect(await screen.findByRole('alert')).toBeTruthy()
  })
})
