import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { AlertAdminSettings, AlertsAdminApi } from '../../api/alertsAdmin'
import type { Group } from '../../api/groups'
import type { ServerSummary } from '../../api/servers'
import { errorMessage } from '../../auth/AuthGate'
import { usePermissions } from '../../auth/permissions'
import { ChannelsCard } from './ChannelsCard'
import { ChecksCard } from './ChecksCard'
import { RulesCard } from './RulesCard'

interface AlertsSettingsProps {
  api: AlertsAdminApi
  servers: ServerSummary[]
  groups: Group[]
}

/** PrivateTargetsCard edits the private-network policy of checks and channels. */
export function PrivateTargetsCard({ api }: { api: AlertsAdminApi }) {
  const { t } = useTranslation(['alertsSettings', 'errors'])
  const [settings, setSettings] = useState<AlertAdminSettings | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    let active = true
    api.getSettings().then(
      (next) => {
        if (active) setSettings(next)
      },
      (cause: unknown) => {
        if (active) setError(errorMessage(t, cause))
      },
    )
    return () => {
      active = false
    }
  }, [api, t])

  const change = async (allowed: boolean) => {
    setSaving(true)
    setError(null)
    setNotice(null)
    try {
      const next = await api.updateSettings(allowed)
      setSettings(next)
      setNotice(t('alertsSettings:privateTargets.saved'))
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section
      className="settings__card alerts-admin__card"
      aria-labelledby="settings-private-targets-title"
    >
      <div>
        <h3 className="settings__card-title" id="settings-private-targets-title">
          {t('alertsSettings:privateTargets.title')}
        </h3>
        <p className="settings__hint">{t('alertsSettings:privateTargets.intro')}</p>
      </div>

      {settings === null ? (
        error === null ? (
          <p className="settings__message" role="status">
            {t('alertsSettings:privateTargets.loading')}
          </p>
        ) : null
      ) : (
        <label className="alerts-admin__policy">
          <input
            type="checkbox"
            checked={settings.allowPrivateTargets}
            disabled={saving}
            onChange={(event) => void change(event.target.checked)}
          />
          <span>{t('alertsSettings:privateTargets.allow')}</span>
        </label>
      )}

      <p className="settings__message settings__message--warning">
        {t('alertsSettings:privateTargets.warning')}
      </p>
      <p className="settings__hint">{t('alertsSettings:privateTargets.cloudMetadata')}</p>

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
    </section>
  )
}

/** AlertsSettings groups the alert rules, checks, channels and private-target policy. */
export function AlertsSettings({ api, servers, groups }: AlertsSettingsProps) {
  const { t } = useTranslation('alertsSettings')
  const { can, loading } = usePermissions()
  if (loading || !can('alerts.manage' as never)) return null
  return (
    <section className="alerts-settings" aria-labelledby="settings-alerts-title">
      <header className="alerts-settings__head">
        <p className="alerts-settings__eyebrow">{t('eyebrow')}</p>
        <h2 className="alerts-settings__title" id="settings-alerts-title">
          {t('title')}
        </h2>
        <p className="settings__hint">{t('intro')}</p>
      </header>
      <RulesCard api={api} servers={servers} groups={groups} />
      <ChecksCard api={api} servers={servers} />
      <ChannelsCard api={api} />
      <PrivateTargetsCard api={api} />
    </section>
  )
}
