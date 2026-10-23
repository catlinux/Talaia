import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { createAuditApi, type AuditApi, type AuditEntry } from '../api/audit'
import { createRecordingsApi, type RecordingsApi } from '../api/recordings'
import { isSignedOut } from '../api/http'
import { errorMessage } from '../auth/AuthGate'
import { usePermissions } from '../auth/permissions'
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

/**
 * Audit lists the append-only audit log newest first. Filters reset the list and
 * Load more follows the opaque cursor; details are always rendered as text.
 */
export function Audit({ api = defaultApi, limit = 50, serverNames, recordingsApi }: AuditProps) {
  const { t, i18n } = useTranslation(['audit', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const { can, loading: permissionsLoading } = usePermissions()
  const canView = !permissionsLoading && can('audit.view')
  const canRecordings = !permissionsLoading && can('recordings.view')
  const recordings = recordingsApi ?? defaultRecordingsApi
  const [tab, setTab] = useState<'audit' | 'recordings'>('audit')
  const activeTab: 'audit' | 'recordings' = !canView
    ? 'recordings'
    : tab === 'recordings' && !canRecordings
      ? 'audit'
      : tab
  const [entries, setEntries] = useState<AuditEntry[]>([])
  const [next, setNext] = useState('')
  const [loading, setLoading] = useState(true)
  const [appending, setAppending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [action, setAction] = useState('')
  const [actor, setActor] = useState('')
  const [appliedActor, setAppliedActor] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const request = useRef(0)

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
          actor: appliedActor || undefined,
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
    [action, api, appliedActor, limit, t],
  )

  useEffect(() => {
    if (!canView || activeTab !== 'audit') return
    // Start after the effect returns: load updates state when the page arrives,
    // and this keeps the first render free of a cascading update.
    queueMicrotask(() => {
      void load('', false)
    })
  }, [activeTab, canView, load])

  useEffect(() => {
    const trimmed = actor.trim()
    if (trimmed === appliedActor) return
    const timer = setTimeout(() => setAppliedActor(trimmed), 300)
    return () => clearTimeout(timer)
  }, [actor, appliedActor])

  const applyActor = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setAppliedActor(actor.trim())
  }

  const changeAction = (value: string) => {
    setAction(value)
    setExpanded(new Set())
  }

  const toggleDetails = (id: string) => {
    setExpanded((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  if (!permissionsLoading && !canView && !canRecordings) {
    return null
  }

  return (
    <section className="audit" aria-labelledby="audit-title" aria-busy={loading || appending}>
      <header className="audit__head">
        <h1 className="audit__title" id="audit-title">
          {t('title')}
        </h1>
        <p className="audit__subtitle">{t('subtitle')}</p>
      </header>

      <div className="audit__tabs" role="tablist" aria-label={t('tabs.label')}>
        {canView ? (
          <button
            id="audit-tab-log"
            type="button"
            role="tab"
            aria-selected={activeTab === 'audit'}
            aria-controls="audit-panel-log"
            className={activeTab === 'audit' ? 'audit__tab audit__tab--on' : 'audit__tab'}
            onClick={() => setTab('audit')}
          >
            {t('tabs.log')}
          </button>
        ) : null}
        {canRecordings ? (
          <button
            id="audit-tab-recordings"
            type="button"
            role="tab"
            aria-selected={activeTab === 'recordings'}
            aria-controls="audit-panel-recordings"
            className={activeTab === 'recordings' ? 'audit__tab audit__tab--on' : 'audit__tab'}
            onClick={() => setTab('recordings')}
          >
            {t('tabs.recordings')}
          </button>
        ) : null}
      </div>

      {activeTab === 'audit' && canView ? (
        <div id="audit-panel-log" role="tabpanel" aria-labelledby="audit-tab-log">
          <div className="audit__filters" role="group" aria-label={t('filters.label')}>
            <label className="audit__field">
              <span>{t('filters.action')}</span>
              <select value={action} onChange={(event) => changeAction(event.target.value)}>
                {actionGroups.map((group) => (
                  <option key={group.value} value={group.value}>
                    {t(group.key)}
                  </option>
                ))}
              </select>
            </label>
            <form className="audit__actor-form" onSubmit={applyActor}>
              <label className="audit__field">
                <span>{t('filters.actor')}</span>
                <input
                  type="search"
                  value={actor}
                  placeholder={t('filters.actorPlaceholder')}
                  onChange={(event) => setActor(event.target.value)}
                />
              </label>
              <button type="submit" className="cbtn">
                {t('filters.apply')}
              </button>
            </form>
          </div>

          {error !== null ? (
            <p className="audit__message audit__message--error" role="alert">
              {error}
            </p>
          ) : null}

          {loading ? (
            <p className="audit__message" role="status">
              {t('loading')}
            </p>
          ) : null}

          {!loading && error === null && entries.length === 0 ? (
            <p className="audit__message">{t('empty')}</p>
          ) : null}

          {entries.length > 0 ? (
            <div className="audit__table-wrap">
              <table className="audit__table">
                <thead>
                  <tr>
                    <th scope="col">{t('table.time')}</th>
                    <th scope="col">{t('table.actor')}</th>
                    <th scope="col">{t('table.action')}</th>
                    <th scope="col">{t('table.result')}</th>
                    <th scope="col">{t('table.server')}</th>
                    <th scope="col">{t('table.details')}</th>
                  </tr>
                </thead>
                <tbody>
                  {entries.map((entry) => {
                    const at = new Date(entry.at)
                    const known = !Number.isNaN(at.getTime())
                    const absolute = known
                      ? new Intl.DateTimeFormat(language, {
                          dateStyle: 'medium',
                          timeStyle: 'medium',
                        }).format(at)
                      : entry.at
                    const relative = known ? formatRelativeTime(at, new Date(), language) : ''
                    const serverID =
                      typeof entry.details?.server_id === 'string' ? entry.details.server_id : ''
                    const server =
                      serverID === '' ? t('unknownServer') : (serverNames?.[serverID] ?? serverID)
                    const tone = resultTone(entry.result)
                    const result = t(`result.${tone}`, { defaultValue: entry.result })
                    const actionLabel = t(`actions.${entry.action}`, {
                      defaultValue: entry.action || t('unknownAction'),
                    })
                    const details = entry.details ?? {}
                    const detailRows = Object.entries(details).slice(0, 8)
                    const open = expanded.has(entry.id)
                    return (
                      <FragmentRows
                        key={entry.id}
                        entry={entry}
                        absolute={absolute}
                        relative={relative}
                        server={server}
                        result={result}
                        tone={tone}
                        actionLabel={actionLabel}
                        detailRows={detailRows}
                        open={open}
                        onToggle={() => toggleDetails(entry.id)}
                        labels={{
                          details: t('details.show'),
                          hide: t('details.hide'),
                          empty: t('details.empty'),
                        }}
                      />
                    )
                  })}
                </tbody>
              </table>
            </div>
          ) : null}

          {!loading && next !== '' ? (
            <div className="audit__more">
              <button
                type="button"
                className="cbtn"
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
    </section>
  )
}

interface FragmentRowsProps {
  entry: AuditEntry
  absolute: string
  relative: string
  server: string
  result: string
  tone: 'success' | 'failure' | 'denied' | 'requested' | 'unknown'
  actionLabel: string
  detailRows: Array<[string, unknown]>
  open: boolean
  onToggle: () => void
  labels: { details: string; hide: string; empty: string }
}

/** FragmentRows is one event and, when open, its details row. */
function FragmentRows({
  entry,
  absolute,
  relative,
  server,
  result,
  tone,
  actionLabel,
  detailRows,
  open,
  onToggle,
  labels,
}: FragmentRowsProps) {
  const { t } = useTranslation('audit')
  return (
    <>
      <tr className="audit__row">
        <td data-label={t('table.time')}>
          <time dateTime={entry.at}>{absolute}</time>
          {relative !== '' ? <span className="audit__relative">{relative}</span> : null}
        </td>
        <td data-label={t('table.actor')}>{entry.actor || '—'}</td>
        <td data-label={t('table.action')}>{actionLabel}</td>
        <td data-label={t('table.result')}>
          <span className={`audit__result audit__result--${tone}`}>{result}</span>
        </td>
        <td data-label={t('table.server')}>{server}</td>
        <td data-label={t('table.details')}>
          {detailRows.length > 0 ? (
            <button
              type="button"
              className="audit__details-toggle"
              aria-expanded={open}
              onClick={onToggle}
            >
              {open ? labels.hide : labels.details}
            </button>
          ) : (
            <span className="audit__muted">{labels.empty}</span>
          )}
        </td>
      </tr>
      {open && detailRows.length > 0 ? (
        <tr className="audit__details-row">
          <td colSpan={6}>
            <dl className="audit__details">
              {detailRows.map(([key, value]) => (
                <div key={key} className="audit__detail">
                  <dt>{key}</dt>
                  <dd>{formatDetail(value)}</dd>
                </div>
              ))}
            </dl>
          </td>
        </tr>
      ) : null}
    </>
  )
}
