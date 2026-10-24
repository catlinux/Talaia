import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Alert, AlertsApi } from '../../api/alerts'
import { errorMessage } from '../../auth/AuthGate'
import { Dialog } from '../users/Dialog'

interface SilenceDialogProps {
  alert: Alert
  api: AlertsApi
  onClose: () => void
  onDone: () => void
}

const presets = [
  { minutes: 30, key: 'minutes30' },
  { minutes: 60, key: 'hour1' },
  { minutes: 240, key: 'hours4' },
  { minutes: 1440, key: 'hours24' },
  { minutes: 10080, key: 'days7' },
]

function silenceScope(alert: Alert): { scopeType: string; serverId?: string; name?: string } {
  if (!alert.serverId) return { scopeType: 'organization' }
  if (alert.subjectKind === 'container') {
    return { scopeType: 'container', serverId: alert.serverId, name: alert.subjectName }
  }
  if (alert.stack !== '') {
    return { scopeType: 'stack', serverId: alert.serverId, name: alert.stack }
  }
  return { scopeType: 'server', serverId: alert.serverId }
}

/** SilenceDialog creates a silence for one alert. */
export function SilenceDialog({ alert, api, onClose, onDone }: SilenceDialogProps) {
  const { t } = useTranslation(['alerts', 'errors'])
  const scope = silenceScope(alert)
  const [preset, setPreset] = useState(60)
  const [custom, setCustom] = useState('')
  const [reason, setReason] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  const duration = custom === '' ? preset : Number(custom)
  const trimmedReason = reason.trim()
  const valid =
    Number.isInteger(duration) &&
    duration >= 1 &&
    duration <= 43200 &&
    trimmedReason !== '' &&
    trimmedReason.length <= 200

  const submit = async () => {
    if (!valid) {
      setError(t('alerts:silence.invalid'))
      return
    }
    setPending(true)
    setError(null)
    try {
      await api.createSilence({
        scopeType: scope.scopeType,
        serverId: scope.serverId,
        name: scope.name,
        ruleType: alert.ruleType || undefined,
        durationMinutes: duration,
        reason: trimmedReason,
      })
      onDone()
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setPending(false)
    }
  }

  const scopeName =
    scope.scopeType === 'container'
      ? alert.subjectName
      : scope.scopeType === 'stack'
        ? alert.stack
        : scope.scopeType === 'server'
          ? alert.serverName
          : ''

  return (
    <Dialog
      title={t('alerts:silence.title')}
      onClose={onClose}
      footer={
        <div className="alerts__actions">
          <button type="button" className="cbtn" onClick={onClose}>
            {t('alerts:actions.cancel')}
          </button>
          <button
            type="button"
            className="cbtn cbtn--primary"
            disabled={pending}
            onClick={() => void submit()}
          >
            {t('alerts:actions.silence')}
          </button>
        </div>
      }
    >
      <form
        className="alerts__form"
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
      >
        <p className="alerts__scope">
          {t('alerts:silence.scope')}:{' '}
          {t(`alerts:scope.${scope.scopeType}`, { defaultValue: scope.scopeType })}
          {scopeName ? ` · ${scopeName}` : ''}
        </p>
        <label className="alerts__field">
          <span>{t('alerts:silence.duration')}</span>
          <select
            value={custom === '' ? String(preset) : 'custom'}
            onChange={(event) => {
              if (event.target.value === 'custom') {
                setCustom(custom === '' ? '60' : custom)
                return
              }
              setCustom('')
              setPreset(Number(event.target.value))
            }}
          >
            {presets.map((option) => (
              <option key={option.minutes} value={option.minutes}>
                {t(`alerts:silence.${option.key}`)}
              </option>
            ))}
            <option value="custom">{t('alerts:silence.custom')}</option>
          </select>
        </label>
        {custom !== '' ? (
          <label className="alerts__field">
            <span>{t('alerts:silence.customMinutes')}</span>
            <input
              type="number"
              min={1}
              max={43200}
              value={custom}
              onChange={(event) => setCustom(event.target.value)}
            />
          </label>
        ) : null}
        <label className="alerts__field">
          <span>{t('alerts:silence.reason')}</span>
          <textarea
            maxLength={200}
            value={reason}
            placeholder={t('alerts:silence.reasonPlaceholder')}
            onChange={(event) => setReason(event.target.value)}
          />
        </label>
        {error !== null ? (
          <p className="alerts__error" role="alert">
            {error}
          </p>
        ) : null}
      </form>
    </Dialog>
  )
}
