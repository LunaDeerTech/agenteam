import { describe, expect, it, vi } from 'vitest'
import { createAccountAPI } from '../api/account'
import { AccountFailure } from '../api/client'

const id = '01900000-0000-7000-8000-000000000001'
const instant = '2026-10-05T12:34:56.123456Z'
const token = 'A'.repeat(43)
const pass = `${id}.${'B'.repeat(43)}`
const user = {
  id,
  email: 'person@example.com',
  username: '',
  display_name: '',
  role: 'user',
  theme: 'system',
  version: '9223372036854775807',
  initial_password_suggestion: false,
}
const session = { id, issued_at: instant, absolute_expires_at: instant, idle_expires_at: instant }
const controller = () => new AbortController()
const response = (body: unknown, status = 200, contentType = 'application/json') =>
  new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': contentType, 'X-Request-ID': id },
  })

describe('the six Account wire contracts', () => {
  it.each([[['unknown']], [{}], [null], [1]])(
    'rejects non-string commit_state without converting it',
    async (commit_state) => {
      const malformed = {
        type: 'urn:agenteam:problem:dependency-unavailable',
        title: 'Unavailable',
        status: 503,
        detail: 'safe error',
        instance: '/api/v1/sessions/login',
        code: 'DEPENDENCY_UNAVAILABLE',
        request_id: id,
        commit_state,
      }
      const api = createAccountAPI(async () => response(malformed, 503, 'application/problem+json'))
      await expect(api.getSession(controller().signal)).rejects.toMatchObject({
        kind: 'invalid-response',
        problem: undefined,
      })
    },
  )
  it('uses only relative exact paths, separate tokens, original input and the declared status/body', async () => {
    const replies = [
      response({ csrf_token: token, challenge_modes: ['rotate'], delivery_channel: 'backend_log' }),
      response({ user, session, csrf_token: 'C'.repeat(43) }),
      response({ user, session, next_path: '/' }),
      response(null, 204),
      response(
        {
          id,
          mode: 'rotate',
          master: 'data:image/png;base64,AA==',
          thumb: 'data:image/png;base64,AA==',
          expires_at: instant,
        },
        201,
      ),
      response({ pass }),
    ]
    const fetcher = vi.fn(async () => replies.shift()!)
    const api = createAccountAPI(fetcher),
      signal = controller().signal
    await api.bootstrap(signal)
    const view = await api.getSession(signal)
    const password = '  Original 密码 12345  '
    await api.login(
      { email: user.email, password, challenge_pass: pass },
      token,
      'login:key-1',
      signal,
    )
    await api.logout(view.csrf_token, 'logout:key-2', signal)
    await api.createChallenge(
      { mode: 'rotate', email: user.email, login_key: 'login:key-1' },
      token,
      signal,
    )
    expect(
      (
        await api.verifyChallenge(
          { email: user.email, login_key: 'login:key-1', challenge_id: id, proof: { angle: 360 } },
          token,
          signal,
        )
      ).pass,
    ).toBe(pass)
    expect(view.user.version).toBe('9223372036854775807')
    const calls = fetcher.mock.calls as unknown as [string, RequestInit][]
    expect(calls.map(([url, init]) => [url, init.method])).toEqual([
      ['/api/v1/auth/bootstrap', 'GET'],
      ['/api/v1/session', 'GET'],
      ['/api/v1/sessions/login', 'POST'],
      ['/api/v1/sessions/logout', 'POST'],
      ['/api/v1/auth/challenges', 'POST'],
      ['/api/v1/auth/challenges/verify', 'POST'],
    ])
    for (const [, init] of calls)
      expect(init).toMatchObject({
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        signal,
      })
    expect(calls[0][1].body).toBeUndefined()
    expect(calls[1][1].body).toBeUndefined()
    expect(JSON.parse(calls[2][1].body as string)).toEqual({
      email: user.email,
      password,
      challenge_pass: pass,
    })
    expect(calls[2][1].headers).toMatchObject({
      'X-CSRF-Token': token,
      'Idempotency-Key': 'login:key-1',
    })
    expect(calls[3][1].headers).toMatchObject({ 'X-CSRF-Token': 'C'.repeat(43) })
    expect(calls[3][1].body).toBe('{}')
    for (const n of [4, 5]) expect(calls[n][1].headers).not.toHaveProperty('Idempotency-Key')
  })
  it.each([43, 79, 81])(
    'rejects a %i-character pass on both boundaries without transmitting invalid input',
    async (length) => {
      const fetcher = vi.fn(async () => response({ pass: 'X'.repeat(length) }))
      const api = createAccountAPI(fetcher),
        signal = controller().signal
      await expect(
        api.verifyChallenge(
          { email: user.email, login_key: 'key', challenge_id: id, proof: { angle: 0 } },
          token,
          signal,
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
      expect(() =>
        api.login(
          {
            email: user.email,
            password: 'OriginalPassword123',
            challenge_pass: 'X'.repeat(length),
          },
          token,
          'key',
          signal,
        ),
      ).toThrow(AccountFailure)
      expect(fetcher).toHaveBeenCalledTimes(1)
    },
  )
  it('accepts the same opaque 80-character pass, while keeping CSRF exactly 43', async () => {
    const fetcher = vi.fn(async () => response({ user, session, next_path: '/' }))
    const api = createAccountAPI(fetcher),
      signal = controller().signal
    await api.login(
      { email: user.email, password: 'OriginalPassword123', challenge_pass: pass },
      token,
      'key',
      signal,
    )
    expect(() => api.logout(pass, 'key', signal)).toThrow(AccountFailure)
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
  it.each([
    { ...user, version: 9223372036854775807 },
    { ...user, version: '9223372036854775808' },
    { ...user, role: 'owner' },
    { ...user, id: 'not-an-id' },
  ])('refuses malformed complete session facts', async (badUser) => {
    const api = createAccountAPI(async () =>
      response({ user: badUser, session, csrf_token: token }),
    )
    await expect(api.getSession(controller().signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('rejects off-origin puzzle images and excess combined image material', async () => {
    for (const master of [
      'https://example.com/image.png',
      'data:image/png;base64,' + 'A'.repeat(262140),
    ]) {
      const api = createAccountAPI(async () =>
        response(
          { id, mode: 'rotate', master, thumb: 'data:image/png;base64,AA==', expires_at: instant },
          201,
        ),
      )
      await expect(
        api.createChallenge(
          { mode: 'rotate', email: user.email, login_key: 'key' },
          token,
          controller().signal,
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
  })
  it('keeps a safe Problem separate from malformed, transport and cancellation failures', async () => {
    const problem = {
      type: 'urn:agenteam:problem:commit-unknown',
      title: 'Unknown',
      status: 503,
      detail: '结果未确认',
      instance: '/api/v1/sessions/login',
      code: 'COMMIT_UNKNOWN',
      request_id: id,
      commit_state: 'unknown',
      retry_hint: 'lookup',
      field_errors: [{ path: '/email', code: 'INVALID_ARGUMENT' }],
    }
    const api = createAccountAPI(async () => response(problem, 503, 'application/problem+json'))
    await expect(api.getSession(controller().signal)).rejects.toMatchObject({
      kind: 'problem',
      problem,
    })
    await expect(
      createAccountAPI(async () => response('<secret>', 503, 'text/html')).getSession(
        controller().signal,
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response', problem: undefined })
    const rejected = createAccountAPI(async () => {
      throw new Error('must not escape')
    })
    await expect(rejected.getSession(controller().signal)).rejects.toMatchObject({
      kind: 'transport',
      message: 'transport',
      problem: undefined,
    })
    const aborted = controller()
    aborted.abort()
    await expect(rejected.getSession(aborted.signal)).rejects.toMatchObject({ kind: 'cancelled' })
  })
})
