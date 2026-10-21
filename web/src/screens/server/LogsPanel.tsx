import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { ContainersApi } from '../../api/containers'
import { isSignedOut } from '../../api/http'
import { errorMessage } from '../../auth/AuthGate'
import { CloseIcon, RestartIcon } from '../../components/icons'
import { TimedOut, waitForJob } from './waitForJob'

/** followEvery is how often the panel asks for new lines while following. */
export const followEvery = 3000
/** maxFollowLines is how many lines the panel keeps in memory. */
export const maxFollowLines = 2000
/** followTail is how many lines one follow request may bring back. */
const followTail = 1000
/** stickThreshold is how close to the bottom counts as being at the bottom. */
const stickThreshold = 8

interface LogsPanelProps {
  api: ContainersApi
  serverId: string
  container: string
  pollMs: number
  onClose: () => void
}

type LogsState =
  { status: 'loading' } | { status: 'ready'; text: string } | { status: 'error'; message: string }

/** ParsedLine is one log line, with the timestamp Docker prefixes when asked. */
interface ParsedLine {
  raw: string
  /** stamp is the RFC 3339 timestamp, or null when the line has none. */
  stamp: string | null
  /** seconds is the integral part of the stamp, or null. */
  seconds: number | null
  /** fraction is the nine digits after the decimal point, empty when there is none. */
  fraction: string
  /** message is the line without the timestamp. */
  message: string
}

// Docker writes the stamp as RFC 3339 with nanoseconds and a zone.
const linePattern = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2}) (.*)$/

function splitLines(text: string): string[] {
  if (text === '') {
    return []
  }
  const lines = text.split('\n')
  if (lines[lines.length - 1] === '') {
    lines.pop()
  }
  return lines
}

function parseLine(raw: string): ParsedLine {
  const match = linePattern.exec(raw)
  if (match === null) {
    return { raw, stamp: null, seconds: null, fraction: '', message: raw }
  }
  const base = match[1] as string
  const fraction = (match[2] ?? '').padEnd(9, '0').slice(0, 9)
  const zone = match[3] as string
  const milliseconds = Date.parse(base + zone)
  if (Number.isNaN(milliseconds)) {
    return { raw, stamp: null, seconds: null, fraction: '', message: raw }
  }
  return {
    raw,
    stamp: match[2] === undefined ? base + zone : `${base}.${match[2]}${zone}`,
    seconds: milliseconds / 1000,
    fraction,
    message: match[4] as string,
  }
}

function parseLines(text: string): ParsedLine[] {
  return splitLines(text).map(parseLine)
}

/** lastStamp returns the newest line that carries a timestamp. */
function lastStamp(lines: ParsedLine[]): ParsedLine | null {
  for (let index = lines.length - 1; index >= 0; index -= 1) {
    const line = lines[index]
    if (line !== undefined && line.seconds !== null) {
      return line
    }
  }
  return null
}

/** isNewer compares two stamps; Docker repeats the boundary line. */
function isNewer(line: ParsedLine, boundary: ParsedLine): boolean {
  if (line.seconds === null || boundary.seconds === null) {
    return false
  }
  if (line.seconds !== boundary.seconds) {
    return line.seconds > boundary.seconds
  }
  return line.fraction > boundary.fraction
}

/** formatFollow shows local HH:MM:SS in front of each line, as the design asks. */
function formatFollow(text: string, language: string): string {
  const clock = new Intl.DateTimeFormat(language, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  })
  return parseLines(text)
    .map((line) =>
      line.seconds === null
        ? line.raw
        : `${clock.format(new Date(line.seconds * 1000))} ${line.message}`,
    )
    .join('\n')
}

/**
 * LogsPanel shows the last lines of a container. Reading them is a job like any
 * other: the panel asks, follows it and shows the output. Follow asks again
 * every few seconds for the lines after the last one it has.
 */
export function LogsPanel({ api, serverId, container, pollMs, onClose }: LogsPanelProps) {
  const { t, i18n } = useTranslation(['server', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [state, setState] = useState<LogsState>({ status: 'loading' })
  const [follow, setFollow] = useState(false)
  const alive = useRef(true)
  // busy is shared by the initial read and the follow requests: one at a time.
  const busy = useRef(false)
  const text = useRef('')
  const viewer = useRef<HTMLPreElement>(null)
  const stickToBottom = useRef(true)

  const errorText = useCallback(
    (error: unknown) =>
      error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error),
    [t],
  )

  const fail = useCallback((message: string) => {
    setState({ status: 'error', message })
    setFollow(false)
  }, [])

  const fetchLogs = useCallback(async () => {
    if (busy.current) return
    busy.current = true
    try {
      const jobId = await api.logs(serverId, container)
      const job = await waitForJob(api, jobId, pollMs, () => alive.current)
      if (job === null || !alive.current) return
      if (job.state !== 'succeeded') {
        fail(job.message)
        return
      }
      text.current = job.output
      setState({ status: 'ready', text: job.output })
    } catch (error) {
      if (!alive.current || isSignedOut(error)) return
      fail(errorText(error))
    } finally {
      busy.current = false
    }
  }, [api, container, errorText, fail, pollMs, serverId])

  const fetchFollow = useCallback(async () => {
    if (busy.current || !alive.current) return
    busy.current = true
    try {
      const current = parseLines(text.current)
      const boundary = lastStamp(current)
      const jobId = await api.logs(serverId, container, followTail, {
        ...(boundary !== null && boundary.stamp !== null ? { since: boundary.stamp } : {}),
        timestamps: true,
      })
      const job = await waitForJob(api, jobId, pollMs, () => alive.current)
      if (job === null || !alive.current) return
      if (job.state !== 'succeeded') {
        fail(job.message)
        return
      }

      const fetched = parseLines(job.output)
      if (fetched.length === 0) return
      if (boundary === null) {
        // An older agent may send no stamps: there is nothing to compare, so
        // the answer is shown as it comes.
        text.current = job.output
        setState({ status: 'ready', text: job.output })
        return
      }

      const fresh = fetched.filter((line) => isNewer(line, boundary))
      if (fresh.length === 0) return
      let lines = [...current, ...fresh]
      if (lines.length > maxFollowLines) {
        lines = lines.slice(lines.length - maxFollowLines)
      }
      const next = lines.map((line) => line.raw).join('\n')
      text.current = next
      setState({ status: 'ready', text: next })
    } catch (error) {
      if (!alive.current || isSignedOut(error)) return
      fail(errorText(error))
    } finally {
      busy.current = false
    }
  }, [api, container, errorText, fail, pollMs, serverId])

  useEffect(() => {
    alive.current = true
    void fetchLogs()
    return () => {
      alive.current = false
    }
  }, [fetchLogs])

  useEffect(() => {
    if (!follow) {
      return
    }
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

  const refresh = () => {
    if (follow || busy.current) return
    setState({ status: 'loading' })
    void fetchLogs()
  }

  const toggleFollow = () => {
    stickToBottom.current = true
    setFollow((current) => !current)
  }

  const handleScroll = () => {
    const element = viewer.current
    if (!element) return
    stickToBottom.current =
      element.scrollHeight - element.scrollTop - element.clientHeight <= stickThreshold
  }

  // The newest lines are at the bottom. In follow mode the view only moves if
  // the reader was already there.
  useEffect(() => {
    const element = viewer.current
    if (!element || state.status !== 'ready') return
    if (!follow || stickToBottom.current) {
      element.scrollTop = element.scrollHeight
    }
  }, [state, follow])

  const title = t('server:logs.title', { name: container })
  const display =
    state.status !== 'ready' ? '' : follow ? formatFollow(state.text, language) : state.text

  return (
    <section className="logs" aria-label={title}>
      <header className="logs__head">
        <div className="logs__heading">
          <h4 className="logs__title">{title}</h4>
          {follow ? (
            <span className="logs__live" role="status">
              {t('server:logs.live')}
            </span>
          ) : null}
        </div>
        <div className="logs__actions">
          <button
            type="button"
            className={follow ? 'cbtn cbtn--live' : 'cbtn'}
            aria-pressed={follow}
            onClick={toggleFollow}
          >
            {t('server:logs.follow')}
          </button>
          <button
            type="button"
            className="cbtn"
            disabled={state.status === 'loading' || follow}
            onClick={refresh}
          >
            <RestartIcon />
            {t('server:logs.refresh')}
          </button>
          <button type="button" className="cbtn" onClick={onClose}>
            <CloseIcon />
            {t('server:logs.close')}
          </button>
        </div>
      </header>

      {state.status === 'loading' ? (
        <p className="logs__message" role="status">
          {t('server:logs.loading')}
        </p>
      ) : null}
      {state.status === 'error' ? (
        <p className="logs__message logs__message--error" role="alert">
          {t('server:progress.failed', { message: state.message })}
        </p>
      ) : null}
      {state.status === 'ready' && state.text.trim() === '' ? (
        <p className="logs__message">{t('server:logs.empty')}</p>
      ) : null}
      {state.status === 'ready' && state.text.trim() !== '' ? (
        <>
          <pre
            className="logs__text"
            ref={viewer}
            tabIndex={0}
            onScroll={handleScroll}
            aria-label={t('server:logs.content')}
          >
            {display}
          </pre>
          <p className="logs__note">{t(follow ? 'server:logs.noteLive' : 'server:logs.note')}</p>
        </>
      ) : null}
    </section>
  )
}
