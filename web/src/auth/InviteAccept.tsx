import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { TotpSetup } from '../api/auth'
import type { InvitesApi, InviteInfo } from '../api/invites'
import { AuthFrame } from './AuthFrame'
import { errorMessage } from './AuthGate'

interface InviteAcceptProps {
  api: InvitesApi
  token: string
  onAccepted: (username: string, setup: TotpSetup) => void
  onLogin: () => void
}

/** clearInvitePath removes the one-time token from the address bar. */
function clearInvitePath(): void {
  window.history.replaceState(null, '', '/')
}

/**
 * InviteAccept is the public first-access screen. The server creates the
 * pending session; the normal TOTP setup follows until the recovery codes.
 */
export function InviteAccept({ api, token, onAccepted, onLogin }: InviteAcceptProps) {
  const { t } = useTranslation(['users', 'errors'])
  const [info, setInfo] = useState<InviteInfo | null>(null)
  const [loading, setLoading] = useState(token !== '')
  const [invalid, setInvalid] = useState(token === '')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let active = true
    if (token === '') return
    api.info(token).then(
      (response) => {
        if (!active) return
        setInfo(response)
        setLoading(false)
      },
      () => {
        if (!active) return
        setInvalid(true)
        setLoading(false)
      },
    )
    return () => {
      active = false
    }
  }, [api, token])

  const login = () => {
    clearInvitePath()
    onLogin()
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    if (password !== confirmation) {
      setError(t('users:inviteAccept.mismatch'))
      return
    }
    setPending(true)
    try {
      const setup = await api.accept(token, password)
      clearInvitePath()
      onAccepted(info?.username ?? '', setup)
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setPending(false)
    }
  }

  return (
    <AuthFrame>
      <div className="auth__card">
        <h1 className="auth__title" id="invite-title">
          {t('users:inviteAccept.title')}
        </h1>

        {loading ? (
          <p className="auth__status" role="status">
            {t('users:inviteAccept.loading')}
          </p>
        ) : invalid ? (
          <>
            <p className="auth__error" role="alert">
              {t('users:inviteAccept.invalid')}
            </p>
            <a
              className="auth__link"
              href="/"
              onClick={(event) => {
                event.preventDefault()
                login()
              }}
            >
              {t('users:inviteAccept.goLogin')}
            </a>
          </>
        ) : (
          <>
            <p className="auth__intro">
              {t('users:inviteAccept.intro', { username: info?.username ?? '' })}
            </p>
            {error !== null ? (
              <p className="auth__error" role="alert">
                {error}
              </p>
            ) : null}
            <form aria-labelledby="invite-title" onSubmit={submit}>
              <label className="auth__field">
                <span className="auth__label">{t('users:inviteAccept.password')}</span>
                <input
                  className="auth__input"
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  disabled={pending}
                  required
                  onChange={(event) => setPassword(event.target.value)}
                />
              </label>
              <p className="auth__hint">{t('users:inviteAccept.passwordHint')}</p>
              <label className="auth__field">
                <span className="auth__label">{t('users:inviteAccept.confirm')}</span>
                <input
                  className="auth__input"
                  type="password"
                  autoComplete="new-password"
                  value={confirmation}
                  disabled={pending}
                  required
                  onChange={(event) => setConfirmation(event.target.value)}
                />
              </label>
              <button className="auth__submit" type="submit" disabled={pending}>
                {pending ? t('users:inviteAccept.submitting') : t('users:inviteAccept.submit')}
              </button>
            </form>
            <p className="auth__hint">
              {t('users:inviteAccept.already', { username: info?.username ?? '' })}{' '}
              <button type="button" className="auth__link auth__link--button" onClick={login}>
                {t('users:inviteAccept.goLogin')}
              </button>
            </p>
          </>
        )}
      </div>
    </AuthFrame>
  )
}
