import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { AlertChannel, AlertRule, AlertRuleInput, AlertsAdminApi } from '../../api/alertsAdmin'
import type { Group } from '../../api/groups'
import type { ServerSummary } from '../../api/servers'
import { errorMessage } from '../../auth/AuthGate'
import { PlusIcon, TrashIcon } from '../../components/icons'
import { Dialog } from '../users/Dialog'
import { RuleForm } from './RuleForm'

interface RulesCardProps {
  api: AlertsAdminApi
  servers: ServerSummary[]
  groups: Group[]
}

function inputOf(rule: AlertRule, enabled = rule.enabled): AlertRuleInput {
  return {
    name: rule.name,
    type: rule.type,
    enabled,
    severity: rule.severity,
    params: rule.params,
    scopeType: rule.scopeType,
    serverId: rule.serverId,
    groupId: rule.groupId,
    scopeName: rule.scopeName,
    channelIds: rule.channelIds,
    repeatIntervalSeconds: rule.repeatIntervalSeconds,
  }
}

function scopeLabel(
  rule: AlertRule,
  servers: ServerSummary[],
  groups: Group[],
  translate: (key: string, options?: Record<string, unknown>) => string,
): string {
  switch (rule.scopeType) {
    case 'organization':
      return translate('alertsSettings:scopes.organization')
    case 'group': {
      const group = groups.find((item) => item.id === rule.groupId)
      return `${translate('alertsSettings:scopes.group')} · ${group?.name ?? rule.groupId ?? '—'}`
    }
    case 'server': {
      const server = servers.find((item) => item.id === rule.serverId)
      return `${translate('alertsSettings:scopes.server')} · ${server?.name ?? rule.serverId ?? '—'}`
    }
    case 'stack': {
      const server = servers.find((item) => item.id === rule.serverId)
      return `${translate('alertsSettings:scopes.stack')} · ${server?.name ?? rule.serverId ?? '—'} / ${rule.scopeName ?? '—'}`
    }
    case 'container': {
      const server = servers.find((item) => item.id === rule.serverId)
      return `${translate('alertsSettings:scopes.container')} · ${server?.name ?? rule.serverId ?? '—'} / ${rule.scopeName ?? '—'}`
    }
  }
}

/** RulesCard manages the alert rules of the organization. */
export function RulesCard({ api, servers, groups }: RulesCardProps) {
  const { t } = useTranslation(['alertsSettings', 'errors'])
  const [rules, setRules] = useState<AlertRule[] | null>(null)
  const [channels, setChannels] = useState<AlertChannel[]>([])
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [editing, setEditing] = useState<AlertRule | 'new' | null>(null)
  const [deleting, setDeleting] = useState<AlertRule | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [toggling, setToggling] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      setRules(await api.listRules())
    } catch (cause) {
      setError(errorMessage(t, cause))
    }
  }, [api, t])

  useEffect(() => {
    let active = true
    queueMicrotask(() => {
      api.listRules().then(
        (rows) => {
          if (active) {
            setRules(rows)
            setError(null)
          }
        },
        (cause: unknown) => {
          if (active) setError(errorMessage(t, cause))
        },
      )
      api.listChannels().then(
        (rows) => {
          if (active) setChannels(rows)
        },
        () => {
          if (active) setChannels([])
        },
      )
    })
    return () => {
      active = false
    }
  }, [api, t])

  const save = async (input: AlertRuleInput) => {
    if (editing === null) return
    if (editing === 'new') {
      await api.createRule(input)
      setNotice(t('alertsSettings:rules.created'))
    } else {
      await api.updateRule(editing.id, input)
      setNotice(t('alertsSettings:rules.updated'))
    }
    setEditing(null)
    await load()
  }

  const toggle = async (rule: AlertRule, enabled: boolean) => {
    setToggling(rule.id)
    setError(null)
    setNotice(null)
    try {
      const next = await api.updateRule(rule.id, inputOf(rule, enabled))
      setRules((rows) => rows?.map((row) => (row.id === next.id ? next : row)) ?? rows)
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setToggling(null)
    }
  }

  const confirmDelete = async () => {
    if (deleting === null) return
    setDeleteBusy(true)
    setDeleteError(null)
    try {
      await api.deleteRule(deleting.id)
      setDeleting(null)
      setNotice(t('alertsSettings:rules.deleted'))
      await load()
    } catch (cause) {
      setDeleteError(errorMessage(t, cause))
    } finally {
      setDeleteBusy(false)
    }
  }

  return (
    <section className="settings__card alerts-admin__card" aria-labelledby="settings-rules-title">
      <div className="settings__card-head">
        <div>
          <h3 className="settings__card-title" id="settings-rules-title">
            {t('alertsSettings:rules.title')}
          </h3>
          <p className="settings__hint">{t('alertsSettings:rules.intro')}</p>
        </div>
        <button
          type="button"
          className="settings__button settings__button--primary"
          onClick={() => setEditing('new')}
        >
          <PlusIcon />
          {t('alertsSettings:rules.add')}
        </button>
      </div>

      {notice !== null ? (
        <p className="settings__notice" role="status">
          {notice}
        </p>
      ) : null}
      {error !== null ? (
        <p className="settings__message settings__message--error" role="alert">
          {error}
        </p>
      ) : null}

      {rules === null ? (
        error === null ? (
          <p className="settings__message" role="status">
            {t('alertsSettings:rules.loading')}
          </p>
        ) : null
      ) : rules.length === 0 ? (
        <p className="settings__message">{t('alertsSettings:rules.empty')}</p>
      ) : (
        <div className="users-table-wrap">
          <table className="users-table alerts-admin__table">
            <thead>
              <tr>
                <th scope="col">{t('alertsSettings:rules.table.name')}</th>
                <th scope="col">{t('alertsSettings:rules.table.type')}</th>
                <th scope="col">{t('alertsSettings:rules.table.severity')}</th>
                <th scope="col">{t('alertsSettings:rules.table.scope')}</th>
                <th scope="col">{t('alertsSettings:rules.table.enabled')}</th>
                <th scope="col">{t('alertsSettings:rules.table.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {rules.map((rule) => (
                <tr key={rule.id}>
                  <td data-label={t('alertsSettings:rules.table.name')}>
                    <span className="alerts-admin__name">{rule.name}</span>
                    {rule.builtin ? (
                      <span className="alerts-admin__builtin">
                        {t('alertsSettings:rules.builtin')}
                      </span>
                    ) : null}
                  </td>
                  <td data-label={t('alertsSettings:rules.table.type')}>
                    {t(`alertsSettings:types.${rule.type}.name`)}
                  </td>
                  <td data-label={t('alertsSettings:rules.table.severity')}>
                    <span
                      className={`alerts-admin__severity alerts-admin__severity--${rule.severity}`}
                    >
                      {t(`alertsSettings:severity.${rule.severity}`)}
                    </span>
                  </td>
                  <td data-label={t('alertsSettings:rules.table.scope')}>
                    {scopeLabel(rule, servers, groups, t)}
                  </td>
                  <td data-label={t('alertsSettings:rules.table.enabled')}>
                    <label className="alerts-admin__switch alerts-admin__switch--inline">
                      <input
                        type="checkbox"
                        checked={rule.enabled}
                        disabled={toggling === rule.id}
                        aria-label={t('alertsSettings:rules.toggle', { name: rule.name })}
                        onChange={(event) => void toggle(rule, event.target.checked)}
                      />
                      <span>
                        {rule.enabled
                          ? t('alertsSettings:common.enabled')
                          : t('alertsSettings:common.disabled')}
                      </span>
                    </label>
                  </td>
                  <td data-label={t('alertsSettings:rules.table.actions')}>
                    <div className="alerts-admin__row-actions">
                      <button
                        type="button"
                        className="settings__button"
                        onClick={() => setEditing(rule)}
                      >
                        {t('alertsSettings:actions.edit')}
                      </button>
                      {!rule.builtin ? (
                        <button
                          type="button"
                          className="settings__button settings__button--danger"
                          onClick={() => {
                            setDeleteError(null)
                            setDeleting(rule)
                          }}
                        >
                          <TrashIcon />
                          {t('alertsSettings:actions.delete')}
                        </button>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {editing !== null ? (
        <RuleForm
          key={editing === 'new' ? 'new' : editing.id}
          rule={editing === 'new' ? null : editing}
          servers={servers}
          groups={groups}
          channels={channels}
          onClose={() => setEditing(null)}
          onSave={save}
        />
      ) : null}

      {deleting !== null ? (
        <Dialog
          title={t('alertsSettings:rules.deleteTitle', { name: deleting.name })}
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
          <p>{t('alertsSettings:rules.deleteBody')}</p>
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
