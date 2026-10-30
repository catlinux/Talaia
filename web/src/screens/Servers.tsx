import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'

import type { AlertsApi, AlertSummary } from '../api/alerts'
import { createApiFleetClient, type FleetClient } from '../api/client'
import { isSignedOut } from '../api/http'
import type { Server, ServerStatus } from '../api/types'
import { usePermissions } from '../auth/permissions'
import {
  DataTable,
  FilterBar,
  Menu,
  PageHeader,
  Select,
  StatusBadge,
  type DataTableColumn,
  type MenuEntry,
  type Status,
} from '../components/ui'
import { WatchtowerArt } from '../components/WatchtowerArt'
import { Link, setQuery, useNavigate, useRoute } from '../router'
import { AddServer } from './fleet/AddServer'
import { ServerCard } from './fleet/ServerCard'
import { applyServerFilters, parseServerFilters, type ServerSort } from './fleet/serverFilters'
import { formatNumber, formatPercent, formatTime } from './fleet/format'

export const refreshInterval = 15_000
const alertsRefreshInterval = 30_000
const SERVERS_VIEW_STORAGE_KEY = 'talaia.serversView'

type ServersView = 'table' | 'cards'
type ServersState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; servers: Server[]; loadedAt: Date }

const statusByServer: Record<ServerStatus, Status> = {
  online: 'ok',
  warning: 'warn',
  critical: 'crit',
  offline: 'offline',
}

const defaultClient = createApiFleetClient()

function readView(): ServersView {
  try {
    return localStorage.getItem(SERVERS_VIEW_STORAGE_KEY) === 'cards' ? 'cards' : 'table'
  } catch {
    return 'table'
  }
}

function storeView(view: ServersView): void {
  try {
    localStorage.setItem(SERVERS_VIEW_STORAGE_KEY, view)
  } catch {
    // The view still works for this session when storage is unavailable.
  }
}

interface ServersProps {
  client?: FleetClient
  alertsApi?: AlertsApi
}

function UsageCell({
  value,
  label,
  language,
}: {
  value: number | null
  label: string
  language: string
}) {
  const { t } = useTranslation('fleet')
  const known = value !== null
  const text = known ? formatPercent(value, language) : '—'
  const tone = known
    ? value >= 90
      ? 'servers__usage--crit'
      : value >= 80
        ? 'servers__usage--warn'
        : ''
    : ''

  return (
    <span className={`servers__usage ${tone}`}>
      <span
        className="servers__usage-track"
        role="meter"
        aria-label={t('metric.aria', { label, value: text })}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={known ? Math.round(value) : undefined}
        aria-valuetext={text}
      >
        <span className="servers__usage-fill" style={{ width: `${known ? value : 0}%` }} />
      </span>
      <span className="servers__usage-value">{text}</span>
    </span>
  )
}

function RowMenu({ server, canAlerts }: { server: Server; canAlerts: boolean }) {
  const { t } = useTranslation('fleet')
  const navigate = useNavigate()
  const base = `/servers/${encodeURIComponent(server.id)}`
  const items: MenuEntry[] = [
    { id: 'open', label: t('servers.menu.open'), onSelect: () => navigate(base) },
    {
      id: 'containers',
      label: t('servers.menu.containers'),
      onSelect: () => navigate(`${base}/containers`),
    },
    {
      id: 'updates',
      label: t('servers.menu.updates'),
      onSelect: () => navigate(`${base}/updates`),
    },
  ]
  if (canAlerts) {
    items.push({
      id: 'alerts',
      label: t('servers.menu.alerts'),
      onSelect: () => navigate(`/alerts?server=${encodeURIComponent(server.id)}`),
    })
  }

  return (
    <Menu
      align="end"
      items={items}
      trigger={(props) => (
        <button
          {...props}
          className="servers__menu"
          aria-label={t('servers.rowActions', { name: server.name })}
        >
          <span aria-hidden="true">⋯</span>
        </button>
      )}
    />
  )
}

/** Servers is the filterable table (and optional card view) of the fleet. */
export function Servers({ client = defaultClient, alertsApi }: ServersProps) {
  const { t, i18n } = useTranslation('fleet')
  const language = i18n.resolvedLanguage ?? i18n.language
  const { can, canAnywhere } = usePermissions()
  const canEnroll = can('server.enroll')
  const canAlerts = canAnywhere('alerts.view')
  const navigate = useNavigate()
  const { query } = useRoute()
  const filters = parseServerFilters(query)

  const [view, setView] = useState<ServersView>(readView)
  const [state, setState] = useState<ServersState>({ status: 'loading' })
  const [refreshFailed, setRefreshFailed] = useState(false)
  const [alertCounts, setAlertCounts] = useState<Map<string, number>>(new Map())
  const [alertSummary, setAlertSummary] = useState<AlertSummary | null>(null)

  const loadServers = useCallback(async () => {
    try {
      const servers = await client.listServers()
      setState({ status: 'ready', servers, loadedAt: new Date() })
      setRefreshFailed(false)
    } catch (error) {
      if (isSignedOut(error)) return
      setState((current) => (current.status === 'ready' ? current : { status: 'error' }))
      setRefreshFailed(true)
    }
  }, [client])

  const loadAlerts = useCallback(async () => {
    if (!canAlerts || alertsApi === undefined) return
    try {
      const [active, summary] = await Promise.all([alertsApi.list('active'), alertsApi.summary()])
      const counts = new Map<string, number>()
      for (const item of active) {
        if (item.serverId !== null) {
          counts.set(item.serverId, (counts.get(item.serverId) ?? 0) + 1)
        }
      }
      setAlertCounts(counts)
      setAlertSummary(summary)
    } catch (error) {
      if (isSignedOut(error)) return
      setAlertCounts(new Map())
      setAlertSummary(null)
    }
  }, [alertsApi, canAlerts])

  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const servers = await client.listServers()
        if (cancelled) return
        setState({ status: 'ready', servers, loadedAt: new Date() })
        setRefreshFailed(false)
      } catch (error) {
        if (cancelled) return
        if (isSignedOut(error)) return
        setState((current) => (current.status === 'ready' ? current : { status: 'error' }))
        setRefreshFailed(true)
      }
    }
    void load()
    const timer = window.setInterval(() => {
      if (!document.hidden) void load()
    }, refreshInterval)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [client])

  useEffect(() => {
    if (!canAlerts || alertsApi === undefined) return
    queueMicrotask(() => {
      void loadAlerts()
    })
    const timer = window.setInterval(() => {
      if (!document.hidden) void loadAlerts()
    }, alertsRefreshInterval)
    return () => window.clearInterval(timer)
  }, [alertsApi, canAlerts, loadAlerts])

  const retry = useCallback(() => {
    setState({ status: 'loading' })
    setRefreshFailed(false)
    void loadServers()
    void loadAlerts()
  }, [loadAlerts, loadServers])

  const alertsAvailable = canAlerts && alertsApi !== undefined
  const visibleCounts = alertsAvailable ? alertCounts : new Map<string, number>()
  const visibleSummary = alertsAvailable ? alertSummary : null

  const allTags =
    state.status === 'ready'
      ? [...new Set(state.servers.flatMap((server) => server.tags))].sort((left, right) =>
          left.localeCompare(right),
        )
      : []
  const filtered =
    state.status === 'ready' ? applyServerFilters(state.servers, filters, visibleCounts) : []

  const statusOptions = [
    { value: '', label: t('servers.filters.allStatuses') },
    { value: 'online', label: t('servers.filters.online') },
    { value: 'warning', label: t('status.warning') },
    { value: 'critical', label: t('status.critical') },
    { value: 'offline', label: t('status.offline') },
  ]
  const tagOptions = [
    { value: '', label: t('servers.filters.allTags') },
    ...allTags.map((tag) => ({ value: tag, label: tag })),
  ]

  const clearFilters = () => {
    setQuery({ q: null, status: null, tag: null, updates: null, reboot: null })
  }

  const chooseView = (next: ServersView) => {
    setView(next)
    storeView(next)
  }

  const changeSort = (next: { id: string; dir: 'asc' | 'desc' }) => {
    setQuery({ sort: `${next.id}:${next.dir}` })
  }

  const subtitle =
    state.status === 'ready'
      ? t('subtitle', {
          count: state.servers.length,
          time: formatTime(state.loadedAt, language),
        })
      : undefined

  const actions = (
    <>
      <div className="servers__view" role="group" aria-label={t('servers.view.label')}>
        <button
          type="button"
          className={
            view === 'table'
              ? 'servers__view-button servers__view-button--on'
              : 'servers__view-button'
          }
          aria-pressed={view === 'table'}
          onClick={() => chooseView('table')}
        >
          {t('servers.view.table')}
        </button>
        <button
          type="button"
          className={
            view === 'cards'
              ? 'servers__view-button servers__view-button--on'
              : 'servers__view-button'
          }
          aria-pressed={view === 'cards'}
          onClick={() => chooseView('cards')}
        >
          {t('servers.view.cards')}
        </button>
      </div>
      {canEnroll ? (
        <AddServer create={client.createEnrollment} initialOpen={query.get('add') === '1'} />
      ) : null}
    </>
  )

  const emptyFleet = (
    <div className="servers__empty">
      <WatchtowerArt className="servers__empty-art" />
      <p className="servers__empty-title">{t('empty.title')}</p>
      <p>{t('empty.body')}</p>
    </div>
  )

  const noMatches = (
    <div className="servers__no-match">
      <p>{t('servers.empty')}</p>
      <button type="button" className="ui-btn" onClick={clearFilters}>
        {t('servers.clearFilters')}
      </button>
    </div>
  )

  if (state.status === 'loading') {
    return (
      <section className="servers-screen" aria-labelledby="servers-title">
        <PageHeader title={t('servers.title')} actions={actions} />
        <p className="ui-sr-only" role="status">
          {t('servers.loading')}
        </p>
        <section className="servers__panel" aria-label={t('servers.title')}>
          <ServersTable
            servers={[]}
            loading
            columns={columnsFor({
              t,
              language,
              alertCounts: visibleCounts,
              alertSummary: visibleSummary,
              canAlerts: alertsAvailable,
            })}
            empty={emptyFleet}
            sort={filters.sort}
            onSortChange={changeSort}
            onOpen={(server) => navigate(`/servers/${encodeURIComponent(server.id)}`)}
          />
        </section>
      </section>
    )
  }

  if (state.status === 'error') {
    return (
      <section className="servers-screen" aria-labelledby="servers-title">
        <PageHeader title={t('servers.title')} actions={actions} />
        <div className="servers__error" role="alert">
          <p>{t('servers.error')}</p>
          <button type="button" className="ui-btn" onClick={retry}>
            {t('servers.retry')}
          </button>
        </div>
      </section>
    )
  }

  return (
    <section className="servers-screen" aria-labelledby="servers-title">
      <PageHeader title={t('servers.title')} subtitle={subtitle} actions={actions} />
      {refreshFailed ? (
        <p className="servers__notice" role="status">
          {t('servers.refreshFailed')}
        </p>
      ) : null}
      <section className="servers__panel" aria-label={t('servers.title')}>
        {state.servers.length > 0 ? (
          <FilterBar
            search={{
              value: filters.q,
              onChange: (value) => setQuery({ q: value }),
              placeholder: t('servers.searchPlaceholder'),
            }}
          >
            <Select
              label={t('servers.filters.status')}
              value={filters.status}
              options={statusOptions}
              onChange={(value) => setQuery({ status: value === '' ? null : value })}
            />
            <Select
              label={t('servers.filters.tag')}
              value={filters.tag}
              options={tagOptions}
              onChange={(value) => setQuery({ tag: value === '' ? null : value })}
            />
            <label className="servers__toggle">
              <input
                type="checkbox"
                checked={filters.securityOnly}
                onChange={(event) =>
                  setQuery({ updates: event.target.checked ? 'security' : null })
                }
              />
              <span>{t('servers.filters.security')}</span>
            </label>
            <label className="servers__toggle">
              <input
                type="checkbox"
                checked={filters.rebootOnly}
                onChange={(event) => setQuery({ reboot: event.target.checked ? '1' : null })}
              />
              <span>{t('servers.filters.reboot')}</span>
            </label>
          </FilterBar>
        ) : null}

        {state.servers.length === 0 ? (
          emptyFleet
        ) : filtered.length === 0 ? (
          noMatches
        ) : view === 'cards' ? (
          <ul className="servers servers--cards">
            {filtered.map((server) => (
              <ServerCard
                key={server.id}
                server={server}
                onOpen={(target) => navigate(`/servers/${encodeURIComponent(target.id)}`)}
                alertSeverity={visibleSummary?.servers[server.id]}
                alertCount={visibleCounts.get(server.id) ?? 0}
              />
            ))}
          </ul>
        ) : (
          <ServersTable
            servers={filtered}
            columns={columnsFor({
              t,
              language,
              alertCounts: visibleCounts,
              alertSummary: visibleSummary,
              canAlerts: alertsAvailable,
            })}
            empty={noMatches}
            sort={filters.sort}
            onSortChange={changeSort}
            onOpen={(server) => navigate(`/servers/${encodeURIComponent(server.id)}`)}
          />
        )}
      </section>
    </section>
  )
}

interface ColumnsContext {
  t: TFunction<'fleet'>
  language: string
  alertCounts: ReadonlyMap<string, number>
  alertSummary: AlertSummary | null
  canAlerts: boolean
}

function columnsFor(context: ColumnsContext): DataTableColumn<Server>[] {
  const { t, language, alertCounts, alertSummary, canAlerts } = context
  const dash = <span className="servers__muted">—</span>
  // Rows arrive already sorted by serverFilters. The constant keeps the header
  // buttons interactive without letting DataTable override that order.
  const sortable = () => 0
  const detailLabel = (server: Server) =>
    server.statusDetailKey
      ? t(server.statusDetailKey, {
          percent: formatPercent(server.diskPercent ?? 0, language),
        })
      : undefined

  return [
    {
      id: 'status',
      header: t('servers.columns.status'),
      sortValue: sortable,
      cell: (server) => (
        <StatusBadge status={statusByServer[server.status]} label={detailLabel(server)} />
      ),
    },
    {
      id: 'name',
      header: t('servers.columns.name'),
      sortValue: sortable,
      cell: (server) => (
        <span className="servers__name">
          <Link to={`/servers/${encodeURIComponent(server.id)}`}>{server.name}</Link>
          {server.limited !== true &&
          server.hostname !== undefined &&
          server.hostname !== '' &&
          server.hostname !== server.name ? (
            <span className="servers__hostname">{server.hostname}</span>
          ) : null}
        </span>
      ),
    },
    {
      id: 'os',
      header: t('servers.columns.os'),
      hideOnMobile: true,
      sortValue: sortable,
      cell: (server) =>
        server.limited
          ? dash
          : [server.os, server.location].filter((value) => value !== '').join(' · ') || dash,
    },
    {
      id: 'cpu',
      header: t('servers.columns.cpu'),
      sortValue: sortable,
      cell: (server) =>
        server.limited ? (
          dash
        ) : (
          <UsageCell value={server.cpuPercent} label={t('metric.cpu')} language={language} />
        ),
    },
    {
      id: 'ram',
      header: t('servers.columns.ram'),
      sortValue: sortable,
      cell: (server) =>
        server.limited ? (
          dash
        ) : (
          <UsageCell value={server.memPercent} label={t('metric.ram')} language={language} />
        ),
    },
    {
      id: 'disk',
      header: t('servers.columns.disk'),
      sortValue: sortable,
      cell: (server) =>
        server.limited ? (
          dash
        ) : (
          <UsageCell value={server.diskPercent} label={t('metric.disk')} language={language} />
        ),
    },
    {
      id: 'containers',
      header: t('servers.columns.containers'),
      sortValue: sortable,
      align: 'right',
      cell: (server) =>
        server.limited
          ? dash
          : `${formatNumber(server.runningContainers ?? server.containers, language)} / ${formatNumber(server.containers, language)}`,
    },
    {
      id: 'alerts',
      header: t('servers.columns.alerts'),
      sortValue: sortable,
      align: 'right',
      cell: (server) => {
        if (server.limited) return dash
        const count = alertCounts.get(server.id) ?? 0
        if (count === 0) return dash
        const severity = alertSummary?.servers[server.id]
        return (
          <Link
            className={
              severity === undefined
                ? 'servers__alerts'
                : `servers__alerts servers__alerts--${severity}`
            }
            to={`/alerts?server=${encodeURIComponent(server.id)}`}
          >
            {formatNumber(count, language)}
          </Link>
        )
      },
    },
    {
      id: 'updates',
      header: t('servers.columns.updates'),
      hideOnMobile: true,
      sortValue: sortable,
      cell: (server) => {
        if (server.limited) return dash
        const security = server.apt?.security ?? 0
        const upgrades = server.apt?.upgradable ?? 0
        const text =
          security > 0
            ? t('servers.securityUpdates', { count: security })
            : upgrades > 0
              ? t('servers.updates', { count: upgrades })
              : ''
        return (
          <span className="servers__updates">
            {text !== '' ? (
              <span className={security > 0 ? 'servers__security' : ''}>{text}</span>
            ) : null}
            {server.apt?.rebootRequired ? (
              <span className="servers__reboot">{t('servers.reboot')}</span>
            ) : null}
            {text === '' && !server.apt?.rebootRequired ? dash : null}
          </span>
        )
      },
    },
    {
      id: 'agent',
      header: t('servers.columns.agent'),
      hideOnMobile: true,
      sortValue: sortable,
      cell: (server) => {
        if (server.limited) return dash
        return (
          <span className="servers__agent">
            <span className="servers__agent-version">{server.agentVersion || dash}</span>
            {server.agentUpdateAvailable ? (
              <span className="servers__tag">{t('server.updateAvailable')}</span>
            ) : null}
            {server.agentUpdateManual ? (
              <span className="servers__tag servers__tag--manual">{t('server.manualUpdate')}</span>
            ) : null}
          </span>
        )
      },
    },
    {
      id: 'actions',
      header: '',
      align: 'right',
      cell: (server) => (server.limited ? dash : <RowMenu server={server} canAlerts={canAlerts} />),
    },
  ]
}

function ServersTable({
  servers,
  columns,
  loading = false,
  empty,
  sort,
  onSortChange,
  onOpen,
}: {
  servers: Server[]
  columns: DataTableColumn<Server>[]
  loading?: boolean
  empty: ReactNode
  sort: ServerSort
  onSortChange: (sort: { id: string; dir: 'asc' | 'desc' }) => void
  onOpen: (server: Server) => void
}) {
  return (
    <div className="servers__table">
      <DataTable
        columns={columns}
        rows={servers}
        rowKey={(server) => server.id}
        loading={loading}
        empty={empty}
        sort={sort}
        onSortChange={onSortChange}
        onRowClick={onOpen}
      />
    </div>
  )
}
