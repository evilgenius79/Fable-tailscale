import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, apiDelete, apiGet, apiPost, apiPut, apiUrl, buildQuery, errorFromResponse, errorMessage, errorTitle, isApiError, statusToCode } from './client'

const jsonResponse = (body: unknown, status = 200, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json; charset=utf-8', ...headers } })

describe('buildQuery / apiUrl', () => {
  it('skips empty values and encodes', () => {
    expect(buildQuery()).toBe('')
    expect(buildQuery({ a: 1, b: 'x y', c: undefined, d: null, e: '' })).toBe('?a=1&b=x+y')
    expect(apiUrl('/devices', { range: '1h' })).toBe('/api/v1/devices?range=1h')
    expect(apiUrl('devices')).toBe('/api/v1/devices')
  })
})

describe('statusToCode', () => {
  it('maps documented statuses', () => {
    expect(statusToCode(400)).toBe('bad_request')
    expect(statusToCode(401)).toBe('unauthorized')
    expect(statusToCode(403)).toBe('forbidden')
    expect(statusToCode(404)).toBe('not_found')
    expect(statusToCode(429)).toBe('rate_limited')
    expect(statusToCode(502)).toBe('upstream')
    expect(statusToCode(500)).toBe('internal')
    expect(statusToCode(418)).toBe('http_error')
  })
})

describe('errorFromResponse', () => {
  it('reads the error envelope', async () => {
    const err = await errorFromResponse(jsonResponse({ error: { code: 'not_configured', message: 'control API disabled' } }, 501))
    expect(err).toBeInstanceOf(ApiError)
    expect(err.code).toBe('not_configured')
    expect(err.message).toBe('control API disabled')
    expect(err.status).toBe(501)
  })
  it('falls back to status mapping for non-JSON bodies', async () => {
    const err = await errorFromResponse(new Response('Forbidden', { status: 403, statusText: 'Forbidden' }))
    expect(err.code).toBe('forbidden')
    expect(err.status).toBe(403)
    expect(err.isAuth).toBe(true)
    expect(err.message).toBe('Forbidden')
  })
  it('gives friendly defaults when the body is empty', async () => {
    const err = await errorFromResponse(new Response(null, { status: 429 }))
    expect(err.code).toBe('rate_limited')
    expect(err.message).toMatch(/too many requests/i)
    expect(err.isRateLimited).toBe(true)
  })
  it('tolerates a malformed envelope', async () => {
    const err = await errorFromResponse(jsonResponse({ nope: true }, 500))
    expect(err.code).toBe('internal')
    expect(err.status).toBe(500)
  })
})

describe('apiRequest', () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
    fetchMock.mockReset()
  })
  afterEach(() => vi.unstubAllGlobals())

  it('GET sends no X-Requested-With and parses JSON', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ login: 'a' }))
    const out = await apiGet<{ login: string }>('/me')
    expect(out.login).toBe('a')
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('/api/v1/me')
    expect((init?.headers as Record<string, string>)['X-Requested-With']).toBeUndefined()
    expect(init?.credentials).toBe('same-origin')
    expect(init?.cache).toBe('no-store')
  })

  it('POST/PUT/DELETE send the X-Requested-With header and JSON bodies', async () => {
    fetchMock.mockImplementation(async () => jsonResponse({ ok: true }))
    await apiPost('/devices/x/tags', { tags: ['tag:a'] })
    await apiPut('/alerts/rules/r', { id: 'r' })
    await apiDelete('/devices/x')
    for (const [, init] of fetchMock.mock.calls) {
      expect((init?.headers as Record<string, string>)['X-Requested-With']).toBe('tailwatch')
    }
    const post = fetchMock.mock.calls[0]![1]!
    expect(post.method).toBe('POST')
    expect(post.body).toBe(JSON.stringify({ tags: ['tag:a'] }))
    expect((post.headers as Record<string, string>)['Content-Type']).toBe('application/json')
    expect(fetchMock.mock.calls[2]![1]!.method).toBe('DELETE')
  })

  it('returns undefined for 204/202', async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }))
    await expect(apiDelete('/devices/x')).resolves.toBeUndefined()
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 202 }))
    await expect(apiPost('/refresh')).resolves.toBeUndefined()
  })

  it('throws ApiError from the envelope on non-2xx', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ error: { code: 'not_found', message: 'device not found' } }, 404))
    const err = await apiGet('/devices/nope').catch((e: unknown) => e)
    expect(isApiError(err)).toBe(true)
    expect((err as ApiError).code).toBe('not_found')
    expect((err as ApiError).isNotFound).toBe(true)
    expect(errorTitle(err)).toBe('Not found')
    expect(errorMessage(err)).toBe('device not found')
  })

  it('wraps network failures', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('Failed to fetch'))
    const err = await apiGet('/overview').catch((e: unknown) => e)
    expect(isApiError(err)).toBe(true)
    expect((err as ApiError).code).toBe('network')
    expect((err as ApiError).status).toBe(0)
    expect((err as ApiError).isNetwork).toBe(true)
    expect(errorTitle(err)).toBe('Connection lost')
  })

  it('rethrows AbortError untouched', async () => {
    fetchMock.mockRejectedValueOnce(new DOMException('aborted', 'AbortError'))
    await expect(apiGet('/overview')).rejects.toMatchObject({ name: 'AbortError' })
  })

  it('errorMessage handles non-errors', () => {
    expect(errorMessage('boom')).toBe('boom')
    expect(errorMessage({})).toBe('Something went wrong')
    expect(errorMessage(new Error(''))).toBe('Something went wrong')
  })
})
