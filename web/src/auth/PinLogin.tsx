import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { ApiError } from '../api/http'
import { errorMessage } from './AuthGate'

/** DEVICE_STORAGE_KEY remembers that this browser has a trusted device. */
export const DEVICE_STORAGE_KEY = 'talaia.device'
/** MAX_PIN_FAILURES is when the browser forgets a disabled trusted device. */
export const MAX_PIN_FAILURES = 5

/** RememberedDevice is the small non-secret record of a trusted device. */
export interface RememberedDevice {
  name: string
}

/** readTrustedDevice returns the remembered device, or null. */
export function readTrustedDevice(): RememberedDevice | null {
  if (typeof window === 'undefined') {
    return null
  }
  try {
    const raw = window.localStorage.getItem(DEVICE_STORAGE_KEY)
    if (raw === null) {
      return null
    }
    const parsed = JSON.parse(raw) as unknown
    if (typeof parsed !== 'object' || parsed === null || !('name' in parsed)) {
      return null
    }
    const name = parsed.name
    return typeof name === 'string' && name.trim() !== '' ? { name } : null
  } catch {
    return null
  }
}

/** rememberTrustedDevice records the device name in localStorage. */
export function rememberTrustedDevice(name: string): void {
  if (typeof window === 'undefined') {
    return
  }
  try {
    window.localStorage.setItem(DEVICE_STORAGE_KEY, JSON.stringify({ name }))
  } catch {
    // Storage can be disabled; the PIN form just falls back to the password.
  }
}

/** forgetTrustedDevice removes the remembered device. */
export function forgetTrustedDevice(): void {
  if (typeof window === 'undefined') {
    return
  }
  try {
    window.localStorage.removeItem(DEVICE_STORAGE_KEY)
  } catch {
    // Nothing to do: the server is the source of truth.
  }
}

export interface PinLoginProps {
  /** deviceName is the trusted device remembered by this browser. */
  deviceName: string
  /** onPin signs in with the PIN; it rejects with invalid_credentials on a wrong PIN. */
  onPin: (pin: string) => Promise<void>
  /** onUsePassword shows the normal password form. */
  onUsePassword: () => void
}

/** PinLogin is the primary sign-in form of a browser with a trusted device. */
export function PinLogin({ deviceName, onPin, onUsePassword }: PinLoginProps) {
  const { t } = useTranslation(['auth', 'errors'])
  const [pin, setPin] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [failures, setFailures] = useState(0)
  const pinField = useRef<HTMLInputElement>(null)

  useEffect(() => {
    pinField.current?.focus()
  }, [])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!/^\d{6,12}$/.test(pin)) {
      setError(t('auth:pin.format'))
      return
    }
    setPending(true)
    setError(null)
    try {
      await onPin(pin)
    } catch (cause) {
      if (cause instanceof ApiError && cause.code === 'invalid_credentials') {
        const next = failures + 1
        setFailures(next)
        if (next >= MAX_PIN_FAILURES) {
          forgetTrustedDevice()
          onUsePassword()
          return
        }
        setError(t('auth:pin.invalid'))
      } else {
        setError(errorMessage(t, cause))
      }
    } finally {
      setPending(false)
      setPin('')
    }
  }

  return (
    <form className="auth__pin" aria-labelledby="login-title" onSubmit={submit}>
      <p className="auth__intro">{t('auth:pin.intro', { name: deviceName })}</p>
      <label className="auth__field">
        <span className="auth__label">{t('auth:pin.label')}</span>
        <input
          ref={pinField}
          className="auth__input auth__input--pin"
          name="pin"
          type="password"
          inputMode="numeric"
          autoComplete="off"
          pattern="[0-9]{6,12}"
          minLength={6}
          maxLength={12}
          value={pin}
          disabled={pending}
          required
          aria-describedby="pin-hint"
          onChange={(event) => setPin(event.target.value.replace(/\D/g, '').slice(0, 12))}
        />
      </label>
      <p className="auth__hint" id="pin-hint">
        {t('auth:pin.hint')}
      </p>
      {error !== null && (
        <p className="auth__error" role="alert">
          {error}
        </p>
      )}
      <button className="auth__submit" type="submit" disabled={pending}>
        {pending ? t('auth:pin.submitting') : t('auth:pin.submit')}
      </button>
      <button className="auth__link auth__toggle" type="button" onClick={onUsePassword}>
        {t('auth:pin.usePassword')}
      </button>
    </form>
  )
}
