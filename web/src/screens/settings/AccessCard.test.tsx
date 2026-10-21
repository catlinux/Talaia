import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AccessApi, PasskeySummary, PasskeysInfo, TrustedDevice } from '../../api/auth'
import { DEVICE_STORAGE_KEY } from '../../auth/PinLogin'
import type {
  PublicKeyCredentialCreationOptionsJSON,
  RegistrationCredentialJSON,
} from '../../auth/webauthn'
import i18n from '../../i18n'
import { AccessCard } from './AccessCard'

const laptop: TrustedDevice = {
  id: 'dev-1',
  name: 'laptop',
  createdAt: '2026-10-20T10:00:00Z',
  lastUsedAt: null,
  current: true,
}

const desktop: TrustedDevice = {
  id: 'dev-2',
  name: 'desktop',
  createdAt: '2026-10-19T09:00:00Z',
  lastUsedAt: '2026-10-21T08:00:00Z',
  current: false,
}

const workbook: PasskeySummary = {
  id: 'key-1',
  name: 'workbook',
  createdAt: '2026-10-18T07:00:00Z',
  lastUsedAt: null,
  backupState: false,
}

const creationOptions: PublicKeyCredentialCreationOptionsJSON = {
  publicKey: {
    challenge: 'AQID',
    rp: { id: 'panel.example.com', name: 'Talaia' },
    user: { id: 'BAUG', name: 'admin', displayName: 'admin' },
    pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
  },
}

const originalCredentials = navigator.credentials

/** fakeAccess builds the access API; the tests override what they need. */
function fakeAccess(overrides: Partial<AccessApi> = {}): AccessApi {
  return {
    listDevices: () => Promise.resolve([]),
    createDevice: vi.fn(() => Promise.resolve(laptop)),
    removeDevice: vi.fn(() => Promise.resolve()),
    listPasskeys: () => Promise.resolve({ available: true, passkeys: [] }),
    removePasskey: vi.fn(() => Promise.resolve()),
    beginPasskeyRegistration: vi.fn(() => Promise.resolve(creationOptions)),
    finishPasskeyRegistration: vi.fn(() => Promise.resolve(workbook)),
    beginPasskeyLogin: vi.fn(() => Promise.reject(new Error('unexpected passkey login'))),
    finishPasskeyLogin: vi.fn(() => Promise.resolve()),
    pinLogin: vi.fn(() => Promise.resolve('full' as const)),
    ...overrides,
  }
}

/** enableCreate installs the browser APIs a passkey registration needs. */
function enableCreate(
  createImpl: (options: CredentialCreationOptions) => Promise<Credential | null>,
) {
  Object.defineProperty(window, 'PublicKeyCredential', {
    value: class {},
    configurable: true,
  })
  const create = vi.fn(createImpl)
  Object.defineProperty(navigator, 'credentials', {
    value: { create, get: vi.fn() },
    configurable: true,
  })
  return create
}

/** mockRegistrationCredential builds the subset the registration serializes. */
function mockRegistrationCredential(): PublicKeyCredential {
  return {
    id: 'new-key',
    rawId: new Uint8Array([1, 2]).buffer,
    type: 'public-key',
    response: {
      clientDataJSON: new Uint8Array([3]).buffer,
      attestationObject: new Uint8Array([4]).buffer,
      getTransports: () => ['internal'],
    },
    getClientExtensionResults: () => ({}),
  } as unknown as PublicKeyCredential
}

function renderCard(api: AccessApi) {
  render(<AccessCard api={api} />)
}

describe('AccessCard', () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    Object.defineProperty(navigator, 'credentials', {
      value: originalCredentials,
      configurable: true,
    })
    Reflect.deleteProperty(window, 'PublicKeyCredential')
    cleanup()
  })

  it('lists the trusted devices and the passkeys', async () => {
    renderCard(
      fakeAccess({
        listDevices: () => Promise.resolve([laptop, desktop]),
        listPasskeys: () => Promise.resolve({ available: true, passkeys: [workbook] }),
      }),
    )

    expect(await screen.findByText('laptop')).toBeTruthy()
    expect(screen.getByText('This device')).toBeTruthy()
    expect(screen.getByText('desktop')).toBeTruthy()
    expect(screen.getByText('workbook')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Remove laptop' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Remove workbook' })).toBeTruthy()
    expect(screen.getAllByText(/Created /)).toHaveLength(3)
  })

  it('removes the current device and forgets it', async () => {
    localStorage.setItem(DEVICE_STORAGE_KEY, JSON.stringify({ name: 'laptop' }))
    const listDevices = vi
      .fn<() => Promise<TrustedDevice[]>>()
      .mockResolvedValueOnce([laptop])
      .mockResolvedValueOnce([])
    const removeDevice = vi.fn<(id: string) => Promise<void>>(() => Promise.resolve())

    renderCard(fakeAccess({ listDevices, removeDevice }))

    fireEvent.click(await screen.findByRole('button', { name: 'Remove laptop' }))
    expect(screen.getByRole('dialog')).toBeTruthy()
    expect(screen.getByText('Remove laptop?')).toBeTruthy()
    expect(screen.getByText('This browser will no longer sign in with a PIN.')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Remove' }))

    await waitFor(() => expect(removeDevice).toHaveBeenCalledWith('dev-1'))
    await waitFor(() => expect(listDevices).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(localStorage.getItem(DEVICE_STORAGE_KEY)).toBeNull()
    expect(screen.getByText('No trusted devices yet.')).toBeTruthy()
  })

  it('trusts this browser and remembers its name', async () => {
    const created: TrustedDevice = { ...laptop, name: 'my laptop' }
    const listDevices = vi
      .fn<() => Promise<TrustedDevice[]>>()
      .mockResolvedValueOnce([])
      .mockResolvedValue([created])
    const createDevice = vi.fn<
      (password: string, pin: string, name: string) => Promise<TrustedDevice>
    >(() => Promise.resolve(created))

    renderCard(fakeAccess({ listDevices, createDevice }))

    const form = await screen.findByRole('form', { name: 'Trust this device' })
    fireEvent.change(within(form).getByLabelText('Device name'), {
      target: { value: 'my laptop' },
    })
    fireEvent.change(within(form).getByLabelText('Current password'), {
      target: { value: 'secret' },
    })
    fireEvent.change(within(form).getByLabelText('PIN'), { target: { value: '123456' } })
    fireEvent.change(within(form).getByLabelText('Confirm PIN'), {
      target: { value: '123456' },
    })
    fireEvent.click(within(form).getByRole('button', { name: 'Trust this device' }))

    await waitFor(() => expect(createDevice).toHaveBeenCalledWith('secret', '123456', 'my laptop'))
    expect(await screen.findByText('This browser is now a trusted device.')).toBeTruthy()
    expect(JSON.parse(localStorage.getItem(DEVICE_STORAGE_KEY) ?? '{}')).toEqual({
      name: 'my laptop',
    })
    expect((within(form).getByLabelText('Current password') as HTMLInputElement).value).toBe('')
    expect((within(form).getByLabelText('PIN') as HTMLInputElement).value).toBe('')
    expect((within(form).getByLabelText('Confirm PIN') as HTMLInputElement).value).toBe('')
  })

  it('explains that passkeys need HTTPS and hides the add form', async () => {
    renderCard(
      fakeAccess({
        listPasskeys: () => Promise.resolve({ available: false, passkeys: [] }),
      }),
    )

    expect(await screen.findByText('Passkeys need HTTPS with a domain name.')).toBeTruthy()
    expect(screen.queryByRole('form', { name: 'Add a passkey' })).toBeNull()
  })

  it('adds a passkey with the browser prompt', async () => {
    const create = enableCreate(() => Promise.resolve(mockRegistrationCredential()))
    const added: PasskeySummary = { ...workbook, name: 'my key' }
    const listPasskeys = vi
      .fn<() => Promise<PasskeysInfo>>()
      .mockResolvedValueOnce({ available: true, passkeys: [] })
      .mockResolvedValue({ available: true, passkeys: [added] })
    const beginPasskeyRegistration = vi.fn<
      (password: string) => Promise<PublicKeyCredentialCreationOptionsJSON>
    >(() => Promise.resolve(creationOptions))
    const finishPasskeyRegistration = vi.fn<
      (name: string, credential: RegistrationCredentialJSON) => Promise<PasskeySummary>
    >(() => Promise.resolve(added))

    renderCard(fakeAccess({ listPasskeys, beginPasskeyRegistration, finishPasskeyRegistration }))

    const form = await screen.findByRole('form', { name: 'Add a passkey' })
    fireEvent.change(within(form).getByLabelText('Passkey name'), {
      target: { value: 'my key' },
    })
    fireEvent.change(within(form).getByLabelText('Current password'), {
      target: { value: 'secret' },
    })
    fireEvent.click(within(form).getByRole('button', { name: 'Add a passkey' }))

    await waitFor(() => expect(beginPasskeyRegistration).toHaveBeenCalledWith('secret'))
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1))
    const options = create.mock.calls[0]?.[0]
    const challenge = options?.publicKey?.challenge as ArrayBuffer
    expect([...new Uint8Array(challenge)]).toEqual([1, 2, 3])
    await waitFor(() =>
      expect(finishPasskeyRegistration).toHaveBeenCalledWith(
        'my key',
        expect.objectContaining({ id: 'new-key' }),
      ),
    )
    expect(await screen.findByText('The passkey was added.')).toBeTruthy()
    expect((within(form).getByLabelText('Passkey name') as HTMLInputElement).value).toBe('')
    expect((within(form).getByLabelText('Current password') as HTMLInputElement).value).toBe('')
    expect(await screen.findByText('my key')).toBeTruthy()
  })

  it('removes a passkey after confirmation', async () => {
    const listPasskeys = vi
      .fn<() => Promise<PasskeysInfo>>()
      .mockResolvedValueOnce({ available: true, passkeys: [workbook] })
      .mockResolvedValue({ available: true, passkeys: [] })
    const removePasskey = vi.fn<(id: string) => Promise<void>>(() => Promise.resolve())

    renderCard(fakeAccess({ listPasskeys, removePasskey }))

    fireEvent.click(await screen.findByRole('button', { name: 'Remove workbook' }))
    expect(screen.getByText('Remove workbook?')).toBeTruthy()
    expect(screen.getByText('This passkey will no longer sign in.')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Remove' }))

    await waitFor(() => expect(removePasskey).toHaveBeenCalledWith('key-1'))
    await waitFor(() => expect(listPasskeys).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(screen.getByText('No passkeys yet.')).toBeTruthy()
  })

  it('does not offer passkeys when the browser has no WebAuthn', async () => {
    renderCard(
      fakeAccess({
        listPasskeys: () => Promise.resolve({ available: true, passkeys: [] }),
      }),
    )

    expect(await screen.findByText('This browser does not support passkeys.')).toBeTruthy()
    expect(screen.queryByRole('form', { name: 'Add a passkey' })).toBeNull()
  })
})
