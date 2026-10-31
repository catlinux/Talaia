import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { ProxyAction, ProxyStatus, WizardApi } from '../../api/apps'
import { isSignedOut } from '../../api/http'
import { errorMessage } from '../../auth/AuthGate'
import { usePermissions } from '../../auth/permissions'
import { ConfirmDialog, StatusBadge } from '../../components/ui'
import './proxy-mode-card.css'

interface ProxyModeCardProps {
  api: WizardApi
  serverId: string
}

/** ProxyModeCard shows how apps are published and controls the managed proxy. */
export function ProxyModeCard({ api, serverId }: ProxyModeCardProps) {
  const { t } = useTranslation(['server', 'errors'])
  const { can, loading: permissionsLoading } = usePermissions()
  const canManage = !permissionsLoading && can('server.proxy', { serverId })
  const [proxy, setProxy] = useState<ProxyStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [busy, setBusy] = useState<ProxyAction | null>(null)
  const [notice, setNotice] = useState('')
  const [actionError, setActionError] = useState('')
  const [confirmRemove, setConfirmRemove] = useState(false)

  const load = useCallback(async () => {
    if (!canManage) return
    try {
      const status = await api.proxy(serverId)
      setProxy(status)
      setLoadError(false)
    } catch (error) {
      if (!isSignedOut(error)) setLoadError(true)
    } finally {
      setLoading(false)
    }
  }, [api, canManage, serverId])

  useEffect(() => {
    if (!canManage) return
    queueMicrotask(() => {
      void load()
    })
  }, [canManage, load])

  const run = async (action: ProxyAction) => {
    setBusy(action)
    setNotice('')
    setActionError('')
    try {
      await api.proxyAction(serverId, action)
      setNotice(t('server:proxy.queued'))
      await load()
    } catch (error) {
      if (!isSignedOut(error)) {
        setActionError(t('server:proxy.failed', { message: errorMessage(t, error) }))
      }
    } finally {
      setBusy(null)
    }
  }

  if (!canManage) return null

  const portsFree = proxy?.port80Free === true && proxy?.port443Free === true
  const portsBusy = proxy?.port80Free === false || proxy?.port443Free === false

  return (
    <section className="proxy-card">
      <div className="proxy-card__head">
        <div>
          <h2>{t('server:proxy.title')}</h2>
          <p>{t('server:proxy.intro')}</p>
        </div>
        {proxy !== null ? (
          <StatusBadge
            status={proxy.mode === 'managed' ? 'ok' : 'unknown'}
            label={t(proxy.mode === 'managed' ? 'server:proxy.managed' : 'server:proxy.external')}
          />
        ) : null}
      </div>

      {loading && proxy === null ? (
        <p className="proxy-card__muted" role="status">
          {t('server:proxy.loading')}
        </p>
      ) : loadError && proxy === null ? (
        <div className="proxy-card__error" role="alert">
          <p>{t('server:proxy.error')}</p>
          <button type="button" className="ui-btn" onClick={() => void load()}>
            {t('server:proxy.refresh')}
          </button>
        </div>
      ) : proxy !== null ? (
        <>
          {proxy.mode === 'managed' ? (
            <>
              <p>{t('server:proxy.managedHint')}</p>
              {proxy.caddyVersion !== undefined && proxy.caddyVersion !== '' ? (
                <p className="proxy-card__muted">
                  {t('server:proxy.caddyVersion', { version: proxy.caddyVersion })}
                </p>
              ) : null}
              <div className="proxy-card__actions">
                <button
                  type="button"
                  className="ui-btn"
                  disabled={busy !== null}
                  onClick={() => void run('apply')}
                >
                  {t('server:proxy.apply')}
                </button>
                <button
                  type="button"
                  className="ui-btn ui-btn--danger"
                  disabled={busy !== null}
                  onClick={() => setConfirmRemove(true)}
                >
                  {t('server:proxy.remove')}
                </button>
              </div>
            </>
          ) : (
            <>
              <p>{t('server:proxy.externalHint')}</p>
              <div className="proxy-card__actions">
                {portsFree ? (
                  <button
                    type="button"
                    className="ui-btn ui-btn--primary"
                    disabled={busy !== null}
                    onClick={() => void run('install')}
                  >
                    {t('server:proxy.install')}
                  </button>
                ) : (
                  <p className="proxy-card__muted">
                    {portsBusy ? t('server:proxy.portsBusy') : t('server:proxy.portsUnknown')}
                  </p>
                )}
              </div>
            </>
          )}
          {notice !== '' ? (
            <p className="proxy-card__notice" role="status">
              {notice}
            </p>
          ) : null}
          {actionError !== '' ? (
            <p className="proxy-card__error" role="alert">
              {actionError}
            </p>
          ) : null}
          <div className="proxy-card__foot">
            <button
              type="button"
              className="ui-btn ui-btn--ghost"
              disabled={busy !== null}
              onClick={() => void load()}
            >
              {t('server:proxy.refresh')}
            </button>
          </div>
        </>
      ) : null}

      <ConfirmDialog
        open={confirmRemove}
        title={t('server:proxy.removeTitle')}
        body={t('server:proxy.removeBody')}
        confirmLabel={t('server:proxy.removeYes')}
        danger
        busy={busy !== null}
        onConfirm={() => {
          setConfirmRemove(false)
          void run('remove')
        }}
        onCancel={() => setConfirmRemove(false)}
      />
    </section>
  )
}
