import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../api/http'
import type { SocketLike, TerminalApi, TerminalSession } from '../../api/terminal'
import i18n from '../../i18n'
import { TerminalPanel } from './TerminalPanel'

// xterm needs a real layout engine; the panel only uses its small surface.
const { terminals, FakeTerminal } = vi.hoisted(() => {
  const created: FakeTerminal[] = []

  class FakeTerminal {
    cols = 90
    rows = 30
    written: Uint8Array[] = []
    disposed = false
    private dataListener: ((data: string) => void) | null = null

    constructor() {
      created.push(this)
    }
    loadAddon() {}
    open() {}
    focus() {}
    write(data: Uint8Array) {
      this.written.push(data)
    }
    onData(listener: (data: string) => void) {
      this.dataListener = listener
      return { dispose: () => (this.dataListener = null) }
    }
    type(data: string) {
      this.dataListener?.(data)
    }
    dispose() {
      this.disposed = true
    }
  }

  return { terminals: created, FakeTerminal }
})

vi.mock('@xterm/xterm', () => ({ Terminal: FakeTerminal }))
vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit() {}
  },
}))
vi.mock('@xterm/xterm/css/xterm.css', () => ({}))

class FakeSocket implements SocketLike {
  binaryType: BinaryType = 'blob'
  onopen: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onclose: ((event: CloseEvent) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  sent: (string | Blob | BufferSource)[] = []
  closed = false
  readyState = 1
  send(data: string | Blob | BufferSource) {
    this.sent.push(data)
  }
  close() {
    this.closed = true
  }
  open() {
    this.onopen?.(new Event('open'))
  }
  receiveText(text: string) {
    this.onmessage?.(new MessageEvent('message', { data: text }))
  }
  receiveBytes(bytes: number[]) {
    this.onmessage?.(new MessageEvent('message', { data: new Uint8Array(bytes).buffer }))
  }
  closeWith(code: number) {
    this.onclose?.(new CloseEvent('close', { code }))
  }
}

const session: TerminalSession = { sessionId: 'sess-1', ticket: 'ticket-1', privileged: false }

function fakeApi(
  opened: Promise<TerminalSession> = Promise.resolve(session),
  hostOpened: Promise<TerminalSession> = opened,
) {
  const socket = new FakeSocket()
  const openHost = vi.fn(() => hostOpened)
  const api = {
    open: vi.fn(() => opened),
    openHost,
    connect: vi.fn(() => socket),
  } satisfies TerminalApi
  return { api, socket, openHost }
}

beforeEach(async () => {
  terminals.length = 0
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
  await i18n.changeLanguage('en')
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderPanel(api: TerminalApi, onClose = vi.fn()) {
  render(
    <TerminalPanel
      api={api}
      serverId="srv-1"
      target={{ kind: 'container', container: 'web' }}
      onClose={onClose}
    />,
  )
  return onClose
}

function renderHostPanel(api: TerminalApi, onClose = vi.fn()) {
  render(
    <TerminalPanel
      api={api}
      serverId="srv-1"
      serverName="vps-web-01"
      target={{ kind: 'host' }}
      onClose={onClose}
    />,
  )
  return onClose
}

describe('TerminalPanel', () => {
  it('asks first and opens nothing until the person agrees', () => {
    const { api } = fakeApi()
    renderPanel(api)

    expect(screen.getByText(/The session is recorded/)).toBeTruthy()
    expect(api.open).not.toHaveBeenCalled()
    expect(terminals).toHaveLength(0)
  })

  it('opens the chosen shell, sends the ticket first and carries both directions', async () => {
    const { api, socket } = fakeApi()
    renderPanel(api)

    fireEvent.change(screen.getByLabelText('Shell'), { target: { value: 'bash' } })
    fireEvent.click(screen.getByRole('button', { name: 'Open the terminal' }))

    await waitFor(() => expect(api.connect).toHaveBeenCalledWith('sess-1'))
    expect(api.open).toHaveBeenCalledWith('srv-1', 'web', 'bash', 90, 30)
    expect(socket.binaryType).toBe('arraybuffer')

    // Nothing but the ticket leaves before the socket is open.
    terminals[0]?.type('early')
    expect(socket.sent).toHaveLength(0)

    act(() => socket.open())
    expect(JSON.parse(socket.sent[0] as string)).toEqual({ type: 'ticket', ticket: 'ticket-1' })

    terminals[0]?.type('ls\n')
    expect(new TextDecoder().decode(socket.sent.at(-1) as Uint8Array)).toBe('ls\n')

    act(() => socket.receiveBytes([104, 105]))
    expect(Array.from(terminals[0]?.written[0] ?? [])).toEqual([104, 105])
    expect(screen.getByText('Recording')).toBeTruthy()
  })

  it('tells the core only about real size changes and nothing once the socket is closed', async () => {
    let observed: ((entries: unknown[]) => void) | null = null
    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(callback: (entries: unknown[]) => void) {
          observed = callback
        }
        observe() {}
        disconnect() {}
      },
    )
    vi.stubGlobal('requestAnimationFrame', (callback: () => void) => {
      queueMicrotask(callback)
      return 1
    })
    vi.stubGlobal('cancelAnimationFrame', () => {})

    const { api, socket } = fakeApi()
    renderPanel(api)
    fireEvent.click(screen.getByRole('button', { name: 'Open the terminal' }))
    await waitFor(() => expect(api.connect).toHaveBeenCalled())
    act(() => socket.open())
    const resizes = () =>
      socket.sent.filter((message) => typeof message === 'string' && message.includes('"resize"'))
    const afterOpen = resizes().length

    // The same box and the same rows: nothing to say.
    const box = (width: number, height: number) => [{ contentRect: { width, height } }]
    const fire = (width: number, height: number) =>
      act(async () => {
        observed?.(box(width, height))
        await Promise.resolve()
      })
    await fire(800, 400)
    await fire(800, 400)
    expect(resizes().length).toBeLessThanOrEqual(afterOpen)

    // A different number of rows is sent once.
    const term = terminals[0]
    if (term === undefined) throw new Error('no terminal')
    term.rows = 40
    await fire(800, 500)
    expect(resizes().length).toBe(afterOpen + 1)

    // Once the socket is closed, neither resizes nor keys go out any more.
    const sentBefore = socket.sent.length
    socket.readyState = 3
    term.rows = 50
    await fire(800, 600)
    term.type('x')
    expect(socket.sent.length).toBe(sentBefore)
  })

  it('splits a long paste into messages the core accepts', async () => {
    const { api, socket } = fakeApi()
    renderPanel(api)
    fireEvent.click(screen.getByRole('button', { name: 'Open the terminal' }))
    await waitFor(() => expect(api.connect).toHaveBeenCalled())
    act(() => socket.open())

    socket.sent.length = 0
    terminals[0]?.type('é'.repeat(5000)) // 10000 bytes
    const sizes = socket.sent.map((message) => (message as Uint8Array).length)
    expect(sizes).toEqual([4096, 4096, 1808])
  })

  it('warns when the container is as good as root on the server', async () => {
    const { api, socket } = fakeApi(Promise.resolve({ ...session, privileged: true }))
    renderPanel(api)
    fireEvent.click(screen.getByRole('button', { name: 'Open the terminal' }))
    await waitFor(() => expect(api.connect).toHaveBeenCalled())
    act(() => socket.open())

    expect(await screen.findByText(/as good as root on the server/)).toBeTruthy()
  })

  it('tells why the session ended and can open a new one', async () => {
    const { api, socket } = fakeApi()
    renderPanel(api)
    fireEvent.click(screen.getByRole('button', { name: 'Open the terminal' }))
    await waitFor(() => expect(api.connect).toHaveBeenCalled())
    act(() => socket.open())

    act(() =>
      socket.receiveText(JSON.stringify({ type: 'closed', reason: 'exited', exit_code: 3 })),
    )
    expect(await screen.findByText('The shell ended (exit code 3).')).toBeTruthy()
    // The close of the socket that follows does not replace the reason.
    act(() => socket.closeWith(1000))
    expect(screen.getByText('The shell ended (exit code 3).')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Open a new terminal' }))
    await waitFor(() => expect(api.open).toHaveBeenCalledTimes(2))
    expect(terminals[0]?.disposed).toBe(true)
  })

  it('explains an unknown reason and a lost connection', async () => {
    const { api, socket } = fakeApi()
    renderPanel(api)
    fireEvent.click(screen.getByRole('button', { name: 'Open the terminal' }))
    await waitFor(() => expect(api.connect).toHaveBeenCalled())
    act(() => socket.open())

    act(() => socket.closeWith(1006))
    expect(await screen.findByText('The connection with the terminal was lost.')).toBeTruthy()
  })

  it('shows the answer of the core when the terminal cannot be opened', async () => {
    const refused = Promise.reject(new ApiError('too_many_terminals', 409))
    refused.catch(() => undefined)
    const { api } = fakeApi(refused)
    renderPanel(api)
    fireEvent.click(screen.getByRole('button', { name: 'Open the terminal' }))

    expect(
      await screen.findByText(/This server already has the most terminals it allows/),
    ).toBeTruthy()
    expect(api.connect).not.toHaveBeenCalled()
  })

  it('asks for a root host shell with a six-digit code', async () => {
    const { api, socket, openHost } = fakeApi()
    renderHostPanel(api)

    expect(
      screen.getByText(/This opens a shell as root on vps-web-01\. It is recorded/),
    ).toBeTruthy()
    const input = screen.getByLabelText('Authenticator code') as HTMLInputElement
    const start = screen.getByRole('button', { name: 'Open the root shell' }) as HTMLButtonElement
    expect(start.disabled).toBe(true)

    fireEvent.change(input, { target: { value: '12345' } })
    expect(start.disabled).toBe(true)
    fireEvent.change(input, { target: { value: '123456' } })
    expect(start.disabled).toBe(false)
    fireEvent.click(start)

    await waitFor(() => expect(openHost).toHaveBeenCalledWith('srv-1', 'sh', '123456', 90, 30))
    expect(api.open).not.toHaveBeenCalled()
    await waitFor(() => expect(api.connect).toHaveBeenCalledWith('sess-1'))
    act(() => socket.open())

    expect(await screen.findByText('Root on vps-web-01')).toBeTruthy()
    expect(screen.getByText('Recording')).toBeTruthy()
  })

  it('clears the code and keeps the dialog when it is invalid', async () => {
    const refused = Promise.reject(new ApiError('code_invalid', 403))
    refused.catch(() => undefined)
    const { api } = fakeApi(Promise.resolve(session), refused)
    renderHostPanel(api)

    const input = screen.getByLabelText('Authenticator code') as HTMLInputElement
    fireEvent.change(input, { target: { value: '123456' } })
    fireEvent.click(screen.getByRole('button', { name: 'Open the root shell' }))

    expect(await screen.findByText('The code is not valid or has already been used.')).toBeTruthy()
    expect(screen.getByText('Open a root shell on vps-web-01?')).toBeTruthy()
    expect((screen.getByLabelText('Authenticator code') as HTMLInputElement).value).toBe('')
    expect(api.openHost).toHaveBeenCalledTimes(1)
  })

  it('maps a disabled host terminal to a clear message', async () => {
    const refused = Promise.reject(new ApiError('host_terminal_disabled', 409))
    refused.catch(() => undefined)
    const { api } = fakeApi(Promise.resolve(session), refused)
    renderHostPanel(api)

    fireEvent.change(screen.getByLabelText('Authenticator code'), {
      target: { value: '123456' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Open the root shell' }))

    expect(await screen.findByText(/This agent does not allow root terminals/)).toBeTruthy()
  })

  it('cancelling closes the host panel without opening anything', () => {
    const { api } = fakeApi()
    const onClose = renderHostPanel(api)
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onClose).toHaveBeenCalled()
    expect(api.openHost).not.toHaveBeenCalled()
  })

  it('closes the socket and the terminal when the panel goes away', async () => {
    const { api, socket } = fakeApi()
    const { unmount } = render(
      <TerminalPanel
        api={api}
        serverId="srv-1"
        target={{ kind: 'container', container: 'web' }}
        onClose={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Open the terminal' }))
    await waitFor(() => expect(api.connect).toHaveBeenCalled())
    act(() => socket.open())

    unmount()
    expect(socket.closed).toBe(true)
    expect(terminals[0]?.disposed).toBe(true)
  })

  it('cancelling closes the panel without opening anything', () => {
    const { api } = fakeApi()
    const onClose = renderPanel(api)
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onClose).toHaveBeenCalled()
    expect(api.open).not.toHaveBeenCalled()
  })
})
