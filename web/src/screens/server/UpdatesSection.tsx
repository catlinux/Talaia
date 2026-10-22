import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { type AptApi, type AptStatus, parseAptStatus } from '../../api/apt'
import { isSignedOut } from '../../api/http'
import { errorMessage } from '../../auth/AuthGate'
import { formatRelativeTime } from '../fleet/format'
import { TimedOut, waitForJob } from './waitForJob'

/** refreshTimeout is how long a refresh may take: apt-get update can be slow. */
const refreshTimeout = 10 * 60_000

interface UpdatesSectionProps {
  aptApi: AptApi
  serverId: string
  pollMs: number
}

type StatusState =
  | { status: 'loading' }
  | { status: 'ready'; data: AptStatus }
  | { status: 'error'; message: string }

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
 * UpdatesSection reads the pending apt updates of a server and lists them. It
 * only reads: installing updates and rebooting belong to a later step. The
 * section asks once on mount and again when the person presses "Check now".
 */
export function UpdatesSection({ aptApi, serverId, pollMs }: UpdatesSectionProps) {
  const { t, i18n } = useTranslation(['server', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [state, setState] = useState<StatusState>({ status: 'loading' })
  const [refreshing, setRefreshing] = useState(false)
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

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
    // Keep the last list on screen while the refresh runs; a retry after an
    // error starts from the loading state.
    setRefreshing(true)
    setState((current) => (current.status === 'ready' ? current : { status: 'loading' }))
    void load(true)
  }

  const data = state.status === 'ready' ? state.data : null
  const packages = data ? sortPackages(data.packages) : []
  const securityCount = packages.filter((pkg) => pkg.security).length
  const busy = refreshing || state.status === 'loading'

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
        <button
          type="button"
          className="cbtn cbtn--primary updates__check"
          disabled={busy}
          onClick={checkNow}
        >
          {refreshing ? t('server:updates.checking') : t('server:updates.check')}
        </button>
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
