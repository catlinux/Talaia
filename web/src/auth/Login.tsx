import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { errorMessage } from './AuthGate'

export interface LoginProps {
  onSubmit: (username: string, password: string) => Promise<void>
  /** expired tells the user the previous session ended on its own. */
  expired?: boolean
}

/** Login is the first screen: username and password only. */
export function Login({ onSubmit, expired = false }: LoginProps) {
  const { t } = useTranslation(['auth', 'errors'])
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const usernameField = useRef<HTMLInputElement>(null)

  useEffect(() => {
    usernameField.current?.focus()
  }, [])

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
    }
  }

  return (
    <div className="auth">
      <form className="auth__card" aria-labelledby="login-title" onSubmit={submit}>
        <h1 className="auth__title" id="login-title">
          {t('auth:login.title')}
        </h1>
        <p className="auth__intro">{t('auth:login.intro')}</p>
        {error === null && expired && (
          <p className="auth__status" role="status">
            {t('errors:sessionRequired')}
          </p>
        )}
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
    </div>
  )
}
