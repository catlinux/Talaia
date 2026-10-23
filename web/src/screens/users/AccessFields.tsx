import type { TFunction } from 'i18next'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Group } from '../../api/groups'
import type { ServerSummary } from '../../api/servers'
import type { Role, Scope, ScopeType } from '../../api/users'
import { createContainersApi } from '../../api/containers'

export const roles: Role[] = ['admin', 'server_operator', 'app_operator', 'viewer', 'metrics_only']

const scopeTypes: ScopeType[] = ['organization', 'group', 'server', 'stack', 'container']

/** roleDescription is the plain-language warning shown under the role select. */
export function roleDescription(t: TFunction, role: Role): string {
  return t(`users:roles.${role}.description`)
}

export function roleLabel(t: TFunction, role: Role): string {
  return t(`users:roles.${role}.label`)
}

export function ScopeLabel({
  scope,
  groups,
  servers,
}: {
  scope: Scope
  groups: Group[]
  servers: ServerSummary[]
}) {
  const { t } = useTranslation('users')
  if (scope.type === 'organization') {
    return <>{t('scope.organization')}</>
  }
  if (scope.type === 'group') {
    const group = groups.find((candidate) => candidate.id === scope.groupId)
    return <>{t('scope.groupValue', { name: group?.name ?? scope.groupId ?? '' })}</>
  }
  const server = servers.find((candidate) => candidate.id === scope.serverId)
  if (scope.type === 'server') {
    return <>{t('scope.serverValue', { name: server?.name ?? scope.serverId ?? '' })}</>
  }
  const name = scope.name ?? ''
  return (
    <>
      {scope.type === 'stack'
        ? t('scope.stackValue', { name, server: server?.name ?? scope.serverId ?? '' })
        : t('scope.containerValue', { name, server: server?.name ?? scope.serverId ?? '' })}
    </>
  )
}

export function RoleSelect({
  value,
  onChange,
  id,
  disabled = false,
  ariaLabel,
}: {
  value: Role
  onChange: (role: Role) => void
  id?: string
  disabled?: boolean
  ariaLabel?: string
}) {
  const { t } = useTranslation('users')
  return (
    <select
      id={id}
      className="field__input"
      aria-label={ariaLabel}
      value={value}
      disabled={disabled}
      onChange={(event) => onChange(event.target.value as Role)}
    >
      {roles.map((role) => (
        <option key={role} value={role}>
          {roleLabel(t, role)}
        </option>
      ))}
    </select>
  )
}

export function RoleDescription({ role }: { role: Role }) {
  const { t } = useTranslation('users')
  return <p className="users__hint">{roleDescription(t, role)}</p>
}

export function ScopeFields({
  scope,
  onChange,
  groups,
  servers,
  showErrors = false,
}: {
  scope: Scope
  onChange: (scope: Scope) => void
  groups: Group[]
  servers: ServerSummary[]
  showErrors?: boolean
}) {
  const { t } = useTranslation('users')
  const [choiceState, setChoiceState] = useState<{ key: string; items: string[] } | null>(null)
  const [loadingKey, setLoadingKey] = useState('')
  const scopeType = scope.type
  const serverId = scopeType === 'stack' || scopeType === 'container' ? scope.serverId : undefined
  useEffect(() => {
    if (!serverId) return
    let active = true
    const api = createContainersApi()
    const load =
      scopeType === 'stack'
        ? api.stacks(serverId).then((result) => result.stacks)
        : api.list(serverId).then((result) => result.containers.map((item) => item.name))
    const key = `${scopeType}:${serverId}`
    void load.then(
      (items) => {
        if (active) {
          setChoiceState({ key, items })
          setLoadingKey((current) => (current === key ? '' : current))
        }
      },
      () => {
        if (active) {
          setChoiceState({ key, items: [] })
          setLoadingKey((current) => (current === key ? '' : current))
        }
      },
    )
    return () => {
      active = false
    }
  }, [scopeType, serverId])
  const key = serverId ? `${scopeType}:${serverId}` : ''
  const choices = choiceState?.key === key ? choiceState.items : null
  const loadingChoices = !!key && loadingKey === key
  const setType = (type: ScopeType) => {
    const nextServerId =
      type === 'server' || type === 'stack' || type === 'container' ? (servers[0]?.id ?? '') : ''
    setLoadingKey(type === 'stack' || type === 'container' ? `${type}:${nextServerId}` : '')
    onChange({
      type,
      ...(type === 'group' ? { groupId: groups[0]?.id ?? '' } : {}),
      ...(type === 'server' || type === 'stack' || type === 'container'
        ? { serverId: servers[0]?.id ?? '' }
        : {}),
      ...(type === 'stack' || type === 'container' ? { name: '' } : {}),
    })
  }

  return (
    <div className="users__scope">
      <label className="field">
        <span className="field__label">{t('scope.type')}</span>
        <select
          className="field__input"
          value={scope.type}
          onChange={(event) => setType(event.target.value as ScopeType)}
        >
          {scopeTypes.map((type) => (
            <option key={type} value={type}>
              {t(`scope.types.${type}`)}
            </option>
          ))}
        </select>
      </label>

      {scope.type === 'group' ? (
        <label className="field">
          <span className="field__label">{t('scope.group')}</span>
          <select
            className="field__input"
            value={scope.groupId ?? ''}
            onChange={(event) => onChange({ ...scope, groupId: event.target.value })}
          >
            {groups.map((group) => (
              <option key={group.id} value={group.id}>
                {group.name}
              </option>
            ))}
          </select>
        </label>
      ) : null}

      {scope.type === 'server' || scope.type === 'stack' || scope.type === 'container' ? (
        <label className="field">
          <span className="field__label">{t('scope.server')}</span>
          <select
            className="field__input"
            value={scope.serverId ?? ''}
            onChange={(event) => {
              setLoadingKey(`${scope.type}:${event.target.value}`)
              onChange({ ...scope, serverId: event.target.value, name: '' })
            }}
          >
            {servers.map((server) => (
              <option key={server.id} value={server.id}>
                {server.name}
              </option>
            ))}
          </select>
        </label>
      ) : null}

      {scope.type === 'stack' || scope.type === 'container' ? (
        <label className="field">
          <span className="field__label">
            {t(scope.type === 'stack' ? 'scope.stackName' : 'scope.containerName')}
            <span aria-hidden="true"> *</span>
          </span>
          {loadingChoices ? (
            <span className="users__hint" role="status">
              {t('scope.loading')}
            </span>
          ) : choices && choices.length > 0 ? (
            <select
              className="field__input"
              value={scope.name ?? ''}
              required
              aria-invalid={showErrors && !(scope.name ?? '').trim()}
              aria-describedby={
                showErrors && !(scope.name ?? '').trim() ? 'scope-name-error' : undefined
              }
              onChange={(event) => onChange({ ...scope, name: event.target.value })}
            >
              <option value="">{t('scope.chooseName')}</option>
              {choices.map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
            </select>
          ) : (
            <>
              <input
                className="field__input"
                type="text"
                required
                value={scope.name ?? ''}
                placeholder={t('scope.namePlaceholder')}
                aria-invalid={showErrors && !(scope.name ?? '').trim()}
                aria-describedby={
                  showErrors && !(scope.name ?? '').trim() ? 'scope-name-error' : undefined
                }
                onChange={(event) => onChange({ ...scope, name: event.target.value })}
              />
              {!choices || choices.length === 0 ? (
                <span className="users__hint">{t('scope.exactName')}</span>
              ) : null}
            </>
          )}
          {showErrors && !(scope.name ?? '').trim() ? (
            <span className="field__error" id="scope-name-error">
              {t('scope.nameRequired')}
            </span>
          ) : null}
        </label>
      ) : null}
    </div>
  )
}
