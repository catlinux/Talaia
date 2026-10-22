import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { isSignedOut } from '../../api/http'
import {
  parseJournalPage,
  type JournalApi,
  type JournalBoot,
  type JournalEntry,
  type JournalQuery,
} from '../../api/journal'
import type { ServiceUnit, ServicesApi } from '../../api/services'
import { errorMessage } from '../../auth/AuthGate'
import { TimedOut, waitForJob } from './waitForJob'

/** followEvery is how often the journal asks for new entries while following. */
export const followEvery = 3000
/** maxFollowLines is how many entries the section keeps in memory. */
export const maxFollowLines = 2000
/** stickThreshold is how close to the bottom counts as being at the bottom. */
const stickThreshold = 8

const lineOptions = [100, 200, 500, 1000] as const
const bootOptions: JournalBoot[] = ['all', 'current', 'previous']
const rangeOptions = ['all', '15m', '1h', '24h', '7d', 'custom'] as const

type RangeKey = (typeof rangeOptions)[number]

const rangeMilliseconds: Record<Exclude<RangeKey, 'all' | 'custom'>, number> = {
  '15m': 15 * 60_000,
  '1h': 60 * 60_000,
  '24h': 24 * 60 * 60_000,
  '7d': 7 * 24 * 60 * 60_000,
}

const priorityKeys = [
  'emerg',
  'alert',
  'crit',
  'err',
  'warning',
  'notice',
  'info',
  'debug',
] as const
const priorityChoices = [
  { value: 'all', maxPriority: null, label: 'all' },
  { value: '3', maxPriority: 3, label: 'err' },
  { value: '4', maxPriority: 4, label: 'warning' },
  { value: '5', maxPriority: 5, label: 'notice' },
  { value: '6', maxPriority: 6, label: 'info' },
  { value: '7', maxPriority: 7, label: 'debug' },
] as const

const defaultQuery: JournalQuery = {
  unit: '',
  maxPriority: null,
  since: '',
  until: '',
  search: '',
  boot: 'all',
  tail: 200,
  afterCursor: '',
}

interface JournalSectionProps {
  api: JournalApi
  servicesApi?: ServicesApi
  services?: ServiceUnit[]
  serverId: string
  pollMs: number
}

type JournalState =
  | { status: 'loading' }
  | { status: 'ready'; entries: JournalEntry[] }
  | { status: 'error'; message: string }

function priorityKey(priority: number): string {
  return priorityKeys[priority] ?? 'info'
}

function priorityTone(priority: number): 'critical' | 'warning' | 'neutral' {
  if (priority <= 3) return 'critical'
  if (priority === 4) return 'warning'
  return 'neutral'
}

function escapePattern(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** Highlighted renders literal search matches without injecting HTML. */
function Highlighted({ text, query }: { text: string; query: string }) {
  if (query === '') return <>{text}</>
  const parts = text.split(new RegExp(`(${escapePattern(query)})`, 'gi'))
  return (
    <>
      {parts.map((part, index) =>
        index % 2 === 1 ? (
          <mark className="journal__mark" key={index}>
            {part}
          </mark>
        ) : (
          part
        ),
      )}
    </>
  )
}

/** localDateToUTC converts a datetime-local value to RFC 3339 UTC. */
function localDateToUTC(value: string): string | null {
  if (value === '') return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  return date.toISOString()
}

/**
 * JournalSection reads the systemd journal of a server. Filters are applied
 * with Search; Follow polls for newer entries from the returned cursor.
 */
export function JournalSection({
  api,
  servicesApi,
  services,
  serverId,
  pollMs,
}: JournalSectionProps) {
  const { t, i18n } = useTranslation(['server', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [state, setState] = useState<JournalState>({ status: 'loading' })
  const [unit, setUnit] = useState('')
  const [maxPriority, setMaxPriority] = useState<number | null>(null)
  const [boot, setBoot] = useState<JournalBoot>('all')
  const [range, setRange] = useState<RangeKey>('all')
  const [customSince, setCustomSince] = useState('')
  const [customUntil, setCustomUntil] = useState('')
  const [search, setSearch] = useState('')
  const [appliedSearch, setAppliedSearch] = useState('')
  const [tail, setTail] = useState<number>(200)
  const [appliedQuery, setAppliedQuery] = useState<JournalQuery>(defaultQuery)
  const [follow, setFollow] = useState(false)
  const [rangeError, setRangeError] = useState<string | null>(null)
  const [loadedUnits, setLoadedUnits] = useState<ServiceUnit[]>([])

  const alive = useRef(true)
  const operation = useRef(0)
  const cursor = useRef('')
  const followRef = useRef(false)
  const busy = useRef(false)
  const viewer = useRef<HTMLDivElement>(null)
  const stickToBottom = useRef(true)

  const errorText = useCallback(
    (error: unknown) =>
      error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error),
    [t],
  )

  const fail = useCallback((message: string) => {
    operation.current += 1
    followRef.current = false
    setFollow(false)
    setState({ status: 'error', message })
  }, [])

  const clock = useMemo(
    () =>
      new Intl.DateTimeFormat(language, {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hourCycle: 'h23',
      }),
    [language],
  )

  const loadOnce = useCallback(
    async (query: JournalQuery) => {
      const token = (operation.current += 1)
      setRangeError(null)
      setState({ status: 'loading' })
      try {
        const jobId = await api.read(serverId, query)
        const job = await waitForJob(
          api,
          jobId,
          pollMs,
          () => alive.current && token === operation.current,
        )
        if (job === null || !alive.current || token !== operation.current) return
        if (job.state !== 'succeeded') {
          setState({ status: 'error', message: job.message || t('server:journal.invalid') })
          return
        }
        let page
        try {
          page = parseJournalPage(job.output)
        } catch {
          setState({ status: 'error', message: t('server:journal.invalid') })
          return
        }
        cursor.current = page.cursor
        setState({ status: 'ready', entries: page.entries })
      } catch (error) {
        if (!alive.current || token !== operation.current || isSignedOut(error)) return
        setState({ status: 'error', message: errorText(error) })
      }
    },
    [api, errorText, pollMs, serverId, t],
  )

  const loadOnceRef = useRef(loadOnce)
  useEffect(() => {
    loadOnceRef.current = loadOnce
  }, [loadOnce])

  const fetchFollow = useCallback(async () => {
    if (busy.current || !alive.current || !followRef.current) return
    busy.current = true
    const token = operation.current
    try {
      const query = { ...appliedQuery, afterCursor: cursor.current }
      const jobId = await api.read(serverId, query)
      const job = await waitForJob(
        api,
        jobId,
        pollMs,
        () => alive.current && followRef.current && token === operation.current,
      )
      if (job === null || !alive.current || !followRef.current || token !== operation.current)
        return
      if (job.state !== 'succeeded') {
        fail(job.message || t('server:journal.invalid'))
        return
      }
      let page
      try {
        page = parseJournalPage(job.output)
      } catch {
        fail(t('server:journal.invalid'))
        return
      }
      const previous = cursor.current
      cursor.current = page.cursor !== '' ? page.cursor : page.entries.length === 0 ? previous : ''
      setState((current) => {
        const base = current.status === 'ready' ? current.entries : []
        const merged = [...base, ...page.entries]
        const entries =
          merged.length > maxFollowLines ? merged.slice(merged.length - maxFollowLines) : merged
        return { status: 'ready', entries }
      })
    } catch (error) {
      if (!alive.current || token !== operation.current || isSignedOut(error)) return
      fail(errorText(error))
    } finally {
      busy.current = false
    }
  }, [api, appliedQuery, errorText, fail, pollMs, serverId, t])

  useEffect(() => {
    alive.current = true
    queueMicrotask(() => {
      void loadOnceRef.current(defaultQuery)
    })
    return () => {
      alive.current = false
      operation.current += 1
    }
  }, [])

  useEffect(() => {
    if (services !== undefined || servicesApi === undefined) return
    let live = true
    void servicesApi.list(serverId).then(
      (listed) => {
        if (live) setLoadedUnits(listed)
      },
      () => undefined,
    )
    return () => {
      live = false
    }
  }, [serverId, services, servicesApi])

  const units = services ?? loadedUnits

  const hasAppliedUntil = Boolean(appliedQuery.until)
  const hasDraftUntil = range === 'custom' && customUntil !== ''
  const followDisabled = hasAppliedUntil || hasDraftUntil

  useEffect(() => {
    if (followDisabled && followRef.current) {
      operation.current += 1
      followRef.current = false
      setFollow(false)
    }
  }, [followDisabled])

  useEffect(() => {
    if (!follow) return
    let cancelled = false
    let running = false
    const poll = () => {
      if (cancelled || running || document.hidden) return
      running = true
      void fetchFollow().finally(() => {
        running = false
      })
    }
    poll()
    const timer = setInterval(poll, followEvery)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [fetchFollow, follow])

  const buildQuery = useCallback((): { query: JournalQuery } | { error: string } => {
    let since = ''
    let until = ''
    if (range === 'custom') {
      const from = localDateToUTC(customSince)
      const to = localDateToUTC(customUntil)
      if (from === null || to === null) {
        return { error: t('server:journal.rangeInvalid') }
      }
      if (from !== '' && to !== '' && Date.parse(to) <= Date.parse(from)) {
        return { error: t('server:journal.rangeInvalid') }
      }
      since = from
      until = to
    } else if (range !== 'all') {
      since = new Date(Date.now() - rangeMilliseconds[range]).toISOString()
    }

    return {
      query: {
        unit: unit.trim(),
        maxPriority,
        since,
        until,
        search,
        boot,
        tail,
        afterCursor: '',
      },
    }
  }, [boot, customSince, customUntil, maxPriority, range, search, t, tail, unit])

  const applyFilters = () => {
    const built = buildQuery()
    if ('error' in built) {
      setRangeError(built.error)
      return
    }
    setRangeError(null)
    setAppliedSearch(search)
    setAppliedQuery(built.query)
    operation.current += 1
    followRef.current = false
    setFollow(false)
    stickToBottom.current = true
    void loadOnce(built.query)
  }

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    applyFilters()
  }

  const toggleFollow = () => {
    if (follow) {
      operation.current += 1
      followRef.current = false
      setFollow(false)
      return
    }
    if (followDisabled || state.status !== 'ready') return
    stickToBottom.current = true
    followRef.current = true
    setFollow(true)
  }

  const handleScroll = () => {
    const element = viewer.current
    if (!element) return
    stickToBottom.current =
      element.scrollHeight - element.scrollTop - element.clientHeight <= stickThreshold
  }

  useEffect(() => {
    const element = viewer.current
    if (!element || state.status !== 'ready') return
    if (!follow || stickToBottom.current) {
      element.scrollTop = element.scrollHeight
    }
  }, [state, follow])

  const failedEntries = state.status === 'ready' && state.entries.length === 0
  const canDownload = state.status === 'ready' && state.entries.length > 0
  const loading = state.status === 'loading'

  const download = () => {
    if (state.status !== 'ready' || state.entries.length === 0) return
    if (typeof URL.createObjectURL !== 'function') return
    const text = state.entries
      .map((entry) => `${entry.ts}\t${entry.unit}\t${entry.priority}\t${entry.message}`)
      .join('\n')
    const url = URL.createObjectURL(new Blob([`${text}\n`], { type: 'text/plain;charset=utf-8' }))
    const link = document.createElement('a')
    link.href = url
    link.download = `journal-${serverId}.txt`
    document.body.appendChild(link)
    link.click()
    link.remove()
    if (typeof URL.revokeObjectURL === 'function') URL.revokeObjectURL(url)
  }

  return (
    <section className="journal" aria-labelledby="journal-title" aria-busy={loading}>
      <header className="stacks__head">
        <div>
          <h2 className="stacks__title" id="journal-title">
            {t('server:journal.title')}
          </h2>
          <p className="stacks__intro">{t('server:journal.intro')}</p>
        </div>
        <div className="journal__head-actions">
          {follow ? (
            <span className="journal__live" role="status">
              {t('server:journal.live')}
            </span>
          ) : null}
          <button type="button" className="cbtn" disabled={!canDownload} onClick={download}>
            {t('server:journal.download')}
          </button>
        </div>
      </header>

      <form className="journal__toolbar" onSubmit={submit}>
        <div className="journal__field journal__field--unit">
          <label className="journal__label" htmlFor="journal-unit">
            {t('server:journal.unit')}
          </label>
          <input
            id="journal-unit"
            className="journal__control"
            type="text"
            list="journal-units"
            value={unit}
            placeholder={t('server:journal.unitPlaceholder')}
            autoComplete="off"
            aria-describedby="journal-unit-hint"
            onChange={(event) => setUnit(event.target.value)}
          />
          <span className="journal__hint" id="journal-unit-hint">
            {t('server:journal.unitHint')}
          </span>
          <datalist id="journal-units">
            {units.map((service) => (
              <option key={service.name} value={service.name}>
                {service.description}
              </option>
            ))}
          </datalist>
        </div>

        <div className="journal__field">
          <label className="journal__label" htmlFor="journal-priority">
            {t('server:journal.priority')}
          </label>
          <select
            id="journal-priority"
            className="journal__control"
            value={maxPriority === null ? 'all' : String(maxPriority)}
            onChange={(event) => {
              const choice = priorityChoices.find(
                (candidate) => candidate.value === event.target.value,
              )
              setMaxPriority(choice?.maxPriority ?? null)
            }}
          >
            {priorityChoices.map((choice) => (
              <option key={choice.value} value={choice.value}>
                {t(`server:journal.priorities.${choice.label}`)}
              </option>
            ))}
          </select>
        </div>

        <div className="journal__field">
          <label className="journal__label" htmlFor="journal-boot">
            {t('server:journal.boot')}
          </label>
          <select
            id="journal-boot"
            className="journal__control"
            value={boot}
            onChange={(event) => setBoot(event.target.value as JournalBoot)}
          >
            {bootOptions.map((option) => (
              <option key={option} value={option}>
                {t(`server:journal.boots.${option}`)}
              </option>
            ))}
          </select>
        </div>

        <div className="journal__field">
          <label className="journal__label" htmlFor="journal-range">
            {t('server:journal.range')}
          </label>
          <select
            id="journal-range"
            className="journal__control"
            value={range}
            onChange={(event) => {
              setRange(event.target.value as RangeKey)
              setRangeError(null)
            }}
          >
            {rangeOptions.map((option) => (
              <option key={option} value={option}>
                {t(`server:journal.ranges.${option}`)}
              </option>
            ))}
          </select>
        </div>

        {range === 'custom' ? (
          <>
            <div className="journal__field">
              <label className="journal__label" htmlFor="journal-since">
                {t('server:journal.since')}
              </label>
              <input
                id="journal-since"
                className="journal__control"
                type="datetime-local"
                value={customSince}
                onChange={(event) => {
                  setCustomSince(event.target.value)
                  setRangeError(null)
                }}
              />
            </div>
            <div className="journal__field">
              <label className="journal__label" htmlFor="journal-until">
                {t('server:journal.until')}
              </label>
              <input
                id="journal-until"
                className="journal__control"
                type="datetime-local"
                value={customUntil}
                onChange={(event) => {
                  setCustomUntil(event.target.value)
                  setRangeError(null)
                }}
              />
            </div>
          </>
        ) : null}

        <div className="journal__field journal__field--search">
          <label className="journal__label" htmlFor="journal-search">
            {t('server:journal.search')}
          </label>
          <input
            id="journal-search"
            className="journal__control"
            type="search"
            value={search}
            placeholder={t('server:journal.searchPlaceholder')}
            aria-describedby="journal-search-hint"
            onChange={(event) => setSearch(event.target.value)}
          />
          <span className="journal__hint" id="journal-search-hint">
            {t('server:journal.searchHint')}
          </span>
        </div>

        <div className="journal__field">
          <label className="journal__label" htmlFor="journal-lines">
            {t('server:journal.lines')}
          </label>
          <select
            id="journal-lines"
            className="journal__control"
            value={String(tail)}
            onChange={(event) => setTail(Number(event.target.value))}
          >
            {lineOptions.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </div>

        <div className="journal__actions">
          <button type="submit" className="cbtn cbtn--primary" disabled={loading}>
            {t('server:journal.search')}
          </button>
          <button
            type="button"
            className={follow ? 'cbtn cbtn--live' : 'cbtn'}
            aria-pressed={follow}
            disabled={followDisabled || loading || state.status === 'error'}
            title={followDisabled ? t('server:journal.followNeedsNoUntil') : undefined}
            onClick={toggleFollow}
          >
            {t('server:journal.follow')}
          </button>
        </div>
      </form>

      {rangeError !== null ? (
        <p className="journal__message journal__message--error" role="alert">
          {rangeError}
        </p>
      ) : null}
      {followDisabled ? (
        <p className="journal__hint">{t('server:journal.followNeedsNoUntil')}</p>
      ) : null}

      {state.status === 'loading' ? (
        <p className="journal__message" role="status">
          {t('server:journal.loading')}
        </p>
      ) : null}
      {state.status === 'error' ? (
        <p className="journal__message journal__message--error" role="alert">
          {t('server:journal.error', { message: state.message })}
        </p>
      ) : null}
      {failedEntries ? <p className="journal__empty">{t('server:journal.empty')}</p> : null}

      {state.status === 'ready' && state.entries.length > 0 ? (
        <>
          <div
            className="journal__viewer"
            ref={viewer}
            tabIndex={0}
            onScroll={handleScroll}
            aria-label={t('server:journal.content')}
          >
            <ol className="journal__list">
              {state.entries.map((entry, index) => {
                const time = Date.parse(entry.ts)
                const tone = priorityTone(entry.priority)
                return (
                  <li className="journal__entry" key={`${entry.ts}-${index}`}>
                    <time
                      className="journal__time"
                      dateTime={entry.ts || undefined}
                      title={entry.ts || undefined}
                    >
                      {Number.isNaN(time) ? entry.ts : clock.format(new Date(time))}
                    </time>
                    <span className={`journal__priority journal__priority--${tone}`}>
                      {t(`server:journal.priorityLevels.${priorityKey(entry.priority)}`)}
                    </span>
                    <span className="journal__unit">{entry.unit}</span>
                    <span className="journal__text">
                      <Highlighted text={entry.message} query={appliedSearch} />
                    </span>
                  </li>
                )
              })}
            </ol>
          </div>
          <p className="journal__note">
            {t(follow ? 'server:journal.noteLive' : 'server:journal.note')}
          </p>
        </>
      ) : null}
    </section>
  )
}
