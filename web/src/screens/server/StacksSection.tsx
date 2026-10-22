import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Container, ContainersApi, StackAction, StackList } from '../../api/containers'
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
  /** onOpenStack opens the screen of a managed stack. */
  onOpenStack: (name: string, adopted: boolean, broken: boolean) => void
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
  onOpenStack,
}: StacksSectionProps) {
  const { t } = useTranslation(['server', 'errors'])
  const [list, setList] = useState<StackList | null>(null)
  const [outcomes, setOutcomes] = useState<Record<string, Outcome>>({})
  const [confirming, setConfirming] = useState<{
    name: string
    action: 'down' | 'remove' | 'forget'
  } | null>(null)
  const [editor, setEditor] = useState<Editor>({ kind: 'closed' })
  const alive = useRef(true)

  // load reads the list; a failure keeps what is on screen until the next refresh.
  const load = useCallback(
    () =>
      api
        .stacks(serverId)
        .then((fresh) => {
          if (alive.current) setList(fresh)
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

  const adopt = useCallback(
    async (name: string) => {
      setOutcome(name, { kind: 'working' })
      try {
        const jobId = await api.adoptStack(serverId, name)
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
    },
    [api, load, pollMs, serverId, setOutcome, t],
  )

  const names = list === null ? null : list.stacks
  const adoptable = list === null ? [] : list.adoptable
  const broken = new Set(list?.broken ?? [])

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
            const adopted = list !== null && list.adopted.includes(name)
            const isBroken = broken.has(name)
            return (
              <li key={name} className={`stack${running > 0 ? ' stack--up' : ''}`}>
                <div className="stack__main">
                  <h3 className="stack__name">
                    <button
                      type="button"
                      className="stack__open"
                      onClick={() => onOpenStack(name, adopted, isBroken)}
                    >
                      {name}
                    </button>
                    {isBroken ? (
                      <span className="tag tag--warning">{t('server:stacks.brokenTag')}</span>
                    ) : adopted ? (
                      <span className="tag">{t('server:stacks.adoptedTag')}</span>
                    ) : null}
                  </h3>
                  <p className="stack__meta">
                    {total === 0
                      ? t('server:stacks.noContainers')
                      : t('server:stacks.summary', { count: total, running })}
                  </p>
                  {isBroken ? (
                    <p className="stack__broken">{t('server:stacks.brokenBody')}</p>
                  ) : null}
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
                ) : isBroken ? (
                  <div className="container__actions">
                    <button
                      type="button"
                      className="cbtn cbtn--danger"
                      disabled={busy}
                      onClick={() => setConfirming({ name, action: 'forget' })}
                    >
                      <TrashIcon />
                      {t('server:stacks.action.forget')}
                    </button>
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
                      onClick={() => setConfirming({ name, action: adopted ? 'forget' : 'remove' })}
                    >
                      <TrashIcon />
                      {t(adopted ? 'server:stacks.action.forget' : 'server:stacks.action.remove')}
                    </button>
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      ) : null}

      {adoptable.length > 0 ? (
        <div className="adoptable">
          <h3 className="adoptable__title">{t('server:stacks.adopt.title')}</h3>
          <p className="adoptable__intro">{t('server:stacks.adopt.intro')}</p>
          <ul className="adoptable__list">
            {adoptable.map((name) => {
              const { total, running } = stateOf(name)
              const outcome = outcomes[name]
              return (
                <li key={name} className="adoptable__item">
                  <div className="stack__main">
                    <span className="stack__name">{name}</span>
                    <span className="stack__meta">
                      {t('server:stacks.summary', { count: total, running })}
                    </span>
                    {outcome?.kind === 'failed' ? (
                      <span className="container__outcome container__outcome--error" role="alert">
                        {t('server:progress.failed', { message: outcome.message })}
                      </span>
                    ) : null}
                  </div>
                  <button
                    type="button"
                    className="cbtn"
                    disabled={outcome?.kind === 'working'}
                    onClick={() => void adopt(name)}
                  >
                    {t('server:stacks.adopt.button')}
                  </button>
                </li>
              )
            })}
          </ul>
        </div>
      ) : null}
    </section>
  )
}
