import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  createAppsApi,
  type App,
  type Domain,
  type ProxyDomain,
  type ProxyStatus,
  type WizardApi,
} from '../api/apps'
import { createApiFleetClient, type FleetClient } from '../api/client'
import type { Server } from '../api/types'
import { usePermissions } from '../auth/permissions'
import {
  DataTable,
  Drawer,
  FilterBar,
  PageHeader,
  Select,
  StatusBadge,
  type DataTableColumn,
  type Status,
} from '../components/ui'
import { Link, useRoute, useSetQuery } from '../router'
import { formatTime } from './fleet/format'
import './domains.css'

type StateFilter = 'all' | 'external' | 'managed' | 'issues'

interface DomainRow {
  key: string
  domain: Domain
  app: App
  serverName: string
  mode: string
  certificate: string
  warning: string
  proxyDomain: ProxyDomain | null
}

interface Filters {
  q: string
  server: string
  state: StateFilter
}

function parseFilters(query: URLSearchParams): Filters {
  const state = query.get('state')
  return {
    q: query.get('q') ?? '',
    server: query.get('server') ?? '',
    state: state === 'external' || state === 'managed' || state === 'issues' ? state : 'all',
  }
}

interface DomainsProps {
  api?: WizardApi
  client?: FleetClient
}

/** Domains lists every hostname and how it is published. */
export function Domains({ api, client }: DomainsProps) {
  const { t, i18n } = useTranslation('domains')
  const { query } = useRoute()
  const setQuery = useSetQuery()
  const { can, canSeeStacks, loading: permissionsLoading } = usePermissions()
  const appsApi = useMemo(() => api ?? createAppsApi(), [api])
  const fleetClient = useMemo(() => client ?? createApiFleetClient(), [client])
  const language = i18n.resolvedLanguage ?? i18n.language
  const filters = parseFilters(query)

  const [rows, setRows] = useState<DomainRow[] | null>(null)
  const [servers, setServers] = useState<Server[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [loadedAt, setLoadedAt] = useState<Date | null>(null)
  const [setup, setSetup] = useState<DomainRow | null>(null)
  const [copied, setCopied] = useState('')
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const load = useCallback(async () => {
    try {
      const [appList, serverList] = await Promise.all([
        appsApi.list(),
        fleetClient.listServers().catch(() => [] as Server[]),
      ])
      if (!alive.current) return
      const visibleApps = appList.filter((app) => canSeeStacks(app.serverId))
      const serverNames = new Map(serverList.map((server) => [server.id, server.name]))
      const serverIDs = [...new Set(visibleApps.map((app) => app.serverId))]
      const proxyEntries = await Promise.all(
        serverIDs.map(async (serverID) => {
          if (!can('server.proxy', { serverId: serverID })) {
            return [serverID, null] as const
          }
          try {
            return [serverID, await appsApi.proxy(serverID)] as const
          } catch {
            return [serverID, null] as const
          }
        }),
      )
      const proxies = new Map<string, ProxyStatus | null>(proxyEntries)
      const domainsByApp = await Promise.all(
        visibleApps.map(async (app) => {
          try {
            return [app.id, await appsApi.appDomains(app.id)] as const
          } catch {
            return [app.id, [] as Domain[]] as const
          }
        }),
      )
      if (!alive.current) return
      const domains = new Map(domainsByApp)
      const nextRows: DomainRow[] = []
      for (const app of visibleApps) {
        const proxy = proxies.get(app.serverId) ?? null
        for (const domain of domains.get(app.id) ?? []) {
          const proxyDomain = proxy?.domains.find((item) => item.hostname === domain.hostname)
          const certificate =
            proxy?.certificates?.find((item) => item.hostname === domain.hostname)?.state ?? ''
          nextRows.push({
            key: domain.id,
            domain,
            app,
            serverName: serverNames.get(app.serverId) ?? app.serverId,
            mode: proxy?.mode ?? '',
            certificate,
            warning: domain.warning || proxyDomain?.warning || '',
            proxyDomain: proxyDomain ?? null,
          })
        }
      }
      setRows(nextRows)
      setServers(serverList)
      setLoadedAt(new Date())
      setLoadError(false)
    } catch {
      if (alive.current) setLoadError(true)
    } finally {
      if (alive.current) setLoading(false)
    }
  }, [appsApi, can, canSeeStacks, fleetClient])

  useEffect(() => {
    if (permissionsLoading) return
    queueMicrotask(() => {
      void load()
    })
  }, [load, permissionsLoading])

  const serverOptions = useMemo(() => {
    const options = [{ value: '', label: t('filters.allServers') }]
    for (const server of servers) {
      if (!canSeeStacks(server.id)) continue
      options.push({ value: server.id, label: server.name })
    }
    return options
  }, [canSeeStacks, servers, t])

  const search = filters.q.trim().toLocaleLowerCase()
  const visibleRows = useMemo(
    () =>
      (rows ?? []).filter((row) => {
        if (filters.server !== '' && row.app.serverId !== filters.server) return false
        if (filters.state === 'external' && row.mode !== 'external') return false
        if (filters.state === 'managed' && row.mode !== 'managed') return false
        if (
          filters.state === 'issues' &&
          row.warning === '' &&
          row.certificate !== 'pending' &&
          row.certificate !== 'failed'
        ) {
          return false
        }
        if (search === '') return true
        return [row.domain.hostname, row.app.stackName, row.serverName]
          .join(' ')
          .toLocaleLowerCase()
          .includes(search)
      }),
    [filters.server, filters.state, rows, search],
  )

  const warningText = (warning: string): string => {
    if (warning === 'the hostname does not resolve') return t('warnings.noResolve')
    if (warning === 'the hostname does not resolve to the server') return t('warnings.mismatch')
    return warning
  }

  const copy = async (key: string, text: string) => {
    if (text === '' || navigator.clipboard === undefined) return
    try {
      await navigator.clipboard.writeText(text)
      setCopied(key)
    } catch {
      // The browser may refuse clipboard access; the block stays selectable.
    }
  }

  const stateLabel = (row: DomainRow): { status: Status; label: string } => {
    if (row.mode === 'external') {
      return {
        status: row.warning === '' ? 'ok' : 'warn',
        label: t('state.external'),
      }
    }
    if (row.mode === 'managed') {
      if (row.certificate === 'valid') return { status: 'ok', label: t('state.valid') }
      if (row.certificate === 'pending') return { status: 'warn', label: t('state.pending') }
      if (row.certificate === 'failed') return { status: 'crit', label: t('state.failed') }
      return { status: 'ok', label: t('state.managed') }
    }
    return { status: 'unknown', label: t('state.unknown') }
  }

  const columns: DataTableColumn<DomainRow>[] = [
    {
      id: 'domain',
      header: t('columns.domain'),
      mobilePrimary: true,
      sortValue: (row) => row.domain.hostname,
      cell: (row) => (
        <span className="domains__name">
          <strong>{row.domain.hostname}</strong>
        </span>
      ),
    },
    {
      id: 'app',
      header: t('columns.app'),
      sortValue: (row) => row.app.stackName,
      cell: (row) => (
        <Link to={`/apps/${encodeURIComponent(row.app.id)}`}>{row.app.stackName}</Link>
      ),
    },
    {
      id: 'server',
      header: t('columns.server'),
      sortValue: (row) => row.serverName,
      cell: (row) => (
        <Link to={`/servers/${encodeURIComponent(row.app.serverId)}/containers`}>
          {row.serverName}
        </Link>
      ),
    },
    {
      id: 'state',
      header: t('columns.state'),
      sortValue: (row) => stateLabel(row).label,
      cell: (row) => {
        const state = stateLabel(row)
        return <StatusBadge status={state.status} label={state.label} />
      },
    },
    {
      id: 'warning',
      header: t('columns.warning'),
      cell: (row) =>
        row.warning === '' ? (
          <span className="domains__muted">—</span>
        ) : (
          <span className="domains__warning">{warningText(row.warning)}</span>
        ),
    },
    {
      id: 'actions',
      header: t('columns.actions'),
      align: 'right',
      width: 190,
      cell: (row) =>
        row.mode === 'external' && row.proxyDomain !== null ? (
          <button
            type="button"
            className="ui-btn domains__setup"
            onClick={() => {
              setCopied('')
              setSetup(row)
            }}
          >
            {t('setup.view')}
          </button>
        ) : (
          <span className="domains__muted">—</span>
        ),
    },
  ]

  const filtersChanged = filters.q !== '' || filters.server !== '' || filters.state !== 'all'
  const clearFilters = () => setQuery({ q: null, server: null, state: null })

  if (loading && rows === null) {
    return (
      <section className="domains">
        <PageHeader title={t('title')} />
        <DataTable columns={columns} rows={[]} rowKey={(row) => row.key} loading />
      </section>
    )
  }

  if (loadError && rows === null) {
    return (
      <section className="domains">
        <PageHeader title={t('title')} />
        <div className="domains__error" role="alert">
          <p>{t('error')}</p>
          <button type="button" className="ui-btn" onClick={() => void load()}>
            {t('retry')}
          </button>
        </div>
      </section>
    )
  }

  return (
    <section className="domains">
      <PageHeader
        title={t('title')}
        subtitle={t('subtitle', {
          count: rows?.length ?? 0,
          time: loadedAt === null ? '' : formatTime(loadedAt, language),
        })}
        actions={
          <button type="button" className="ui-btn" onClick={() => void load()}>
            {t('refresh')}
          </button>
        }
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
          label={t('filters.state')}
          value={filters.state}
          options={[
            { value: 'all', label: t('filters.allStates') },
            { value: 'external', label: t('state.external') },
            { value: 'managed', label: t('state.managed') },
            { value: 'issues', label: t('filters.issues') },
          ]}
          onChange={(value) => setQuery({ state: value === 'all' ? null : value })}
        />
      </FilterBar>

      <DataTable
        columns={columns}
        rows={visibleRows}
        rowKey={(row) => row.key}
        empty={
          visibleRows.length === 0 && !filtersChanged ? (
            <div className="domains__empty">
              <h2>{t('emptyTitle')}</h2>
              <p>{t('emptyBody')}</p>
            </div>
          ) : (
            <div className="domains__empty">
              <h2>{t('noResultsTitle')}</h2>
              <p>{t('noResultsBody')}</p>
              {filtersChanged ? (
                <button type="button" className="ui-btn" onClick={clearFilters}>
                  {t('filters.clear')}
                </button>
              ) : null}
            </div>
          )
        }
      />

      <Drawer
        open={setup !== null}
        title={t('setup.title')}
        width={760}
        onClose={() => {
          setSetup(null)
          setCopied('')
        }}
      >
        {setup !== null && setup.proxyDomain !== null ? (
          <div className="domains__setup-details">
            <p className="domains__setup-target">
              <code>{setup.domain.hostname}</code>
              <span aria-hidden="true">→</span>
              <code>127.0.0.1:{setup.proxyDomain.hostPort}</code>
            </p>
            <p className="domains__setup-note">{t('setup.note')}</p>
            {[
              { key: 'apache', label: t('setup.apache'), text: setup.proxyDomain.apache },
              { key: 'nginx', label: t('setup.nginx'), text: setup.proxyDomain.nginx },
              { key: 'steps', label: t('setup.steps'), text: setup.proxyDomain.certbot },
            ].map((section) => (
              <section className="domains__setup-section" key={section.key}>
                <div className="domains__setup-head">
                  <h3>{section.label}</h3>
                  <button
                    type="button"
                    className="ui-btn domains__copy"
                    disabled={section.text === ''}
                    onClick={() => void copy(section.key, section.text)}
                  >
                    {copied === section.key ? t('setup.copied') : t('setup.copy')}
                  </button>
                </div>
                <pre className="domains__code">
                  {section.text === '' ? t('setup.empty') : section.text}
                </pre>
              </section>
            ))}
          </div>
        ) : null}
      </Drawer>
    </section>
  )
}
