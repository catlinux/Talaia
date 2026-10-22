/**
 * Minimal JSON client for the core API. It always sends the session cookie
 * (same-origin credentials), declares JSON on writes, and turns the error
 * envelope of the core into a typed ApiError.
 */

/** Codes the core returns in {error:{code}}, plus the ones the client adds. */
export type ApiErrorCode =
  | 'invalid_credentials'
  | 'mfa_required'
  | 'session_required'
  | 'forbidden_origin'
  | 'invalid_request'
  | 'weak_password'
  | 'rate_limited'
  | 'name_taken'
  | 'confirmation_required'
  | 'agent_offline'
  | 'agent_busy'
  | 'too_many_terminals'
  | 'terminal_exists'
  | 'terminal_open_failed'
  | 'not_found'
  | 'target_not_offered'
  | 'update_in_progress'
  | 'operation_in_progress'
  | 'host_helper_missing'
  | 'update_check_pending'
  | 'internal'
  | 'network'

const knownCodes: readonly ApiErrorCode[] = [
  'invalid_credentials',
  'mfa_required',
  'session_required',
  'forbidden_origin',
  'invalid_request',
  'weak_password',
  'rate_limited',
  'name_taken',
  'confirmation_required',
  'agent_offline',
  'agent_busy',
  'too_many_terminals',
  'terminal_exists',
  'terminal_open_failed',
  'not_found',
  'target_not_offered',
  'update_in_progress',
  'operation_in_progress',
  'host_helper_missing',
  'update_check_pending',
  'internal',
]

/** ApiError is a failed API call, with the stable code of the core. */
export class ApiError extends Error {
  readonly code: ApiErrorCode
  readonly status: number

  constructor(code: ApiErrorCode, status: number) {
    super(`${code} (${status})`)
    this.name = 'ApiError'
    this.code = code
    this.status = status
  }
}

/**
 * isSignedOut reports whether an error means there is no usable session: the
 * caller should show the sign-in screen again.
 */
export function isSignedOut(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.code === 'session_required' || error.code === 'mfa_required')
  )
}

const signedOutListeners = new Set<() => void>()

/**
 * onSignedOut registers a listener that runs when a call answers that the
 * session is gone, and returns the function that removes it.
 */
export function onSignedOut(listener: () => void): () => void {
  signedOutListeners.add(listener)
  return () => {
    signedOutListeners.delete(listener)
  }
}

function notifySignedOut(): void {
  for (const listener of signedOutListeners) {
    listener()
  }
}

/** HttpClient is the tiny API client the auth functions share. */
export interface HttpClient {
  get<T>(path: string): Promise<T>
  post<T>(path: string, body?: unknown): Promise<T>
  put<T>(path: string, body?: unknown): Promise<T>
}

/** errorCode reads {error:{code}} and falls back to a generic code. */
function errorCode(payload: unknown): ApiErrorCode {
  if (typeof payload === 'object' && payload !== null && 'error' in payload) {
    const error = (payload as { error?: unknown }).error
    if (typeof error === 'object' && error !== null && 'code' in error) {
      const code = (error as { code?: unknown }).code
      if (typeof code === 'string' && (knownCodes as readonly string[]).includes(code)) {
        return code as ApiErrorCode
      }
    }
  }
  return 'internal'
}

/** parseBody returns the JSON body, or undefined when there is none. */
async function parseBody(response: Response): Promise<unknown> {
  const text = await response.text()
  if (text === '') {
    return undefined
  }
  try {
    return JSON.parse(text) as unknown
  } catch {
    return undefined
  }
}

/** defaultFetch keeps the browser's fetch bound to the window. */
function defaultFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return globalThis.fetch(input, init)
}

/** createHttpClient returns a client that talks to the core API. */
export function createHttpClient(fetchImpl: typeof fetch = defaultFetch): HttpClient {
  async function request(
    method: 'GET' | 'POST' | 'PUT',
    path: string,
    body?: unknown,
  ): Promise<unknown> {
    const headers: Record<string, string> = {}
    if (method !== 'GET') {
      headers['Content-Type'] = 'application/json'
    }

    let response: Response
    try {
      response = await fetchImpl(path, {
        method,
        credentials: 'same-origin',
        headers,
        body: method === 'GET' ? undefined : JSON.stringify(body ?? {}),
      })
    } catch {
      throw new ApiError('network', 0)
    }

    const payload = await parseBody(response)
    if (!response.ok) {
      const error = new ApiError(errorCode(payload), response.status)
      if (isSignedOut(error)) {
        notifySignedOut()
      }
      throw error
    }
    return payload
  }

  return {
    get: <T>(path: string): Promise<T> => request('GET', path) as Promise<T>,
    post: <T>(path: string, body?: unknown): Promise<T> =>
      request('POST', path, body) as Promise<T>,
    put: <T>(path: string, body?: unknown): Promise<T> => request('PUT', path, body) as Promise<T>,
  }
}
