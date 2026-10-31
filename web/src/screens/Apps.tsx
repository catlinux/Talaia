import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { createAppsApi, type App, type AppsApi, type AppSource } from '../api/apps'
import { createApiFleetClient, type FleetClient } from '../api/client'
import type { Server } from '../api/types'
import { usePermissions } from '../auth/permissions'
import { DataTable, FilterBar, PageHeader, Select, type DataTableColumn } from '../components/ui'
import { Link, useNavigate, useRoute, useSetQuery } from '../router'
import { formatDateTime } from './apps/format'
import { formatTime } from './fleet/format'

import './apps/apps.css'

export const appsRefreshMs = 15_000

type SourceFilter = 'all' | AppSource

interface Filters {
  q: string
  server: string
  source: SourceFilter
}

function parseFilters(query: URLSearchParams): Filters {
  const source = query.get('source')
  return {
    q: query.get('q') ?? '',
    server: query.get('server') ?? '',
    source: source === 'git' || source === 'image' || source === 'template' ? source : 'all',
  }
}

interface AppsListProps {
  api?: AppsApi
  client?: FleetClient
  refreshMs?: number
}

/** Apps is the fleet-wide list of apps. */
export function Apps({ api, client, refreshMs = appsRefreshMs }: AppsListProps) {
  const { t, i18n } = useTranslation('apps')
  const { query } = useRoute()
  const setQuery = useSetQuery()
  const navigate = useNavigate()
  const { canSeeStacks, loading: permissionsLoading } = usePermissions()
  const appsApi = useMemo(() => api ?? createAppsApi(), [api])
  const fleetClient = useMemo(() => client ?? createApiFleetClient(), [client])
  const language = i18n.resolvedLanguage ?? i18n.language
  const filters = parseFilters(query)

  const [apps, setApps] = useState<App[] | null>(null)
  const [servers, setServers] = useState<Server[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [loadedAt, setLoadedAt] = useState<Date | null>(null)
  const alive = useRef(true)
  const loaded = useRef(false)

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const load = useCallback(async () => {
    try {
      const [list, serverList] = await Promise.all([
        appsApi.list(),
        fleetClient.listServers().catch(() => [] as Server[]),
      ])
      if (!alive.current) return
      setApps(list)
      setServers(serverList)
      setLoadedAt(new Date())
      setLoadError(false)
      setLoading(false)
      loaded.current = true
    } catch {
      if (!alive.current) return
      if (!loaded.current) setLoadError(true)
      setLoading(false)
    }
  }, [appsApi, fleetClient])

  useEffect(() => {
    if (permissionsLoading) return
    queueMicrotask(() => {
      void load()
    })
    if (refreshMs <= 0) return
    const timer = window.setInterval(() => {
      if (!document.hidden) void load()
    }, refreshMs)
    return () => window.clearInterval(timer)
  }, [load, permissionsLoading, refreshMs])

  const serverNames = useMemo(() => {
    const names = new Map<string, string>()
    for (const server of servers) names.set(server.id, server.name)
    return names
  }, [servers])

  const serverOptions = useMemo(() => {
    const options = [{ value: '', label: t('filters.allServers') }]
    for (const server of servers) {
      if (!canSeeStacks(server.id)) continue
      options.push({ value: server.id, label: server.name })
    }
    return options
  }, [canSeeStacks, servers, t])

  const visibleApps = useMemo(
    () => (apps ?? []).filter((app) => canSeeStacks(app.serverId)),
    [apps, canSeeStacks],
  )

  const search = filters.q.trim().toLocaleLowerCase()
  const rows = useMemo(
    () =>
      visibleApps.filter((app) => {
        if (filters.server !== '' && app.serverId !== filters.server) return false
        if (filters.source !== 'all' && app.source !== filters.source) return false
        if (search === '') return true
        const haystack = [
          app.stackName,
          app.repoUrl,
          app.branch,
          app.image,
          app.templateId,
          serverNames.get(app.serverId) ?? app.serverId,
        ]
          .join(' ')
          .toLocaleLowerCase()
        return haystack.includes(search)
      }),
    [filters.server, filters.source, search, serverNames, visibleApps],
  )

  const filtersChanged = filters.q !== '' || filters.server !== '' || filters.source !== 'all'

  const columns: DataTableColumn<App>[] = [
    {
      id: 'app',
      header: t('columns.app'),
      mobilePrimary: true,
      sortValue: (app) => app.stackName,
      cell: (app) => (
        <span className="apps__name">
          <Link to={`/apps/${encodeURIComponent(app.id)}`}>{app.stackName}</Link>
          <span className="apps__sub">{appDetail(app)}</span>
        </span>
      ),
    },
    {
      id: 'server',
      header: t('columns.server'),
      sortValue: (app) => serverNames.get(app.serverId) ?? app.serverId,
      cell: (app) => (
        <Link to={`/servers/${encodeURIComponent(app.serverId)}/containers`}>
          {serverNames.get(app.serverId) ?? app.serverId}
        </Link>
      ),
    },
    {
      id: 'source',
      header: t('columns.source'),
      sortValue: (app) => app.source,
      cell: (app) => <span className="apps__tag">{t(`source.${app.source}`)}</span>,
    },
    {
      id: 'endpoint',
      header: t('columns.endpoint'),
      hideOnMobile: true,
      cell: (app) => (
        <span className="apps__mono">
          {app.hostPort > 0 ? `127.0.0.1:${app.hostPort} → ` : ''}
          {app.service}:{app.port}
        </span>
      ),
    },
    {
      id: 'autoDeploy',
      header: t('columns.autoDeploy'),
      hideOnMobile: true,
      cell: (app) => (
        <span className={app.autoDeploy ? 'apps__tag apps__tag--ok' : 'apps__muted'}>
          {t(app.autoDeploy ? 'yes' : 'no')}
        </span>
      ),
    },
    {
      id: 'updated',
      header: t('columns.updated'),
      hideOnMobile: true,
      sortValue: (app) => app.updatedAt,
      cell: (app) => <span className="apps__muted">{formatDateTime(app.updatedAt, language)}</span>,
    },
  ]

  const clearFilters = () => setQuery({ q: null, server: null, source: null })

  if (loading && apps === null) {
    return (
      <section className="apps">
        <PageHeader title={t('list.title')} />
        <p className="ui-sr-only" role="status">
          {t('list.loading')}
        </p>
        <DataTable columns={columns} rows={[]} rowKey={(app) => app.id} loading />
      </section>
    )
  }

  if (loadError && apps === null) {
    return (
      <section className="apps">
        <PageHeader title={t('list.title')} />
        <div className="apps__error" role="alert">
          <p>{t('list.error')}</p>
          <button type="button" className="ui-btn" onClick={() => void load()}>
            {t('list.retry')}
          </button>
        </div>
      </section>
    )
  }

  return (
    <section className="apps">
      <PageHeader
        title={t('list.title')}
        subtitle={t('list.subtitle', {
          count: visibleApps.length,
          time: loadedAt === null ? '' : formatTime(loadedAt, language),
        })}
      />

      <FilterBar
        search={{
          value: filters.q,
          onChange: (value) => setQuery({ q: value }),
          placeholder: t('filters.search'),
        }}
      >
        <Select
          label={t('filters.server')}
          value={filters.server}
          options={serverOptions}
          onChange={(value) => setQuery({ server: value || null })}
        />
        <Select
          label={t('filters.source')}
          value={filters.source}
          options={[
            { value: 'all', label: t('filters.allSources') },
            { value: 'git', label: t('source.git') },
            { value: 'image', label: t('source.image') },
            { value: 'template', label: t('source.template') },
          ]}
          onChange={(value) => setQuery({ source: value === 'all' ? null : value })}
        />
      </FilterBar>

      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(app) => app.id}
        onRowClick={(app) => navigate(`/apps/${encodeURIComponent(app.id)}`)}
        empty={
          visibleApps.length === 0 ? (
            <div className="apps__empty">
              <h2>{t('list.emptyTitle')}</h2>
              <p>{t('list.emptyBody')}</p>
            </div>
          ) : (
            <div className="apps__empty">
              <h2>{t('list.noResultsTitle')}</h2>
              <p>{t('list.noResultsBody')}</p>
              {filtersChanged ? (
                <button type="button" className="ui-btn" onClick={clearFilters}>
                  {t('filters.clear')}
                </button>
              ) : null}
            </div>
          )
        }
      />
    </section>
  )
}

function appDetail(app: App): string {
  if (app.source === 'git') return app.repoUrl
  if (app.source === 'template') return `${app.templateId} ${app.templateVersion}`.trim()
  return app.image
}
