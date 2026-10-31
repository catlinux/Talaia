import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  createAppsApi,
  type AppInput,
  type AppSource,
  type CreatedApp,
  type Domain,
  type ProxyDomain,
  type ProxyStatus,
  type TemplateSummary,
  type WizardApi,
} from '../../../api/apps'
import { createApiFleetClient, type FleetClient } from '../../../api/client'
import { ApiError, isSignedOut } from '../../../api/http'
import type { Server } from '../../../api/types'
import { errorMessage } from '../../../auth/AuthGate'
import { usePermissions } from '../../../auth/permissions'
import { PageHeader } from '../../../components/ui'
import { useNavigate } from '../../../router'
import './wizard.css'

type Step = 'source' | 'build' | 'target' | 'domain' | 'review'

const steps: Step[] = ['source', 'build', 'target', 'domain', 'review']

const repoPrefix = /^(https:\/\/github\.com\/|git@github\.com:|ssh:\/\/git@github\.com\/)/
const servicePattern = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$/
const stackPattern = /^[a-z0-9][a-z0-9_-]{0,62}$/
const domainPattern = /^(?=.{1,253}$)([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/

interface FormState {
  source: AppSource
  repoUrl: string
  branch: string
  dockerfilePath: string
  buildContext: string
  image: string
  templateId: string
  templateVersion: string
  service: string
  port: number
  autoDeploy: boolean
  pollEnabled: boolean
  zeroDowntime: boolean
  serverId: string
  stackName: string
  domain: string
}

interface CreatedResult {
  app: CreatedApp
  domain: Domain | null
  domainError: string
  deployError: string
  deployRequested: boolean
  proxyDomain: ProxyDomain | null
  proxyMode: string
}

interface NewAppWizardProps {
  api?: WizardApi
  client?: FleetClient
}

function stackNameFrom(value: string): string {
  let candidate = value
    .trim()
    .replace(/[?#].*$/, '')
    .replace(/\/+$/, '')
  if (candidate === '') return ''
  if (candidate.includes('/')) {
    candidate = candidate.split('/').pop() ?? ''
  } else if (candidate.startsWith('git@') && candidate.includes(':')) {
    candidate = candidate.split(':').pop() ?? ''
  }
  candidate = candidate
    .replace(/\.git$/i, '')
    .split('@')[0]!
    .split(':')[0]!
    .toLocaleLowerCase()
    .replace(/[^a-z0-9_-]+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^[-_]+|[-_]+$/g, '')
  if (!/^[a-z0-9]/.test(candidate)) return ''
  return candidate.slice(0, 63)
}

function initialForm(): FormState {
  return {
    source: 'git',
    repoUrl: '',
    branch: 'main',
    dockerfilePath: 'Dockerfile',
    buildContext: '.',
    image: '',
    templateId: '',
    templateVersion: '',
    service: 'app',
    port: 3000,
    autoDeploy: true,
    pollEnabled: false,
    zeroDowntime: true,
    serverId: '',
    stackName: '',
    domain: '',
  }
}

/** NewAppWizard creates an app in the five steps of mock-up 4. */
export function NewAppWizard({ api, client }: NewAppWizardProps) {
  const { t, i18n } = useTranslation('apps')
  const navigate = useNavigate()
  const { can } = usePermissions()
  const appsApi = useMemo(() => api ?? createAppsApi(), [api])
  const fleetClient = useMemo(() => client ?? createApiFleetClient(), [client])
  const language = i18n.resolvedLanguage ?? i18n.language
  const alive = useRef(true)

  const [step, setStep] = useState<Step>('source')
  const [form, setForm] = useState<FormState>(initialForm)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [nameEdited, setNameEdited] = useState(false)
  const [templates, setTemplates] = useState<TemplateSummary[] | null>(null)
  const [templatesError, setTemplatesError] = useState(false)
  const [servers, setServers] = useState<Server[] | null>(null)
  const [serversError, setServersError] = useState(false)
  const [proxy, setProxy] = useState<ProxyStatus | null>(null)
  const [proxyLoading, setProxyLoading] = useState(false)
  const [proxyError, setProxyError] = useState(false)
  const [created, setCreated] = useState<CreatedResult | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const loadTemplates = useCallback(async () => {
    try {
      const list = await appsApi.templates()
      if (!alive.current) return
      setTemplates(list)
      setTemplatesError(false)
    } catch {
      if (alive.current) setTemplatesError(true)
    }
  }, [appsApi])

  const loadServers = useCallback(async () => {
    try {
      const list = await fleetClient.listServers()
      if (!alive.current) return
      setServers(list)
      setServersError(false)
    } catch {
      if (alive.current) setServersError(true)
    }
  }, [fleetClient])

  useEffect(() => {
    queueMicrotask(() => {
      void loadTemplates()
      void loadServers()
    })
  }, [loadServers, loadTemplates])

  useEffect(() => {
    if (form.serverId === '') return
    let active = true
    appsApi.proxy(form.serverId).then(
      (status) => {
        if (!active) return
        setProxy(status)
        setProxyLoading(false)
        setProxyError(false)
      },
      () => {
        if (!active) return
        setProxy(null)
        setProxyLoading(false)
        setProxyError(true)
      },
    )
    return () => {
      active = false
    }
  }, [appsApi, form.serverId])

  const eligibleServers = useMemo(
    () => (servers ?? []).filter((server) => can('stack.edit', { serverId: server.id })),
    [can, servers],
  )

  const stepIndex = steps.indexOf(step)
  const domainHostname = form.domain.trim().toLocaleLowerCase()
  const matchingProxyDomain =
    proxy?.domains.find((domain) => domain.hostname === domainHostname) ?? null
  const selectedTemplate = templates?.find((template) => template.id === form.templateId)
  const selectedServer = eligibleServers.find((server) => server.id === form.serverId)

  const setField = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((current) => ({ ...current, [key]: value }))
    setErrors({})
    setSubmitError('')
  }

  const updateRepo = (value: string) => {
    setForm((current) => ({
      ...current,
      repoUrl: value,
      ...(nameEdited ? {} : { stackName: stackNameFrom(value) }),
    }))
    setErrors({})
    setSubmitError('')
  }

  const updateImage = (value: string) => {
    setForm((current) => ({
      ...current,
      image: value,
      ...(nameEdited ? {} : { stackName: stackNameFrom(value) }),
    }))
    setErrors({})
    setSubmitError('')
  }

  const updateStackName = (value: string) => {
    setNameEdited(true)
    setField('stackName', value)
  }

  const chooseSource = (source: AppSource) => {
    setForm((current) => ({
      ...current,
      source,
      autoDeploy: source === 'git',
      pollEnabled: false,
      zeroDowntime: source !== 'template',
      ...(nameEdited
        ? {}
        : {
            stackName: stackNameFrom(
              source === 'git'
                ? current.repoUrl
                : source === 'image'
                  ? current.image
                  : current.templateId,
            ),
          }),
      ...(source === 'git' && current.source !== 'git'
        ? { service: 'app', port: 3000, image: '', templateId: '', templateVersion: '' }
        : {}),
      ...(source === 'image'
        ? { image: current.image, templateId: '', templateVersion: '', service: 'app', port: 3000 }
        : {}),
      ...(source === 'template' ? { image: '', autoDeploy: false, pollEnabled: false } : {}),
    }))
    setErrors({})
  }

  const chooseTemplate = (template: TemplateSummary) => {
    setForm((current) => ({
      ...current,
      source: 'template',
      templateId: template.id,
      templateVersion: template.version,
      image: template.image,
      service: template.service,
      port: template.port,
      autoDeploy: false,
      pollEnabled: false,
      zeroDowntime: false,
      ...(nameEdited ? {} : { stackName: stackNameFrom(template.id) }),
    }))
    setErrors({})
  }

  const chooseServer = (serverId: string) => {
    if (serverId === form.serverId) return
    setForm((current) => ({ ...current, serverId }))
    setErrors({})
    setProxy(null)
    setProxyError(false)
    setProxyLoading(serverId !== '')
  }

  const validateStep = (target: Step): Record<string, string> => {
    const result: Record<string, string> = {}
    if (target === 'build') {
      if (form.source === 'git') {
        if (!repoPrefix.test(form.repoUrl.trim())) result.repoUrl = t('wizard.validation.repo')
        if (form.branch.trim() === '') result.branch = t('wizard.validation.branch')
        if (form.dockerfilePath.trim() === '') {
          result.dockerfilePath = t('wizard.validation.dockerfile')
        }
        if (form.buildContext.trim() === '') result.buildContext = t('wizard.validation.context')
      }
      if (form.source === 'image' && form.image.trim() === '') {
        result.image = t('wizard.validation.image')
      }
      if (form.source === 'template' && form.templateId === '') {
        result.template = t('wizard.validation.template')
      }
      if (!servicePattern.test(form.service.trim())) result.service = t('wizard.validation.service')
      if (!Number.isInteger(form.port) || form.port < 1 || form.port > 65535) {
        result.port = t('wizard.validation.port')
      }
    }
    if (target === 'target') {
      if (form.serverId === '' || !eligibleServers.some((server) => server.id === form.serverId)) {
        result.serverId = t('wizard.validation.server')
      }
      if (!stackPattern.test(form.stackName.trim())) {
        result.stackName = t('wizard.validation.stack')
      }
    }
    if (target === 'domain' && form.domain.trim() !== '') {
      if (!domainPattern.test(domainHostname)) result.domain = t('wizard.validation.domain')
    }
    return result
  }

  const validateAll = (): { step: Step; errors: Record<string, string> } | null => {
    for (const target of ['build', 'target', 'domain'] as Step[]) {
      const result = validateStep(target)
      if (Object.keys(result).length > 0) return { step: target, errors: result }
    }
    return null
  }

  const next = () => {
    const result = validateStep(step)
    if (Object.keys(result).length > 0) {
      setErrors(result)
      return
    }
    setErrors({})
    setSubmitError('')
    if (stepIndex < steps.length - 1) setStep(steps[stepIndex + 1]!)
  }

  const back = () => {
    setErrors({})
    setSubmitError('')
    if (stepIndex > 0) setStep(steps[stepIndex - 1]!)
  }

  const create = async () => {
    const invalid = validateAll()
    if (invalid !== null) {
      setErrors(invalid.errors)
      setStep(invalid.step)
      return
    }
    setErrors({})
    setSubmitError('')
    setSubmitting(true)
    try {
      const input: AppInput = {
        stackName: form.stackName.trim(),
        source: form.source,
        repoUrl: form.source === 'git' ? form.repoUrl.trim() : '',
        branch: form.source === 'git' ? form.branch.trim() : '',
        build: 'dockerfile',
        dockerfilePath: form.source === 'git' ? form.dockerfilePath.trim() : 'Dockerfile',
        buildContext: form.source === 'git' ? form.buildContext.trim() : '.',
        image: form.source === 'image' ? form.image.trim() : '',
        service: form.service.trim(),
        port: form.port,
        autoDeploy: form.source === 'git' ? form.autoDeploy : false,
        pollEnabled: form.source === 'git' ? form.pollEnabled : false,
        zeroDowntime: form.zeroDowntime,
        templateId: form.source === 'template' ? form.templateId : '',
        templateVersion: form.source === 'template' ? form.templateVersion : '',
      }
      const app = await appsApi.create(form.serverId, input)
      let domain: Domain | null = null
      let domainError = ''
      if (domainHostname !== '') {
        try {
          domain = await appsApi.addDomain(app.id, {
            hostname: domainHostname,
            service: form.service.trim(),
            port: form.port,
          })
        } catch (error) {
          if (!isSignedOut(error)) {
            domainError = errorMessage(t, error)
          }
        }
      }
      let deployError = ''
      let deployRequested = app.source !== 'template'
      if (
        app.source === 'template' &&
        can('stack.deploy', { serverId: form.serverId, stack: form.stackName })
      ) {
        try {
          await appsApi.deploy(app.id)
          deployRequested = true
        } catch (error) {
          if (!isSignedOut(error)) deployError = errorMessage(t, error)
        }
      }
      let proxyDomain: ProxyDomain | null = null
      let proxyMode = ''
      if (domain !== null) {
        try {
          const status = await appsApi.proxy(form.serverId)
          proxyMode = status.mode
          proxyDomain = status.domains.find((item) => item.hostname === domain?.hostname) ?? null
        } catch {
          proxyDomain = null
        }
      }
      if (!alive.current) return
      setCreated({
        app,
        domain,
        domainError,
        deployError,
        deployRequested,
        proxyDomain,
        proxyMode,
      })
      setStep('review')
    } catch (error) {
      if (!alive.current) return
      if (!isSignedOut(error)) {
        let message = errorMessage(t, error)
        if (error instanceof ApiError) {
          if (error.code === 'app_exists') {
            message = t('wizard.review.nameExists', { name: form.stackName.trim() })
          } else if (error.code === 'invalid_request') {
            message = t('wizard.review.invalidName')
          }
        }
        setSubmitError(t('wizard.review.createError', { message }))
      }
    } finally {
      if (alive.current) setSubmitting(false)
    }
  }

  const sourceName = (source: AppSource) => t(`wizard.source.${source}.title`)
  const templateName = (template: TemplateSummary) =>
    template.names[language] ?? template.names.en ?? template.id
  const templateDescription = (template: TemplateSummary) =>
    template.descriptions[language] ?? template.descriptions.en ?? ''

  const summaryRows: Array<{ label: string; value: string; wide?: boolean }> = [
    { label: t('wizard.summary.source'), value: sourceName(form.source) },
  ]
  if (form.source === 'git') {
    if (form.repoUrl !== '') {
      summaryRows.push({ label: t('wizard.summary.repository'), value: form.repoUrl, wide: true })
    }
    if (form.branch !== '') {
      summaryRows.push({ label: t('wizard.summary.branch'), value: form.branch })
    }
  }
  if (form.source === 'image' && form.image !== '') {
    summaryRows.push({ label: t('wizard.summary.image'), value: form.image, wide: true })
  }
  if (form.source === 'template' && selectedTemplate !== undefined) {
    summaryRows.push({ label: t('wizard.summary.template'), value: templateName(selectedTemplate) })
  }
  if (form.service !== '') {
    summaryRows.push({
      label: t('wizard.summary.service'),
      value: `${form.service}:${form.port}`,
    })
  }
  if (form.serverId !== '') {
    summaryRows.push({
      label: t('wizard.summary.server'),
      value: selectedServer?.name ?? form.serverId,
    })
  }
  if (form.stackName !== '') {
    summaryRows.push({ label: t('wizard.summary.stack'), value: form.stackName })
  }
  if (form.domain.trim() !== '') {
    summaryRows.push({ label: t('wizard.summary.domain'), value: form.domain.trim() })
  }

  const renderSource = () => (
    <>
      <h2 className="wizard__section-title">{t('wizard.source.title')}</h2>
      <p className="wizard__section-intro">{t('wizard.source.intro')}</p>
      <div className="wizard__options">
        {(['git', 'template', 'image'] as AppSource[]).map((source) => (
          <label
            className={
              form.source === source ? 'wizard__option wizard__option--on' : 'wizard__option'
            }
            key={source}
          >
            <input
              type="radio"
              name="source"
              value={source}
              checked={form.source === source}
              onChange={() => chooseSource(source)}
            />
            <span>
              <strong>{t(`wizard.source.${source}.title`)}</strong>
              <small>{t(`wizard.source.${source}.description`)}</small>
            </span>
          </label>
        ))}
      </div>
    </>
  )

  const renderField = (
    name: keyof FormState,
    label: string,
    value: string,
    onChange: (value: string) => void,
    options: { placeholder?: string; type?: string; hint?: string } = {},
  ) => (
    <label className="wizard__field">
      <span>{label}</span>
      <input
        className="wizard__input"
        type={options.type ?? 'text'}
        value={value}
        placeholder={options.placeholder}
        aria-label={label}
        aria-invalid={errors[name] !== undefined}
        onChange={(event) => onChange(event.target.value)}
      />
      {options.hint !== undefined && errors[name] === undefined ? (
        <small className="wizard__hint">{options.hint}</small>
      ) : null}
      {errors[name] !== undefined ? (
        <small className="wizard__error" role="alert">
          {errors[name]}
        </small>
      ) : null}
    </label>
  )

  const renderBuild = () => (
    <>
      <h2 className="wizard__section-title">{t('wizard.build.title')}</h2>
      <p className="wizard__section-intro">{t(`wizard.build.${form.source}Hint`)}</p>
      {form.source === 'git' ? (
        <div className="wizard__grid">
          {renderField('repoUrl', t('wizard.build.repoUrl'), form.repoUrl, updateRepo, {
            placeholder: t('wizard.build.repoPlaceholder'),
            hint: t('wizard.build.repoHint'),
          })}
          {renderField('branch', t('wizard.build.branch'), form.branch, (value) =>
            setField('branch', value),
          )}
          {renderField(
            'dockerfilePath',
            t('wizard.build.dockerfile'),
            form.dockerfilePath,
            (value) => setField('dockerfilePath', value),
          )}
          {renderField('buildContext', t('wizard.build.context'), form.buildContext, (value) =>
            setField('buildContext', value),
          )}
        </div>
      ) : null}
      {form.source === 'image' ? (
        <div className="wizard__grid">
          {renderField('image', t('wizard.build.image'), form.image, updateImage, {
            placeholder: t('wizard.build.imagePlaceholder'),
          })}
        </div>
      ) : null}
      {form.source === 'template' ? (
        <div className="wizard__template-list">
          {templates === null && !templatesError ? (
            <p className="wizard__muted" role="status">
              {t('wizard.build.templatesLoading')}
            </p>
          ) : templatesError ? (
            <div className="wizard__inline-error" role="alert">
              <p>{t('wizard.build.templatesError')}</p>
              <button type="button" className="ui-btn" onClick={() => void loadTemplates()}>
                {t('wizard.build.retry')}
              </button>
            </div>
          ) : (
            (templates ?? []).map((template) => (
              <label
                className={
                  form.templateId === template.id
                    ? 'wizard__option wizard__option--on'
                    : 'wizard__option'
                }
                key={template.id}
              >
                <input
                  type="radio"
                  name="template"
                  value={template.id}
                  checked={form.templateId === template.id}
                  onChange={() => chooseTemplate(template)}
                />
                <span>
                  <strong>{templateName(template)}</strong>
                  <small>{templateDescription(template)}</small>
                  <small className="wizard__meta">
                    {t('wizard.build.templateMeta', {
                      image: template.image,
                      port: template.port,
                      ram: template.ramMB,
                    })}
                  </small>
                </span>
              </label>
            ))
          )}
          {errors.template !== undefined ? (
            <small className="wizard__error" role="alert">
              {errors.template}
            </small>
          ) : null}
        </div>
      ) : null}
      {form.source !== 'template' ? (
        <div className="wizard__grid wizard__grid--runtime">
          {renderField('service', t('wizard.build.service'), form.service, (value) =>
            setField('service', value),
          )}
          {renderField(
            'port',
            t('wizard.build.port'),
            String(form.port || ''),
            (value) => setField('port', Number(value)),
            { type: 'number', placeholder: '3000' },
          )}
        </div>
      ) : null}
      {form.source === 'template' && selectedTemplate !== undefined ? (
        <p className="wizard__hint">
          {t('wizard.build.templateService', {
            service: selectedTemplate.service,
            port: selectedTemplate.port,
          })}
        </p>
      ) : null}
    </>
  )

  const renderTarget = () => (
    <>
      <h2 className="wizard__section-title">{t('wizard.target.title')}</h2>
      <p className="wizard__section-intro">{t('wizard.target.intro')}</p>
      {servers === null && !serversError ? (
        <p className="wizard__muted" role="status">
          {t('wizard.target.serversLoading')}
        </p>
      ) : serversError ? (
        <div className="wizard__inline-error" role="alert">
          <p>{t('wizard.target.serversError')}</p>
          <button type="button" className="ui-btn" onClick={() => void loadServers()}>
            {t('wizard.build.retry')}
          </button>
        </div>
      ) : eligibleServers.length === 0 ? (
        <p className="wizard__inline-error" role="alert">
          {t('wizard.target.noServers')}
        </p>
      ) : (
        <div className="wizard__options">
          {eligibleServers.map((server) => (
            <label
              className={
                form.serverId === server.id ? 'wizard__option wizard__option--on' : 'wizard__option'
              }
              key={server.id}
            >
              <input
                type="radio"
                name="server"
                value={server.id}
                checked={form.serverId === server.id}
                onChange={() => chooseServer(server.id)}
              />
              <span>
                <strong className="wizard__mono">{server.name}</strong>
                <small>
                  {server.os} ·{' '}
                  {t('wizard.target.serverResources', {
                    cpu: server.vcpu,
                    ram: server.memoryGb,
                  })}
                </small>
              </span>
            </label>
          ))}
        </div>
      )}
      {errors.serverId !== undefined ? (
        <small className="wizard__error" role="alert">
          {errors.serverId}
        </small>
      ) : null}
      <div className="wizard__grid wizard__grid--target">
        {renderField('stackName', t('wizard.target.stack'), form.stackName, updateStackName, {
          placeholder: t('wizard.target.stackPlaceholder'),
          hint: t('wizard.target.stackHint', {
            stack: form.stackName === '' ? t('wizard.target.stackPlaceholder') : form.stackName,
          }),
        })}
      </div>
      {form.source === 'git' ? (
        <div className="wizard__checks">
          <label className="wizard__check">
            <input
              type="checkbox"
              checked={form.autoDeploy}
              onChange={(event) => setField('autoDeploy', event.target.checked)}
            />
            <span>{t('wizard.target.autoDeploy')}</span>
          </label>
          <label className="wizard__check">
            <input
              type="checkbox"
              checked={form.pollEnabled}
              onChange={(event) => setField('pollEnabled', event.target.checked)}
            />
            <span>{t('wizard.target.poll')}</span>
          </label>
        </div>
      ) : null}
      <div className="wizard__checks">
        <label className="wizard__check">
          <input
            type="checkbox"
            checked={form.zeroDowntime}
            onChange={(event) => setField('zeroDowntime', event.target.checked)}
          />
          <span>{t('wizard.target.zeroDowntime')}</span>
        </label>
      </div>
      {proxy?.mode === 'external' ? (
        <p className="wizard__hint">{t('wizard.target.zeroDowntimeHint')}</p>
      ) : null}
    </>
  )

  const renderDomain = () => {
    const block = matchingProxyDomain
    return (
      <>
        <h2 className="wizard__section-title">{t('wizard.domain.title')}</h2>
        <p className="wizard__section-intro">{t('wizard.domain.intro')}</p>
        <div className="wizard__grid">
          {renderField(
            'domain',
            t('wizard.domain.hostname'),
            form.domain,
            (value) => setField('domain', value.toLocaleLowerCase()),
            {
              placeholder: t('wizard.domain.hostnamePlaceholder'),
              hint: t('wizard.domain.optional'),
            },
          )}
        </div>
        {proxyLoading ? (
          <p className="wizard__muted" role="status">
            {t('wizard.domain.proxyLoading')}
          </p>
        ) : proxyError ? (
          <p className="wizard__notice" role="status">
            {t('wizard.domain.blockUnavailable')}
          </p>
        ) : proxy?.mode === 'managed' ? (
          <div className="wizard__notice">
            <strong>{t('wizard.domain.managed')}</strong>
            <p>{t('wizard.domain.managedHint')}</p>
          </div>
        ) : proxy?.mode === 'external' ? (
          <div className="wizard__proxy">
            <h3>{t('wizard.domain.external')}</h3>
            <p>{t('wizard.domain.externalHint')}</p>
            <h4>{t('wizard.domain.apache')}</h4>
            {block?.apache ? (
              <pre className="wizard__code">{block.apache}</pre>
            ) : (
              <p className="wizard__muted">{t('wizard.domain.blockPending')}</p>
            )}
            {block?.certbot ? (
              <>
                <h4>{t('wizard.domain.certbot')}</h4>
                <pre className="wizard__code">{block.certbot}</pre>
              </>
            ) : null}
          </div>
        ) : null}
      </>
    )
  }

  const renderReview = () => (
    <>
      <h2 className="wizard__section-title">{t('wizard.review.title')}</h2>
      <dl className="wizard__review">
        {summaryRows.map((row) => (
          <div
            className={row.wide === true ? 'wizard__review--wide' : undefined}
            key={`${row.label}-${row.value}`}
          >
            <dt>{row.label}</dt>
            <dd>{row.value}</dd>
          </div>
        ))}
        {form.source === 'git' ? (
          <div>
            <dt>{t('wizard.review.deploy')}</dt>
            <dd>{form.autoDeploy ? t('wizard.review.autoDeploy') : t('wizard.review.manual')}</dd>
          </div>
        ) : null}
      </dl>
      {form.source === 'git' ? (
        <p className="wizard__hint">{t('wizard.review.secretsHint')}</p>
      ) : null}
      {submitError !== '' ? (
        <p className="wizard__inline-error" role="alert">
          {submitError}
        </p>
      ) : null}
    </>
  )

  const stepContent = () => {
    switch (step) {
      case 'source':
        return renderSource()
      case 'build':
        return renderBuild()
      case 'target':
        return renderTarget()
      case 'domain':
        return renderDomain()
      case 'review':
        return renderReview()
    }
  }

  const summary = (
    <aside className="wizard__summary">
      <h2>{t('wizard.summary.title')}</h2>
      {summaryRows.length === 0 ? (
        <p className="wizard__muted">{t('wizard.summary.empty')}</p>
      ) : (
        <dl>
          {summaryRows.map((row) => (
            <div key={`${row.label}-${row.value}`}>
              <dt>{row.label}</dt>
              <dd>{row.value}</dd>
            </div>
          ))}
        </dl>
      )}
    </aside>
  )

  if (created !== null) {
    const app = created.app
    const hasGitSecrets = app.source === 'git' && app.deployKeyPublic !== ''
    return (
      <section className="wizard">
        <PageHeader
          title={t('wizard.success.title')}
          subtitle={t('wizard.success.subtitle', {
            stack: app.stackName,
            server: selectedServer?.name ?? app.serverId,
          })}
          actions={
            <>
              <button type="button" className="ui-btn" onClick={() => navigate('/apps')}>
                {t('wizard.success.back')}
              </button>
              <button
                type="button"
                className="ui-btn ui-btn--primary"
                onClick={() => navigate(`/apps/${encodeURIComponent(app.id)}`)}
              >
                {t('wizard.success.open')}
              </button>
            </>
          }
        />
        <div className="wizard__layout">
          <div className="wizard__main">
            <section className="wizard__panel">
              {created.deployRequested && created.deployError === '' ? (
                <p className="wizard__success-note">{t('wizard.success.queued')}</p>
              ) : null}
              {app.source === 'template' && !created.deployRequested ? (
                <p className="wizard__notice">{t('wizard.success.deploySkipped')}</p>
              ) : null}
              {hasGitSecrets ? (
                <div className="wizard__secrets">
                  <p className="wizard__notice">{t('wizard.success.once')}</p>
                  <h3>{t('wizard.success.deployKey')}</h3>
                  <code className="wizard__secret-value">{app.deployKeyPublic}</code>
                  {app.webhookUrl !== '' ? (
                    <>
                      <h3>{t('wizard.success.webhookUrl')}</h3>
                      <code className="wizard__secret-value">{app.webhookUrl}</code>
                    </>
                  ) : null}
                  {app.webhookSecret !== '' ? (
                    <>
                      <h3>{t('wizard.success.webhookSecret')}</h3>
                      <code className="wizard__secret-value">{app.webhookSecret}</code>
                    </>
                  ) : null}
                </div>
              ) : (
                <p className="wizard__hint">{t('wizard.success.gitOnly')}</p>
              )}
              {created.domain !== null && created.domain.warning !== '' ? (
                <p className="wizard__notice" role="status">
                  {t('wizard.success.domainWarning', {
                    warning: domainWarning(created.domain.warning, t),
                  })}
                </p>
              ) : null}
              {created.domainError !== '' ? (
                <p className="wizard__inline-error" role="alert">
                  {t('wizard.success.domainError', { message: created.domainError })}
                </p>
              ) : null}
              {created.deployError !== '' ? (
                <p className="wizard__inline-error" role="alert">
                  {t('wizard.success.deployWarning', { message: created.deployError })}
                </p>
              ) : null}
              {created.domain !== null && created.proxyMode === 'managed' ? (
                <p className="wizard__notice">{t('wizard.domain.managedHint')}</p>
              ) : null}
              {created.domain !== null && created.proxyMode === 'external' ? (
                app.hostPort > 0 && created.proxyDomain?.apache ? (
                  <div className="wizard__proxy">
                    <h3>{t('wizard.success.apacheTitle')}</h3>
                    <pre className="wizard__code">{created.proxyDomain.apache}</pre>
                  </div>
                ) : (
                  <p className="wizard__hint">{t('wizard.success.apachePending')}</p>
                )
              ) : null}
            </section>
          </div>
          {summary}
        </div>
      </section>
    )
  }

  return (
    <section className="wizard">
      <PageHeader
        title={t('wizard.title')}
        subtitle={t('wizard.stepOf', { current: stepIndex + 1, total: steps.length })}
        actions={
          <button type="button" className="ui-btn" onClick={() => navigate('/apps')}>
            {t('wizard.cancel')}
          </button>
        }
      />
      <div className="wizard__layout">
        <div className="wizard__main">
          <ol className="wizard__steps">
            {steps.map((item, index) => {
              const done = index < stepIndex
              return (
                <li
                  key={item}
                  className={
                    item === step
                      ? 'wizard__step wizard__step--on'
                      : done
                        ? 'wizard__step wizard__step--done'
                        : 'wizard__step'
                  }
                >
                  <button
                    type="button"
                    disabled={!done}
                    aria-current={item === step ? 'step' : undefined}
                    onClick={() => {
                      if (done) {
                        setErrors({})
                        setSubmitError('')
                        setStep(item)
                      }
                    }}
                  >
                    <b>{done ? '✓' : index + 1}</b>
                    <span>{t(`wizard.steps.${item}`)}</span>
                  </button>
                </li>
              )
            })}
          </ol>
          <section className="wizard__panel">{stepContent()}</section>
          <div className="wizard__nav">
            {stepIndex > 0 ? (
              <button type="button" className="ui-btn" onClick={back}>
                {t('wizard.back')}
              </button>
            ) : null}
            {step === 'review' ? (
              <button
                type="button"
                className="ui-btn ui-btn--primary"
                disabled={submitting}
                onClick={() => void create()}
              >
                {submitting ? t('wizard.creating') : t('wizard.create')}
              </button>
            ) : (
              <button type="button" className="ui-btn ui-btn--primary" onClick={next}>
                {t('wizard.next')}
              </button>
            )}
          </div>
        </div>
        {summary}
      </div>
    </section>
  )
}

function domainWarning(warning: string, t: (key: string) => string): string {
  switch (warning) {
    case 'the hostname does not resolve':
      return t('wizard.warnings.noResolve')
    case 'the hostname does not resolve to the server':
      return t('wizard.warnings.mismatch')
    default:
      return warning
  }
}
