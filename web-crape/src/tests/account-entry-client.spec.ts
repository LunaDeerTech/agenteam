import { describe, expect, it, vi } from 'vitest'
import { createAccountAPI } from '../api/account'
import { AccountFailure, type Fetch } from '../api/client'

const id = '01900000-0000-7000-8000-000000000001'
const token = id + '.' + 'T'.repeat(43)
const instant = '2026-10-05T12:34:56.123456Z'
const csrfToken = 'A'.repeat(43),
  key = 'original-key'
const password = '  中文 password 12345  '
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })

describe('public Account wire', () => {
  it('uses the five fixed browser endpoints, exact statuses and original Unicode inputs', async () => {
    const results = [
      json({ email: 'member@example.com', expires_at: instant }),
      json({ completed: true, login_required: true }, 201),
      json({ accepted: true, delivery_channel: 'backend_log' }, 202),
      json({ valid: true, expires_at: instant }),
      new Response(null, { status: 204 }),
    ]
    const fetch = vi.fn<Fetch>(async () => results.shift()!)
    const api = createAccountAPI(fetch)
    await api.inspectInvitation({ token }, { csrfToken })
    await api.redeemInvitation(
      { token, username: 'New-Member', display_name: ' 原名称 ', password, confirmation: password },
      { csrfToken, key },
    )
    await api.requestPasswordReset({ email: 'member@example.com' }, { csrfToken, key })
    await api.inspectPasswordReset({ token }, { csrfToken })
    await api.completePasswordReset(
      { token, new_password: password, confirmation: password },
      { csrfToken, key },
    )
    expect(fetch.mock.calls.map((call) => call[0])).toEqual([
      '/api/v1/invitations/inspect',
      '/api/v1/invitations/redeem',
      '/api/v1/password-resets/request',
      '/api/v1/password-resets/inspect',
      '/api/v1/password-resets/complete',
    ])
    fetch.mock.calls.forEach((call, index) => {
      const init = call[1] as RequestInit
      expect(init).toMatchObject({
        method: 'POST',
        credentials: 'same-origin',
        redirect: 'error',
        cache: 'no-store',
      })
      expect(init.headers).toMatchObject({ 'X-CSRF-Token': csrfToken })
      expect((init.headers as Record<string, string>)['Idempotency-Key']).toBe(
        index === 0 || index === 3 ? undefined : key,
      )
    })
    expect(JSON.parse(fetch.mock.calls[1]![1]!.body as string)).toEqual({
      token,
      username: 'New-Member',
      display_name: ' 原名称 ',
      password,
      confirmation: password,
    })
    expect(JSON.parse(fetch.mock.calls[4]![1]!.body as string)).toEqual({
      token,
      new_password: password,
      confirmation: password,
    })
  })

  it.each([
    '',
    token + 'x',
    token.replace('.', '%2E'),
    token.replace('01900000', '0190000A'),
    '#' + token,
    'https://example.com/#' + token,
  ])('rejects a noncanonical capability before transport: %s', async (value) => {
    const fetch = vi.fn()
    const api = createAccountAPI(fetch)
    expect(() => api.inspectInvitation({ token: value }, { csrfToken })).toThrowError(
      new AccountFailure('invalid-input'),
    )
    expect(fetch).not.toHaveBeenCalled()
  })

  it.each([
    { completed: 'true', login_required: true },
    { completed: true, login_required: false },
    { completed: true, login_required: true, user: {} },
  ])('rejects malformed redemption DTOs', async (body) => {
    const api = createAccountAPI(async () => json(body, 201))
    await expect(
      api.redeemInvitation(
        { token, username: 'member', password, confirmation: password },
        { csrfToken, key },
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })

  it.each([
    { accepted: false, delivery_channel: 'smtp' },
    { accepted: true, delivery_channel: 'fallback' },
    { accepted: true, delivery_channel: 'smtp', exists: true },
  ])('does not accept account existence or delivery details', async (body) => {
    const api = createAccountAPI(async () => json(body, 202))
    await expect(
      api.requestPasswordReset({ email: 'member@example.com' }, { csrfToken, key }),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })

  it('rejects bad Instant, HTML, wrong status and redirect, and joins rejected-body cancellation', async () => {
    for (const response of [
      json({ valid: true, expires_at: 'tomorrow' }),
      new Response('<html>secret</html>', { headers: { 'Content-Type': 'text/html' } }),
      json({ valid: true, expires_at: instant }, 201),
    ]) {
      await expect(
        createAccountAPI(async () => response).inspectPasswordReset({ token }, { csrfToken }),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
      expect(response.body?.locked).toBe(false)
    }
    let release!: () => void
    const cancelled = new Promise<void>((resolve) => {
      release = resolve
    })
    const cancel = vi.fn(() => cancelled)
    const response = new Response(new ReadableStream({ cancel }), {
      headers: { 'Content-Type': 'text/html' },
    })
    let finished = false
    const pending = createAccountAPI(async () => response)
      .inspectPasswordReset({ token }, { csrfToken })
      .catch((error: unknown) => {
        finished = true
        return error
      })
    await Promise.resolve()
    await Promise.resolve()
    expect(cancel).toHaveBeenCalledOnce()
    expect(finished).toBe(false)
    release()
    expect(await pending).toMatchObject({ kind: 'invalid-response' })
  })

  it('preserves typed Problem facts without retaining the request body', async () => {
    const problem = {
      type: 'urn:agenteam:problem:conflict',
      title: 'Conflict',
      status: 409,
      detail: 'safe',
      instance: '/api/v1/invitations/redeem',
      code: 'IDEMPOTENCY_KEY_REUSED',
      request_id: id,
      commit_state: 'unknown',
      retry_hint: 'retry_original',
      field_errors: [{ path: '/username', code: 'INVALID' }],
    }
    const api = createAccountAPI(
      async () =>
        new Response(JSON.stringify(problem), {
          status: 409,
          headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id },
        }),
    )
    const failure = await api
      .redeemInvitation(
        { token, username: 'member', password, confirmation: password },
        { csrfToken, key },
      )
      .catch((error: unknown) => error)
    expect(failure).toMatchObject({ kind: 'problem', problem })
    expect(JSON.stringify(failure)).not.toContain(token)
    expect(JSON.stringify(failure)).not.toContain(password)
  })

  it('rejects every nonempty reset 204 body, including whitespace', async () => {
    for (const body of [' ', '{"completed":true}']) {
      const response = new Response(body)
      Object.defineProperty(response, 'status', { value: 204 })
      await expect(
        createAccountAPI(async () => response).completePasswordReset(
          { token, new_password: password, confirmation: password },
          { csrfToken, key },
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
      expect(response.body?.locked).toBe(false)
    }
  })

  it('joins cancellation of a reset 204 body before returning its terminal failure', async () => {
    let release!: () => void
    const cancelled = new Promise<void>((resolve) => {
      release = resolve
    })
    const cancel = vi.fn(() => cancelled)
    const response = new Response(new ReadableStream({ cancel }))
    Object.defineProperty(response, 'status', { value: 204 })
    const abort = new AbortController()
    let finished = false
    const pending = createAccountAPI(async () => response)
      .completePasswordReset(
        { token, new_password: password, confirmation: password },
        { csrfToken, key, signal: abort.signal },
      )
      .catch((error: unknown) => {
        finished = true
        return error
      })
    await Promise.resolve()
    await Promise.resolve()
    abort.abort()
    await Promise.resolve()
    expect(cancel).toHaveBeenCalledOnce()
    expect(finished).toBe(false)
    release()
    expect(await pending).toMatchObject({ kind: 'cancelled' })
    expect(response.body?.locked).toBe(false)
  })

  it('enforces codepoint/UTF8 limits and exact confirmation without trimming', () => {
    const fetch = vi.fn()
    const api = createAccountAPI(fetch)
    for (const value of ['短'.repeat(14), '界'.repeat(129), '😀'.repeat(129)]) {
      expect(() =>
        api.completePasswordReset(
          { token, new_password: value, confirmation: value },
          { csrfToken, key },
        ),
      ).toThrowError(new AccountFailure('invalid-input'))
    }
    expect(() =>
      api.completePasswordReset(
        { token, new_password: password, confirmation: password.trim() },
        { csrfToken, key },
      ),
    ).toThrowError(new AccountFailure('invalid-input'))
    expect(() =>
      api.requestPasswordReset({ email: ' member@example.com ' }, { csrfToken, key }),
    ).toThrowError(new AccountFailure('invalid-input'))
    expect(fetch).not.toHaveBeenCalled()
  })
})
