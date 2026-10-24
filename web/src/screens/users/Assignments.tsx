import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Group } from '../../api/groups'
import type { ServerSummary } from '../../api/servers'
import type { Binding, Role, Scope } from '../../api/users'
import { RoleDescription, RoleSelect, ScopeFields, ScopeLabel, roleLabel } from './AccessFields'
import { TrashIcon } from '../../components/icons'

interface AssignmentsProps {
  bindings: Binding[]
  groups: Group[]
  servers: ServerSummary[]
  onChangeRole: (binding: Binding, role: Role) => void
  onRemove: (binding: Binding) => void
  onAdd: (role: Role, scope: Scope) => void
  busy?: boolean
  error?: string | null
}

/** Assignments lists role assignments and the form that adds another. */
export function Assignments({
  bindings,
  groups,
  servers,
  onChangeRole,
  onRemove,
  onAdd,
  busy = false,
  error = null,
}: AssignmentsProps) {
  const { t } = useTranslation('users')
  const [adding, setAdding] = useState(false)
  const [role, setRole] = useState<Role>('viewer')
  const [scope, setScope] = useState<Scope>({ type: 'organization' })
  const [showErrors, setShowErrors] = useState(false)
  const [dismissedError, setDismissedError] = useState<string | null>(null)
  const clearError = () => setDismissedError(error)

  const submit = () => {
    setShowErrors(true)
    if ((scope.type === 'stack' || scope.type === 'container') && !scope.name?.trim()) {
      requestAnimationFrame(() =>
        document.querySelector<HTMLElement>('.assignments__add [aria-invalid="true"]')?.focus(),
      )
      return
    }
    onAdd(role, scope)
    setAdding(false)
    setRole('viewer')
    setScope({ type: 'organization' })
  }

  return (
    <div className="assignments">
      {error !== null && error !== dismissedError ? (
        <p className="users__error" role="alert">
          {error}
        </p>
      ) : null}
      {bindings.length === 0 ? (
        <p className="users__hint">{t('access.empty')}</p>
      ) : (
        <ul className="assignments__list">
          {bindings.map((binding) => (
            <li key={binding.id} className="assignment">
              <div className="assignment__main">
                <span className="assignment__role">{roleLabel(t, binding.role)}</span>
                <span className="assignment__scope">
                  <ScopeLabel scope={binding.scope} groups={groups} servers={servers} />
                </span>
              </div>
              <div className="assignment__actions">
                <RoleSelect
                  value={binding.role}
                  ariaLabel={t('access.role')}
                  disabled={busy}
                  onChange={(next) => onChangeRole(binding, next)}
                />
                <button
                  type="button"
                  className="iconbtn"
                  aria-label={t('access.remove', { role: roleLabel(t, binding.role) })}
                  disabled={busy}
                  onClick={() => onRemove(binding)}
                >
                  <TrashIcon />
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}

      {adding ? (
        <div className="assignments__add">
          <label className="field">
            <span className="field__label">{t('access.role')}</span>
            <RoleSelect
              value={role}
              onChange={(value) => {
                clearError()
                setRole(value)
              }}
            />
          </label>
          <RoleDescription role={role} />
          <ScopeFields
            scope={scope}
            onChange={(value) => {
              clearError()
              setScope(value)
            }}
            groups={groups}
            servers={servers}
            showErrors={showErrors}
          />
          <div className="users__actions">
            <button
              type="button"
              className="settings__button settings__button--primary"
              onClick={() => {
                clearError()
                submit()
              }}
            >
              {t('access.add')}
            </button>
            <button
              type="button"
              className="settings__button"
              onClick={() => {
                clearError()
                setAdding(false)
              }}
            >
              {t('actions.cancel')}
            </button>
          </div>
        </div>
      ) : (
        <button
          type="button"
          className="cbtn"
          disabled={busy}
          onClick={() => {
            clearError()
            setAdding(true)
          }}
        >
          {t('access.addButton')}
        </button>
      )}
    </div>
  )
}
