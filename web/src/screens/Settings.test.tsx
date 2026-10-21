import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AccessApi } from '../api/auth'
import { ApiError } from '../api/http'
import { AgentUpdateError, type ServerSummary, type ServersApi } from '../api/servers'
import type { SystemVersion, UpdateInfo, UpdateStatus } from '../api/system'
import i18n from '../i18n'
import { Settings, pollEvery, type SettingsApi } from './Settings'

const current = 'a'.repeat(40)
const latest = 'b'.repeat(40)

const baseUpdate: UpdateInfo = {
  current,
  latest,
  pending: [{ sha: 'bbbbbbb', subject: 'Fix the panel', author: 'Ana' }],
  notes: { en: [], es: [] },
  ci: 'green',
  checkedAt: '2026-10-21T08:00:00Z',
  error: '',
  updateAvailable: true,
}

const baseStatus: UpdateStatus = {
  state: 'idle',
  from: '',
  to: '',
  startedAt: '',
  finishedAt: '',
  log: [],
}

interface SystemOverrides {
  version?: string
  commit?: string
  update?: Partial<UpdateInfo>
  status?: Partial<UpdateStatus>
}

/** makeSystem builds the answer of the API with sensible defaults. */
function makeSystem(overrides: SystemOverrides = {}): SystemVersion {
  return {
    version: overrides.version ?? '1.2.3',
    commit: overrides.commit ?? current,
    update: { ...baseUpdate, ...overrides.update },
    status: { ...baseStatus, ...overrides.status },
  }
}

function apiWith(
  version: () => Promise<SystemVersion>,
  update?: SettingsApi['update'],
  check?: SettingsApi['check'],
  servers?: Partial<
    Pick<ServersApi, 'listServers' | 'removeServer' | 'updateAgent' | 'updateAllAgents'>
  >,
): SettingsApi {
  return {
    version,
    check: check ?? vi.fn(() => Promise.reject(new Error('unexpected check'))),
    update: update ?? vi.fn(() => Promise.reject(new Error('unexpected update'))),
    ...servers,
  }
}

function renderSettings(api: SettingsApi, access?: AccessApi) {
  render(<Settings api={api} accessApi={access} />)
}

/** fakeAccess answers the access lists; the unused calls reject. */
function fakeAccess(): AccessApi {
  const unexpected = () => Promise.reject(new Error('unexpected access call'))
  return {
    listDevices: () =>
      Promise.resolve([
        {
          id: 'dev-1',
          name: 'laptop',
          createdAt: '2026-10-20T10:00:00Z',
          lastUsedAt: null,
          current: true,
        },
      ]),
    createDevice: unexpected,
    removeDevice: unexpected,
    listPasskeys: () => Promise.resolve({ available: false, passkeys: [] }),
    removePasskey: unexpected,
    beginPasskeyRegistration: unexpected,
    finishPasskeyRegistration: unexpected,
    beginPasskeyLogin: unexpected,
    finishPasskeyLogin: unexpected,
    pinLogin: unexpected,
  }
}

describe('Settings', () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    vi.useRealTimers()
    cleanup()
  })

  it('shows the version, the commit, the CI badge and the pending commits', async () => {
    renderSettings(apiWith(() => Promise.resolve(makeSystem())))

    expect(await screen.findByText('1.2.3')).toBeTruthy()
    expect(screen.getByText('aaaaaaa')).toBeTruthy()
    expect(screen.getByText('CI green')).toBeTruthy()
    expect(screen.getByText('Latest green commit bbbbbbb')).toBeTruthy()
    expect(screen.getByText('1 commit pending')).toBeTruthy()
    expect(screen.getByText('Fix the panel')).toBeTruthy()
    expect(screen.getByText('Ana')).toBeTruthy()
    expect(screen.getByText('Pending commits')).toBeTruthy()

    const button = screen.getByRole('button', { name: 'Update to bbbbbbb' }) as HTMLButtonElement
    expect(button.disabled).toBe(false)
  })

  it('shows the access settings with the trusted devices and passkeys', async () => {
    renderSettings(
      apiWith(() => Promise.resolve(makeSystem())),
      fakeAccess(),
    )

    expect(await screen.findByText('Access')).toBeTruthy()
    expect(await screen.findByText('laptop')).toBeTruthy()
    expect(await screen.findByText('Passkeys need HTTPS with a domain name.')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Trust this device' })).toBeTruthy()
  })

  it('lists the servers with their connection state', async () => {
    const servers: ServerSummary[] = [
      {
        id: 'srv-1',
        name: 'vps-web-01',
        connected: true,
        revoked: false,
        agentVersion: '0.2.0',
        agentLatestVersion: '0.2.8',
        agentUpdateAvailable: false,
        agentUpdateManual: false,
        agentUpgradeCommand: '',
      },
      {
        id: 'srv-2',
        name: 'vps-db-01',
        connected: false,
        revoked: false,
        agentVersion: '',
        agentLatestVersion: '0.2.8',
        agentUpdateAvailable: false,
        agentUpdateManual: false,
        agentUpgradeCommand: '',
      },
    ]
    renderSettings(
      apiWith(() => Promise.resolve(makeSystem()), undefined, undefined, {
        listServers: () => Promise.resolve(servers),
      }),
    )

    expect(await screen.findByText('Servers')).toBeTruthy()
    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    expect(screen.getByText('vps-db-01')).toBeTruthy()
    expect(screen.getByText('Connected')).toBeTruthy()
    expect(screen.getByText('Offline')).toBeTruthy()
  })

  it('shows the agent version and updates one server after confirmation', async () => {
    const servers: ServerSummary[] = [
      {
        id: 'srv-1',
        name: 'vps-web-01',
        connected: true,
        revoked: false,
        agentVersion: '0.2.0',
        agentLatestVersion: '0.2.8',
        agentUpdateAvailable: true,
        agentUpdateManual: false,
        agentUpgradeCommand: '',
      },
    ]
    const listServers = vi.fn<() => Promise<ServerSummary[]>>().mockResolvedValue(servers)
    const updateAgent = vi.fn<(id: string) => Promise<void>>(() => Promise.resolve())
    renderSettings(
      apiWith(() => Promise.resolve(makeSystem()), undefined, undefined, {
        listServers,
        updateAgent,
      }),
    )

    expect(await screen.findByText('Agent 0.2.0')).toBeTruthy()
    fireEvent.click(await screen.findByRole('button', { name: 'Update' }))

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Update the agent of vps-web-01?')).toBeTruthy()
    expect(within(dialog).getByText(/verifies its SHA-256/)).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update agent' }))

    expect(updateAgent).toHaveBeenCalledWith('srv-1')
    expect(await screen.findByText('Updating… the agent will reconnect.')).toBeTruthy()
    await waitFor(() => expect(listServers).toHaveBeenCalledTimes(2))
  })

  it('updates all agents and shows the queued and skipped counts', async () => {
    const servers: ServerSummary[] = [
      {
        id: 'srv-1',
        name: 'vps-web-01',
        connected: true,
        revoked: false,
        agentVersion: '0.2.0',
        agentLatestVersion: '0.2.8',
        agentUpdateAvailable: true,
        agentUpdateManual: false,
        agentUpgradeCommand: '',
      },
      {
        id: 'srv-2',
        name: 'vps-db-01',
        connected: true,
        revoked: false,
        agentVersion: '0.2.0',
        agentLatestVersion: '0.2.8',
        agentUpdateAvailable: false,
        agentUpdateManual: false,
        agentUpgradeCommand: '',
      },
    ]
    const updateAllAgents = vi.fn(() =>
      Promise.resolve({
        queued: ['srv-1'],
        skipped: [{ id: 'srv-2', reason: 'no_update' }],
      }),
    )
    renderSettings(
      apiWith(() => Promise.resolve(makeSystem()), undefined, undefined, {
        listServers: () => Promise.resolve(servers),
        updateAllAgents,
      }),
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Update all agents (1)' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Update all agents?')).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update all agents' }))

    expect(updateAllAgents).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('1 update queued, 1 skipped.')).toBeTruthy()
  })

  it('shows why a server agent update was refused', async () => {
    const servers: ServerSummary[] = [
      {
        id: 'srv-1',
        name: 'vps-web-01',
        connected: true,
        revoked: false,
        agentVersion: '0.2.0',
        agentLatestVersion: '0.2.8',
        agentUpdateAvailable: true,
        agentUpdateManual: false,
        agentUpgradeCommand: '',
      },
    ]
    const updateAgent = vi.fn(() => Promise.reject(new AgentUpdateError('update_in_progress', 409)))
    renderSettings(
      apiWith(() => Promise.resolve(makeSystem()), undefined, undefined, {
        listServers: () => Promise.resolve(servers),
        updateAgent,
      }),
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Update' }))
    fireEvent.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'Update agent' }),
    )

    expect((await screen.findByRole('alert')).textContent).toBe(
      'An agent update is already in progress.',
    )
  })

  it('shows the manual upgrade command for an agent that is too old', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const servers: ServerSummary[] = [
      {
        id: 'srv-1',
        name: 'vps-old-01',
        connected: true,
        revoked: false,
        agentVersion: '0.2.7',
        agentLatestVersion: '0.2.8',
        agentUpdateAvailable: false,
        agentUpdateManual: true,
        agentUpgradeCommand: 'sudo bash install.sh --upgrade',
      },
    ]
    renderSettings(
      apiWith(() => Promise.resolve(makeSystem()), undefined, undefined, {
        listServers: () => Promise.resolve(servers),
      }),
    )

    expect(await screen.findByText(/This agent is too old to update itself/)).toBeTruthy()
    expect(screen.getByText('sudo bash install.sh --upgrade')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Update' })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Copy the command' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('sudo bash install.sh --upgrade'))
    expect(await screen.findByText('Copied')).toBeTruthy()
  })

  it('points to the docs when the manual upgrade command is empty', async () => {
    const servers: ServerSummary[] = [
      {
        id: 'srv-1',
        name: 'vps-old-01',
        connected: true,
        revoked: false,
        agentVersion: '0.2.7',
        agentLatestVersion: '0.2.8',
        agentUpdateAvailable: false,
        agentUpdateManual: true,
        agentUpgradeCommand: '',
      },
    ]
    renderSettings(
      apiWith(() => Promise.resolve(makeSystem()), undefined, undefined, {
        listServers: () => Promise.resolve(servers),
      }),
    )

    expect(await screen.findByText(/This agent is too old to update itself/)).toBeTruthy()
    expect(screen.getByText('See the installation guide for the manual steps.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Copy the command' })).toBeNull()
  })

  it('requires the exact server name and refreshes the list after removal', async () => {
    const servers: ServerSummary[] = [
      {
        id: 'srv-1',
        name: 'vps-web-01',
        connected: true,
        revoked: false,
        agentVersion: '0.2.0',
        agentLatestVersion: '0.2.8',
        agentUpdateAvailable: false,
        agentUpdateManual: false,
        agentUpgradeCommand: '',
      },
    ]
    const listServers = vi
      .fn<() => Promise<ServerSummary[]>>()
      .mockResolvedValueOnce(servers)
      .mockResolvedValueOnce([])
    const removeServer = vi.fn<(id: string) => Promise<void>>(() => Promise.resolve())
    renderSettings(
      apiWith(() => Promise.resolve(makeSystem()), undefined, undefined, {
        listServers,
        removeServer,
      }),
    )

    fireEvent.click((await screen.findAllByRole('button', { name: 'Remove' }))[0])
    expect(screen.getByRole('dialog')).toBeTruthy()
    expect(screen.getByText('Remove vps-web-01?')).toBeTruthy()
    expect(screen.getByText(/Containers on the server are not touched/)).toBeTruthy()
    expect(
      screen.getByText(/run the install command again to replace the old identity/),
    ).toBeTruthy()

    const confirm = screen.getByRole('button', { name: 'Remove server' }) as HTMLButtonElement
    expect(confirm.disabled).toBe(true)

    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'vps-web-0' } })
    expect(confirm.disabled).toBe(true)
    expect(removeServer).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'vps-web-01' } })
    expect(confirm.disabled).toBe(false)
    fireEvent.click(confirm)

    await waitFor(() => expect(removeServer).toHaveBeenCalledWith('srv-1'))
    await waitFor(() => expect(listServers).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(screen.queryByText('vps-web-01')).toBeNull()
  })

  it('cancels a server removal without calling the API', async () => {
    const servers: ServerSummary[] = [
      {
        id: 'srv-1',
        name: 'vps-web-01',
        connected: true,
        revoked: false,
        agentVersion: '0.2.0',
        agentLatestVersion: '0.2.8',
        agentUpdateAvailable: false,
        agentUpdateManual: false,
        agentUpgradeCommand: '',
      },
    ]
    const removeServer = vi.fn<(id: string) => Promise<void>>(() => Promise.resolve())
    renderSettings(
      apiWith(() => Promise.resolve(makeSystem()), undefined, undefined, {
        listServers: () => Promise.resolve(servers),
        removeServer,
      }),
    )

    fireEvent.click((await screen.findAllByRole('button', { name: 'Remove' }))[0])
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).toBeNull()
    expect(removeServer).not.toHaveBeenCalled()
  })

  const noteLanguages = [
    ['en', "What's new", 'English note'],
    ['es', 'Novedades', 'Nota en español'],
    ['ca', 'Novetats', 'Nota en español'],
  ] as const

  for (const [language, heading, note] of noteLanguages) {
    it(`shows the release notes in ${language}`, async () => {
      await i18n.changeLanguage(language)
      renderSettings(
        apiWith(() =>
          Promise.resolve(
            makeSystem({ update: { notes: { en: ['English note'], es: ['Nota en español'] } } }),
          ),
        ),
      )

      expect(await screen.findByText(heading)).toBeTruthy()
      expect(screen.getByText(note)).toBeTruthy()
      expect(screen.getByText('Commits')).toBeTruthy()
    })
  }

  it('renders code spans in notes without HTML', async () => {
    const { container } = render(
      <Settings
        api={apiWith(() =>
          Promise.resolve(
            makeSystem({
              update: {
                notes: { en: ['Use `talaia-core` and <b>not HTML</b>'], es: [] },
              },
            }),
          ),
        )}
      />,
    )

    expect(await screen.findByText('talaia-core')).toBeTruthy()
    expect(container.querySelector('code')?.textContent).toBe('talaia-core')
    expect(container.querySelector('b')).toBeNull()
    expect(screen.getByText(/not HTML/)).toBeTruthy()
  })

  it('collapses notes after eight and expands them with Show all', async () => {
    const notes = Array.from({ length: 10 }, (_, index) => `Note ${index + 1}`)
    renderSettings(
      apiWith(() => Promise.resolve(makeSystem({ update: { notes: { en: notes, es: [] } } }))),
    )

    expect(await screen.findByText('Note 8')).toBeTruthy()
    expect(screen.queryByText('Note 9')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Show all' }))
    expect(await screen.findByText('Note 9')).toBeTruthy()
    expect(screen.getByText('Note 10')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Show less' }))
    expect(screen.queryByText('Note 9')).toBeNull()
  })

  it('keeps the commit-only card when there are no notes', async () => {
    renderSettings(apiWith(() => Promise.resolve(makeSystem())))

    expect(await screen.findByText('Fix the panel')).toBeTruthy()
    expect(screen.queryByText("What's new")).toBeNull()
    expect(screen.getByText('Commits')).toBeTruthy()
  })

  it('says the version is unknown when the build has no commit', async () => {
    renderSettings(apiWith(() => Promise.resolve(makeSystem({ commit: '' }))))

    expect(await screen.findByText('unknown version')).toBeTruthy()
  })

  it('lists at most ten pending commits and summarises the rest', async () => {
    const pending = Array.from({ length: 12 }, (_, index) => ({
      sha: index.toString(16).padStart(40, '0'),
      subject: `Commit ${index}`,
      author: 'Ana',
    }))
    renderSettings(apiWith(() => Promise.resolve(makeSystem({ update: { pending } }))))

    expect(await screen.findByText('Commit 9')).toBeTruthy()
    expect(screen.queryByText('Commit 10')).toBeNull()
    expect(screen.getByText('and 2 more')).toBeTruthy()
  })

  it('disables the button when there is no update available', async () => {
    renderSettings(
      apiWith(() =>
        Promise.resolve(
          makeSystem({ update: { latest: current, updateAvailable: false, pending: [] } }),
        ),
      ),
    )

    const button = (await screen.findByRole('button', {
      name: 'Update to aaaaaaa',
    })) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    expect(screen.getByText('Up to date')).toBeTruthy()
  })

  it('explains that there is no green build yet', async () => {
    renderSettings(
      apiWith(() =>
        Promise.resolve(
          makeSystem({ update: { latest: '', updateAvailable: false, pending: [] } }),
        ),
      ),
    )

    expect(await screen.findByText('No green build yet')).toBeTruthy()
    const button = screen.getByRole('button', { name: 'No update available' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
  })

  it('disables the button when the host helper is missing', async () => {
    renderSettings(
      apiWith(() => Promise.resolve(makeSystem({ update: { error: 'host_helper_missing' } }))),
    )

    expect(
      await screen.findByText(/The host update helper is not installed or is not running/),
    ).toBeTruthy()
    const button = screen.getByRole('button', { name: 'Update to bbbbbbb' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
  })

  const friendlyErrors = [
    ['rate_limited', 'GitHub has rate-limited the update checks. The panel will try again later.'],
    ['unreachable', 'The update server could not be reached. The panel will try again later.'],
    ['invalid_response', 'The update check failed. The panel will try again later.'],
  ] as const

  for (const [code, message] of friendlyErrors) {
    it(`shows a friendly message for the "${code}" check error`, async () => {
      renderSettings(apiWith(() => Promise.resolve(makeSystem({ update: { error: code } }))))

      expect(await screen.findByText(message)).toBeTruthy()
    })
  }

  it('checks now and replaces the version card with the new answer', async () => {
    const refreshed = makeSystem({
      update: {
        latest: 'd'.repeat(40),
        pending: [{ sha: 'ddddddd', subject: 'New commit', author: 'Bea' }],
        checkedAt: '2026-10-21T09:00:00Z',
        updateAvailable: true,
      },
    })
    const check = vi.fn(() => Promise.resolve(refreshed))
    renderSettings(apiWith(() => Promise.resolve(makeSystem()), undefined, check))

    fireEvent.click(await screen.findByRole('button', { name: 'Check now' }))

    expect(check).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('New commit')).toBeTruthy()
    expect(screen.getByText('Latest green commit ddddddd')).toBeTruthy()
    const checked = new Intl.DateTimeFormat('en', {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date('2026-10-21T09:00:00Z'))
    expect(screen.getByText(checked)).toBeTruthy()
  })

  it('disables Check now while the check runs', async () => {
    let resolveCheck: (system: SystemVersion) => void = () => {}
    const check = vi.fn(
      () =>
        new Promise<SystemVersion>((resolve) => {
          resolveCheck = resolve
        }),
    )
    renderSettings(apiWith(() => Promise.resolve(makeSystem()), undefined, check))

    const button = (await screen.findByRole('button', { name: 'Check now' })) as HTMLButtonElement
    fireEvent.click(button)
    expect(button.disabled).toBe(true)
    expect(screen.getByRole('button', { name: 'Checking.' })).toBeTruthy()

    await act(async () => {
      resolveCheck(makeSystem())
    })
    expect(await screen.findByRole('button', { name: 'Check now' })).toBeTruthy()
  })

  it('shows the short notice when the check is rate limited', async () => {
    const check = vi.fn(() => Promise.reject(new ApiError('rate_limited', 429)))
    renderSettings(apiWith(() => Promise.resolve(makeSystem()), undefined, check))

    fireEvent.click(await screen.findByRole('button', { name: 'Check now' }))

    expect(await screen.findByText('Checked a moment ago. Try again in a minute.')).toBeTruthy()
    expect(
      screen.queryByText(
        'GitHub has rate-limited the update checks. The panel will try again later.',
      ),
    ).toBeNull()
  })

  it('shows the usual error for another failed check', async () => {
    const check = vi.fn(() => Promise.reject(new ApiError('internal', 500)))
    renderSettings(apiWith(() => Promise.resolve(makeSystem()), undefined, check))

    fireEvent.click(await screen.findByRole('button', { name: 'Check now' }))

    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(screen.getByText('Something went wrong. Try again.')).toBeTruthy()
  })

  it('disables Check now while an update is running', async () => {
    renderSettings(
      apiWith(() =>
        Promise.resolve(makeSystem({ status: { state: 'running', log: ['building'] } })),
      ),
    )

    expect(await screen.findByText('The update is running.')).toBeTruthy()
    const button = screen.getByRole('button', { name: 'Check now' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
  })

  it('disables Check now while an update is queued', async () => {
    const update = vi.fn(() => new Promise<{ target: string }>(() => {}))
    renderSettings(apiWith(() => Promise.resolve(makeSystem()), update))

    fireEvent.click(await screen.findByRole('button', { name: 'Update to bbbbbbb' }))
    fireEvent.click(screen.getByRole('button', { name: 'Update now' }))
    const button = screen.getByRole('button', { name: 'Check now' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
  })

  it('asks for confirmation before queuing the update', async () => {
    const update = vi.fn(() => Promise.resolve({ target: latest }))
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockResolvedValueOnce(makeSystem())
      .mockResolvedValue(makeSystem({ status: { state: 'running', log: ['building'] } }))
    renderSettings(apiWith(version, update))

    fireEvent.click(await screen.findByRole('button', { name: 'Update to bbbbbbb' }))
    expect(screen.getByRole('dialog')).toBeTruthy()
    expect(update).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(update).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Update to bbbbbbb' }))
    fireEvent.click(screen.getByRole('button', { name: 'Update now' }))

    expect(update).toHaveBeenCalledWith(latest)
    expect(await screen.findByText('The update is running.')).toBeTruthy()
    expect(screen.getByText('building')).toBeTruthy()
  })

  it('stops waiting for a queued update when the host helper does not take it', async () => {
    vi.useFakeTimers()
    const update = vi.fn(() => Promise.resolve({ target: latest }))
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockResolvedValueOnce(makeSystem())
      .mockResolvedValueOnce(makeSystem())
      .mockResolvedValue(makeSystem({ update: { error: 'host_helper_missing' } }))
    renderSettings(apiWith(version, update))

    await act(async () => {})
    fireEvent.click(screen.getByRole('button', { name: 'Update to bbbbbbb' }))
    fireEvent.click(screen.getByRole('button', { name: 'Update now' }))
    await act(async () => {})
    expect(update).toHaveBeenCalledWith(latest)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getByText(/is not installed or is not running/)).toBeTruthy()

    const calls = version.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery * 3)
    })
    expect(version).toHaveBeenCalledTimes(calls)
  })

  it('polls a running update until it succeeds and then stops', async () => {
    vi.useFakeTimers()
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockResolvedValueOnce(makeSystem({ status: { state: 'running', log: ['building'] } }))
      .mockResolvedValueOnce(
        makeSystem({ status: { state: 'running', log: ['building', 'restarting'] } }),
      )
      .mockResolvedValue(
        makeSystem({ version: '1.3.0', status: { state: 'succeeded', log: ['done'] } }),
      )
    renderSettings(apiWith(version))

    await act(async () => {})
    expect(screen.getByText('The update is running.')).toBeTruthy()
    expect(screen.getByText('building')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getByText('restarting')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getByText('Updated. The panel is running 1.3.0.')).toBeTruthy()

    const calls = version.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery * 3)
    })
    expect(version).toHaveBeenCalledTimes(calls)
  })

  it('keeps polling while the core restarts and then shows the success', async () => {
    vi.useFakeTimers()
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockResolvedValueOnce(makeSystem({ status: { state: 'running', log: ['building'] } }))
      .mockRejectedValueOnce(new ApiError('network', 0))
      .mockRejectedValueOnce(new ApiError('update_check_pending', 503))
      .mockResolvedValue(
        makeSystem({ version: '1.3.0', status: { state: 'succeeded', log: ['done'] } }),
      )
    renderSettings(apiWith(version))

    await act(async () => {})
    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getAllByText('The core is restarting. Waiting…').length).toBeGreaterThan(0)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getAllByText('The core is restarting. Waiting…').length).toBeGreaterThan(0)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getByText('Updated. The panel is running 1.3.0.')).toBeTruthy()
  })

  it('resumes the progress from the API after a reload', async () => {
    vi.useFakeTimers()
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockResolvedValueOnce(makeSystem({ status: { state: 'running', log: ['building'] } }))
      .mockResolvedValue(makeSystem({ status: { state: 'running', log: ['almost'] } }))
    renderSettings(apiWith(version))

    await act(async () => {})
    expect(screen.getByText('The update is running.')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(version).toHaveBeenCalledTimes(2)
    expect(screen.getByText('almost')).toBeTruthy()
  })

  it('shows a failed update with the log', async () => {
    renderSettings(
      apiWith(() =>
        Promise.resolve(makeSystem({ status: { state: 'failed', log: ['build failed: boom'] } })),
      ),
    )

    expect(await screen.findByText('The update failed.')).toBeTruthy()
    expect(screen.getByText('build failed: boom')).toBeTruthy()
  })

  it('shows a rolled-back update with the log', async () => {
    renderSettings(
      apiWith(() =>
        Promise.resolve(
          makeSystem({ status: { state: 'rolled_back', log: ['health check failed'] } }),
        ),
      ),
    )

    expect(
      await screen.findByText('The update failed and the previous version was restored.'),
    ).toBeTruthy()
    expect(screen.getByText('health check failed')).toBeTruthy()
  })

  it('retries while the first check has not finished', async () => {
    vi.useFakeTimers()
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockRejectedValueOnce(new ApiError('update_check_pending', 503))
      .mockResolvedValue(
        makeSystem({ update: { latest: current, updateAvailable: false, pending: [] } }),
      )
    renderSettings(apiWith(version))

    await act(async () => {})
    expect(screen.getByText('Checking for updates…')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery)
    })
    expect(screen.getByText('Up to date')).toBeTruthy()
  })

  it('shows the error when the first load fails', async () => {
    renderSettings(apiWith(() => Promise.reject(new ApiError('internal', 500))))

    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(screen.getByText('Something went wrong. Try again.')).toBeTruthy()
  })

  it('stops polling when it unmounts', async () => {
    vi.useFakeTimers()
    const version = vi
      .fn<() => Promise<SystemVersion>>()
      .mockResolvedValue(makeSystem({ status: { state: 'running' } }))
    const { unmount } = render(<Settings api={apiWith(version)} />)

    await act(async () => {})
    expect(version).toHaveBeenCalledTimes(1)

    unmount()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(pollEvery * 3)
    })
    expect(version).toHaveBeenCalledTimes(1)
  })
})
