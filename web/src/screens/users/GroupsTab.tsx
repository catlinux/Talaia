import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { Group, GroupsApi } from '../../api/groups'
import type { ServerSummary } from '../../api/servers'
import { errorMessage } from '../../auth/AuthGate'
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
  const toast = useToast()
  const [dialog, setDialog] = useState<GroupDialog>(null)
  const [removing, setRemoving] = useState<Group | null>(null)
  const [server, setServer] = useState(servers[0]?.id ?? '')
  const [group, setGroup] = useState(groups[0]?.id ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const activeServer = servers.some((candidate) => candidate.id === server)
    ? server
    : (servers[0]?.id ?? '')
  const activeGroup = groups.some((candidate) => candidate.id === group)
    ? group
    : (groups[0]?.id ?? '')

  const run = async (action: () => Promise<void>, success?: string) => {
    setBusy(true)
    setError(null)
    try {
      await action()
      await onRefresh()
      if (success !== undefined) toast.success(success)
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setBusy(false)
    }
  }

  const save = async (event: FormEvent<HTMLFormElement>, name: string) => {
    event.preventDefault()
    if (dialog === null) return
    const current = dialog
    await run(async () => {
      if (current.kind === 'create') {
        await groupsApi.create(name)
        toast.success(t('users:toast.groupCreated', { name }))
      } else {
        await groupsApi.rename(current.group.id, name)
        toast.success(t('users:toast.groupRenamed', { name }))
      }
    })
    setDialog(null)
  }

  const assign = async () => {
    if (activeServer === '' || activeGroup === '') return
    await run(() => groupsApi.setServerGroup(activeServer, activeGroup), t('users:groups.assigned'))
  }

  const unassign = async () => {
    if (activeServer === '') return
    await run(() => groupsApi.setServerGroup(activeServer, null), t('users:groups.unassigned'))
  }

  const remove = async () => {
    if (removing === null) return
    const target = removing
    await run(async () => {
      await groupsApi.remove(target.id)
      toast.success(t('users:toast.groupDeleted', { name: target.name }))
    })
    setRemoving(null)
  }

  const columns: DataTableColumn<Group>[] = [
    {
      id: 'name',
      header: t('users:groups.name'),
      sortValue: (candidate) => candidate.name,
      cell: (candidate) => <span className="group__name">{candidate.name}</span>,
    },
    {
      id: 'actions',
      header: t('users:table.actions'),
      align: 'right',
      cell: (candidate) => {
        const entries: MenuEntry[] = [
          {
            id: 'rename',
            label: t('users:groups.rename'),
            onSelect: () => setDialog({ kind: 'rename', group: candidate }),
          },
          {
            id: 'delete',
            label: t('users:groups.delete'),
            danger: true,
            onSelect: () => setRemoving(candidate),
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
                aria-label={t('users:rowActions', { name: candidate.name })}
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
    <div className="groups">
      <div className="users__toolbar">
        <div>
          <h2 className="settings__card-title">{t('users:groups.title')}</h2>
          <p className="users__hint">{t('users:groups.intro')}</p>
        </div>
        <button
          type="button"
          className="ui-btn ui-btn--primary"
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

      <DataTable
        columns={columns}
        rows={groups}
        rowKey={(candidate) => candidate.id}
        empty={<p className="users__hint">{t('users:groups.empty')}</p>}
      />

      <section className="users__card" aria-labelledby="group-assign-title">
        <h3 className="settings__section-title" id="group-assign-title">
          {t('users:groups.assignTitle')}
        </h3>
        <p className="users__hint">{t('users:groups.assignIntro')}</p>
        <div className="users__assign">
          <Select
            label={t('users:groups.server')}
            value={activeServer}
            options={servers.map((candidate) => ({
              value: candidate.id,
              label: candidate.name,
            }))}
            onChange={setServer}
          />
          <Select
            label={t('users:groups.group')}
            value={activeGroup}
            options={groups.map((candidate) => ({
              value: candidate.id,
              label: candidate.name,
            }))}
            onChange={setGroup}
          />
          <button
            type="button"
            className="ui-btn ui-btn--primary"
            disabled={busy || activeServer === '' || activeGroup === ''}
            onClick={() => void assign()}
          >
            {t('users:groups.assign')}
          </button>
          <button
            type="button"
            className="ui-btn"
            disabled={busy || activeServer === ''}
            onClick={() => void unassign()}
          >
            {t('users:groups.unassign')}
          </button>
        </div>
      </section>

      {dialog !== null ? (
        <NameDrawer
          title={dialog.kind === 'create' ? t('users:groups.create') : t('users:groups.rename')}
          initial={dialog.kind === 'rename' ? dialog.group.name : ''}
          onClose={() => setDialog(null)}
          onSave={(name, event) => void save(event, name)}
        />
      ) : null}

      <ConfirmDialog
        open={removing !== null}
        title={t('users:groups.deleteTitle', { name: removing?.name ?? '' })}
        body={<p>{t('users:groups.deleteBody')}</p>}
        confirmLabel={t('users:groups.delete')}
        danger
        busy={busy}
        onConfirm={() => void remove()}
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
          <button type="submit" form="group-name-form" className="ui-btn ui-btn--primary">
            {t('actions.save')}
          </button>
        </div>
      }
    >
      <form id="group-name-form" noValidate onSubmit={submit}>
        <label className="field">
          <span className="field__label">{t('users:groups.name')} *</span>
          <input
            className="field__input"
            value={name}
            required
            aria-invalid={showError && !name.trim()}
            aria-describedby={showError && !name.trim() ? 'group-name-error' : undefined}
            onChange={(event) => setName(event.target.value)}
          />
          {showError && !name.trim() ? (
            <span className="field__error" id="group-name-error">
              {t('validation:required')}
            </span>
          ) : null}
        </label>
      </form>
    </Drawer>
  )
}
