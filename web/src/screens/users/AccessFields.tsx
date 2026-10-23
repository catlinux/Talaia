import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'

import type { Group } from '../../api/groups'
import type { ServerSummary } from '../../api/servers'
import type { Role, Scope, ScopeType } from '../../api/users'

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
}: {
  scope: Scope
  onChange: (scope: Scope) => void
  groups: Group[]
  servers: ServerSummary[]
}) {
  const { t } = useTranslation('users')
  const setType = (type: ScopeType) => {
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
            onChange={(event) => onChange({ ...scope, serverId: event.target.value })}
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
          </span>
          <input
            className="field__input"
            type="text"
            value={scope.name ?? ''}
            placeholder={t('scope.namePlaceholder')}
            onChange={(event) => onChange({ ...scope, name: event.target.value })}
          />
        </label>
      ) : null}
    </div>
  )
}
