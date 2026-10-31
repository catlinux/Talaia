import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Alert, AlertsApi } from '../../api/alerts'
import type { AlertsAdminApi } from '../../api/alertsAdmin'
import type { ContainersApi } from '../../api/containers'
import type { MetricsApi } from '../../api/metrics'
import { errorMessage } from '../../auth/AuthGate'
import { usePermissions } from '../../auth/permissions'
import {
  ConfirmDialog,
  DataTable,
  Menu,
  SeverityTag,
  useToast,
  type DataTableColumn,
  type DataTableGroup,
  type MenuEntry,
  type SelectOption,
  type Severity,
} from '../../components/ui'
import { Link, useNavigate, useRoute, useSetQuery } from '../../router'
import { formatRelativeTime } from '../fleet/format'
import { Dialog } from '../users/Dialog'
import { AlertDrawer } from './AlertDrawer'
import { AlertMessage, alertMessageText, ruleLabel } from './AlertMessage'
import { BulkActions } from './BulkActions'
import { BulkSilenceDialog } from './BulkSilenceDialog'
import { InboxFilters } from './InboxFilters'
import {
  alertScope,
  alertState,
  defaultSort,
  filterAlerts,
  groupKey,
  inboxCounts,
  type InboxCounts,
  type InboxFilters as InboxFilterValues,
  type InboxGroup,
  type StateFilter,
  type SeverityFilter,
} from './inboxModel'
import { SilenceDialog } from './SilenceDialog'

export const refreshInterval = 30_000

interface InboxProps {
  api: AlertsApi
  onRefresh: () => void
  onOpenServer?: (serverId: string, serverName: string) => void
  onCounts?: (counts: InboxCounts) => void
  alertsAdminApi?: AlertsAdminApi
  metricsApi?: MetricsApi
  containersApi?: ContainersApi
  pollMs?: number
  refreshMs?: number
}

const severityValues = ['critical', 'warning', 'info'] as const

function parseFilters(query: URLSearchParams): InboxFilterValues {
  const severity = query.get('severity')
  const state = query.get('state')
  const group = query.get('group')
  return {
    severity: (severityValues as readonly string[]).includes(severity ?? '')
      ? (severity as SeverityFilter)
      : 'all',
    state:
      state === 'new' || state === 'seen' || state === 'silenced' ? (state as StateFilter) : 'all',
    server: query.get('server') ?? '',
    rule: query.get('rule') ?? '',
    q: query.get('q') ?? '',
    group: group === 'server' || group === 'rule' ? (group as InboxGroup) : 'none',
  }
}

function filtersChanged(filters: InboxFilterValues): boolean {
  return (
    filters.severity !== 'all' ||
    filters.state !== 'all' ||
    filters.server !== '' ||
    filters.rule !== '' ||
    filters.q !== '' ||
    filters.group !== 'none'
  )
}

function severityOf(value: string): Severity {
  return value === 'critical' || value === 'warning' || value === 'info' ? value : 'info'
}

function exactDate(value: string, language: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(language, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

function severityCountsText(
  rows: Alert[],
  t: (key: string, options?: Record<string, unknown>) => string,
): string {
  const counts = new Map<string, number>()
  for (const row of rows) counts.set(row.severity, (counts.get(row.severity) ?? 0) + 1)
  return severityValues
    .filter((severity) => (counts.get(severity) ?? 0) > 0)
    .map((severity) => `${counts.get(severity)} ${t(`severity.${severity}`)}`)
    .join(' · ')
}

interface AlertMenuProps {
  alert: Alert
  can: ReturnType<typeof usePermissions>['can']
  onAcknowledge: (alert: Alert) => void
  onSilence: (alert: Alert) => void
  onUnwatch: (alert: Alert) => void
  onOpenServer: (alert: Alert) => void
}

function AlertMenu({
  alert,
  can,
  onAcknowledge,
  onSilence,
  onUnwatch,
  onOpenServer,
}: AlertMenuProps) {
  const { t } = useTranslation('alerts')
  if (!can('alerts.silence', alertScope(alert))) {
    return <span className="alerts-inbox__muted">—</span>
  }

  const items: MenuEntry[] = []
  if (alertState(alert) !== 'seen') {
    items.push({
      id: 'acknowledge',
      label: t('actions.acknowledge'),
      onSelect: () => onAcknowledge(alert),
    })
  }
  items.push({
    id: 'silence',
    label: t('actions.silenceMenu'),
    onSelect: () => onSilence(alert),
  })
  if (alert.serverId) {
    items.push({
      id: 'open',
      label: t('actions.openServer'),
      onSelect: () => onOpenServer(alert),
    })
  }
  if (alert.subjectKind === 'container') {
    items.push({ separator: true })
    items.push({
      id: 'unwatch',
      label: t('actions.unwatch'),
      danger: true,
      onSelect: () => onUnwatch(alert),
    })
  }

  return (
    <Menu
      align="end"
      items={items}
      trigger={(props) => (
        <button
          {...props}
          className="ui-btn ui-btn--ghost alerts-inbox__menu"
          aria-label={t('menu.row', { subject: alert.subjectName || alert.serverName || '—' })}
        >
          ⋯
        </button>
      )}
    />
  )
}

/** Inbox is the active alerts table with filters, grouping and bulk actions. */
export function Inbox({
  api,
  onRefresh,
  onOpenServer,
  onCounts,
  alertsAdminApi,
  metricsApi,
  containersApi,
  pollMs = 800,
  refreshMs = refreshInterval,
}: InboxProps) {
  const { t, i18n } = useTranslation(['alerts', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const { query } = useRoute()
  const setQuery = useSetQuery()
  const navigate = useNavigate()
  const { can, canAnywhere } = usePermissions()
  const toast = useToast()
  const filters = parseFilters(query)
  const focus = query.get('focus') ?? ''
  const [alerts, setAlerts] = useState<Alert[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const [silence, setSilence] = useState<Alert | null>(null)
  const [unwatch, setUnwatch] = useState<Alert | null>(null)
  const [bulkSilence, setBulkSilence] = useState<{ alerts: Alert[]; skipped: number } | null>(null)
  const [bulkUnwatch, setBulkUnwatch] = useState<{ alerts: Alert[]; skipped: number } | null>(null)
  const [historyFocus, setHistoryFocus] = useState<{ id: string; alert: Alert | null } | null>(null)
  const alive = useRef(true)
  const countsCallback = useRef(onCounts)
  const request = useRef(0)

  useEffect(() => {
    countsCallback.current = onCounts
  }, [onCounts])

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const messageOf = useCallback((alert: Alert) => alertMessageText(t, alert), [t])

  const load = useCallback(async () => {
    const current = request.current + 1
    request.current = current
    try {
      const rows = await api.list('active')
      if (!alive.current || current !== request.current) return
      setAlerts(rows)
      setError(null)
      countsCallback.current?.(inboxCounts(rows))
      setSelected((currentSelection) => {
        const next = new Set(
          [...currentSelection].filter((id) => rows.some((alert) => alert.id === id)),
        )
        return next.size === currentSelection.size ? currentSelection : next
      })
    } catch (cause) {
      if (!alive.current || current !== request.current) return
      setError(errorMessage(t, cause))
    } finally {
      if (alive.current && current === request.current) setLoading(false)
    }
  }, [api, t])

  useEffect(() => {
    queueMicrotask(() => {
      void load()
    })
    const timer = window.setInterval(() => {
      if (!document.hidden) void load()
    }, refreshMs)
    return () => window.clearInterval(timer)
  }, [load, refreshMs])

  useEffect(() => {
    if (focus === '') return
    const row = document.querySelector<HTMLElement>('.alerts-inbox__row--focus')
    row?.scrollIntoView?.({ block: 'center' })
  }, [focus, alerts])

  const activeFocus = alerts?.find((alert) => alert.id === focus)
  const historyAlert = historyFocus?.id === focus ? historyFocus.alert : null
  const drawerAlert = activeFocus ?? historyAlert
  const focusMissing =
    focus !== '' &&
    alerts !== null &&
    activeFocus === undefined &&
    historyFocus?.id === focus &&
    historyAlert === null

  useEffect(() => {
    if (focus === '' || alerts === null || activeFocus !== undefined) return
    if (historyFocus?.id === focus) return
    let active = true
    api.list('history').then(
      (rows) => {
        if (!active) return
        setHistoryFocus({
          id: focus,
          alert: rows.find((alert) => alert.id === focus) ?? null,
        })
      },
      () => {
        if (active) setHistoryFocus({ id: focus, alert: null })
      },
    )
    return () => {
      active = false
    }
  }, [focus, alerts, activeFocus, historyFocus, api])

  const rows = useMemo(() => {
    if (alerts === null) return []
    return [...filterAlerts(alerts, filters, messageOf)].sort(defaultSort)
  }, [alerts, filters, messageOf])

  const selectedAlerts = useMemo(() => {
    return (alerts ?? []).filter((alert) => selected.has(alert.id))
  }, [alerts, selected])

  const serverOptions = useMemo<SelectOption[]>(() => {
    const seen = new Map<string, string>()
    for (const alert of alerts ?? []) {
      if (alert.serverId && !seen.has(alert.serverId)) {
        seen.set(alert.serverId, alert.serverName || alert.serverId)
      }
    }
    return [
      { value: '', label: t('filters.allServers') },
      ...[...seen.entries()].map(([value, label]) => ({ value, label })),
    ]
  }, [alerts, t])

  const ruleOptions = useMemo<SelectOption[]>(() => {
    const rules = new Set((alerts ?? []).map((alert) => alert.ruleType).filter(Boolean))
    return [
      { value: '', label: t('filters.allRules') },
      ...[...rules].sort().map((rule) => ({ value: rule, label: ruleLabel(t, rule) })),
    ]
  }, [alerts, t])

  const groupBy: DataTableGroup<Alert> | undefined =
    filters.group === 'none'
      ? undefined
      : {
          key: (alert) => groupKey(alert, filters.group),
          label: (key, group) => {
            const name =
              filters.group === 'server' ? group[0]?.serverName || key : ruleLabel(t, key)
            const counts = severityCountsText(group, t)
            return counts === '' ? name : `${name} · ${counts}`
          },
        }

  const changeFilter = (key: keyof InboxFilterValues, value: string) => {
    if (key === 'group') {
      setQuery({ group: value === 'none' ? null : value })
      return
    }
    if (key === 'severity') {
      setQuery({ severity: value === 'all' ? null : value })
      return
    }
    if (key === 'state') {
      setQuery({ state: value === 'all' ? null : value })
      return
    }
    setQuery({ [key]: value === '' ? null : value })
  }

  const clearFilters = () => {
    setQuery({
      severity: null,
      state: null,
      server: null,
      rule: null,
      q: null,
      group: null,
    })
  }

  const openServer = (alert: Alert) => {
    if (!alert.serverId) return
    if (onOpenServer) onOpenServer(alert.serverId, alert.serverName || alert.serverId)
    else navigate(`/servers/${encodeURIComponent(alert.serverId)}`)
  }

  const openFocus = (alert: Alert) => {
    const params = new URLSearchParams(window.location.search)
    params.set('focus', alert.id)
    navigate(`${window.location.pathname}?${params.toString()}${window.location.hash}`)
  }

  const closeDrawer = () => setQuery({ focus: null })
  const refreshDrawer = () => {
    void load()
    onRefresh()
  }

  const runSingle = async (action: () => Promise<void>) => {
    setBusy(true)
    setError(null)
    try {
      await action()
      await load()
      onRefresh()
    } catch (cause) {
      if (alive.current) setError(errorMessage(t, cause))
    } finally {
      if (alive.current) setBusy(false)
    }
  }

  const runBulk = async (
    action: (alert: Alert) => Promise<void>,
    targets: Alert[],
    skipped: number,
  ) => {
    setBusy(true)
    setError(null)
    let done = 0
    let failed = 0
    for (const alert of targets) {
      try {
        await action(alert)
        done += 1
      } catch {
        failed += 1
      }
    }
    await load()
    onRefresh()
    setSelected(new Set())
    setBusy(false)
    toast[failed > 0 ? 'error' : 'success'](t('bulk.result', { done, failed, skipped }))
  }

  const columns: DataTableColumn<Alert>[] = [
    {
      id: 'severity',
      header: t('columns.severity'),
      cell: (alert) => <SeverityTag severity={severityOf(alert.severity)} />,
    },
    {
      id: 'what',
      header: t('columns.what'),
      cell: (alert) => (
        <span
          className={
            alertState(alert) === 'new'
              ? 'alerts-inbox__message alerts-inbox__message--new'
              : 'alerts-inbox__message'
          }
        >
          <AlertMessage alert={alert} />
        </span>
      ),
    },
    {
      id: 'where',
      header: t('columns.where'),
      cell: (alert) => (
        <span className="alerts-inbox__where">
          {alert.serverId ? (
            <Link to={`/servers/${encodeURIComponent(alert.serverId)}`}>
              {alert.serverName || alert.serverId}
            </Link>
          ) : (
            alert.serverName
          )}
          <span className="alerts-inbox__where-extra">
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
                <span className="alerts-inbox__container">{alert.subjectName}</span>
              </>
            ) : null}
          </span>
        </span>
      ),
    },
    {
      id: 'since',
      header: t('columns.since'),
      cell: (alert) => {
        const started = new Date(alert.startedAt)
        return (
          <time dateTime={alert.startedAt} title={exactDate(alert.startedAt, language)}>
            {formatRelativeTime(started, new Date(), language)}
          </time>
        )
      },
    },
    {
      id: 'state',
      header: t('columns.state'),
      cell: (alert) => {
        const state = alertState(alert)
        if (state === 'new') {
          return (
            <span className="alerts-inbox__state alerts-inbox__state--new">{t('state.new')}</span>
          )
        }
        if (state === 'silenced') {
          return (
            <span className="alerts-inbox__state alerts-inbox__state--silenced">
              {t('state.silenced')}
            </span>
          )
        }
        return (
          <span className="alerts-inbox__state alerts-inbox__state--seen">
            {alert.ackedBy ? t('state.seenBy', { user: alert.ackedBy }) : t('state.seen')}
          </span>
        )
      },
    },
    {
      id: 'actions',
      header: t('columns.actions'),
      align: 'right',
      width: 56,
      cell: (alert) => (
        <AlertMenu
          alert={alert}
          can={can}
          onAcknowledge={(target) => void runSingle(() => api.acknowledge(target.id))}
          onSilence={setSilence}
          onUnwatch={setUnwatch}
          onOpenServer={openServer}
        />
      ),
    },
  ]

  if (alerts === null && loading) {
    return (
      <section className="alerts-inbox" aria-busy="true">
        <p className="ui-sr-only" role="status">
          {t('loading')}
        </p>
        <DataTable columns={columns} rows={[]} rowKey={(alert) => alert.id} loading />
      </section>
    )
  }

  if (alerts === null && error !== null) {
    return (
      <section className="alerts-inbox">
        <div className="alerts__error" role="alert">
          <p>{error}</p>
          <button
            type="button"
            className="ui-btn"
            onClick={() => {
              setLoading(true)
              void load()
            }}
          >
            {t('retry')}
          </button>
        </div>
      </section>
    )
  }

  const changed = filtersChanged(filters)
  const showFilters = (alerts?.length ?? 0) > 0 || changed

  return (
    <section className="alerts-inbox">
      {error !== null ? (
        <p className="alerts__error" role="alert">
          {error}
        </p>
      ) : null}

      {showFilters ? (
        <InboxFilters
          filters={filters}
          servers={serverOptions}
          rules={ruleOptions}
          changed={changed}
          onChange={changeFilter}
          onClear={clearFilters}
        />
      ) : null}

      <BulkActions
        selected={selectedAlerts}
        can={can}
        disabled={busy}
        onClear={() => setSelected(new Set())}
        onAcknowledge={(targets) =>
          void runBulk(
            (alert) => api.acknowledge(alert.id),
            targets,
            selectedAlerts.length - targets.length,
          )
        }
        onSilence={(targets) =>
          setBulkSilence({
            alerts: targets,
            skipped: selectedAlerts.length - targets.length,
          })
        }
        onUnwatch={(targets) =>
          setBulkUnwatch({
            alerts: targets,
            skipped: selectedAlerts.length - targets.length,
          })
        }
      />

      {alerts !== null && alerts.length === 0 && !changed ? (
        <div className="alerts__empty alerts-inbox__all-clear">
          <span className="alerts__empty-mark" aria-hidden="true">
            ✓
          </span>
          <h2>{t('empty.allClear')}</h2>
          <p>{t('empty.body')}</p>
        </div>
      ) : (
        <DataTable
          columns={columns}
          rows={rows}
          rowKey={(alert) => alert.id}
          selectable={canAnywhere('alerts.silence')}
          selected={selected}
          onSelectionChange={setSelected}
          onRowClick={openFocus}
          rowClassName={(alert) => {
            const classes = [
              alert.id === focus ? 'alerts-inbox__row--focus' : '',
              alertState(alert) === 'new' ? 'alerts-inbox__row--new' : '',
            ]
            return classes.filter(Boolean).join(' ') || undefined
          }}
          groupBy={groupBy}
          empty={
            <div className="alerts__empty alerts-inbox__no-results">
              <h2>{t('empty.noResults')}</h2>
              <p>{t('empty.noResultsBody')}</p>
              {changed ? (
                <button type="button" className="ui-btn" onClick={clearFilters}>
                  {t('filters.clear')}
                </button>
              ) : null}
            </div>
          }
        />
      )}

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
          title={t('unwatch.title', { subject: unwatch.subjectName })}
          onClose={() => setUnwatch(null)}
          footer={
            <div className="alerts__actions">
              <button type="button" className="cbtn" onClick={() => setUnwatch(null)}>
                {t('actions.cancel')}
              </button>
              <button
                type="button"
                className="cbtn cbtn--danger"
                disabled={busy}
                onClick={() => {
                  const target = unwatch
                  setUnwatch(null)
                  void runSingle(() => api.unwatch(target.id))
                }}
              >
                {t('unwatch.confirm')}
              </button>
            </div>
          }
        >
          <p>{t('unwatch.body')}</p>
        </Dialog>
      ) : null}

      {bulkSilence !== null ? (
        <BulkSilenceDialog
          alerts={bulkSilence.alerts}
          api={api}
          onClose={() => setBulkSilence(null)}
          onFinished={(result) => {
            const skipped = bulkSilence.skipped
            setBulkSilence(null)
            void (async () => {
              await load()
              onRefresh()
              setSelected(new Set())
              toast[result.failed > 0 ? 'error' : 'success'](
                t('bulk.result', {
                  done: result.done,
                  failed: result.failed,
                  skipped,
                }),
              )
            })()
          }}
        />
      ) : null}

      {bulkUnwatch !== null ? (
        <ConfirmDialog
          open
          title={t('bulk.unwatchTitle', { count: bulkUnwatch.alerts.length })}
          body={
            <>
              <p>{t('bulk.unwatchBody', { count: bulkUnwatch.alerts.length })}</p>
              {bulkUnwatch.skipped > 0 ? (
                <p className="alerts__warning">
                  {t('bulk.notAllowed', { count: bulkUnwatch.skipped })}
                </p>
              ) : null}
            </>
          }
          confirmLabel={t('actions.unwatch')}
          danger
          requireText={String(bulkUnwatch.alerts.length)}
          onConfirm={() => {
            const targets = bulkUnwatch.alerts
            const skipped = bulkUnwatch.skipped
            setBulkUnwatch(null)
            void runBulk((alert) => api.unwatch(alert.id), targets, skipped)
          }}
          onCancel={() => setBulkUnwatch(null)}
        />
      ) : null}

      <AlertDrawer
        key={drawerAlert?.id ?? (focusMissing ? 'missing' : 'none')}
        alert={drawerAlert}
        missing={focusMissing}
        api={api}
        alertsAdminApi={alertsAdminApi}
        metricsApi={metricsApi}
        containersApi={containersApi}
        onClose={closeDrawer}
        onRefresh={refreshDrawer}
        pollMs={pollMs}
      />
    </section>
  )
}
