import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { StackAction } from '../api/containers'
import { createContainersApi } from '../api/containers'
import { createApiFleetClient } from '../api/client'
import { isSignedOut } from '../api/http'
import type { Server } from '../api/types'
import { errorMessage } from '../auth/AuthGate'
import { usePermissions } from '../auth/permissions'
import {
  BulkBar,
  ConfirmDialog,
  DataTable,
  FilterBar,
  Menu,
  PageHeader,
  Select,
  StatusBadge,
  useToast,
  type DataTableColumn,
  type MenuEntry,
  type Status,
} from '../components/ui'
import { Link, useSetQuery, useRoute } from '../router'
import { formatTime } from './fleet/format'
import { stackPath } from './server/serverTabs'
import { TimedOut, waitForJob } from './server/waitForJob'
import { stackBulkAvailability, type StackBulkAction } from './fleetwide/bulk'
import {
  loadFleet,
  type FleetAdoptableRow,
  type FleetLoad,
  type FleetStackRow,
} from './fleetwide/loadFleet'
import { runBulkJobs } from './fleetwide/runBulk'
import { fleetRefreshMs, type ContainersProps } from './Containers'

export const refreshInterval = fleetRefreshMs

type StatusFilter = 'all' | 'running' | 'problems'

interface Filters {
  q: string
  server: string
  status: StatusFilter
}

interface StackRowData extends FleetStackRow {
  key: string
}

function stackKey(row: FleetStackRow): string {
  return JSON.stringify([row.serverId, row.stack])
}

function rowData(row: FleetStackRow): StackRowData {
  return { ...row, key: stackKey(row) }
}

function healthOf(row: FleetStackRow): Status {
  const running = row.containers.filter((container) => container.state === 'running').length
  if (row.containers.length > 0 && running === row.containers.length) return 'ok'
  return running > 0 ? 'warn' : 'crit'
}

function parseFilters(query: URLSearchParams): Filters {
  const status = query.get('status')
  return {
    q: query.get('q') ?? '',
    server: query.get('server') ?? '',
    status: status === 'running' || status === 'problems' || status === 'all' ? status : 'all',
  }
}

function defaultOrder(a: StackRowData, b: StackRowData): number {
  const server = a.serverName.localeCompare(b.serverName)
  if (server !== 0) return server
  return a.stack.localeCompare(b.stack)
}

/** keepFailedServers keeps the previous rows of a server that could not be read again. */
function keepFailedServers(previous: FleetLoad | null, next: FleetLoad): FleetLoad {
  if (previous === null || next.failed.length === 0) return next
  const failed = new Set(next.failed.map((row) => row.serverId))
  const containers = new Set(next.containers.map((row) => row.serverId))
  const stacks = new Set(next.stacks.map((row) => row.serverId))
  const adoptable = new Set(next.adoptable.map((row) => row.serverId))
  return {
    ...next,
    containers: [
      ...next.containers,
      ...previous.containers.filter(
        (row) => failed.has(row.serverId) && !containers.has(row.serverId),
      ),
    ],
    stacks: [
      ...next.stacks,
      ...previous.stacks.filter((row) => failed.has(row.serverId) && !stacks.has(row.serverId)),
    ],
    adoptable: [
      ...next.adoptable,
      ...previous.adoptable.filter(
        (row) => failed.has(row.serverId) && !adoptable.has(row.serverId),
      ),
    ],
  }
}

interface StackMenuProps {
  row: StackRowData
  onAction: (row: StackRowData, action: StackAction) => void
  onDown: (row: StackRowData) => void
}

function StackMenu({ row, onAction, onDown }: StackMenuProps) {
  const { t } = useTranslation('fleetwide')
  const { can } = usePermissions()
  const scope = { serverId: row.serverId, stack: row.stack }
  if (!can('stack.deploy', scope)) return <span className="fleetwide__muted">—</span>

  const items: MenuEntry[] = [
    {
      id: 'up',
      label: t('common.action.up'),
      onSelect: () => onAction(row, 'up'),
    },
    {
      id: 'restart',
      label: t('common.action.restart'),
      onSelect: () => onAction(row, 'restart'),
    },
    {
      id: 'pull',
      label: t('common.action.pull'),
      onSelect: () => onAction(row, 'pull'),
    },
    { separator: true },
    {
      id: 'down',
      label: t('common.action.down'),
      danger: true,
      onSelect: () => onDown(row),
    },
  ]

  return (
    <Menu
      align="end"
      items={items}
      trigger={(props) => (
        <button
          {...props}
          className="ui-btn ui-btn--ghost fleetwide__menu-trigger"
          aria-label={t('common.rowActions', { name: row.stack })}
        >
          ⋯
        </button>
      )}
    />
  )
}

/** Stacks is the fleet-wide list of every managed stack. */
export function Stacks({
  client,
  containersApi,
  pollMs = 800,
  refreshMs = fleetRefreshMs,
}: ContainersProps) {
  const { t, i18n } = useTranslation('fleetwide')
  const toast = useToast()
  const { query } = useRoute()
  const setQuery = useSetQuery()
  const { can, canAnywhere, canSeeStacks, loading: permissionsLoading } = usePermissions()
  const fleetClient = useMemo(() => client ?? createApiFleetClient(), [client])
  const api = useMemo(() => containersApi ?? createContainersApi(), [containersApi])
  const filters = parseFilters(query)
  const language = i18n.resolvedLanguage ?? i18n.language

  const [data, setData] = useState<FleetLoad | null>(null)
  const dataRef = useRef<FleetLoad | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [refreshFailed, setRefreshFailed] = useState(false)
  const [loadedAt, setLoadedAt] = useState<Date | null>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [downing, setDowning] = useState<StackRowData | null>(null)
  const [bulkConfirm, setBulkConfirm] = useState<{
    action: StackBulkAction
    rows: StackRowData[]
  } | null>(null)
  const alive = useRef(true)
  const request = useRef(0)

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const canLoad = useCallback((server: Server) => canSeeStacks(server.id), [canSeeStacks])

  const load = useCallback(async () => {
    const current = request.current + 1
    request.current = current
    try {
      const next = keepFailedServers(
        dataRef.current,
        await loadFleet(fleetClient, api, {
          canLoad,
          canLoadStacks: canLoad,
        }),
      )
      if (!alive.current || current !== request.current) return
      dataRef.current = next
      setData(next)
      setLoadedAt(new Date())
      setLoadError(false)
      setRefreshFailed(false)
      setLoading(false)
    } catch {
      if (!alive.current || current !== request.current) return
      if (dataRef.current === null) setLoadError(true)
      else setRefreshFailed(true)
      setLoading(false)
    }
  }, [api, canLoad, fleetClient])

  useEffect(() => {
    if (permissionsLoading) return
    queueMicrotask(() => {
      void load()
    })
    const timer = window.setInterval(() => {
      if (!document.hidden) void load()
    }, refreshMs)
    return () => window.clearInterval(timer)
  }, [load, permissionsLoading, refreshMs])

  const filtersChanged = filters.q !== '' || filters.server !== '' || filters.status !== 'all'

  const serverOptions = useMemo(() => {
    const options = [{ value: '', label: t('stacks.filters.allServers') }]
    for (const server of data?.servers ?? []) {
      if (!canSeeStacks(server.id)) continue
      options.push({ value: server.id, label: server.name })
    }
    return options
  }, [canSeeStacks, data, t])

  const search = filters.q.trim().toLocaleLowerCase()
  const rows = useMemo(() => {
    return (data?.stacks ?? [])
      .map(rowData)
      .filter((row) => {
        if (filters.server !== '' && row.serverId !== filters.server) return false
        const health = healthOf(row)
        if (filters.status === 'running' && health !== 'ok') return false
        if (filters.status === 'problems' && health === 'ok') return false
        if (search === '') return true
        return [row.stack, row.serverName].some((value) =>
          value.toLocaleLowerCase().includes(search),
        )
      })
      .sort(defaultOrder)
  }, [data, filters.server, filters.status, search])

  const adoptable = useMemo(() => {
    return (data?.adoptable ?? []).filter((row) => {
      if (filters.server !== '' && row.serverId !== filters.server) return false
      if (search === '') return true
      return [row.name, row.serverName].some((value) => value.toLocaleLowerCase().includes(search))
    })
  }, [data, filters.server, search])

  const selectedRows = useMemo(() => {
    return (data?.stacks ?? []).map(rowData).filter((row) => selected.has(row.key))
  }, [data, selected])

  const availability = stackBulkAvailability(
    selectedRows.map((row) => ({ serverId: row.serverId, stack: row.stack })),
    can,
  )

  const submitAction = useCallback(
    async (row: StackRowData, action: StackAction, confirm = false) => {
      const actionLabel = t(`common.action.${action}`)
      try {
        const jobId = await api.runStack(row.serverId, row.stack, action, confirm)
        const job = await waitForJob(api, jobId, pollMs, () => alive.current)
        if (job === null) return
        if (job.state === 'succeeded') {
          toast.success(t('common.toast.done', { action: actionLabel, name: row.stack }))
        } else {
          toast.error(
            t('common.toast.failed', {
              action: actionLabel,
              name: row.stack,
              message: job.message,
            }),
          )
        }
      } catch (error) {
        if (!alive.current || isSignedOut(error)) return
        toast.error(
          t('common.toast.failed', {
            action: actionLabel,
            name: row.stack,
            message: error instanceof TimedOut ? t('common.toast.timeout') : errorMessage(t, error),
          }),
        )
      } finally {
        if (alive.current) void load()
      }
    },
    [api, load, pollMs, t, toast],
  )

  const down = useCallback(() => {
    const row = downing
    setDowning(null)
    if (row !== null) void submitAction(row, 'down', true)
  }, [downing, setDowning, submitAction])

  const runBulk = useCallback(
    async (action: StackBulkAction, targets: StackRowData[]) => {
      setBulkConfirm(null)
      const summary = await runBulkJobs(
        targets.map((row) => ({
          serverId: row.serverId,
          run: () => api.runStack(row.serverId, row.stack, action),
        })),
        api,
        pollMs,
        () => alive.current,
      )
      if (!alive.current) return
      toast[summary.failed > 0 ? 'error' : 'success'](
        t('common.toast.bulk', { done: summary.done, failed: summary.failed }),
      )
      setSelected(new Set())
      void load()
    },
    [api, load, pollMs, setBulkConfirm, setSelected, t, toast],
  )

  const clearFilters = () => {
    setQuery({ q: null, server: null, status: null })
  }

  const columns: DataTableColumn<StackRowData>[] = [
    {
      id: 'status',
      header: t('stacks.columns.status'),
      cell: (row) => {
        const health = healthOf(row)
        return (
          <StatusBadge
            status={health}
            label={t(
              health === 'ok'
                ? 'stacks.status.allRunning'
                : health === 'warn'
                  ? 'stacks.status.someStopped'
                  : 'stacks.status.stopped',
            )}
          />
        )
      },
    },
    {
      id: 'name',
      header: t('stacks.columns.name'),
      cell: (row) => (
        <Link to={stackPath(row.serverId, row.stack)} className="fleetwide__strong">
          {row.stack}
        </Link>
      ),
    },
    {
      id: 'server',
      header: t('stacks.columns.server'),
      cell: (row) => (
        <Link to={`/servers/${encodeURIComponent(row.serverId)}/containers`}>{row.serverName}</Link>
      ),
    },
    {
      id: 'containers',
      header: t('stacks.columns.containers'),
      cell: (row) => {
        const running = row.containers.filter((container) => container.state === 'running').length
        return t('stacks.containersValue', { running, total: row.containers.length })
      },
    },
    {
      id: 'flags',
      header: t('stacks.columns.flags'),
      hideOnMobile: true,
      cell: (row) => (
        <span className="fleetwide__tags">
          {row.adopted ? <span className="fleetwide__tag">{t('stacks.adopted')}</span> : null}
          {row.broken ? (
            <span className="fleetwide__tag fleetwide__tag--warning">{t('stacks.broken')}</span>
          ) : null}
          {!row.adopted && !row.broken ? <span className="fleetwide__muted">—</span> : null}
        </span>
      ),
    },
    {
      id: 'actions',
      header: t('stacks.columns.actions'),
      align: 'right',
      width: 56,
      cell: (row) => (
        <StackMenu
          row={row}
          onAction={(target, action) => void submitAction(target, action)}
          onDown={setDowning}
        />
      ),
    },
  ]

  const total = data?.stacks.length ?? 0
  const hasServers = (data?.servers.length ?? 0) > 0
  const hasAnything = total > 0 || (data?.adoptable.length ?? 0) > 0

  if (loading && data === null) {
    return (
      <section className="fleetwide">
        <PageHeader title={t('stacks.title')} />
        <p className="ui-sr-only" role="status">
          {t('stacks.loading')}
        </p>
        <DataTable
          columns={columns}
          rows={[]}
          rowKey={(row) => row.key}
          loading
          empty={t('stacks.loading')}
        />
      </section>
    )
  }

  if (loadError && data === null) {
    return (
      <section className="fleetwide">
        <PageHeader title={t('stacks.title')} />
        <div className="fleetwide__error" role="alert">
          <p>{t('common.loadError')}</p>
          <button type="button" className="ui-btn" onClick={() => void load()}>
            {t('common.retry')}
          </button>
        </div>
      </section>
    )
  }

  return (
    <section className="fleetwide">
      <PageHeader
        title={t('stacks.title')}
        subtitle={t('stacks.subtitle', {
          count: total,
          time: loadedAt === null ? '' : formatTime(loadedAt, language),
        })}
      />

      {refreshFailed ? (
        <div className="fleetwide__notice" role="status">
          {t('common.refreshFailed')}
        </div>
      ) : null}
      {(data?.failed.length ?? 0) > 0 ? (
        <div className="fleetwide__notice fleetwide__notice--warning" role="status">
          {t('common.failed', {
            count: data?.failed.length,
            servers: (data?.failed ?? []).map((row) => row.serverName).join(', '),
          })}
        </div>
      ) : null}
      {(data?.notConnected.length ?? 0) > 0 ? (
        <div className="fleetwide__notice" role="status">
          {t('common.notConnected', {
            count: data?.notConnected.length,
            servers: (data?.notConnected ?? []).join(', '),
          })}
        </div>
      ) : null}

      {!hasServers ? (
        <div className="fleetwide__empty">
          <h2>{t('common.noServersTitle')}</h2>
          <p>{t('common.noServersBody')}</p>
          <Link className="ui-btn" to="/servers">
            {t('common.goServers')}
          </Link>
        </div>
      ) : !hasAnything ? (
        <div className="fleetwide__empty">
          <h2>{t('stacks.emptyTitle')}</h2>
          <p>{t('stacks.emptyBody')}</p>
          <Link className="ui-btn" to="/servers">
            {t('common.goServers')}
          </Link>
        </div>
      ) : (
        <>
          <FilterBar
            search={{
              value: filters.q,
              onChange: (value) => setQuery({ q: value }),
              placeholder: t('stacks.filters.search'),
            }}
          >
            <Select
              label={t('stacks.filters.server')}
              value={filters.server}
              options={serverOptions}
              onChange={(value) => setQuery({ server: value || null })}
            />
            <Select
              label={t('stacks.filters.status')}
              value={filters.status}
              options={[
                { value: 'all', label: t('stacks.filters.allStatuses') },
                { value: 'running', label: t('stacks.filters.allRunning') },
                { value: 'problems', label: t('stacks.filters.problems') },
              ]}
              onChange={(value) => setQuery({ status: value === 'all' ? null : value })}
            />
          </FilterBar>

          <BulkBar count={selectedRows.length} onClear={() => setSelected(new Set())}>
            {(['restart', 'pull'] as const).map((action) => {
              const state = availability[action]
              const title = state.allowed
                ? undefined
                : t('stacks.bulk.notAllowed', { count: state.denied })
              return (
                <span key={action} className="fleetwide__bulk-action" title={title}>
                  <button
                    type="button"
                    className="ui-btn"
                    disabled={!state.allowed}
                    title={title}
                    onClick={() => setBulkConfirm({ action, rows: selectedRows })}
                  >
                    {t(`common.action.${action}`)}
                  </button>
                </span>
              )
            })}
          </BulkBar>

          {total > 0 ? (
            <DataTable
              columns={columns}
              rows={rows}
              rowKey={(row) => row.key}
              selectable={canAnywhere('stack.deploy')}
              selected={selected}
              onSelectionChange={setSelected}
              rowClassName={(row) => (row.broken ? 'fleetwide__row--broken' : undefined)}
              empty={
                <div className="fleetwide__empty">
                  <h2>{t('stacks.noResultsTitle')}</h2>
                  <p>{t('stacks.noResultsBody')}</p>
                  {filtersChanged ? (
                    <button type="button" className="ui-btn" onClick={clearFilters}>
                      {t('common.clearFilters')}
                    </button>
                  ) : null}
                </div>
              }
            />
          ) : (
            <div className="fleetwide__empty">
              <h2>{t('stacks.emptyTitle')}</h2>
              <p>{t('stacks.emptyBody')}</p>
              <Link className="ui-btn" to="/servers">
                {t('common.goServers')}
              </Link>
            </div>
          )}

          <Adoptable rows={adoptable} />
        </>
      )}

      <ConfirmDialog
        open={downing !== null}
        title={t('stacks.down.title', { name: downing?.stack ?? '' })}
        body={t('stacks.down.body')}
        confirmLabel={t('stacks.down.confirm')}
        danger
        requireText={downing?.stack ?? ''}
        onConfirm={down}
        onCancel={() => setDowning(null)}
      />

      <ConfirmDialog
        open={bulkConfirm !== null}
        title={t(`stacks.bulk.${bulkConfirm?.action ?? 'restart'}Title`, {
          count: bulkConfirm?.rows.length ?? 0,
        })}
        body={
          <>
            <p>{t(`stacks.bulk.${bulkConfirm?.action ?? 'restart'}Body`)}</p>
            <ul className="fleetwide__confirm-list">
              {(bulkConfirm?.rows ?? []).map((row) => (
                <li key={row.key}>{row.stack}</li>
              ))}
            </ul>
          </>
        }
        confirmLabel={t('common.bulk.confirm')}
        requireText={String(bulkConfirm?.rows.length ?? '')}
        onConfirm={() => {
          if (bulkConfirm !== null) void runBulk(bulkConfirm.action, bulkConfirm.rows)
        }}
        onCancel={() => setBulkConfirm(null)}
      />
    </section>
  )
}

function Adoptable({ rows }: { rows: FleetAdoptableRow[] }) {
  const { t } = useTranslation('fleetwide')
  if (rows.length === 0) return null
  return (
    <section className="fleetwide__adoptable" aria-labelledby="fleetwide-not-managed">
      <div className="fleetwide__adoptable-head">
        <h2 id="fleetwide-not-managed">{t('stacks.notManaged')}</h2>
        <p>{t('stacks.notManagedBody')}</p>
      </div>
      <ul className="fleetwide__adoptable-list">
        {rows.map((row) => (
          <li key={`${row.serverId}:${row.name}`}>
            <span className="fleetwide__strong">{row.name}</span>
            <span className="fleetwide__muted">{row.serverName}</span>
            <Link
              to={`/servers/${encodeURIComponent(row.serverId)}/containers`}
              className="ui-btn ui-btn--ghost"
            >
              {t('stacks.adopt')}
            </Link>
          </li>
        ))}
      </ul>
    </section>
  )
}
