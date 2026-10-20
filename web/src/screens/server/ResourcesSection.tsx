import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type {
  ContainersApi,
  RemovalResult,
  ResourceItem,
  ResourceKind,
  ResourceListing,
} from '../../api/containers'
import { isSignedOut } from '../../api/http'
import { errorMessage } from '../../auth/AuthGate'
import { formatBytes } from '../fleet/format'
import { TimedOut, waitForJob } from './waitForJob'

const kinds: ResourceKind[] = ['image', 'volume', 'network']

interface ResourcesSectionProps {
  api: ContainersApi
  serverId: string
  pollMs: number
}

type ListState =
  | { status: 'loading' }
  | { status: 'ready'; items: ResourceItem[] }
  | { status: 'error'; message: string }

type Removal =
  | { phase: 'idle' }
  | { phase: 'confirming' }
  | { phase: 'working' }
  | { phase: 'done'; results: RemovalResult[] }
  | { phase: 'failed'; message: string }

/** labelOf is the name of a resource a person recognises. */
function labelOf(item: ResourceItem, untagged: string): string {
  if (item.name) return item.name
  if (item.tags && item.tags.length > 0) return item.tags.join(', ')
  return `${item.id.replace('sha256:', '').slice(0, 12)} (${untagged})`
}

/** removable tells whether a resource can be offered for removal. */
function removable(item: ResourceItem): boolean {
  return !item.in_use && item.system !== true
}

/**
 * ResourcesSection lists the images, volumes and networks of a server and cleans
 * up the ones nothing uses. What is listed is what is removed: the selection goes
 * to the agent resource by resource, never forced, so Docker refuses what is
 * still in use. Nothing is removed without a confirmation.
 */
export function ResourcesSection({ api, serverId, pollMs }: ResourcesSectionProps) {
  const { t, i18n } = useTranslation(['server', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [open, setOpen] = useState(false)
  const [kind, setKind] = useState<ResourceKind>('image')
  const [state, setState] = useState<ListState>({ status: 'loading' })
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [removal, setRemoval] = useState<Removal>({ phase: 'idle' })
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  // fetchList asks for the listing of the current kind and shows it when it comes.
  const fetchList = useCallback(
    async (listKind: ResourceKind) => {
      try {
        const jobId = await api.listResources(serverId, listKind)
        const job = await waitForJob(api, jobId, pollMs, () => alive.current)
        if (job === null || !alive.current) return
        if (job.state !== 'succeeded') {
          setState({ status: 'error', message: job.message })
          return
        }
        const listing = JSON.parse(job.output) as ResourceListing
        setState({ status: 'ready', items: listing.items })
      } catch (error) {
        if (!alive.current || isSignedOut(error)) return
        setState({
          status: 'error',
          message:
            error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error),
        })
      }
    },
    [api, pollMs, serverId, t],
  )

  useEffect(() => {
    if (open) void fetchList(kind)
  }, [fetchList, kind, open])

  const reload = (next: ResourceKind) => {
    setState({ status: 'loading' })
    setSelected(new Set())
    setRemoval({ phase: 'idle' })
    if (next === kind) void fetchList(next)
    else setKind(next)
  }

  const items = state.status === 'ready' ? state.items : []
  const unused = items.filter(removable)
  const freeable = unused.reduce((sum, item) => sum + (item.size ?? 0), 0)

  const toggle = (id: string) => {
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const remove = async () => {
    const chosen = items.filter((item) => selected.has(item.id) && removable(item))
    setRemoval({ phase: 'working' })
    try {
      const jobId = await api.removeResources(
        serverId,
        chosen.map((item) => ({ kind, id: item.id })),
      )
      const job = await waitForJob(api, jobId, pollMs, () => alive.current)
      if (job === null || !alive.current) return
      if (job.state !== 'succeeded') {
        setRemoval({ phase: 'failed', message: job.message })
        return
      }
      const results = (JSON.parse(job.output) as { results: RemovalResult[] }).results
      setSelected(new Set())
      setRemoval({ phase: 'done', results })
      setState({ status: 'loading' })
      void fetchList(kind)
    } catch (error) {
      if (!alive.current || isSignedOut(error)) return
      setRemoval({
        phase: 'failed',
        message: error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error),
      })
    }
  }

  const chosenCount = items.filter((item) => selected.has(item.id) && removable(item)).length

  return (
    <section className="resources" aria-labelledby="resources-title">
      <header className="stacks__head">
        <div>
          <h2 className="stacks__title" id="resources-title">
            {t('server:resources.title')}
          </h2>
          <p className="stacks__intro">{t('server:resources.intro')}</p>
        </div>
        <button
          type="button"
          className="cbtn"
          aria-expanded={open}
          onClick={() => setOpen((current) => !current)}
        >
          {open ? t('server:resources.hide') : t('server:resources.show')}
        </button>
      </header>

      {open ? (
        <div className="resources__body">
          <div className="resources__tabs" role="tablist">
            {kinds.map((candidate) => (
              <button
                key={candidate}
                type="button"
                role="tab"
                aria-selected={candidate === kind}
                className={
                  candidate === kind ? 'resources__tab resources__tab--on' : 'resources__tab'
                }
                onClick={() => reload(candidate)}
              >
                {t(`server:resources.kinds.${candidate}`)}
              </button>
            ))}
          </div>

          {state.status === 'loading' ? (
            <p className="resources__message" role="status">
              {t('server:resources.loading')}
            </p>
          ) : null}
          {state.status === 'error' ? (
            <p className="resources__message resources__message--error" role="alert">
              {t('server:progress.failed', { message: state.message })}
            </p>
          ) : null}

          {state.status === 'ready' && items.length === 0 ? (
            <p className="resources__message">{t('server:resources.empty')}</p>
          ) : null}

          {state.status === 'ready' && items.length > 0 ? (
            <>
              <div className="resources__toolbar">
                <p className="resources__summary">
                  {kind === 'image' && freeable > 0
                    ? t('server:resources.summarySize', {
                        unused: unused.length,
                        total: items.length,
                        size: formatBytes(freeable, language),
                      })
                    : t('server:resources.summary', { unused: unused.length, total: items.length })}
                </p>
                <div className="resources__actions">
                  <button
                    type="button"
                    className="cbtn"
                    disabled={unused.length === 0}
                    onClick={() => setSelected(new Set(unused.map((item) => item.id)))}
                  >
                    {t('server:resources.selectUnused')}
                  </button>
                  <button
                    type="button"
                    className="cbtn"
                    disabled={selected.size === 0}
                    onClick={() => setSelected(new Set())}
                  >
                    {t('server:resources.clearSelection')}
                  </button>
                  <button type="button" className="cbtn" onClick={() => reload(kind)}>
                    {t('server:resources.refresh')}
                  </button>
                </div>
              </div>

              <ul className="resourcelist">
                {items.map((item) => {
                  const label = labelOf(item, t('server:resources.untagged'))
                  return (
                    <li
                      key={item.id}
                      className={item.in_use ? 'resource resource--used' : 'resource'}
                    >
                      <input
                        type="checkbox"
                        className="resource__check"
                        aria-label={t('server:resources.selectRow', { name: label })}
                        disabled={!removable(item)}
                        checked={selected.has(item.id)}
                        onChange={() => toggle(item.id)}
                      />
                      <div className="resource__main">
                        <span className="resource__name">{label}</span>
                        <span className="resource__meta">
                          {[
                            item.driver,
                            item.size ? formatBytes(item.size, language) : null,
                            item.created ? item.created.slice(0, 10) : null,
                          ]
                            .filter(Boolean)
                            .join(' · ')}
                        </span>
                      </div>
                      <span
                        className={`status status--${item.in_use ? 'online' : item.system ? 'offline' : 'warning'}`}
                      >
                        {item.system
                          ? t('server:resources.system')
                          : item.in_use
                            ? t('server:resources.inUse')
                            : t('server:resources.free')}
                      </span>
                    </li>
                  )
                })}
              </ul>

              {removal.phase === 'confirming' ? (
                <div
                  className="container__confirm resources__confirm"
                  role="group"
                  aria-label={t('server:resources.confirmTitle', { count: chosenCount })}
                >
                  <p className="container__confirm-title">
                    {t('server:resources.confirmTitle', { count: chosenCount })}
                  </p>
                  <p className="container__confirm-body">{t('server:resources.confirmBody')}</p>
                  {kind === 'volume' ? (
                    <p className="container__confirm-body">
                      {t('server:resources.confirmVolumes')}
                    </p>
                  ) : null}
                  <div className="container__actions">
                    <button
                      type="button"
                      className="cbtn cbtn--danger"
                      onClick={() => void remove()}
                    >
                      {t('server:resources.confirmYes')}
                    </button>
                    <button
                      type="button"
                      className="cbtn"
                      onClick={() => setRemoval({ phase: 'idle' })}
                    >
                      {t('server:resources.confirmNo')}
                    </button>
                  </div>
                </div>
              ) : (
                <div className="resources__footer">
                  <button
                    type="button"
                    className="cbtn cbtn--danger"
                    disabled={chosenCount === 0 || removal.phase === 'working'}
                    onClick={() => setRemoval({ phase: 'confirming' })}
                  >
                    {removal.phase === 'working'
                      ? t('server:resources.removing')
                      : t('server:resources.removeSelected', { count: chosenCount })}
                  </button>
                </div>
              )}
            </>
          ) : null}

          {removal.phase === 'failed' ? (
            <p className="resources__message resources__message--error" role="alert">
              {t('server:progress.failed', { message: removal.message })}
            </p>
          ) : null}
          {removal.phase === 'done' ? <RemovalReport results={removal.results} /> : null}
        </div>
      ) : null}
    </section>
  )
}

function RemovalReport({ results }: { results: RemovalResult[] }) {
  const { t } = useTranslation('server')
  const ok = results.filter((result) => result.ok).length

  return (
    <div className="resources__report" role="status">
      <p className="resources__summary">
        {t('resources.summaryDone', { ok, total: results.length })}
      </p>
      <ul className="resources__results">
        {results
          .filter((result) => !result.ok)
          .map((result) => (
            <li key={result.id} className="container__outcome container__outcome--error">
              {result.id}: {t('resources.resultFailed', { message: result.message ?? '' })}
            </li>
          ))}
      </ul>
    </div>
  )
}
