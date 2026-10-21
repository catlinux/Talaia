import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'

import { createAuthApi, type AuthApi, type TotpPayload } from '../api/auth'
import { ApiError, onSignedOut, type ApiErrorCode } from '../api/http'
import { Login } from './Login'
import { RecoveryCodes } from './RecoveryCodes'
import { TotpSetup } from './TotpSetup'
import { TotpVerify } from './TotpVerify'
import { serializeAssertionCredential, toRequestOptions } from './webauthn'

/** errorKeys maps the stable code of the core to the errors namespace. */
const errorKeys: Record<ApiErrorCode, string> = {
  invalid_credentials: 'invalidCredentials',
  mfa_required: 'mfaRequired',
  session_required: 'sessionRequired',
  forbidden_origin: 'forbiddenOrigin',
  invalid_request: 'invalidRequest',
  weak_password: 'weakPassword',
  rate_limited: 'rateLimited',
  name_taken: 'nameTaken',
  confirmation_required: 'confirmationRequired',
  agent_offline: 'agentOffline',
  agent_busy: 'agentBusy',
  too_many_terminals: 'tooManyTerminals',
  terminal_exists: 'terminalExists',
  terminal_open_failed: 'terminalOpenFailed',
  not_found: 'notFound',
  target_not_offered: 'targetNotOffered',
  update_in_progress: 'updateInProgress',
  host_helper_missing: 'hostHelperMissing',
  update_check_pending: 'updateCheckPending',
  internal: 'generic',
  network: 'network',
}

/** errorMessage returns the translated text of a failed call. */
export function errorMessage(t: TFunction, error: unknown): string {
  const code = error instanceof ApiError ? error.code : 'internal'
  return t(`errors:${errorKeys[code]}`)
}

/** AuthContextValue is what the panel knows about the signed-in user. */
export interface AuthContextValue {
  username: string
  signOut: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

/** useAuth returns the session of the panel, or null outside the gate. */
export function useAuth(): AuthContextValue | null {
  return useContext(AuthContext)
}

type GateState =
  | { kind: 'loading' }
  | { kind: 'login'; expired?: boolean }
  | { kind: 'setup'; username: string }
  | { kind: 'verify'; username: string }
  | { kind: 'codes'; username: string; codes: string[] }
  | { kind: 'ready'; username: string }

export interface AuthGateProps {
  /** api is injected by the tests; the panel builds its own otherwise. */
  api?: AuthApi
  children: ReactNode
}

/**
 * AuthGate keeps the panel behind a full session: it asks the core who is
 * signed in and shows the sign-in, second-factor or recovery-code screens
 * until the session is complete.
 */
export function AuthGate({ api, children }: AuthGateProps) {
  const { t } = useTranslation('auth')
  const client = useMemo(() => api ?? createAuthApi(), [api])
  const [state, setState] = useState<GateState>({ kind: 'loading' })

  const toLogin = useCallback(() => {
    // A call found no session: tell the user why the form is back.
    setState({ kind: 'login', expired: true })
  }, [])

  useEffect(() => {
    let active = true
    client
      .session()
      .then((session) => {
        if (!active) {
          return
        }
        setState(
          session.stage === 'full'
            ? { kind: 'ready', username: session.username }
            : { kind: 'verify', username: session.username },
        )
      })
      .catch(() => {
        if (active) {
          setState({ kind: 'login' })
        }
      })
    return () => {
      active = false
    }
  }, [client])

  // A later call that finds no session sends the user back to the form.
  useEffect(() => onSignedOut(toLogin), [toLogin])

  const login = useCallback(
    async (username: string, password: string) => {
      const result = await client.login(username, password)
      const step = result.next === 'totp_setup' ? 'setup' : 'verify'
      setState({ kind: step, username })
    },
    [client],
  )

  const loginPin = useCallback(
    async (pin: string) => {
      const pinLogin = client.pinLogin
      if (!pinLogin) {
        return
      }
      const result = await pinLogin(pin)
      const session = await client.session()
      setState(
        result === 'full' && session.stage === 'full'
          ? { kind: 'ready', username: session.username }
          : { kind: 'verify', username: session.username },
      )
    },
    [client],
  )

  const loginPasskey = useCallback(async () => {
    const begin = client.beginPasskeyLogin
    const finish = client.finishPasskeyLogin
    if (!begin || !finish) {
      return
    }
    const started = await begin()
    const credential = await navigator.credentials.get({
      publicKey: toRequestOptions(started.options),
    })
    if (credential === null) {
      return
    }
    await finish(
      started.ceremonyID,
      serializeAssertionCredential(credential as PublicKeyCredential),
    )
    const session = await client.session()
    setState(
      session.stage === 'full'
        ? { kind: 'ready', username: session.username }
        : { kind: 'verify', username: session.username },
    )
  }, [client])

  const startSetup = useCallback(() => client.startTotpSetup(), [client])

  const confirmSetup = useCallback(
    async (code: string) => {
      const codes = await client.confirmTotpSetup(code)
      setState((current) =>
        current.kind === 'setup' ? { kind: 'codes', username: current.username, codes } : current,
      )
    },
    [client],
  )

  const verifyTotp = useCallback(
    async (payload: TotpPayload) => {
      await client.verifyTotp(payload)
      setState((current) =>
        current.kind === 'verify' ? { kind: 'ready', username: current.username } : current,
      )
    },
    [client],
  )

  const signOut = useCallback(async () => {
    try {
      await client.logout()
    } catch {
      // The session is gone anyway: the user asked to sign out.
    }
    setState({ kind: 'login' })
  }, [client])

  if (state.kind === 'loading') {
    return (
      <section className="auth">
        <p className="auth__card auth__status" role="status">
          {t('loading')}
        </p>
      </section>
    )
  }

  if (state.kind === 'login') {
    return (
      <Login
        onSubmit={login}
        onPin={client.pinLogin ? loginPin : undefined}
        onPasskey={client.beginPasskeyLogin && client.finishPasskeyLogin ? loginPasskey : undefined}
        expired={state.expired === true}
      />
    )
  }

  if (state.kind === 'setup') {
    return <TotpSetup start={startSetup} confirm={confirmSetup} />
  }

  if (state.kind === 'verify') {
    return <TotpVerify verify={verifyTotp} />
  }

  if (state.kind === 'codes') {
    return (
      <RecoveryCodes
        codes={state.codes}
        onContinue={() => setState({ kind: 'ready', username: state.username })}
      />
    )
  }

  return (
    <AuthContext.Provider value={{ username: state.username, signOut }}>
      {children}
    </AuthContext.Provider>
  )
}
