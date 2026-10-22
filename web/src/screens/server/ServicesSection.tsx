import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { isSignedOut } from '../../api/http'
import {
  type ServiceAction,
  type ServiceState,
  type ServiceUnit,
  type ServicesApi,
  parseServiceState,
} from '../../api/services'
import { errorMessage } from '../../auth/AuthGate'
import { TimedOut, waitForJob } from './waitForJob'

/** accessCritical are the services whose interruption can cut the connection. */
const accessCritical = new Set([
  'ssh.service',
  'sshd.service',
  'docker.service',
  'networking.service',
  'systemd-networkd.service',
  'NetworkManager.service',
])

/** staticFileStates cannot be enabled or disabled from the panel. */
const staticFileStates = new Set(['static', 'masked', 'alias'])

interface ServicesSectionProps {
  api: ServicesApi
  serverId: string
  pollMs: number
}

type Filter = 'all' | 'active' | 'failed' | 'inactive'

type ListState =
  | { status: 'loading' }
  | { status: 'ready'; services: ServiceUnit[] }
  | { status: 'error'; message: string }

type ConfirmAction = 'stop' | 'restart' | 'disable'

type PendingAction = {
  service: ServiceUnit
  action: ConfirmAction
}

type Outcome = { kind: 'done' } | { kind: 'failed'; message: string }

function needsConfirmation(action: ServiceAction): action is ConfirmAction {
  return action === 'stop' || action === 'restart' || action === 'disable'
}

function sortServices(services: ServiceUnit[]): ServiceUnit[] {
  return [...services].sort((left, right) => {
    const leftFailed = left.activeState === 'failed'
    const rightFailed = right.activeState === 'failed'
    if (leftFailed !== rightFailed) {
      return leftFailed ? -1 : 1
    }
    return left.name.localeCompare(right.name)
  })
}

function stateTone(activeState: string): 'online' | 'critical' | 'offline' | 'warning' {
  switch (activeState) {
    case 'active':
      return 'online'
    case 'failed':
      return 'critical'
    case 'inactive':
      return 'offline'
    default:
      return 'warning'
  }
}

function fileStateActions(service: ServiceUnit): { enable: boolean; disable: boolean } {
  if (staticFileStates.has(service.unitFileState)) {
    return { enable: false, disable: false }
  }
  return {
    enable: service.unitFileState !== 'enabled',
    disable: service.unitFileState === 'enabled',
  }
}

function applyState(
  services: ServiceUnit[],
  state: ServiceState,
  fallbackUnit: string,
): ServiceUnit[] {
  const unit = state.unit || fallbackUnit
  return services.map((service) => {
    if (service.name !== unit) return service
    return {
      ...service,
      activeState: state.activeState || service.activeState,
      subState: state.subState || service.subState,
      unitFileState: state.unitFileState || service.unitFileState,
      loadState: state.loadState || service.loadState,
    }
  })
}

/**
 * ServicesSection lists the systemd units of one server and runs typed actions
 * on them. Stop, restart and disable ask first; nothing is sent before the
 * person confirms.
 */
export function ServicesSection({ api, serverId, pollMs }: ServicesSectionProps) {
  const { t } = useTranslation(['server', 'errors'])
  const [state, setState] = useState<ListState>({ status: 'loading' })
  const [filter, setFilter] = useState<Filter>('all')
  const [search, setSearch] = useState('')
  const [pending, setPending] = useState<PendingAction | null>(null)
  const [busyUnit, setBusyUnit] = useState<string | null>(null)
  const [outcomes, setOutcomes] = useState<Record<string, Outcome>>({})
  const [actionError, setActionError] = useState<string | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const load = useCallback(
    async (silent: boolean, override?: { unit: string; state: ServiceState }) => {
      try {
        const listed = await api.list(serverId)
        if (!alive.current) return
        const services = override ? applyState(listed, override.state, override.unit) : listed
        setState({ status: 'ready', services })
        setLoadError(null)
      } catch (error) {
        if (!alive.current || isSignedOut(error)) return
        const message = errorMessage(t, error)
        if (silent) {
          setLoadError(t('server:services.loadError', { message }))
        } else {
          setState({ status: 'error', message })
        }
      }
    },
    [api, serverId, t],
  )

  useEffect(() => {
    // Start after the effect returns: load updates state once the list arrives,
    // and this keeps the first render free of a cascading update.
    queueMicrotask(() => {
      void load(false)
    })
  }, [load])

  const refresh = () => {
    setActionError(null)
    setLoadError(null)
    setState({ status: 'loading' })
    void load(false)
  }

  const setOutcome = (unit: string, outcome: Outcome | null) => {
    setOutcomes((current) => {
      const next = { ...current }
      if (outcome === null) delete next[unit]
      else next[unit] = outcome
      return next
    })
  }

  const runAction = async (service: ServiceUnit, action: ServiceAction, confirm: boolean) => {
    setPending(null)
    setActionError(null)
    setLoadError(null)
    setOutcome(service.name, null)
    setBusyUnit(service.name)
    try {
      const jobId = await api.action(serverId, service.name, action, confirm)
      const job = await waitForJob(api, jobId, pollMs, () => alive.current)
      if (job === null || !alive.current) return
      if (job.state !== 'succeeded') {
        setOutcome(service.name, {
          kind: 'failed',
          message: job.message || t('server:services.invalid'),
        })
        await load(true)
        return
      }
      let next: ServiceState
      try {
        next = parseServiceState(job.output)
      } catch {
        setOutcome(service.name, {
          kind: 'failed',
          message: t('server:services.invalid'),
        })
        await load(true)
        return
      }
      setState((current) =>
        current.status === 'ready'
          ? { status: 'ready', services: applyState(current.services, next, service.name) }
          : current,
      )
      setOutcome(service.name, { kind: 'done' })
      // The job state is authoritative, but the list is read again so a
      // description or another unit changed by the same action is fresh too.
      // The override keeps the row on the job state if that list is briefly
      // stale.
      await load(true, { unit: service.name, state: next })
    } catch (error) {
      if (!alive.current || isSignedOut(error)) return
      setActionError(
        error instanceof TimedOut ? t('server:progress.timeout') : errorMessage(t, error),
      )
    } finally {
      if (alive.current) setBusyUnit(null)
    }
  }

  const chooseAction = (service: ServiceUnit, action: ServiceAction) => {
    if (needsConfirmation(action)) {
      setActionError(null)
      setPending({ service, action })
      return
    }
    void runAction(service, action, false)
  }

  const services = state.status === 'ready' ? sortServices(state.services) : []
  const counts = {
    all: services.length,
    active: services.filter((service) => service.activeState === 'active').length,
    failed: services.filter((service) => service.activeState === 'failed').length,
    inactive: services.filter((service) => service.activeState === 'inactive').length,
  }
  const query = search.trim().toLowerCase()
  const visible = services.filter((service) => {
    if (filter !== 'all' && service.activeState !== filter) return false
    if (query === '') return true
    return (
      service.name.toLowerCase().includes(query) ||
      service.description.toLowerCase().includes(query)
    )
  })
  const filters: Filter[] = ['all', 'active', 'failed', 'inactive']
  const busy = busyUnit !== null

  return (
    <section className="services" aria-labelledby="services-title" aria-busy={busy}>
      <header className="stacks__head">
        <div>
          <h2 className="stacks__title" id="services-title">
            {t('server:services.title')}
          </h2>
          {state.status === 'ready' ? (
            <p className="stacks__intro">
              {t('server:services.count', { count: services.length })}
            </p>
          ) : null}
        </div>
        <button
          type="button"
          className="cbtn"
          disabled={state.status === 'loading' || busy}
          onClick={refresh}
        >
          {t('server:services.refresh')}
        </button>
      </header>

      <div className="services__body">
        {state.status === 'ready' ? (
          <div className="services__toolbar">
            <label className="services__search">
              <span className="services__search-label">{t('server:services.search')}</span>
              <input
                className="services__input"
                type="search"
                value={search}
                placeholder={t('server:services.searchPlaceholder')}
                onChange={(event) => setSearch(event.target.value)}
              />
            </label>
            <div
              className="services__filters"
              role="group"
              aria-label={t('server:services.filterLabel')}
            >
              {filters.map((candidate) => (
                <button
                  key={candidate}
                  type="button"
                  className={
                    candidate === filter
                      ? 'services__filter services__filter--on'
                      : 'services__filter'
                  }
                  aria-pressed={candidate === filter}
                  aria-label={t('server:services.filterCount', {
                    label: t(`server:services.filters.${candidate}`),
                    count: counts[candidate],
                  })}
                  onClick={() => setFilter(candidate)}
                >
                  <span>{t(`server:services.filters.${candidate}`)}</span>
                  <span className="services__filter-count">{counts[candidate]}</span>
                </button>
              ))}
            </div>
          </div>
        ) : null}

        {state.status === 'loading' ? (
          <p className="services__message" role="status">
            {t('server:services.loading')}
          </p>
        ) : null}
        {state.status === 'error' ? (
          <p className="services__message services__message--error" role="alert">
            {t('server:services.loadError', { message: state.message })}
          </p>
        ) : null}
        {loadError !== null ? (
          <p className="services__message services__message--error" role="alert">
            {loadError}
          </p>
        ) : null}
        {actionError !== null ? (
          <p className="services__message services__message--error" role="alert">
            {actionError}
          </p>
        ) : null}

        {state.status === 'ready' && services.length === 0 ? (
          <p className="services__empty">{t('server:services.empty')}</p>
        ) : null}
        {state.status === 'ready' && services.length > 0 && visible.length === 0 ? (
          <p className="services__empty">{t('server:services.empty')}</p>
        ) : null}

        {visible.length > 0 ? (
          <ul className="servicelist">
            {visible.map((service) => {
              const self = service.name === 'talaia-agent.service'
              const active = service.activeState === 'active'
              const fileActions = fileStateActions(service)
              const outcome = outcomes[service.name]
              const working = busyUnit === service.name
              return (
                <li key={service.name} className="servicerow">
                  <div className="services__main">
                    <div className="services__headline">
                      <h3 className="services__name">{service.name}</h3>
                      <span className={`status status--${stateTone(service.activeState)}`}>
                        {service.activeState
                          ? t(`server:services.state.${service.activeState}`, {
                              defaultValue: service.activeState,
                            })
                          : t('server:services.unknown')}
                      </span>
                      <span className="tag">
                        {service.unitFileState
                          ? t(`server:services.fileState.${service.unitFileState}`, {
                              defaultValue: service.unitFileState,
                            })
                          : t('server:services.unknown')}
                      </span>
                    </div>
                    <p className="services__description">
                      {service.description || t('server:services.unknown')}
                    </p>
                    {self ? (
                      <p className="services__self">{t('server:services.selfManaged')}</p>
                    ) : null}
                    {working ? (
                      <p className="services__working" role="status">
                        <span className="services__spinner" aria-hidden="true" />
                        {t('server:services.working')}
                      </p>
                    ) : null}
                    {outcome?.kind === 'done' ? (
                      <p className="services__outcome services__outcome--ok" role="status">
                        {t('server:services.done')}
                      </p>
                    ) : null}
                    {outcome?.kind === 'failed' ? (
                      <p className="services__outcome services__outcome--error" role="alert">
                        {t('server:services.failed', { message: outcome.message })}
                      </p>
                    ) : null}
                  </div>
                  <div className="services__actions">
                    {!active ? (
                      <button
                        type="button"
                        className="cbtn services__action"
                        disabled={busy}
                        onClick={() => chooseAction(service, 'start')}
                      >
                        {t('server:services.action.start')}
                      </button>
                    ) : null}
                    {!self ? (
                      <>
                        <button
                          type="button"
                          className="cbtn cbtn--danger services__action"
                          disabled={busy}
                          onClick={() => chooseAction(service, 'stop')}
                        >
                          {t('server:services.action.stop')}
                        </button>
                        <button
                          type="button"
                          className="cbtn cbtn--danger services__action"
                          disabled={busy}
                          onClick={() => chooseAction(service, 'restart')}
                        >
                          {t('server:services.action.restart')}
                        </button>
                      </>
                    ) : null}
                    {active ? (
                      <button
                        type="button"
                        className="cbtn services__action"
                        disabled={busy}
                        onClick={() => chooseAction(service, 'reload')}
                      >
                        {t('server:services.action.reload')}
                      </button>
                    ) : null}
                    {!self && fileActions.disable ? (
                      <button
                        type="button"
                        className="cbtn cbtn--danger services__action"
                        disabled={busy}
                        onClick={() => chooseAction(service, 'disable')}
                      >
                        {t('server:services.action.disable')}
                      </button>
                    ) : null}
                    {fileActions.enable ? (
                      <button
                        type="button"
                        className="cbtn services__action"
                        disabled={busy}
                        onClick={() => chooseAction(service, 'enable')}
                      >
                        {t('server:services.action.enable')}
                      </button>
                    ) : null}
                  </div>
                </li>
              )
            })}
          </ul>
        ) : null}
      </div>

      {pending !== null ? (
        <div className="settings__overlay">
          <div
            className="settings__dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="service-action-title"
          >
            <h2 className="settings__dialog-title" id="service-action-title">
              {t(`server:services.confirm.${pending.action}Title`, {
                unit: pending.service.name,
              })}
            </h2>
            <p className="settings__dialog-body">
              {t(`server:services.confirm.${pending.action}Body`)}
            </p>
            {accessCritical.has(pending.service.name) ? (
              <p className="services__warning">{t('server:services.accessWarning')}</p>
            ) : null}
            <div className="settings__dialog-actions">
              <button
                type="button"
                className="settings__button settings__button--danger"
                onClick={() => void runAction(pending.service, pending.action, true)}
              >
                {t(`server:services.confirm.${pending.action}Yes`)}
              </button>
              <button type="button" className="settings__button" onClick={() => setPending(null)}>
                {t('server:services.confirm.cancel')}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </section>
  )
}
