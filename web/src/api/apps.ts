import { createHttpClient } from './http'

export type AppSource = 'git' | 'image' | 'template'
export type DeploymentState =
  'queued' | 'building' | 'releasing' | 'succeeded' | 'failed' | 'superseded'

/** App is one deployable application backed by a Compose stack. */
export interface App {
  id: string
  serverId: string
  stackName: string
  source: AppSource
  repoUrl: string
  branch: string
  build: string
  dockerfilePath: string
  buildContext: string
  image: string
  service: string
  port: number
  hostPort: number
  autoDeploy: boolean
  pollEnabled: boolean
  zeroDowntime: boolean
  templateId: string
  templateVersion: string
  createdAt: string
  updatedAt: string
  updatedBy: string
}

/** Deployment is one build and release attempt. */
export interface Deployment {
  id: string
  appId: string
  number: number
  trigger: string
  commitSha: string
  commitSubject: string
  imageRef: string
  state: DeploymentState
  startedAt: string
  finishedAt: string
  error: string
}

export interface DeploymentDetail extends Deployment {
  log: string
}

/** Variable is one app environment variable. Secret values are never sent. */
export interface Variable {
  key: string
  value?: string
  secret: boolean
  buildTime: boolean
}

export interface VariableInput {
  value: string
  secret: boolean
  buildTime: boolean
}

/** TemplateSummary is one one-click app template exposed by the core. */
export interface TemplateSummary {
  id: string
  version: string
  names: Record<string, string>
  descriptions: Record<string, string>
  image: string
  service: string
  port: number
  ramMB: number
}

/** AppInput is the body that creates a new app. */
export interface AppInput {
  stackName: string
  source: AppSource
  repoUrl: string
  branch: string
  build: string
  dockerfilePath: string
  buildContext: string
  image: string
  service: string
  port: number
  autoDeploy: boolean
  pollEnabled: boolean
  zeroDowntime: boolean
  templateId: string
  templateVersion: string
}

/** CreatedApp carries the one-time Git secrets returned at creation. */
export interface CreatedApp extends App {
  deployKeyPublic: string
  webhookSecret: string
  webhookUrl: string
}

/** Domain is one hostname routed to an app. */
export interface Domain {
  id: string
  appId: string
  hostname: string
  service: string
  port: number
  createdAt: string
  warning: string
}

export interface DomainInput {
  hostname: string
  service: string
  port: number
}

/** ProxyDomain is one external proxy block returned by the core. */
export interface ProxyDomain {
  hostname: string
  appId: string
  hostPort: number
  warning: string
  apache: string
  nginx: string
  certbot: string
}

export interface ProxyStatus {
  mode: 'external' | 'managed'
  domains: ProxyDomain[]
}

/** AppsApi reads and controls the apps of the core. */
export interface AppsApi {
  list(): Promise<App[]>
  listByServer(serverId: string): Promise<App[]>
  get(appId: string): Promise<App>
  variables(appId: string): Promise<Variable[]>
  setVariable(appId: string, key: string, input: VariableInput): Promise<void>
  deleteVariable(appId: string, key: string): Promise<void>
  deployments(appId: string, limit?: number): Promise<Deployment[]>
  deployment(appId: string, deploymentId: string): Promise<DeploymentDetail>
  deploy(appId: string, commit?: string): Promise<void>
  rollback(appId: string, deploymentId: string): Promise<void>
}

/** WizardApi is the extra surface the new-app wizard needs. */
export interface WizardApi extends AppsApi {
  templates(): Promise<TemplateSummary[]>
  create(serverId: string, input: AppInput): Promise<CreatedApp>
  addDomain(appId: string, input: DomainInput): Promise<Domain>
  proxy(serverId: string): Promise<ProxyStatus>
}

interface apiApp {
  id: string
  server_id: string
  stack_name: string
  source: AppSource
  repo_url: string
  branch: string
  build: string
  dockerfile_path: string
  build_context: string
  image: string
  service: string
  port: number
  host_port: number
  auto_deploy: boolean
  poll_enabled: boolean
  zero_downtime: boolean
  template_id: string
  template_version: string
  created_at: string
  updated_at: string
  updated_by: string
}

interface apiDeployment {
  id: string
  app_id: string
  number: number
  trigger: string
  commit_sha: string
  commit_subject: string
  image_ref: string
  state: DeploymentState
  started_at: string
  finished_at: string
  error: string
}

interface apiDeploymentDetail extends apiDeployment {
  log: string
}

interface apiVariable {
  key: string
  value?: string
  secret: boolean
  build_time: boolean
}

interface apiTemplate {
  id: string
  version: string
  names: Record<string, string>
  descriptions: Record<string, string>
  image: string
  service: string
  port: number
  ram_mb: number
}

interface apiCreatedApp extends apiApp {
  deploy_key_public?: string
  webhook_secret?: string
  webhook_url?: string
}

interface apiDomain {
  id: string
  app_id: string
  hostname: string
  service: string
  port: number
  created_at: string
  warning?: string
}

interface apiProxyDomain {
  hostname: string
  app_id: string
  host_port: number
  warning?: string
  apache?: string
  nginx?: string
  certbot?: string
}

interface apiProxy {
  mode: 'external' | 'managed'
  domains: apiProxyDomain[]
}

function toApp(app: apiApp): App {
  return {
    id: app.id,
    serverId: app.server_id,
    stackName: app.stack_name,
    source: app.source,
    repoUrl: app.repo_url,
    branch: app.branch,
    build: app.build,
    dockerfilePath: app.dockerfile_path,
    buildContext: app.build_context,
    image: app.image,
    service: app.service,
    port: app.port,
    hostPort: app.host_port,
    autoDeploy: app.auto_deploy,
    pollEnabled: app.poll_enabled,
    zeroDowntime: app.zero_downtime,
    templateId: app.template_id,
    templateVersion: app.template_version,
    createdAt: app.created_at,
    updatedAt: app.updated_at,
    updatedBy: app.updated_by,
  }
}

function toDeployment(item: apiDeployment): Deployment {
  return {
    id: item.id,
    appId: item.app_id,
    number: item.number,
    trigger: item.trigger,
    commitSha: item.commit_sha,
    commitSubject: item.commit_subject,
    imageRef: item.image_ref,
    state: item.state,
    startedAt: item.started_at,
    finishedAt: item.finished_at,
    error: item.error,
  }
}

function toVariable(item: apiVariable): Variable {
  return {
    key: item.key,
    ...(item.value === undefined ? {} : { value: item.value }),
    secret: item.secret,
    buildTime: item.build_time,
  }
}

function toTemplate(item: apiTemplate): TemplateSummary {
  return {
    id: item.id,
    version: item.version,
    names: item.names,
    descriptions: item.descriptions,
    image: item.image,
    service: item.service,
    port: item.port,
    ramMB: item.ram_mb,
  }
}

function toCreatedApp(item: apiCreatedApp): CreatedApp {
  return {
    ...toApp(item),
    deployKeyPublic: item.deploy_key_public ?? '',
    webhookSecret: item.webhook_secret ?? '',
    webhookUrl: item.webhook_url ?? '',
  }
}

function toDomain(item: apiDomain): Domain {
  return {
    id: item.id,
    appId: item.app_id,
    hostname: item.hostname,
    service: item.service,
    port: item.port,
    createdAt: item.created_at,
    warning: item.warning ?? '',
  }
}

function toProxyDomain(item: apiProxyDomain): ProxyDomain {
  return {
    hostname: item.hostname,
    appId: item.app_id,
    hostPort: item.host_port,
    warning: item.warning ?? '',
    apache: item.apache ?? '',
    nginx: item.nginx ?? '',
    certbot: item.certbot ?? '',
  }
}

/** createAppsApi returns the apps client of the core. */
export function createAppsApi(fetchImpl?: typeof fetch): WizardApi {
  const http = createHttpClient(fetchImpl)
  return {
    list: async () => (await http.get<apiApp[]>('/api/v1/apps')).map(toApp),
    listByServer: async (serverId) =>
      (await http.get<apiApp[]>(`/api/v1/servers/${encodeURIComponent(serverId)}/apps`)).map(toApp),
    get: async (appId) =>
      toApp(await http.get<apiApp>(`/api/v1/apps/${encodeURIComponent(appId)}`)),
    variables: async (appId) =>
      (await http.get<apiVariable[]>(`/api/v1/apps/${encodeURIComponent(appId)}/env`)).map(
        toVariable,
      ),
    setVariable: (appId, key, input) =>
      http.put<void>(`/api/v1/apps/${encodeURIComponent(appId)}/env/${encodeURIComponent(key)}`, {
        value: input.value,
        secret: input.secret,
        build_time: input.buildTime,
      }),
    deleteVariable: (appId, key) =>
      http.delete<void>(`/api/v1/apps/${encodeURIComponent(appId)}/env/${encodeURIComponent(key)}`),
    deployments: async (appId, limit = 50) =>
      (
        await http.get<apiDeployment[]>(
          `/api/v1/apps/${encodeURIComponent(appId)}/deployments?limit=${limit}`,
        )
      ).map(toDeployment),
    deployment: async (appId, deploymentId) => {
      const body = await http.get<apiDeploymentDetail>(
        `/api/v1/apps/${encodeURIComponent(appId)}/deployments/${encodeURIComponent(deploymentId)}`,
      )
      return { ...toDeployment(body), log: body.log }
    },
    deploy: (appId, commit = '') =>
      http.post<void>(`/api/v1/apps/${encodeURIComponent(appId)}/deployments`, { commit }),
    rollback: (appId, deploymentId) =>
      http.post<void>(
        `/api/v1/apps/${encodeURIComponent(appId)}/deployments/${encodeURIComponent(deploymentId)}/rollback`,
      ),
    templates: async () =>
      ((await http.get<apiTemplate[] | null>('/api/v1/templates')) ?? []).map(toTemplate),
    create: async (serverId, input) =>
      toCreatedApp(
        await http.post<apiCreatedApp>(`/api/v1/servers/${encodeURIComponent(serverId)}/apps`, {
          stack_name: input.stackName,
          source: input.source,
          repo_url: input.repoUrl,
          branch: input.branch,
          build: input.build,
          dockerfile_path: input.dockerfilePath,
          build_context: input.buildContext,
          image: input.image,
          service: input.service,
          port: input.port,
          auto_deploy: input.autoDeploy,
          poll_enabled: input.pollEnabled,
          zero_downtime: input.zeroDowntime,
          template_id: input.templateId,
          template_version: input.templateVersion,
        }),
      ),
    addDomain: async (appId, input) =>
      toDomain(
        await http.post<apiDomain>(`/api/v1/apps/${encodeURIComponent(appId)}/domains`, {
          hostname: input.hostname,
          service: input.service,
          port: input.port,
        }),
      ),
    proxy: async (serverId) => {
      const body = await http.get<apiProxy>(`/api/v1/servers/${encodeURIComponent(serverId)}/proxy`)
      return { mode: body.mode, domains: (body.domains ?? []).map(toProxyDomain) }
    },
  }
}
