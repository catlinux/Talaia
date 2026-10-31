import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  createAppsApi,
  type App,
  type AppsApi,
  type Deployment,
  type DeploymentDetail,
  type Variable,
  type VariableInput,
} from '../../api/apps'
import { createApiFleetClient, type FleetClient } from '../../api/client'
import { isSignedOut } from '../../api/http'
import { errorMessage } from '../../auth/AuthGate'
import { usePermissions } from '../../auth/permissions'
import {
  ConfirmDialog,
  DataTable,
  Drawer,
  PageHeader,
  StatusBadge,
  useToast,
  type DataTableColumn,
  type Status,
} from '../../components/ui'
import { Link } from '../../router'
import { deploymentDuration, formatDateTime, isActiveDeployment, shortCommit } from './format'
import './apps.css'

export const appDetailRefreshMs = 5_000
export const appLogPollMs = 2_000

const variableKeyPattern = /^[A-Z_][A-Z0-9_]*$/

const deploymentStatus: Record<Deployment['state'], Status> = {
  queued: 'warn',
  building: 'warn',
  releasing: 'warn',
  succeeded: 'ok',
  failed: 'crit',
  superseded: 'unknown',
}

interface AppDetailProps {
  appId: string
  api?: AppsApi
  client?: FleetClient
  refreshMs?: number
  logPollMs?: number
}

interface VariableFormState {
  originalKey: string
  key: string
  value: string
  secret: boolean
  buildTime: boolean
}

/** AppDetail is the deployments, variables and live log page of one app. */
export function AppDetail({
  appId,
  api,
  client,
  refreshMs = appDetailRefreshMs,
  logPollMs = appLogPollMs,
}: AppDetailProps) {
  const { t, i18n } = useTranslation('apps')
  const toast = useToast()
  const { can } = usePermissions()
  const appsApi = useMemo(() => api ?? createAppsApi(), [api])
  const fleetClient = useMemo(() => client ?? createApiFleetClient(), [client])
  const language = i18n.resolvedLanguage ?? i18n.language
  const alive = useRef(true)

  const [app, setApp] = useState<App | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [serverName, setServerName] = useState('')

  const [deployments, setDeployments] = useState<Deployment[] | null>(null)
  const [deploymentsError, setDeploymentsError] = useState(false)
  const [variables, setVariables] = useState<Variable[] | null>(null)
  const [variablesError, setVariablesError] = useState(false)

  const [deploying, setDeploying] = useState(false)
  const [rollingBack, setRollingBack] = useState(false)
  const [rollbackTarget, setRollbackTarget] = useState<Deployment | null>(null)

  const [selected, setSelected] = useState<Deployment | null>(null)
  const [log, setLog] = useState<DeploymentDetail | null>(null)
  const [logLoading, setLogLoading] = useState(false)
  const [logError, setLogError] = useState(false)

  const [form, setForm] = useState<VariableFormState | null>(null)
  const formKey = form?.originalKey ?? null
  const formRef = useRef<HTMLFormElement>(null)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [deleteKey, setDeleteKey] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const loadApp = useCallback(async () => {
    setLoading(true)
    try {
      const value = await appsApi.get(appId)
      if (!alive.current) return
      setApp(value)
      setLoadError(false)
      setLoading(false)
      fleetClient.listServers().then(
        (servers) => {
          if (!alive.current) return
          const found = servers.find((server) => server.id === value.serverId)
          if (found !== undefined) setServerName(found.name)
        },
        () => undefined,
      )
    } catch {
      if (!alive.current) return
      setLoadError(true)
      setLoading(false)
    }
  }, [appsApi, appId, fleetClient])

  const loadDeployments = useCallback(async () => {
    try {
      const list = await appsApi.deployments(appId)
      if (!alive.current) return
      setDeployments(list)
      setDeploymentsError(false)
    } catch {
      if (alive.current) setDeploymentsError(true)
    }
  }, [appsApi, appId])

  const loadVariables = useCallback(async () => {
    try {
      const list = await appsApi.variables(appId)
      if (!alive.current) return
      setVariables(list)
      setVariablesError(false)
    } catch {
      if (alive.current) setVariablesError(true)
    }
  }, [appsApi, appId])

  useEffect(() => {
    queueMicrotask(() => {
      void loadApp()
    })
  }, [loadApp])

  useEffect(() => {
    if (app === null) return
    queueMicrotask(() => {
      void loadDeployments()
      void loadVariables()
    })
  }, [app, loadDeployments, loadVariables])

  const hasActive = (deployments ?? []).some(isActiveDeployment)
  useEffect(() => {
    if (!hasActive || refreshMs <= 0) return
    const timer = window.setInterval(() => {
      if (!document.hidden) void loadDeployments()
    }, refreshMs)
    return () => window.clearInterval(timer)
  }, [hasActive, loadDeployments, refreshMs])

  useEffect(() => {
    if (formKey === null) return
    const node = formRef.current
    node?.scrollIntoView?.({ block: 'center' })
    node?.querySelector<HTMLInputElement>('input:not([disabled])')?.focus()
  }, [formKey])

  useEffect(() => {
    if (selected === null) return
    let active = true
    let timer: number | undefined
    const refresh = async () => {
      try {
        const detail = await appsApi.deployment(appId, selected.id)
        if (!active) return
        setLog(detail)
        setLogLoading(false)
        setLogError(false)
        if (!isActiveDeployment(detail)) {
          if (timer !== undefined) window.clearInterval(timer)
          if (selected.state !== detail.state) void loadDeployments()
        }
      } catch {
        if (!active) return
        setLogError(true)
        setLogLoading(false)
      }
    }
    void refresh()
    if (isActiveDeployment(selected) && logPollMs > 0) {
      timer = window.setInterval(() => {
        if (!document.hidden) void refresh()
      }, logPollMs)
    }
    return () => {
      active = false
      if (timer !== undefined) window.clearInterval(timer)
    }
  }, [appsApi, appId, loadDeployments, logPollMs, selected])

  const scope = app === null ? undefined : { serverId: app.serverId, stack: app.stackName }
  const canDeploy = app !== null && can('stack.deploy', scope)
  const canEdit = app !== null && can('stack.edit', scope)

  const deploy = async (commit = '') => {
    if (app === null || deploying) return
    setDeploying(true)
    try {
      if (commit === '') {
        await appsApi.deploy(app.id)
      } else {
        await appsApi.deploy(app.id, commit)
      }
      toast.success(t('detail.deployQueued'))
      await loadDeployments()
    } catch (error) {
      if (!isSignedOut(error)) {
        toast.error(t('detail.deployFailed', { message: errorMessage(t, error) }))
      }
    } finally {
      if (alive.current) setDeploying(false)
    }
  }

  const rollback = async () => {
    const target = rollbackTarget
    if (app === null || target === null) return
    setRollbackTarget(null)
    setRollingBack(true)
    try {
      await appsApi.rollback(app.id, target.id)
      toast.success(t('detail.rollbackQueued'))
      await loadDeployments()
    } catch (error) {
      if (!isSignedOut(error)) {
        toast.error(t('detail.rollbackFailed', { message: errorMessage(t, error) }))
      }
    } finally {
      if (alive.current) setRollingBack(false)
    }
  }

  const openLog = (deployment: Deployment) => {
    setLog(null)
    setLogError(false)
    setLogLoading(true)
    setSelected(deployment)
  }

  const openAddForm = () => {
    setFormError('')
    setForm({ originalKey: '', key: '', value: '', secret: false, buildTime: false })
  }

  const openEditForm = (variable: Variable) => {
    setFormError('')
    setForm({
      originalKey: variable.key,
      key: variable.key,
      value: variable.secret ? '' : (variable.value ?? ''),
      secret: variable.secret,
      buildTime: variable.buildTime,
    })
  }

  const saveVariable = async () => {
    if (form === null || saving) return
    const key = form.key.trim()
    if (!variableKeyPattern.test(key)) {
      setFormError(t('detail.invalidKey'))
      return
    }
    if (form.secret && form.originalKey !== '' && form.value === '') {
      setFormError(t('detail.secretRequired'))
      return
    }
    const input: VariableInput = {
      value: form.value,
      secret: form.secret,
      buildTime: form.buildTime,
    }
    setSaving(true)
    try {
      await appsApi.setVariable(appId, key, input)
      toast.success(t('detail.variableSaved'))
      setForm(null)
      setFormError('')
      await loadVariables()
    } catch (error) {
      if (!isSignedOut(error)) {
        setFormError(t('detail.variableSaveFailed', { message: errorMessage(t, error) }))
      }
    } finally {
      if (alive.current) setSaving(false)
    }
  }

  const deleteVariable = async () => {
    const key = deleteKey
    if (key === null) return
    setDeleteKey(null)
    setDeleting(true)
    try {
      await appsApi.deleteVariable(appId, key)
      toast.success(t('detail.variableDeleted'))
      await loadVariables()
    } catch (error) {
      if (!isSignedOut(error)) {
        toast.error(t('detail.variableDeleteFailed', { message: errorMessage(t, error) }))
      }
    } finally {
      if (alive.current) setDeleting(false)
    }
  }

  const deploymentColumns: DataTableColumn<Deployment>[] = [
    {
      id: 'number',
      header: t('columns.number'),
      width: 64,
      sortValue: (deployment) => deployment.number,
      cell: (deployment) => <span className="apps__strong">#{deployment.number}</span>,
    },
    {
      id: 'state',
      header: t('columns.state'),
      sortValue: (deployment) => deployment.state,
      cell: (deployment) => (
        <StatusBadge
          status={deploymentStatus[deployment.state]}
          label={t(`deploymentState.${deployment.state}`)}
        />
      ),
    },
    {
      id: 'trigger',
      header: t('columns.trigger'),
      hideOnMobile: true,
      sortValue: (deployment) => deployment.trigger,
      cell: (deployment) =>
        t(`trigger.${deployment.trigger}`, { defaultValue: deployment.trigger }),
    },
    {
      id: 'commit',
      header: t('columns.commit'),
      cell: (deployment) => (
        <span className="apps__commit">
          <code>{shortCommit(deployment.commitSha)}</code>
          {deployment.commitSubject !== '' ? (
            <span className="apps__sub">{deployment.commitSubject}</span>
          ) : null}
        </span>
      ),
    },
    {
      id: 'started',
      header: t('columns.started'),
      hideOnMobile: true,
      sortValue: (deployment) => deployment.startedAt,
      cell: (deployment) => (
        <span className="apps__muted">{formatDateTime(deployment.startedAt, language)}</span>
      ),
    },
    {
      id: 'duration',
      header: t('columns.duration'),
      hideOnMobile: true,
      cell: (deployment) => (
        <span className="apps__muted">{deploymentDuration(deployment, language)}</span>
      ),
    },
    ...(canDeploy
      ? [
          {
            id: 'actions',
            header: t('columns.actions'),
            align: 'right' as const,
            width: 230,
            cell: (deployment: Deployment) => (
              <span className="apps__row-actions">
                <button type="button" className="ui-btn" onClick={() => openLog(deployment)}>
                  {t('detail.logs')}
                </button>
                {deployment.state === 'failed' && deployment.commitSha !== '' ? (
                  <button
                    type="button"
                    className="ui-btn"
                    disabled={deploying || hasActive}
                    onClick={() => void deploy(deployment.commitSha)}
                  >
                    {t('detail.retry')}
                  </button>
                ) : null}
                {deployment.state === 'succeeded' && deployment.commitSha !== '' ? (
                  <button
                    type="button"
                    className="ui-btn"
                    disabled={rollingBack || hasActive}
                    onClick={() => setRollbackTarget(deployment)}
                  >
                    {t('detail.rollback')}
                  </button>
                ) : null}
              </span>
            ),
          },
        ]
      : []),
  ]

  const variableColumns: DataTableColumn<Variable>[] = [
    {
      id: 'key',
      header: t('columns.key'),
      mobilePrimary: true,
      cell: (variable) => (
        <span className="apps__name">
          <code className="apps__strong">{variable.key}</code>
          {variable.secret || variable.buildTime ? (
            <span className="apps__tags apps__mobile-only">
              {variable.secret ? <span className="apps__tag">{t('flags.secret')}</span> : null}
              {variable.buildTime ? (
                <span className="apps__tag">{t('flags.buildTime')}</span>
              ) : null}
            </span>
          ) : null}
        </span>
      ),
    },
    {
      id: 'value',
      header: t('columns.value'),
      cell: (variable) =>
        variable.secret ? (
          <span className="apps__masked" aria-label={t('detail.maskedLabel')}>
            ••••••••
          </span>
        ) : (
          <code className="apps__value">
            {variable.value === undefined || variable.value === '' ? '—' : variable.value}
          </code>
        ),
    },
    {
      id: 'flags',
      header: t('columns.flags'),
      hideOnMobile: true,
      cell: (variable) => (
        <span className="apps__tags">
          {variable.secret ? <span className="apps__tag">{t('flags.secret')}</span> : null}
          {variable.buildTime ? <span className="apps__tag">{t('flags.buildTime')}</span> : null}
          {!variable.secret && !variable.buildTime ? <span className="apps__muted">—</span> : null}
        </span>
      ),
    },
    ...(canEdit
      ? [
          {
            id: 'actions',
            header: t('columns.actions'),
            align: 'right' as const,
            width: 170,
            cell: (variable: Variable) => (
              <span className="apps__row-actions">
                <button type="button" className="ui-btn" onClick={() => openEditForm(variable)}>
                  {t('detail.editVariable')}
                </button>
                <button
                  type="button"
                  className="ui-btn"
                  disabled={deleting}
                  onClick={() => setDeleteKey(variable.key)}
                >
                  {t('detail.deleteVariable')}
                </button>
              </span>
            ),
          },
        ]
      : []),
  ]

  if (loading && app === null) {
    return (
      <section className="apps">
        <PageHeader title={t('detail.loading')} />
        <p className="ui-sr-only" role="status">
          {t('detail.loading')}
        </p>
      </section>
    )
  }

  if (loadError || app === null) {
    return (
      <section className="apps">
        <PageHeader title={t('detail.notFound')} />
        <div className="apps__error" role="alert">
          <p>{t('detail.loadError')}</p>
          <Link className="ui-btn" to="/apps">
            {t('detail.back')}
          </Link>
        </div>
      </section>
    )
  }

  const activeDeployment = hasActive || deploying || rollingBack

  return (
    <section className="apps">
      <PageHeader
        title={app.stackName}
        subtitle={t('detail.subtitle', {
          server: serverName === '' ? app.serverId : serverName,
          source: t(`source.${app.source}`),
        })}
        actions={
          canDeploy ? (
            <button
              type="button"
              className="ui-btn ui-btn--primary"
              disabled={activeDeployment}
              title={activeDeployment ? t('detail.deploymentRunning') : undefined}
              onClick={() => void deploy()}
            >
              {deploying ? t('detail.deploying') : t('detail.deploy')}
            </button>
          ) : undefined
        }
      />

      <section className="apps__panel">
        <div className="apps__section-head">
          <h2>{t('detail.summary')}</h2>
        </div>
        <dl className="apps__facts">
          <div className="apps__fact">
            <dt>{t('detail.source')}</dt>
            <dd>
              <span className="apps__tag">{t(`source.${app.source}`)}</span>
            </dd>
          </div>
          {app.source === 'git' ? (
            <>
              <div className="apps__fact apps__fact--wide">
                <dt>{t('detail.repository')}</dt>
                <dd>
                  <a
                    className="apps__repo"
                    href={app.repoUrl}
                    target="_blank"
                    rel="noreferrer"
                    title={app.repoUrl}
                  >
                    {app.repoUrl}
                  </a>
                </dd>
              </div>
              <div className="apps__fact">
                <dt>{t('detail.branch')}</dt>
                <dd className="apps__mono">{app.branch}</dd>
              </div>
            </>
          ) : null}
          {app.source === 'image' ? (
            <div className="apps__fact">
              <dt>{t('detail.image')}</dt>
              <dd className="apps__mono">{app.image}</dd>
            </div>
          ) : null}
          {app.source === 'template' ? (
            <div className="apps__fact">
              <dt>{t('detail.template')}</dt>
              <dd className="apps__mono">
                {app.templateId} {app.templateVersion}
              </dd>
            </div>
          ) : null}
          <div className="apps__fact">
            <dt>{t('detail.endpoint')}</dt>
            <dd className="apps__mono">
              {app.hostPort > 0 ? `127.0.0.1:${app.hostPort} → ` : ''}
              {app.service}:{app.port}
            </dd>
          </div>
          <div className="apps__fact">
            <dt>{t('detail.hostPort')}</dt>
            <dd className="apps__mono">
              {app.hostPort > 0 ? app.hostPort : t('detail.noHostPort')}
            </dd>
          </div>
          <div className="apps__fact">
            <dt>{t('detail.autoDeploy')}</dt>
            <dd>{t(app.autoDeploy ? 'yes' : 'no')}</dd>
          </div>
          <div className="apps__fact">
            <dt>{t('detail.poll')}</dt>
            <dd>{t(app.pollEnabled ? 'yes' : 'no')}</dd>
          </div>
          <div className="apps__fact">
            <dt>{t('detail.zeroDowntime')}</dt>
            <dd>
              {t(app.zeroDowntime ? 'yes' : 'no')}
              {app.hostPort > 0 ? (
                <span className="apps__fact-hint">{t('detail.zeroDowntimeHint')}</span>
              ) : null}
            </dd>
          </div>
          <div className="apps__fact">
            <dt>{t('detail.updated')}</dt>
            <dd>{formatDateTime(app.updatedAt, language)}</dd>
          </div>
        </dl>
      </section>

      <section className="apps__panel">
        <div className="apps__section-head">
          <h2>{t('detail.deployments')}</h2>
        </div>
        {deploymentsError && deployments === null ? (
          <div className="apps__inline-error" role="alert">
            <p>{t('detail.deploymentsError')}</p>
            <button type="button" className="ui-btn" onClick={() => void loadDeployments()}>
              {t('detail.retry')}
            </button>
          </div>
        ) : (
          <DataTable
            columns={deploymentColumns}
            rows={deployments ?? []}
            rowKey={(deployment) => deployment.id}
            loading={deployments === null}
            onRowClick={canDeploy ? (deployment) => openLog(deployment) : undefined}
            empty={<div className="apps__empty">{t('detail.noDeployments')}</div>}
          />
        )}
      </section>

      <section className="apps__panel">
        <div className="apps__section-head">
          <h2>{t('detail.variables')}</h2>
          {canEdit ? (
            <button type="button" className="ui-btn" onClick={openAddForm}>
              {t('detail.addVariable')}
            </button>
          ) : null}
        </div>
        {variablesError && variables === null ? (
          <div className="apps__inline-error" role="alert">
            <p>{t('detail.variablesError')}</p>
            <button type="button" className="ui-btn" onClick={() => void loadVariables()}>
              {t('detail.retry')}
            </button>
          </div>
        ) : (
          <DataTable
            columns={variableColumns}
            rows={variables ?? []}
            rowKey={(variable) => variable.key}
            loading={variables === null}
            empty={<div className="apps__empty">{t('detail.noVariables')}</div>}
          />
        )}
        {form !== null ? (
          <form
            ref={formRef}
            className="apps__form"
            onSubmit={(event) => {
              event.preventDefault()
              void saveVariable()
            }}
          >
            <label className="apps__field">
              <span>{t('detail.variableKey')}</span>
              <input
                className="apps__input"
                value={form.key}
                disabled={form.originalKey !== ''}
                autoComplete="off"
                spellCheck={false}
                placeholder={t('detail.variableKeyPlaceholder')}
                onChange={(event) => setForm({ ...form, key: event.target.value.toUpperCase() })}
              />
            </label>
            <label className="apps__field">
              <span>{t('detail.variableValue')}</span>
              <input
                className="apps__input"
                type={form.secret ? 'password' : 'text'}
                value={form.value}
                autoComplete={form.secret ? 'new-password' : 'off'}
                onChange={(event) => setForm({ ...form, value: event.target.value })}
              />
            </label>
            <div className="apps__form-actions">
              <button type="submit" className="ui-btn ui-btn--primary" disabled={saving}>
                {saving ? t('detail.saving') : t('detail.save')}
              </button>
              <button
                type="button"
                className="ui-btn"
                disabled={saving}
                onClick={() => {
                  setForm(null)
                  setFormError('')
                }}
              >
                {t('detail.cancel')}
              </button>
            </div>
            <div className="apps__form-checks">
              <label className="apps__check">
                <input
                  type="checkbox"
                  checked={form.secret}
                  onChange={(event) => setForm({ ...form, secret: event.target.checked })}
                />
                <span>{t('detail.variableSecret')}</span>
              </label>
              <label className="apps__check">
                <input
                  type="checkbox"
                  checked={form.buildTime}
                  onChange={(event) => setForm({ ...form, buildTime: event.target.checked })}
                />
                <span>{t('detail.variableBuildTime')}</span>
              </label>
              {form.secret ? <span className="apps__hint">{t('detail.secretHint')}</span> : null}
            </div>
            {formError !== '' ? (
              <p className="apps__form-error" role="alert">
                {formError}
              </p>
            ) : null}
          </form>
        ) : null}
      </section>

      <ConfirmDialog
        open={rollbackTarget !== null}
        title={t('detail.rollbackTitle', { number: rollbackTarget?.number ?? 0 })}
        body={t('detail.rollbackBody', { commit: shortCommit(rollbackTarget?.commitSha ?? '') })}
        confirmLabel={t('detail.rollback')}
        busy={rollingBack}
        onConfirm={() => void rollback()}
        onCancel={() => setRollbackTarget(null)}
      />

      <ConfirmDialog
        open={deleteKey !== null}
        title={t('detail.deleteVariableTitle')}
        body={t('detail.deleteVariableBody', { key: deleteKey ?? '' })}
        confirmLabel={t('detail.deleteVariable')}
        danger
        busy={deleting}
        onConfirm={() => void deleteVariable()}
        onCancel={() => setDeleteKey(null)}
      />

      <Drawer
        open={selected !== null}
        title={selected === null ? '' : t('detail.logTitle', { number: selected.number })}
        width={720}
        onClose={() => setSelected(null)}
        footer={
          <>
            <button type="button" className="ui-btn" onClick={() => setSelected(null)}>
              {t('detail.close')}
            </button>
            {canDeploy &&
            log !== null &&
            log.state === 'succeeded' &&
            log.commitSha !== '' &&
            app.source === 'git' ? (
              <button
                type="button"
                className="ui-btn ui-btn--primary"
                disabled={rollingBack}
                onClick={() => setRollbackTarget(log)}
              >
                {t('detail.rollback')}
              </button>
            ) : null}
          </>
        }
      >
        {selected !== null ? (
          <>
            <div className="apps__log-head">
              <StatusBadge
                status={deploymentStatus[(log ?? selected).state]}
                label={t(`deploymentState.${(log ?? selected).state}`)}
              />
              {isActiveDeployment(log ?? selected) ? (
                <span className="apps__live">{t('detail.live')}</span>
              ) : null}
            </div>
            {logLoading && log === null ? (
              <p className="apps__muted" role="status">
                {t('detail.logLoading')}
              </p>
            ) : logError && log === null ? (
              <div className="apps__inline-error" role="alert">
                <p>{t('detail.logError')}</p>
              </div>
            ) : (
              <pre className="apps__log" aria-live="polite">
                {log?.log === '' || log?.log === undefined ? t('detail.logEmpty') : log.log}
              </pre>
            )}
          </>
        ) : null}
      </Drawer>
    </section>
  )
}
