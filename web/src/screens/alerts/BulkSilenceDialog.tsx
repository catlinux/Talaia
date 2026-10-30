import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Alert, AlertsApi } from '../../api/alerts'
import { errorMessage } from '../../auth/AuthGate'
import { Dialog } from '../users/Dialog'
import { distinctSilenceScopes } from './inboxModel'

interface BulkSilenceDialogProps {
  alerts: Alert[]
  api: AlertsApi
  onClose: () => void
  onFinished: (result: { done: number; failed: number }) => void
}

const presets = [
  { minutes: 60, key: 'hour1' },
  { minutes: 240, key: 'hours4' },
  { minutes: 1440, key: 'hours24' },
]

/** BulkSilenceDialog creates one silence for each distinct scope in the selection. */
export function BulkSilenceDialog({ alerts, api, onClose, onFinished }: BulkSilenceDialogProps) {
  const { t } = useTranslation(['alerts', 'errors'])
  const scopes = distinctSilenceScopes(alerts)
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
    let done = 0
    let failed = 0
    let lastError: unknown = null
    for (const scope of scopes) {
      try {
        await api.createSilence({
          scopeType: scope.scopeType,
          serverId: scope.serverId,
          name: scope.name,
          ruleType: scope.ruleType,
          durationMinutes: duration,
          reason: trimmedReason,
        })
        done += 1
      } catch (cause) {
        failed += 1
        lastError = cause
      }
    }
    if (failed > 0 && done === 0) {
      setError(errorMessage(t, lastError))
      setPending(false)
      return
    }
    onFinished({ done, failed })
  }

  return (
    <Dialog
      title={t('alerts:bulkSilence.title')}
      onClose={onClose}
      footer={
        <div className="alerts__actions">
          <button type="button" className="cbtn" onClick={onClose} disabled={pending}>
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
          {t('alerts:bulkSilence.scopeCount', { count: scopes.length })}
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
