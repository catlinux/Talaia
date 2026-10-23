import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { PasskeysUnavailableError } from '../api/auth'
import type { OIDCLoginConfig } from '../api/oidc'
import { errorMessage } from './AuthGate'
import { AuthFrame } from './AuthFrame'
import { PinLogin, readTrustedDevice } from './PinLogin'
import { isPasskeyCancellation, passkeysSupported } from './webauthn'

export interface LoginProps {
  onSubmit: (username: string, password: string) => Promise<void>
  /** onPin signs in with the PIN of the remembered trusted device. */
  onPin?: (pin: string) => Promise<void>
  /** onPasskey starts the discoverable passkey flow. */
  onPasskey?: () => Promise<void>
  /** expired tells the user the previous session ended on its own. */
  expired?: 'expired' | 'revoked' | 'disabled'
  /** oidc is the public configuration of the single sign-on provider. */
  oidc?: OIDCLoginConfig | null
}

/** Login is the first screen: PIN for a trusted browser, password and passkeys. */
export function Login({ onSubmit, onPin, onPasskey, expired, oidc }: LoginProps) {
  const { t } = useTranslation(['auth', 'errors'])
  const [trustedDevice] = useState(() => readTrustedDevice())
  const [mode, setMode] = useState<'pin' | 'password'>(() =>
    trustedDevice !== null && onPin !== undefined ? 'pin' : 'password',
  )
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [passkeyPending, setPasskeyPending] = useState(false)
  const [passkeyError, setPasskeyError] = useState<string | null>(null)
  const [passkeyAvailable, setPasskeyAvailable] = useState(true)
  const usernameField = useRef<HTMLInputElement>(null)
  const oidcError = new URLSearchParams(window.location.search).has('oidc_error')

  useEffect(() => {
    if (mode === 'password') {
      usernameField.current?.focus()
    }
  }, [mode])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setPending(true)
    setError(null)
    try {
      await onSubmit(username, password)
    } catch (cause) {
      // The message is generic for a wrong password: it never says which field
      // was wrong.
      setError(errorMessage(t, cause))
    } finally {
      setPending(false)
      setPassword('')
    }
  }

  async function startPasskey() {
    if (!onPasskey) {
      return
    }
    setPasskeyPending(true)
    setPasskeyError(null)
    try {
      await onPasskey()
    } catch (cause) {
      if (cause instanceof PasskeysUnavailableError) {
        setPasskeyAvailable(false)
        return
      }
      if (isPasskeyCancellation(cause)) {
        return
      }
      setPasskeyError(t('auth:passkey.failed'))
    } finally {
      setPasskeyPending(false)
    }
  }

  const showPasskey = onPasskey !== undefined && passkeysSupported() && passkeyAvailable

  return (
    <AuthFrame>
      <div className="auth__card" aria-labelledby="login-title">
        <h1 className="auth__title" id="login-title">
          {t('auth:login.title')}
        </h1>
        {error === null && expired && (
          <p className="auth__status" role="status">
            {t(
              expired === 'disabled'
                ? 'errors:accountDisabled'
                : expired === 'revoked'
                  ? 'errors:sessionRevoked'
                  : 'errors:sessionRequired',
            )}
          </p>
        )}
        {oidcError ? (
          <p className="auth__status" role="status">
            {t('auth:oidc.failed')}
          </p>
        ) : null}

        {mode === 'pin' && trustedDevice !== null && onPin !== undefined ? (
          <PinLogin
            deviceName={trustedDevice.name}
            onPin={onPin}
            onUsePassword={() => setMode('password')}
          />
        ) : (
          <form className="auth__form" aria-labelledby="login-title" onSubmit={submit}>
            <p className="auth__intro">{t('auth:login.intro')}</p>
            {error !== null && (
              <p className="auth__error" role="alert">
                {error}
              </p>
            )}
            <label className="auth__field">
              <span className="auth__label">{t('auth:login.username')}</span>
              <input
                ref={usernameField}
                className="auth__input"
                name="username"
                autoComplete="username"
                value={username}
                disabled={pending}
                required
                onChange={(event) => setUsername(event.target.value)}
              />
            </label>
            <label className="auth__field">
              <span className="auth__label">{t('auth:login.password')}</span>
              <input
                className="auth__input"
                name="password"
                type="password"
                autoComplete="current-password"
                value={password}
                disabled={pending}
                required
                onChange={(event) => setPassword(event.target.value)}
              />
            </label>
            <button className="auth__submit" type="submit" disabled={pending}>
              {pending ? t('auth:login.submitting') : t('auth:login.submit')}
            </button>
          </form>
        )}

        {oidc?.enabled ? (
          <button
            className="auth__button auth__sso"
            type="button"
            onClick={() => window.location.assign('/api/v1/auth/oidc/start')}
          >
            {t('auth:oidc.button', {
              label: oidc.label.trim() === '' ? t('auth:oidc.defaultLabel') : oidc.label,
            })}
          </button>
        ) : null}
        {showPasskey ? (
          <button
            className="auth__button auth__passkey"
            type="button"
            disabled={passkeyPending}
            onClick={() => {
              void startPasskey()
            }}
          >
            {passkeyPending ? t('auth:passkey.pending') : t('auth:passkey.button')}
          </button>
        ) : null}
        {passkeyError !== null ? (
          <p className="auth__error" role="alert">
            {passkeyError}
          </p>
        ) : null}
      </div>
    </AuthFrame>
  )
}
