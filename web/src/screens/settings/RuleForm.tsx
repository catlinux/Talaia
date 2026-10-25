import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type {
  AlertChannel,
  AlertRule,
  AlertRuleInput,
  AlertRuleScopeType,
  AlertRuleSeverity,
  AlertRuleType,
} from '../../api/alertsAdmin'
import type { Group } from '../../api/groups'
import type { ServerSummary } from '../../api/servers'
import { errorMessage } from '../../auth/AuthGate'
import { Dialog } from '../users/Dialog'

interface RuleFormProps {
  rule: AlertRule | null
  servers: ServerSummary[]
  groups: Group[]
  channels: AlertChannel[]
  onClose: () => void
  onSave: (input: AlertRuleInput) => Promise<void>
}

interface FormState {
  name: string
  type: AlertRuleType
  enabled: boolean
  severity: AlertRuleSeverity
  scopeType: AlertRuleScopeType
  serverId: string
  groupId: string
  scopeName: string
  repeatIntervalSeconds: number
  channelIds: string[]
  params: Record<string, string>
}

const ruleTypes: AlertRuleType[] = [
  'server_offline',
  'cpu_high',
  'mem_high',
  'disk_high',
  'container_down',
  'container_unhealthy',
  'check_failed',
  'cert_expiry',
]

const repeatIntervals = [0, 300, 900, 3600, 21600, 86400]

export interface RuleParamField {
  key: string
  min: number
  max: number
}

export const ruleParamFields: Record<AlertRuleType, RuleParamField[]> = {
  server_offline: [{ key: 'seconds', min: 0, max: 86400 }],
  cpu_high: [
    { key: 'threshold', min: 1, max: 100 },
    { key: 'for_seconds', min: 0, max: 86400 },
  ],
  mem_high: [
    { key: 'threshold', min: 1, max: 100 },
    { key: 'for_seconds', min: 0, max: 86400 },
  ],
  disk_high: [
    { key: 'threshold', min: 1, max: 100 },
    { key: 'for_seconds', min: 0, max: 86400 },
  ],
  container_down: [{ key: 'seconds', min: 0, max: 86400 }],
  container_unhealthy: [{ key: 'seconds', min: 0, max: 86400 }],
  check_failed: [{ key: 'consecutive_failures', min: 1, max: 20 }],
  cert_expiry: [{ key: 'days', min: 1, max: 365 }],
}

const defaultParams: Record<string, number> = {
  seconds: 300,
  threshold: 90,
  for_seconds: 0,
  consecutive_failures: 2,
  days: 30,
}

export function ruleParamDefaults(type: AlertRuleType): Record<string, string> {
  return Object.fromEntries(
    ruleParamFields[type].map((field) => [field.key, String(defaultParams[field.key] ?? 0)]),
  )
}

/** ruleParamValues turns the numeric params of a rule into form strings. */
export function ruleParamValues(
  type: AlertRuleType,
  params: Record<string, number>,
): Record<string, string> {
  return Object.fromEntries(
    ruleParamFields[type].map((field) => [
      field.key,
      String(params[field.key] ?? ruleParamDefaults(type)[field.key] ?? 0),
    ]),
  )
}

/** ruleParamNumbers reads validated form values as integers. */
export function ruleParamNumbers(
  type: AlertRuleType,
  values: Record<string, string>,
): Record<string, number> {
  return Object.fromEntries(
    ruleParamFields[type].map((field) => [field.key, Number(values[field.key])]),
  )
}

/** ruleParamsValid checks the same bounds as the core. */
export function ruleParamsValid(type: AlertRuleType, values: Record<string, string>): boolean {
  return ruleParamFields[type].every((field) => {
    const value = Number(values[field.key])
    return (
      values[field.key] !== '' &&
      Number.isInteger(value) &&
      value >= field.min &&
      value <= field.max
    )
  })
}

interface RuleParamFieldsProps {
  type: AlertRuleType
  values: Record<string, string>
  invalid?: boolean
  onChange: (key: string, value: string) => void
}

/** RuleParamFields renders the parameter inputs shared by rule forms. */
export function RuleParamFields({ type, values, invalid = false, onChange }: RuleParamFieldsProps) {
  const { t } = useTranslation('alertsSettings')
  return (
    <div className="alerts-admin__grid">
      {ruleParamFields[type].map((field) => (
        <label className="field" key={field.key}>
          <span className="field__label">{t(`params.${field.key}`)}</span>
          <input
            className="field__input"
            type="number"
            min={field.min}
            max={field.max}
            step={1}
            value={values[field.key] ?? ''}
            aria-invalid={invalid}
            onChange={(event) => onChange(field.key, event.target.value)}
          />
          <span className="settings__hint">
            {t(`params.${field.key}Hint`, { min: field.min, max: field.max })}
          </span>
        </label>
      ))}
    </div>
  )
}

function formOf(rule: AlertRule | null): FormState {
  if (rule === null) {
    return {
      name: '',
      type: 'cpu_high',
      enabled: true,
      severity: 'warning',
      scopeType: 'organization',
      serverId: '',
      groupId: '',
      scopeName: '',
      repeatIntervalSeconds: 0,
      channelIds: [],
      params: ruleParamDefaults('cpu_high'),
    }
  }
  return {
    name: rule.name,
    type: rule.type,
    enabled: rule.enabled,
    severity: rule.severity,
    scopeType: rule.scopeType,
    serverId: rule.serverId ?? '',
    groupId: rule.groupId ?? '',
    scopeName: rule.scopeName ?? '',
    repeatIntervalSeconds: rule.repeatIntervalSeconds,
    channelIds: rule.channelIds,
    params: ruleParamValues(rule.type, rule.params),
  }
}

/** RuleForm creates or edits an alert rule. */
export function RuleForm({ rule, servers, groups, channels, onClose, onSave }: RuleFormProps) {
  const { t } = useTranslation(['alertsSettings', 'errors'])
  const [form, setForm] = useState<FormState>(() => formOf(rule))
  const [showErrors, setShowErrors] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  const nameInvalid = form.name.trim() === '' || form.name.trim().length > 200
  const scopeInvalid =
    (form.scopeType === 'server' && form.serverId === '') ||
    (form.scopeType === 'group' && form.groupId === '') ||
    ((form.scopeType === 'stack' || form.scopeType === 'container') &&
      (form.serverId === '' || form.scopeName.trim() === ''))
  const badParams = !ruleParamsValid(form.type, form.params)
  const valid = !nameInvalid && !scopeInvalid && !badParams

  const changeType = (type: AlertRuleType) => {
    setForm({ ...form, type, params: ruleParamDefaults(type) })
  }

  const submit = async () => {
    if (!valid) {
      setShowErrors(true)
      return
    }
    const params = ruleParamNumbers(form.type, form.params)
    setPending(true)
    setSaveError(null)
    try {
      await onSave({
        name: form.name.trim(),
        type: form.type,
        enabled: form.enabled,
        severity: form.severity,
        params,
        scopeType: form.scopeType,
        serverId:
          form.scopeType === 'group' || form.scopeType === 'organization' ? null : form.serverId,
        groupId: form.scopeType === 'group' ? form.groupId : null,
        scopeName:
          form.scopeType === 'stack' || form.scopeType === 'container'
            ? form.scopeName.trim()
            : null,
        channelIds: form.channelIds,
        repeatIntervalSeconds: form.repeatIntervalSeconds,
      })
    } catch (cause) {
      setSaveError(errorMessage(t, cause))
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog
      title={
        rule === null ? t('alertsSettings:rules.createTitle') : t('alertsSettings:rules.editTitle')
      }
      onClose={onClose}
      footer={
        <div className="alerts-admin__actions">
          <button type="button" className="settings__button" onClick={onClose}>
            {t('alertsSettings:actions.cancel')}
          </button>
          <button
            type="button"
            className="settings__button settings__button--primary"
            disabled={pending}
            onClick={() => void submit()}
          >
            {t('alertsSettings:actions.save')}
          </button>
        </div>
      }
    >
      <form
        className="alerts-admin__form"
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
      >
        {showErrors && !valid ? (
          <p className="settings__message settings__message--error" role="alert">
            {t('alertsSettings:validation.fixFields')}
          </p>
        ) : null}

        <label className="field">
          <span className="field__label">{t('alertsSettings:rules.form.name')}</span>
          <input
            className="field__input"
            value={form.name}
            maxLength={200}
            aria-invalid={showErrors && nameInvalid}
            onChange={(event) => setForm({ ...form, name: event.target.value })}
          />
          {showErrors && nameInvalid ? (
            <span className="field__error">{t('alertsSettings:validation.name')}</span>
          ) : null}
        </label>

        <div className="alerts-admin__grid">
          <label className="field">
            <span className="field__label">{t('alertsSettings:rules.form.type')}</span>
            <select
              className="field__input"
              value={form.type}
              onChange={(event) => changeType(event.target.value as AlertRuleType)}
            >
              {ruleTypes.map((type) => (
                <option key={type} value={type}>
                  {t(`alertsSettings:types.${type}.name`)}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span className="field__label">{t('alertsSettings:rules.form.severity')}</span>
            <select
              className="field__input"
              value={form.severity}
              onChange={(event) =>
                setForm({ ...form, severity: event.target.value as AlertRuleSeverity })
              }
            >
              <option value="warning">{t('alertsSettings:severity.warning')}</option>
              <option value="critical">{t('alertsSettings:severity.critical')}</option>
            </select>
          </label>
        </div>
        <p className="settings__hint">{t(`alertsSettings:types.${form.type}.hint`)}</p>

        <div className="alerts-admin__grid">
          <label className="field">
            <span className="field__label">{t('alertsSettings:rules.form.scope')}</span>
            <select
              className="field__input"
              value={form.scopeType}
              onChange={(event) =>
                setForm({ ...form, scopeType: event.target.value as AlertRuleScopeType })
              }
            >
              {(['organization', 'group', 'server', 'stack', 'container'] as const).map((scope) => (
                <option key={scope} value={scope}>
                  {t(`alertsSettings:scopes.${scope}`)}
                </option>
              ))}
            </select>
          </label>
          {form.scopeType === 'group' ? (
            <label className="field">
              <span className="field__label">{t('alertsSettings:rules.form.group')}</span>
              <select
                className="field__input"
                value={form.groupId}
                aria-invalid={showErrors && scopeInvalid}
                onChange={(event) => setForm({ ...form, groupId: event.target.value })}
              >
                <option value="">{t('alertsSettings:rules.form.chooseGroup')}</option>
                {groups.map((group) => (
                  <option key={group.id} value={group.id}>
                    {group.name}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          {form.scopeType === 'server' ||
          form.scopeType === 'stack' ||
          form.scopeType === 'container' ? (
            <label className="field">
              <span className="field__label">{t('alertsSettings:rules.form.server')}</span>
              <select
                className="field__input"
                value={form.serverId}
                aria-invalid={showErrors && scopeInvalid}
                onChange={(event) => setForm({ ...form, serverId: event.target.value })}
              >
                <option value="">{t('alertsSettings:rules.form.chooseServer')}</option>
                {servers.map((server) => (
                  <option key={server.id} value={server.id}>
                    {server.name}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          {form.scopeType === 'stack' || form.scopeType === 'container' ? (
            <label className="field">
              <span className="field__label">
                {form.scopeType === 'stack'
                  ? t('alertsSettings:rules.form.stackName')
                  : t('alertsSettings:rules.form.containerName')}
              </span>
              <input
                className="field__input"
                value={form.scopeName}
                aria-invalid={showErrors && scopeInvalid}
                onChange={(event) => setForm({ ...form, scopeName: event.target.value })}
              />
            </label>
          ) : null}
        </div>
        {showErrors && scopeInvalid ? (
          <span className="field__error">{t('alertsSettings:validation.scope')}</span>
        ) : null}

        <fieldset className="alerts-admin__fieldset">
          <legend className="field__label">{t('alertsSettings:rules.form.parameters')}</legend>
          <RuleParamFields
            type={form.type}
            values={form.params}
            invalid={showErrors && badParams}
            onChange={(key, value) =>
              setForm({ ...form, params: { ...form.params, [key]: value } })
            }
          />
          {showErrors && badParams ? (
            <span className="field__error">{t('alertsSettings:validation.params')}</span>
          ) : null}
        </fieldset>

        <label className="field">
          <span className="field__label">{t('alertsSettings:rules.form.repeat')}</span>
          <select
            className="field__input"
            value={form.repeatIntervalSeconds}
            onChange={(event) =>
              setForm({ ...form, repeatIntervalSeconds: Number(event.target.value) })
            }
          >
            {repeatIntervals.map((seconds) => (
              <option key={seconds} value={seconds}>
                {t(`alertsSettings:repeat.${seconds}`)}
              </option>
            ))}
          </select>
        </label>

        <fieldset className="alerts-admin__fieldset">
          <legend className="field__label">{t('alertsSettings:rules.form.channels')}</legend>
          <p className="settings__hint">{t('alertsSettings:rules.form.channelsHint')}</p>
          {channels.length === 0 ? (
            <p className="settings__hint">{t('alertsSettings:rules.form.noChannels')}</p>
          ) : (
            <div className="alerts-admin__choices">
              {channels.map((channel) => (
                <label className="alerts-admin__choice" key={channel.id}>
                  <input
                    type="checkbox"
                    checked={form.channelIds.includes(channel.id)}
                    onChange={(event) => {
                      const next = event.target.checked
                        ? [...form.channelIds, channel.id]
                        : form.channelIds.filter((id) => id !== channel.id)
                      setForm({ ...form, channelIds: next })
                    }}
                  />
                  <span>{channel.name}</span>
                  <span className="alerts-admin__choice-type">
                    {t(`alertsSettings:channels.types.${channel.type}`)}
                  </span>
                </label>
              ))}
            </div>
          )}
        </fieldset>

        <label className="alerts-admin__switch">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(event) => setForm({ ...form, enabled: event.target.checked })}
          />
          <span>{t('alertsSettings:rules.form.enabled')}</span>
        </label>

        {saveError !== null ? (
          <p className="settings__message settings__message--error" role="alert">
            {saveError}
          </p>
        ) : null}
      </form>
    </Dialog>
  )
}
