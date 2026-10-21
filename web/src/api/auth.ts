import {
  type AssertionCredentialJSON,
  type PublicKeyCredentialCreationOptionsJSON,
  type PublicKeyCredentialRequestOptionsJSON,
  type RegistrationCredentialJSON,
} from '../auth/webauthn'
import { ApiError, type ApiErrorCode, createHttpClient } from './http'

/** Session is what the dashboard knows about the current sign-in. */
export interface Session {
  username: string
  stage: 'mfa_pending' | 'full'
}

/** LoginResult says which second-factor step comes next. */
export interface LoginResult {
  next: 'totp_setup' | 'totp'
}

/** TotpSetup is the pending secret and the URI of the authenticator app. */
export interface TotpSetup {
  secret: string
  uri: string
}

/** TotpPayload carries either a code or a recovery code. */
export type TotpPayload = { code: string } | { recovery_code: string }

/** PinLoginResult says whether the PIN opened a full session or needs TOTP. */
export type PinLoginResult = 'full' | 'totp'

/** TrustedDevice is a browser that can sign in with a PIN. */
export interface TrustedDevice {
  id: string
  name: string
  createdAt: string
  lastUsedAt: string | null
  current: boolean
}

/** PasskeySummary is one registered passkey. */
export interface PasskeySummary {
  id: string
  name: string
  createdAt: string
  lastUsedAt: string | null
  backupState: boolean
}

/** PasskeysInfo is the passkey list and whether this deployment supports them. */
export interface PasskeysInfo {
  available: boolean
  passkeys: PasskeySummary[]
}

/** PasskeyLoginBegin is the server challenge for a discoverable login. */
export interface PasskeyLoginBegin {
  ceremonyID: string
  options: PublicKeyCredentialRequestOptionsJSON
}

/** PasskeysUnavailableError means the deployment has no relying-party DNS name. */
export class PasskeysUnavailableError extends Error {
  constructor() {
    super('passkeys_unavailable')
    this.name = 'PasskeysUnavailableError'
  }
}

/** AccessApi is the trusted-device and passkey API of the panel. */
export interface AccessApi {
  listDevices(): Promise<TrustedDevice[]>
  createDevice(password: string, pin: string, name: string): Promise<TrustedDevice>
  removeDevice(id: string): Promise<void>
  listPasskeys(): Promise<PasskeysInfo>
  removePasskey(id: string): Promise<void>
  beginPasskeyRegistration(password: string): Promise<PublicKeyCredentialCreationOptionsJSON>
  finishPasskeyRegistration(
    name: string,
    credential: RegistrationCredentialJSON,
  ): Promise<PasskeySummary>
  beginPasskeyLogin(): Promise<PasskeyLoginBegin>
  finishPasskeyLogin(ceremonyID: string, credential: AssertionCredentialJSON): Promise<void>
  /** pinLogin resolves full or totp; wrong PINs reject with invalid_credentials. */
  pinLogin(pin: string): Promise<PinLoginResult>
}

/** AuthApi is the typed authentication API of the core. */
export interface AuthApi extends Partial<AccessApi> {
  session(): Promise<Session>
  login(username: string, password: string): Promise<LoginResult>
  startTotpSetup(): Promise<TotpSetup>
  confirmTotpSetup(code: string): Promise<string[]>
  verifyTotp(payload: TotpPayload): Promise<void>
  logout(): Promise<void>
}

interface apiTrustedDevice {
  id: string
  name: string
  created_at: string
  last_used_at: string | null
  current: boolean
}

interface apiPasskey {
  id: string
  name: string
  created_at: string
  last_used_at: string | null
  backup_state: boolean
}

interface apiDevices {
  devices: apiTrustedDevice[]
}

interface apiPasskeys {
  available: boolean
  passkeys: apiPasskey[]
}

interface apiPasskeyLoginBegin {
  ceremony_id: string
  options: PublicKeyCredentialRequestOptionsJSON
}

const directCodes: Record<string, ApiErrorCode> = {
  invalid_credentials: 'invalid_credentials',
  mfa_required: 'mfa_required',
  session_required: 'session_required',
  invalid_request: 'invalid_request',
  rate_limited: 'rate_limited',
  not_found: 'not_found',
  confirmation_required: 'confirmation_required',
}

function toTrustedDevice(device: apiTrustedDevice): TrustedDevice {
  return {
    id: device.id,
    name: device.name,
    createdAt: device.created_at,
    lastUsedAt: device.last_used_at,
    current: device.current,
  }
}

function toPasskey(passkey: apiPasskey): PasskeySummary {
  return {
    id: passkey.id,
    name: passkey.name,
    createdAt: passkey.created_at,
    lastUsedAt: passkey.last_used_at,
    backupState: passkey.backup_state,
  }
}

function readErrorCode(payload: string): string {
  try {
    const parsed = JSON.parse(payload) as unknown
    if (typeof parsed !== 'object' || parsed === null || !('error' in parsed)) {
      return ''
    }
    const error = parsed.error
    if (typeof error !== 'object' || error === null || !('code' in error)) {
      return ''
    }
    return typeof error.code === 'string' ? error.code : ''
  } catch {
    return ''
  }
}

async function responseError(response: Response): Promise<ApiError> {
  const code = readErrorCode(await response.text())
  return new ApiError(directCodes[code] ?? 'internal', response.status)
}

/** createAuthApi returns the auth API, optionally over an injected fetch. */
export function createAuthApi(fetchImpl?: typeof fetch): AuthApi & AccessApi {
  const doFetch = fetchImpl ?? globalThis.fetch
  const http = createHttpClient(doFetch)

  async function send(method: 'POST' | 'DELETE', path: string, body?: unknown): Promise<Response> {
    try {
      return await doFetch(path, {
        method,
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
      })
    } catch {
      throw new ApiError('network', 0)
    }
  }

  return {
    session: () => http.get<Session>('/api/v1/auth/session'),
    login: (username, password) =>
      http.post<LoginResult>('/api/v1/auth/login', { username, password }),
    startTotpSetup: () => http.post<TotpSetup>('/api/v1/auth/totp/setup'),
    confirmTotpSetup: async (code) => {
      const result = await http.post<{ recovery_codes: string[] }>('/api/v1/auth/totp/confirm', {
        code,
      })
      return result.recovery_codes
    },
    verifyTotp: (payload) => http.post<void>('/api/v1/auth/totp', payload),
    logout: () => http.post<void>('/api/v1/auth/logout'),

    pinLogin: async (pin) => {
      const response = await send('POST', '/api/v1/auth/pin/login', { pin })
      if (response.status === 204) {
        return 'full'
      }
      if (response.status === 401) {
        const code = readErrorCode(await response.text())
        if (code === 'mfa_required') {
          return 'totp'
        }
        throw new ApiError('invalid_credentials', 401)
      }
      throw await responseError(response)
    },

    listDevices: async () => {
      const body = await http.get<apiDevices>('/api/v1/auth/devices')
      return body.devices.map(toTrustedDevice)
    },
    createDevice: async (password, pin, name) => {
      const body = await http.post<apiTrustedDevice>('/api/v1/auth/devices', {
        password,
        pin,
        name,
      })
      return toTrustedDevice(body)
    },
    removeDevice: async (id) => {
      const response = await send('DELETE', `/api/v1/auth/devices/${encodeURIComponent(id)}`)
      if (!response.ok) {
        throw await responseError(response)
      }
    },

    listPasskeys: async () => {
      const body = await http.get<apiPasskeys>('/api/v1/auth/passkeys')
      return {
        available: body.available,
        passkeys: body.passkeys.map(toPasskey),
      }
    },
    removePasskey: async (id) => {
      const response = await send('DELETE', `/api/v1/auth/passkeys/${encodeURIComponent(id)}`)
      if (!response.ok) {
        throw await responseError(response)
      }
    },
    beginPasskeyRegistration: (password) =>
      http.post<PublicKeyCredentialCreationOptionsJSON>('/api/v1/auth/passkeys/register/begin', {
        password,
      }),
    finishPasskeyRegistration: async (name, credential) => {
      const body = await http.post<apiPasskey>('/api/v1/auth/passkeys/register/finish', {
        name,
        credential,
      })
      return toPasskey(body)
    },
    beginPasskeyLogin: async () => {
      const response = await send('POST', '/api/v1/auth/passkey/login/begin', {})
      if (!response.ok) {
        const code = readErrorCode(await response.text())
        if (response.status === 503 && code === 'passkeys_unavailable') {
          throw new PasskeysUnavailableError()
        }
        throw new ApiError(directCodes[code] ?? 'internal', response.status)
      }
      const body = (await response.json()) as apiPasskeyLoginBegin
      return { ceremonyID: body.ceremony_id, options: body.options }
    },
    finishPasskeyLogin: async (ceremonyID, credential) => {
      await http.post<void>('/api/v1/auth/passkey/login/finish', {
        ceremony_id: ceremonyID,
        credential,
      })
    },
  }
}
