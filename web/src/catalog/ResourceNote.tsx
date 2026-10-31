import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { createNotesApi, type Note, type NoteKind, type NotesApi } from '../api/notes'
import { errorMessage } from '../auth/AuthGate'
import { useToast } from '../components/ui'

interface ResourceNoteProps {
  serverId: string
  kind: NoteKind
  name: string
  canEdit: boolean
  username?: string
  /** api is injected by the tests; the card builds its own otherwise. */
  api?: NotesApi
}

/** ResourceNote shows and edits the operator note of one catalogue resource. */
export function ResourceNote({
  serverId,
  kind,
  name,
  canEdit,
  username = '',
  api,
}: ResourceNoteProps) {
  const { t, i18n } = useTranslation(['catalog', 'errors'])
  const toast = useToast()
  const client = useMemo(() => api ?? createNotesApi(), [api])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [note, setNote] = useState<Note | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  useEffect(() => {
    let active = true
    client.list(serverId, kind).then(
      (notes) => {
        if (!active) return
        setNote(notes.find((candidate) => candidate.name === name) ?? null)
        setLoaded(true)
      },
      (cause: unknown) => {
        if (!active) return
        setLoadError(errorMessage(t, cause))
        setLoaded(true)
      },
    )
    return () => {
      active = false
    }
  }, [client, kind, name, serverId, t])

  const date = (value: string): string => {
    const parsed = new Date(value)
    if (Number.isNaN(parsed.getTime())) return value
    return new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }).format(
      parsed,
    )
  }

  const startEditing = () => {
    setDraft(note?.note ?? '')
    setSaveError(null)
    setEditing(true)
  }

  const cancel = () => {
    setDraft('')
    setSaveError(null)
    setEditing(false)
  }

  const save = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const value = draft.trim()
    setSaving(true)
    setSaveError(null)
    try {
      await client.save(serverId, kind, name, value)
      if (value === '') {
        setNote(null)
      } else {
        setNote({
          name,
          note: value,
          updatedBy: username,
          updatedAt: new Date().toISOString(),
        })
      }
      setEditing(false)
      toast.success(t(value === '' ? 'note.removed' : 'note.saved'))
    } catch (cause) {
      setSaveError(errorMessage(t, cause))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="whatisthis__section whatisthis__note">
      <h3 className="whatisthis__heading">{t('note.label')}</h3>
      {!loaded ? <p className="whatisthis__note-empty">{t('note.loading')}</p> : null}
      {loadError !== null ? (
        <p className="whatisthis__note-error" role="alert">
          {loadError}
        </p>
      ) : null}
      {loaded && loadError === null ? (
        <>
          {note !== null ? (
            <>
              <p className="whatisthis__note-text">{note.note}</p>
              <p className="whatisthis__note-meta">
                {t('note.by', { user: note.updatedBy, date: date(note.updatedAt) })}
              </p>
            </>
          ) : (
            <p className="whatisthis__note-empty">{t('note.empty')}</p>
          )}
          {canEdit && !editing ? (
            <button type="button" className="whatisthis__note-action" onClick={startEditing}>
              {note === null ? t('note.add') : t('note.edit')}
            </button>
          ) : null}
        </>
      ) : null}

      {editing ? (
        <form className="whatisthis__note-form" onSubmit={(event) => void save(event)}>
          <textarea
            className="whatisthis__note-input"
            aria-label={t('note.label')}
            rows={4}
            value={draft}
            onChange={(event) => setDraft(Array.from(event.target.value).slice(0, 500).join(''))}
          />
          <div className="whatisthis__note-foot">
            <span className="whatisthis__note-counter" aria-live="polite">
              {t('note.counter', { count: Array.from(draft).length })}
            </span>
            <span className="whatisthis__note-actions">
              <button type="button" className="ui-btn" disabled={saving} onClick={cancel}>
                {t('note.cancel')}
              </button>
              <button type="submit" className="ui-btn ui-btn--primary" disabled={saving}>
                {saving ? t('note.saving') : t('note.save')}
              </button>
            </span>
          </div>
          {saveError !== null ? (
            <p className="whatisthis__note-error" role="alert">
              {saveError}
            </p>
          ) : null}
        </form>
      ) : null}
    </section>
  )
}
