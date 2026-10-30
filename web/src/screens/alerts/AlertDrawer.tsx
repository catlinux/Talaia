import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Alert, AlertsApi } from '../../api/alerts'
import type { AlertsAdminApi, AlertRule } from '../../api/alertsAdmin'
import type { ContainersApi } from '../../api/containers'
import type { MetricsApi, MetricsRange, MetricsResponse } from '../../api/metrics'
import { isHiddenMetricsError } from '../../api/metrics'
import { usePermissions } from '../../auth/permissions'
import { TimeSeriesChart, type ChartSeries } from '../../components/chart/TimeSeriesChart'
import { SeverityTag, ConfirmDialog, Drawer, useToast, type Severity } from '../../components/ui'
import { Link } from '../../router'
import { TimedOut, waitForJob } from '../server/waitForJob'
import { storeSettingsTab } from '../settings/SettingsTabs'
import { alertMessageText, ruleLabel } from './AlertMessage'
import { alertActions, type AlertCauseAction } from './alertActions'
import { alertScope, alertState } from './inboxModel'
import { SilenceDialog } from './SilenceDialog'

interface AlertDrawerProps {
  alert: Alert | null
  missing?: boolean
  api: AlertsApi
  alertsAdminApi?: AlertsAdminApi
  metricsApi?: MetricsApi
  containersApi?: ContainersApi
  onClose: () => void
  onRefresh?: () => void
  pollMs?: number
}

function exactDate(value: string, language: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(language, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

function durationText(seconds: number, language: string): string {
  const minutes = Math.max(1, Math.round(seconds / 60))
  if (minutes < 60) {
    return new Intl.NumberFormat(language, {
      style: 'unit',
      unit: 'minute',
      unitDisplay: 'short',
      maximumFractionDigits: 0,
    }).format(minutes)
  }
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  const hourText = new Intl.NumberFormat(language, {
    style: 'unit',
    unit: 'hour',
    unitDisplay: 'short',
    maximumFractionDigits: 0,
  }).format(hours)
  return rest === 0
    ? hourText
    : `${hourText} ${new Intl.NumberFormat(language, {
        style: 'unit',
        unit: 'minute',
        unitDisplay: 'short',
        maximumFractionDigits: 0,
      }).format(rest)}`
}

function thresholdText(
  rule: AlertRule | null,
  t: (key: string, params?: Record<string, unknown>) => string,
  language: string,
): string {
  if (rule === null) return ''
  const { threshold, for_seconds: forSeconds, ...rest } = rule.params
  if (typeof threshold === 'number') {
    const percent =
      rule.type === 'cpu_high' || rule.type === 'mem_high' || rule.type === 'disk_high'
    const value = percent ? `${threshold} %` : String(threshold)
    const above = t('drawer.above', { value })
    return forSeconds && forSeconds > 0
      ? `${above} ${t('drawer.for', { duration: durationText(forSeconds, language) })}`
      : above
  }
  return Object.entries(rest)
    .map(([key, value]) => `${key}=${value}`)
    .join(' · ')
}

function metricKind(alert: Alert): 'cpu' | 'mem' | 'disk' | 'container' | null {
  if (alert.ruleType === 'cpu_high') return 'cpu'
  if (alert.ruleType === 'mem_high') return 'mem'
  if (alert.ruleType === 'disk_high') return 'disk'
  if (alert.subjectKind === 'container') return 'container'
  return null
}

function metricRange(alert: Alert): MetricsRange {
  const start = new Date(alert.startedAt).getTime()
  const end = new Date(alert.resolvedAt || Date.now()).getTime()
  if (!Number.isFinite(start) || !Number.isFinite(end)) return '1h'
  const hours = Math.max(1, (end - start) / 3_600_000 + 1)
  if (hours <= 1) return '1h'
  if (hours <= 6) return '6h'
  if (hours <= 24) return '24h'
  if (hours <= 24 * 7) return '7d'
  return '30d'
}

function stateText(
  alert: Alert,
  t: (key: string, params?: Record<string, string>) => string,
): string {
  if (alert.resolvedAt !== '') return t('drawer.state.resolved')
  const state = alertState(alert)
  if (state === 'new') return t('state.new')
  if (state === 'silenced') return t('state.silenced')
  return alert.ackedBy ? t('state.seenBy', { user: alert.ackedBy }) : t('state.seen')
}

function actionLabel(t: (key: string) => string, action: AlertCauseAction): string {
  switch (action.id) {
    case 'open-server':
      return t('drawer.action.openServer')
    case 'start-container':
      return t('drawer.action.startContainer')
    case 'restart-container':
      return t('drawer.action.restartContainer')
    case 'logs':
      return t('drawer.action.logs')
    case 'terminal':
      return t('drawer.action.terminal')
    case 'open-stack':
      return t('drawer.action.openStack')
    case 'open-services':
      return t('drawer.action.openServices')
    case 'view-updates':
      return t('drawer.action.viewUpdates')
    case 'cleanup-resources':
      return t('drawer.action.cleanupResources')
  }
}

/** AlertDrawer explains one alert and offers direct actions on its cause. */
export function AlertDrawer({
  alert,
  missing = false,
  api,
  alertsAdminApi,
  metricsApi,
  containersApi,
  onClose,
  onRefresh = () => undefined,
  pollMs = 800,
}: AlertDrawerProps) {
  const { t, i18n } = useTranslation(['alerts', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const { can } = usePermissions()
  const toast = useToast()
  const [rule, setRule] = useState<AlertRule | null>(null)
  const [metric, setMetric] = useState<MetricsResponse | null>(null)
  const [metricFailed, setMetricFailed] = useState(false)
  const [metricLoading, setMetricLoading] = useState(false)
  const [metricHidden, setMetricHidden] = useState(false)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState<'start' | 'restart' | null>(null)
  const [silence, setSilence] = useState(false)
  const [unwatch, setUnwatch] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const scope = alert === null ? undefined : alertScope(alert)
  const canManage = alert !== null && can('alerts.manage', scope)
  const canView = alert !== null && (canManage || can('alerts.view', scope))

  useEffect(() => {
    if (alert === null || alertsAdminApi === undefined || !canView) return
    let active = true
    alertsAdminApi.listRules().then(
      (rules) => {
        if (!active) return
        setRule(
          rules.find((candidate) => candidate.id === alert.ruleId) ??
            rules.find((candidate) => candidate.type === alert.ruleType) ??
            null,
        )
      },
      () => {
        if (active) setRule(null)
      },
    )
    return () => {
      active = false
    }
  }, [alert, alertsAdminApi, canView])

  useEffect(() => {
    if (alert === null || metricsApi === undefined) return
    const kind = metricKind(alert)
    if (kind === null || alert.serverId === null) return
    let active = true
    const range = metricRange(alert)
    const request =
      kind === 'container'
        ? metricsApi.container(alert.serverId, alert.subjectName, range)
        : metricsApi.server(alert.serverId, range)
    queueMicrotask(() => {
      if (!active) return
      setMetricLoading(true)
      setMetricFailed(false)
      setMetricHidden(false)
      request.then(
        (result) => {
          if (!active) return
          setMetric(result)
          setMetricLoading(false)
        },
        (cause) => {
          if (!active) return
          setMetricLoading(false)
          if (isHiddenMetricsError(cause)) setMetricHidden(true)
          else setMetricFailed(true)
        },
      )
    })
    return () => {
      active = false
    }
  }, [alert, metricsApi])

  const runAction = async (action: () => Promise<void>) => {
    setBusy(true)
    setError(null)
    try {
      await action()
      onRefresh()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('drawer.actionFailed'))
    } finally {
      setBusy(false)
    }
  }

  const runContainer = useCallback(async () => {
    if (alert === null || containersApi === undefined || confirming === null) return
    const action = confirming
    setConfirming(null)
    setBusy(true)
    try {
      const jobId = await containersApi.run(alert.serverId ?? '', alert.subjectName, action)
      const job = await waitForJob(containersApi, jobId, pollMs, () => true)
      if (job === null) return
      const label =
        action === 'start' ? t('drawer.action.startContainer') : t('drawer.action.restartContainer')
      if (job.state === 'succeeded') {
        toast.success(t('drawer.containerActionDone', { action: label, name: alert.subjectName }))
      } else {
        toast.error(
          t('drawer.containerActionFailed', {
            action: label,
            name: alert.subjectName,
            message: job.message,
          }),
        )
      }
      onRefresh()
    } catch (cause) {
      const label =
        action === 'start' ? t('drawer.action.startContainer') : t('drawer.action.restartContainer')
      toast.error(
        t('drawer.containerActionFailed', {
          action: label,
          name: alert.subjectName,
          message:
            cause instanceof TimedOut
              ? t('drawer.timeout')
              : cause instanceof Error
                ? cause.message
                : t('drawer.actionFailed'),
        }),
      )
    } finally {
      setBusy(false)
    }
  }, [alert, confirming, containersApi, onRefresh, pollMs, t, toast])

  if (missing && alert === null) {
    return (
      <Drawer open title={t('drawer.missingTitle')} onClose={onClose}>
        <p className="alerts-drawer__missing">{t('drawer.missing')}</p>
      </Drawer>
    )
  }
  if (alert === null) return null

  const kind = metricKind(alert)
  const points = metric?.points ?? []
  const chartSeries: ChartSeries[] = (() => {
    if (kind === null) return []
    const values = (get: (point: MetricsResponse['points'][number]) => number | null | undefined) =>
      points
        .map((point) => ({ t: Date.parse(point.time), v: get(point) ?? null }))
        .filter((point) => Number.isFinite(point.t))
    if (kind === 'cpu') {
      return [
        {
          id: 'cpu',
          label: t('drawer.metricCpu'),
          color: 'accent' as const,
          points: values((p) => p.cpu),
        },
      ]
    }
    if (kind === 'mem') {
      return [
        {
          id: 'mem',
          label: t('drawer.metricMemory'),
          color: 'ok' as const,
          points: values((p) =>
            p.mem_used !== null && p.mem_total ? (p.mem_used / p.mem_total) * 100 : null,
          ),
        },
      ]
    }
    if (kind === 'disk') {
      return [
        {
          id: 'disk',
          label: t('drawer.metricDisk'),
          color: 'warn' as const,
          points: values((p) =>
            p.disk_used != null && p.disk_total ? (p.disk_used / p.disk_total) * 100 : null,
          ),
        },
      ]
    }
    return [
      {
        id: 'cpu',
        label: t('drawer.metricCpu'),
        color: 'accent' as const,
        points: values((p) => p.cpu),
      },
      {
        id: 'mem',
        label: t('drawer.metricMemory'),
        color: 'ok' as const,
        points: values((p) =>
          p.mem_used !== null && p.mem_total ? (p.mem_used / p.mem_total) * 100 : null,
        ),
      },
    ]
  })()
  const actions = alertActions(alert, can)
  const state = stateText(alert, t)
  const timeline: Array<{ id: string; label: string; at?: string }> = [
    {
      id: 'started',
      label: t('drawer.timeline.started'),
      at: exactDate(alert.startedAt, language),
    },
  ]
  if (alert.ackedAt !== '') {
    timeline.push({
      id: 'acknowledged',
      label: alert.ackedBy
        ? t('drawer.timeline.acknowledgedBy', { user: alert.ackedBy })
        : t('drawer.timeline.acknowledged'),
      at: exactDate(alert.ackedAt, language),
    })
  }
  if (alert.silenced) timeline.push({ id: 'silenced', label: t('drawer.timeline.silenced') })
  if (alert.resolvedAt !== '') {
    timeline.push({
      id: 'resolved',
      label: t('drawer.timeline.resolved'),
      at: exactDate(alert.resolvedAt, language),
    })
  }
  const editRule = canManage ? '/settings' : null

  return (
    <>
      <Drawer open title={alertMessageText(t, alert)} onClose={onClose}>
        <div className="alerts-drawer">
          <div className="alerts-drawer__status">
            <SeverityTag
              severity={
                alert.severity === 'critical' ||
                alert.severity === 'warning' ||
                alert.severity === 'info'
                  ? (alert.severity as Severity)
                  : 'info'
              }
            />
            <span className={`alerts-drawer__state alerts-drawer__state--${alertState(alert)}`}>
              {state}
            </span>
          </div>
          {error !== null ? (
            <p className="alerts__error" role="alert">
              {error}
            </p>
          ) : null}

          <div className="alerts-drawer__section">
            <h3>{t('drawer.where')}</h3>
            <p className="alerts-drawer__where">
              {alert.serverId ? (
                <Link to={`/servers/${encodeURIComponent(alert.serverId)}`}>
                  {alert.serverName || alert.serverId}
                </Link>
              ) : (
                alert.serverName
              )}
              {alert.stack !== '' ? (
                <>
                  {' › '}
                  {alert.serverId ? (
                    <Link
                      to={`/servers/${encodeURIComponent(alert.serverId)}/stacks/${encodeURIComponent(alert.stack)}`}
                    >
                      {alert.stack}
                    </Link>
                  ) : (
                    alert.stack
                  )}
                </>
              ) : null}
              {alert.subjectKind === 'container' && alert.subjectName !== '' ? (
                <>
                  {' › '}
                  {alert.serverId ? (
                    <Link
                      to={`/servers/${encodeURIComponent(alert.serverId)}/containers`}
                      className="alerts-drawer__container"
                    >
                      {alert.subjectName}
                    </Link>
                  ) : (
                    <span className="alerts-drawer__container">{alert.subjectName}</span>
                  )}
                </>
              ) : null}
            </p>
          </div>

          <div className="alerts-drawer__section">
            <h3>{t('drawer.actions')}</h3>
            <div className="alerts-drawer__actions">
              {actions.map((action) =>
                action.kind === 'container' ? (
                  containersApi !== undefined ? (
                    <button
                      key={action.id}
                      type="button"
                      className="ui-btn"
                      disabled={busy}
                      onClick={() => setConfirming(action.containerAction ?? 'restart')}
                    >
                      {actionLabel(t, action)}
                    </button>
                  ) : null
                ) : action.href !== undefined ? (
                  <Link key={action.id} className="ui-btn ui-btn--ghost" to={action.href}>
                    {actionLabel(t, action)}
                  </Link>
                ) : null,
              )}
              {alert.ruleType === 'server_offline' ? (
                <p className="alerts-drawer__hint">{t('drawer.agentHint')}</p>
              ) : null}
              {can('alerts.silence', scope) ? (
                <button
                  type="button"
                  className="ui-btn"
                  disabled={busy || alertState(alert) === 'seen'}
                  onClick={() => void runAction(() => api.acknowledge(alert.id))}
                >
                  {t('actions.acknowledge')}
                </button>
              ) : null}
              {can('alerts.silence', scope) ? (
                <button
                  type="button"
                  className="ui-btn"
                  disabled={busy}
                  onClick={() => setSilence(true)}
                >
                  {t('actions.silenceMenu')}
                </button>
              ) : null}
              {can('alerts.silence', scope) && alert.subjectKind === 'container' ? (
                <button
                  type="button"
                  className="ui-btn ui-btn--danger"
                  disabled={busy}
                  onClick={() => setUnwatch(true)}
                >
                  {t('actions.unwatch')}
                </button>
              ) : null}
            </div>
          </div>

          <div className="alerts-drawer__section">
            <h3>{t('drawer.timeline.title')}</h3>
            <ol className="alerts-drawer__timeline">
              {timeline.map((event) => (
                <li key={event.id}>
                  <span>{event.label}</span>
                  {event.at !== undefined ? <time dateTime={event.at}>{event.at}</time> : null}
                </li>
              ))}
            </ol>
          </div>

          <div className="alerts-drawer__section">
            <h3>{t('drawer.rule.title')}</h3>
            <div className="alerts-drawer__rule">
              <strong>{rule?.name || ruleLabel(t, alert.ruleType)}</strong>
              <span>{ruleLabel(t, rule?.type ?? alert.ruleType)}</span>
              {rule !== null ? <SeverityTag severity={rule.severity} /> : null}
              <span>{thresholdText(rule, t, language)}</span>
              {editRule !== null ? (
                <Link to={editRule} onClick={() => storeSettingsTab('alerts')}>
                  {t('drawer.rule.edit')}
                </Link>
              ) : null}
            </div>
          </div>

          {kind !== null && alert.serverId !== null && metricsApi !== undefined && !metricHidden ? (
            <div className="alerts-drawer__section">
              <h3>{t('drawer.metric.title')}</h3>
              {metricFailed ? (
                <p className="alerts__error">{t('drawer.metric.failed')}</p>
              ) : (
                <TimeSeriesChart
                  label={t('drawer.metric.title')}
                  series={chartSeries}
                  format="percent"
                  yDomain={[0, 100]}
                  range={metricRange(alert)}
                  loading={metricLoading}
                  height={170}
                />
              )}
            </div>
          ) : null}
        </div>
      </Drawer>

      <ConfirmDialog
        open={confirming !== null}
        title={
          confirming === 'start'
            ? t('drawer.confirm.startTitle', { name: alert.subjectName })
            : t('drawer.confirm.restartTitle', { name: alert.subjectName })
        }
        body={
          confirming === 'start' ? t('drawer.confirm.startBody') : t('drawer.confirm.restartBody')
        }
        confirmLabel={
          confirming === 'start'
            ? t('drawer.action.startContainer')
            : t('drawer.action.restartContainer')
        }
        danger={confirming === 'restart'}
        onConfirm={() => void runContainer()}
        onCancel={() => setConfirming(null)}
      />
      {silence ? (
        <SilenceDialog
          alert={alert}
          api={api}
          onClose={() => setSilence(false)}
          onDone={() => {
            setSilence(false)
            onRefresh()
          }}
        />
      ) : null}
      <ConfirmDialog
        open={unwatch}
        title={t('unwatch.title', { subject: alert.subjectName })}
        body={t('unwatch.body')}
        confirmLabel={t('unwatch.confirm')}
        danger
        onConfirm={() => {
          setUnwatch(false)
          void runAction(() => api.unwatch(alert.id))
        }}
        onCancel={() => setUnwatch(false)}
      />
    </>
  )
}
