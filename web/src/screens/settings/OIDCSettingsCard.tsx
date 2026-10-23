import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { OIDCApi, OIDCSettings } from '../../api/oidc'
import { errorMessage } from '../../auth/AuthGate'
import { CheckIcon, CopyIcon } from '../../components/icons'

interface OIDCSettingsCardProps {
  api: OIDCApi
}

interface FormState {
  enabled: boolean
  issuer: string
  clientId: string
  clientSecret: string
  buttonLabel: string
}

function formOf(settings: OIDCSettings): FormState {
  return {
    enabled: settings.enabled,
    issuer: settings.issuer,
    clientId: settings.clientId,
    // The secret is write-only: the core never returns it.
    clientSecret: '',
    buttonLabel: settings.buttonLabel,
  }
}

/** OIDCSettingsCard edits the single sign-on configuration. */
export function OIDCSettingsCard({ api }: OIDCSettingsCardProps) {
  const { t } = useTranslation(['settings', 'errors'])
  const [settings, setSettings] = useState<OIDCSettings | null>(null)
  const [form, setForm] = useState<FormState>({
    enabled: false,
    issuer: '',
    clientId: '',
    clientSecret: '',
    buttonLabel: '',
  })
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    let active = true
    api.settings().then(
      (next) => {
        if (!active) return
        setSettings(next)
        setForm(formOf(next))
        setLoading(false)
      },
      (cause: unknown) => {
        if (!active) return
        setError(errorMessage(t, cause))
        setLoading(false)
      },
    )
    return () => {
      active = false
    }
  }, [api, t])

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setSaving(true)
    setError(null)
    setNotice(null)
    try {
      const next = await api.update(form)
      setSettings(next)
      setForm(formOf(next))
      setNotice(t('settings:oidc.saved'))
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setSaving(false)
    }
  }

  const copyRedirect = async () => {
    if (settings === null) return
    try {
      await navigator.clipboard.writeText(settings.redirectUrl)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <section className="settings__card" aria-labelledby="settings-oidc-title">
      <div>
        <h2 className="settings__card-title" id="settings-oidc-title">
          {t('settings:oidc.title')}
        </h2>
        <p className="settings__hint">{t('settings:oidc.intro')}</p>
      </div>

      {loading ? (
        <p className="settings__message" role="status">
          {t('settings:oidc.loading')}
        </p>
      ) : settings === null ? (
        <p className="settings__message settings__message--error" role="alert">
          {error ?? t('settings:oidc.loadError')}
        </p>
      ) : (
        <form className="oidc-form" onSubmit={submit}>
          <label className="oidc-form__switch">
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={(event) => setForm({ ...form, enabled: event.target.checked })}
            />
            <span>{t('settings:oidc.enabled')}</span>
          </label>

          <p className="settings__hint">{t('settings:oidc.explanation')}</p>

          <label className="field">
            <span className="field__label">{t('settings:oidc.issuer')}</span>
            <input
              className="field__input"
              type="url"
              value={form.issuer}
              placeholder={t('settings:oidc.issuerPlaceholder')}
              onChange={(event) => setForm({ ...form, issuer: event.target.value })}
            />
          </label>

          <label className="field">
            <span className="field__label">{t('settings:oidc.clientId')}</span>
            <input
              className="field__input"
              value={form.clientId}
              onChange={(event) => setForm({ ...form, clientId: event.target.value })}
            />
          </label>

          <label className="field">
            <span className="field__label">{t('settings:oidc.clientSecret')}</span>
            <input
              className="field__input"
              type="password"
              autoComplete="new-password"
              value={form.clientSecret}
              placeholder={
                settings.hasSecret
                  ? t('settings:oidc.secretSetPlaceholder')
                  : t('settings:oidc.secretEmptyPlaceholder')
              }
              onChange={(event) => setForm({ ...form, clientSecret: event.target.value })}
            />
            <span className="settings__hint">
              {settings.hasSecret
                ? t('settings:oidc.secretSetHint')
                : t('settings:oidc.secretEmptyHint')}
            </span>
          </label>

          <label className="field">
            <span className="field__label">{t('settings:oidc.buttonLabel')}</span>
            <input
              className="field__input"
              value={form.buttonLabel}
              maxLength={40}
              placeholder={t('settings:oidc.buttonLabelPlaceholder')}
              onChange={(event) => setForm({ ...form, buttonLabel: event.target.value })}
            />
          </label>

          <div className="oidc-form__redirect">
            <span className="field__label">{t('settings:oidc.redirect')}</span>
            <code>{settings.redirectUrl}</code>
            <button type="button" className="settings__button" onClick={() => void copyRedirect()}>
              {copied ? <CheckIcon /> : <CopyIcon />}
              {copied ? t('settings:oidc.copied') : t('settings:oidc.copyRedirect')}
            </button>
          </div>
          <p className="settings__hint">{t('settings:oidc.redirectHint')}</p>

          {error !== null ? (
            <p className="settings__message settings__message--error" role="alert">
              {error}
            </p>
          ) : null}
          {notice !== null ? (
            <p className="settings__notice" role="status">
              {notice}
            </p>
          ) : null}

          <div className="users__actions">
            <button
              type="submit"
              className="settings__button settings__button--primary"
              disabled={saving || (form.enabled && (form.issuer === '' || form.clientId === ''))}
            >
              {saving ? t('settings:oidc.saving') : t('settings:oidc.save')}
            </button>
          </div>
        </form>
      )}
    </section>
  )
}
