import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  closeSessionRevoked,
  parseTerminalMessage,
  type SocketLike,
  type TerminalApi,
  type TerminalShell,
} from '../../api/terminal'
import { ApiError, isSignedOut } from '../../api/http'
import { errorMessage } from '../../auth/AuthGate'
import { CloseIcon, TerminalIcon } from '../../components/icons'

/** TerminalTarget says where the shell runs. */
export type TerminalTarget = { kind: 'container'; container: string } | { kind: 'host' }

interface TerminalPanelProps {
  api: TerminalApi
  serverId: string
  /** target is the preferred form; container is kept for older callers. */
  target?: TerminalTarget
  container?: string
  /** serverName labels a host shell and its red banner. */
  serverName?: string
  onClose: () => void
}

type Phase =
  | { kind: 'confirm' }
  | { kind: 'opening' }
  | { kind: 'live'; privileged: boolean }
  | { kind: 'ended'; text: string }
  | { kind: 'error'; message: string }

/** maxKeystrokes is the most bytes the core takes in one message. */
const maxKeystrokes = 4096

const encoder = new TextEncoder()

/** webSocketOpen is the readyState of an open WebSocket. */
const webSocketOpen = 1

/** isOpen tells whether the socket can still send; a fake without readyState counts as open. */
function isOpen(socket: SocketLike): boolean {
  return socket.readyState === undefined || socket.readyState === webSocketOpen
}

/** sendKeystrokes sends what was typed or pasted, in messages the core accepts. */
function sendKeystrokes(socket: SocketLike, text: string): void {
  const bytes = encoder.encode(text)
  for (let start = 0; start < bytes.length; start += maxKeystrokes) {
    socket.send(bytes.subarray(start, start + maxKeystrokes))
  }
}

/** terminalColors follows the look of the panel, light or dark. */
function terminalColors(): Record<string, string> {
  const style = getComputedStyle(document.documentElement)
  const read = (name: string, fallback: string) => style.getPropertyValue(name).trim() || fallback
  return {
    background: read('--code-bg', '#0f1c22'),
    foreground: read('--code-ink', '#d8e4e8'),
    cursor: read('--code-key', '#66d9e8'),
    cursorAccent: read('--code-bg', '#0f1c22'),
    selectionBackground: 'rgba(102, 217, 232, 0.28)',
  }
}

/**
 * TerminalPanel opens an interactive shell in a container or on the host. It asks
 * first, because the session is recorded and may be as good as root on the server;
 * then the core starts the shell and the panel attaches to it over a WebSocket with
 * a ticket.
 */
export function TerminalPanel({
  api,
  serverId,
  target,
  container: legacyContainer,
  serverName,
  onClose,
}: TerminalPanelProps) {
  const { t } = useTranslation(['server', 'errors'])
  const host = target?.kind === 'host'
  const container = target?.kind === 'container' ? target.container : (legacyContainer ?? '')
  const serverLabel = serverName ?? container
  const [phase, setPhase] = useState<Phase>({ kind: 'confirm' })
  const [shell, setShell] = useState<TerminalShell>('sh')
  // attempt changes to start a new terminal after one ended.
  const [attempt, setAttempt] = useState(0)
  // code lives only while the person types it; it is cleared before every request.
  const [code, setCode] = useState('')
  const [confirmError, setConfirmError] = useState<string | null>(null)
  const submittedCode = useRef('')
  const screen = useRef<HTMLDivElement>(null)
  // The terminal must not restart when the language or the shell choice changes
  // while it runs, so the effect reads them from refs.
  const latest = useRef({ t, shell })
  useEffect(() => {
    latest.current = { t, shell }
  })

  const title = host
    ? t('server:terminal.hostTitle', { name: serverLabel })
    : t('server:terminal.title', { name: container })
  const started = phase.kind !== 'confirm'

  useEffect(() => {
    if (!started || screen.current === null) return

    let alive = true
    let socket: SocketLike | null = null
    // finished is set once the core told why the session ended, so the close of
    // the socket that follows does not replace the reason.
    let finished = false
    // ready is true once the socket is open and the ticket has been sent.
    let ready = false
    const text = (reason: string, exitCode: number) => {
      const { t } = latest.current
      return t(`server:terminal.ended.${reason}`, {
        code: exitCode,
        reason,
        defaultValue: t('server:terminal.ended.other', { reason }),
      })
    }

    const term = new Terminal({
      cursorBlink: true,
      fontFamily: "'JetBrains Mono', ui-monospace, monospace",
      fontSize: 13,
      scrollback: 5000,
      theme: terminalColors(),
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(screen.current)
    fit.fit()

    // Ctrl+C copies when text is selected (otherwise it stays the interrupt) and
    // Ctrl+V / Ctrl+Shift+C / Ctrl+Shift+V work as in a browser. Returning false
    // keeps the key from reaching the shell; the native paste event still fires.
    term.attachCustomKeyEventHandler((event) => {
      if (event.type !== 'keydown' || !(event.ctrlKey || event.metaKey) || event.altKey) {
        return true
      }
      const key = event.key.toLowerCase()
      if (key === 'v') return false
      if (key === 'c' && (event.shiftKey || term.hasSelection())) {
        event.preventDefault()
        if (term.hasSelection()) {
          void navigator.clipboard?.writeText(term.getSelection()).catch(() => undefined)
        }
        return false
      }
      return true
    })

    // The last size sent: the core only needs to hear about a real change.
    let sentCols = 0
    let sentRows = 0
    const sendResize = () => {
      if (socket === null || !ready || finished || !isOpen(socket)) return
      if (term.cols === sentCols && term.rows === sentRows) return
      sentCols = term.cols
      sentRows = term.rows
      socket.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
    }
    // One fit per frame, and only when the box really changed size: a fit that
    // changes the box would otherwise call itself again.
    let frame = 0
    let boxWidth = 0
    let boxHeight = 0
    const observer = new ResizeObserver((entries) => {
      const box = entries[entries.length - 1]?.contentRect
      if (box !== undefined && box.width === boxWidth && box.height === boxHeight) return
      if (box !== undefined) {
        boxWidth = box.width
        boxHeight = box.height
      }
      if (frame !== 0) return
      frame = requestAnimationFrame(() => {
        frame = 0
        if (finished) return
        fit.fit()
        sendResize()
      })
    })
    observer.observe(screen.current)

    const input = term.onData((data) => {
      if (socket !== null && ready && !finished && isOpen(socket)) sendKeystrokes(socket, data)
    })

    const end = (text: string) => {
      finished = true
      ready = false
      observer.disconnect()
      if (alive) setPhase({ kind: 'ended', text })
    }

    const requestCode = host ? submittedCode.current : ''
    submittedCode.current = ''
    const openTerminal = host
      ? () => api.openHost(serverId, latest.current.shell, requestCode, term.cols, term.rows)
      : () => api.open(serverId, container, latest.current.shell, term.cols, term.rows)

    openTerminal()
      .then((session) => {
        if (!alive) return
        const opened = api.connect(session.sessionId)
        socket = opened
        opened.binaryType = 'arraybuffer'
        opened.onopen = () => {
          opened.send(JSON.stringify({ type: 'ticket', ticket: session.ticket }))
          ready = true
          sendResize()
          setPhase({ kind: 'live', privileged: session.privileged })
          term.focus()
        }
        opened.onmessage = (event: MessageEvent) => {
          if (typeof event.data === 'string') {
            const message = parseTerminalMessage(event.data)
            if (message !== null) end(text(message.reason, message.exitCode))
            return
          }
          term.write(new Uint8Array(event.data as ArrayBuffer))
        }
        opened.onclose = (event: CloseEvent) => {
          if (finished || !alive) return
          if (event.code === closeSessionRevoked) end(text('session_revoked', 0))
          else end(latest.current.t('server:terminal.connectionLost'))
        }
      })
      .catch((error: unknown) => {
        if (!alive || isSignedOut(error)) return
        finished = true
        const current = latest.current.t
        if (host && error instanceof ApiError && error.code === 'code_invalid') {
          setCode('')
          setConfirmError(errorMessage(current, error))
          setPhase({ kind: 'confirm' })
          return
        }
        let message = errorMessage(current, error)
        if (host && error instanceof ApiError) {
          if (error.code === 'terminal_open_failed') {
            message = current('errors:hostTerminalOpenFailed')
          } else if (error.code === 'terminal_exists') {
            message = current('errors:hostTerminalExists')
          }
        }
        setPhase({ kind: 'error', message })
      })

    return () => {
      alive = false
      if (frame !== 0) cancelAnimationFrame(frame)
      observer.disconnect()
      input.dispose()
      if (socket !== null) {
        socket.onclose = null
        socket.close()
      }
      term.dispose()
    }
    // attempt restarts the terminal.
  }, [api, attempt, container, host, serverId, started])

  const live = phase.kind === 'live'

  return (
    <section className="terminal-panel" aria-label={title}>
      <header className="terminal__head">
        <h4 className="terminal__title">{title}</h4>
        <div className="terminal__actions">
          {started && phase.kind !== 'error' ? (
            <span className="terminal__rec" title={t('server:terminal.recordingHint')}>
              <span className="terminal__rec-dot" aria-hidden="true" />
              {t('server:terminal.recording')}
            </span>
          ) : null}
          <button type="button" className="cbtn" onClick={onClose}>
            <CloseIcon />
            {t('server:terminal.close')}
          </button>
        </div>
      </header>

      {phase.kind === 'confirm' ? (
        <div
          className="terminal__confirm"
          role="group"
          aria-label={t(
            host ? 'server:terminal.hostConfirmTitle' : 'server:terminal.confirmTitle',
            { name: host ? serverLabel : container },
          )}
        >
          <p className="terminal__confirm-title">
            {t(host ? 'server:terminal.hostConfirmTitle' : 'server:terminal.confirmTitle', {
              name: host ? serverLabel : container,
            })}
          </p>
          <p className="terminal__confirm-body">
            {t(host ? 'server:terminal.hostConfirmBody' : 'server:terminal.confirmBody', {
              name: serverLabel,
            })}
          </p>
          {confirmError !== null ? (
            <p className="terminal__message terminal__message--error" role="alert">
              {confirmError}
            </p>
          ) : null}
          <label className="terminal__field">
            <span>{t('server:terminal.shell')}</span>
            <select
              value={shell}
              onChange={(event) => setShell(event.target.value as TerminalShell)}
            >
              <option value="sh">
                {t(host ? 'server:terminal.hostShellSh' : 'server:terminal.shellSh')}
              </option>
              <option value="bash">
                {t(host ? 'server:terminal.hostShellBash' : 'server:terminal.shellBash')}
              </option>
            </select>
          </label>
          {host ? (
            <div className="terminal__field">
              <label htmlFor="terminal-code">{t('server:terminal.code')}</label>
              <input
                id="terminal-code"
                className="terminal__code"
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                pattern="[0-9]*"
                maxLength={6}
                value={code}
                aria-describedby="terminal-code-hint"
                onChange={(event) => setCode(event.target.value.replace(/\D/g, '').slice(0, 6))}
              />
              <span className="terminal__hint" id="terminal-code-hint">
                {t('server:terminal.codeHint')}
              </span>
            </div>
          ) : null}
          <div className="container__actions">
            <button
              type="button"
              className="cbtn cbtn--primary"
              disabled={host && code.length !== 6}
              onClick={() => {
                if (host && code.length !== 6) return
                submittedCode.current = host ? code : ''
                setCode('')
                setConfirmError(null)
                setPhase({ kind: 'opening' })
              }}
            >
              <TerminalIcon />
              {t(host ? 'server:terminal.hostStart' : 'server:terminal.start')}
            </button>
            <button type="button" className="cbtn" onClick={onClose}>
              {t('server:terminal.cancel')}
            </button>
          </div>
        </div>
      ) : null}

      {phase.kind === 'opening' ? (
        <p className="terminal__message" role="status">
          {t('server:terminal.opening')}
        </p>
      ) : null}

      {live && host ? (
        <p className="terminal__root" role="alert">
          {t('server:terminal.hostBanner', { name: serverLabel })}
        </p>
      ) : null}

      {live && phase.privileged && !host ? (
        <p className="terminal__warning" role="alert">
          {t('server:terminal.privileged')}
        </p>
      ) : null}

      {phase.kind === 'error' ? (
        <p className="terminal__message terminal__message--error" role="alert">
          {t('server:progress.failed', { message: phase.message })}
        </p>
      ) : null}

      {phase.kind === 'ended' ? (
        <div className="terminal__ended" role="status">
          <p className="terminal__message">{phase.text}</p>
          <button
            type="button"
            className="cbtn"
            onClick={() => {
              setConfirmError(null)
              setCode('')
              if (host) {
                setPhase({ kind: 'confirm' })
                return
              }
              setPhase({ kind: 'opening' })
              setAttempt((current) => current + 1)
            }}
          >
            <TerminalIcon />
            {t(host ? 'server:terminal.hostAgain' : 'server:terminal.again')}
          </button>
        </div>
      ) : null}

      {started ? (
        <div
          className={`terminal__screen${live ? '' : ' terminal__screen--idle'}`}
          ref={screen}
          hidden={phase.kind === 'error'}
        />
      ) : null}
      {started && phase.kind === 'error' ? (
        <div className="container__actions">
          <button
            type="button"
            className="cbtn"
            onClick={() => {
              setConfirmError(null)
              setCode('')
              setPhase({ kind: 'confirm' })
            }}
          >
            {t(host ? 'server:terminal.hostAgain' : 'server:terminal.again')}
          </button>
        </div>
      ) : null}
    </section>
  )
}
