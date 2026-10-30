import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { createAlertsApi, type Alert, type AlertsApi, type AlertSummary } from '../../api/alerts'
import { usePermissions } from '../../auth/permissions'
import { AlertIcon } from '../icons'
import { AlertMessage } from '../../screens/alerts/AlertMessage'
import { alertState } from '../../screens/alerts/inboxModel'
import { Link } from '../../router'
import { formatRelativeTime } from '../../screens/fleet/format'
import { SeverityTag, type Severity } from '../ui'

interface AlertBellProps {
  summary: AlertSummary | null
  api?: AlertsApi
}

/** AlertBell shows the open alert count and the latest new or critical alerts. */
export function AlertBell({ summary, api }: AlertBellProps) {
  const { t, i18n } = useTranslation(['common', 'alerts'])
  const { canAnywhere } = usePermissions()
  const client = useMemo(() => api ?? createAlertsApi(), [api])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [open, setOpen] = useState(false)
  const [rows, setRows] = useState<Alert[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [failed, setFailed] = useState(false)
  const wrapper = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)

  const count =
    summary === null ? 0 : (summary.counts.warning ?? 0) + (summary.counts.critical ?? 0)
  const critical = (summary?.counts.critical ?? 0) > 0
  const canView = canAnywhere('alerts.view')

  useEffect(() => {
    if (!open) return
    const onOutside = (event: MouseEvent) => {
      if (wrapper.current !== null && event.target instanceof Node) {
        if (!wrapper.current.contains(event.target)) setOpen(false)
      }
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      setOpen(false)
      trigger.current?.focus()
    }
    document.addEventListener('mousedown', onOutside)
    document.addEventListener('click', onOutside)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onOutside)
      document.removeEventListener('click', onOutside)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  if (!canView || count === 0) return null

  const load = async () => {
    setLoading(true)
    setFailed(false)
    try {
      const active = await client.list('active')
      const latest = active
        .filter((alert) => alertState(alert) === 'new' || alert.severity === 'critical')
        .sort((left, right) => right.startedAt.localeCompare(left.startedAt))
        .slice(0, 5)
      setRows(latest)
    } catch {
      setFailed(true)
      setRows([])
    } finally {
      setLoading(false)
    }
  }

  const toggle = () => {
    if (open) {
      setOpen(false)
      return
    }
    setOpen(true)
    void load()
  }

  return (
    <div className="shell__bell-wrap" ref={wrapper}>
      <button
        ref={trigger}
        type="button"
        className="shell__bell"
        aria-label={t('common:shell.alertsOpen', { count })}
        aria-expanded={open}
        onClick={toggle}
      >
        <AlertIcon />
        <span
          className={critical ? 'shell__bell-dot shell__bell-dot--crit' : 'shell__bell-dot'}
          aria-hidden="true"
        >
          {count}
        </span>
      </button>
      {open ? (
        <div className="alert-bell" role="menu" aria-label={t('alerts:bell.title')}>
          <header className="alert-bell__head">
            <strong>{t('alerts:bell.title')}</strong>
          </header>
          {loading ? (
            <p className="alert-bell__state">{t('alerts:bell.loading')}</p>
          ) : failed ? (
            <p className="alert-bell__state" role="alert">
              {t('alerts:bell.error')}
            </p>
          ) : (rows ?? []).length === 0 ? (
            <p className="alert-bell__state">{t('alerts:bell.empty')}</p>
          ) : (
            <ul className="alert-bell__list">
              {(rows ?? []).map((alert) => {
                const severity: Severity =
                  alert.severity === 'critical' ||
                  alert.severity === 'warning' ||
                  alert.severity === 'info'
                    ? alert.severity
                    : 'info'
                return (
                  <li key={alert.id} role="menuitem">
                    <Link
                      to={`/alerts?focus=${encodeURIComponent(alert.id)}`}
                      className="alert-bell__item"
                      onClick={() => setOpen(false)}
                    >
                      <span className="alert-bell__top">
                        <SeverityTag severity={severity} />
                        <time dateTime={alert.startedAt}>
                          {formatRelativeTime(new Date(alert.startedAt), new Date(), language)}
                        </time>
                      </span>
                      <span className="alert-bell__message">
                        <AlertMessage alert={alert} />
                      </span>
                      <span className="alert-bell__where">{alert.serverName}</span>
                    </Link>
                  </li>
                )
              })}
            </ul>
          )}
          <div role="menuitem">
            <Link to="/alerts" className="alert-bell__all" onClick={() => setOpen(false)}>
              {t('alerts:bell.viewAll')}
            </Link>
          </div>
        </div>
      ) : null}
    </div>
  )
}
