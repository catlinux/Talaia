import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { ContainersApi, StackFiles } from '../../api/containers'
import { isSignedOut } from '../../api/http'
import { errorMessage } from '../../auth/AuthGate'
import { TimedOut, waitForJob } from './waitForJob'

/** stackNamePattern is what the core accepts as the name of a stack. */
const stackNamePattern = '[a-z0-9][a-z0-9_\\u002D]{0,62}'

interface StackEditorProps {
  api: ContainersApi
  serverId: string
  /** name is set when an existing stack is edited and empty for a new one. */
  name: string
  pollMs: number
  /** onDone is called after a successful save; start tells whether it also started. */
  onDone: (name: string, start: boolean) => void
  onCancel: () => void
}

type Phase =
  | { kind: 'loading' }
  | { kind: 'editing' }
  | { kind: 'saving' }
  | { kind: 'error'; message: string }

/**
 * StackEditor edits the compose.yaml and .env of a managed stack. Saving sends
 * them to the agent, which validates them with docker compose before it replaces
 * anything; the message of a rejection is shown as it comes.
 */
export function StackEditor({ api, serverId, name, pollMs, onDone, onCancel }: StackEditorProps) {
  const { t } = useTranslation(['server', 'errors'])
  const editing = name !== ''
  const [stackName, setStackName] = useState(name)
  const [compose, setCompose] = useState('')
  const [env, setEnv] = useState('')
  const [phase, setPhase] = useState<Phase>(editing ? { kind: 'loading' } : { kind: 'editing' })
  const alive = useRef(true)

  // An existing stack is read from the server first.
  useEffect(() => {
    alive.current = true
    if (!editing) {
      return () => {
        alive.current = false
      }
    }
    void (async () => {
      try {
        const jobId = await api.readStack(serverId, name)
        const job = await waitForJob(api, jobId, pollMs, () => alive.current)
        if (job === null || !alive.current) return
        if (job.state !== 'succeeded') {
          setPhase({ kind: 'error', message: job.message })
          return
        }
        const files = JSON.parse(job.output) as StackFiles
        setCompose(files.compose)
        setEnv(files.env)
        setPhase({ kind: 'editing' })
      } catch (error) {
        if (!alive.current || isSignedOut(error)) return
        setPhase({ kind: 'error', message: errorText(error) })
      }
    })()
    return () => {
      alive.current = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- read once per stack
  }, [api, editing, name, pollMs, serverId])

  function errorText(error: unknown): string {
    return error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error)
  }

  async function save(start: boolean) {
    setPhase({ kind: 'saving' })
    try {
      const writeId = await api.writeStack(serverId, stackName, compose, env)
      const written = await waitForJob(api, writeId, pollMs, () => alive.current)
      if (written === null || !alive.current) return
      if (written.state !== 'succeeded') {
        setPhase({ kind: 'error', message: written.message })
        return
      }
      if (start) {
        const startId = await api.runStack(serverId, stackName, 'up')
        const started = await waitForJob(api, startId, pollMs, () => alive.current)
        if (started === null || !alive.current) return
        if (started.state !== 'succeeded') {
          setPhase({ kind: 'error', message: started.message })
          return
        }
      }
      onDone(stackName, start)
    } catch (error) {
      if (!alive.current || isSignedOut(error)) return
      setPhase({ kind: 'error', message: errorText(error) })
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void save(false)
  }

  const busy = phase.kind === 'saving' || phase.kind === 'loading'
  const title = editing
    ? t('server:stacks.editor.titleEdit', { name })
    : t('server:stacks.editor.titleNew')

  return (
    <section className="stackeditor" aria-label={title}>
      <h3 className="stackeditor__title">{title}</h3>

      {phase.kind === 'loading' ? (
        <p className="stackeditor__message" role="status">
          {t('server:stacks.editor.loading')}
        </p>
      ) : (
        <form onSubmit={submit}>
          {phase.kind === 'error' ? (
            <pre className="stackeditor__error" role="alert">
              {t('server:progress.failed', { message: phase.message })}
            </pre>
          ) : null}

          <label className="stackeditor__field">
            <span className="stackeditor__label">{t('server:stacks.editor.name')}</span>
            <input
              className="stackeditor__input"
              name="name"
              autoComplete="off"
              spellCheck={false}
              maxLength={63}
              pattern={stackNamePattern}
              value={stackName}
              disabled={editing || busy}
              required
              onChange={(event) => setStackName(event.target.value)}
            />
            <span className="stackeditor__hint">{t('server:stacks.editor.nameHint')}</span>
          </label>

          <label className="stackeditor__field">
            <span className="stackeditor__label">{t('server:stacks.editor.compose')}</span>
            <textarea
              className="stackeditor__text"
              name="compose"
              rows={14}
              spellCheck={false}
              value={compose}
              disabled={busy}
              required
              onChange={(event) => setCompose(event.target.value)}
            />
          </label>

          <label className="stackeditor__field">
            <span className="stackeditor__label">{t('server:stacks.editor.env')}</span>
            <textarea
              className="stackeditor__text stackeditor__text--short"
              name="env"
              rows={5}
              spellCheck={false}
              value={env}
              disabled={busy}
              onChange={(event) => setEnv(event.target.value)}
            />
            <span className="stackeditor__hint">{t('server:stacks.editor.envHint')}</span>
          </label>

          <div className="stackeditor__actions">
            <button type="submit" className="cbtn cbtn--primary" disabled={busy}>
              {phase.kind === 'saving'
                ? t('server:stacks.editor.saving')
                : t('server:stacks.editor.save')}
            </button>
            <button type="button" className="cbtn" disabled={busy} onClick={() => void save(true)}>
              {t('server:stacks.editor.saveStart')}
            </button>
            <button type="button" className="cbtn" onClick={onCancel}>
              {t('server:stacks.editor.cancel')}
            </button>
          </div>
        </form>
      )}
    </section>
  )
}
