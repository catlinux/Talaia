import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { Group } from '../../api/groups'
import type { ServerSummary } from '../../api/servers'
import type { Team, TeamDetail, TeamsApi } from '../../api/teams'
import type { AdminUser, Binding, Role, Scope, UsersApi } from '../../api/users'
import { errorMessage } from '../../auth/AuthGate'
import { TrashIcon } from '../../components/icons'
import {
  ConfirmDialog,
  DataTable,
  Drawer,
  Menu,
  Select,
  useToast,
  type DataTableColumn,
  type MenuEntry,
} from '../../components/ui'
import { Assignments } from './Assignments'

interface TeamsTabProps {
  teams: Team[]
  users: AdminUser[]
  groups: Group[]
  servers: ServerSummary[]
  teamsApi: TeamsApi
  usersApi: UsersApi
  onRefresh: () => Promise<void>
}

type NameDialog = { kind: 'create' } | { kind: 'rename'; team: Team } | null

/** TeamsTab manages teams, members and the team assignments. */
export function TeamsTab({
  teams,
  users,
  groups,
  servers,
  teamsApi,
  usersApi,
  onRefresh,
}: TeamsTabProps) {
  const { t } = useTranslation(['users', 'errors'])
  const toast = useToast()
  const [selected, setSelected] = useState<Team | null>(teams[0] ?? null)
  const [detail, setDetail] = useState<TeamDetail | null>(null)
  const [nameDialog, setNameDialog] = useState<NameDialog>(null)
  const [removing, setRemoving] = useState<Team | null>(null)
  const [member, setMember] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (selected === null) return
    let active = true
    teamsApi.get(selected.id).then(
      (team) => {
        if (active) setDetail(team)
      },
      (cause: unknown) => {
        if (active) setError(errorMessage(t, cause))
      },
    )
    return () => {
      active = false
    }
  }, [selected, teamsApi, t])

  useEffect(() => {
    queueMicrotask(() => {
      if (selected === null && teams.length > 0) setSelected(teams[0] ?? null)
      if (selected !== null && !teams.some((team) => team.id === selected.id)) {
        setSelected(teams[0] ?? null)
      }
    })
  }, [selected, teams])

  const run = async (action: () => Promise<void>, success?: string) => {
    setBusy(true)
    setError(null)
    try {
      await action()
      if (success !== undefined) toast.success(success)
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setBusy(false)
    }
  }

  const reloadDetail = async () => {
    if (selected !== null) setDetail(await teamsApi.get(selected.id))
  }

  const saveName = async (event: FormEvent<HTMLFormElement>, name: string) => {
    event.preventDefault()
    if (nameDialog === null) return
    const dialog = nameDialog
    await run(async () => {
      if (dialog.kind === 'create') {
        const team = await teamsApi.create(name)
        await onRefresh()
        setSelected(team)
        toast.success(t('users:toast.teamCreated', { name }))
      } else {
        await teamsApi.rename(dialog.team.id, name)
        await onRefresh()
        setSelected({ id: dialog.team.id, name })
        toast.success(t('users:toast.teamRenamed', { name }))
      }
    })
    setNameDialog(null)
  }

  const removeTeam = async () => {
    if (removing === null) return
    const team = removing
    await run(async () => {
      await teamsApi.remove(team.id)
      await onRefresh()
      setSelected(null)
      toast.success(t('users:toast.teamDeleted', { name: team.name }))
    })
    setRemoving(null)
  }

  const addMember = async () => {
    if (selected === null || member === '') return
    await run(async () => {
      await teamsApi.addMember(selected.id, member)
      setMember('')
      await reloadDetail()
      toast.success(t('users:toast.memberAdded'))
    })
  }

  const removeMember = async (username: string) => {
    if (selected === null) return
    const user = users.find((candidate) => candidate.username === username)
    if (!user) return
    await run(async () => {
      await teamsApi.removeMember(selected.id, user.id)
      await reloadDetail()
      toast.success(t('users:toast.memberRemoved', { name: username }))
    })
  }

  const addBinding = async (role: Role, scope: Scope) => {
    if (selected === null) return
    await run(async () => {
      await teamsApi.addBinding(selected.id, role, scope)
      await reloadDetail()
      toast.success(t('users:toast.accessUpdated'))
    })
  }

  const removeBinding = async (binding: Binding) => {
    await run(async () => {
      await usersApi.removeBinding(binding.id)
      await reloadDetail()
      toast.success(t('users:toast.accessUpdated'))
    })
  }

  const changeBinding = async (binding: Binding, role: Role) => {
    await run(async () => {
      await usersApi.updateBinding(binding.id, role)
      await reloadDetail()
      toast.success(t('users:toast.accessUpdated'))
    })
  }

  const availableMembers = users.filter(
    (user) => !detail?.members.includes(user.username) && !user.disabled,
  )

  const columns: DataTableColumn<Team>[] = [
    {
      id: 'name',
      header: t('users:teams.name'),
      sortValue: (team) => team.name,
      cell: (team) => <span className="users__name">{team.name}</span>,
    },
    {
      id: 'actions',
      header: t('users:table.actions'),
      align: 'right',
      cell: (team) => {
        const entries: MenuEntry[] = [
          {
            id: 'rename',
            label: t('users:teams.rename'),
            onSelect: () => setNameDialog({ kind: 'rename', team }),
          },
          {
            id: 'delete',
            label: t('users:teams.delete'),
            danger: true,
            onSelect: () => setRemoving(team),
          },
        ]
        return (
          <Menu
            align="end"
            items={entries}
            trigger={(props) => (
              <button
                {...props}
                className="ui-btn ui-btn--ghost"
                aria-label={t('users:rowActions', { name: team.name })}
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
    <div className="teams">
      <div className="users__toolbar">
        <div>
          <h2 className="settings__card-title">{t('users:teams.title')}</h2>
          <p className="users__hint">{t('users:teams.select')}</p>
        </div>
        <button
          type="button"
          className="ui-btn ui-btn--primary"
          onClick={() => setNameDialog({ kind: 'create' })}
        >
          {t('users:teams.create')}
        </button>
      </div>

      {error !== null ? (
        <p className="users__error" role="alert">
          {error}
        </p>
      ) : null}

      <DataTable
        columns={columns}
        rows={teams}
        rowKey={(team) => team.id}
        empty={<p className="users__hint">{t('users:teams.empty')}</p>}
        onRowClick={(team) => setSelected(team)}
        rowClassName={(team) => (team.id === selected?.id ? 'users__row--on' : undefined)}
      />

      <section className="teams__detail">
        {selected === null || detail === null ? (
          <p className="users__hint">{t('users:teams.select')}</p>
        ) : (
          <>
            <h2 className="settings__card-title">{detail.name}</h2>
            <h3 className="settings__section-title">{t('users:teams.members')}</h3>
            <ul className="members">
              {detail.members.length === 0 ? (
                <li className="users__hint">{t('users:teams.noMembers')}</li>
              ) : (
                detail.members.map((username) => (
                  <li className="member" key={username}>
                    <span>{username}</span>
                    <button
                      type="button"
                      className="iconbtn"
                      aria-label={t('users:teams.removeMember', { name: username })}
                      onClick={() => void removeMember(username)}
                    >
                      <TrashIcon />
                    </button>
                  </li>
                ))
              )}
            </ul>
            <div className="users__addMember">
              <Select
                label={t('users:teams.addMember')}
                value={member}
                options={[
                  { value: '', label: t('users:teams.chooseUser') },
                  ...availableMembers.map((user) => ({
                    value: user.id,
                    label: user.displayName || user.username,
                  })),
                ]}
                onChange={setMember}
              />
              <button
                type="button"
                className="ui-btn"
                disabled={member === '' || busy}
                onClick={() => void addMember()}
              >
                {t('users:teams.add')}
              </button>
            </div>

            <h3 className="settings__section-title">{t('users:teams.assignments')}</h3>
            <Assignments
              bindings={detail.bindings}
              groups={groups}
              servers={servers}
              busy={busy}
              error={error}
              onAdd={(role, scope) => void addBinding(role, scope)}
              onRemove={(binding) => void removeBinding(binding)}
              onChangeRole={(binding, role) => void changeBinding(binding, role)}
            />
          </>
        )}
      </section>

      {nameDialog !== null ? (
        <NameDrawer
          title={nameDialog.kind === 'create' ? t('users:teams.create') : t('users:teams.rename')}
          initial={nameDialog.kind === 'rename' ? nameDialog.team.name : ''}
          onClose={() => setNameDialog(null)}
          onSave={(name, event) => void saveName(event, name)}
        />
      ) : null}

      <ConfirmDialog
        open={removing !== null}
        title={t('users:teams.deleteTitle', { name: removing?.name ?? '' })}
        body={<p>{t('users:teams.deleteBody')}</p>}
        confirmLabel={t('users:teams.delete')}
        danger
        busy={busy}
        onConfirm={() => void removeTeam()}
        onCancel={() => setRemoving(null)}
      />
    </div>
  )
}

function NameDrawer({
  title,
  initial,
  onClose,
  onSave,
}: {
  title: string
  initial: string
  onClose: () => void
  onSave: (name: string, event: FormEvent<HTMLFormElement>) => void
}) {
  const { t } = useTranslation('users')
  const [name, setName] = useState(initial)
  const [showError, setShowError] = useState(false)
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setShowError(true)
    if (!name.trim()) {
      event.currentTarget.querySelector('input')?.focus()
      return
    }
    onSave(name.trim(), event)
  }
  return (
    <Drawer
      open
      title={title}
      onClose={onClose}
      footer={
        <div className="users__actions">
          <button type="button" className="ui-btn" onClick={onClose}>
            {t('actions.cancel')}
          </button>
          <button type="submit" form="team-name-form" className="ui-btn ui-btn--primary">
            {t('actions.save')}
          </button>
        </div>
      }
    >
      <form id="team-name-form" noValidate onSubmit={submit}>
        <label className="field">
          <span className="field__label">{t('users:teams.name')} *</span>
          <input
            className="field__input"
            value={name}
            required
            aria-invalid={showError && !name.trim()}
            aria-describedby={showError && !name.trim() ? 'team-name-error' : undefined}
            onChange={(event) => setName(event.target.value)}
          />
          {showError && !name.trim() ? (
            <span className="field__error" id="team-name-error">
              {t('validation:required')}
            </span>
          ) : null}
        </label>
      </form>
    </Drawer>
  )
}
