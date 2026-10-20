import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { ContainersApi } from '../../api/containers'
import { isSignedOut } from '../../api/http'
import { errorMessage } from '../../auth/AuthGate'
import { CloseIcon, RestartIcon } from '../../components/icons'
import { TimedOut, waitForJob } from './waitForJob'

interface LogsPanelProps {
  api: ContainersApi
  serverId: string
  container: string
  pollMs: number
  onClose: () => void
}

type LogsState =
  { status: 'loading' } | { status: 'ready'; text: string } | { status: 'error'; message: string }

/**
 * LogsPanel shows the last lines of a container. Reading them is a job like any
 * other: the panel asks, follows it and shows the output.
 */
export function LogsPanel({ api, serverId, container, pollMs, onClose }: LogsPanelProps) {
  const { t } = useTranslation(['server', 'errors'])
  const [state, setState] = useState<LogsState>({ status: 'loading' })
  const alive = useRef(true)
  const viewer = useRef<HTMLPreElement>(null)

  // fetchLogs asks for the lines and shows them. It does not touch the state until
  // the answer arrives, so an effect can call it.
  const fetchLogs = useCallback(async () => {
    try {
      const jobId = await api.logs(serverId, container)
      const job = await waitForJob(api, jobId, pollMs, () => alive.current)
      if (job === null || !alive.current) return
      setState(
        job.state === 'succeeded'
          ? { status: 'ready', text: job.output }
          : { status: 'error', message: job.message },
      )
    } catch (error) {
      if (!alive.current || isSignedOut(error)) return
      setState({
        status: 'error',
        message: error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error),
      })
    }
  }, [api, container, pollMs, serverId, t])

  useEffect(() => {
    alive.current = true
    void fetchLogs()
    return () => {
      alive.current = false
    }
  }, [fetchLogs])

  const refresh = () => {
    setState({ status: 'loading' })
    void fetchLogs()
  }

  // The newest lines are at the bottom: show them.
  useEffect(() => {
    if (state.status === 'ready' && viewer.current) {
      viewer.current.scrollTop = viewer.current.scrollHeight
    }
  }, [state])

  const title = t('server:logs.title', { name: container })

  return (
    <section className="logs" aria-label={title}>
      <header className="logs__head">
        <h4 className="logs__title">{title}</h4>
        <div className="logs__actions">
          <button
            type="button"
            className="cbtn"
            disabled={state.status === 'loading'}
            onClick={refresh}
          >
            <RestartIcon />
            {t('server:logs.refresh')}
          </button>
          <button type="button" className="cbtn" onClick={onClose}>
            <CloseIcon />
            {t('server:logs.close')}
          </button>
        </div>
      </header>

      {state.status === 'loading' ? (
        <p className="logs__message" role="status">
          {t('server:logs.loading')}
        </p>
      ) : null}
      {state.status === 'error' ? (
        <p className="logs__message logs__message--error" role="alert">
          {t('server:progress.failed', { message: state.message })}
        </p>
      ) : null}
      {state.status === 'ready' && state.text.trim() === '' ? (
        <p className="logs__message">{t('server:logs.empty')}</p>
      ) : null}
      {state.status === 'ready' && state.text.trim() !== '' ? (
        <>
          <pre className="logs__text" ref={viewer} tabIndex={0}>
            {state.text}
          </pre>
          <p className="logs__note">{t('server:logs.note')}</p>
        </>
      ) : null}
    </section>
  )
}
