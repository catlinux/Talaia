import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Note, NotesApi } from '../api/notes'
import { ApiError } from '../api/http'
import { ToastProvider } from '../components/ui'
import i18n from '../i18n'
import { ResourceNote } from './ResourceNote'

function notesApi(notes: Note[] = []): NotesApi & {
  list: ReturnType<typeof vi.fn>
  save: ReturnType<typeof vi.fn>
} {
  return {
    list: vi.fn(() => Promise.resolve(notes)),
    save: vi.fn(() => Promise.resolve()),
  }
}

function renderNote(api: NotesApi, canEdit = false) {
  return render(
    <ToastProvider>
      <ResourceNote
        serverId="s1"
        kind="containers"
        name="web"
        canEdit={canEdit}
        username="bob"
        api={api}
      />
    </ToastProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

afterEach(cleanup)

describe('ResourceNote', () => {
  it('shows the note with its author and date but no editor without permission', async () => {
    const api = notesApi([
      {
        name: 'web',
        note: 'shop website,\ndo not touch during opening hours',
        updatedBy: 'alice',
        updatedAt: '2026-10-31T12:00:00Z',
      },
    ])
    renderNote(api)

    expect(await screen.findByText('shop website, do not touch during opening hours')).toBeTruthy()
    expect(screen.getByText(/by alice, .*2026/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Add note' })).toBeNull()
  })

  it('adds a note and shows it with the current user', async () => {
    const api = notesApi()
    renderNote(api, true)

    expect(await screen.findByText('No note yet.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Add note' }))
    fireEvent.change(screen.getByLabelText('Your note'), { target: { value: 'keep it' } })
    expect(screen.getByText('7/500 characters')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(api.save).toHaveBeenCalledWith('s1', 'containers', 'web', 'keep it'))
    expect(await screen.findByText('Note saved.')).toBeTruthy()
    expect(await screen.findByText('keep it')).toBeTruthy()
    expect(screen.getByText(/by bob, .*2026/)).toBeTruthy()
  })

  it('keeps the textarea at 500 characters', async () => {
    const api = notesApi()
    renderNote(api, true)
    await screen.findByText('No note yet.')

    fireEvent.click(screen.getByRole('button', { name: 'Add note' }))
    const input = screen.getByLabelText('Your note') as HTMLTextAreaElement
    fireEvent.change(input, { target: { value: 'a'.repeat(501) } })
    expect(input.value).toHaveLength(500)
    expect(screen.getByText('500/500 characters')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(api.save).toHaveBeenCalledWith('s1', 'containers', 'web', 'a'.repeat(500)),
    )
  })

  it('deletes the note when the textarea is cleared', async () => {
    const api = notesApi([
      { name: 'web', note: 'old', updatedBy: 'alice', updatedAt: '2026-10-31T12:00:00Z' },
    ])
    renderNote(api, true)

    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
    fireEvent.change(screen.getByLabelText('Your note'), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(api.save).toHaveBeenCalledWith('s1', 'containers', 'web', ''))
    expect(await screen.findByText('Note removed.')).toBeTruthy()
    expect(await screen.findByText('No note yet.')).toBeTruthy()
  })

  it('shows the save error inline', async () => {
    const api = notesApi()
    api.save.mockRejectedValueOnce(new ApiError('forbidden', 403))
    renderNote(api, true)
    await screen.findByText('No note yet.')

    fireEvent.click(screen.getByRole('button', { name: 'Add note' }))
    fireEvent.change(screen.getByLabelText('Your note'), { target: { value: 'hello' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect((await screen.findByRole('alert')).textContent).toBeTruthy()
  })
})
