import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { AccessApi, PasskeySummary, PasskeysInfo, TrustedDevice } from '../../api/auth'
import { ApiError, isSignedOut } from '../../api/http'
import { errorMessage } from '../../auth/AuthGate'
import { forgetTrustedDevice, rememberTrustedDevice } from '../../auth/PinLogin'
import {
  isPasskeyCancellation,
  passkeysSupported,
  serializeRegistrationCredential,
  toCreationOptions,
} from '../../auth/webauthn'

export interface AccessCardProps {
  api: AccessApi
}

/** browserName returns a short default name for a trusted device. */
function browserName(): string {
  if (typeof navigator === 'undefined') {
    return ''
  }
  const agent = navigator.userAgent
  if (agent.includes('Edg/')) return 'Edge'
  if (agent.includes('Firefox/')) return 'Firefox'
  if (agent.includes('Chrome/')) return 'Chrome'
  if (agent.includes('Safari/')) return 'Safari'
  return navigator.platform || ''
}

/** formatDate writes a timestamp with the language of the panel. */
function formatDate(value: string, language: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

/**
 * AccessCard manages the trusted devices that sign in with a PIN and the
 * passkeys that sign in with the fingerprint or the device PIN.
 */
export function AccessCard({ api }: AccessCardProps) {
  const { t, i18n } = useTranslation(['settings', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [devices, setDevices] = useState<TrustedDevice[] | null>(null)
  const [passkeys, setPasskeys] = useState<PasskeysInfo | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)

  const [deviceName, setDeviceName] = useState(browserName)
  const [pin, setPin] = useState('')
  const [pinConfirm, setPinConfirm] = useState('')
  const [devicePassword, setDevicePassword] = useState('')
  const [devicePending, setDevicePending] = useState(false)
  const [deviceError, setDeviceError] = useState<string | null>(null)
  const [deviceAdded, setDeviceAdded] = useState(false)

  const [passkeyName, setPasskeyName] = useState('')
  const [passkeyPassword, setPasskeyPassword] = useState('')
  const [passkeyPending, setPasskeyPending] = useState(false)
  const [passkeyError, setPasskeyError] = useState<string | null>(null)
  const [passkeyAdded, setPasskeyAdded] = useState(false)

  const [removingDevice, setRemovingDevice] = useState<TrustedDevice | null>(null)
  const [removingPasskey, setRemovingPasskey] = useState<PasskeySummary | null>(null)
  const [removePending, setRemovePending] = useState(false)
  const [removeError, setRemoveError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    Promise.all([api.listDevices(), api.listPasskeys()]).then(
      ([nextDevices, nextPasskeys]) => {
        if (cancelled) return
        setDevices(nextDevices)
        setPasskeys(nextPasskeys)
        setLoadError(null)
      },
      (cause: unknown) => {
        if (cancelled || isSignedOut(cause)) return
        setLoadError(errorMessage(t, cause))
      },
    )
    return () => {
      cancelled = true
    }
  }, [api, t])

  async function reload() {
    try {
      const [nextDevices, nextPasskeys] = await Promise.all([api.listDevices(), api.listPasskeys()])
      setDevices(nextDevices)
      setPasskeys(nextPasskeys)
      setLoadError(null)
    } catch (cause) {
      if (isSignedOut(cause)) return
      setLoadError(errorMessage(t, cause))
    }
  }

  async function trustDevice(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const name = deviceName.trim()
    if (name === '') {
      setDeviceError(t('settings:access.devices.nameRequired'))
      return
    }
    if (!/^\d{6,12}$/.test(pin)) {
      setDeviceError(t('settings:access.devices.pinFormat'))
      return
    }
    if (pin !== pinConfirm) {
      setDeviceError(t('settings:access.devices.pinMismatch'))
      return
    }
    if (devicePassword === '') {
      setDeviceError(t('settings:access.devices.passwordRequired'))
      return
    }

    setDevicePending(true)
    setDeviceError(null)
    setDeviceAdded(false)
    try {
      const created = await api.createDevice(devicePassword, pin, name)
      rememberTrustedDevice(created.name)
      await reload()
      setDeviceAdded(true)
    } catch (cause) {
      if (isSignedOut(cause)) return
      if (cause instanceof ApiError && cause.code === 'invalid_credentials') {
        setDeviceError(t('settings:access.devices.invalidPassword'))
      } else {
        setDeviceError(errorMessage(t, cause))
      }
    } finally {
      setDevicePending(false)
      setPin('')
      setPinConfirm('')
      setDevicePassword('')
    }
  }

  async function addPasskey(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const name = passkeyName.trim()
    if (name === '') {
      setPasskeyError(t('settings:access.passkeys.nameRequired'))
      return
    }
    if (passkeyPassword === '') {
      setPasskeyError(t('settings:access.passkeys.passwordRequired'))
      return
    }

    setPasskeyPending(true)
    setPasskeyError(null)
    setPasskeyAdded(false)
    try {
      const options = await api.beginPasskeyRegistration(passkeyPassword)
      const credential = await navigator.credentials.create({
        publicKey: toCreationOptions(options),
      })
      if (credential === null) {
        return
      }
      await api.finishPasskeyRegistration(
        name,
        serializeRegistrationCredential(credential as PublicKeyCredential),
      )
      await reload()
      setPasskeyAdded(true)
    } catch (cause) {
      if (isSignedOut(cause) || isPasskeyCancellation(cause)) {
        return
      }
      if (cause instanceof ApiError && cause.code === 'invalid_credentials') {
        setPasskeyError(t('settings:access.passkeys.invalidPassword'))
      } else {
        setPasskeyError(t('settings:access.passkeys.failed'))
      }
    } finally {
      setPasskeyPending(false)
      setPasskeyName('')
      setPasskeyPassword('')
    }
  }

  function startDeviceRemoval(device: TrustedDevice) {
    setRemovingDevice(device)
    setRemovingPasskey(null)
    setRemoveError(null)
  }

  function startPasskeyRemoval(passkey: PasskeySummary) {
    setRemovingPasskey(passkey)
    setRemovingDevice(null)
    setRemoveError(null)
  }

  function cancelRemoval() {
    setRemovingDevice(null)
    setRemovingPasskey(null)
    setRemoveError(null)
  }

  async function confirmRemoval() {
    const device = removingDevice
    const passkey = removingPasskey
    if (device === null && passkey === null) {
      return
    }
    setRemovePending(true)
    setRemoveError(null)
    try {
      if (device !== null) {
        await api.removeDevice(device.id)
        if (device.current) {
          forgetTrustedDevice()
        }
      } else if (passkey !== null) {
        await api.removePasskey(passkey.id)
      }
      await reload()
      cancelRemoval()
    } catch (cause) {
      if (isSignedOut(cause)) return
      setRemoveError(errorMessage(t, cause))
    } finally {
      setRemovePending(false)
    }
  }

  const unavailable = passkeys !== null && !passkeys.available
  const unsupported = !passkeysSupported()

  return (
    <section className="settings__card" aria-labelledby="settings-access-title">
      <h2 className="settings__card-title" id="settings-access-title">
        {t('settings:access.title')}
      </h2>
      <p className="settings__access-intro">{t('settings:access.intro')}</p>

      {loadError !== null ? (
        <p className="settings__message settings__message--error" role="alert">
          {loadError}
        </p>
      ) : null}
      {devices === null || passkeys === null ? (
        loadError === null ? (
          <p className="settings__message" role="status">
            {t('settings:access.loading')}
          </p>
        ) : null
      ) : (
        <>
          <h3 className="settings__section-title">{t('settings:access.devices.title')}</h3>
          {devices.length === 0 ? (
            <p className="settings__access-empty">{t('settings:access.devices.empty')}</p>
          ) : (
            <ul className="settings__access-list">
              {devices.map((device) => (
                <li className="settings__access-item" key={device.id}>
                  <div className="settings__access-main">
                    <span className="settings__access-name">
                      {device.name}
                      {device.current ? (
                        <span className="settings__access-badge">
                          {t('settings:access.devices.thisDevice')}
                        </span>
                      ) : null}
                    </span>
                    <span className="settings__access-meta">
                      {t('settings:access.created', {
                        date: formatDate(device.createdAt, language),
                      })}
                      {' · '}
                      {device.lastUsedAt === null
                        ? t('settings:access.neverUsed')
                        : t('settings:access.lastUsed', {
                            date: formatDate(device.lastUsedAt, language),
                          })}
                    </span>
                  </div>
                  <button
                    type="button"
                    className="settings__button settings__access-remove"
                    aria-label={t('settings:access.devices.removeLabel', { name: device.name })}
                    onClick={() => startDeviceRemoval(device)}
                  >
                    {t('settings:access.devices.remove')}
                  </button>
                </li>
              ))}
            </ul>
          )}

          <h3 className="settings__section-title">{t('settings:access.devices.trust')}</h3>
          <form
            className="settings__access-form"
            aria-label={t('settings:access.devices.trust')}
            onSubmit={trustDevice}
          >
            <div className="settings__form-row">
              <label className="settings__field">
                <span>{t('settings:access.devices.name')}</span>
                <input
                  className="settings__input"
                  name="device_name"
                  autoComplete="off"
                  value={deviceName}
                  disabled={devicePending}
                  required
                  onChange={(event) => setDeviceName(event.target.value)}
                />
              </label>
              <label className="settings__field">
                <span>{t('settings:access.devices.password')}</span>
                <input
                  className="settings__input"
                  name="device_password"
                  type="password"
                  autoComplete="current-password"
                  value={devicePassword}
                  disabled={devicePending}
                  required
                  onChange={(event) => setDevicePassword(event.target.value)}
                />
              </label>
            </div>
            <div className="settings__form-row">
              <label className="settings__field">
                <span>{t('settings:access.devices.pin')}</span>
                <input
                  className="settings__input"
                  name="device_pin"
                  type="password"
                  inputMode="numeric"
                  autoComplete="off"
                  pattern="[0-9]{6,12}"
                  minLength={6}
                  maxLength={12}
                  value={pin}
                  disabled={devicePending}
                  required
                  onChange={(event) => setPin(event.target.value.replace(/\D/g, '').slice(0, 12))}
                />
              </label>
              <label className="settings__field">
                <span>{t('settings:access.devices.pinConfirm')}</span>
                <input
                  className="settings__input"
                  name="device_pin_confirm"
                  type="password"
                  inputMode="numeric"
                  autoComplete="off"
                  pattern="[0-9]{6,12}"
                  minLength={6}
                  maxLength={12}
                  value={pinConfirm}
                  disabled={devicePending}
                  required
                  onChange={(event) =>
                    setPinConfirm(event.target.value.replace(/\D/g, '').slice(0, 12))
                  }
                />
              </label>
            </div>
            <p className="settings__access-hint">{t('settings:access.devices.pinHint')}</p>
            {deviceError !== null ? (
              <p className="settings__message settings__message--error" role="alert">
                {deviceError}
              </p>
            ) : null}
            {deviceAdded ? (
              <p className="settings__notice" role="status">
                {t('settings:access.devices.added')}
              </p>
            ) : null}
            <div className="settings__form-actions">
              <button
                type="submit"
                className="settings__button settings__button--primary"
                disabled={devicePending}
              >
                {devicePending
                  ? t('settings:access.devices.submitting')
                  : t('settings:access.devices.submit')}
              </button>
            </div>
          </form>

          <h3 className="settings__section-title">{t('settings:access.passkeys.title')}</h3>
          {unavailable ? (
            <p className="settings__access-hint">{t('settings:access.passkeys.unavailable')}</p>
          ) : unsupported ? (
            <p className="settings__access-hint">{t('settings:access.passkeys.unsupported')}</p>
          ) : null}
          {passkeys.passkeys.length === 0 ? (
            <p className="settings__access-empty">{t('settings:access.passkeys.empty')}</p>
          ) : (
            <ul className="settings__access-list">
              {passkeys.passkeys.map((passkey) => (
                <li className="settings__access-item" key={passkey.id}>
                  <div className="settings__access-main">
                    <span className="settings__access-name">{passkey.name}</span>
                    <span className="settings__access-meta">
                      {t('settings:access.created', {
                        date: formatDate(passkey.createdAt, language),
                      })}
                      {' · '}
                      {passkey.lastUsedAt === null
                        ? t('settings:access.neverUsed')
                        : t('settings:access.lastUsed', {
                            date: formatDate(passkey.lastUsedAt, language),
                          })}
                    </span>
                  </div>
                  <button
                    type="button"
                    className="settings__button settings__access-remove"
                    aria-label={t('settings:access.passkeys.removeLabel', { name: passkey.name })}
                    onClick={() => startPasskeyRemoval(passkey)}
                  >
                    {t('settings:access.passkeys.remove')}
                  </button>
                </li>
              ))}
            </ul>
          )}

          {!unavailable && !unsupported ? (
            <>
              <h3 className="settings__section-title">{t('settings:access.passkeys.add')}</h3>
              <form
                className="settings__access-form"
                aria-label={t('settings:access.passkeys.add')}
                onSubmit={addPasskey}
              >
                <div className="settings__form-row">
                  <label className="settings__field">
                    <span>{t('settings:access.passkeys.name')}</span>
                    <input
                      className="settings__input"
                      name="passkey_name"
                      autoComplete="off"
                      value={passkeyName}
                      disabled={passkeyPending}
                      required
                      onChange={(event) => setPasskeyName(event.target.value)}
                    />
                  </label>
                  <label className="settings__field">
                    <span>{t('settings:access.passkeys.password')}</span>
                    <input
                      className="settings__input"
                      name="passkey_password"
                      type="password"
                      autoComplete="current-password"
                      value={passkeyPassword}
                      disabled={passkeyPending}
                      required
                      onChange={(event) => setPasskeyPassword(event.target.value)}
                    />
                  </label>
                </div>
                {passkeyError !== null ? (
                  <p className="settings__message settings__message--error" role="alert">
                    {passkeyError}
                  </p>
                ) : null}
                {passkeyAdded ? (
                  <p className="settings__notice" role="status">
                    {t('settings:access.passkeys.added')}
                  </p>
                ) : null}
                <div className="settings__form-actions">
                  <button
                    type="submit"
                    className="settings__button settings__button--primary"
                    disabled={passkeyPending}
                  >
                    {passkeyPending
                      ? t('settings:access.passkeys.submitting')
                      : t('settings:access.passkeys.submit')}
                  </button>
                </div>
              </form>
            </>
          ) : null}
        </>
      )}

      {removingDevice !== null || removingPasskey !== null ? (
        <div className="settings__overlay">
          <div
            className="settings__dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="settings-access-remove-title"
          >
            <h2 className="settings__dialog-title" id="settings-access-remove-title">
              {t('settings:access.remove.title', {
                name: removingDevice?.name ?? removingPasskey?.name ?? '',
              })}
            </h2>
            <p className="settings__dialog-body">
              {removingDevice !== null
                ? t('settings:access.devices.removeBody')
                : t('settings:access.passkeys.removeBody')}
            </p>
            {removeError !== null ? (
              <p className="settings__message settings__message--error" role="alert">
                {removeError}
              </p>
            ) : null}
            <div className="settings__dialog-actions">
              <button
                type="button"
                className="settings__button settings__button--danger"
                disabled={removePending}
                onClick={() => {
                  void confirmRemoval()
                }}
              >
                {removePending
                  ? t('settings:access.remove.removing')
                  : t('settings:access.remove.confirm')}
              </button>
              <button
                type="button"
                className="settings__button"
                disabled={removePending}
                onClick={cancelRemoval}
              >
                {t('settings:access.remove.cancel')}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </section>
  )
}
