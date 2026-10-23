import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { Group } from '../../api/groups'
import type { CreatedInvite, Invite, InvitesApi } from '../../api/invites'
import type { ServerSummary } from '../../api/servers'
import type { AdminUser, Binding, Role, Scope, UsersApi } from '../../api/users'
import { errorMessage } from '../../auth/AuthGate'
import { CheckIcon, CopyIcon } from '../../components/icons'
import { Dialog } from './Dialog'
import { Assignments } from './Assignments'
import { RoleDescription, RoleSelect, ScopeFields, ScopeLabel, roleLabel } from './AccessFields'

interface UsersTabProps {
  users: AdminUser[]
  groups: Group[]
  servers: ServerSummary[]
  invites: Invite[]
  usersApi: UsersApi
  invitesApi: InvitesApi
  onRefresh: () => Promise<void>
}

interface EditState {
  user: AdminUser
  displayName: string
  email: string
}

/** UsersTab lists accounts, manages access and creates invitations. */
export function UsersTab({
  users,
  groups,
  servers,
  invites,
  usersApi,
  invitesApi,
  onRefresh,
}: UsersTabProps) {
  const { t, i18n } = useTranslation(['users', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [accessFor, setAccessFor] = useState<string | null>(null)
  const [editing, setEditing] = useState<EditState | null>(null)
  const [toggling, setToggling] = useState<AdminUser | null>(null)
  const [inviting, setInviting] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [editErrors, setEditErrors] = useState(false)

  const selected = accessFor === null ? null : (users.find((user) => user.id === accessFor) ?? null)

  const run = async (action: () => Promise<void>) => {
    setBusy(true)
    setError(null)
    try {
      await action()
      await onRefresh()
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setBusy(false)
    }
  }

  const saveUser = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (editing === null) return
    setEditErrors(true)
    if (!editing.email.trim()) {
      const form = event.currentTarget
      requestAnimationFrame(() => form.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus())
      return
    }
    await run(() => usersApi.update(editing.user.id, editing.displayName, editing.email))
    setEditing(null)
  }

  const toggleUser = async () => {
    if (toggling === null) return
    await run(() =>
      toggling.disabled ? usersApi.enable(toggling.id) : usersApi.disable(toggling.id),
    )
    setToggling(null)
  }

  const addAccess = (role: Role, scope: Scope) => {
    if (selected === null) return
    void run(() => usersApi.addBinding(selected.id, role, scope).then(() => undefined))
  }

  const removeAccess = (binding: Binding) => {
    void run(() => usersApi.removeBinding(binding.id))
  }

  const changeAccess = (binding: Binding, role: Role) => {
    void run(() => usersApi.updateBinding(binding.id, role))
  }

  const revoke = (invite: Invite) => {
    void run(() => invitesApi.revoke(invite.id))
  }

  return (
    <div className="users-tab">
      <div className="users__toolbar">
        <div>
          <h2 className="settings__card-title">{t('users:title')}</h2>
          <p className="users__hint">{t('users:intro')}</p>
        </div>
        <button
          type="button"
          className="settings__button settings__button--primary"
          onClick={() => setInviting(true)}
        >
          {t('users:invite.open')}
        </button>
      </div>

      {error !== null ? (
        <p className="users__error" role="alert">
          {error}
        </p>
      ) : null}

      {users.length === 0 ? (
        <p className="users__hint">{t('users:empty')}</p>
      ) : (
        <div className="users-table-wrap">
          <table className="users-table">
            <thead>
              <tr>
                <th scope="col">{t('users:table.name')}</th>
                <th scope="col">{t('users:table.username')}</th>
                <th scope="col">{t('users:table.email')}</th>
                <th scope="col">{t('users:table.access')}</th>
                <th scope="col">{t('users:table.status')}</th>
                <th scope="col">{t('users:table.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {users.map((user) => (
                <tr key={user.id}>
                  <td data-label={t('users:table.name')}>
                    <button
                      type="button"
                      className="users__name"
                      onClick={() => setAccessFor(user.id)}
                    >
                      {user.displayName || user.username}
                    </button>
                  </td>
                  <td data-label={t('users:table.username')}>
                    <code>{user.username}</code>
                  </td>
                  <td data-label={t('users:table.email')}>
                    <span className="users__email">{user.email}</span>
                  </td>
                  <td data-label={t('users:table.access')}>
                    <div className="users__chips">
                      {user.bindings.length === 0 ? (
                        <span className="users__hint">{t('users:noAccess')}</span>
                      ) : (
                        user.bindings.map((binding) => (
                          <span className="chip" key={binding.id}>
                            {roleLabel(t, binding.role)} ·{' '}
                            <ScopeLabel scope={binding.scope} groups={groups} servers={servers} />
                          </span>
                        ))
                      )}
                    </div>
                  </td>
                  <td data-label={t('users:table.status')}>
                    <span
                      className={user.disabled ? 'status status--offline' : 'status status--online'}
                    >
                      {user.disabled ? t('users:disabled') : t('users:active')}
                    </span>
                    <span className={user.totpEnabled ? 'chip chip--ok' : 'chip chip--warn'}>
                      {user.totpEnabled ? t('users:totpOn') : t('users:totpOff')}
                    </span>
                  </td>
                  <td data-label={t('users:table.actions')}>
                    <div className="users__actions">
                      <button type="button" className="cbtn" onClick={() => setAccessFor(user.id)}>
                        {t('users:accessButton')}
                      </button>
                      <button
                        type="button"
                        className="cbtn"
                        onClick={() =>
                          setEditing({ user, displayName: user.displayName, email: user.email })
                        }
                      >
                        {t('users:edit')}
                      </button>
                      <button
                        type="button"
                        className={user.disabled ? 'cbtn' : 'cbtn cbtn--danger'}
                        onClick={() => {
                          setError(null)
                          setToggling(user)
                        }}
                      >
                        {user.disabled ? t('users:enable') : t('users:disable')}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <section className="users__invites" aria-labelledby="pending-invites-title">
        <h3 className="settings__section-title" id="pending-invites-title">
          {t('users:invites.title')}
        </h3>
        {invites.length === 0 ? (
          <p className="users__hint">{t('users:invites.empty')}</p>
        ) : (
          <ul className="invites">
            {invites.map((invite) => (
              <li className="invite" key={invite.id}>
                <div>
                  <strong>{invite.username}</strong>
                  <span className="users__hint">
                    {roleLabel(t, invite.role)} ·{' '}
                    <ScopeLabel scope={invite.scope} groups={groups} servers={servers} />
                  </span>
                </div>
                <span className="users__hint">
                  {t('users:invites.expires', {
                    date: new Intl.DateTimeFormat(language, {
                      dateStyle: 'medium',
                      timeStyle: 'short',
                    }).format(new Date(invite.expiresAt)),
                  })}
                </span>
                <button type="button" className="cbtn cbtn--danger" onClick={() => revoke(invite)}>
                  {t('users:invites.revoke')}
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      {selected !== null ? (
        <Dialog
          title={t('users:access.title', { name: selected.displayName || selected.username })}
          onClose={() => setAccessFor(null)}
          side
        >
          <Assignments
            bindings={selected.bindings}
            groups={groups}
            servers={servers}
            busy={busy}
            error={error}
            onAdd={addAccess}
            onRemove={removeAccess}
            onChangeRole={changeAccess}
          />
        </Dialog>
      ) : null}

      {editing !== null ? (
        <Dialog
          title={t('users:editDialog.title')}
          onClose={() => setEditing(null)}
          footer={
            <div className="users__actions">
              <button type="button" className="settings__button" onClick={() => setEditing(null)}>
                {t('actions.cancel')}
              </button>
              <button
                type="submit"
                form="edit-user-form"
                className="settings__button settings__button--primary"
                disabled={busy}
              >
                {t('actions.save')}
              </button>
            </div>
          }
        >
          <form id="edit-user-form" noValidate onSubmit={saveUser}>
            <label className="field">
              <span className="field__label">{t('users:editDialog.name')}</span>
              <input
                className="field__input"
                value={editing.displayName}
                onChange={(event) => setEditing({ ...editing, displayName: event.target.value })}
              />
            </label>
            <label className="field">
              <span className="field__label">{t('users:editDialog.email')} *</span>
              <input
                className="field__input"
                type="email"
                required
                value={editing.email}
                aria-invalid={editErrors && !editing.email.trim()}
                aria-describedby={
                  editErrors && !editing.email.trim() ? 'edit-email-error' : undefined
                }
                onChange={(event) => setEditing({ ...editing, email: event.target.value })}
              />
              {editErrors && !editing.email.trim() ? (
                <span className="field__error" id="edit-email-error">
                  {t('validation:required')}
                </span>
              ) : null}
            </label>
          </form>
        </Dialog>
      ) : null}

      {toggling !== null ? (
        <Dialog
          title={
            toggling.disabled
              ? t('users:toggle.enableTitle', { name: toggling.username })
              : t('users:toggle.disableTitle', { name: toggling.username })
          }
          onClose={() => setToggling(null)}
          footer={
            <div className="users__actions">
              <button type="button" className="settings__button" onClick={() => setToggling(null)}>
                {t('actions.cancel')}
              </button>
              <button
                type="button"
                className={
                  toggling.disabled
                    ? 'settings__button settings__button--primary'
                    : 'settings__button settings__button--danger'
                }
                disabled={busy}
                onClick={() => void toggleUser()}
              >
                {toggling.disabled ? t('users:enable') : t('users:disable')}
              </button>
            </div>
          }
        >
          <p>{t(toggling.disabled ? 'users:toggle.enableBody' : 'users:toggle.disableBody')}</p>
        </Dialog>
      ) : null}

      {inviting ? (
        <InviteDialog
          api={invitesApi}
          groups={groups}
          servers={servers}
          onClose={() => setInviting(false)}
          onCreated={() => void onRefresh()}
        />
      ) : null}
    </div>
  )
}

function InviteDialog({
  api,
  groups,
  servers,
  onClose,
  onCreated,
}: {
  api: InvitesApi
  groups: Group[]
  servers: ServerSummary[]
  onClose: () => void
  onCreated: () => void
}) {
  const { t, i18n } = useTranslation(['users', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [username, setUsername] = useState('')
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<Role>('viewer')
  const [scope, setScope] = useState<Scope>({ type: 'organization' })
  const [created, setCreated] = useState<CreatedInvite | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [showErrors, setShowErrors] = useState(false)

  const link = created === null ? '' : `${window.location.origin}${created.linkPath}`

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setShowErrors(true)
    const invalid =
      !username.trim() ||
      !email.trim() ||
      ((scope.type === 'stack' || scope.type === 'container') && !scope.name?.trim())
    if (invalid) {
      const form = event.currentTarget
      requestAnimationFrame(() => form.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus())
      return
    }
    setPending(true)
    setError(null)
    try {
      const result = await api.create({
        username: username.trim(),
        email: email.trim(),
        role,
        scope,
      })
      setCreated(result)
      onCreated()
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setPending(false)
    }
  }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <Dialog
      title={t('users:invite.title')}
      onClose={onClose}
      footer={
        <div className="users__actions">
          <button type="button" className="settings__button" onClick={onClose}>
            {created ? t('actions.done') : t('actions.cancel')}
          </button>
          {created === null ? (
            <button
              type="submit"
              form="invite-user-form"
              className="settings__button settings__button--primary"
              disabled={pending}
            >
              {pending ? t('users:invite.creating') : t('users:invite.create')}
            </button>
          ) : null}
        </div>
      }
    >
      {created === null ? (
        <form id="invite-user-form" noValidate onSubmit={submit}>
          <label className="field">
            <span className="field__label">{t('users:invite.username')} *</span>
            <input
              className="field__input"
              value={username}
              required
              aria-required="true"
              aria-invalid={showErrors && !username.trim()}
              aria-describedby={
                showErrors && !username.trim() ? 'invite-username-error' : undefined
              }
              onChange={(event) => setUsername(event.target.value)}
            />
            {showErrors && !username.trim() ? (
              <span className="field__error" id="invite-username-error">
                {t('validation:required')}
              </span>
            ) : null}
          </label>
          <label className="field">
            <span className="field__label">{t('users:invite.email')} *</span>
            <input
              className="field__input"
              type="email"
              required
              aria-required="true"
              aria-invalid={showErrors && !email.trim()}
              aria-describedby={showErrors && !email.trim() ? 'invite-email-error' : undefined}
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
            {showErrors && !email.trim() ? (
              <span className="field__error" id="invite-email-error">
                {t('validation:required')}
              </span>
            ) : null}
          </label>
          <label className="field">
            <span className="field__label">{t('access.role')}</span>
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
          {error !== null ? (
            <p className="users__error" role="alert">
              {error}
            </p>
          ) : null}
        </form>
      ) : (
        <div>
          <p className="users__hint">{t('users:invite.once')}</p>
          <code className="users__link">{link}</code>
          <div className="users__actions">
            <button type="button" className="settings__button" onClick={() => void copy()}>
              {copied ? <CheckIcon /> : <CopyIcon />}
              {copied ? t('users:invite.copied') : t('users:invite.copy')}
            </button>
          </div>
          <p className="users__hint">
            {t('users:invite.expires', {
              date: new Intl.DateTimeFormat(language, {
                dateStyle: 'medium',
                timeStyle: 'short',
              }).format(new Date(created.expiresAt)),
            })}
          </p>
        </div>
      )}
    </Dialog>
  )
}
