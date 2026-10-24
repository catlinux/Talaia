import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { AlertSilence, AlertsApi } from '../../api/alerts'
import { errorMessage } from '../../auth/AuthGate'
import { usePermissions } from '../../auth/permissions'
import { formatRelativeTime } from '../fleet/format'
import { ruleLabel } from './AlertMessage'

interface SilencesTabProps {
  api: AlertsApi
  onRefresh: () => void
}

export function SilencesTab({ api, onRefresh }: SilencesTabProps) {
  const { t, i18n } = useTranslation(['alerts', 'errors'])
  const { can } = usePermissions()
  const language = i18n.resolvedLanguage ?? i18n.language
  const [rows, setRows] = useState<AlertSilence[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      setRows(await api.silences())
    } catch (cause) {
      setError(errorMessage(t, cause))
    }
  }, [api, t])

  useEffect(() => {
    queueMicrotask(() => {
      void load()
    })
  }, [load])

  const remove = async (silence: AlertSilence) => {
    setBusyId(silence.id)
    setError(null)
    try {
      await api.deleteSilence(silence.id)
      await load()
      onRefresh()
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setBusyId(null)
    }
  }

  const exact = (value: string): string => {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return value
    return new Intl.DateTimeFormat(language, {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(date)
  }

  return (
    <div className="alerts__silences">
      {error !== null ? (
        <p className="alerts__error" role="alert">
          {error}
        </p>
      ) : null}
      {rows === null ? (
        error === null ? (
          <p className="alerts__loading" role="status">
            {t('alerts:loading')}
          </p>
        ) : null
      ) : rows.length === 0 ? (
        <p className="alerts__empty-text">{t('alerts:empty.silences')}</p>
      ) : (
        <div className="alerts__list">
          {rows.map((silence) => {
            const end = new Date(silence.endsAt)
            const scope =
              silence.scopeType === 'stack' || silence.scopeType === 'container'
                ? {
                    serverId: silence.serverId ?? undefined,
                    stack: silence.scopeType === 'stack' ? (silence.name ?? undefined) : undefined,
                    container:
                      silence.scopeType === 'container' ? (silence.name ?? undefined) : undefined,
                  }
                : { serverId: silence.serverId ?? undefined }
            const canDelete =
              silence.scopeType === 'group'
                ? can('alerts.silence' as never)
                : can('alerts.silence' as never, scope)
            const detail =
              silence.name ??
              (silence.scopeType === 'server'
                ? silence.serverId
                : silence.scopeType === 'group'
                  ? silence.groupId
                  : null)
            return (
              <article className="alert-card alert-card--silence" key={silence.id}>
                <h2 className="alert-card__title">
                  {t(`alerts:scope.${silence.scopeType}`, {
                    defaultValue: silence.scopeType,
                  })}
                  {detail ? ` · ${detail}` : ''}
                </h2>
                <p className="alert-card__message">{silence.reason}</p>
                <p className="alert-card__target">
                  {silence.ruleType ? ruleLabel(t, silence.ruleType) : t('alerts:silence.allRules')}
                  {' · '}
                  {t('alerts:silence.createdBy', { user: silence.createdBy })}
                </p>
                <time dateTime={silence.endsAt} title={exact(silence.endsAt)}>
                  {t('alerts:silence.until', {
                    time: formatRelativeTime(end, new Date(), language),
                  })}
                </time>
                {canDelete ? (
                  <footer className="alert-card__actions">
                    <button
                      type="button"
                      className="cbtn cbtn--danger"
                      disabled={busyId === silence.id}
                      onClick={() => void remove(silence)}
                    >
                      {t('alerts:actions.removeSilence')}
                    </button>
                  </footer>
                ) : null}
              </article>
            )
          })}
        </div>
      )}
    </div>
  )
}
