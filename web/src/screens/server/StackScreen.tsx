import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Container, ContainersApi, StackAction } from '../../api/containers'
import { isSignedOut } from '../../api/http'
import { errorMessage } from '../../auth/AuthGate'
import { ArrowLeftIcon, PlayIcon, RestartIcon, StopIcon, TrashIcon } from '../../components/icons'
import { formatBytes, formatPercent } from '../fleet/format'
import { LogsPanel } from './LogsPanel'
import { StackEditor } from './StackEditor'
import { TimedOut, waitForJob } from './waitForJob'

interface StackScreenProps {
  api: ContainersApi
  server: { id: string; name: string }
  name: string
  adopted: boolean
  /** broken marks an adopted stack whose files are gone from the server. */
  broken: boolean
  /** containers are the ones the server screen already loads. */
  containers: Container[]
  connected: boolean
  dockerAvailable: boolean
  pollMs: number
  onBack: () => void
  /** onChanged asks the server screen to reload the containers. */
  onChanged: () => void
}

type Outcome = { kind: 'working' } | { kind: 'done' } | { kind: 'failed'; message: string }

/**
 * StackScreen is the screen of one managed stack: its containers, the compose
 * file and the live logs, with the same state rule and confirmations as the
 * list in StacksSection.
 */
export function StackScreen({
  api,
  server,
  name,
  adopted,
  broken,
  containers,
  connected,
  dockerAvailable,
  pollMs,
  onBack,
  onChanged,
}: StackScreenProps) {
  const { t, i18n } = useTranslation(['server', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [outcome, setOutcome] = useState<Outcome | null>(null)
  const [confirming, setConfirming] = useState<'down' | 'forget' | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  // chosen is null until the reader picks a container: the first one is shown.
  const [chosen, setChosen] = useState<string | null>(null)
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const own = containers.filter((container) => container.composeProject === name)
  const running = own.filter((container) => container.state === 'running').length
  const selected = chosen !== null ? chosen : (own[0]?.name ?? '')
  const logContainer = own.some((container) => container.name === selected) ? selected : ''
  const busy = outcome?.kind === 'working'
  const disabled = busy || !connected || !dockerAvailable

  const run = async (action: StackAction) => {
    setConfirming(null)
    setNotice(null)
    setOutcome({ kind: 'working' })
    let succeeded = false
    try {
      const jobId = await api.runStack(server.id, name, action, action === 'down')
      const job = await waitForJob(api, jobId, pollMs, () => alive.current)
      if (job === null || !alive.current) return
      succeeded = job.state === 'succeeded'
      setOutcome(
        job.state === 'succeeded' ? { kind: 'done' } : { kind: 'failed', message: job.message },
      )
    } catch (error) {
      if (!alive.current || isSignedOut(error)) return
      setOutcome({
        kind: 'failed',
        message: error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error),
      })
    }
    onChanged()
    if (succeeded && action === 'forget') {
      onBack()
    }
  }

  const containerState = (container: Container) =>
    t(`server:state.${container.state}`, { defaultValue: container.state })

  return (
    <section className="stackscreen" aria-labelledby="stack-title">
      <button type="button" className="serverscreen__back" onClick={onBack}>
        <ArrowLeftIcon />
        {t('server:stack.back')}
      </button>

      <nav className="stackscreen__crumb" aria-label={t('server:stack.breadcrumb')}>
        <button type="button" onClick={onBack}>
          {server.name}
        </button>
        <span aria-hidden="true">/</span>
        <button type="button" onClick={onBack}>
          {t('server:stacks.title')}
        </button>
        <span aria-hidden="true">/</span>
        <span aria-current="page">{name}</span>
      </nav>

      <header className="stackscreen__head">
        <div className="stackscreen__titles">
          <h1 className="stackscreen__title" id="stack-title">
            {name}
            {broken ? (
              <span className="tag tag--warning">{t('server:stacks.brokenTag')}</span>
            ) : adopted ? (
              <span className="tag">{t('server:stacks.adoptedTag')}</span>
            ) : null}
          </h1>
          <p className="stackscreen__meta">
            <span className={`status status--${running > 0 ? 'online' : 'offline'}`}>
              {t('server:stack.summary', { running, total: own.length })}
            </span>
          </p>
          {broken ? <p className="stackscreen__broken">{t('server:stacks.brokenBody')}</p> : null}
        </div>

        {confirming ? (
          <div
            className="container__confirm"
            role="group"
            aria-label={t(`server:stacks.confirm.${confirming}Title`, { name })}
          >
            <p className="container__confirm-title">
              {t(`server:stacks.confirm.${confirming}Title`, { name })}
            </p>
            <p className="container__confirm-body">
              {t(`server:stacks.confirm.${confirming}Body`)}
            </p>
            <div className="container__actions">
              <button
                type="button"
                className="cbtn cbtn--danger"
                onClick={() => void run(confirming)}
              >
                {t(`server:stacks.confirm.${confirming}Yes`)}
              </button>
              <button type="button" className="cbtn" onClick={() => setConfirming(null)}>
                {t('server:stacks.confirm.no')}
              </button>
            </div>
          </div>
        ) : broken ? (
          <div className="stackscreen__actions">
            <button
              type="button"
              className="cbtn cbtn--danger"
              disabled={busy}
              onClick={() => setConfirming('forget')}
            >
              <TrashIcon />
              {t('server:stacks.action.forget')}
            </button>
          </div>
        ) : (
          <div className="stackscreen__actions">
            <button
              type="button"
              className="cbtn"
              disabled={disabled}
              onClick={() => void run('restart')}
            >
              <RestartIcon />
              {t('server:stack.actions.restart')}
            </button>
            <button
              type="button"
              className="cbtn cbtn--danger"
              disabled={disabled}
              onClick={() => setConfirming('down')}
            >
              <StopIcon />
              {t('server:stack.actions.stop')}
            </button>
            <button
              type="button"
              className="cbtn cbtn--primary"
              disabled={disabled}
              onClick={() => void run('up')}
            >
              <PlayIcon />
              {t('server:stack.actions.deploy')}
            </button>
            <button
              type="button"
              className="cbtn"
              disabled={disabled}
              onClick={() => void run('pull')}
            >
              {t('server:stack.actions.pull')}
            </button>
          </div>
        )}
      </header>

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

      {!connected ? (
        <p className="serverscreen__message">{t('server:offline')}</p>
      ) : (
        <>
          <div className={`stackscreen__grid${broken ? ' stackscreen__grid--single' : ''}`}>
            {!broken ? (
              <section className="stackscreen__card" aria-labelledby="stack-compose-title">
                <h2 className="stackscreen__card-title" id="stack-compose-title">
                  {t('server:stack.compose.title')}
                </h2>
                {notice !== null ? (
                  <p className="stackscreen__notice" role="status">
                    {notice}
                  </p>
                ) : null}
                <StackEditor
                  key={name}
                  api={api}
                  serverId={server.id}
                  name={name}
                  pollMs={pollMs}
                  onDone={(_, start) => {
                    setNotice(
                      t(start ? 'server:stacks.editor.started' : 'server:stacks.editor.saved'),
                    )
                    onChanged()
                  }}
                />
              </section>
            ) : null}

            <section className="stackscreen__card" aria-labelledby="stack-containers-title">
              <h2 className="stackscreen__card-title" id="stack-containers-title">
                {t('server:stack.containers.title')}
              </h2>
              {!dockerAvailable ? (
                <p className="serverscreen__message">{t('server:noDocker')}</p>
              ) : own.length === 0 ? (
                <p className="serverscreen__message">{t('server:stack.containers.empty')}</p>
              ) : (
                <div className="stackscreen__tablewrap">
                  <table className="stackscreen__table">
                    <thead>
                      <tr>
                        <th scope="col">{t('server:stack.containers.name')}</th>
                        <th scope="col">{t('server:stack.containers.state')}</th>
                        <th scope="col" className="stackscreen__num">
                          {t('server:stack.containers.cpu')}
                        </th>
                        <th scope="col" className="stackscreen__num">
                          {t('server:stack.containers.memory')}
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {own.map((container) => (
                        <tr key={container.id}>
                          <td>
                            <code>{container.name}</code>
                          </td>
                          <td>
                            <span
                              className={`status status--${
                                container.state === 'running' ? 'online' : 'offline'
                              }`}
                            >
                              {containerState(container)}
                            </span>
                          </td>
                          <td className="stackscreen__num">
                            {container.use
                              ? formatPercent(container.use.cpuPercent, language)
                              : null}
                          </td>
                          <td className="stackscreen__num">
                            {container.use
                              ? formatBytes(container.use.memUsedBytes, language)
                              : null}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
          </div>

          <section className="stackscreen__card" aria-labelledby="stack-logs-title">
            <h2 className="stackscreen__card-title" id="stack-logs-title">
              {t('server:stack.logs.title')}
            </h2>
            {!dockerAvailable ? (
              <p className="serverscreen__message">{t('server:noDocker')}</p>
            ) : own.length === 0 ? (
              <p className="serverscreen__message">{t('server:stack.containers.empty')}</p>
            ) : (
              <>
                <label className="field stackscreen__field">
                  <span className="field__label">{t('server:stack.logs.select')}</span>
                  <select
                    className="field__input"
                    value={logContainer}
                    onChange={(event) => setChosen(event.target.value)}
                  >
                    <option value="">{t('server:stack.logs.choose')}</option>
                    {own.map((container) => (
                      <option key={container.id} value={container.name}>
                        {container.name}
                      </option>
                    ))}
                  </select>
                </label>
                {logContainer === '' ? (
                  <p className="serverscreen__message">{t('server:stack.logs.choose')}</p>
                ) : (
                  <LogsPanel
                    key={logContainer}
                    api={api}
                    serverId={server.id}
                    container={logContainer}
                    pollMs={pollMs}
                    onClose={() => setChosen('')}
                  />
                )}
              </>
            )}
          </section>
        </>
      )}
    </section>
  )
}
