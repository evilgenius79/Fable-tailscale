// Thin JSON client for /api/v1. Every non-GET request carries
// `X-Requested-With: tailwatch` (the hub rejects it otherwise) and the browser
// sends no credentials because there are none — identity is the Tailscale IP.

export const API_BASE = '/api/v1'

export const REQUEST_HEADER = 'X-Requested-With'
export const REQUEST_HEADER_VALUE = 'tailwatch'

export type ApiErrorCode =
  | 'bad_request'
  | 'unauthorized'
  | 'forbidden'
  | 'not_found'
  | 'rate_limited'
  | 'not_configured'
  | 'upstream'
  | 'internal'
  | 'network'
  | 'http_error'
  | (string & {})

/** Normalised API failure. `status` is 0 for network failures. */
export class ApiError extends Error {
  readonly code: ApiErrorCode
  readonly status: number

  constructor(code: ApiErrorCode, message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
  }

  get isAuth(): boolean {
    return this.status === 401 || this.status === 403
  }
  get isNotFound(): boolean {
    return this.status === 404
  }
  get isRateLimited(): boolean {
    return this.status === 429
  }
  get isNetwork(): boolean {
    return this.status === 0
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError
}

/** Best-effort human message for any thrown value. */
export function errorMessage(e: unknown, fallback = 'Something went wrong'): string {
  if (isApiError(e)) return e.message || fallback
  if (e instanceof Error) return e.message || fallback
  if (typeof e === 'string') return e
  return fallback
}

/** Short, friendly title for an error code. */
export function errorTitle(e: unknown): string {
  if (!isApiError(e)) return 'Request failed'
  switch (e.code) {
    case 'unauthorized':
      return 'Not signed in'
    case 'forbidden':
      return 'Not allowed'
    case 'not_found':
      return 'Not found'
    case 'rate_limited':
      return 'Slow down'
    case 'not_configured':
      return 'Not configured'
    case 'upstream':
      return 'Upstream error'
    case 'network':
      return 'Connection lost'
    case 'bad_request':
      return 'Invalid request'
    default:
      return 'Request failed'
  }
}

/** Map an HTTP status to the closest documented error code. */
export function statusToCode(status: number): ApiErrorCode {
  switch (status) {
    case 400:
      return 'bad_request'
    case 401:
      return 'unauthorized'
    case 403:
      return 'forbidden'
    case 404:
      return 'not_found'
    case 429:
      return 'rate_limited'
    case 501:
      return 'not_configured'
    case 502:
    case 503:
    case 504:
      return 'upstream'
    default:
      return status >= 500 ? 'internal' : 'http_error'
  }
}

export type QueryParams = Record<string, string | number | boolean | null | undefined>

/** Build "?a=1&b=x" from params, skipping null/undefined/empty-string values. */
export function buildQuery(params?: QueryParams): string {
  if (!params) return ''
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === null || v === undefined || v === '') continue
    sp.set(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

/** Absolute API URL for a path like "/devices/abc". Path segments are not encoded — use `encodeURIComponent` on ids. */
export function apiUrl(path: string, params?: QueryParams): string {
  const p = path.startsWith('/') ? path : `/${path}`
  return `${API_BASE}${p}${buildQuery(params)}`
}

export interface RequestOptions {
  params?: QueryParams
  body?: unknown
  signal?: AbortSignal
}

function isJsonResponse(res: Response): boolean {
  const ct = res.headers.get('content-type') ?? ''
  return ct.includes('application/json')
}

/** Turn a non-2xx Response into an ApiError, reading the documented envelope when present. */
export async function errorFromResponse(res: Response): Promise<ApiError> {
  let code: ApiErrorCode = statusToCode(res.status)
  let message = res.statusText || `HTTP ${res.status}`
  try {
    if (isJsonResponse(res)) {
      const data = (await res.json()) as unknown
      if (data && typeof data === 'object' && 'error' in data) {
        const err = (data as { error?: { code?: unknown; message?: unknown } }).error
        if (err && typeof err === 'object') {
          if (typeof err.code === 'string' && err.code) code = err.code
          if (typeof err.message === 'string' && err.message) message = err.message
        }
      }
    } else {
      const text = await res.text()
      if (text && text.length < 200) message = text.trim() || message
    }
  } catch {
    /* body unreadable; keep defaults */
  }
  if (message === `HTTP ${res.status}` || message === res.statusText) {
    // Friendlier defaults for the common cases.
    if (res.status === 401) message = 'Your request could not be attributed to a tailnet identity.'
    else if (res.status === 403) message = 'You do not have permission to do that.'
    else if (res.status === 404) message = 'The requested resource was not found.'
    else if (res.status === 429) message = 'Too many requests. Please wait a moment.'
    else if (res.status >= 500) message = 'The hub reported an internal error.'
  }
  return new ApiError(code, message, res.status)
}

/** Low-level request. Throws ApiError for HTTP and network failures; rethrows AbortError untouched. */
export async function apiRequest<T>(method: 'GET' | 'POST' | 'PUT' | 'DELETE', path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (method !== 'GET') headers[REQUEST_HEADER] = REQUEST_HEADER_VALUE
  const init: RequestInit = {
    method,
    headers,
    credentials: 'same-origin',
    cache: 'no-store',
    signal: opts.signal ?? null,
  }
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(opts.body)
  }
  let res: Response
  try {
    res = await fetch(apiUrl(path, opts.params), init)
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e
    throw new ApiError('network', 'Could not reach the hub. Check your tailnet connection.', 0)
  }
  if (!res.ok) throw await errorFromResponse(res)
  if (res.status === 204 || res.status === 202) return undefined as T
  const len = res.headers.get('content-length')
  if (len === '0') return undefined as T
  if (!isJsonResponse(res)) {
    const text = await res.text()
    if (!text) return undefined as T
    try {
      return JSON.parse(text) as T
    } catch {
      throw new ApiError('internal', 'The hub returned a non-JSON response.', res.status)
    }
  }
  return (await res.json()) as T
}

export const apiGet = <T>(path: string, opts?: Omit<RequestOptions, 'body'>) => apiRequest<T>('GET', path, opts)
export const apiPost = <T>(path: string, body?: unknown, opts?: Omit<RequestOptions, 'body'>) =>
  apiRequest<T>('POST', path, { ...opts, body })
export const apiPut = <T>(path: string, body?: unknown, opts?: Omit<RequestOptions, 'body'>) =>
  apiRequest<T>('PUT', path, { ...opts, body })
export const apiDelete = <T = void>(path: string, opts?: Omit<RequestOptions, 'body'>) => apiRequest<T>('DELETE', path, opts)

/** Encode a device id / rule id for use in a path segment. */
export function seg(id: string | number): string {
  return encodeURIComponent(String(id))
}
