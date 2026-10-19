import { createHttpClient } from './http'

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

/** AuthApi is the typed authentication API of the core. */
export interface AuthApi {
  session(): Promise<Session>
  login(username: string, password: string): Promise<LoginResult>
  startTotpSetup(): Promise<TotpSetup>
  confirmTotpSetup(code: string): Promise<string[]>
  verifyTotp(payload: TotpPayload): Promise<void>
  logout(): Promise<void>
}

/** createAuthApi returns the auth API, optionally over an injected fetch. */
export function createAuthApi(fetchImpl?: typeof fetch): AuthApi {
  const http = createHttpClient(fetchImpl)

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
  }
}
