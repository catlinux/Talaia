import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { AlertChannel, AlertChannelInput, AlertsAdminApi } from '../../api/alertsAdmin'
import { errorMessage } from '../../auth/AuthGate'
import { PlusIcon, TrashIcon } from '../../components/icons'
import { Dialog } from '../users/Dialog'
import { ChannelForm } from './ChannelForm'

interface ChannelsCardProps {
  api: AlertsAdminApi
}

interface TestNotice {
  ok: boolean
  text: string
}

function inputOf(channel: AlertChannel, enabled = channel.enabled): AlertChannelInput {
  return {
    name: channel.name,
    type: channel.type,
    enabled,
    language: channel.language,
    config: channel.config,
  }
}

function formatDate(value: string, language: string, never: string): string {
  if (value === '') return never
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

/** ChannelsCard manages the notification channels and their delivery tests. */
export function ChannelsCard({ api }: ChannelsCardProps) {
  const { t, i18n } = useTranslation(['alertsSettings', 'errors'])
  const language = i18n.resolvedLanguage ?? i18n.language
  const [channels, setChannels] = useState<AlertChannel[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [testNotice, setTestNotice] = useState<TestNotice | null>(null)
  const [editing, setEditing] = useState<AlertChannel | 'new' | null>(null)
  const [deleting, setDeleting] = useState<AlertChannel | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [testing, setTesting] = useState<string | null>(null)
  const [toggling, setToggling] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      setChannels(await api.listChannels())
    } catch (cause) {
      setError(errorMessage(t, cause))
    }
  }, [api, t])

  useEffect(() => {
    let active = true
    queueMicrotask(() => {
      api.listChannels().then(
        (rows) => {
          if (active) {
            setChannels(rows)
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
  }, [api, t])

  const save = async (input: AlertChannelInput) => {
    if (editing === null) return
    if (editing === 'new') {
      await api.createChannel(input)
      setNotice(t('alertsSettings:channels.created'))
    } else {
      await api.updateChannel(editing.id, input)
      setNotice(t('alertsSettings:channels.updated'))
    }
    setEditing(null)
    await load()
  }

  const toggle = async (channel: AlertChannel, enabled: boolean) => {
    setToggling(channel.id)
    setError(null)
    setNotice(null)
    try {
      const next = await api.updateChannel(channel.id, inputOf(channel, enabled))
      setChannels((rows) => rows?.map((row) => (row.id === next.id ? next : row)) ?? rows)
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setToggling(null)
    }
  }

  const test = async (channel: AlertChannel) => {
    setTesting(channel.id)
    setError(null)
    setTestNotice(null)
    try {
      const result = await api.testChannel(channel.id)
      setTestNotice({
        ok: result.ok,
        text: result.ok
          ? t('alertsSettings:channels.testOk', { name: channel.name })
          : t('alertsSettings:channels.testFail', {
              name: channel.name,
              error: result.error || t('alertsSettings:channels.unknownError'),
            }),
      })
      await load()
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setTesting(null)
    }
  }

  const confirmDelete = async () => {
    if (deleting === null) return
    setDeleteBusy(true)
    setDeleteError(null)
    try {
      await api.deleteChannel(deleting.id)
      setDeleting(null)
      setNotice(t('alertsSettings:channels.deleted'))
      await load()
    } catch (cause) {
      setDeleteError(errorMessage(t, cause))
    } finally {
      setDeleteBusy(false)
    }
  }

  return (
    <section
      className="settings__card alerts-admin__card"
      aria-labelledby="settings-channels-title"
    >
      <div className="settings__card-head">
        <div>
          <h3 className="settings__card-title" id="settings-channels-title">
            {t('alertsSettings:channels.title')}
          </h3>
          <p className="settings__hint">{t('alertsSettings:channels.intro')}</p>
        </div>
        <button
          type="button"
          className="settings__button settings__button--primary"
          onClick={() => setEditing('new')}
        >
          <PlusIcon />
          {t('alertsSettings:channels.add')}
        </button>
      </div>

      {notice !== null ? (
        <p className="settings__notice" role="status">
          {notice}
        </p>
      ) : null}
      {testNotice !== null ? (
        <p
          className={
            testNotice.ok ? 'settings__notice' : 'settings__notice settings__notice--error'
          }
          role={testNotice.ok ? 'status' : 'alert'}
        >
          {testNotice.text}
        </p>
      ) : null}
      {error !== null ? (
        <p className="settings__message settings__message--error" role="alert">
          {error}
        </p>
      ) : null}

      {channels === null ? (
        error === null ? (
          <p className="settings__message" role="status">
            {t('alertsSettings:channels.loading')}
          </p>
        ) : null
      ) : channels.length === 0 ? (
        <p className="settings__message">{t('alertsSettings:channels.empty')}</p>
      ) : (
        <div className="users-table-wrap">
          <table className="users-table alerts-admin__table">
            <thead>
              <tr>
                <th scope="col">{t('alertsSettings:channels.table.name')}</th>
                <th scope="col">{t('alertsSettings:channels.table.type')}</th>
                <th scope="col">{t('alertsSettings:channels.table.language')}</th>
                <th scope="col">{t('alertsSettings:channels.table.enabled')}</th>
                <th scope="col">{t('alertsSettings:channels.table.status')}</th>
                <th scope="col">{t('alertsSettings:channels.table.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {channels.map((channel) => (
                <tr key={channel.id}>
                  <td data-label={t('alertsSettings:channels.table.name')}>
                    <span className="alerts-admin__name">{channel.name}</span>
                    {channel.hasSecret ? (
                      <span className="alerts-admin__builtin">
                        {t('alertsSettings:channels.secretSaved')}
                      </span>
                    ) : null}
                  </td>
                  <td data-label={t('alertsSettings:channels.table.type')}>
                    <span className="alerts-admin__type">
                      {t(`alertsSettings:channels.types.${channel.type}`)}
                    </span>
                  </td>
                  <td data-label={t('alertsSettings:channels.table.language')}>
                    {t(`alertsSettings:languages.${channel.language}`)}
                  </td>
                  <td data-label={t('alertsSettings:channels.table.enabled')}>
                    <label className="alerts-admin__switch alerts-admin__switch--inline">
                      <input
                        type="checkbox"
                        checked={channel.enabled}
                        disabled={toggling === channel.id}
                        aria-label={t('alertsSettings:channels.toggle', { name: channel.name })}
                        onChange={(event) => void toggle(channel, event.target.checked)}
                      />
                      <span>
                        {channel.enabled
                          ? t('alertsSettings:common.enabled')
                          : t('alertsSettings:common.disabled')}
                      </span>
                    </label>
                  </td>
                  <td data-label={t('alertsSettings:channels.table.status')}>
                    {channel.lastError !== '' ? (
                      <span className="alerts-admin__error-text" title={channel.lastError}>
                        {channel.lastError}
                      </span>
                    ) : channel.lastOkAt !== null ? (
                      <span className="settings__badge settings__badge--green">
                        {t('alertsSettings:channels.lastOk', {
                          date: formatDate(
                            channel.lastOkAt,
                            language,
                            t('alertsSettings:common.never'),
                          ),
                        })}
                      </span>
                    ) : (
                      <span className="settings__hint">
                        {t('alertsSettings:channels.neverUsed')}
                      </span>
                    )}
                  </td>
                  <td data-label={t('alertsSettings:channels.table.actions')}>
                    <div className="alerts-admin__row-actions">
                      <button
                        type="button"
                        className="settings__button"
                        disabled={testing === channel.id}
                        onClick={() => void test(channel)}
                      >
                        {testing === channel.id
                          ? t('alertsSettings:channels.testing')
                          : t('alertsSettings:channels.test')}
                      </button>
                      <button
                        type="button"
                        className="settings__button"
                        onClick={() => setEditing(channel)}
                      >
                        {t('alertsSettings:actions.edit')}
                      </button>
                      <button
                        type="button"
                        className="settings__button settings__button--danger"
                        onClick={() => {
                          setDeleteError(null)
                          setDeleting(channel)
                        }}
                      >
                        <TrashIcon />
                        {t('alertsSettings:actions.delete')}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {editing !== null ? (
        <ChannelForm
          key={editing === 'new' ? 'new' : editing.id}
          channel={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSave={save}
        />
      ) : null}

      {deleting !== null ? (
        <Dialog
          title={t('alertsSettings:channels.deleteTitle', { name: deleting.name })}
          onClose={() => setDeleting(null)}
          footer={
            <div className="alerts-admin__actions">
              <button type="button" className="settings__button" onClick={() => setDeleting(null)}>
                {t('alertsSettings:actions.cancel')}
              </button>
              <button
                type="button"
                className="settings__button settings__button--danger"
                disabled={deleteBusy}
                onClick={() => void confirmDelete()}
              >
                {t('alertsSettings:actions.delete')}
              </button>
            </div>
          }
        >
          <p>{t('alertsSettings:channels.deleteBody')}</p>
          {deleteError !== null ? (
            <p className="settings__message settings__message--error" role="alert">
              {deleteError}
            </p>
          ) : null}
        </Dialog>
      ) : null}
    </section>
  )
}
