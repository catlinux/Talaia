import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { isSignedOut } from '../../api/http'
import type { Recording, RecordingsApi } from '../../api/recordings'
import { errorMessage } from '../../auth/AuthGate'

interface RecordingsTabProps {
  api: RecordingsApi
}

function formatDuration(seconds: number, language: string): string {
  const safe = Math.max(0, Math.trunc(seconds))
  if (safe < 60) {
    return new Intl.NumberFormat(language, {
      style: 'unit',
      unit: 'second',
      unitDisplay: 'short',
    }).format(safe)
  }
  const hours = Math.floor(safe / 3600)
  const minutes = Math.floor((safe % 3600) / 60)
  const rest = safe % 60
  if (hours > 0) {
    return `${new Intl.NumberFormat(language, { style: 'unit', unit: 'hour', unitDisplay: 'short' }).format(hours)} ${new Intl.NumberFormat(language, { style: 'unit', unit: 'minute', unitDisplay: 'short' }).format(minutes)}`
  }
  return `${new Intl.NumberFormat(language, { style: 'unit', unit: 'minute', unitDisplay: 'short' }).format(minutes)} ${new Intl.NumberFormat(language, { style: 'unit', unit: 'second', unitDisplay: 'short' }).format(rest)}`
}

function closeReasonKey(reason: string): string {
  switch (reason) {
    case 'exited':
      return 'exited'
    case 'session_revoked':
      return 'sessionRevoked'
    case 'ticket_expired':
      return 'ticketExpired'
    case 'open_failed':
      return 'openFailed'
    case 'detached':
      return 'detached'
    default:
      return ''
  }
}

/** RecordingsTab lists terminal sessions and links to their asciicast downloads. */
export function RecordingsTab({ api }: RecordingsTabProps) {
  const { t, i18n } = useTranslation(['audit', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [recordings, setRecordings] = useState<Recording[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let active = true
    api.list().then(
      (next) => {
        if (!active) return
        // The core already sorts, but keep the newest-first contract here too.
        setRecordings(
          [...next].sort(
            (left, right) =>
              new Date(right.startedAt).getTime() - new Date(left.startedAt).getTime(),
          ),
        )
        setError(null)
      },
      (cause: unknown) => {
        if (!active || isSignedOut(cause)) return
        setError(errorMessage(t, cause))
      },
    )
    return () => {
      active = false
    }
  }, [api, t])

  return (
    <div className="recordings">
      <div className="recordings__intro">
        <p>{t('audit:recordings.intro')}</p>
        <p className="recordings__hint">
          {t('audit:recordings.playHint')} <code>asciinema play file.cast</code>
        </p>
      </div>

      {error !== null ? (
        <p className="audit__message audit__message--error" role="alert">
          {error}
        </p>
      ) : null}

      {recordings === null ? (
        <p className="audit__message" role="status">
          {t('audit:recordings.loading')}
        </p>
      ) : recordings.length === 0 ? (
        <p className="audit__message">{t('audit:recordings.empty')}</p>
      ) : (
        <div className="audit__table-wrap">
          <table className="audit__table recordings__table">
            <thead>
              <tr>
                <th scope="col">{t('audit:recordings.table.when')}</th>
                <th scope="col">{t('audit:recordings.table.who')}</th>
                <th scope="col">{t('audit:recordings.table.server')}</th>
                <th scope="col">{t('audit:recordings.table.where')}</th>
                <th scope="col">{t('audit:recordings.table.duration')}</th>
                <th scope="col">{t('audit:recordings.table.reason')}</th>
                <th scope="col">{t('audit:recordings.table.privileged')}</th>
                <th scope="col">{t('audit:recordings.table.download')}</th>
              </tr>
            </thead>
            <tbody>
              {recordings.map((recording) => {
                const started = new Date(recording.startedAt)
                const known = !Number.isNaN(started.getTime())
                const when = known
                  ? new Intl.DateTimeFormat(language, {
                      dateStyle: 'medium',
                      timeStyle: 'medium',
                    }).format(started)
                  : recording.startedAt
                const reasonKey = closeReasonKey(recording.closeReason)
                const reason =
                  recording.closeReason === ''
                    ? t('audit:recordings.noReason')
                    : reasonKey === ''
                      ? recording.closeReason
                      : t(`audit:recordings.reasons.${reasonKey}`)
                return (
                  <tr className="audit__row" key={recording.id}>
                    <td data-label={t('audit:recordings.table.when')}>
                      <time dateTime={recording.startedAt}>{when}</time>
                    </td>
                    <td data-label={t('audit:recordings.table.who')}>{recording.user || '—'}</td>
                    <td data-label={t('audit:recordings.table.server')}>
                      {recording.serverName || recording.serverId}
                    </td>
                    <td data-label={t('audit:recordings.table.where')}>
                      {recording.kind === 'host' ? (
                        t('audit:recordings.host')
                      ) : (
                        <code>{recording.container}</code>
                      )}
                    </td>
                    <td data-label={t('audit:recordings.table.duration')}>
                      {formatDuration(recording.durationSeconds, language)}
                    </td>
                    <td data-label={t('audit:recordings.table.reason')}>{reason}</td>
                    <td data-label={t('audit:recordings.table.privileged')}>
                      {recording.privileged ? (
                        <span className="chip chip--warn">{t('audit:recordings.privileged')}</span>
                      ) : (
                        <span className="audit__muted">{t('audit:recordings.unprivileged')}</span>
                      )}
                    </td>
                    <td data-label={t('audit:recordings.table.download')}>
                      {recording.endedAt !== null ? (
                        <a className="recordings__download" href={api.downloadUrl(recording.id)}>
                          {t('audit:recordings.download')}
                        </a>
                      ) : (
                        <span className="audit__muted">{t('audit:recordings.open')}</span>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
