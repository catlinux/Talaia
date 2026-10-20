import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ApiError, isSignedOut } from '../api/http'
import {
  createSystemApi,
  type SystemApi,
  type SystemVersion,
  type UpdateStatus,
} from '../api/system'
import { errorMessage } from '../auth/AuthGate'

/** pollEvery is how often the progress of a running update is asked for. */
export const pollEvery = 3000
/** pendingShown is how many commits the card lists before summarising the rest. */
export const pendingShown = 10
/** logShown is how many of the last log lines the progress card shows. */
const logShown = 8

interface SettingsProps {
  /** api is injected by the tests; the panel builds its own otherwise. */
  api?: SystemApi
  /** pollMs overrides the polling interval, for the tests. */
  pollMs?: number
}

type View =
  | { status: 'loading' }
  | { status: 'checking' }
  | { status: 'ready'; system: SystemVersion }
  | { status: 'error'; message: string }

const defaultApi = createSystemApi()

/** shortSHA returns the seven characters the panel shows of a commit. */
function shortSHA(sha: string): string {
  return sha.slice(0, 7)
}

/** isFinal reports whether the host helper finished, one way or another. */
function isFinal(state: UpdateStatus['state']): boolean {
  return state === 'succeeded' || state === 'failed' || state === 'rolled_back'
}

/** restarting reports whether a failed call may be the core restarting. */
function restarting(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 502 || error.status === 503 || error.code === 'network')
  )
}

function updateErrorKey(code: string): string {
  switch (code) {
    case 'host_helper_missing':
      return 'updateError.hostHelperMissing'
    case 'rate_limited':
      return 'updateError.rateLimited'
    case 'unreachable':
      return 'updateError.unreachable'
    default:
      return 'updateError.generic'
  }
}

/** formatChecked writes the last check with the date and the time of the locale. */
function formatChecked(value: string, language: string, never: string): string {
  if (value === '') {
    return never
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return never
  }
  return new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

/**
 * Settings shows the version of the panel and the available update. The update
 * button only queues the request: the host helper does the work, and the screen
 * follows its progress through the status of the API, so a reload resumes it.
 */
export function Settings({ api = defaultApi, pollMs = pollEvery }: SettingsProps) {
  const { t, i18n } = useTranslation(['settings', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [view, setView] = useState<View>({ status: 'loading' })
  const [confirming, setConfirming] = useState(false)
  const [requesting, setRequesting] = useState(false)
  const [requested, setRequested] = useState(false)
  const [waiting, setWaiting] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    api.version().then(
      (system) => {
        if (cancelled) return
        setView({ status: 'ready', system })
        setWaiting(false)
        if (isFinal(system.status.state)) {
          setRequested(false)
        }
      },
      (error: unknown) => {
        if (cancelled || isSignedOut(error)) return
        // The first check has not finished yet: there is nothing to report.
        if (error instanceof ApiError && error.code === 'update_check_pending') {
          setView({ status: 'checking' })
          return
        }
        if (restarting(error)) {
          setView({ status: 'checking' })
          setWaiting(true)
          return
        }
        setView({ status: 'error', message: errorMessage(t, error) })
      },
    )
    return () => {
      cancelled = true
    }
  }, [api, t])

  const status = view.status === 'ready' ? view.system.status : null
  const polling =
    view.status === 'checking' || (status !== null && (status.state === 'running' || requested))

  useEffect(() => {
    if (!polling) {
      return
    }

    let cancelled = false
    const timer = setInterval(() => {
      api.version().then(
        (system) => {
          if (cancelled) return
          setView({ status: 'ready', system })
          setWaiting(false)
          if (isFinal(system.status.state)) {
            setRequested(false)
          }
        },
        (error: unknown) => {
          if (cancelled || isSignedOut(error)) return
          if (restarting(error)) {
            setWaiting(true)
            return
          }
          setView({ status: 'error', message: errorMessage(t, error) })
        },
      )
    }, pollMs)

    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [api, pollMs, polling, t])

  const requestUpdate = () => {
    if (view.status !== 'ready') {
      return
    }
    const target = view.system.update.latest
    setConfirming(false)
    setActionError(null)
    setRequesting(true)
    setRequested(true)

    api.update(target).then(
      () => {
        setRequesting(false)
        // Ask once now; the polling effect keeps asking until the helper ends.
        api.version().then(
          (system) => {
            setView({ status: 'ready', system })
            setWaiting(false)
            if (isFinal(system.status.state)) {
              setRequested(false)
            }
          },
          (error: unknown) => {
            if (isSignedOut(error)) return
            setWaiting(true)
          },
        )
      },
      (error: unknown) => {
        setRequesting(false)
        setRequested(false)
        if (isSignedOut(error)) return
        setActionError(errorMessage(t, error))
      },
    )
  }

  const system = view.status === 'ready' ? view.system : null
  const update = system?.update ?? null

  const helperMissing = update?.error === 'host_helper_missing'
  const busy = requesting || requested || status?.state === 'running'
  const canUpdate =
    update !== null && update.updateAvailable && update.latest !== '' && !helperMissing && !busy

  let stateText: string | null = null
  if (update !== null) {
    if (update.error !== '') {
      stateText = t(`settings:${updateErrorKey(update.error)}`)
    } else if (update.updateAvailable) {
      stateText =
        update.pending.length > 0
          ? t('settings:state.pending', { count: update.pending.length })
          : t('settings:state.available')
    } else if (update.latest === '') {
      stateText = t('settings:state.noGreen')
    } else {
      stateText = t('settings:state.upToDate')
    }
  }

  const shown = update?.pending.slice(0, pendingShown) ?? []
  const more = (update?.pending.length ?? 0) - shown.length
  const showProgress =
    status !== null && (status.state !== 'idle' || requested || waiting) && system !== null

  let progressText = ''
  if (status !== null && system !== null) {
    if (waiting) {
      progressText = t('settings:progress.restarting')
    } else if (status.state === 'running') {
      progressText = t('settings:progress.running')
    } else if (status.state === 'succeeded') {
      progressText = t('settings:progress.succeeded', { version: system.version })
    } else if (status.state === 'failed') {
      progressText = t('settings:progress.failed')
    } else if (status.state === 'rolled_back') {
      progressText = t('settings:progress.rolledBack')
    } else if (status.state === 'unknown') {
      progressText = t('settings:progress.unknown')
    } else {
      progressText = t('settings:progress.queued')
    }
  }

  const progressTone =
    status?.state === 'succeeded'
      ? ' settings__progress--ok'
      : status?.state === 'failed' || status?.state === 'rolled_back'
        ? ' settings__progress--error'
        : ''

  let buttonText = t('settings:update.none')
  if (update !== null && update.latest !== '') {
    buttonText = busy
      ? t('settings:update.busy')
      : t('settings:update.to', { sha: shortSHA(update.latest) })
  }

  return (
    <section className="settings" aria-labelledby="settings-title">
      <header className="settings__head">
        <h1 className="settings__title" id="settings-title">
          {t('settings:title')}
        </h1>
        <p className="settings__subtitle">{t('settings:subtitle')}</p>
      </header>

      {waiting ? (
        <p className="settings__notice" role="status">
          {t('settings:progress.restarting')}
        </p>
      ) : null}

      {view.status === 'loading' || view.status === 'checking' ? (
        <p className="settings__message" role="status">
          {t('settings:state.checking')}
        </p>
      ) : null}

      {view.status === 'error' ? (
        <p className="settings__message settings__message--error" role="alert">
          {view.message}
        </p>
      ) : null}

      {system !== null && update !== null ? (
        <>
          <section className="settings__card" aria-labelledby="settings-version-title">
            <h2 className="settings__card-title" id="settings-version-title">
              {t('settings:version.title')}
            </h2>

            <dl className="settings__facts">
              <div className="settings__fact">
                <dt>{t('settings:version.current')}</dt>
                <dd>{system.version}</dd>
              </div>
              <div className="settings__fact">
                <dt>{t('settings:version.commit')}</dt>
                <dd>
                  {system.commit === '' ? t('settings:version.unknown') : shortSHA(system.commit)}
                </dd>
              </div>
              <div className="settings__fact">
                <dt>{t('settings:version.checked')}</dt>
                <dd>{formatChecked(update.checkedAt, language, t('settings:version.never'))}</dd>
              </div>
            </dl>

            <div className="settings__meta">
              {stateText !== null ? (
                <span
                  className={
                    update.error !== ''
                      ? 'settings__state settings__state--error'
                      : 'settings__state'
                  }
                >
                  {stateText}
                </span>
              ) : null}
              {update.latest !== '' ? (
                <>
                  <span className={`settings__badge settings__badge--${update.ci}`}>
                    {t('settings:ci.label')} {t(`settings:ci.${update.ci}`)}
                  </span>
                  <span className="settings__latest">
                    {t('settings:version.latest', { sha: shortSHA(update.latest) })}
                  </span>
                </>
              ) : null}
            </div>

            <div className="settings__actions">
              <button
                type="button"
                className="settings__button settings__button--primary"
                disabled={!canUpdate}
                onClick={() => setConfirming(true)}
              >
                {buttonText}
              </button>
            </div>

            {actionError !== null ? (
              <p className="settings__message settings__message--error" role="alert">
                {actionError}
              </p>
            ) : null}
          </section>

          {update.pending.length > 0 ? (
            <section className="settings__card" aria-labelledby="settings-pending-title">
              <h2 className="settings__card-title" id="settings-pending-title">
                {t('settings:pending.title')}
              </h2>
              <ul className="settings__pending">
                {shown.map((commit) => (
                  <li className="settings__commit" key={commit.sha}>
                    <code>{shortSHA(commit.sha)}</code>
                    <span className="settings__commit-subject">{commit.subject}</span>
                    <span className="settings__commit-author">{commit.author}</span>
                  </li>
                ))}
              </ul>
              {more > 0 ? (
                <p className="settings__more">{t('settings:pending.more', { count: more })}</p>
              ) : null}
            </section>
          ) : null}

          {showProgress && status !== null ? (
            <section
              className={`settings__card settings__progress${progressTone}`}
              aria-labelledby="settings-progress-title"
            >
              <h2 className="settings__card-title" id="settings-progress-title">
                {t('settings:progress.title')}
              </h2>
              <p className="settings__progress-state" role="status">
                {progressText}
              </p>
              {status.log.length > 0 ? (
                <ul className="settings__log" aria-label={t('settings:progress.log')}>
                  {status.log.slice(-logShown).map((line, index) => (
                    <li key={`${index}-${line}`}>{line}</li>
                  ))}
                </ul>
              ) : null}
            </section>
          ) : null}
        </>
      ) : null}

      {confirming && update !== null ? (
        <div className="settings__overlay">
          <div
            className="settings__dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="settings-confirm-title"
          >
            <h2 className="settings__dialog-title" id="settings-confirm-title">
              {t('settings:confirm.title')}
            </h2>
            <p className="settings__dialog-body">
              {t('settings:confirm.body', { sha: shortSHA(update.latest) })}
            </p>
            <p className="settings__dialog-body">{t('settings:confirm.backup')}</p>
            <div className="settings__dialog-actions">
              <button
                type="button"
                className="settings__button settings__button--primary"
                onClick={requestUpdate}
              >
                {t('settings:confirm.yes')}
              </button>
              <button
                type="button"
                className="settings__button"
                onClick={() => setConfirming(false)}
              >
                {t('settings:confirm.no')}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </section>
  )
}
