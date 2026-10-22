import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { type AptApi, type AptStatus, type AptUpgradeMode, parseAptStatus } from '../../api/apt'
import { isSignedOut } from '../../api/http'
import { errorMessage } from '../../auth/AuthGate'
import { formatRelativeTime } from '../fleet/format'
import { TimedOut, waitForJob } from './waitForJob'

/** refreshTimeout is how long a refresh may take: apt-get update can be slow. */
const refreshTimeout = 10 * 60_000
/** upgradeTimeout is how long an apt upgrade may take. */
const upgradeTimeout = 60 * 60_000

/** dockerPackageNames are the packages whose upgrade restarts the containers. */
const dockerPackageNames = new Set([
  'docker-ce',
  'docker.io',
  'containerd',
  'containerd.io',
  'docker-compose-plugin',
])

interface UpdatesSectionProps {
  aptApi: AptApi
  serverId: string
  hostname: string
  pollMs: number
}

type StatusState =
  | { status: 'loading' }
  | { status: 'ready'; data: AptStatus }
  | { status: 'error'; message: string }

type DialogState = { kind: 'upgrade'; mode: AptUpgradeMode } | { kind: 'reboot' } | null

interface UpgradeRun {
  mode: AptUpgradeMode
  state: 'running' | 'succeeded' | 'failed'
  message: string
  log: string
}

interface TimeInfo {
  iso: string
  relative: string
  absolute: string
}

/** timeOf formats one timestamp relative to now, or returns null when unknown. */
function timeOf(value: string | undefined, language: string): TimeInfo | null {
  if (!value) return null
  const at = new Date(value)
  if (Number.isNaN(at.getTime())) return null
  return {
    iso: value,
    relative: formatRelativeTime(at, new Date(), language),
    absolute: new Intl.DateTimeFormat(language, {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(at),
  }
}

/** sortPackages puts the security updates first and then orders by name. */
function sortPackages(packages: AptStatus['packages']): AptStatus['packages'] {
  return [...packages].sort((left, right) => {
    if (left.security !== right.security) {
      return left.security ? -1 : 1
    }
    return left.name.localeCompare(right.name)
  })
}

/**
 * UpdatesSection reads the pending apt updates of a server and runs the
 * confirmed upgrade and reboot operations. The section asks for the list once
 * on mount and again after every operation.
 */
export function UpdatesSection({ aptApi, serverId, hostname, pollMs }: UpdatesSectionProps) {
  const { t, i18n } = useTranslation(['server', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [state, setState] = useState<StatusState>({ status: 'loading' })
  const [refreshing, setRefreshing] = useState(false)
  const [dialog, setDialog] = useState<DialogState>(null)
  const [hostnameInput, setHostnameInput] = useState('')
  const [upgrade, setUpgrade] = useState<UpgradeRun | null>(null)
  const [upgradeBusy, setUpgradeBusy] = useState(false)
  const [rebootBusy, setRebootBusy] = useState(false)
  const [rebooting, setRebooting] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const alive = useRef(true)
  const logRef = useRef<HTMLPreElement | null>(null)

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  // Keep the live log at its end while the agent appends lines.
  useEffect(() => {
    const node = logRef.current
    if (node !== null) {
      node.scrollTop = node.scrollHeight
    }
  }, [upgrade?.log])

  const load = useCallback(
    async (refresh: boolean) => {
      try {
        const jobId = await aptApi.status(serverId, refresh)
        const job = await waitForJob(
          aptApi,
          jobId,
          pollMs,
          () => alive.current,
          refresh ? refreshTimeout : undefined,
        )
        if (job === null || !alive.current) return
        if (job.state !== 'succeeded') {
          setState({
            status: 'error',
            message: job.message || t('server:updates.invalid'),
          })
          return
        }
        let data: AptStatus
        try {
          data = parseAptStatus(job.output)
        } catch {
          setState({ status: 'error', message: t('server:updates.invalid') })
          return
        }
        setState({ status: 'ready', data })
      } catch (error) {
        if (!alive.current || isSignedOut(error)) return
        setState({
          status: 'error',
          message:
            error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error),
        })
      } finally {
        if (alive.current) setRefreshing(false)
      }
    },
    [aptApi, pollMs, serverId, t],
  )

  useEffect(() => {
    void load(false)
  }, [load])

  const checkNow = () => {
    setActionError(null)
    // Keep the last list on screen while the refresh runs; a retry after an
    // error starts from the loading state.
    setRefreshing(true)
    setState((current) => (current.status === 'ready' ? current : { status: 'loading' }))
    void load(true)
  }

  const openUpgrade = (mode: AptUpgradeMode) => {
    setActionError(null)
    setDialog({ kind: 'upgrade', mode })
  }

  const openReboot = () => {
    setActionError(null)
    setHostnameInput('')
    setDialog({ kind: 'reboot' })
  }

  const startUpgrade = async (mode: AptUpgradeMode) => {
    setDialog(null)
    setActionError(null)
    setUpgrade({ mode, state: 'running', message: '', log: '' })
    setUpgradeBusy(true)
    let started = false
    try {
      const jobId = await aptApi.upgrade(serverId, mode)
      started = true
      const job = await waitForJob(
        aptApi,
        jobId,
        pollMs,
        () => alive.current,
        upgradeTimeout,
        (progress) => {
          if (!alive.current) return
          setUpgrade((current) =>
            current && current.state === 'running' ? { ...current, log: progress.output } : current,
          )
        },
      )
      if (job === null || !alive.current) return
      setUpgrade((current) => ({
        mode,
        state: job.state === 'succeeded' ? 'succeeded' : 'failed',
        message: job.state === 'succeeded' ? '' : job.message || t('server:updates.invalid'),
        log: job.output || current?.log || '',
      }))
      await load(false)
    } catch (error) {
      if (!alive.current || isSignedOut(error)) return
      if (!started) {
        setUpgrade(null)
        setActionError(errorMessage(t, error))
        return
      }
      const message =
        error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error)
      setUpgrade((current) =>
        current
          ? { ...current, state: 'failed', message }
          : { mode, state: 'failed', message, log: '' },
      )
      await load(false)
    } finally {
      if (alive.current) setUpgradeBusy(false)
    }
  }

  const startReboot = async () => {
    setDialog(null)
    setActionError(null)
    setRebootBusy(true)
    try {
      // The API accepting the order is enough: the agent answers it and the
      // stream is expected to drop while the machine restarts.
      await aptApi.reboot(serverId, hostname.trim())
      if (alive.current) setRebooting(true)
    } catch (error) {
      if (!alive.current || isSignedOut(error)) return
      setActionError(
        error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error),
      )
    } finally {
      if (alive.current) setRebootBusy(false)
    }
  }

  const data = state.status === 'ready' ? state.data : null
  const packages = data ? sortPackages(data.packages) : []
  const securityCount = packages.filter((pkg) => pkg.security).length
  const dockerUpdates = packages.filter((pkg) => dockerPackageNames.has(pkg.name))
  const expectedHostname = hostname.trim()
  const hostnameMatches =
    expectedHostname !== '' && hostnameInput.trim().toLowerCase() === expectedHostname.toLowerCase()
  const busy = refreshing || state.status === 'loading' || upgradeBusy || rebootBusy || rebooting

  return (
    <section className="updates" aria-labelledby="updates-title" aria-busy={busy}>
      <header className="stacks__head">
        <div>
          <h2 className="stacks__title" id="updates-title">
            {t('server:updates.title')}
          </h2>
          <p className="stacks__intro">{t('server:updates.intro')}</p>
          {data ? (
            <p className="updates__summary">
              {t('server:updates.summary', {
                count: packages.length,
                security: securityCount,
              })}
            </p>
          ) : null}
        </div>
        <div className="updates__actions">
          <button type="button" className="cbtn cbtn--primary" disabled={busy} onClick={checkNow}>
            {refreshing ? t('server:updates.checking') : t('server:updates.check')}
          </button>
          {data && securityCount > 0 ? (
            <button
              type="button"
              className="cbtn"
              disabled={busy}
              onClick={() => openUpgrade('security')}
            >
              {t('server:updates.upgrade.securityButton', { count: securityCount })}
            </button>
          ) : null}
          {data && packages.length > 0 ? (
            <button
              type="button"
              className="cbtn"
              disabled={busy}
              onClick={() => openUpgrade('all')}
            >
              {t('server:updates.upgrade.allButton', { count: packages.length })}
            </button>
          ) : null}
          <button
            type="button"
            className="cbtn updates__reboot-open"
            disabled={busy}
            onClick={openReboot}
          >
            {t('server:updates.reboot.open')}
          </button>
        </div>
      </header>

      <div className="updates__body">
        {state.status === 'loading' ? (
          <p className="updates__message" role="status">
            {refreshing ? t('server:updates.checking') : t('server:updates.loading')}
          </p>
        ) : null}
        {state.status === 'error' ? (
          <p className="updates__message updates__message--error" role="alert">
            {t('server:updates.error', { message: state.message })}
          </p>
        ) : null}
        {refreshing && data ? (
          <p className="updates__message" role="status">
            {t('server:updates.checking')}
          </p>
        ) : null}
        {actionError !== null ? (
          <p className="updates__message updates__message--error" role="alert">
            {actionError}
          </p>
        ) : null}
        {rebootBusy ? (
          <p className="updates__message" role="status">
            {t('server:updates.reboot.sending')}
          </p>
        ) : null}
        {rebooting ? (
          <p className="updates__notice" role="status">
            {t('server:updates.reboot.running')}
          </p>
        ) : null}

        {upgrade !== null ? (
          <div className={`updates__progress updates__progress--${upgrade.state}`}>
            <p
              className={
                upgrade.state === 'failed'
                  ? 'updates__message updates__message--error'
                  : 'updates__message'
              }
              role={upgrade.state === 'failed' ? 'alert' : 'status'}
            >
              {upgrade.state === 'running'
                ? t('server:updates.upgrade.running')
                : upgrade.state === 'succeeded'
                  ? t('server:updates.upgrade.succeeded')
                  : t('server:updates.upgrade.failed', { message: upgrade.message })}
            </p>
            <pre
              className="updates__log"
              ref={logRef}
              role="log"
              aria-label={t('server:updates.upgrade.log')}
            >
              {upgrade.log ||
                (upgrade.state === 'running'
                  ? t('server:updates.upgrade.waiting')
                  : t('server:updates.upgrade.emptyLog'))}
            </pre>
          </div>
        ) : null}

        {data ? (
          <>
            <div className="updates__times">
              <Time
                label={t('server:updates.checked')}
                value={timeOf(data.checkedAt, language)}
                unknown={t('server:updates.unknown')}
              />
              <Time
                label={t('server:updates.listed')}
                value={timeOf(data.listsUpdatedAt, language)}
                unknown={t('server:updates.unknown')}
              />
            </div>

            {data.rebootRequired ? (
              <div className="updates__reboot">
                <p className="updates__reboot-title">{t('server:updates.rebootTitle')}</p>
                <p className="updates__reboot-body">{t('server:updates.rebootBody')}</p>
                {data.rebootReasons.length > 0 ? (
                  <ul className="updates__reboot-reasons">
                    {data.rebootReasons.map((reason) => (
                      <li key={reason}>{reason}</li>
                    ))}
                  </ul>
                ) : null}
                <div className="updates__reboot-actions">
                  <button
                    type="button"
                    className="cbtn cbtn--danger"
                    disabled={busy}
                    onClick={openReboot}
                  >
                    {t('server:updates.reboot.now')}
                  </button>
                </div>
              </div>
            ) : null}

            {packages.length === 0 ? (
              <p className="updates__empty">{t('server:updates.empty')}</p>
            ) : (
              <div className="updates__tablewrap">
                <table className="updates__table">
                  <thead>
                    <tr>
                      <th scope="col">{t('server:updates.columns.package')}</th>
                      <th scope="col">{t('server:updates.columns.version')}</th>
                      <th scope="col">{t('server:updates.columns.origin')}</th>
                      <th scope="col">{t('server:updates.columns.security')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {packages.map((pkg) => (
                      <tr key={pkg.name}>
                        <td>
                          <span className="updates__package">{pkg.name}</span>
                        </td>
                        <td className="updates__version">
                          {pkg.current === '' ? (
                            <>
                              <span className="updates__new">{t('server:updates.new')}</span>
                              <span aria-hidden="true"> → </span>
                            </>
                          ) : (
                            <>
                              {pkg.current}
                              <span aria-hidden="true"> → </span>
                            </>
                          )}
                          {pkg.candidate || t('server:updates.unknown')}
                        </td>
                        <td className="updates__origin">
                          {pkg.origin || t('server:updates.unknown')}
                        </td>
                        <td>
                          {pkg.security ? (
                            <span className="updates__security">
                              {t('server:updates.securityBadge')}
                            </span>
                          ) : (
                            <span className="updates__none" aria-hidden="true">
                              —
                            </span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            {data.held.length > 0 ? (
              <details className="updates__held">
                <summary>{t('server:updates.held', { count: data.held.length })}</summary>
                <p className="updates__held-hint">{t('server:updates.heldHint')}</p>
                <ul className="updates__held-list">
                  {data.held.map((name) => (
                    <li key={name}>{name}</li>
                  ))}
                </ul>
              </details>
            ) : null}
          </>
        ) : null}
      </div>

      {dialog !== null ? (
        <div className="settings__overlay">
          <div
            className="settings__dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="updates-dialog-title"
          >
            {dialog.kind === 'upgrade' ? (
              <>
                <h2 className="settings__dialog-title" id="updates-dialog-title">
                  {dialog.mode === 'security'
                    ? t('server:updates.upgrade.securityTitle')
                    : t('server:updates.upgrade.allTitle')}
                </h2>
                <p className="settings__dialog-body">{t('server:updates.upgrade.body')}</p>
                {dialog.mode === 'all' && dockerUpdates.length > 0 ? (
                  <p className="updates__warning">
                    {t('server:updates.upgrade.dockerWarning', {
                      packages: dockerUpdates.map((pkg) => pkg.name).join(', '),
                    })}
                  </p>
                ) : null}
                {data && data.held.length > 0 ? (
                  <p className="updates__dialog-note">
                    {t('server:updates.upgrade.heldNote', { count: data.held.length })}
                  </p>
                ) : null}
                <div className="settings__dialog-actions">
                  <button
                    type="button"
                    className="settings__button settings__button--primary"
                    onClick={() => void startUpgrade(dialog.mode)}
                  >
                    {t('server:updates.upgrade.confirm')}
                  </button>
                  <button
                    type="button"
                    className="settings__button"
                    onClick={() => setDialog(null)}
                  >
                    {t('server:updates.cancel')}
                  </button>
                </div>
              </>
            ) : (
              <>
                <h2 className="settings__dialog-title" id="updates-dialog-title">
                  {t('server:updates.reboot.title')}
                </h2>
                <p className="settings__dialog-body">{t('server:updates.reboot.body')}</p>
                {data && data.rebootReasons.length > 0 ? (
                  <>
                    <p className="updates__dialog-note">{t('server:updates.reboot.reasons')}</p>
                    <ul className="updates__dialog-reasons">
                      {data.rebootReasons.map((reason) => (
                        <li key={reason}>{reason}</li>
                      ))}
                    </ul>
                  </>
                ) : null}
                <label className="updates__field">
                  <span className="updates__field-label">
                    {t('server:updates.reboot.hostnameLabel')}
                  </span>
                  <input
                    className="updates__input"
                    type="text"
                    value={hostnameInput}
                    onChange={(event) => setHostnameInput(event.target.value)}
                    autoComplete="off"
                    spellCheck={false}
                  />
                </label>
                <p className="updates__hint">
                  {t('server:updates.reboot.hostnameHint', {
                    hostname: expectedHostname || t('server:updates.unknown'),
                  })}
                </p>
                <div className="settings__dialog-actions">
                  <button
                    type="button"
                    className="settings__button settings__button--danger"
                    disabled={!hostnameMatches || busy}
                    onClick={() => void startReboot()}
                  >
                    {t('server:updates.reboot.confirm')}
                  </button>
                  <button
                    type="button"
                    className="settings__button"
                    onClick={() => setDialog(null)}
                  >
                    {t('server:updates.cancel')}
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      ) : null}
    </section>
  )
}

function Time({
  label,
  value,
  unknown,
}: {
  label: string
  value: TimeInfo | null
  unknown: string
}) {
  return (
    <p className="updates__time">
      <span className="updates__time-label">{label}</span>
      {value ? (
        <>
          <time className="updates__time-relative" dateTime={value.iso}>
            {value.relative}
          </time>
          <span className="updates__time-absolute">{value.absolute}</span>
        </>
      ) : (
        <span className="updates__time-absolute">{unknown}</span>
      )}
    </p>
  )
}
