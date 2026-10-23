import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { Group, GroupsApi } from '../../api/groups'
import type { ServerSummary } from '../../api/servers'
import { errorMessage } from '../../auth/AuthGate'
import { TrashIcon } from '../../components/icons'
import { Dialog } from './Dialog'

interface GroupsTabProps {
  groups: Group[]
  servers: ServerSummary[]
  groupsApi: GroupsApi
  onRefresh: () => Promise<void>
}

type GroupDialog = { kind: 'create' } | { kind: 'rename'; group: Group } | null

/** GroupsTab manages server groups and which group a server belongs to. */
export function GroupsTab({ groups, servers, groupsApi, onRefresh }: GroupsTabProps) {
  const { t } = useTranslation(['users', 'errors'])
  const [dialog, setDialog] = useState<GroupDialog>(null)
  const [removing, setRemoving] = useState<Group | null>(null)
  const [server, setServer] = useState(servers[0]?.id ?? '')
  const [group, setGroup] = useState(groups[0]?.id ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const activeServer = servers.some((candidate) => candidate.id === server)
    ? server
    : (servers[0]?.id ?? '')
  const activeGroup = groups.some((candidate) => candidate.id === group)
    ? group
    : (groups[0]?.id ?? '')

  const run = async (action: () => Promise<void>, success?: string) => {
    setBusy(true)
    setError(null)
    setNotice(null)
    try {
      await action()
      await onRefresh()
      if (success) setNotice(success)
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setBusy(false)
    }
  }

  const save = async (event: FormEvent<HTMLFormElement>, name: string) => {
    event.preventDefault()
    if (dialog === null) return
    await run(async () => {
      if (dialog.kind === 'create') await groupsApi.create(name)
      else await groupsApi.rename(dialog.group.id, name)
    })
    setDialog(null)
  }

  const assign = () => {
    if (activeServer === '' || activeGroup === '') return
    void run(() => groupsApi.setServerGroup(activeServer, activeGroup), t('users:groups.assigned'))
  }

  const unassign = () => {
    if (activeServer === '') return
    void run(() => groupsApi.setServerGroup(activeServer, null), t('users:groups.unassigned'))
  }

  return (
    <div className="groups">
      <div className="users__toolbar">
        <div>
          <h2 className="settings__card-title">{t('users:groups.title')}</h2>
          <p className="users__hint">{t('users:groups.intro')}</p>
        </div>
        <button
          type="button"
          className="settings__button settings__button--primary"
          onClick={() => setDialog({ kind: 'create' })}
        >
          {t('users:groups.create')}
        </button>
      </div>

      {error !== null ? (
        <p className="users__error" role="alert">
          {error}
        </p>
      ) : null}
      {notice !== null ? (
        <p className="users__notice" role="status">
          {notice}
        </p>
      ) : null}

      {groups.length === 0 ? (
        <p className="users__hint">{t('users:groups.empty')}</p>
      ) : (
        <ul className="groups__list">
          {groups.map((candidate) => (
            <li className="group" key={candidate.id}>
              <span className="group__name">{candidate.name}</span>
              <div className="users__actions">
                <button
                  type="button"
                  className="cbtn"
                  onClick={() => setDialog({ kind: 'rename', group: candidate })}
                >
                  {t('users:groups.rename')}
                </button>
                <button
                  type="button"
                  className="cbtn cbtn--danger"
                  onClick={() => setRemoving(candidate)}
                >
                  <TrashIcon />
                  {t('users:groups.delete')}
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}

      <section className="users__card" aria-labelledby="group-assign-title">
        <h3 className="settings__section-title" id="group-assign-title">
          {t('users:groups.assignTitle')}
        </h3>
        <p className="users__hint">{t('users:groups.assignIntro')}</p>
        <div className="users__assign">
          <label className="field">
            <span className="field__label">{t('users:groups.server')}</span>
            <select
              className="field__input"
              value={activeServer}
              onChange={(event) => setServer(event.target.value)}
            >
              {servers.map((candidate) => (
                <option key={candidate.id} value={candidate.id}>
                  {candidate.name}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span className="field__label">{t('users:groups.group')}</span>
            <select
              className="field__input"
              value={activeGroup}
              onChange={(event) => setGroup(event.target.value)}
            >
              {groups.map((candidate) => (
                <option key={candidate.id} value={candidate.id}>
                  {candidate.name}
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            className="settings__button settings__button--primary"
            disabled={busy || activeServer === '' || activeGroup === ''}
            onClick={assign}
          >
            {t('users:groups.assign')}
          </button>
          <button
            type="button"
            className="settings__button"
            disabled={busy || activeServer === ''}
            onClick={unassign}
          >
            {t('users:groups.unassign')}
          </button>
        </div>
      </section>

      {dialog !== null ? (
        <GroupNameDialog
          title={dialog.kind === 'create' ? t('users:groups.create') : t('users:groups.rename')}
          initial={dialog.kind === 'rename' ? dialog.group.name : ''}
          onClose={() => setDialog(null)}
          onSave={(name, event) => void save(event, name)}
        />
      ) : null}

      {removing !== null ? (
        <Dialog
          title={t('users:groups.deleteTitle', { name: removing.name })}
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
                onClick={() => {
                  void run(() => groupsApi.remove(removing.id))
                  setRemoving(null)
                }}
              >
                {t('users:groups.delete')}
              </button>
            </div>
          }
        >
          <p>{t('users:groups.deleteBody')}</p>
        </Dialog>
      ) : null}
    </div>
  )
}

function GroupNameDialog({
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
            form="group-name-form"
            className="settings__button settings__button--primary"
          >
            {t('actions.save')}
          </button>
        </div>
      }
    >
      <form id="group-name-form" onSubmit={(event) => onSave(name.trim(), event)}>
        <label className="field">
          <span className="field__label">{t('users:groups.name')}</span>
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
