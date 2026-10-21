import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { createAuthApi, type AccessApi } from '../api/auth'
import { ApiError, isSignedOut } from '../api/http'
import {
  AgentUpdateError,
  type AgentUpdateAllResult,
  type ServerSummary,
  type ServersApi,
} from '../api/servers'
import {
  createSystemApi,
  type SystemApi,
  type SystemVersion,
  type UpdateStatus,
} from '../api/system'
import { errorMessage } from '../auth/AuthGate'
import { ManualUpgrade } from './server/ManualUpgrade'
import { AccessCard } from './settings/AccessCard'

/** pollEvery is how often the progress of a running update is asked for. */
export const pollEvery = 3000
/** pendingShown is how many commits the card lists before summarising the rest. */
export const pendingShown = 10
/** logShown is how many of the last log lines the progress card shows. */
const logShown = 8

/** SettingsApi is the system API plus the optional agent update calls. */
export type SettingsApi = SystemApi & Partial<Pick<ServersApi, 'updateAgent' | 'updateAllAgents'>>

interface SettingsProps {
  /** api is injected by the tests; the panel builds its own otherwise. */
  api?: SettingsApi
  /** accessApi manages trusted devices and passkeys; the panel builds its own otherwise. */
  accessApi?: AccessApi
  /** pollMs overrides the polling interval, for the tests. */
  pollMs?: number
}

type View =
  | { status: 'loading' }
  | { status: 'checking' }
  | { status: 'ready'; system: SystemVersion }
  | { status: 'error'; message: string }

const defaultApi = createSystemApi()
const defaultAccessApi = createAuthApi()

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

/** agentUpdateErrorKey returns the settings key of a refused agent update. */
function agentUpdateErrorKey(error: unknown): string {
  if (!(error instanceof AgentUpdateError)) {
    return ''
  }
  switch (error.code) {
    case 'agent_offline':
      return 'servers.updateErrors.agent_offline'
    case 'no_update':
      return 'servers.updateErrors.no_update'
    case 'update_in_progress':
      return 'servers.updateErrors.update_in_progress'
    case 'session_required':
      return 'servers.updateErrors.session_required'
    case 'not_found':
      return 'servers.updateErrors.not_found'
    default:
      return 'servers.updateErrors.generic'
  }
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

/** NoteText renders a changelog bullet with only `code` spans, never HTML. */
function NoteText({ note }: { note: string }) {
  return (
    <>
      {note
        .split('`')
        .map((part, index) =>
          index % 2 === 1 ? <code key={index}>{part}</code> : <span key={index}>{part}</span>,
        )}
    </>
  )
}

/**
 * Settings shows the version of the panel and the available update. The update
 * button only queues the request: the host helper does the work, and the screen
 * follows its progress through the status of the API, so a reload resumes it.
 */
export function Settings({ api = defaultApi, accessApi, pollMs = pollEvery }: SettingsProps) {
  const { t, i18n } = useTranslation(['settings', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  // A test or an embedding may pass a system-only API. In production the
  // default system API means the panel also manages trusted devices and
  // passkeys.
  const access = accessApi ?? (api === defaultApi ? defaultAccessApi : undefined)
  const [view, setView] = useState<View>({ status: 'loading' })
  const [confirming, setConfirming] = useState(false)
  const [requesting, setRequesting] = useState(false)
  const [requested, setRequested] = useState(false)
  const [waiting, setWaiting] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [checkingNow, setCheckingNow] = useState(false)
  const [checkNotice, setCheckNotice] = useState<string | null>(null)
  const [notesExpanded, setNotesExpanded] = useState(false)
  const [servers, setServers] = useState<ServerSummary[] | null>(null)
  const [serversError, setServersError] = useState<string | null>(null)
  const [removing, setRemoving] = useState<ServerSummary | null>(null)
  const [confirmName, setConfirmName] = useState('')
  const [removeBusy, setRemoveBusy] = useState(false)
  const [removeError, setRemoveError] = useState<string | null>(null)

  const [updatingAgent, setUpdatingAgent] = useState<ServerSummary | null>(null)
  const [updatingAll, setUpdatingAll] = useState(false)
  const [updateBusy, setUpdateBusy] = useState(false)
  const [updateError, setUpdateError] = useState<string | null>(null)
  const [updateNotice, setUpdateNotice] = useState<string | null>(null)

  const showServers = api.listServers !== undefined
  const canUpdateAgent = api.updateAgent !== undefined
  const canUpdateAll = api.updateAllAgents !== undefined
  const updateAvailableCount = servers?.filter((server) => server.agentUpdateAvailable).length ?? 0

  useEffect(() => {
    if (!api.listServers) {
      return
    }
    let cancelled = false
    api.listServers().then(
      (list) => {
        if (cancelled) return
        setServers(list)
        setServersError(null)
      },
      (error: unknown) => {
        if (cancelled || isSignedOut(error)) return
        setServersError(errorMessage(t, error))
      },
    )
    return () => {
      cancelled = true
    }
  }, [api, t])

  useEffect(() => {
    let cancelled = false
    api.version().then(
      (system) => {
        if (cancelled) return
        setView({ status: 'ready', system })
        setWaiting(false)
        if (isFinal(system.status.state) || system.update.error === 'host_helper_missing') {
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
          if (isFinal(system.status.state) || system.update.error === 'host_helper_missing') {
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
    setCheckNotice(null)
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
            if (isFinal(system.status.state) || system.update.error === 'host_helper_missing') {
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
  const notes =
    update === null
      ? []
      : language.startsWith('es') || language.startsWith('ca')
        ? update.notes.es
        : update.notes.en
  const visibleNotes = notesExpanded ? notes : notes.slice(0, 8)

  const helperMissing = update?.error === 'host_helper_missing'
  const busy = requesting || requested || status?.state === 'running'
  const canUpdate =
    update !== null && update.updateAvailable && update.latest !== '' && !helperMissing && !busy

  const checkNow = () => {
    if (view.status !== 'ready' || checkingNow || busy) {
      return
    }
    setCheckingNow(true)
    setCheckNotice(null)
    setActionError(null)
    api.check().then(
      (system) => {
        setCheckingNow(false)
        setView({ status: 'ready', system })
        setWaiting(false)
        if (isFinal(system.status.state) || system.update.error === 'host_helper_missing') {
          setRequested(false)
        }
      },
      (error: unknown) => {
        setCheckingNow(false)
        if (isSignedOut(error)) return
        if (error instanceof ApiError && error.code === 'rate_limited') {
          setCheckNotice(t('settings:check.rateLimited'))
          return
        }
        setActionError(errorMessage(t, error))
      },
    )
  }

  const reloadServers = () => {
    const list = api.listServers
    if (!list) {
      return
    }
    list().then(
      (next) => {
        setServers(next)
        setServersError(null)
      },
      (error: unknown) => {
        if (isSignedOut(error)) return
        setServersError(errorMessage(t, error))
      },
    )
  }

  const startRemoval = (server: ServerSummary) => {
    setRemoving(server)
    setConfirmName('')
    setRemoveError(null)
    setUpdatingAgent(null)
    setUpdatingAll(false)
  }

  const cancelRemoval = () => {
    setRemoving(null)
    setConfirmName('')
    setRemoveError(null)
  }

  const confirmRemoval = () => {
    if (!removing || !api.removeServer || confirmName !== removing.name) {
      return
    }
    const target = removing
    setRemoveBusy(true)
    setRemoveError(null)
    api.removeServer(target.id).then(
      () => {
        setRemoveBusy(false)
        setRemoving(null)
        setConfirmName('')
        setServers((current) =>
          current === null ? current : current.filter((server) => server.id !== target.id),
        )
        reloadServers()
      },
      (error: unknown) => {
        setRemoveBusy(false)
        if (isSignedOut(error)) return
        setRemoveError(errorMessage(t, error))
      },
    )
  }

  const updateErrorMessage = (error: unknown): string => {
    const key = agentUpdateErrorKey(error)
    return key === '' ? errorMessage(t, error) : t(`settings:${key}`)
  }

  const startAgentUpdate = (server: ServerSummary) => {
    setUpdatingAgent(server)
    setUpdatingAll(false)
    setRemoving(null)
    setUpdateError(null)
    setUpdateNotice(null)
  }

  const startAllUpdates = () => {
    setUpdatingAll(true)
    setUpdatingAgent(null)
    setRemoving(null)
    setUpdateError(null)
    setUpdateNotice(null)
  }

  const cancelUpdate = () => {
    setUpdatingAgent(null)
    setUpdatingAll(false)
    setUpdateError(null)
  }

  const confirmAgentUpdate = () => {
    const updateAgent = api.updateAgent
    if (!updatingAgent || !updateAgent) {
      return
    }
    const target = updatingAgent
    setUpdateBusy(true)
    setUpdateError(null)
    updateAgent(target.id).then(
      () => {
        setUpdateBusy(false)
        setUpdatingAgent(null)
        setUpdateNotice(t('settings:servers.updateStarted'))
        reloadServers()
      },
      (error: unknown) => {
        setUpdateBusy(false)
        if (isSignedOut(error)) return
        setUpdateError(updateErrorMessage(error))
      },
    )
  }

  const confirmAllUpdates = () => {
    const updateAll = api.updateAllAgents
    if (!updatingAll || !updateAll) {
      return
    }
    setUpdateBusy(true)
    setUpdateError(null)
    updateAll().then(
      (result: AgentUpdateAllResult) => {
        setUpdateBusy(false)
        setUpdatingAll(false)
        setUpdateNotice(
          t('settings:servers.updateAllResult', {
            queued: result.queued.length,
            skipped: result.skipped.length,
          }),
        )
        reloadServers()
      },
      (error: unknown) => {
        setUpdateBusy(false)
        if (isSignedOut(error)) return
        setUpdateError(updateErrorMessage(error))
      },
    )
  }

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
                className="settings__button"
                disabled={checkingNow || busy}
                onClick={checkNow}
              >
                {checkingNow ? t('settings:check.checking') : t('settings:check.button')}
              </button>
              <button
                type="button"
                className="settings__button settings__button--primary"
                disabled={!canUpdate}
                onClick={() => setConfirming(true)}
              >
                {buttonText}
              </button>
            </div>

            {checkNotice !== null ? (
              <p className="settings__notice" role="status">
                {checkNotice}
              </p>
            ) : null}

            {actionError !== null ? (
              <p className="settings__message settings__message--error" role="alert">
                {actionError}
              </p>
            ) : null}
          </section>

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

          {access !== undefined ? <AccessCard api={access} /> : null}

          {showServers ? (
            <section className="settings__card" aria-labelledby="settings-servers-title">
              <div className="settings__card-head">
                <h2 className="settings__card-title" id="settings-servers-title">
                  {t('settings:servers.title')}
                </h2>
                {canUpdateAll && updateAvailableCount > 0 ? (
                  <button
                    type="button"
                    className="settings__button settings__server-update-all"
                    onClick={startAllUpdates}
                  >
                    {t('settings:servers.updateAll', { count: updateAvailableCount })}
                  </button>
                ) : null}
              </div>
              {serversError !== null ? (
                <p className="settings__message settings__message--error" role="alert">
                  {serversError}
                </p>
              ) : servers === null ? (
                <p className="settings__message" role="status">
                  {t('settings:servers.loading')}
                </p>
              ) : servers.length === 0 ? (
                <p className="settings__message">{t('settings:servers.empty')}</p>
              ) : (
                <ul className="settings__servers">
                  {servers.map((server) => (
                    <li className="settings__server" key={server.id}>
                      <span className="settings__server-name">{server.name}</span>
                      <span className="settings__server-agent">
                        {server.agentVersion === ''
                          ? t('settings:servers.agentUnknown')
                          : t('settings:servers.agentVersion', { version: server.agentVersion })}
                      </span>
                      <span
                        className={
                          server.connected
                            ? 'settings__server-state settings__server-state--online'
                            : 'settings__server-state'
                        }
                      >
                        {server.connected
                          ? t('settings:servers.connected')
                          : t('settings:servers.offline')}
                      </span>
                      {canUpdateAgent && server.agentUpdateAvailable ? (
                        <button
                          type="button"
                          className="settings__button settings__server-update"
                          onClick={() => startAgentUpdate(server)}
                        >
                          {t('settings:servers.update')}
                        </button>
                      ) : null}
                      <button
                        type="button"
                        className="settings__button settings__server-remove"
                        onClick={() => startRemoval(server)}
                      >
                        {t('settings:servers.remove')}
                      </button>
                      {server.agentUpdateManual ? (
                        <ManualUpgrade command={server.agentUpgradeCommand} />
                      ) : null}
                    </li>
                  ))}
                </ul>
              )}
              {updateNotice !== null ? (
                <p className="settings__notice" role="status">
                  {updateNotice}
                </p>
              ) : null}
              {updateError !== null && updatingAgent === null && !updatingAll ? (
                <p className="settings__message settings__message--error" role="alert">
                  {updateError}
                </p>
              ) : null}
            </section>
          ) : null}

          {update.pending.length > 0 || notes.length > 0 ? (
            <section className="settings__card" aria-labelledby="settings-pending-title">
              <h2 className="settings__card-title" id="settings-pending-title">
                {t('settings:pending.title')}
              </h2>
              {notes.length > 0 ? (
                <>
                  <h3 className="settings__section-title">{t('settings:pending.whatsNew')}</h3>
                  <ul className="settings__notes">
                    {visibleNotes.map((note, index) => (
                      <li key={`${index}-${note}`}>
                        <NoteText note={note} />
                      </li>
                    ))}
                  </ul>
                  {notes.length > 8 ? (
                    <button
                      type="button"
                      className="settings__button settings__notes-toggle"
                      aria-expanded={notesExpanded}
                      onClick={() => setNotesExpanded((value) => !value)}
                    >
                      {notesExpanded
                        ? t('settings:pending.showLess')
                        : t('settings:pending.showAll')}
                    </button>
                  ) : null}
                </>
              ) : null}
              {update.pending.length > 0 ? (
                <>
                  <h3 className="settings__section-title">{t('settings:pending.commits')}</h3>
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
                </>
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

      {updatingAgent !== null || updatingAll ? (
        <div className="settings__overlay">
          <div
            className="settings__dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="settings-agent-update-title"
          >
            <h2 className="settings__dialog-title" id="settings-agent-update-title">
              {updatingAll
                ? t('settings:servers.updateConfirm.titleAll')
                : t('settings:servers.updateConfirm.titleAgent', {
                    name: updatingAgent?.name ?? '',
                  })}
            </h2>
            <p className="settings__dialog-body">
              {updatingAll
                ? t('settings:servers.updateConfirm.bodyAll')
                : t('settings:servers.updateConfirm.bodyAgent')}
            </p>
            {updateError !== null ? (
              <p className="settings__message settings__message--error" role="alert">
                {updateError}
              </p>
            ) : null}
            <div className="settings__dialog-actions">
              <button
                type="button"
                className="settings__button settings__button--primary"
                disabled={updateBusy}
                onClick={updatingAll ? confirmAllUpdates : confirmAgentUpdate}
              >
                {updateBusy
                  ? t('settings:servers.updateConfirm.updating')
                  : updatingAll
                    ? t('settings:servers.updateConfirm.confirmAll')
                    : t('settings:servers.updateConfirm.confirmAgent')}
              </button>
              <button
                type="button"
                className="settings__button"
                disabled={updateBusy}
                onClick={cancelUpdate}
              >
                {t('settings:servers.updateConfirm.cancel')}
              </button>
            </div>
          </div>
        </div>
      ) : null}

      {removing !== null ? (
        <div className="settings__overlay">
          <div
            className="settings__dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="settings-remove-title"
          >
            <h2 className="settings__dialog-title" id="settings-remove-title">
              {t('settings:servers.confirm.title', { name: removing.name })}
            </h2>
            <p className="settings__dialog-body">{t('settings:servers.confirm.effect')}</p>
            <p className="settings__dialog-body">{t('settings:servers.confirm.reinstall')}</p>
            <p className="settings__dialog-body">
              {t('settings:servers.confirm.prompt', { name: removing.name })}
            </p>
            <label className="settings__field" htmlFor="settings-remove-name">
              <span>{t('settings:servers.confirm.name')}</span>
              <input
                id="settings-remove-name"
                className="settings__input"
                type="text"
                autoComplete="off"
                value={confirmName}
                onChange={(event) => setConfirmName(event.target.value)}
              />
            </label>
            {removeError !== null ? (
              <p className="settings__message settings__message--error" role="alert">
                {removeError}
              </p>
            ) : null}
            <div className="settings__dialog-actions">
              <button
                type="button"
                className="settings__button settings__button--danger"
                disabled={confirmName !== removing.name || removeBusy}
                onClick={confirmRemoval}
              >
                {removeBusy
                  ? t('settings:servers.confirm.removing')
                  : t('settings:servers.confirm.confirm')}
              </button>
              <button
                type="button"
                className="settings__button"
                disabled={removeBusy}
                onClick={cancelRemoval}
              >
                {t('settings:servers.confirm.cancel')}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </section>
  )
}
