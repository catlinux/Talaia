import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { AlertCheck, AlertCheckInput, CheckKind } from '../../../api/alertsAdmin'
import type { ServerSummary } from '../../../api/servers'
import { errorMessage } from '../../../auth/AuthGate'
import { Dialog } from '../../users/Dialog'

interface CheckFormProps {
  check: AlertCheck | null
  servers: ServerSummary[]
  onClose: () => void
  onSave: (input: AlertCheckInput) => Promise<void>
}

interface FormState {
  name: string
  kind: CheckKind
  target: string
  method: string
  expectedCodes: string
  timeoutSeconds: string
  intervalSeconds: string
  serverId: string
  enabled: boolean
}

function formOf(check: AlertCheck | null): FormState {
  if (check === null) {
    return {
      name: '',
      kind: 'http',
      target: '',
      method: 'GET',
      expectedCodes: '200-399',
      timeoutSeconds: '10',
      intervalSeconds: '60',
      serverId: '',
      enabled: true,
    }
  }
  return {
    name: check.name,
    kind: check.kind,
    target: check.target,
    method: check.config.method,
    expectedCodes: check.config.expectedCodes.join(', '),
    timeoutSeconds: String(check.config.timeoutSeconds),
    intervalSeconds: String(check.intervalSeconds),
    serverId: check.serverId ?? '',
    enabled: check.enabled,
  }
}

function expectedCodes(value: string): string[] {
  return value
    .split(',')
    .map((code) => code.trim())
    .filter((code) => code !== '')
}

function validUrl(value: string): boolean {
  try {
    const parsed = new URL(value)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:'
  } catch {
    return false
  }
}

function invalidCode(code: string): boolean {
  const parts = code.split('-')
  if (parts.length > 2) return true
  const low = Number(parts[0])
  if (!Number.isInteger(low) || low < 100 || low > 599) return true
  if (parts.length === 2) {
    const high = Number(parts[1])
    if (!Number.isInteger(high) || high < low || high > 599) return true
  }
  return false
}

/** CheckForm creates or edits an active check. */
export function CheckForm({ check, servers, onClose, onSave }: CheckFormProps) {
  const { t } = useTranslation(['alertsSettings', 'errors'])
  const [form, setForm] = useState<FormState>(() => formOf(check))
  const [showErrors, setShowErrors] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  const codes = expectedCodes(form.expectedCodes)
  const hostPort = form.kind === 'http' ? null : /^([^:\s]+):(\d+)$/.exec(form.target.trim())
  const targetInvalid =
    form.target.trim() === '' ||
    (form.kind === 'http' && !validUrl(form.target.trim())) ||
    (form.kind !== 'http' &&
      (hostPort === null || Number(hostPort[2]) < 1 || Number(hostPort[2]) > 65535))
  const interval = Number(form.intervalSeconds)
  const timeout = Number(form.timeoutSeconds)
  const intervalInvalid = !Number.isInteger(interval) || interval < 30 || interval > 86400
  const timeoutInvalid = !Number.isInteger(timeout) || timeout < 1 || timeout > 10
  const codesInvalid =
    form.kind === 'http' && (codes.length === 0 || codes.some((code) => invalidCode(code)))
  const nameInvalid = form.name.trim() === '' || form.name.trim().length > 200
  const valid =
    !nameInvalid && !targetInvalid && !intervalInvalid && !timeoutInvalid && !codesInvalid

  const submit = async () => {
    if (!valid) {
      setShowErrors(true)
      return
    }
    setPending(true)
    setSaveError(null)
    try {
      await onSave({
        name: form.name.trim(),
        kind: form.kind,
        target: form.target.trim(),
        config: {
          method: form.method,
          expectedCodes: codes,
          timeoutSeconds: timeout,
        },
        intervalSeconds: interval,
        enabled: form.enabled,
        serverId: form.serverId === '' ? null : form.serverId,
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
        check === null
          ? t('alertsSettings:checks.createTitle')
          : t('alertsSettings:checks.editTitle')
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
          <span className="field__label">{t('alertsSettings:checks.form.name')}</span>
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
            <span className="field__label">{t('alertsSettings:checks.form.kind')}</span>
            <select
              className="field__input"
              value={form.kind}
              onChange={(event) => setForm({ ...form, kind: event.target.value as CheckKind })}
            >
              <option value="http">{t('alertsSettings:checks.kinds.http')}</option>
              <option value="tcp">{t('alertsSettings:checks.kinds.tcp')}</option>
              <option value="tls">{t('alertsSettings:checks.kinds.tls')}</option>
            </select>
          </label>
          <label className="field">
            <span className="field__label">{t('alertsSettings:checks.form.interval')}</span>
            <input
              className="field__input"
              type="number"
              min={30}
              max={86400}
              step={1}
              value={form.intervalSeconds}
              aria-invalid={showErrors && intervalInvalid}
              onChange={(event) => setForm({ ...form, intervalSeconds: event.target.value })}
            />
            <span className="settings__hint">{t('alertsSettings:checks.form.intervalHint')}</span>
          </label>
        </div>

        <label className="field">
          <span className="field__label">{t('alertsSettings:checks.form.target')}</span>
          <input
            className="field__input"
            value={form.target}
            aria-invalid={showErrors && targetInvalid}
            placeholder={t(`alertsSettings:checks.form.targetPlaceholder.${form.kind}`)}
            onChange={(event) => setForm({ ...form, target: event.target.value })}
          />
          <span className="settings__hint">
            {t(`alertsSettings:checks.form.targetHint.${form.kind}`)}
          </span>
          {showErrors && targetInvalid ? (
            <span className="field__error">{t('alertsSettings:validation.target')}</span>
          ) : null}
        </label>

        {form.kind === 'http' ? (
          <div className="alerts-admin__grid">
            <label className="field">
              <span className="field__label">{t('alertsSettings:checks.form.method')}</span>
              <select
                className="field__input"
                value={form.method}
                onChange={(event) => setForm({ ...form, method: event.target.value })}
              >
                <option value="GET">GET</option>
                <option value="HEAD">HEAD</option>
              </select>
            </label>
            <label className="field">
              <span className="field__label">{t('alertsSettings:checks.form.timeout')}</span>
              <input
                className="field__input"
                type="number"
                min={1}
                max={10}
                step={1}
                value={form.timeoutSeconds}
                aria-invalid={showErrors && timeoutInvalid}
                onChange={(event) => setForm({ ...form, timeoutSeconds: event.target.value })}
              />
              <span className="settings__hint">{t('alertsSettings:checks.form.timeoutHint')}</span>
            </label>
            <label className="field">
              <span className="field__label">{t('alertsSettings:checks.form.expectedCodes')}</span>
              <input
                className="field__input"
                value={form.expectedCodes}
                aria-invalid={showErrors && codesInvalid}
                onChange={(event) => setForm({ ...form, expectedCodes: event.target.value })}
              />
              <span className="settings__hint">
                {t('alertsSettings:checks.form.expectedCodesHint')}
              </span>
            </label>
          </div>
        ) : null}
        {showErrors && (timeoutInvalid || codesInvalid) ? (
          <span className="field__error">{t('alertsSettings:validation.http')}</span>
        ) : null}

        <label className="field">
          <span className="field__label">{t('alertsSettings:checks.form.server')}</span>
          <select
            className="field__input"
            value={form.serverId}
            onChange={(event) => setForm({ ...form, serverId: event.target.value })}
          >
            <option value="">{t('alertsSettings:checks.form.anyServer')}</option>
            {servers.map((server) => (
              <option key={server.id} value={server.id}>
                {server.name}
              </option>
            ))}
          </select>
          <span className="settings__hint">{t('alertsSettings:checks.form.serverHint')}</span>
        </label>

        <label className="alerts-admin__switch">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(event) => setForm({ ...form, enabled: event.target.checked })}
          />
          <span>{t('alertsSettings:checks.form.enabled')}</span>
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
