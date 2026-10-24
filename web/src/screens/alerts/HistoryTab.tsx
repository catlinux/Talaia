import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Alert, AlertsApi } from '../../api/alerts'
import { errorMessage } from '../../auth/AuthGate'
import { formatRelativeTime } from '../fleet/format'
import { AlertMessage } from './AlertMessage'

interface HistoryTabProps {
  api: AlertsApi
}

function formatDuration(seconds: number, language: string): string {
  const safe = Math.max(0, Math.round(seconds))
  if (safe < 60) {
    return new Intl.NumberFormat(language, {
      style: 'unit',
      unit: 'second',
      unitDisplay: 'short',
    }).format(safe)
  }
  const minutes = Math.floor(safe / 60)
  if (minutes < 60) {
    return new Intl.NumberFormat(language, {
      style: 'unit',
      unit: 'minute',
      unitDisplay: 'short',
    }).format(minutes)
  }
  const hours = Math.floor(minutes / 60)
  return `${new Intl.NumberFormat(language, {
    style: 'unit',
    unit: 'hour',
    unitDisplay: 'short',
  }).format(hours)} ${new Intl.NumberFormat(language, {
    style: 'unit',
    unit: 'minute',
    unitDisplay: 'short',
  }).format(minutes % 60)}`
}

export function HistoryTab({ api }: HistoryTabProps) {
  const { t, i18n } = useTranslation(['alerts', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [rows, setRows] = useState<Alert[] | null>(null)
  const [server, setServer] = useState('')
  const [offset, setOffset] = useState(0)
  const [more, setMore] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const request = useRef(0)

  const load = useCallback(
    async (next: number, append: boolean) => {
      const current = ++request.current
      setLoading(true)
      setError(null)
      try {
        const page = await api.list('history', next, server.trim() || undefined)
        if (current !== request.current) return
        setRows((old) => (append && old !== null ? [...old, ...page] : page))
        setOffset(next + page.length)
        setMore(page.length === 50)
      } catch (cause) {
        if (current !== request.current) return
        setError(errorMessage(t, cause))
      } finally {
        if (current === request.current) setLoading(false)
      }
    },
    [api, server, t],
  )

  useEffect(() => {
    queueMicrotask(() => {
      void load(0, false)
    })
  }, [load])

  const formatWhen = (value: string): string => {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return value
    return new Intl.DateTimeFormat(language, {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(date)
  }

  return (
    <div className="alerts__history">
      <label className="alerts__filter">
        <span>{t('alerts:history.server')}</span>
        <input
          type="search"
          value={server}
          placeholder={t('alerts:history.serverPlaceholder')}
          onChange={(event) => setServer(event.target.value)}
        />
      </label>

      {error !== null ? (
        <p className="alerts__error" role="alert">
          {error}
        </p>
      ) : null}
      {rows === null ? (
        error === null ? (
          <p className="alerts__loading" role="status">
            {t('alerts:history.loading')}
          </p>
        ) : null
      ) : rows.length === 0 ? (
        <p className="alerts__empty-text">{t('alerts:empty.history')}</p>
      ) : (
        <div className="alerts__list">
          {rows.map((alert) => {
            const started = new Date(alert.startedAt)
            const resolved = alert.resolvedAt === '' ? null : new Date(alert.resolvedAt)
            const duration =
              resolved !== null &&
              !Number.isNaN(started.getTime()) &&
              !Number.isNaN(resolved.getTime())
                ? formatDuration((resolved.getTime() - started.getTime()) / 1000, language)
                : null
            return (
              <article className="alert-card alert-card--history" key={alert.id}>
                <div className="alert-card__top">
                  <span className={`alert-severity alert-severity--${alert.severity}`}>
                    <span className="alert-severity__mark" aria-hidden="true">
                      {alert.severity === 'critical'
                        ? '!'
                        : alert.severity === 'warning'
                          ? '▲'
                          : 'i'}
                    </span>
                    {t(`alerts:severity.${alert.severity}`, { defaultValue: alert.severity })}
                  </span>
                  <time dateTime={alert.startedAt} title={formatWhen(alert.startedAt)}>
                    {formatRelativeTime(started, new Date(), language)}
                  </time>
                </div>
                <p className="alert-card__message">
                  <AlertMessage alert={alert} />
                </p>
                <p className="alert-card__target">
                  {alert.serverName}
                  {alert.stack ? ` · ${alert.stack}` : ''}
                  {alert.subjectKind === 'container' ? ` · ${alert.subjectName}` : ''}
                </p>
                <div className="alert-card__meta">
                  <span>{t('alerts:history.started', { time: formatWhen(alert.startedAt) })}</span>
                  <span>
                    {resolved === null
                      ? t('alerts:history.stillFiring')
                      : t('alerts:history.resolved', { time: formatWhen(alert.resolvedAt) })}
                  </span>
                  {duration !== null ? (
                    <span>{t('alerts:history.duration', { duration })}</span>
                  ) : null}
                </div>
              </article>
            )
          })}
        </div>
      )}
      {more ? (
        <button
          type="button"
          className="cbtn alerts__more"
          disabled={loading}
          onClick={() => void load(offset, true)}
        >
          {t('alerts:history.more')}
        </button>
      ) : null}
    </div>
  )
}
