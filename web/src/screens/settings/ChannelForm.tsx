import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type {
  AlertChannel,
  AlertChannelInput,
  ChannelLanguage,
  ChannelType,
} from '../../api/alertsAdmin'
import { errorMessage } from '../../auth/AuthGate'
import { CheckIcon } from '../../components/icons'
import { Dialog } from '../users/Dialog'

interface ChannelFormProps {
  channel: AlertChannel | null
  onClose: () => void
  onSave: (input: AlertChannelInput) => Promise<void>
}

interface FormState {
  name: string
  type: ChannelType
  enabled: boolean
  language: ChannelLanguage
  chatId: string
  server: string
  priority: string
  host: string
  port: string
  from: string
  to: string
  tlsMode: string
}

interface SecretState {
  token: string
  topic: string
  username: string
  password: string
  url: string
  hmacSecret: string
}

const emptySecret: SecretState = {
  token: '',
  topic: '',
  username: '',
  password: '',
  url: '',
  hmacSecret: '',
}

function defaultsFor(type: ChannelType, channel: AlertChannel | null): Partial<FormState> {
  if (type === 'ntfy') {
    return {
      server: channel?.config.server || 'https://ntfy.sh',
      priority: String(channel?.config.priority ?? 3),
    }
  }
  if (type === 'email') {
    return {
      host: channel?.config.host ?? '',
      port: String(channel?.config.port ?? 587),
      from: channel?.config.from ?? '',
      to: (channel?.config.to ?? []).join(', '),
      tlsMode: channel?.config.tlsMode || 'starttls',
    }
  }
  if (type === 'telegram') {
    return { chatId: channel?.config.chatId ?? '' }
  }
  return {}
}

function formOf(channel: AlertChannel | null): FormState {
  const type = channel?.type ?? 'telegram'
  return {
    name: channel?.name ?? '',
    type,
    enabled: channel?.enabled ?? true,
    language: channel?.language ?? 'en',
    chatId: channel?.config.chatId ?? '',
    server: channel?.config.server || (type === 'ntfy' ? 'https://ntfy.sh' : ''),
    priority: String(channel?.config.priority ?? 3),
    host: channel?.config.host ?? '',
    port: String(channel?.config.port ?? 587),
    from: channel?.config.from ?? '',
    to: (channel?.config.to ?? []).join(', '),
    tlsMode: channel?.config.tlsMode || 'starttls',
  }
}

function recipients(value: string): string[] {
  return value
    .split(/[,\n;]/)
    .map((item) => item.trim())
    .filter((item) => item !== '')
}

function validEmail(value: string): boolean {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)
}

/** ChannelForm creates or edits a notification channel without keeping its secret. */
export function ChannelForm({ channel, onClose, onSave }: ChannelFormProps) {
  const { t } = useTranslation(['alertsSettings', 'errors'])
  const [form, setForm] = useState<FormState>(() => formOf(channel))
  const [secret, setSecret] = useState<SecretState>(emptySecret)
  const [replacing, setReplacing] = useState(false)
  const [showErrors, setShowErrors] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  const savedSecret = channel !== null && channel.hasSecret
  const secretVisible = !savedSecret || replacing
  const port = Number(form.port)
  const priority = Number(form.priority)

  const nameInvalid = form.name.trim() === '' || form.name.trim().length > 80
  const configInvalid =
    (form.type === 'telegram' && form.chatId.trim() === '') ||
    (form.type === 'ntfy' &&
      ((form.server.trim() !== '' && !/^https?:\/\//.test(form.server.trim())) ||
        !Number.isInteger(priority) ||
        priority < 1 ||
        priority > 5)) ||
    (form.type === 'email' &&
      (form.host.trim() === '' ||
        !Number.isInteger(port) ||
        port < 1 ||
        port > 65535 ||
        !validEmail(form.from.trim()) ||
        recipients(form.to).length === 0 ||
        !recipients(form.to).every(validEmail) ||
        (form.tlsMode !== 'starttls' && form.tlsMode !== 'tls'))) ||
    (form.type === 'discord' &&
      secretVisible &&
      !/^https:\/\/(discord\.com|discordapp\.com)\/api\/webhooks\//.test(secret.url.trim())) ||
    (form.type === 'webhook' && secretVisible && !/^https?:\/\//.test(secret.url.trim()))
  const secretInvalid =
    secretVisible &&
    ((form.type === 'telegram' && secret.token.trim() === '') ||
      (form.type === 'ntfy' && secret.topic.trim() === '') ||
      // An SMTP relay without authentication is allowed: both or none.
      (form.type === 'email' && (secret.username.trim() === '') !== (secret.password === '')) ||
      ((form.type === 'discord' || form.type === 'webhook') && secret.url.trim() === ''))
  const valid = !nameInvalid && !configInvalid && !secretInvalid

  const changeType = (type: ChannelType) => {
    setForm({ ...form, type, ...defaultsFor(type, null) })
    setSecret(emptySecret)
    if (channel !== null) setReplacing(true)
  }

  const submit = async () => {
    if (!valid) {
      setShowErrors(true)
      return
    }
    setPending(true)
    setSaveError(null)
    const typedSecret = secretVisible
      ? {
          token: secret.token.trim() || undefined,
          topic: secret.topic.trim() || undefined,
          username: secret.username.trim() || undefined,
          password: secret.password || undefined,
          url: secret.url.trim() || undefined,
          hmacSecret: secret.hmacSecret || undefined,
        }
      : undefined
    try {
      await onSave({
        name: form.name.trim(),
        type: form.type,
        enabled: form.enabled,
        language: form.language,
        config: {
          chatId: form.chatId.trim(),
          server: form.server.trim(),
          priority: Number.isInteger(priority) ? priority : null,
          host: form.host.trim(),
          port: Number.isInteger(port) ? port : null,
          from: form.from.trim(),
          to: recipients(form.to),
          tlsMode: form.tlsMode,
        },
        ...(typedSecret === undefined ? {} : { secret: typedSecret }),
      })
      setSecret(emptySecret)
    } catch (cause) {
      setSaveError(errorMessage(t, cause))
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog
      title={
        channel === null
          ? t('alertsSettings:channels.createTitle')
          : t('alertsSettings:channels.editTitle')
      }
      onClose={onClose}
      footer={
        <div className="alerts-admin__actions">
          <button type="button" className="settings__button" onClick={onClose}>
            {t('alertsSettings:actions.cancel')}
          </button>
          <button
            type="button"
            className="settings__button settings__button--primary"
            disabled={pending}
            onClick={() => void submit()}
          >
            {t('alertsSettings:actions.save')}
          </button>
        </div>
      }
    >
      <form
        className="alerts-admin__form"
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
      >
        {showErrors && !valid ? (
          <p className="settings__message settings__message--error" role="alert">
            {t('alertsSettings:validation.fixFields')}
          </p>
        ) : null}

        <label className="field">
          <span className="field__label">{t('alertsSettings:channels.form.name')}</span>
          <input
            className="field__input"
            value={form.name}
            maxLength={80}
            aria-invalid={showErrors && nameInvalid}
            onChange={(event) => setForm({ ...form, name: event.target.value })}
          />
          {showErrors && nameInvalid ? (
            <span className="field__error">{t('alertsSettings:validation.name')}</span>
          ) : null}
        </label>

        <div className="alerts-admin__grid">
          <label className="field">
            <span className="field__label">{t('alertsSettings:channels.form.type')}</span>
            <select
              className="field__input"
              value={form.type}
              onChange={(event) => changeType(event.target.value as ChannelType)}
            >
              {(['telegram', 'ntfy', 'email', 'discord', 'webhook'] as const).map((type) => (
                <option key={type} value={type}>
                  {t(`alertsSettings:channels.types.${type}`)}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span className="field__label">{t('alertsSettings:channels.form.language')}</span>
            <select
              className="field__input"
              value={form.language}
              onChange={(event) =>
                setForm({ ...form, language: event.target.value as ChannelLanguage })
              }
            >
              <option value="ca">{t('alertsSettings:languages.ca')}</option>
              <option value="es">{t('alertsSettings:languages.es')}</option>
              <option value="en">{t('alertsSettings:languages.en')}</option>
            </select>
          </label>
        </div>

        {form.type === 'telegram' ? (
          <label className="field">
            <span className="field__label">{t('alertsSettings:channels.form.chatId')}</span>
            <input
              className="field__input"
              value={form.chatId}
              aria-invalid={showErrors && configInvalid}
              onChange={(event) => setForm({ ...form, chatId: event.target.value })}
            />
          </label>
        ) : null}

        {form.type === 'ntfy' ? (
          <div className="alerts-admin__grid">
            <label className="field">
              <span className="field__label">{t('alertsSettings:channels.form.server')}</span>
              <input
                className="field__input"
                type="url"
                value={form.server}
                aria-invalid={showErrors && configInvalid}
                onChange={(event) => setForm({ ...form, server: event.target.value })}
              />
            </label>
            <label className="field">
              <span className="field__label">{t('alertsSettings:channels.form.priority')}</span>
              <select
                className="field__input"
                value={form.priority}
                onChange={(event) => setForm({ ...form, priority: event.target.value })}
              >
                {[1, 2, 3, 4, 5].map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            </label>
          </div>
        ) : null}

        {form.type === 'email' ? (
          <>
            <div className="alerts-admin__grid">
              <label className="field">
                <span className="field__label">{t('alertsSettings:channels.form.host')}</span>
                <input
                  className="field__input"
                  value={form.host}
                  aria-invalid={showErrors && configInvalid}
                  onChange={(event) => setForm({ ...form, host: event.target.value })}
                />
              </label>
              <label className="field">
                <span className="field__label">{t('alertsSettings:channels.form.port')}</span>
                <input
                  className="field__input"
                  type="number"
                  min={1}
                  max={65535}
                  value={form.port}
                  aria-invalid={showErrors && configInvalid}
                  onChange={(event) => setForm({ ...form, port: event.target.value })}
                />
              </label>
            </div>
            <label className="field">
              <span className="field__label">{t('alertsSettings:channels.form.from')}</span>
              <input
                className="field__input"
                type="email"
                value={form.from}
                aria-invalid={showErrors && configInvalid}
                onChange={(event) => setForm({ ...form, from: event.target.value })}
              />
            </label>
            <label className="field">
              <span className="field__label">{t('alertsSettings:channels.form.to')}</span>
              <input
                className="field__input"
                value={form.to}
                aria-invalid={showErrors && configInvalid}
                onChange={(event) => setForm({ ...form, to: event.target.value })}
              />
              <span className="settings__hint">{t('alertsSettings:channels.form.toHint')}</span>
            </label>
            <label className="field">
              <span className="field__label">{t('alertsSettings:channels.form.tlsMode')}</span>
              <select
                className="field__input"
                value={form.tlsMode}
                onChange={(event) => setForm({ ...form, tlsMode: event.target.value })}
              >
                <option value="starttls">{t('alertsSettings:channels.form.starttls')}</option>
                <option value="tls">{t('alertsSettings:channels.form.tls')}</option>
              </select>
              <span className="settings__hint">{t('alertsSettings:channels.form.tlsHint')}</span>
            </label>
          </>
        ) : null}

        <fieldset className="alerts-admin__fieldset">
          <legend className="field__label">{t('alertsSettings:channels.form.secret')}</legend>
          {savedSecret && !replacing ? (
            <div className="alerts-admin__secret-saved">
              <span className="settings__badge settings__badge--green">
                <CheckIcon />
                {t('alertsSettings:channels.form.secretSaved')}
              </span>
              <button
                type="button"
                className="settings__button"
                onClick={() => {
                  setReplacing(true)
                  setSecret(emptySecret)
                }}
              >
                {t('alertsSettings:channels.form.replace')}
              </button>
            </div>
          ) : (
            <div className="alerts-admin__secret-fields">
              {form.type === 'telegram' ? (
                <label className="field">
                  <span className="field__label">{t('alertsSettings:channels.form.botToken')}</span>
                  <input
                    className="field__input"
                    type="password"
                    autoComplete="new-password"
                    value={secret.token}
                    aria-invalid={showErrors && secretInvalid}
                    onChange={(event) => setSecret({ ...secret, token: event.target.value })}
                  />
                </label>
              ) : null}

              {form.type === 'ntfy' ? (
                <>
                  <label className="field">
                    <span className="field__label">{t('alertsSettings:channels.form.topic')}</span>
                    <input
                      className="field__input"
                      type="password"
                      autoComplete="new-password"
                      value={secret.topic}
                      aria-invalid={showErrors && secretInvalid}
                      onChange={(event) => setSecret({ ...secret, topic: event.target.value })}
                    />
                  </label>
                  <label className="field">
                    <span className="field__label">
                      {t('alertsSettings:channels.form.accessToken')}
                    </span>
                    <input
                      className="field__input"
                      type="password"
                      autoComplete="new-password"
                      value={secret.token}
                      onChange={(event) => setSecret({ ...secret, token: event.target.value })}
                    />
                    <span className="settings__hint">
                      {t('alertsSettings:channels.form.optional')}
                    </span>
                  </label>
                </>
              ) : null}

              {form.type === 'email' ? (
                <div className="alerts-admin__grid">
                  <label className="field">
                    <span className="field__label">
                      {t('alertsSettings:channels.form.username')}
                    </span>
                    <input
                      className="field__input"
                      type="password"
                      autoComplete="new-password"
                      value={secret.username}
                      aria-invalid={showErrors && secretInvalid}
                      onChange={(event) => setSecret({ ...secret, username: event.target.value })}
                    />
                  </label>
                  <label className="field">
                    <span className="field__label">
                      {t('alertsSettings:channels.form.password')}
                    </span>
                    <input
                      className="field__input"
                      type="password"
                      autoComplete="new-password"
                      value={secret.password}
                      aria-invalid={showErrors && secretInvalid}
                      onChange={(event) => setSecret({ ...secret, password: event.target.value })}
                    />
                  </label>
                </div>
              ) : null}

              {form.type === 'discord' || form.type === 'webhook' ? (
                <label className="field">
                  <span className="field__label">
                    {form.type === 'discord'
                      ? t('alertsSettings:channels.form.webhookUrl')
                      : t('alertsSettings:channels.form.endpointUrl')}
                  </span>
                  <input
                    className="field__input"
                    type="password"
                    autoComplete="new-password"
                    value={secret.url}
                    aria-invalid={showErrors && secretInvalid}
                    onChange={(event) => setSecret({ ...secret, url: event.target.value })}
                  />
                </label>
              ) : null}

              {form.type === 'webhook' ? (
                <label className="field">
                  <span className="field__label">{t('alertsSettings:channels.form.hmac')}</span>
                  <input
                    className="field__input"
                    type="password"
                    autoComplete="new-password"
                    value={secret.hmacSecret}
                    onChange={(event) => setSecret({ ...secret, hmacSecret: event.target.value })}
                  />
                  <span className="settings__hint">
                    {t('alertsSettings:channels.form.hmacHint')}
                  </span>
                  <span className="settings__hint">
                    {t('alertsSettings:channels.form.signatureHint')}
                  </span>
                </label>
              ) : null}
            </div>
          )}
          {showErrors && secretInvalid ? (
            <span className="field__error">{t('alertsSettings:validation.secret')}</span>
          ) : null}
        </fieldset>

        <label className="alerts-admin__switch">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(event) => setForm({ ...form, enabled: event.target.checked })}
          />
          <span>{t('alertsSettings:channels.form.enabled')}</span>
        </label>

        {saveError !== null ? (
          <p className="settings__message settings__message--error" role="alert">
            {saveError}
          </p>
        ) : null}
      </form>
    </Dialog>
  )
}
