import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { APIKey, APIKeysApi, CreatedAPIKey } from '../../api/apikeys'
import type { Group } from '../../api/groups'
import type { ServerSummary } from '../../api/servers'
import type { Role, Scope } from '../../api/users'
import { errorMessage } from '../../auth/AuthGate'
import { CheckIcon, CopyIcon, PlusIcon, TrashIcon } from '../../components/icons'
import {
  RoleDescription,
  RoleSelect,
  ScopeFields,
  ScopeLabel,
  roleLabel,
} from '../users/AccessFields'
import { Dialog } from '../users/Dialog'

interface APIKeysCardProps {
  api: APIKeysApi
  canListAll: boolean
  servers: ServerSummary[]
  groups: Group[]
}

type KeyStatus = 'active' | 'expired' | 'revoked'

// One clock for the whole page: keys that expire while it is open are rare.
const loadedAt = Date.now()

function keyStatus(key: APIKey, now: number): KeyStatus {
  if (key.revoked) return 'revoked'
  const expires = new Date(key.expiresAt).getTime()
  if (!Number.isNaN(expires) && expires <= now) return 'expired'
  return 'active'
}

function formatDate(value: string | null, language: string, never: string): string {
  if (value === null || value === '') return never
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

function scopeValid(scope: Scope): boolean {
  switch (scope.type) {
    case 'organization':
      return true
    case 'group':
      return (scope.groupId ?? '') !== ''
    case 'server':
      return (scope.serverId ?? '') !== ''
    case 'stack':
    case 'container':
      return (scope.serverId ?? '') !== '' && (scope.name ?? '').trim() !== ''
  }
}

/** APIKeysCard lists and creates the API keys of the signed-in user. */
export function APIKeysCard({ api, canListAll, servers, groups }: APIKeysCardProps) {
  const { t, i18n } = useTranslation(['settings', 'users', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [keys, setKeys] = useState<APIKey[] | null>(null)
  const [all, setAll] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [revoking, setRevoking] = useState<APIKey | null>(null)
  const [revokeBusy, setRevokeBusy] = useState(false)
  const [revokeError, setRevokeError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      setKeys(await api.list(all))
    } catch (cause) {
      setError(errorMessage(t, cause))
    }
  }, [all, api, t])

  useEffect(() => {
    let active = true
    queueMicrotask(() => {
      api.list(all).then(
        (next) => {
          if (active) {
            setKeys(next)
            setError(null)
          }
        },
        (cause: unknown) => {
          if (active) setError(errorMessage(t, cause))
        },
      )
    })
    return () => {
      active = false
    }
  }, [all, api, t])

  const confirmRevoke = async () => {
    if (revoking === null) return
    setRevokeBusy(true)
    setRevokeError(null)
    try {
      await api.revoke(revoking.id)
      setRevoking(null)
      await load()
    } catch (cause) {
      setRevokeError(errorMessage(t, cause))
    } finally {
      setRevokeBusy(false)
    }
  }

  return (
    <section className="settings__card" aria-labelledby="settings-apikeys-title">
      <div className="settings__card-head">
        <div>
          <h2 className="settings__card-title" id="settings-apikeys-title">
            {t('settings:apiKeys.title')}
          </h2>
          <p className="settings__hint">{t('settings:apiKeys.intro')}</p>
        </div>
        <div className="users__actions">
          {canListAll ? (
            <button
              type="button"
              className={all ? 'settings__button settings__button--primary' : 'settings__button'}
              aria-pressed={all}
              onClick={() => setAll((value) => !value)}
            >
              {t('settings:apiKeys.allToggle')}
            </button>
          ) : null}
          <button
            type="button"
            className="settings__button settings__button--primary"
            onClick={() => setCreating(true)}
          >
            <PlusIcon />
            {t('settings:apiKeys.create')}
          </button>
        </div>
      </div>

      {error !== null ? (
        <p className="settings__message settings__message--error" role="alert">
          {error}
        </p>
      ) : null}

      {keys === null ? (
        <p className="settings__message" role="status">
          {t('settings:apiKeys.loading')}
        </p>
      ) : keys.length === 0 ? (
        <p className="settings__message">{t('settings:apiKeys.empty')}</p>
      ) : (
        <div className="users-table-wrap">
          <table className="users-table">
            <thead>
              <tr>
                <th scope="col">{t('settings:apiKeys.table.name')}</th>
                <th scope="col">{t('settings:apiKeys.table.prefix')}</th>
                <th scope="col">{t('settings:apiKeys.table.access')}</th>
                <th scope="col">{t('settings:apiKeys.table.expires')}</th>
                <th scope="col">{t('settings:apiKeys.table.lastUsed')}</th>
                <th scope="col">{t('settings:apiKeys.table.status')}</th>
                <th scope="col">{t('settings:apiKeys.table.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {keys.map((key) => {
                const status = keyStatus(key, loadedAt)
                return (
                  <tr key={key.id}>
                    <td data-label={t('settings:apiKeys.table.name')}>{key.name}</td>
                    <td data-label={t('settings:apiKeys.table.prefix')}>
                      <code>{key.keyId}</code>
                    </td>
                    <td data-label={t('settings:apiKeys.table.access')}>
                      <div className="users__chips">
                        {key.bindings.map((binding) => (
                          <span className="chip" key={binding.id}>
                            {roleLabel(t, binding.role)} ·{' '}
                            <ScopeLabel scope={binding.scope} groups={groups} servers={servers} />
                          </span>
                        ))}
                      </div>
                    </td>
                    <td data-label={t('settings:apiKeys.table.expires')}>
                      {formatDate(key.expiresAt, language, t('settings:apiKeys.never'))}
                    </td>
                    <td data-label={t('settings:apiKeys.table.lastUsed')}>
                      {formatDate(key.lastUsedAt, language, t('settings:apiKeys.never'))}
                    </td>
                    <td data-label={t('settings:apiKeys.table.status')}>
                      <span className={`chip chip--${status === 'active' ? 'ok' : 'warn'}`}>
                        {t(`settings:apiKeys.status.${status}`)}
                      </span>
                    </td>
                    <td data-label={t('settings:apiKeys.table.actions')}>
                      {!key.revoked ? (
                        <button
                          type="button"
                          className="cbtn cbtn--danger"
                          onClick={() => {
                            setRevokeError(null)
                            setRevoking(key)
                          }}
                        >
                          <TrashIcon />
                          {t('settings:apiKeys.revoke')}
                        </button>
                      ) : null}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {creating ? (
        <CreateKeyDialog
          api={api}
          groups={groups}
          servers={servers}
          onClose={() => setCreating(false)}
          onCreated={() => void load()}
        />
      ) : null}

      {revoking !== null ? (
        <Dialog
          title={t('settings:apiKeys.revokeTitle', { name: revoking.name })}
          onClose={() => setRevoking(null)}
          footer={
            <div className="users__actions">
              <button
                type="button"
                className="settings__button"
                disabled={revokeBusy}
                onClick={() => setRevoking(null)}
              >
                {t('settings:apiKeys.cancel')}
              </button>
              <button
                type="button"
                className="settings__button settings__button--danger"
                disabled={revokeBusy}
                onClick={() => void confirmRevoke()}
              >
                {revokeBusy ? t('settings:apiKeys.revoking') : t('settings:apiKeys.revokeConfirm')}
              </button>
            </div>
          }
        >
          <p>{t('settings:apiKeys.revokeBody')}</p>
          {revokeError !== null ? (
            <p className="settings__message settings__message--error" role="alert">
              {revokeError}
            </p>
          ) : null}
        </Dialog>
      ) : null}
    </section>
  )
}

function CreateKeyDialog({
  api,
  groups,
  servers,
  onClose,
  onCreated,
}: {
  api: APIKeysApi
  groups: Group[]
  servers: ServerSummary[]
  onClose: () => void
  onCreated: () => void
}) {
  const { t, i18n } = useTranslation(['settings', 'users', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [name, setName] = useState('')
  const [days, setDays] = useState(90)
  const [role, setRole] = useState<Role>('viewer')
  const [scope, setScope] = useState<Scope>({ type: 'organization' })
  const [code, setCode] = useState('')
  const [created, setCreated] = useState<CreatedAPIKey | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState<'key' | 'curl' | null>(null)
  const [showErrors, setShowErrors] = useState(false)

  const key = created?.key ?? ''
  const curl =
    created === null
      ? ''
      : `curl -H "Authorization: Bearer <key>" ${window.location.origin}/api/v1/servers`

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setShowErrors(true)
    if (pending) return
    if (name.trim() === '' || code.length !== 6 || !scopeValid(scope)) {
      const form = event.currentTarget
      requestAnimationFrame(() => form.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus())
      return
    }
    setPending(true)
    setError(null)
    try {
      const result = await api.create({
        name: name.trim(),
        expiresInDays: days,
        bindings: [{ role, scope }],
        totpCode: code,
      })
      setCreated(result)
      setCode('')
      onCreated()
    } catch (cause) {
      setCode('')
      setError(errorMessage(t, cause))
    } finally {
      setPending(false)
    }
  }

  const copy = async (value: string, kind: 'key' | 'curl') => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(kind)
    } catch {
      setCopied(null)
    }
  }

  return (
    <Dialog
      title={
        created === null ? t('settings:apiKeys.createTitle') : t('settings:apiKeys.createdTitle')
      }
      onClose={onClose}
      footer={
        <div className="users__actions">
          <button type="button" className="settings__button" onClick={onClose}>
            {created === null ? t('settings:apiKeys.cancel') : t('settings:apiKeys.done')}
          </button>
          {created === null ? (
            <button
              type="submit"
              form="api-key-create-form"
              className="settings__button settings__button--primary"
              disabled={pending}
            >
              {pending ? t('settings:apiKeys.creating') : t('settings:apiKeys.createConfirm')}
            </button>
          ) : null}
        </div>
      }
    >
      {created === null ? (
        <form id="api-key-create-form" noValidate onSubmit={submit}>
          <label className="field">
            <span className="field__label">{t('settings:apiKeys.name')} *</span>
            <input
              className="field__input"
              value={name}
              maxLength={64}
              required
              aria-invalid={showErrors && !name.trim()}
              aria-describedby={showErrors && !name.trim() ? 'api-key-name-error' : undefined}
              onChange={(event) => setName(event.target.value)}
            />
            {showErrors && !name.trim() ? (
              <span className="field__error" id="api-key-name-error">
                {t('validation:required')}
              </span>
            ) : null}
          </label>

          <label className="field">
            <span className="field__label">{t('settings:apiKeys.expiry')}</span>
            <select
              className="field__input"
              value={days}
              onChange={(event) => setDays(Number(event.target.value))}
            >
              {[30, 90, 180, 365].map((option) => (
                <option key={option} value={option}>
                  {t('settings:apiKeys.days', { count: option })}
                </option>
              ))}
            </select>
          </label>

          <label className="field">
            <span className="field__label">{t('settings:apiKeys.access')}</span>
            <RoleSelect value={role} onChange={setRole} />
          </label>
          <RoleDescription role={role} />
          <ScopeFields
            scope={scope}
            onChange={setScope}
            groups={groups}
            servers={servers}
            showErrors={showErrors}
          />

          <p className="settings__notice">{t('settings:apiKeys.limits')}</p>

          <label className="field">
            <span className="field__label">{t('settings:apiKeys.code')} *</span>
            <input
              className="field__input"
              type="text"
              inputMode="numeric"
              autoComplete="one-time-code"
              pattern="[0-9]*"
              maxLength={6}
              required
              value={code}
              aria-invalid={showErrors && code.length !== 6}
              aria-describedby={showErrors && code.length !== 6 ? 'api-key-code-error' : undefined}
              onChange={(event) => setCode(event.target.value.replace(/\D/g, '').slice(0, 6))}
            />
            <span className="settings__hint">{t('settings:apiKeys.codeHint')}</span>
            {showErrors && code.length !== 6 ? (
              <span className="field__error" id="api-key-code-error">
                {t('validation:codeLength')}
              </span>
            ) : null}
          </label>

          {error !== null ? (
            <p className="settings__message settings__message--error" role="alert">
              {error}
            </p>
          ) : null}
        </form>
      ) : (
        <div className="apikeys__once">
          <p className="settings__notice" role="alert">
            {t('settings:apiKeys.once')}
          </p>
          <code className="apikeys__secret">{key}</code>
          <div className="users__actions">
            <button
              type="button"
              className="settings__button"
              onClick={() => void copy(key, 'key')}
            >
              {copied === 'key' ? <CheckIcon /> : <CopyIcon />}
              {copied === 'key' ? t('settings:apiKeys.copied') : t('settings:apiKeys.copy')}
            </button>
          </div>
          <h3 className="settings__section-title">{t('settings:apiKeys.example')}</h3>
          <code className="apikeys__curl">{curl}</code>
          <div className="users__actions">
            <button
              type="button"
              className="settings__button"
              onClick={() => void copy(curl, 'curl')}
            >
              {copied === 'curl' ? <CheckIcon /> : <CopyIcon />}
              {copied === 'curl' ? t('settings:apiKeys.copied') : t('settings:apiKeys.copyCurl')}
            </button>
          </div>
          <p className="settings__hint">
            {t('settings:apiKeys.expires', {
              date: formatDate(created.expiresAt, language, t('settings:apiKeys.never')),
            })}
          </p>
        </div>
      )}
    </Dialog>
  )
}
