import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { TotpSetup as Setup } from '../api/auth'
import { errorMessage } from './AuthGate'
import { QrCode } from './QrCode'
import { AuthFrame } from './AuthFrame'

export interface TotpSetupProps {
  start: () => Promise<Setup>
  confirm: (code: string) => Promise<void>
}

/** groupSecret splits the base32 secret in blocks of four to read it out. */
export function groupSecret(secret: string): string {
  return secret.replace(/(.{4})/g, '$1 ').trim()
}

/** TotpSetup shows the pending secret and confirms the first code. */
export function TotpSetup({ start, confirm }: TotpSetupProps) {
  const { t } = useTranslation(['auth', 'errors'])
  const [setup, setSetup] = useState<Setup | null>(null)
  const [code, setCode] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const codeField = useRef<HTMLInputElement>(null)
  const requested = useRef(false)

  useEffect(() => {
    // The ref keeps StrictMode's double effect from asking for two secrets.
    if (requested.current) {
      return
    }
    requested.current = true
    start()
      .then(setSetup)
      .catch((cause: unknown) => setError(errorMessage(t, cause)))
  }, [start, t])

  useEffect(() => {
    if (setup !== null) {
      codeField.current?.focus()
    }
  }, [setup])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setPending(true)
    setError(null)
    try {
      await confirm(code)
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setPending(false)
    }
  }

  return (
    <AuthFrame>
      <div className="auth__card">
        <h1 className="auth__title" id="setup-title">
          {t('auth:setup.title')}
        </h1>
        <p className="auth__intro">{t('auth:setup.intro')}</p>
        {error !== null && (
          <p className="auth__error" role="alert">
            {error}
          </p>
        )}
        {setup === null ? (
          <p className="auth__status" role="status">
            {t('auth:setup.loading')}
          </p>
        ) : (
          <form aria-labelledby="setup-title" onSubmit={submit}>
            <p className="auth__intro">{t('auth:setup.steps')}</p>
            <QrCode value={setup.uri} label={t('auth:setup.qrLabel')} />
            <p className="auth__label" id="setup-secret-label">
              {t('auth:setup.secretLabel')}
            </p>
            <code className="auth__secret">{groupSecret(setup.secret)}</code>
            <p className="auth__hint">{t('auth:setup.secretHint')}</p>
            <a className="auth__link" href={setup.uri}>
              {t('auth:setup.openLink')}
            </a>
            <label className="auth__field">
              <span className="auth__label" id="setup-code-label">
                {t('auth:setup.codeLabel')}
              </span>
              <input
                ref={codeField}
                className="auth__input auth__input--code"
                name="code"
                inputMode="numeric"
                autoComplete="one-time-code"
                pattern="[0-9]{6}"
                maxLength={6}
                value={code}
                disabled={pending}
                required
                onChange={(event) => setCode(event.target.value.replace(/\D/g, '').slice(0, 6))}
              />
            </label>
            <button className="auth__submit" type="submit" disabled={pending}>
              {pending ? t('auth:setup.submitting') : t('auth:setup.submit')}
            </button>
          </form>
        )}
      </div>
    </AuthFrame>
  )
}
