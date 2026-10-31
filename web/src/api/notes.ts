import { createHttpClient } from './http'

/** NoteKind is the plural resource family used by the notes endpoints. */
export type NoteKind = 'containers' | 'stacks' | 'services'

/** Note is one operator note attached to a resource. */
export interface Note {
  name: string
  note: string
  updatedBy: string
  updatedAt: string
}

/** NotesApi reads and writes the notes of one server. */
export interface NotesApi {
  list(serverId: string, kind: NoteKind): Promise<Note[]>
  save(serverId: string, kind: NoteKind, name: string, note: string): Promise<void>
}

interface apiNote {
  name: string
  note: string
  updated_by: string
  updated_at: string
}

interface notesResponse {
  notes: apiNote[]
}

/** createNotesApi returns the resource notes API of the core. */
export function createNotesApi(fetchImpl?: typeof fetch): NotesApi {
  const http = createHttpClient(fetchImpl)
  return {
    list: async (serverId, kind) => {
      const body = await http.get<notesResponse>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/notes/${kind}`,
      )
      return body.notes.map((note) => ({
        name: note.name,
        note: note.note,
        updatedBy: note.updated_by,
        updatedAt: note.updated_at,
      }))
    },
    save: (serverId, kind, name, note) =>
      http.put<void>(
        `/api/v1/servers/${encodeURIComponent(serverId)}/notes/${kind}/${encodeURIComponent(name)}`,
        { note },
      ),
  }
}
