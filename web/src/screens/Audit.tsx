import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { createAuditApi, type AuditApi, type AuditEntry } from '../api/audit'
import { createRecordingsApi, type RecordingsApi } from '../api/recordings'
import { isSignedOut } from '../api/http'
import { errorMessage } from '../auth/AuthGate'
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
import { Link, setQuery, useNavigate, useRoute } from '../router'
import { formatRelativeTime } from './fleet/format'
import { RecordingsTab } from './audit/RecordingsTab'

const actionGroups = [
  { value: '', key: 'filters.all' },
  { value: 'login*', key: 'filters.signIn' },
  { value: 'server_*', key: 'filters.servers' },
  { value: 'container_*', key: 'filters.containers' },
  { value: 'stack_*', key: 'filters.stacks' },
  { value: 'apt_*', key: 'filters.updates' },
  { value: 'service_*', key: 'filters.services' },
  { value: 'journal_*', key: 'filters.journal' },
  { value: 'terminal_*', key: 'filters.terminal' },
  { value: 'host_terminal_*', key: 'filters.hostTerminal' },
  { value: 'host_*', key: 'filters.host' },
] as const

const defaultApi = createAuditApi()
const defaultRecordingsApi = createRecordingsApi()

interface AuditProps {
  /** api is injected by the tests; the panel builds its own otherwise. */
  api?: AuditApi
  /** limit is the page size; the core accepts 1..200. */
  limit?: number
  /** serverNames is optional; without it the id from the details is shown. */
  serverNames?: Record<string, string>
  /** recordingsApi is injected by the tests; the panel builds its own otherwise. */
  recordingsApi?: RecordingsApi
}

/** formatDetail turns a JSON value into text, never HTML. */
function formatDetail(value: unknown): string {
  if (value === null) return '—'
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  try {
    return JSON.stringify(value) ?? String(value)
  } catch {
    return String(value)
  }
}

/** resultTone maps the stored result to the badge vocabulary. */
function resultTone(result: string): 'success' | 'failure' | 'denied' | 'requested' | 'unknown' {
  switch (result) {
    case 'success':
    case 'ok':
      return 'success'
    case 'failure':
      return 'failure'
    case 'denied':
      return 'denied'
    case 'requested':
      return 'requested'
    default:
      return 'unknown'
  }
}

function resultStatus(tone: ReturnType<typeof resultTone>): Status {
  switch (tone) {
    case 'success':
      return 'ok'
    case 'failure':
    case 'denied':
      return 'crit'
    case 'requested':
      return 'warn'
    default:
      return 'unknown'
  }
}

function detailTarget(details: Record<string, unknown> | null): string {
  if (details === null) return ''
  for (const key of ['container', 'stack', 'service', 'target', 'name']) {
    const value = details[key]
    if (typeof value === 'string' && value !== '') return value
  }
  return ''
}

function auditTabPath(tab: 'audit' | 'recordings'): string {
  return tab === 'audit' ? '/audit' : '/audit/recordings'
}

function auditTabFromPath(path: string): 'audit' | 'recordings' | null {
  if (path === '/audit') return 'audit'
  if (path === '/audit/recordings') return 'recordings'
  return null
}

/**
 * Audit lists the append-only audit log newest first. Filters reset the list and
 * Load more follows the opaque cursor; details are always rendered as text.
 */
export function Audit({ api = defaultApi, limit = 50, serverNames, recordingsApi }: AuditProps) {
  const { t, i18n } = useTranslation(['audit', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const { can, loading: permissionsLoading } = usePermissions()
  const { path, query } = useRoute()
  const navigate = useNavigate()
  const canView = !permissionsLoading && can('audit.view')
  const canRecordings = !permissionsLoading && can('recordings.view')
  const recordings = recordingsApi ?? defaultRecordingsApi
  const tab = auditTabFromPath(path)
  const activeTab: 'audit' | 'recordings' =
    tab === 'audit' && !canView
      ? 'recordings'
      : tab === 'recordings' && !canRecordings
        ? 'audit'
        : (tab ?? (canView ? 'audit' : 'recordings'))
  const [entries, setEntries] = useState<AuditEntry[]>([])
  const [next, setNext] = useState('')
  const [loading, setLoading] = useState(true)
  const [appending, setAppending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [details, setDetails] = useState<AuditEntry | null>(null)
  const [actor, setActor] = useState(() => query.get('user') ?? query.get('actor') ?? '')
  const request = useRef(0)

  const q = query.get('q') ?? ''
  const action = query.get('action') ?? ''
  const user = query.get('user') ?? query.get('actor') ?? ''
  const server = query.get('server') ?? ''

  useEffect(() => {
    if (permissionsLoading) return
    if (tab === null || (tab === 'audit' && !canView) || (tab === 'recordings' && !canRecordings)) {
      navigate(auditTabPath(canView ? 'audit' : 'recordings'), { replace: true })
    }
  }, [canRecordings, canView, navigate, permissionsLoading, tab])

  const load = useCallback(
    async (before: string, append: boolean) => {
      const current = ++request.current
      if (append) setAppending(true)
      else setLoading(true)
      setError(null)
      try {
        const page = await api.list({
          limit,
          before: before || undefined,
          action: action || undefined,
          actor: user || undefined,
          serverId: server || undefined,
        })
        if (current !== request.current) return
        setEntries((previous) => (append ? [...previous, ...page.entries] : page.entries))
        setNext(page.next)
      } catch (caught) {
        if (current !== request.current || isSignedOut(caught)) return
        setError(t('error', { message: errorMessage(t, caught) }))
      } finally {
        if (current === request.current) {
          setLoading(false)
          setAppending(false)
        }
      }
    },
    [action, api, limit, server, t, user],
  )

  useEffect(() => {
    if (!canView || activeTab !== 'audit' || tab === null) return
    queueMicrotask(() => {
      void load('', false)
    })
  }, [activeTab, canView, load, tab])

  useEffect(() => {
    queueMicrotask(() => setActor(user))
  }, [user])

  useEffect(() => {
    const trimmed = actor.trim()
    if (trimmed === user) return
    const timer = window.setTimeout(() => setQuery({ user: trimmed === '' ? null : trimmed }), 300)
    return () => window.clearTimeout(timer)
  }, [actor, user])

  const changeAction = (value: string) => {
    setDetails(null)
    setQuery({ action: value === '' ? null : value })
  }

  const changeServer = (value: string) => {
    setDetails(null)
    setQuery({ server: value === '' ? null : value })
  }

  const applyActor = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setQuery({ user: actor.trim() === '' ? null : actor.trim() })
  }

  const serverOptions = useMemo(() => {
    const ids = new Set<string>(Object.keys(serverNames ?? {}))
    if (server !== '') ids.add(server)
    for (const entry of entries) {
      const id = entry.details?.server_id
      if (typeof id === 'string' && id !== '') ids.add(id)
    }
    return [
      { value: '', label: t('filters.allServers') },
      ...[...ids].map((id) => ({
        value: id,
        label: t('filters.serverOption', { name: serverNames?.[id] ?? id }),
      })),
    ]
  }, [entries, server, serverNames, t])

  const visibleEntries = useMemo(() => {
    const needle = q.trim().toLowerCase()
    if (needle === '') return entries
    return entries.filter((entry) => {
      const serverID = typeof entry.details?.server_id === 'string' ? entry.details.server_id : ''
      return [
        entry.actor,
        entry.action,
        entry.result,
        entry.ip,
        serverID,
        serverNames?.[serverID] ?? '',
      ].some((value) => value.toLowerCase().includes(needle))
    })
  }, [entries, q, serverNames])

  const columns: DataTableColumn<AuditEntry>[] = [
    {
      id: 'time',
      header: t('table.time'),
      sortValue: (entry) => new Date(entry.at).getTime(),
      cell: (entry) => {
        const at = new Date(entry.at)
        const known = !Number.isNaN(at.getTime())
        const absolute = known
          ? new Intl.DateTimeFormat(language, {
              dateStyle: 'medium',
              timeStyle: 'medium',
            }).format(at)
          : entry.at
        const relative = known ? formatRelativeTime(at, new Date(), language) : ''
        return (
          <>
            <time dateTime={entry.at}>{absolute}</time>
            {relative !== '' ? <span className="audit__relative">{relative}</span> : null}
          </>
        )
      },
    },
    {
      id: 'user',
      header: t('table.actor'),
      sortValue: (entry) => entry.actor,
      cell: (entry) => entry.actor || '—',
    },
    {
      id: 'action',
      header: t('table.action'),
      sortValue: (entry) => entry.action,
      cell: (entry) =>
        t(`actions.${entry.action}`, { defaultValue: entry.action || t('unknownAction') }),
    },
    {
      id: 'target',
      header: t('table.target'),
      cell: (entry) => {
        const target = detailTarget(entry.details)
        return target === '' ? '—' : target
      },
    },
    {
      id: 'server',
      header: t('table.server'),
      cell: (entry) => {
        const serverID = typeof entry.details?.server_id === 'string' ? entry.details.server_id : ''
        return serverID === '' ? t('unknownServer') : (serverNames?.[serverID] ?? serverID)
      },
    },
    {
      id: 'result',
      header: t('table.result'),
      sortValue: (entry) => entry.result,
      cell: (entry) => {
        const tone = resultTone(entry.result)
        return (
          <StatusBadge
            status={resultStatus(tone)}
            label={t(`result.${tone}`, { defaultValue: entry.result })}
          />
        )
      },
    },
    {
      id: 'details',
      header: t('table.details'),
      cell: (entry) =>
        entry.details !== null && Object.keys(entry.details).length > 0 ? (
          <button type="button" className="ui-btn" onClick={() => setDetails(entry)}>
            {t('details.show')}
          </button>
        ) : (
          <span className="audit__muted">{t('details.empty')}</span>
        ),
    },
  ]

  if (!permissionsLoading && !canView && !canRecordings) return null

  return (
    <section className="audit" aria-label={t('title')} aria-busy={loading || appending}>
      <PageHeader title={t('title')} subtitle={t('subtitle')} />

      <div className="audit__tabs" role="tablist" aria-label={t('tabs.label')}>
        {canView ? (
          <Link
            id="audit-tab-log"
            to="/audit"
            role="tab"
            aria-selected={activeTab === 'audit'}
            aria-controls="audit-panel-log"
            tabIndex={activeTab === 'audit' ? 0 : -1}
            className={activeTab === 'audit' ? 'audit__tab audit__tab--on' : 'audit__tab'}
          >
            {t('tabs.log')}
          </Link>
        ) : null}
        {canRecordings ? (
          <Link
            id="audit-tab-recordings"
            to="/audit/recordings"
            role="tab"
            aria-selected={activeTab === 'recordings'}
            aria-controls="audit-panel-recordings"
            tabIndex={activeTab === 'recordings' ? 0 : -1}
            className={activeTab === 'recordings' ? 'audit__tab audit__tab--on' : 'audit__tab'}
          >
            {t('tabs.recordings')}
          </Link>
        ) : null}
      </div>

      {activeTab === 'audit' && canView ? (
        <div id="audit-panel-log" role="tabpanel" aria-labelledby="audit-tab-log">
          <FilterBar
            search={{
              value: q,
              onChange: (value) => setQuery({ q: value }),
              placeholder: t('filters.search'),
            }}
          >
            <Select
              label={t('filters.action')}
              value={action}
              options={actionGroups.map((group) => ({
                value: group.value,
                label: t(group.key),
              }))}
              onChange={changeAction}
            />
            <Select
              label={t('filters.server')}
              value={server}
              options={serverOptions}
              onChange={changeServer}
            />
            <form className="audit__actor" onSubmit={applyActor}>
              <label className="ui-field">
                <span className="ui-field__label">{t('filters.actor')}</span>
                <input
                  className="ui-filter-bar__input"
                  type="search"
                  value={actor}
                  placeholder={t('filters.actorPlaceholder')}
                  onChange={(event) => setActor(event.target.value)}
                />
              </label>
              <button type="submit" className="ui-btn">
                {t('filters.apply')}
              </button>
            </form>
          </FilterBar>

          <DataTable
            columns={columns}
            rows={visibleEntries}
            rowKey={(entry) => entry.id}
            loading={loading}
            error={error ?? undefined}
            onRetry={() => void load('', false)}
            empty={<p className="audit__message">{t('empty')}</p>}
          />

          {!loading && error === null && next !== '' ? (
            <div className="audit__more">
              <button
                type="button"
                className="ui-btn"
                disabled={appending}
                onClick={() => void load(next, true)}
              >
                {appending ? t('loadingMore') : t('loadMore')}
              </button>
            </div>
          ) : null}
        </div>
      ) : canRecordings ? (
        <div id="audit-panel-recordings" role="tabpanel" aria-labelledby="audit-tab-recordings">
          <RecordingsTab api={recordings} />
        </div>
      ) : null}

      <Drawer open={details !== null} title={t('details.title')} onClose={() => setDetails(null)}>
        {details !== null ? (
          <dl className="audit__details">
            {Object.entries(details.details ?? {})
              .slice(0, 8)
              .map(([key, value]) => (
                <div key={key} className="audit__detail">
                  <dt>{key}</dt>
                  <dd>{formatDetail(value)}</dd>
                </div>
              ))}
          </dl>
        ) : null}
      </Drawer>
    </section>
  )
}
