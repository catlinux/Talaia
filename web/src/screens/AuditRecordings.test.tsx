import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AuditApi } from '../api/audit'
import type { Recording, RecordingsApi } from '../api/recordings'
import i18n from '../i18n'
import { RouterProvider } from '../router'
import { Audit } from './Audit'

const recordings: Recording[] = [
  {
    id: 'r1',
    serverId: 'srv1',
    serverName: 'vps-web-01',
    container: 'web',
    kind: 'container',
    shell: 'bash',
    user: 'admin',
    startedAt: '2026-10-20T10:00:00Z',
    endedAt: '2026-10-20T10:02:00Z',
    durationSeconds: 120,
    closeReason: 'exited',
    privileged: false,
    bytes: 2048,
  },
  {
    id: 'r2',
    serverId: 'srv1',
    serverName: 'vps-web-01',
    container: '',
    kind: 'host',
    shell: 'sh',
    user: 'admin',
    startedAt: '2026-10-20T11:00:00Z',
    endedAt: null,
    durationSeconds: 0,
    closeReason: '',
    privileged: true,
    bytes: 0,
  },
]

const auditApi: AuditApi = { list: () => Promise.resolve({ entries: [], next: '' }) }

function fakeRecordings(): RecordingsApi {
  return {
    list: vi.fn(() => Promise.resolve(recordings)),
    downloadUrl: (id) => `/api/v1/recordings/${id}/download`,
  }
}

function renderAudit(ui: ReactNode, path = '/audit') {
  window.history.replaceState({}, '', path)
  return render(<RouterProvider>{ui}</RouterProvider>)
}

describe('Audit recordings', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  afterEach(() => cleanup())

  it('lists recordings newest first, marks privileged sessions and links finished ones', async () => {
    const api = fakeRecordings()
    renderAudit(<Audit api={auditApi} recordingsApi={api} />)

    fireEvent.click(await screen.findByRole('tab', { name: 'Recordings' }))

    expect(await screen.findAllByText('vps-web-01')).toHaveLength(2)
    expect(screen.getAllByText('admin')).toHaveLength(2)
    expect(screen.getByText('web')).toBeTruthy()
    expect(screen.getByText('Host')).toBeTruthy()
    expect(screen.getByText('Shell exited')).toBeTruthy()
    expect(screen.getAllByText('Privileged')).toHaveLength(2)
    expect(screen.getByText('asciinema play file.cast')).toBeTruthy()

    const download = screen.getByRole('link', { name: 'Download .cast' })
    expect(download.getAttribute('href')).toBe('/api/v1/recordings/r1/download')
    expect(screen.getByText('Still open')).toBeTruthy()
    expect(api.list).toHaveBeenCalled()
  })

  it('opens recordings directly from its route', async () => {
    renderAudit(<Audit api={auditApi} recordingsApi={fakeRecordings()} />, '/audit/recordings')

    expect(
      (await screen.findByRole('tab', { name: 'Recordings' })).getAttribute('aria-selected'),
    ).toBe('true')
    expect(await screen.findAllByText('vps-web-01')).toHaveLength(2)
  })

  it('shows the recordings error without hiding the audit sections', async () => {
    const api: RecordingsApi = {
      list: () => Promise.reject(new Error('unavailable')),
      downloadUrl: (id) => `/api/v1/recordings/${id}/download`,
    }
    renderAudit(<Audit api={auditApi} recordingsApi={api} />)

    fireEvent.click(await screen.findByRole('tab', { name: 'Recordings' }))
    await waitFor(() => expect(screen.getByRole('alert')).toBeTruthy())
    expect(screen.getByRole('tab', { name: 'Audit log' })).toBeTruthy()
  })
})
