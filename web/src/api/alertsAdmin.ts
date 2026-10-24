import { createHttpClient } from './http'

/** AlertRuleType is one supported alert rule. */
export type AlertRuleType =
  | 'server_offline'
  | 'cpu_high'
  | 'mem_high'
  | 'disk_high'
  | 'container_down'
  | 'container_unhealthy'
  | 'check_failed'
  | 'cert_expiry'

/** AlertRuleSeverity is the severity a rule raises. */
export type AlertRuleSeverity = 'warning' | 'critical'

/** AlertRuleScopeType is where a rule applies. */
export type AlertRuleScopeType = 'organization' | 'group' | 'server' | 'stack' | 'container'

/** AlertRule is one rule returned by the core. */
export interface AlertRule {
  id: string
  name: string
  type: AlertRuleType
  enabled: boolean
  severity: AlertRuleSeverity
  params: Record<string, number>
  scopeType: AlertRuleScopeType
  serverId: string | null
  groupId: string | null
  scopeName: string | null
  channelIds: string[]
  repeatIntervalSeconds: number
  builtin: boolean
}

/** AlertRuleInput is the body of a rule create or update call. */
export interface AlertRuleInput {
  name: string
  type: AlertRuleType
  enabled: boolean
  severity: AlertRuleSeverity
  params: Record<string, number>
  scopeType: AlertRuleScopeType
  serverId?: string | null
  groupId?: string | null
  scopeName?: string | null
  channelIds: string[]
  repeatIntervalSeconds: number
}

/** CheckKind is a kind of active check. */
export type CheckKind = 'http' | 'tcp' | 'tls'

/** CheckConfig is the type-specific configuration of a check. */
export interface CheckConfig {
  method: string
  expectedCodes: string[]
  timeoutSeconds: number
}

/** CheckResult is the last result or the answer of a run-now request. */
export interface CheckResult {
  ok: boolean
  latencyMs: number
  statusCode: number | null
  error: string
  certNotAfter: string | null
  time: string | null
}

/** AlertCheck is one active check returned by the core. */
export interface AlertCheck {
  id: string
  name: string
  kind: CheckKind
  target: string
  config: CheckConfig
  intervalSeconds: number
  enabled: boolean
  serverId: string | null
  latestResult: CheckResult | null
}

/** AlertCheckInput is the body of a check create or update call. */
export interface AlertCheckInput {
  name: string
  kind: CheckKind
  target: string
  config: CheckConfig
  intervalSeconds: number
  enabled: boolean
  serverId?: string | null
}

/** ChannelType is a delivery channel supported by the core. */
export type ChannelType = 'telegram' | 'ntfy' | 'email' | 'discord' | 'webhook'

/** ChannelLanguage is the language of the messages a channel sends. */
export type ChannelLanguage = 'ca' | 'es' | 'en'

/** ChannelConfig is the non-secret configuration of a channel. */
export interface ChannelConfig {
  chatId: string
  server: string
  priority: number | null
  host: string
  port: number | null
  from: string
  to: string[]
  tlsMode: string
}

/** ChannelSecretInput holds typed secrets. It is never populated from the API. */
export interface ChannelSecretInput {
  token?: string
  topic?: string
  username?: string
  password?: string
  url?: string
  hmacSecret?: string
}

/** AlertChannel is one channel returned by the core; secrets are never included. */
export interface AlertChannel {
  id: string
  name: string
  type: ChannelType
  enabled: boolean
  language: ChannelLanguage
  config: ChannelConfig
  hasSecret: boolean
  lastError: string
  lastOkAt: string | null
}

/** AlertChannelInput is the body of a channel create or update call. */
export interface AlertChannelInput {
  name: string
  type: ChannelType
  enabled: boolean
  language: ChannelLanguage
  config: ChannelConfig
  secret?: ChannelSecretInput
}

/** ChannelTestResult is the answer of a send-test request. */
export interface ChannelTestResult {
  ok: boolean
  error: string
}

/** AlertAdminSettings is the private-target policy of alerting. */
export interface AlertAdminSettings {
  allowPrivateTargets: boolean
}

/** AlertsAdminApi manages alert rules, checks, channels and settings. */
export interface AlertsAdminApi {
  listRules(): Promise<AlertRule[]>
  createRule(input: AlertRuleInput): Promise<AlertRule>
  updateRule(id: string, input: AlertRuleInput): Promise<AlertRule>
  deleteRule(id: string): Promise<void>
  listChecks(): Promise<AlertCheck[]>
  createCheck(input: AlertCheckInput): Promise<AlertCheck>
  updateCheck(id: string, input: AlertCheckInput): Promise<AlertCheck>
  deleteCheck(id: string): Promise<void>
  runCheck(id: string): Promise<CheckResult>
  listChannels(): Promise<AlertChannel[]>
  createChannel(input: AlertChannelInput): Promise<AlertChannel>
  updateChannel(id: string, input: AlertChannelInput): Promise<AlertChannel>
  deleteChannel(id: string): Promise<void>
  testChannel(id: string): Promise<ChannelTestResult>
  getSettings(): Promise<AlertAdminSettings>
  updateSettings(allowPrivateTargets: boolean): Promise<AlertAdminSettings>
}

type raw = Record<string, unknown>

function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function nullableText(value: unknown): string | null {
  return typeof value === 'string' && value !== '' ? value : null
}

function number(value: unknown, fallback = 0): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback
}

function bool(value: unknown): boolean {
  return value === true
}

function record(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {}
}

function stringList(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === 'string')
    : []
}

function numberParams(value: unknown): Record<string, number> {
  const out: Record<string, number> = {}
  for (const [key, item] of Object.entries(record(value))) {
    if (typeof item === 'number') out[key] = item
  }
  return out
}

function mapRule(value: raw): AlertRule {
  return {
    id: text(value.id),
    name: text(value.name),
    type: text(value.type) as AlertRuleType,
    enabled: bool(value.enabled),
    severity: text(value.severity) as AlertRuleSeverity,
    params: numberParams(value.params),
    scopeType: text(value.scope_type) as AlertRuleScopeType,
    serverId: nullableText(value.server_id),
    groupId: nullableText(value.group_id),
    scopeName: nullableText(value.scope_name),
    channelIds: stringList(value.channel_ids),
    repeatIntervalSeconds: number(value.repeat_interval_seconds),
    builtin: bool(value.builtin),
  }
}

function ruleBody(input: AlertRuleInput) {
  return {
    name: input.name,
    type: input.type,
    enabled: input.enabled,
    severity: input.severity,
    params: input.params,
    scope_type: input.scopeType,
    server_id: input.serverId ?? '',
    group_id: input.groupId ?? '',
    scope_name: input.scopeName ?? '',
    channel_ids: input.channelIds,
    repeat_interval_seconds: input.repeatIntervalSeconds,
  }
}

function mapCheckResult(value: raw): CheckResult {
  return {
    ok: bool(value.ok),
    latencyMs: number(value.latency_ms),
    statusCode: typeof value.status_code === 'number' ? value.status_code : null,
    error: text(value.error),
    certNotAfter: nullableText(value.cert_not_after),
    time: nullableText(value.time),
  }
}

function mapCheckConfig(value: unknown): CheckConfig {
  const config = record(value)
  const codes = Array.isArray(config.expected_codes)
    ? config.expected_codes.map((item) => String(item))
    : []
  return {
    method: text(config.method) || 'GET',
    expectedCodes: codes.length > 0 ? codes : ['200-399'],
    timeoutSeconds: number(config.timeout_seconds, 10),
  }
}

function mapCheck(value: raw): AlertCheck {
  return {
    id: text(value.id),
    name: text(value.name),
    kind: text(value.kind) as CheckKind,
    target: text(value.target),
    config: mapCheckConfig(value.config),
    intervalSeconds: number(value.interval_seconds),
    enabled: bool(value.enabled),
    serverId: nullableText(value.server_id),
    latestResult:
      typeof value.latest_result === 'object' && value.latest_result !== null
        ? mapCheckResult(value.latest_result as raw)
        : null,
  }
}

function checkBody(input: AlertCheckInput) {
  return {
    name: input.name,
    kind: input.kind,
    target: input.target,
    config: {
      method: input.config.method,
      expected_codes: input.config.expectedCodes,
      timeout_seconds: input.config.timeoutSeconds,
    },
    interval_seconds: input.intervalSeconds,
    enabled: input.enabled,
    server_id: input.serverId ?? '',
  }
}

function mapChannelConfig(value: unknown): ChannelConfig {
  const config = record(value)
  return {
    chatId: text(config.chat_id),
    server: text(config.server),
    priority: typeof config.priority === 'number' ? config.priority : null,
    host: text(config.host),
    port: typeof config.port === 'number' ? config.port : null,
    from: text(config.from),
    to: stringList(config.to),
    tlsMode: text(config.tls_mode),
  }
}

function mapChannel(value: raw): AlertChannel {
  return {
    id: text(value.id),
    name: text(value.name),
    type: text(value.type) as ChannelType,
    enabled: bool(value.enabled),
    language: (text(value.language) || 'en') as ChannelLanguage,
    config: mapChannelConfig(value.config),
    hasSecret: bool(value.has_secret),
    lastError: text(value.last_error),
    lastOkAt: nullableText(value.last_ok_at),
  }
}

function channelConfigBody(type: ChannelType, config: ChannelConfig): Record<string, unknown> {
  switch (type) {
    case 'telegram':
      return { chat_id: config.chatId }
    case 'ntfy':
      return {
        server: config.server,
        ...(config.priority === null ? {} : { priority: config.priority }),
      }
    case 'email':
      return {
        host: config.host,
        port: config.port ?? 0,
        from: config.from,
        to: config.to,
        tls_mode: config.tlsMode,
      }
    default:
      return {}
  }
}

function channelSecretBody(type: ChannelType, secret: ChannelSecretInput): Record<string, string> {
  switch (type) {
    case 'telegram':
      return { token: secret.token ?? '' }
    case 'ntfy':
      return {
        topic: secret.topic ?? '',
        ...(secret.token ? { token: secret.token } : {}),
      }
    case 'email':
      return { username: secret.username ?? '', password: secret.password ?? '' }
    case 'discord':
      return { url: secret.url ?? '' }
    case 'webhook':
      return {
        url: secret.url ?? '',
        ...(secret.hmacSecret ? { hmac_secret: secret.hmacSecret } : {}),
      }
  }
}

function channelBody(input: AlertChannelInput) {
  return {
    name: input.name,
    type: input.type,
    enabled: input.enabled,
    language: input.language,
    config: channelConfigBody(input.type, input.config),
    ...(input.secret ? { secret: channelSecretBody(input.type, input.secret) } : {}),
  }
}

/** createAlertsAdminApi returns the alert administration API of the core. */
export function createAlertsAdminApi(fetchImpl?: typeof fetch): AlertsAdminApi {
  const http = createHttpClient(fetchImpl)
  return {
    listRules: async () => {
      const rows = await http.get<raw[]>('/api/v1/alert-rules')
      return (rows ?? []).map(mapRule)
    },
    createRule: async (input) =>
      mapRule(await http.post<raw>('/api/v1/alert-rules', ruleBody(input))),
    updateRule: async (id, input) =>
      mapRule(
        await http.patch<raw>(`/api/v1/alert-rules/${encodeURIComponent(id)}`, ruleBody(input)),
      ),
    deleteRule: async (id) => {
      await http.delete<void>(`/api/v1/alert-rules/${encodeURIComponent(id)}`)
    },
    listChecks: async () => {
      const rows = await http.get<raw[]>('/api/v1/checks')
      return (rows ?? []).map(mapCheck)
    },
    createCheck: async (input) =>
      mapCheck(await http.post<raw>('/api/v1/checks', checkBody(input))),
    updateCheck: async (id, input) =>
      mapCheck(await http.patch<raw>(`/api/v1/checks/${encodeURIComponent(id)}`, checkBody(input))),
    deleteCheck: async (id) => {
      await http.delete<void>(`/api/v1/checks/${encodeURIComponent(id)}`)
    },
    runCheck: async (id) =>
      mapCheckResult(await http.post<raw>(`/api/v1/checks/${encodeURIComponent(id)}/run`)),
    listChannels: async () => {
      const rows = await http.get<raw[]>('/api/v1/channels')
      return (rows ?? []).map(mapChannel)
    },
    createChannel: async (input) =>
      mapChannel(await http.post<raw>('/api/v1/channels', channelBody(input))),
    updateChannel: async (id, input) =>
      mapChannel(
        await http.patch<raw>(`/api/v1/channels/${encodeURIComponent(id)}`, channelBody(input)),
      ),
    deleteChannel: async (id) => {
      await http.delete<void>(`/api/v1/channels/${encodeURIComponent(id)}`)
    },
    testChannel: async (id) => {
      const body = await http.post<raw>(`/api/v1/channels/${encodeURIComponent(id)}/test`)
      return { ok: bool(body.ok), error: text(body.error) }
    },
    getSettings: async () => {
      const body = await http.get<raw>('/api/v1/settings/alerts')
      return { allowPrivateTargets: bool(body.allow_private_targets) }
    },
    updateSettings: async (allowPrivateTargets) => {
      const body = await http.put<raw>('/api/v1/settings/alerts', {
        allow_private_targets: allowPrivateTargets,
      })
      return { allowPrivateTargets: bool(body.allow_private_targets) }
    },
  }
}
