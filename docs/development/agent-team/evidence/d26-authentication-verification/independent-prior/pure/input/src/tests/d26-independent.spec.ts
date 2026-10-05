import { afterEach, describe, expect, it, vi } from 'vitest'
import { createAccountAPI } from '../api/account'
import { createSessionController } from '../composables/useSession'

const userA = '01900000-0000-7000-8000-000000000101'
const userB = '01900000-0000-7000-8000-000000000102'
const sessionA = '01900000-0000-7000-8000-000000000201'
const sessionB = '01900000-0000-7000-8000-000000000202'
const requestID = '01900000-0000-7000-8000-000000000301'
const time = '2026-10-05T12:34:56.123456Z'
const anonymousCSRF = 'A'.repeat(43)
const sessionCSRF = 'S'.repeat(43)
// Synthetic local inputs. Assertions deliberately avoid displaying request bodies.
const email = 'original@example.test'
const password = '  Synthetic 密码 12345  '

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
function facts(uid = userA, sid = sessionA) {
  return {
    user: {
      id: uid,
      email: uid === userA ? email : 'changed@example.test',
      username: '',
      display_name: '',
      role: 'user',
      theme: 'system',
      version: '9223372036854775807',
      initial_password_suggestion: false,
    },
    session: {
      id: sid,
      issued_at: time,
      absolute_expires_at: time,
      idle_expires_at: time,
    },
  }
}
function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': requestID,
    },
  })
}
function anonymous() {
  return json({
    csrf_token: anonymousCSRF,
    challenge_modes: ['rotate'],
    delivery_channel: 'backend_log',
  })
}
function absent() {
  return json(
    {
      type: 'urn:agenteam:problem:unauthenticated',
      title: 'Unauthenticated',
      status: 401,
      detail: 'Current session unavailable.',
      instance: '/api/v1/session',
      code: 'UNAUTHENTICATED',
      request_id: requestID,
      commit_state: 'not_started',
    },
    401,
  )
}

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('independent D26 fixed core boundaries', () => {
  it('holds the actual Cookie tail across 30s, leave and replacement identity intent', async () => {
    vi.useFakeTimers()
    const cancelEntered = deferred<void>()
    const releaseCancel = deferred<void>()
    const replacementEntered = deferred<void>()
    const replacementResponse = deferred<Response>()
    const calls: string[] = []
    let reads = 0
    let cancellationCount = 0
    let uiReturned = false
    let actualCancellationReturned = false
    const api = createAccountAPI(async (url) => {
      calls.push(url)
      if (url === '/api/v1/auth/bootstrap') return anonymous()
      if (url === '/api/v1/session') {
        ++reads
        if (reads === 1) return absent()
        replacementEntered.resolve()
        return replacementResponse.promise
      }
      if (url === '/api/v1/sessions/login') {
        return new Response(
          new ReadableStream<Uint8Array>({
            cancel() {
              ++cancellationCount
              cancelEntered.resolve()
              return releaseCancel.promise.then(() => {
                actualCancellationReturned = true
              })
            },
          }),
          { status: 200, headers: { 'Content-Type': 'text/html' } },
        )
      }
      throw new Error('Unexpected controlled endpoint')
    })
    const c = createSessionController(api)
    let old: Promise<void> | undefined
    let replacement: Promise<void> | undefined
    try {
      await c.restore()
      old = c.login(email, password).then(() => {
        uiReturned = true
      })
      await cancelEntered.promise
      expect(cancellationCount).toBe(1)
      expect(actualCancellationReturned).toBe(false)
      expect(c.state.busy).toBe(true)
      await vi.advanceTimersByTimeAsync(30000)
      await old
      expect(uiReturned).toBe(true)
      expect(c.state.phase).toBe('uncertain')
      expect(c.state.busy).toBe(true)
      const admittedBeforeLeave = calls.length
      c.leave()
      await c.restart()
      await c.restore()
      await c.login('replacement@example.test', password)
      await c.logout()
      expect(calls.length).toBe(admittedBeforeLeave)
      expect(c.state.phase).toBe('checking')
      expect(c.state.user).toBeNull()
      expect(c.state.busy).toBe(true)
      expect(actualCancellationReturned).toBe(false)

      releaseCancel.resolve()
      await vi.advanceTimersByTimeAsync(0)
      expect(actualCancellationReturned).toBe(true)
      expect(c.state.busy).toBe(false)
      expect(c.state.user).toBeNull()
      replacement = c.restore()
      await replacementEntered.promise
      expect(reads).toBe(2)
      expect(c.state.busy).toBe(true)
      await old
      await vi.advanceTimersByTimeAsync(0)
      expect(c.state.busy).toBe(true)
      expect(c.state.user).toBeNull()
      replacementResponse.resolve(json({ ...facts(userB, sessionB), csrf_token: sessionCSRF }))
      await replacement
      expect(c.state.busy).toBe(false)
      expect(c.state.phase).toBe('authenticated')
      expect(c.state.user?.id === userB && c.state.session?.id === sessionB).toBe(true)
      expect(calls.filter((url) => url === '/api/v1/sessions/login').length).toBe(1)
      expect(calls.filter((url) => url === '/api/v1/sessions/logout').length).toBe(0)
    } finally {
      releaseCancel.resolve()
      replacementResponse.resolve(json({ ...facts(userB, sessionB), csrf_token: sessionCSRF }))
      await vi.advanceTimersByTimeAsync(0)
      await old
      await replacement
      c.leave()
    }
  })

  it('rejects either identity mismatch after login200 and retains the next query owner', async () => {
    vi.useFakeTimers()
    // Each ID is independently required; no test can pass merely by matching the other one.
    for (const changed of ['user', 'session'] as const) {
      const confirmEntered = deferred<void>()
      const confirmResponse = deferred<Response>()
      const nextEntered = deferred<void>()
      const nextResponse = deferred<Response>()
      let reads = 0
      let writes = 0
      const api = createAccountAPI(async (url) => {
        if (url === '/api/v1/auth/bootstrap') return anonymous()
        if (url === '/api/v1/session') {
          ++reads
          if (reads === 1) return absent()
          if (reads === 2) {
            confirmEntered.resolve()
            return confirmResponse.promise
          }
          nextEntered.resolve()
          return nextResponse.promise
        }
        if (url === '/api/v1/sessions/login') {
          ++writes
          return json({ ...facts(), next_path: '/' })
        }
        ++writes
        throw new Error('Unexpected controlled write')
      })
      const c = createSessionController(api)
      let login: Promise<void> | undefined
      let next: Promise<void> | undefined
      try {
        await c.restore()
        login = c.login(email, password)
        await confirmEntered.promise
        expect(c.state.phase).toBe('authenticating')
        expect(c.state.user).toBeNull()
        expect(c.state.busy).toBe(true)
        await c.logout()
        expect(writes).toBe(1)
        confirmResponse.resolve(
          json({
            ...facts(changed === 'user' ? userB : userA, changed === 'session' ? sessionB : sessionA),
            csrf_token: sessionCSRF,
          }),
        )
        await login
        expect(c.state.phase).toBe('unavailable')
        expect(c.state.user).toBeNull()
        expect(c.state.session).toBeNull()
        expect(c.state.busy).toBe(false)
        await c.logout()
        expect(writes).toBe(1)

        next = c.restore()
        await nextEntered.promise
        await login
        await vi.advanceTimersByTimeAsync(0)
        expect(c.state.phase).toBe('checking')
        expect(c.state.busy).toBe(true)
        expect(c.state.user).toBeNull()
        await c.logout()
        expect(writes).toBe(1)
        nextResponse.resolve(json({ ...facts(), csrf_token: sessionCSRF }))
        await next
        expect(c.state.phase).toBe('authenticated')
        expect(c.state.user?.id === userA && c.state.session?.id === sessionA).toBe(true)
        expect(c.state.busy).toBe(false)
        expect(reads).toBe(3)
      } finally {
        confirmResponse.resolve(json({ ...facts(), csrf_token: sessionCSRF }))
        nextResponse.resolve(json({ ...facts(), csrf_token: sessionCSRF }))
        await login
        await next
        c.leave()
      }
    }
  })
})
