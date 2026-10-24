import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type {
  AlertCheck,
  AlertCheckInput,
  AlertsAdminApi,
  CheckResult,
} from '../../api/alertsAdmin'
import type { ServerSummary } from '../../api/servers'
import { errorMessage } from '../../auth/AuthGate'
import { PlusIcon, TrashIcon } from '../../components/icons'
import { Dialog } from '../users/Dialog'
import { CheckForm } from './CheckForm'

// One clock for the card: certificate days do not need to tick per render.
const loadedAt = Date.now()

interface ChecksCardProps {
  api: AlertsAdminApi
  servers: ServerSummary[]
}

function inputOf(check: AlertCheck, enabled = check.enabled): AlertCheckInput {
  return {
    name: check.name,
    kind: check.kind,
    target: check.target,
    config: check.config,
    intervalSeconds: check.intervalSeconds,
    enabled,
    serverId: check.serverId,
  }
}

function ResultBadge({ result, now }: { result: CheckResult | null; now: number }) {
  const { t } = useTranslation('alertsSettings')
  if (result === null) {
    return <span className="settings__hint">{t('checks.result.never')}</span>
  }
  const certDays =
    result.certNotAfter === null
      ? null
      : Math.ceil((new Date(result.certNotAfter).getTime() - now) / 86_400_000)
  return (
    <div className="alerts-admin__result">
      <span
        className={`settings__badge ${result.ok ? 'settings__badge--green' : 'settings__badge--red'}`}
      >
        {result.ok ? t('checks.result.ok') : t('checks.result.fail')}
      </span>
      <span>{t('checks.result.latency', { ms: result.latencyMs })}</span>
      {result.statusCode !== null ? (
        <span>{t('checks.result.status', { code: result.statusCode })}</span>
      ) : null}
      {certDays !== null ? <span>{t('checks.result.certDays', { days: certDays })}</span> : null}
      {result.error !== '' ? (
        <span className="alerts-admin__error-text" title={result.error}>
          {result.error}
        </span>
      ) : null}
    </div>
  )
}

/** ChecksCard manages the active checks that feed alert rules. */
export function ChecksCard({ api, servers }: ChecksCardProps) {
  const { t } = useTranslation(['alertsSettings', 'errors'])
  const [checks, setChecks] = useState<AlertCheck[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [runNotice, setRunNotice] = useState<{ ok: boolean; text: string } | null>(null)
  const [editing, setEditing] = useState<AlertCheck | 'new' | null>(null)
  const [deleting, setDeleting] = useState<AlertCheck | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [running, setRunning] = useState<string | null>(null)
  const [toggling, setToggling] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      setChecks(await api.listChecks())
    } catch (cause) {
      setError(errorMessage(t, cause))
    }
  }, [api, t])

  useEffect(() => {
    let active = true
    queueMicrotask(() => {
      api.listChecks().then(
        (rows) => {
          if (active) {
            setChecks(rows)
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
  }, [api, t])

  const save = async (input: AlertCheckInput) => {
    if (editing === null) return
    if (editing === 'new') {
      await api.createCheck(input)
      setNotice(t('alertsSettings:checks.created'))
    } else {
      await api.updateCheck(editing.id, input)
      setNotice(t('alertsSettings:checks.updated'))
    }
    setEditing(null)
    await load()
  }

  const toggle = async (check: AlertCheck, enabled: boolean) => {
    setToggling(check.id)
    setError(null)
    setNotice(null)
    try {
      const next = await api.updateCheck(check.id, inputOf(check, enabled))
      setChecks(
        (rows) =>
          rows?.map((row) =>
            row.id === next.id ? { ...next, latestResult: row.latestResult } : row,
          ) ?? rows,
      )
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setToggling(null)
    }
  }

  const run = async (check: AlertCheck) => {
    setRunning(check.id)
    setError(null)
    setRunNotice(null)
    try {
      const result = await api.runCheck(check.id)
      setChecks(
        (rows) =>
          rows?.map((row) => (row.id === check.id ? { ...row, latestResult: result } : row)) ??
          rows,
      )
      setRunNotice({
        ok: result.ok,
        text: result.ok
          ? t('alertsSettings:checks.runOk', { name: check.name })
          : t('alertsSettings:checks.runFail', {
              name: check.name,
              error: result.error || t('alertsSettings:checks.result.unknownError'),
            }),
      })
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setRunning(null)
    }
  }

  const confirmDelete = async () => {
    if (deleting === null) return
    setDeleteBusy(true)
    setDeleteError(null)
    try {
      await api.deleteCheck(deleting.id)
      setDeleting(null)
      setNotice(t('alertsSettings:checks.deleted'))
      await load()
    } catch (cause) {
      setDeleteError(errorMessage(t, cause))
    } finally {
      setDeleteBusy(false)
    }
  }

  return (
    <section className="settings__card alerts-admin__card" aria-labelledby="settings-checks-title">
      <div className="settings__card-head">
        <div>
          <h3 className="settings__card-title" id="settings-checks-title">
            {t('alertsSettings:checks.title')}
          </h3>
          <p className="settings__hint">{t('alertsSettings:checks.intro')}</p>
        </div>
        <button
          type="button"
          className="settings__button settings__button--primary"
          onClick={() => setEditing('new')}
        >
          <PlusIcon />
          {t('alertsSettings:checks.add')}
        </button>
      </div>

      {notice !== null ? (
        <p className="settings__notice" role="status">
          {notice}
        </p>
      ) : null}
      {runNotice !== null ? (
        <p
          className={runNotice.ok ? 'settings__notice' : 'settings__notice settings__notice--error'}
          role={runNotice.ok ? 'status' : 'alert'}
        >
          {runNotice.text}
        </p>
      ) : null}
      {error !== null ? (
        <p className="settings__message settings__message--error" role="alert">
          {error}
        </p>
      ) : null}

      {checks === null ? (
        error === null ? (
          <p className="settings__message" role="status">
            {t('alertsSettings:checks.loading')}
          </p>
        ) : null
      ) : checks.length === 0 ? (
        <p className="settings__message">{t('alertsSettings:checks.empty')}</p>
      ) : (
        <div className="users-table-wrap">
          <table className="users-table alerts-admin__table">
            <thead>
              <tr>
                <th scope="col">{t('alertsSettings:checks.table.name')}</th>
                <th scope="col">{t('alertsSettings:checks.table.kind')}</th>
                <th scope="col">{t('alertsSettings:checks.table.target')}</th>
                <th scope="col">{t('alertsSettings:checks.table.interval')}</th>
                <th scope="col">{t('alertsSettings:checks.table.result')}</th>
                <th scope="col">{t('alertsSettings:checks.table.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {checks.map((check) => (
                <tr key={check.id}>
                  <td data-label={t('alertsSettings:checks.table.name')}>
                    <span className="alerts-admin__name">{check.name}</span>
                    <label className="alerts-admin__switch alerts-admin__switch--inline">
                      <input
                        type="checkbox"
                        checked={check.enabled}
                        disabled={toggling === check.id}
                        aria-label={t('alertsSettings:checks.toggle', { name: check.name })}
                        onChange={(event) => void toggle(check, event.target.checked)}
                      />
                      <span>
                        {check.enabled
                          ? t('alertsSettings:common.enabled')
                          : t('alertsSettings:common.disabled')}
                      </span>
                    </label>
                  </td>
                  <td data-label={t('alertsSettings:checks.table.kind')}>
                    {t(`alertsSettings:checks.kinds.${check.kind}`)}
                  </td>
                  <td data-label={t('alertsSettings:checks.table.target')}>
                    <code className="alerts-admin__code">{check.target}</code>
                  </td>
                  <td data-label={t('alertsSettings:checks.table.interval')}>
                    {t('alertsSettings:checks.interval', { seconds: check.intervalSeconds })}
                  </td>
                  <td data-label={t('alertsSettings:checks.table.result')}>
                    <ResultBadge result={check.latestResult} now={loadedAt} />
                  </td>
                  <td data-label={t('alertsSettings:checks.table.actions')}>
                    <div className="alerts-admin__row-actions">
                      <button
                        type="button"
                        className="settings__button"
                        disabled={running === check.id}
                        onClick={() => void run(check)}
                      >
                        {running === check.id
                          ? t('alertsSettings:checks.running')
                          : t('alertsSettings:checks.run')}
                      </button>
                      <button
                        type="button"
                        className="settings__button"
                        onClick={() => setEditing(check)}
                      >
                        {t('alertsSettings:actions.edit')}
                      </button>
                      <button
                        type="button"
                        className="settings__button settings__button--danger"
                        onClick={() => {
                          setDeleteError(null)
                          setDeleting(check)
                        }}
                      >
                        <TrashIcon />
                        {t('alertsSettings:actions.delete')}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {editing !== null ? (
        <CheckForm
          key={editing === 'new' ? 'new' : editing.id}
          check={editing === 'new' ? null : editing}
          servers={servers}
          onClose={() => setEditing(null)}
          onSave={save}
        />
      ) : null}

      {deleting !== null ? (
        <Dialog
          title={t('alertsSettings:checks.deleteTitle', { name: deleting.name })}
          onClose={() => setDeleting(null)}
          footer={
            <div className="alerts-admin__actions">
              <button type="button" className="settings__button" onClick={() => setDeleting(null)}>
                {t('alertsSettings:actions.cancel')}
              </button>
              <button
                type="button"
                className="settings__button settings__button--danger"
                disabled={deleteBusy}
                onClick={() => void confirmDelete()}
              >
                {t('alertsSettings:actions.delete')}
              </button>
            </div>
          }
        >
          <p>{t('alertsSettings:checks.deleteBody')}</p>
          {deleteError !== null ? (
            <p className="settings__message settings__message--error" role="alert">
              {deleteError}
            </p>
          ) : null}
        </Dialog>
      ) : null}
    </section>
  )
}
