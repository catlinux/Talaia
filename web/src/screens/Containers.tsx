import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { ContainerAction, ContainersApi } from '../api/containers'
import { createContainersApi } from '../api/containers'
import { createApiFleetClient, type FleetClient } from '../api/client'
import { isSignedOut } from '../api/http'
import type { Server } from '../api/types'
import { errorMessage } from '../auth/AuthGate'
import { usePermissions } from '../auth/permissions'
import {
  BulkBar,
  ConfirmDialog,
  DataTable,
  FilterBar,
  InfoTip,
  Menu,
  PageHeader,
  Select,
  StatusBadge,
  useToast,
  type DataTableColumn,
  type DataTableGroup,
  type MenuEntry,
  type Status,
} from '../components/ui'
import { WhatIsThis } from '../catalog/WhatIsThis'
import { Link, useNavigate, useSetQuery, useRoute } from '../router'
import { formatBytes, formatPercent, formatTime } from './fleet/format'
import { stackPath } from './server/serverTabs'
import { TimedOut, waitForJob } from './server/waitForJob'
import { containerBulkAvailability, type ContainerBulkAction } from './fleetwide/bulk'
import { loadFleet, type FleetContainerRow, type FleetLoad } from './fleetwide/loadFleet'
import { runBulkJobs } from './fleetwide/runBulk'

export const fleetRefreshMs = 15_000
export const refreshInterval = fleetRefreshMs

export interface ContainersProps {
  client?: FleetClient
  containersApi?: ContainersApi
  /** pollMs is the job poll interval; tests shorten it. */
  pollMs?: number
  refreshMs?: number
}

type StateFilter = 'all' | 'running' | 'stopped' | 'problems'
type GroupBy = 'none' | 'server' | 'stack'

interface Filters {
  q: string
  state: StateFilter
  server: string
  stack: string
  group: GroupBy
}

interface ContainerRowData extends FleetContainerRow {
  key: string
}

const problemStates = new Set(['exited', 'restarting', 'dead'])

const noStackGroup = '__no_stack__'

function rowKey(row: FleetContainerRow): string {
  return `${row.serverId}:${row.container.id}`
}

function managedKey(serverId: string, stack: string): string {
  return JSON.stringify([serverId, stack])
}

function rowData(row: FleetContainerRow): ContainerRowData {
  return { ...row, key: rowKey(row) }
}

function statusOf(state: string): Status {
  switch (state) {
    case 'running':
      return 'ok'
    case 'restarting':
    case 'paused':
      return 'warn'
    case 'dead':
      return 'crit'
    case 'exited':
      return 'offline'
    default:
      return 'unknown'
  }
}

function parseFilters(query: URLSearchParams): Filters {
  const state = query.get('state')
  const group = query.get('group')
  return {
    q: query.get('q') ?? '',
    state: state === 'running' || state === 'stopped' || state === 'problems' ? state : 'all',
    server: query.get('server') ?? '',
    stack: query.get('stack') ?? '',
    group: group === 'server' || group === 'stack' ? group : 'none',
  }
}

function matchesState(state: string, filter: StateFilter): boolean {
  switch (filter) {
    case 'running':
      return state === 'running'
    case 'stopped':
      return state === 'exited'
    case 'problems':
      return problemStates.has(state)
    default:
      return true
  }
}

function defaultOrder(a: ContainerRowData, b: ContainerRowData): number {
  const aProblem = problemStates.has(a.container.state) ? 0 : 1
  const bProblem = problemStates.has(b.container.state) ? 0 : 1
  if (aProblem !== bProblem) return aProblem - bProblem
  const server = a.serverName.localeCompare(b.serverName)
  if (server !== 0) return server
  return a.container.name.localeCompare(b.container.name)
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

interface ContainerMenuProps {
  row: ContainerRowData
  onAction: (row: ContainerRowData, action: ContainerAction) => void
  onRemove: (row: ContainerRowData) => void
}

function ContainerMenu({ row, onAction, onRemove }: ContainerMenuProps) {
  const { t } = useTranslation('fleetwide')
  const { can } = usePermissions()
  const navigate = useNavigate()
  const container = row.container
  const running = container.state === 'running'
  const scope = {
    serverId: row.serverId,
    stack: container.composeProject || undefined,
    container: container.name,
  }
  const canControl = can('container.control', scope)
  const canLogs = can('container.logs', scope)
  const canTerminal = can('container.terminal', scope)
  const canRemove = can('container.remove', scope)

  const items: MenuEntry[] = []
  if (canControl) {
    if (running) {
      items.push({
        id: 'restart',
        label: t('common.action.restart'),
        onSelect: () => onAction(row, 'restart'),
      })
      items.push({
        id: 'stop',
        label: t('common.action.stop'),
        onSelect: () => onAction(row, 'stop'),
      })
    } else {
      items.push({
        id: 'start',
        label: t('common.action.start'),
        onSelect: () => onAction(row, 'start'),
      })
    }
  }
  if (canLogs) {
    items.push({
      id: 'logs',
      label: t('common.action.logs'),
      onSelect: () => navigate(`/servers/${encodeURIComponent(row.serverId)}/containers`),
    })
  }
  if (canTerminal && running) {
    items.push({
      id: 'terminal',
      label: t('common.action.terminal'),
      onSelect: () => navigate(`/servers/${encodeURIComponent(row.serverId)}/containers`),
    })
  }
  if (canRemove) {
    if (items.length > 0) items.push({ separator: true })
    items.push({
      id: 'remove',
      label: t('common.action.remove'),
      danger: true,
      onSelect: () => onRemove(row),
    })
  }

  if (items.length === 0) return <span className="fleetwide__muted">—</span>

  return (
    <Menu
      align="end"
      items={items}
      trigger={(props) => (
        <button
          {...props}
          className="ui-btn ui-btn--ghost fleetwide__menu-trigger"
          aria-label={t('common.rowActions', { name: container.name })}
        >
          ⋯
        </button>
      )}
    />
  )
}

/** Containers is the fleet-wide list of every container the user can see. */
export function Containers({
  client,
  containersApi,
  pollMs = 800,
  refreshMs = fleetRefreshMs,
}: ContainersProps) {
  const { t, i18n } = useTranslation('fleetwide')
  const toast = useToast()
  const { query } = useRoute()
  const setQuery = useSetQuery()
  const {
    can,
    canAnywhere,
    canSeeContainers,
    canSeeStacks,
    loading: permissionsLoading,
  } = usePermissions()
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
  const [removing, setRemoving] = useState<ContainerRowData | null>(null)
  const [bulkConfirm, setBulkConfirm] = useState<{
    action: Exclude<ContainerBulkAction, 'start'>
    rows: ContainerRowData[]
  } | null>(null)
  const alive = useRef(true)
  const request = useRef(0)

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const canLoad = useCallback((server: Server) => canSeeContainers(server.id), [canSeeContainers])
  const canLoadStacks = useCallback((server: Server) => canSeeStacks(server.id), [canSeeStacks])

  const load = useCallback(async () => {
    const current = request.current + 1
    request.current = current
    try {
      const next = keepFailedServers(
        dataRef.current,
        await loadFleet(fleetClient, api, { canLoad, canLoadStacks }),
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
  }, [api, canLoad, canLoadStacks, fleetClient])

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

  const filtersChanged =
    filters.q !== '' || filters.state !== 'all' || filters.server !== '' || filters.stack !== ''

  const serverOptions = useMemo(() => {
    const options = [{ value: '', label: t('containers.filters.allServers') }]
    for (const server of data?.servers ?? []) {
      if (!canSeeContainers(server.id)) continue
      options.push({ value: server.id, label: server.name })
    }
    return options
  }, [canSeeContainers, data, t])

  const stackOptions = useMemo(() => {
    const names = new Set<string>()
    for (const row of data?.containers ?? []) {
      if (row.container.composeProject !== '') names.add(row.container.composeProject)
    }
    for (const row of data?.stacks ?? []) {
      names.add(row.stack)
    }
    return [
      { value: '', label: t('containers.filters.allStacks') },
      ...[...names].sort().map((name) => ({ value: name, label: name })),
    ]
  }, [data, t])

  const rows = useMemo(() => {
    const search = filters.q.trim().toLocaleLowerCase()
    return (data?.containers ?? [])
      .map(rowData)
      .filter((row) => {
        const container = row.container
        if (filters.server !== '' && row.serverId !== filters.server) return false
        if (filters.stack !== '' && container.composeProject !== filters.stack) return false
        if (!matchesState(container.state, filters.state)) return false
        if (search === '') return true
        return [
          container.name,
          container.image,
          container.composeProject,
          row.serverName,
          container.status,
        ].some((value) => value.toLocaleLowerCase().includes(search))
      })
      .sort(defaultOrder)
  }, [data, filters.q, filters.server, filters.stack, filters.state])

  const selectedRows = useMemo(() => {
    return (data?.containers ?? []).map(rowData).filter((row) => selected.has(row.key))
  }, [data, selected])

  const availability = containerBulkAvailability(
    selectedRows.map((row) => ({ serverId: row.serverId, container: row.container })),
    can,
  )

  const managedStacks = useMemo(() => {
    return new Set((data?.stacks ?? []).map((row) => managedKey(row.serverId, row.stack)))
  }, [data])

  const submitAction = useCallback(
    async (row: ContainerRowData, action: ContainerAction, confirm = false) => {
      const actionLabel = t(`common.action.${action}`)
      try {
        const jobId = await api.run(row.serverId, row.container.name, action, confirm)
        const job = await waitForJob(api, jobId, pollMs, () => alive.current)
        if (job === null) return
        if (job.state === 'succeeded') {
          toast.success(t('common.toast.done', { action: actionLabel, name: row.container.name }))
        } else {
          toast.error(
            t('common.toast.failed', {
              action: actionLabel,
              name: row.container.name,
              message: job.message,
            }),
          )
        }
      } catch (error) {
        if (!alive.current || isSignedOut(error)) return
        toast.error(
          t('common.toast.failed', {
            action: actionLabel,
            name: row.container.name,
            message: error instanceof TimedOut ? t('common.toast.timeout') : errorMessage(t, error),
          }),
        )
      } finally {
        if (alive.current) void load()
      }
    },
    [api, load, pollMs, t, toast],
  )

  const remove = useCallback(() => {
    const row = removing
    setRemoving(null)
    if (row !== null) void submitAction(row, 'remove', true)
  }, [removing, submitAction])

  const runBulk = useCallback(
    async (action: ContainerBulkAction, targets: ContainerRowData[]) => {
      setBulkConfirm(null)
      const summary = await runBulkJobs(
        targets.map((row) => ({
          serverId: row.serverId,
          run: () => api.run(row.serverId, row.container.name, action),
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

  const requestBulk = (action: ContainerBulkAction) => {
    if (action === 'start' || selectedRows.length < 2) {
      void runBulk(action, selectedRows)
      return
    }
    setBulkConfirm({ action, rows: selectedRows })
  }

  const clearFilters = () => {
    setQuery({ q: null, state: null, server: null, stack: null })
  }

  const columns: DataTableColumn<ContainerRowData>[] = [
    {
      id: 'state',
      header: t('containers.columns.state'),
      cell: (row) => (
        <StatusBadge
          status={statusOf(row.container.state)}
          label={t(`common.state.${row.container.state}`, {
            defaultValue: row.container.state,
          })}
        />
      ),
    },
    {
      id: 'name',
      header: t('containers.columns.name'),
      cell: (row) => (
        <div className="fleetwide__name">
          <span className="fleetwide__name-line">
            <strong>{row.container.name}</strong>
            <InfoTip>
              <WhatIsThis target={{ kind: 'container', container: row.container }} />
            </InfoTip>
          </span>
          {row.container.composeService !== '' ? (
            <span className="fleetwide__sub">
              {t('common.service', { name: row.container.composeService })}
            </span>
          ) : null}
        </div>
      ),
    },
    {
      id: 'image',
      header: t('containers.columns.image'),
      hideOnMobile: true,
      cell: (row) => <span className="fleetwide__mono">{row.container.image}</span>,
    },
    {
      id: 'stack',
      header: t('containers.columns.stack'),
      cell: (row) => {
        const project = row.container.composeProject
        if (project === '') return <span className="fleetwide__muted">—</span>
        return managedStacks.has(managedKey(row.serverId, project)) ? (
          <Link to={stackPath(row.serverId, project)}>{project}</Link>
        ) : (
          project
        )
      },
    },
    {
      id: 'server',
      header: t('containers.columns.server'),
      cell: (row) => (
        <Link to={`/servers/${encodeURIComponent(row.serverId)}/containers`}>{row.serverName}</Link>
      ),
    },
    {
      id: 'cpu',
      header: t('containers.columns.cpu'),
      align: 'right',
      hideOnMobile: true,
      cell: (row) =>
        row.container.use === undefined
          ? '—'
          : formatPercent(row.container.use.cpuPercent, language),
    },
    {
      id: 'ram',
      header: t('containers.columns.ram'),
      align: 'right',
      hideOnMobile: true,
      cell: (row) => {
        const use = row.container.use
        if (use === undefined) return '—'
        const used = formatBytes(use.memUsedBytes, language)
        if (use.memLimitBytes <= 0) return used
        return `${used} / ${formatBytes(use.memLimitBytes, language)}`
      },
    },
    {
      id: 'status',
      header: t('containers.columns.status'),
      hideOnMobile: true,
      cell: (row) => row.container.status,
    },
    {
      id: 'actions',
      header: t('containers.columns.actions'),
      align: 'right',
      width: 56,
      cell: (row) => (
        <ContainerMenu
          row={row}
          onAction={(target, action) => void submitAction(target, action)}
          onRemove={setRemoving}
        />
      ),
    },
  ]

  const groupBy: DataTableGroup<ContainerRowData> | undefined =
    filters.group === 'server'
      ? {
          key: (row) => row.serverId,
          label: (_key, group) => `${group[0]?.serverName ?? ''} (${group.length})`,
        }
      : filters.group === 'stack'
        ? {
            key: (row) => row.container.composeProject || noStackGroup,
            label: (key, group) =>
              `${key === noStackGroup ? t('common.noStack') : key} (${group.length})`,
          }
        : undefined

  if (loading && data === null) {
    return (
      <section className="fleetwide">
        <PageHeader title={t('containers.title')} />
        <p className="ui-sr-only" role="status">
          {t('containers.loading')}
        </p>
        <DataTable
          columns={columns}
          rows={[]}
          rowKey={(row) => row.key}
          loading
          empty={t('containers.loading')}
        />
      </section>
    )
  }

  if (loadError && data === null) {
    return (
      <section className="fleetwide">
        <PageHeader title={t('containers.title')} />
        <div className="fleetwide__error" role="alert">
          <p>{t('common.loadError')}</p>
          <button type="button" className="ui-btn" onClick={() => void load()}>
            {t('common.retry')}
          </button>
        </div>
      </section>
    )
  }

  const total = data?.containers.length ?? 0
  const hasServers = (data?.servers.length ?? 0) > 0

  return (
    <section className="fleetwide">
      <PageHeader
        title={t('containers.title')}
        subtitle={t('containers.subtitle', {
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
      ) : total === 0 ? (
        <div className="fleetwide__empty">
          <h2>{t('containers.emptyTitle')}</h2>
          <p>{t('containers.emptyBody')}</p>
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
              placeholder: t('containers.filters.search'),
            }}
            right={
              <Select
                label={t('containers.filters.group')}
                value={filters.group}
                options={[
                  { value: 'none', label: t('containers.filters.groupNone') },
                  { value: 'server', label: t('containers.filters.groupServer') },
                  { value: 'stack', label: t('containers.filters.groupStack') },
                ]}
                onChange={(value) => setQuery({ group: value === 'none' ? null : value })}
              />
            }
          >
            <Select
              label={t('containers.filters.state')}
              value={filters.state}
              options={[
                { value: 'all', label: t('containers.filters.allStates') },
                { value: 'running', label: t('containers.filters.running') },
                { value: 'stopped', label: t('containers.filters.stopped') },
                { value: 'problems', label: t('containers.filters.problems') },
              ]}
              onChange={(value) => setQuery({ state: value === 'all' ? null : value })}
            />
            <Select
              label={t('containers.filters.server')}
              value={filters.server}
              options={serverOptions}
              onChange={(value) => setQuery({ server: value || null })}
            />
            <Select
              label={t('containers.filters.stack')}
              value={filters.stack}
              options={stackOptions}
              onChange={(value) => setQuery({ stack: value || null })}
            />
          </FilterBar>

          <BulkBar count={selectedRows.length} onClear={() => setSelected(new Set())}>
            {(['start', 'stop', 'restart'] as const).map((action) => {
              const state = availability[action]
              const title = state.allowed
                ? undefined
                : t('common.bulk.notAllowed', { count: state.denied })
              return (
                <span key={action} className="fleetwide__bulk-action" title={title}>
                  <button
                    type="button"
                    className="ui-btn"
                    disabled={!state.allowed}
                    title={title}
                    onClick={() => requestBulk(action)}
                  >
                    {t(`common.action.${action}`)}
                  </button>
                </span>
              )
            })}
          </BulkBar>

          <DataTable
            columns={columns}
            rows={rows}
            rowKey={(row) => row.key}
            selectable={canAnywhere('container.control')}
            selected={selected}
            onSelectionChange={setSelected}
            groupBy={groupBy}
            empty={
              <div className="fleetwide__empty">
                <h2>{t('containers.noResultsTitle')}</h2>
                <p>{t('containers.noResultsBody')}</p>
                {filtersChanged ? (
                  <button type="button" className="ui-btn" onClick={clearFilters}>
                    {t('common.clearFilters')}
                  </button>
                ) : null}
              </div>
            }
          />
        </>
      )}

      <ConfirmDialog
        open={removing !== null}
        title={t('containers.remove.title', { name: removing?.container.name ?? '' })}
        body={t('containers.remove.body')}
        confirmLabel={t('containers.remove.confirm')}
        danger
        requireText={removing?.container.name ?? ''}
        onConfirm={remove}
        onCancel={() => setRemoving(null)}
      />

      <ConfirmDialog
        open={bulkConfirm !== null}
        title={t(`containers.bulk.${bulkConfirm?.action ?? 'stop'}Title`, {
          count: bulkConfirm?.rows.length ?? 0,
        })}
        body={
          <>
            <p>{t(`containers.bulk.${bulkConfirm?.action ?? 'stop'}Body`)}</p>
            <ul className="fleetwide__confirm-list">
              {(bulkConfirm?.rows ?? []).map((row) => (
                <li key={row.key}>{row.container.name}</li>
              ))}
            </ul>
          </>
        }
        confirmLabel={t('common.bulk.confirm')}
        danger={bulkConfirm?.action === 'stop'}
        requireText={String(bulkConfirm?.rows.length ?? '')}
        onConfirm={() => {
          if (bulkConfirm !== null) void runBulk(bulkConfirm.action, bulkConfirm.rows)
        }}
        onCancel={() => setBulkConfirm(null)}
      />
    </section>
  )
}
