import { useState, type FormEvent } from 'react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'

import type { Group } from '../../api/groups'
import type { CreatedInvite, Invite, InvitesApi } from '../../api/invites'
import type { ServerSummary } from '../../api/servers'
import type { AdminUser, Binding, Role, Scope, UsersApi } from '../../api/users'
import { errorMessage, useAuth } from '../../auth/AuthGate'
import { CheckIcon, CopyIcon, PlusIcon } from '../../components/icons'
import {
  ConfirmDialog,
  DataTable,
  Drawer,
  FilterBar,
  Menu,
  Select,
  StatusBadge,
  useToast,
  type DataTableColumn,
  type MenuEntry,
} from '../../components/ui'
import { setQuery, useRoute } from '../../router'
import { Assignments } from './Assignments'
import {
  RoleDescription,
  RoleSelect,
  ScopeFields,
  ScopeLabel,
  roleLabel,
  roles,
} from './AccessFields'

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

function matchesQuery(user: AdminUser, query: string, t: TFunction): boolean {
  const needle = query.trim().toLowerCase()
  if (needle === '') return true
  return [
    user.displayName,
    user.username,
    user.email,
    ...user.bindings.map((binding) => roleLabel(t, binding.role)),
  ].some((value) => value.toLowerCase().includes(needle))
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
  const auth = useAuth()
  const toast = useToast()
  const { query } = useRoute()
  const language = i18n.resolvedLanguage ?? i18n.language
  const [accessFor, setAccessFor] = useState<string | null>(null)
  const [editing, setEditing] = useState<EditState | null>(null)
  const [toggling, setToggling] = useState<AdminUser | null>(null)
  const [deleting, setDeleting] = useState<AdminUser | null>(null)
  const [inviting, setInviting] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [editErrors, setEditErrors] = useState(false)

  const search = query.get('q') ?? ''
  const roleFilter = roles.includes((query.get('role') ?? '') as Role)
    ? (query.get('role') as Role)
    : ''
  const selected = accessFor === null ? null : (users.find((user) => user.id === accessFor) ?? null)
  const visibleUsers = users.filter(
    (user) =>
      matchesQuery(user, search, t) &&
      (roleFilter === '' || user.bindings.some((binding) => binding.role === roleFilter)),
  )

  const run = async (action: () => Promise<void>, success?: string): Promise<boolean> => {
    setBusy(true)
    setError(null)
    try {
      await action()
      await onRefresh()
      if (success !== undefined) toast.success(success)
      return true
    } catch (cause) {
      setError(errorMessage(t, cause))
      return false
    } finally {
      setBusy(false)
    }
  }

  const saveUser = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (editing === null) return
    setEditErrors(true)
    if (editing.email.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(editing.email.trim())) {
      const form = event.currentTarget
      requestAnimationFrame(() => form.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus())
      return
    }
    const saved = await run(
      () => usersApi.update(editing.user.id, editing.displayName, editing.email.trim()),
      t('users:toast.updated'),
    )
    if (saved) setEditing(null)
  }

  const toggleUser = async () => {
    if (toggling === null) return
    const user = toggling
    const saved = await run(
      () => (user.disabled ? usersApi.enable(user.id) : usersApi.disable(user.id)),
      t(user.disabled ? 'users:toast.enabled' : 'users:toast.disabled', { name: user.username }),
    )
    if (saved) setToggling(null)
  }

  const deleteUser = async () => {
    if (deleting === null) return
    const user = deleting
    const deleted = await run(
      () => usersApi.delete(user.id, user.username),
      t('users:toast.deleted', { name: user.username }),
    )
    if (deleted) setDeleting(null)
  }

  const addAccess = async (role: Role, scope: Scope) => {
    if (selected === null) return
    await run(
      () => usersApi.addBinding(selected.id, role, scope).then(() => undefined),
      t('users:toast.accessUpdated'),
    )
  }

  const removeAccess = async (binding: Binding) => {
    await run(() => usersApi.removeBinding(binding.id), t('users:toast.accessUpdated'))
  }

  const changeAccess = async (binding: Binding, role: Role) => {
    await run(() => usersApi.updateBinding(binding.id, role), t('users:toast.accessUpdated'))
  }

  const revoke = async (invite: Invite) => {
    await run(() => invitesApi.revoke(invite.id), t('users:toast.inviteRevoked'))
  }

  const columns: DataTableColumn<AdminUser>[] = [
    {
      id: 'name',
      header: t('users:table.name'),
      sortValue: (user) => user.displayName || user.username,
      cell: (user) => (
        <button type="button" className="users__name" onClick={() => setAccessFor(user.id)}>
          {user.displayName || user.username}
        </button>
      ),
    },
    {
      id: 'username',
      header: t('users:table.username'),
      sortValue: (user) => user.username,
      cell: (user) => <code>{user.username}</code>,
    },
    {
      id: 'email',
      header: t('users:table.email'),
      sortValue: (user) => user.email,
      cell: (user) => <span className="users__email">{user.email}</span>,
    },
    {
      id: 'access',
      header: t('users:table.access'),
      cell: (user) => (
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
      ),
    },
    {
      id: 'totp',
      header: t('users:table.totp'),
      cell: (user) => (
        <span className={user.totpEnabled ? 'chip chip--ok' : 'chip chip--warn'}>
          {user.totpEnabled ? t('users:totpOn') : t('users:totpOff')}
        </span>
      ),
    },
    {
      id: 'status',
      header: t('users:table.status'),
      sortValue: (user) => (user.disabled ? 1 : 0),
      cell: (user) => (
        <StatusBadge
          status={user.disabled ? 'offline' : 'ok'}
          label={user.disabled ? t('users:disabled') : t('users:active')}
        />
      ),
    },
    {
      id: 'actions',
      header: t('users:table.actions'),
      align: 'right',
      cell: (user) => {
        const entries: MenuEntry[] = [
          {
            id: 'access',
            label: t('users:accessButton'),
            onSelect: () => setAccessFor(user.id),
          },
          {
            id: 'edit',
            label: t('users:edit'),
            onSelect: () => setEditing({ user, displayName: user.displayName, email: user.email }),
          },
          {
            id: 'toggle',
            label: user.disabled ? t('users:enable') : t('users:disable'),
            danger: !user.disabled,
            onSelect: () => {
              setError(null)
              setToggling(user)
            },
          },
        ]
        if (user.disabled && user.username !== auth?.username) {
          entries.push({
            id: 'delete',
            label: t('users:delete'),
            danger: true,
            onSelect: () => {
              setError(null)
              setDeleting(user)
            },
          })
        }
        return (
          <Menu
            align="end"
            items={entries}
            trigger={(props) => (
              <button
                {...props}
                className="ui-btn ui-btn--ghost"
                aria-label={t('users:rowActions', { name: user.username })}
              >
                {t('users:table.actions')}
              </button>
            )}
          />
        )
      },
    },
  ]

  return (
    <div className="users-tab">
      <div className="users__toolbar">
        <div>
          <h2 className="settings__card-title">{t('users:title')}</h2>
          <p className="users__hint">{t('users:intro')}</p>
        </div>
        <button type="button" className="ui-btn ui-btn--primary" onClick={() => setInviting(true)}>
          <PlusIcon />
          {t('users:invite.open')}
        </button>
      </div>

      {error !== null && toggling === null && deleting === null ? (
        <p className="users__error" role="alert">
          {error}
        </p>
      ) : null}

      <FilterBar
        search={{
          value: search,
          onChange: (value) => setQuery({ q: value }),
          placeholder: t('users:filters.search'),
        }}
      >
        <Select
          label={t('users:filters.role')}
          value={roleFilter}
          options={[
            { value: '', label: t('users:filters.allRoles') },
            ...roles.map((role) => ({ value: role, label: roleLabel(t, role) })),
          ]}
          onChange={(value) => setQuery({ role: value === '' ? null : value })}
        />
      </FilterBar>

      <DataTable
        columns={columns}
        rows={visibleUsers}
        rowKey={(user) => user.id}
        empty={
          <p className="users__hint">
            {t(search === '' && roleFilter === '' ? 'users:empty' : 'users:noResults')}
          </p>
        }
      />

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
                <button
                  type="button"
                  className="ui-btn ui-btn--ghost"
                  onClick={() => void revoke(invite)}
                >
                  {t('users:invites.revoke')}
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      {selected !== null ? (
        <Drawer
          open
          title={t('users:access.title', { name: selected.displayName || selected.username })}
          onClose={() => {
            setAccessFor(null)
            setError(null)
          }}
        >
          <Assignments
            bindings={selected.bindings}
            groups={groups}
            servers={servers}
            busy={busy}
            error={error}
            onAdd={(role, scope) => void addAccess(role, scope)}
            onRemove={(binding) => void removeAccess(binding)}
            onChangeRole={(binding, role) => void changeAccess(binding, role)}
          />
        </Drawer>
      ) : null}

      {editing !== null ? (
        <Drawer
          open
          title={t('users:editDialog.title')}
          onClose={() => setEditing(null)}
          footer={
            <div className="users__actions">
              <button type="button" className="ui-btn" onClick={() => setEditing(null)}>
                {t('actions.cancel')}
              </button>
              <button
                type="submit"
                form="edit-user-form"
                className="ui-btn ui-btn--primary"
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
              <span className="field__label">{t('users:editDialog.email')}</span>
              <input
                className="field__input"
                type="email"
                value={editing.email}
                aria-invalid={
                  editErrors &&
                  !!editing.email.trim() &&
                  !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(editing.email.trim())
                }
                aria-describedby={
                  editErrors &&
                  !!editing.email.trim() &&
                  !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(editing.email.trim())
                    ? 'edit-email-error'
                    : undefined
                }
                onChange={(event) => setEditing({ ...editing, email: event.target.value })}
              />
              {editErrors &&
              !!editing.email.trim() &&
              !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(editing.email.trim()) ? (
                <span className="field__error" id="edit-email-error">
                  {t('validation:emailInvalid')}
                </span>
              ) : null}
            </label>
          </form>
        </Drawer>
      ) : null}

      <ConfirmDialog
        open={toggling !== null}
        title={
          toggling?.disabled
            ? t('users:toggle.enableTitle', { name: toggling.username })
            : t('users:toggle.disableTitle', { name: toggling?.username ?? '' })
        }
        body={
          <>
            <p>{t(toggling?.disabled ? 'users:toggle.enableBody' : 'users:toggle.disableBody')}</p>
            {error !== null ? <p role="alert">{error}</p> : null}
          </>
        }
        confirmLabel={toggling?.disabled ? t('users:enable') : t('users:disable')}
        danger={!toggling?.disabled}
        busy={busy}
        onConfirm={() => void toggleUser()}
        onCancel={() => {
          setToggling(null)
          setError(null)
        }}
      />

      <ConfirmDialog
        open={deleting !== null}
        title={t('users:deleteDialog.title', { name: deleting?.username ?? '' })}
        body={
          <>
            <p>{t('users:deleteDialog.body')}</p>
            {error !== null ? <p role="alert">{error}</p> : null}
          </>
        }
        confirmLabel={t('users:delete')}
        danger
        busy={busy}
        requireText={deleting?.username ?? ''}
        onConfirm={() => void deleteUser()}
        onCancel={() => {
          setDeleting(null)
          setError(null)
        }}
      />

      {inviting ? (
        <InviteDrawer
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

function InviteDrawer({
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
  const toast = useToast()
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
      (!!email.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) ||
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
      toast.success(t('users:toast.invited', { name: username.trim() }))
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
    <Drawer
      open
      title={t('users:invite.title')}
      onClose={onClose}
      footer={
        <div className="users__actions">
          <button type="button" className="ui-btn" onClick={onClose}>
            {created ? t('actions.done') : t('actions.cancel')}
          </button>
          {created === null ? (
            <button
              type="submit"
              form="invite-user-form"
              className="ui-btn ui-btn--primary"
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
            <span className="field__label">{t('users:invite.email')}</span>
            <input
              className="field__input"
              type="email"
              aria-invalid={
                showErrors && !!email.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())
              }
              aria-describedby={
                showErrors && !!email.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())
                  ? 'invite-email-error'
                  : undefined
              }
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
            {showErrors && !!email.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim()) ? (
              <span className="field__error" id="invite-email-error">
                {t('validation:emailInvalid')}
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
            <button type="button" className="ui-btn" onClick={() => void copy()}>
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
    </Drawer>
  )
}
