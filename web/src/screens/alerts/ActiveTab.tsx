import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Alert, AlertsApi } from '../../api/alerts'
import { usePermissions } from '../../auth/permissions'
import { errorMessage } from '../../auth/AuthGate'
import { formatRelativeTime } from '../fleet/format'
import { Dialog } from '../users/Dialog'
import { AlertMessage } from './AlertMessage'
import { SilenceDialog } from './SilenceDialog'

interface ActiveTabProps {
  api: AlertsApi
  onRefresh: () => void
  onOpenServer?: (serverId: string, serverName: string) => void
}

const severityRank: Record<string, number> = { critical: 3, warning: 2, info: 1 }

function alertScope(alert: Alert) {
  return {
    serverId: alert.serverId ?? undefined,
    stack: alert.stack || undefined,
    container: alert.subjectKind === 'container' ? alert.subjectName : undefined,
  }
}

export function ActiveTab({ api, onRefresh, onOpenServer }: ActiveTabProps) {
  const { t, i18n } = useTranslation(['alerts', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const { can } = usePermissions()
  const [rows, setRows] = useState<Alert[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [silence, setSilence] = useState<Alert | null>(null)
  const [unwatch, setUnwatch] = useState<Alert | null>(null)

  const exact = (value: string): string => {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return value
    return new Intl.DateTimeFormat(language, {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(date)
  }

  const load = useCallback(async () => {
    setError(null)
    try {
      const alerts = await api.list('active')
      setRows(
        [...alerts].sort(
          (left, right) =>
            (severityRank[right.severity] ?? 0) - (severityRank[left.severity] ?? 0) ||
            right.startedAt.localeCompare(left.startedAt),
        ),
      )
    } catch (cause) {
      setError(errorMessage(t, cause))
    }
  }, [api, t])

  useEffect(() => {
    queueMicrotask(() => {
      void load()
    })
  }, [load])

  const act = async (alert: Alert, action: () => Promise<void>) => {
    setBusyId(alert.id)
    setError(null)
    try {
      await action()
      await load()
      onRefresh()
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setBusyId(null)
    }
  }

  if (error !== null && rows === null) {
    return (
      <p className="alerts__error" role="alert">
        {error}
      </p>
    )
  }
  if (rows === null) {
    return (
      <p className="alerts__loading" role="status">
        {t('alerts:loading')}
      </p>
    )
  }
  if (rows.length === 0) {
    return (
      <div className="alerts__empty">
        <span className="alerts__empty-mark" aria-hidden="true">
          ✓
        </span>
        <h2>{t('alerts:empty.title')}</h2>
        <p>{t('alerts:empty.body')}</p>
      </div>
    )
  }

  return (
    <div className="alerts__list">
      {error !== null ? (
        <p className="alerts__error" role="alert">
          {error}
        </p>
      ) : null}
      {rows.map((alert) => {
        const allowed = can('alerts.silence' as never, alertScope(alert))
        const target = [alert.stack, alert.subjectKind === 'container' ? alert.subjectName : '']
          .filter(Boolean)
          .join(' · ')
        const started = new Date(alert.startedAt)
        return (
          <article className={`alert-card alert-card--${alert.severity}`} key={alert.id}>
            <div className="alert-card__top">
              <span className={`alert-severity alert-severity--${alert.severity}`}>
                <span className="alert-severity__mark" aria-hidden="true">
                  {alert.severity === 'critical' ? '!' : alert.severity === 'warning' ? '▲' : 'i'}
                </span>
                {t(`alerts:severity.${alert.severity}`, {
                  defaultValue: alert.severity,
                })}
              </span>
              <time dateTime={alert.startedAt} title={exact(alert.startedAt)}>
                {formatRelativeTime(started, new Date(), language)}
              </time>
            </div>
            <p className="alert-card__message">
              <AlertMessage alert={alert} />
            </p>
            <p className="alert-card__target">
              {alert.serverId && onOpenServer ? (
                <button
                  type="button"
                  className="alert-card__server"
                  onClick={() =>
                    onOpenServer(alert.serverId ?? '', alert.serverName || alert.serverId || '')
                  }
                >
                  {alert.serverName || alert.serverId}
                </button>
              ) : (
                <span>{alert.serverName}</span>
              )}
              {target ? ` · ${target}` : ''}
            </p>
            <div className="alert-card__meta">
              {alert.ackedBy ? (
                <span>{t('alerts:acknowledgedBy', { user: alert.ackedBy })}</span>
              ) : null}
              {alert.silenced ? <span>{t('alerts:silenced')}</span> : null}
            </div>
            {allowed ? (
              <footer className="alert-card__actions">
                {!alert.ackedBy ? (
                  <button
                    type="button"
                    className="cbtn"
                    disabled={busyId === alert.id}
                    onClick={() => void act(alert, () => api.acknowledge(alert.id))}
                  >
                    {t('alerts:actions.acknowledge')}
                  </button>
                ) : null}
                <button
                  type="button"
                  className="cbtn"
                  disabled={busyId === alert.id}
                  onClick={() => setSilence(alert)}
                >
                  {t('alerts:actions.silence')}
                </button>
                {alert.subjectKind === 'container' ? (
                  <button
                    type="button"
                    className="cbtn cbtn--danger"
                    disabled={busyId === alert.id}
                    onClick={() => setUnwatch(alert)}
                  >
                    {t('alerts:actions.unwatch')}
                  </button>
                ) : null}
              </footer>
            ) : null}
          </article>
        )
      })}
      {silence !== null ? (
        <SilenceDialog
          alert={silence}
          api={api}
          onClose={() => setSilence(null)}
          onDone={() => {
            setSilence(null)
            void load()
            onRefresh()
          }}
        />
      ) : null}
      {unwatch !== null ? (
        <Dialog
          title={t('alerts:unwatch.title', { subject: unwatch.subjectName })}
          onClose={() => setUnwatch(null)}
          footer={
            <div className="alerts__actions">
              <button type="button" className="cbtn" onClick={() => setUnwatch(null)}>
                {t('alerts:actions.cancel')}
              </button>
              <button
                type="button"
                className="cbtn cbtn--danger"
                disabled={busyId === unwatch.id}
                onClick={() => {
                  const current = unwatch
                  setUnwatch(null)
                  void act(current, () => api.unwatch(current.id))
                }}
              >
                {t('alerts:unwatch.confirm')}
              </button>
            </div>
          }
        >
          <p>{t('alerts:unwatch.body')}</p>
        </Dialog>
      ) : null}
    </div>
  )
}
