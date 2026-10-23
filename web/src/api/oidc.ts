import { createHttpClient } from './http'

/** OIDCLoginConfig is the public subset of the configuration used by the login. */
export interface OIDCLoginConfig {
  enabled: boolean
  label: string
}

/** OIDCSettings is the configuration shown to an administrator. */
export interface OIDCSettings {
  enabled: boolean
  issuer: string
  clientId: string
  /** hasSecret says a client secret is stored; the secret itself is never returned. */
  hasSecret: boolean
  buttonLabel: string
  redirectUrl: string
}

/** OIDCSettingsInput is the write-only form sent to the core. */
export interface OIDCSettingsInput {
  enabled: boolean
  issuer: string
  clientId: string
  /** clientSecret is empty to keep the stored secret. */
  clientSecret: string
  buttonLabel: string
}

/** OIDCApi reads the public login config and the administrator settings. */
export interface OIDCApi {
  loginConfig(): Promise<OIDCLoginConfig>
  settings(): Promise<OIDCSettings>
  update(input: OIDCSettingsInput): Promise<OIDCSettings>
}

interface apiLoginConfig {
  enabled: boolean
  label: string
}

interface apiSettings {
  enabled: boolean
  issuer: string
  client_id: string
  has_secret: boolean
  button_label: string
  redirect_url: string
}

function toSettings(settings: apiSettings): OIDCSettings {
  return {
    enabled: settings.enabled,
    issuer: settings.issuer,
    clientId: settings.client_id,
    hasSecret: settings.has_secret,
    buttonLabel: settings.button_label,
    redirectUrl: settings.redirect_url,
  }
}

/** createOIDCApi returns the OpenID Connect client of the core. */
export function createOIDCApi(fetchImpl?: typeof fetch): OIDCApi {
  const http = createHttpClient(fetchImpl)
  return {
    loginConfig: async () => {
      const body = await http.get<apiLoginConfig>('/api/v1/auth/oidc/config')
      return { enabled: body.enabled, label: body.label }
    },
    settings: async () => toSettings(await http.get<apiSettings>('/api/v1/settings/oidc')),
    update: async (input) => {
      const body = await http.put<apiSettings>('/api/v1/settings/oidc', {
        enabled: input.enabled,
        issuer: input.issuer,
        client_id: input.clientId,
        client_secret: input.clientSecret,
        button_label: input.buttonLabel,
      })
      return toSettings(body)
    },
  }
}
