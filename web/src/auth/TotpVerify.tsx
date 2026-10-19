import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { TotpPayload } from '../api/auth'
import { errorMessage } from './AuthGate'

export interface TotpVerifyProps {
  verify: (payload: TotpPayload) => Promise<void>
}

/** formatRecoveryCode upper-cases the code and puts the dash in place. */
export function formatRecoveryCode(value: string): string {
  const clean = value
    .toUpperCase()
    .replace(/[^A-Z0-9]/g, '')
    .slice(0, 10)
  return clean.length > 5 ? `${clean.slice(0, 5)}-${clean.slice(5)}` : clean
}

/** TotpVerify asks for the six-digit code or a recovery code. */
export function TotpVerify({ verify }: TotpVerifyProps) {
  const { t } = useTranslation(['auth', 'errors'])
  const [recovery, setRecovery] = useState(false)
  const [code, setCode] = useState('')
  const [recoveryCode, setRecoveryCode] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const firstField = useRef<HTMLInputElement>(null)

  useEffect(() => {
    firstField.current?.focus()
  }, [recovery])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setPending(true)
    setError(null)
    try {
      await verify(recovery ? { recovery_code: recoveryCode } : { code })
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="auth">
      <form className="auth__card" aria-labelledby="verify-title" onSubmit={submit}>
        <h1 className="auth__title" id="verify-title">
          {t('auth:verify.title')}
        </h1>
        <p className="auth__intro">{t('auth:verify.intro')}</p>
        {error !== null && (
          <p className="auth__error" role="alert">
            {error}
          </p>
        )}
        {recovery ? (
          <>
            <label className="auth__field">
              <span className="auth__label">{t('auth:verify.recoveryLabel')}</span>
              <input
                ref={firstField}
                className="auth__input auth__input--code"
                name="recovery_code"
                autoComplete="off"
                spellCheck={false}
                maxLength={11}
                value={recoveryCode}
                disabled={pending}
                required
                onChange={(event) => setRecoveryCode(formatRecoveryCode(event.target.value))}
              />
            </label>
            <p className="auth__hint">{t('auth:verify.recoveryHint')}</p>
          </>
        ) : (
          <label className="auth__field">
            <span className="auth__label">{t('auth:verify.codeLabel')}</span>
            <input
              ref={firstField}
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
        )}
        <button className="auth__submit" type="submit" disabled={pending}>
          {pending ? t('auth:verify.submitting') : t('auth:verify.submit')}
        </button>
        <button
          className="auth__link auth__toggle"
          type="button"
          onClick={() => setRecovery((current) => !current)}
        >
          {recovery ? t('auth:verify.useCode') : t('auth:verify.useRecovery')}
        </button>
      </form>
    </div>
  )
}
