import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type {
  AlertRule,
  AlertRuleInput,
  AlertRuleSeverity,
  AlertRuleType,
  AlertsAdminApi,
} from '../../api/alertsAdmin'
import { errorMessage } from '../../auth/AuthGate'
import { usePermissions } from '../../auth/permissions'
import { Dialog } from '../users/Dialog'
import {
  RuleParamFields,
  ruleParamFields,
  ruleParamNumbers,
  ruleParamValues,
  ruleParamsValid,
} from '../settings/RuleForm'

interface AlertsSectionProps {
  api: AlertsAdminApi
  serverId: string
  serverName: string
  /** groupId is the server's group when the caller knows it. */
  groupId?: string
}

interface RuleRow {
  inherited: AlertRule
  override: AlertRule | null
}

interface DialogState {
  mode: 'customise' | 'edit'
  rule: AlertRule
}

const typeOrder = Object.keys(ruleParamFields) as AlertRuleType[]

function pairKey(rule: AlertRule): string {
  return `${rule.type}:${rule.severity}`
}

function severityRank(severity: AlertRuleSeverity): number {
  return severity === 'critical' ? 2 : 1
}

function inputOf(rule: AlertRule, enabled: boolean, serverId: string): AlertRuleInput {
  return {
    name: rule.name,
    type: rule.type,
    enabled,
    severity: rule.severity,
    params: rule.params,
    scopeType: 'server',
    serverId,
    groupId: null,
    scopeName: null,
    channelIds: [...rule.channelIds],
    repeatIntervalSeconds: rule.repeatIntervalSeconds,
  }
}

/** AlertsSection lists the rules that apply to one server. */
export function AlertsSection({ api, serverId, serverName, groupId }: AlertsSectionProps) {
  const { t } = useTranslation(['server', 'alertsSettings', 'common', 'errors'])
  const permissions = usePermissions()
  const canView = !permissions.loading && permissions.can('alerts.view' as never)
  const canEdit = !permissions.loading && permissions.can('alerts.manage' as never)
  const [rules, setRules] = useState<AlertRule[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [dialog, setDialog] = useState<DialogState | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      setRules(await api.listRules())
    } catch (cause) {
      setError(errorMessage(t, cause))
    }
  }, [api, t])

  useEffect(() => {
    if (!canView) return
    let active = true
    queueMicrotask(() => {
      api.listRules().then(
        (next) => {
          if (active) {
            setRules(next)
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
  }, [api, canView, t])

  const { rows, other } = useMemo(() => {
    const inherited = new Map<string, { rule: AlertRule; rank: number }>()
    for (const rule of rules ?? []) {
      let rank = -1
      if (rule.scopeType === 'organization') rank = 0
      else if (rule.scopeType === 'group' && groupId !== undefined && rule.groupId === groupId) {
        rank = 1
      }
      if (rank < 0) continue
      const key = pairKey(rule)
      const current = inherited.get(key)
      if (current === undefined || rank > current.rank) {
        inherited.set(key, { rule, rank })
      }
    }

    const effective: RuleRow[] = [...inherited.values()]
      .map(({ rule }) => ({ inherited: rule, override: null }))
      .sort(
        (left, right) =>
          typeOrder.indexOf(left.inherited.type) - typeOrder.indexOf(right.inherited.type) ||
          severityRank(right.inherited.severity) - severityRank(left.inherited.severity),
      )
    const byPair = new Map(effective.map((row) => [pairKey(row.inherited), row]))
    const handled = new Set(effective.map((row) => row.inherited.id))
    for (const rule of rules ?? []) {
      if (rule.scopeType !== 'server' || rule.serverId !== serverId) continue
      const row = byPair.get(pairKey(rule))
      if (row !== undefined && row.override === null) {
        row.override = rule
        handled.add(rule.id)
      }
    }

    const otherRules = (rules ?? []).filter((rule) => {
      if (rule.scopeType === 'server' && rule.serverId === serverId) {
        return !handled.has(rule.id)
      }
      return (
        (rule.scopeType === 'stack' || rule.scopeType === 'container') && rule.serverId === serverId
      )
    })
    otherRules.sort(
      (left, right) =>
        typeOrder.indexOf(left.type) - typeOrder.indexOf(right.type) ||
        left.name.localeCompare(right.name),
    )
    return { rows: effective, other: otherRules }
  }, [groupId, rules, serverId])

  if (!canView) return null

  const typeLabel = (type: AlertRuleType) => t(`alertsSettings:types.${type}.name`)
  const severityLabel = (severity: AlertRuleSeverity) => t(`alertsSettings:severity.${severity}`)
  const paramsLabel = (rule: AlertRule) => {
    const values = ruleParamValues(rule.type, rule.params)
    return ruleParamFields[rule.type]
      .map((field) => `${t(`alertsSettings:params.${field.key}`)}: ${values[field.key]}`)
      .join(' · ')
  }
  const scopeLabel = (rule: AlertRule) => {
    const scope = t(`alertsSettings:scopes.${rule.scopeType}`)
    if (rule.scopeType === 'server') {
      return `${scope} · ${rule.serverId === serverId ? serverName : (rule.serverId ?? '')}`
    }
    if (rule.scopeType === 'stack' || rule.scopeType === 'container') {
      return `${scope} · ${rule.scopeName ?? ''}`
    }
    return scope
  }
  const badge = (row: RuleRow) => {
    if (row.override === null) return { text: t('alerts.inherited'), className: 'settings__badge' }
    if (!row.override.enabled) {
      return {
        text: t('alerts.off'),
        className: 'settings__badge settings__badge--red',
      }
    }
    return {
      text: t('alerts.customised'),
      className: 'settings__badge settings__badge--green',
    }
  }

  const turnOff = async (rule: AlertRule) => {
    setBusy(pairKey(rule))
    setActionError(null)
    try {
      await api.createRule(inputOf(rule, false, serverId))
      await load()
    } catch (cause) {
      setActionError(errorMessage(t, cause))
    } finally {
      setBusy(null)
    }
  }

  const restore = async (rule: AlertRule) => {
    setBusy(pairKey(rule))
    setActionError(null)
    try {
      await api.deleteRule(rule.id)
      await load()
    } catch (cause) {
      setActionError(errorMessage(t, cause))
    } finally {
      setBusy(null)
    }
  }

  return (
    <section className="services" aria-labelledby="server-alerts-title" aria-busy={rules === null}>
      <header className="stacks__head">
        <div>
          <h2 className="stacks__title" id="server-alerts-title">
            {t('alerts.title')}
          </h2>
          <p className="stacks__intro">{t('alerts.intro')}</p>
        </div>
      </header>

      <div className="services__body">
        {error !== null ? (
          <p className="services__message services__message--error" role="alert">
            {error}
          </p>
        ) : null}
        {actionError !== null ? (
          <p className="services__message services__message--error" role="alert">
            {actionError}
          </p>
        ) : null}

        {rules === null ? (
          error === null ? (
            <p className="services__message" role="status">
              {t('alerts.loading')}
            </p>
          ) : null
        ) : rows.length === 0 ? (
          <p className="services__empty">{t('alerts.empty')}</p>
        ) : (
          <ul className="servicelist" aria-label={t('alerts.title')}>
            {rows.map((row) => {
              const effective = row.override ?? row.inherited
              const label = badge(row)
              return (
                <li className="servicerow" key={pairKey(row.inherited)}>
                  <div className="services__main">
                    <div className="services__headline">
                      <h3 className="services__name">{effective.name}</h3>
                      <span className={label.className}>{label.text}</span>
                      <span
                        className={`alerts-admin__severity alerts-admin__severity--${effective.severity}`}
                      >
                        {severityLabel(effective.severity)}
                      </span>
                    </div>
                    <p className="services__description">
                      {typeLabel(effective.type)} · {scopeLabel(effective)}
                    </p>
                    <p className="services__description">{paramsLabel(effective)}</p>
                    {row.override !== null && !row.override.enabled ? (
                      <p className="services__warning">{t('alerts.offNote')}</p>
                    ) : null}
                  </div>
                  {canEdit ? (
                    <div className="services__actions">
                      {row.override === null ? (
                        <>
                          <button
                            type="button"
                            className="cbtn services__action"
                            disabled={busy === pairKey(row.inherited)}
                            onClick={() => setDialog({ mode: 'customise', rule: row.inherited })}
                          >
                            {t('alerts.actions.customise')}
                          </button>
                          <button
                            type="button"
                            className="cbtn services__action"
                            disabled={busy === pairKey(row.inherited)}
                            onClick={() => void turnOff(row.inherited)}
                          >
                            {t('alerts.actions.turnOff')}
                          </button>
                        </>
                      ) : (
                        <>
                          <button
                            type="button"
                            className="cbtn services__action"
                            disabled={busy === pairKey(row.inherited)}
                            onClick={() => setDialog({ mode: 'edit', rule: row.override! })}
                          >
                            {t('alerts.actions.edit')}
                          </button>
                          <button
                            type="button"
                            className="cbtn services__action"
                            disabled={busy === pairKey(row.inherited)}
                            onClick={() => void restore(row.override!)}
                          >
                            {t('alerts.actions.restore')}
                          </button>
                        </>
                      )}
                    </div>
                  ) : null}
                </li>
              )
            })}
          </ul>
        )}

        {other.length > 0 ? (
          <section aria-labelledby="server-alerts-other-title">
            <h3 className="services__name" id="server-alerts-other-title">
              {t('alerts.otherTitle')}
            </h3>
            <p className="services__description">
              {t('alerts.otherHint', {
                settings: t('common:nav.settings'),
                alerts: t('common:nav.alerts'),
              })}
            </p>
            <ul className="servicelist">
              {other.map((rule) => (
                <li className="servicerow" key={rule.id}>
                  <div className="services__main">
                    <div className="services__headline">
                      <h4 className="services__name">{rule.name}</h4>
                      <span className="settings__badge">{t('alerts.readOnly')}</span>
                      <span
                        className={`alerts-admin__severity alerts-admin__severity--${rule.severity}`}
                      >
                        {severityLabel(rule.severity)}
                      </span>
                    </div>
                    <p className="services__description">
                      {typeLabel(rule.type)} · {scopeLabel(rule)}
                    </p>
                  </div>
                </li>
              ))}
            </ul>
          </section>
        ) : null}
      </div>

      {dialog !== null ? (
        <OverrideDialog
          api={api}
          serverId={serverId}
          state={dialog}
          onClose={() => setDialog(null)}
          onSaved={async () => {
            setDialog(null)
            await load()
          }}
        />
      ) : null}
    </section>
  )
}

interface OverrideDialogProps {
  api: AlertsAdminApi
  serverId: string
  state: DialogState
  onClose: () => void
  onSaved: () => Promise<void>
}

function OverrideDialog({ api, serverId, state, onClose, onSaved }: OverrideDialogProps) {
  const { t } = useTranslation(['server', 'alertsSettings', 'errors'])
  const [values, setValues] = useState(() => ruleParamValues(state.rule.type, state.rule.params))
  const [showErrors, setShowErrors] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)
  const valid = ruleParamsValid(state.rule.type, values)

  const save = async () => {
    if (!valid) {
      setShowErrors(true)
      return
    }
    setPending(true)
    setSaveError(null)
    const input: AlertRuleInput = {
      name: state.rule.name,
      type: state.rule.type,
      enabled: state.mode === 'edit' ? state.rule.enabled : true,
      severity: state.rule.severity,
      params: ruleParamNumbers(state.rule.type, values),
      scopeType: 'server',
      serverId,
      groupId: null,
      scopeName: null,
      channelIds: [...state.rule.channelIds],
      repeatIntervalSeconds: state.rule.repeatIntervalSeconds,
    }
    try {
      if (state.mode === 'edit') {
        await api.updateRule(state.rule.id, input)
      } else {
        await api.createRule(input)
      }
      await onSaved()
    } catch (cause) {
      setSaveError(errorMessage(t, cause))
    } finally {
      setPending(false)
    }
  }

  const title =
    state.mode === 'edit'
      ? t('alerts.editTitle', { type: t(`alertsSettings:types.${state.rule.type}.name`) })
      : t('alerts.customiseTitle', { type: t(`alertsSettings:types.${state.rule.type}.name`) })

  return (
    <Dialog
      title={title}
      onClose={onClose}
      footer={
        <div className="alerts-admin__actions">
          <button type="button" className="settings__button" onClick={onClose}>
            {t('alerts.actions.cancel')}
          </button>
          <button
            type="button"
            className="settings__button settings__button--primary"
            disabled={pending}
            onClick={() => void save()}
          >
            {t('alerts.actions.save')}
          </button>
        </div>
      }
    >
      <form
        className="alerts-admin__form"
        onSubmit={(event) => {
          event.preventDefault()
          void save()
        }}
      >
        <p className="settings__hint">{t('alerts.dialogIntro')}</p>
        <p className="services__description">{state.rule.name}</p>
        <fieldset className="alerts-admin__fieldset">
          <legend className="field__label">{t('alerts.params')}</legend>
          <RuleParamFields
            type={state.rule.type}
            values={values}
            invalid={showErrors && !valid}
            onChange={(key, value) => setValues({ ...values, [key]: value })}
          />
          {showErrors && !valid ? (
            <span className="field__error">{t('alertsSettings:validation.params')}</span>
          ) : null}
        </fieldset>
        {saveError !== null ? (
          <p className="settings__message settings__message--error" role="alert">
            {saveError}
          </p>
        ) : null}
      </form>
    </Dialog>
  )
}
