import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Container, ContainersApi, StackAction } from '../../api/containers'
import { isSignedOut } from '../../api/http'
import { errorMessage } from '../../auth/AuthGate'
import { PlayIcon, PlusIcon, RestartIcon, StopIcon, TrashIcon } from '../../components/icons'
import { StackEditor } from './StackEditor'
import { TimedOut, waitForJob } from './waitForJob'

/** refreshEvery is how often the list of stacks reloads. */
const refreshEvery = 10_000

interface StacksSectionProps {
  api: ContainersApi
  serverId: string
  /** containers are the ones the server reports; they give the state of each stack. */
  containers: Container[]
  pollMs: number
  /** onChanged is called when an operation ends, so the containers reload. */
  onChanged: () => void
}

type Outcome = { kind: 'working' } | { kind: 'done' } | { kind: 'failed'; message: string }

type Editor = { kind: 'closed' } | { kind: 'open'; name: string }

const rowActions: StackAction[] = ['up', 'pull', 'restart', 'down']

const actionIcons: Record<string, typeof PlayIcon> = {
  up: PlayIcon,
  restart: RestartIcon,
  down: StopIcon,
  remove: TrashIcon,
}

/**
 * StacksSection lists the Compose stacks Talaia manages on the server, runs
 * operations on them and opens the editor. Stopping or removing a stack asks for
 * a confirmation first.
 */
export function StacksSection({
  api,
  serverId,
  containers,
  pollMs,
  onChanged,
}: StacksSectionProps) {
  const { t } = useTranslation(['server', 'errors'])
  const [names, setNames] = useState<string[] | null>(null)
  const [outcomes, setOutcomes] = useState<Record<string, Outcome>>({})
  const [confirming, setConfirming] = useState<{ name: string; action: 'down' | 'remove' } | null>(
    null,
  )
  const [editor, setEditor] = useState<Editor>({ kind: 'closed' })
  const alive = useRef(true)

  // load reads the list; a failure keeps what is on screen until the next refresh.
  const load = useCallback(
    () =>
      api
        .stacks(serverId)
        .then((list) => {
          if (alive.current) setNames(list.stacks)
        })
        .catch((error: unknown) => {
          if (!alive.current || isSignedOut(error)) return
        }),
    [api, serverId],
  )

  useEffect(() => {
    alive.current = true
    void load()
    const timer = setInterval(() => {
      if (!document.hidden) void load()
    }, refreshEvery)
    return () => {
      alive.current = false
      clearInterval(timer)
    }
  }, [load])

  const setOutcome = useCallback((name: string, outcome: Outcome | null) => {
    setOutcomes((current) => {
      const next = { ...current }
      if (outcome === null) delete next[name]
      else next[name] = outcome
      return next
    })
  }, [])

  const run = useCallback(
    async (name: string, action: StackAction) => {
      setConfirming(null)
      setOutcome(name, { kind: 'working' })
      try {
        const jobId = await api.runStack(
          serverId,
          name,
          action,
          action === 'down' || action === 'remove',
        )
        const job = await waitForJob(api, jobId, pollMs, () => alive.current)
        if (job === null) return
        setOutcome(
          name,
          job.state === 'succeeded' ? { kind: 'done' } : { kind: 'failed', message: job.message },
        )
      } catch (error) {
        if (!alive.current || isSignedOut(error)) return
        setOutcome(name, {
          kind: 'failed',
          message:
            error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error),
        })
      }
      await load()
      onChanged()
    },
    [api, load, onChanged, pollMs, serverId, setOutcome, t],
  )

  const stateOf = (name: string) => {
    const own = containers.filter((container) => container.composeProject === name)
    return {
      total: own.length,
      running: own.filter((container) => container.state === 'running').length,
    }
  }

  return (
    <section className="stacks" aria-labelledby="stacks-title">
      <header className="stacks__head">
        <div>
          <h2 className="stacks__title" id="stacks-title">
            {t('server:stacks.title')}
          </h2>
          <p className="stacks__intro">{t('server:stacks.intro')}</p>
        </div>
        <button
          type="button"
          className="cbtn cbtn--primary"
          disabled={editor.kind === 'open'}
          onClick={() => setEditor({ kind: 'open', name: '' })}
        >
          <PlusIcon />
          {t('server:stacks.new')}
        </button>
      </header>

      {editor.kind === 'open' ? (
        <StackEditor
          key={editor.name}
          api={api}
          serverId={serverId}
          name={editor.name}
          pollMs={pollMs}
          onCancel={() => setEditor({ kind: 'closed' })}
          onDone={(name, start) => {
            setEditor({ kind: 'closed' })
            setOutcome(name, { kind: 'done' })
            void load()
            if (start) onChanged()
          }}
        />
      ) : null}

      {names !== null && names.length === 0 && editor.kind === 'closed' ? (
        <p className="stacks__empty">{t('server:stacks.empty')}</p>
      ) : null}

      {names !== null && names.length > 0 ? (
        <ul className="stacklist">
          {names.map((name) => {
            const { total, running } = stateOf(name)
            const outcome = outcomes[name]
            const busy = outcome?.kind === 'working'
            return (
              <li key={name} className={`stack${running > 0 ? ' stack--up' : ''}`}>
                <div className="stack__main">
                  <h3 className="stack__name">{name}</h3>
                  <p className="stack__meta">
                    {total === 0
                      ? t('server:stacks.noContainers')
                      : t('server:stacks.summary', { count: total, running })}
                  </p>
                  {outcome?.kind === 'working' ? (
                    <p className="container__outcome" role="status">
                      {t('server:progress.working')}
                    </p>
                  ) : null}
                  {outcome?.kind === 'done' ? (
                    <p className="container__outcome container__outcome--ok" role="status">
                      {t('server:progress.done')}
                    </p>
                  ) : null}
                  {outcome?.kind === 'failed' ? (
                    <p className="container__outcome container__outcome--error" role="alert">
                      {t('server:progress.failed', { message: outcome.message })}
                    </p>
                  ) : null}
                </div>

                {confirming?.name === name ? (
                  <div
                    className="container__confirm"
                    role="group"
                    aria-label={t(`server:stacks.confirm.${confirming.action}Title`, { name })}
                  >
                    <p className="container__confirm-title">
                      {t(`server:stacks.confirm.${confirming.action}Title`, { name })}
                    </p>
                    <p className="container__confirm-body">
                      {t(`server:stacks.confirm.${confirming.action}Body`)}
                    </p>
                    <div className="container__actions">
                      <button
                        type="button"
                        className="cbtn cbtn--danger"
                        onClick={() => void run(name, confirming.action)}
                      >
                        {t(`server:stacks.confirm.${confirming.action}Yes`)}
                      </button>
                      <button type="button" className="cbtn" onClick={() => setConfirming(null)}>
                        {t('server:stacks.confirm.no')}
                      </button>
                    </div>
                  </div>
                ) : (
                  <div className="container__actions">
                    {rowActions.map((action) => {
                      const ActionIcon = actionIcons[action]
                      return (
                        <button
                          key={action}
                          type="button"
                          className={action === 'down' ? 'cbtn cbtn--danger' : 'cbtn'}
                          disabled={busy}
                          onClick={() =>
                            action === 'down'
                              ? setConfirming({ name, action })
                              : void run(name, action)
                          }
                        >
                          {ActionIcon ? <ActionIcon /> : null}
                          {t(`server:stacks.action.${action}`)}
                        </button>
                      )
                    })}
                    <button
                      type="button"
                      className="cbtn"
                      disabled={busy || editor.kind === 'open'}
                      onClick={() => setEditor({ kind: 'open', name })}
                    >
                      {t('server:stacks.action.edit')}
                    </button>
                    <button
                      type="button"
                      className="cbtn cbtn--danger"
                      disabled={busy}
                      onClick={() => setConfirming({ name, action: 'remove' })}
                    >
                      <TrashIcon />
                      {t('server:stacks.action.remove')}
                    </button>
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      ) : null}
    </section>
  )
}
