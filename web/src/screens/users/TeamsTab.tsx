import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { Group } from '../../api/groups'
import type { ServerSummary } from '../../api/servers'
import type { Team, TeamDetail, TeamsApi } from '../../api/teams'
import type { AdminUser, Binding, Role, Scope, UsersApi } from '../../api/users'
import { errorMessage } from '../../auth/AuthGate'
import { TrashIcon } from '../../components/icons'
import { Dialog } from './Dialog'
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

  const run = async (action: () => Promise<void>) => {
    setBusy(true)
    setError(null)
    try {
      await action()
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
    await run(async () => {
      if (nameDialog.kind === 'create') {
        const team = await teamsApi.create(name)
        await onRefresh()
        setSelected(team)
      } else {
        await teamsApi.rename(nameDialog.team.id, name)
        await onRefresh()
        setSelected({ id: nameDialog.team.id, name })
      }
    })
    setNameDialog(null)
  }

  const removeTeam = async () => {
    if (removing === null) return
    await run(async () => {
      await teamsApi.remove(removing.id)
      await onRefresh()
      setSelected(null)
    })
    setRemoving(null)
  }

  const addMember = () => {
    if (selected === null || member === '') return
    void run(async () => {
      await teamsApi.addMember(selected.id, member)
      setMember('')
      await reloadDetail()
    })
  }

  const removeMember = (username: string) => {
    if (selected === null) return
    const user = users.find((candidate) => candidate.username === username)
    if (!user) return
    void run(async () => {
      await teamsApi.removeMember(selected.id, user.id)
      await reloadDetail()
    })
  }

  const addBinding = (role: Role, scope: Scope) => {
    if (selected === null) return
    void run(async () => {
      await teamsApi.addBinding(selected.id, role, scope)
      await reloadDetail()
    })
  }

  const removeBinding = (binding: Binding) => {
    void run(async () => {
      await usersApi.removeBinding(binding.id)
      await reloadDetail()
    })
  }

  const changeBinding = (binding: Binding, role: Role) => {
    void run(async () => {
      await usersApi.updateBinding(binding.id, role)
      await reloadDetail()
    })
  }

  const availableMembers = users.filter(
    (user) => !detail?.members.includes(user.username) && !user.disabled,
  )

  return (
    <div className="teams">
      <aside className="teams__list" aria-label={t('users:teams.title')}>
        <div className="users__toolbar">
          <h2 className="settings__card-title">{t('users:teams.title')}</h2>
          <button
            type="button"
            className="settings__button settings__button--primary"
            onClick={() => setNameDialog({ kind: 'create' })}
          >
            {t('users:teams.create')}
          </button>
        </div>
        {teams.length === 0 ? (
          <p className="users__hint">{t('users:teams.empty')}</p>
        ) : (
          <ul className="teams__names">
            {teams.map((team) => (
              <li key={team.id}>
                <button
                  type="button"
                  className={
                    team.id === selected?.id ? 'teams__name teams__name--on' : 'teams__name'
                  }
                  onClick={() => setSelected(team)}
                >
                  {team.name}
                </button>
                <button
                  type="button"
                  className="cbtn"
                  aria-label={t('users:teams.rename')}
                  onClick={() => setNameDialog({ kind: 'rename', team })}
                >
                  {t('users:teams.renameShort')}
                </button>
                <button
                  type="button"
                  className="iconbtn"
                  aria-label={t('users:teams.delete')}
                  onClick={() => setRemoving(team)}
                >
                  <TrashIcon />
                </button>
              </li>
            ))}
          </ul>
        )}
      </aside>

      <section className="teams__detail">
        {error !== null ? (
          <p className="users__error" role="alert">
            {error}
          </p>
        ) : null}
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
                      onClick={() => removeMember(username)}
                    >
                      <TrashIcon />
                    </button>
                  </li>
                ))
              )}
            </ul>
            <div className="users__addMember">
              <label className="field">
                <span className="field__label">{t('users:teams.addMember')}</span>
                <select
                  className="field__input"
                  value={member}
                  onChange={(event) => setMember(event.target.value)}
                >
                  <option value="">{t('users:teams.chooseUser')}</option>
                  {availableMembers.map((user) => (
                    <option key={user.id} value={user.id}>
                      {user.displayName || user.username}
                    </option>
                  ))}
                </select>
              </label>
              <button
                type="button"
                className="cbtn"
                disabled={member === '' || busy}
                onClick={addMember}
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
              onAdd={addBinding}
              onRemove={removeBinding}
              onChangeRole={changeBinding}
            />
          </>
        )}
      </section>

      {nameDialog !== null ? (
        <NameDialog
          title={nameDialog.kind === 'create' ? t('users:teams.create') : t('users:teams.rename')}
          initial={nameDialog.kind === 'rename' ? nameDialog.team.name : ''}
          onClose={() => setNameDialog(null)}
          onSave={(name, event) => void saveName(event, name)}
        />
      ) : null}

      {removing !== null ? (
        <Dialog
          title={t('users:teams.deleteTitle', { name: removing.name })}
          onClose={() => setRemoving(null)}
          footer={
            <div className="users__actions">
              <button type="button" className="settings__button" onClick={() => setRemoving(null)}>
                {t('actions.cancel')}
              </button>
              <button
                type="button"
                className="settings__button settings__button--danger"
                disabled={busy}
                onClick={() => void removeTeam()}
              >
                {t('users:teams.delete')}
              </button>
            </div>
          }
        >
          <p>{t('users:teams.deleteBody')}</p>
        </Dialog>
      ) : null}
    </div>
  )
}

function NameDialog({
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
  return (
    <Dialog
      title={title}
      onClose={onClose}
      footer={
        <div className="users__actions">
          <button type="button" className="settings__button" onClick={onClose}>
            {t('actions.cancel')}
          </button>
          <button
            type="submit"
            form="team-name-form"
            className="settings__button settings__button--primary"
          >
            {t('actions.save')}
          </button>
        </div>
      }
    >
      <form id="team-name-form" onSubmit={(event) => onSave(name.trim(), event)}>
        <label className="field">
          <span className="field__label">{t('users:teams.name')}</span>
          <input
            className="field__input"
            value={name}
            required
            onChange={(event) => setName(event.target.value)}
          />
        </label>
      </form>
    </Dialog>
  )
}
