import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { isSignedOut } from '../../api/http'
import type { Recording, RecordingsApi } from '../../api/recordings'
import { errorMessage } from '../../auth/AuthGate'
import { DataTable, type DataTableColumn } from '../../components/ui'

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

  const columns: DataTableColumn<Recording>[] = [
    {
      id: 'when',
      header: t('audit:recordings.table.when'),
      sortValue: (recording) => new Date(recording.startedAt).getTime(),
      cell: (recording) => {
        const started = new Date(recording.startedAt)
        const known = !Number.isNaN(started.getTime())
        const when = known
          ? new Intl.DateTimeFormat(language, {
              dateStyle: 'medium',
              timeStyle: 'medium',
            }).format(started)
          : recording.startedAt
        return <time dateTime={recording.startedAt}>{when}</time>
      },
    },
    {
      id: 'who',
      header: t('audit:recordings.table.who'),
      sortValue: (recording) => recording.user,
      cell: (recording) => recording.user || '—',
    },
    {
      id: 'server',
      header: t('audit:recordings.table.server'),
      sortValue: (recording) => recording.serverName || recording.serverId,
      cell: (recording) => recording.serverName || recording.serverId,
    },
    {
      id: 'where',
      header: t('audit:recordings.table.where'),
      cell: (recording) =>
        recording.kind === 'host' ? t('audit:recordings.host') : <code>{recording.container}</code>,
    },
    {
      id: 'duration',
      header: t('audit:recordings.table.duration'),
      sortValue: (recording) => recording.durationSeconds,
      cell: (recording) => formatDuration(recording.durationSeconds, language),
    },
    {
      id: 'reason',
      header: t('audit:recordings.table.reason'),
      cell: (recording) => {
        const reasonKey = closeReasonKey(recording.closeReason)
        return recording.closeReason === ''
          ? t('audit:recordings.noReason')
          : reasonKey === ''
            ? recording.closeReason
            : t(`audit:recordings.reasons.${reasonKey}`)
      },
    },
    {
      id: 'privileged',
      header: t('audit:recordings.table.privileged'),
      cell: (recording) =>
        recording.privileged ? (
          <span className="chip chip--warn">{t('audit:recordings.privileged')}</span>
        ) : (
          <span className="audit__muted">{t('audit:recordings.unprivileged')}</span>
        ),
    },
    {
      id: 'download',
      header: t('audit:recordings.table.download'),
      cell: (recording) =>
        recording.endedAt !== null ? (
          <a className="recordings__download" href={api.downloadUrl(recording.id)}>
            {t('audit:recordings.download')}
          </a>
        ) : (
          <span className="audit__muted">{t('audit:recordings.open')}</span>
        ),
    },
  ]

  return (
    <div className="recordings">
      <div className="recordings__intro">
        <p>{t('audit:recordings.intro')}</p>
        <p className="recordings__hint">
          {t('audit:recordings.playHint')} <code>asciinema play file.cast</code>
        </p>
      </div>

      <DataTable
        columns={columns}
        rows={recordings ?? []}
        rowKey={(recording) => recording.id}
        loading={recordings === null && error === null}
        error={error ?? undefined}
        empty={<p className="audit__message">{t('audit:recordings.empty')}</p>}
      />
    </div>
  )
}
