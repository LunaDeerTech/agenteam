import { afterEach, describe, expect, it, vi } from 'vitest'
import { createSessionController } from '../composables/useSession'
import { AccountFailure, type Problem } from '../api/client'
import { createAccountAPI, type AccountAPI, type SessionView } from '../api/account'
import { createProjectModelSettingsAPI } from '../api/project-models'

const id = '01900000-0000-7000-8000-000000000001'
const time = '2026-10-05T12:34:56.123456Z'
const anonymous = 'A'.repeat(43),
  sessionToken = 'S'.repeat(43)
const password = '  Original 密码 12345  '
const view: SessionView = {
  user: {
    id,
    email: 'person@example.com',
    username: '',
    display_name: 'Person',
    role: 'user',
    theme: 'dark',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id, issued_at: time, absolute_expires_at: time, idle_expires_at: time },
  csrf_token: sessionToken,
}
function problem(
  code: string,
  status = 401,
  commit_state: Problem['commit_state'] = 'not_started',
) {
  return new AccountFailure('problem', {
    type: 'urn:agenteam:problem:test',
    title: 'Test',
    status,
    detail: 'safe error',
    instance: '/api/v1/session',
    code,
    request_id: id,
    commit_state,
  })
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((a, b) => {
    resolve = a
    reject = b
  })
  return { promise, resolve, reject }
}
function fixture() {
  const api = {
    getProfile: vi.fn<AccountAPI['getProfile']>(async () => {
      throw new Error('unexpected personal request')
    }),
    updateProfile: vi.fn<AccountAPI['updateProfile']>(async () => {
      throw new Error('unexpected personal request')
    }),
    getPreferences: vi.fn<AccountAPI['getPreferences']>(async () => {
      throw new Error('unexpected personal request')
    }),
    setPreferences: vi.fn<AccountAPI['setPreferences']>(async () => {
      throw new Error('unexpected personal request')
    }),
    readAvatar: vi.fn<AccountAPI['readAvatar']>(async () => {
      throw new Error('unexpected personal request')
    }),
    putAvatar: vi.fn<AccountAPI['putAvatar']>(async () => {
      throw new Error('unexpected personal request')
    }),
    deleteAvatar: vi.fn<AccountAPI['deleteAvatar']>(async () => {
      throw new Error('unexpected personal request')
    }),
    changePassword: vi.fn<AccountAPI['changePassword']>(async () => {
      throw new Error('unexpected personal request')
    }),
    inspectInvitation: vi.fn<AccountAPI['inspectInvitation']>(async () => {
      throw new Error('unexpected public entry request')
    }),
    redeemInvitation: vi.fn<AccountAPI['redeemInvitation']>(async () => {
      throw new Error('unexpected public entry request')
    }),
    requestPasswordReset: vi.fn<AccountAPI['requestPasswordReset']>(async () => {
      throw new Error('unexpected public entry request')
    }),
    inspectPasswordReset: vi.fn<AccountAPI['inspectPasswordReset']>(async () => {
      throw new Error('unexpected public entry request')
    }),
    completePasswordReset: vi.fn<AccountAPI['completePasswordReset']>(async () => {
      throw new Error('unexpected public entry request')
    }),
    bootstrap: vi.fn<AccountAPI['bootstrap']>(async () => ({
      csrf_token: anonymous,
      challenge_modes: ['rotate'] as ['rotate'],
      delivery_channel: 'backend_log' as const,
    })),
    getSession: vi.fn<AccountAPI['getSession']>(async (): Promise<SessionView> => {
      throw problem('UNAUTHENTICATED')
    }),
    login: vi.fn<AccountAPI['login']>(async () => ({
      user: view.user,
      session: view.session,
      next_path: '/' as const,
    })),
    logout: vi.fn<AccountAPI['logout']>(async () => undefined),
    createChallenge: vi.fn<AccountAPI['createChallenge']>(async () => ({
      id,
      mode: 'rotate' as const,
      master: 'data:image/png;base64,AA==',
      thumb: 'data:image/png;base64,AA==',
      expires_at: time,
    })),
    verifyChallenge: vi.fn<AccountAPI['verifyChallenge']>(async () => ({
      pass: id + '.' + 'P'.repeat(43),
    })),
  } satisfies AccountAPI
  return { api, controller: createSessionController(api) }
}
afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})
describe('Account Session ownership', () => {
  it.each(['content-type', 'oversize'])(
    'retains Cookie ownership until response body cancellation joins: %s',
    async (mode) => {
      vi.useFakeTimers()
      const release = deferred<void>()
      let cancellations = 0,
        reads = 0,
        returned = false
      const json = (data: unknown, status = 200) =>
        new Response(JSON.stringify(data), {
          status,
          headers: {
            'Content-Type': status === 200 ? 'application/json' : 'application/problem+json',
            'X-Request-ID': id,
          },
        })
      const api = createAccountAPI(async (url) => {
        if (url.endsWith('/bootstrap'))
          return json({
            csrf_token: anonymous,
            challenge_modes: ['rotate'],
            delivery_channel: 'backend_log',
          })
        if (url.endsWith('/session')) {
          ++reads
          return json(problem('UNAUTHENTICATED').problem, 401)
        }
        const stream = new ReadableStream<Uint8Array>({
          start(stream) {
            if (mode === 'oversize') stream.enqueue(new Uint8Array(600001))
          },
          cancel() {
            ++cancellations
            return release.promise
          },
        })
        return new Response(stream, {
          status: 200,
          headers: { 'Content-Type': mode === 'content-type' ? 'text/html' : 'application/json' },
        })
      })
      const c = createSessionController(api)
      await c.restore()
      const call = c.login(view.user.email, password).then(() => {
        returned = true
      })
      try {
        await vi.advanceTimersByTimeAsync(0)
        expect(cancellations).toBe(1)
        expect(c.state.busy).toBe(true)
        await c.restore()
        await c.logout()
        expect(reads).toBe(1)
        await vi.advanceTimersByTimeAsync(30000)
        expect(returned).toBe(true)
        expect(c.state.phase).toBe('uncertain')
        expect(c.state.busy).toBe(true)
      } finally {
        release.resolve()
        await call
        await vi.advanceTimersByTimeAsync(0)
      }
      expect(c.state.busy).toBe(false)
    },
  )
  it.each(['create', 'verify'])(
    'bounds challenge user waiting while the real reader ignores abort: %s',
    async (mode) => {
      vi.useFakeTimers()
      const cancelReturn = deferred<void>()
      let stream!: ReadableStreamDefaultController<Uint8Array>
      let cancelled = false,
        held = true,
        returned = false
      const secondID = '01900000-0000-7000-8000-000000000002'
      const question = (id: string) => ({
        id,
        mode: 'rotate',
        master: 'data:image/png;base64,AA==',
        thumb: 'data:image/png;base64,AA==',
        expires_at: time,
      })
      const json = (body: unknown, status = 200) =>
        new Response(JSON.stringify(body), {
          status,
          headers: {
            'Content-Type': status < 400 ? 'application/json' : 'application/problem+json',
            'X-Request-ID': id,
          },
        })
      const api = createAccountAPI(async (url) => {
        if (url.endsWith('/bootstrap'))
          return json({
            csrf_token: anonymous,
            challenge_modes: ['rotate'],
            delivery_channel: 'backend_log',
          })
        if (url.endsWith('/session')) return json(problem('UNAUTHENTICATED').problem, 401)
        if (url.endsWith('/login')) return json(problem('CHALLENGE_REQUIRED').problem, 401)
        if (held && url.endsWith(mode === 'create' ? '/challenges' : '/verify')) {
          held = false
          return new Response(
            new ReadableStream<Uint8Array>({
              start(c) {
                stream = c
              },
              cancel() {
                cancelled = true
                return cancelReturn.promise
              },
            }),
            {
              status: mode === 'create' ? 201 : 200,
              headers: { 'Content-Type': 'application/json' },
            },
          )
        }
        return url.endsWith('/challenges')
          ? json(question(secondID), 201)
          : json({ pass: secondID + '.' + 'N'.repeat(43) })
      })
      const c = createSessionController(api)
      await c.restore()
      await c.login(view.user.email, password)
      if (mode === 'verify') await c.createChallenge()
      const call = (mode === 'create' ? c.createChallenge() : c.verifyChallenge(60)).then(() => {
        returned = true
      })
      try {
        await vi.advanceTimersByTimeAsync(30000)
        expect(returned).toBe(true)
        expect(c.state.challengeBusy).toBe(false)
        expect(c.state.passReady).toBe(false)
        expect(c.state.challenge).toBeNull()
        await c.createChallenge()
        await c.verifyChallenge(12)
        expect(c.state.passReady).toBe(true)
      } finally {
        if (!cancelled) {
          stream.enqueue(
            new TextEncoder().encode(
              JSON.stringify(
                mode === 'create' ? question(id) : { pass: id + '.' + 'O'.repeat(43) },
              ),
            ),
          )
          stream.close()
        }
        cancelReturn.resolve()
        await call
        await vi.advanceTimersByTimeAsync(0)
      }
      expect(c.state.passReady).toBe(true)
      expect(c.state.challengeBusy).toBe(false)
    },
  )
  it.each(['pending', 'unknown'])(
    'dismiss cannot alter the complete original request after a pass-bearing login is %s',
    async (timing) => {
      const { api, controller: c } = fixture()
      await c.restore()
      api.login.mockRejectedValueOnce(problem('CHALLENGE_REQUIRED'))
      await c.login(view.user.email, password)
      await c.createChallenge()
      await c.verifyChallenge(75)
      const held = deferred<Awaited<ReturnType<AccountAPI['login']>>>()
      api.login.mockImplementationOnce(() => held.promise)
      const call = c.login(view.user.email, password)
      await Promise.resolve()
      const original = structuredClone(api.login.mock.calls[1].slice(0, 3))
      if (timing === 'pending') c.dismissChallenge()
      held.reject(problem('COMMIT_UNKNOWN', 503, 'unknown'))
      await call
      if (timing === 'unknown') c.dismissChallenge()
      api.login.mockRejectedValueOnce(problem('COMMIT_UNKNOWN', 503, 'unknown'))
      await c.retryOriginal()
      expect(JSON.stringify(api.login.mock.calls[2].slice(0, 3)) === JSON.stringify(original)).toBe(
        true,
      )
      expect(c.state.phase).toBe('uncertain')
    },
  )
  it('the real client treats malformed commit_state as unconfirmed and retains the complete login request', async () => {
    const writes: { body: BodyInit | null | undefined; csrf: string; key: string }[] = []
    const api = createAccountAPI(async (url, init) => {
      const headers = new Headers({ 'Content-Type': 'application/json', 'X-Request-ID': id })
      if (url === '/api/v1/auth/bootstrap')
        return new Response(
          JSON.stringify({
            csrf_token: anonymous,
            challenge_modes: ['rotate'],
            delivery_channel: 'backend_log',
          }),
          { status: 200, headers },
        )
      headers.set('Content-Type', 'application/problem+json')
      if (url === '/api/v1/session')
        return new Response(JSON.stringify(problem('UNAUTHENTICATED').problem), {
          status: 401,
          headers,
        })
      const sent = new Headers(init.headers)
      writes.push({
        body: init.body,
        csrf: sent.get('X-CSRF-Token')!,
        key: sent.get('Idempotency-Key')!,
      })
      return new Response(
        JSON.stringify({
          ...problem('DEPENDENCY_UNAVAILABLE', 503).problem,
          commit_state: ['unknown'],
        }),
        { status: 503, headers },
      )
    })
    const c = createSessionController(api)
    await c.restore()
    await c.login(view.user.email, password)
    expect(c.state.phase).toBe('uncertain')
    await c.retryOriginal()
    expect(writes).toHaveLength(2)
    expect(JSON.stringify(writes[0]) === JSON.stringify(writes[1])).toBe(true)
  })
  it('distinguishes actual anonymous from unavailable, then confirms Session CSRF after login', async () => {
    const { api, controller: c } = fixture()
    await c.restore()
    expect(c.state.phase).toBe('anonymous')
    api.getSession.mockRejectedValueOnce(problem('DEPENDENCY_UNAVAILABLE', 503))
    await c.restore()
    expect(c.state.phase).toBe('unavailable')
    expect(api.bootstrap).toHaveBeenCalledTimes(1)
    await c.restart()
    api.getSession.mockResolvedValueOnce(view)
    await c.login(view.user.email, password)
    expect(c.state.phase).toBe('authenticated')
    expect(api.login.mock.calls[0][0]).toEqual({ email: view.user.email, password })
    expect(api.login.mock.calls[0][1]).toBe(anonymous)
    await c.logout()
    expect(api.logout.mock.calls[0][0]).toBe(sessionToken)
    expect(c.state.phase).toBe('anonymous')
    expect(JSON.stringify(c.state)).not.toContain(password)
    expect(JSON.stringify(c.state)).not.toContain(sessionToken)
  })
  it('retains the exact key and password after Unknown and does not bootstrap after a negative current observation', async () => {
    const { api, controller: c } = fixture()
    await c.restore()
    api.login.mockRejectedValue(problem('COMMIT_UNKNOWN', 503, 'unknown'))
    await c.login(view.user.email, password)
    expect(c.state.phase).toBe('uncertain')
    await c.restore()
    expect(c.state.phase).toBe('uncertain')
    expect(api.bootstrap).toHaveBeenCalledTimes(1)
    await c.retryOriginal()
    expect(api.login.mock.calls).toHaveLength(2)
    expect(api.login.mock.calls[1].slice(0, 3)).toEqual(api.login.mock.calls[0].slice(0, 3))
    expect(c.state.canRetryOriginal).toBe(true)
    api.login.mockRejectedValueOnce(problem('CSRF_FAILED', 403))
    await c.retryOriginal()
    expect(c.state.phase).toBe('uncertain')
    expect(c.state.canRetryOriginal).toBe(false)
    await c.restore()
    expect(api.bootstrap).toHaveBeenCalledTimes(1)
  })
  it('adds a real verify pass to the same login intent and invalidates it on changed input', async () => {
    const { api, controller: c } = fixture()
    await c.restore()
    api.login.mockRejectedValueOnce(problem('CHALLENGE_REQUIRED'))
    await c.login(view.user.email, password)
    await c.createChallenge()
    await c.verifyChallenge(270)
    expect(c.state.passReady).toBe(true)
    api.getSession.mockResolvedValueOnce(view)
    await c.login(view.user.email, password)
    expect(api.login.mock.calls[1][2]).toBe(api.login.mock.calls[0][2])
    expect(api.login.mock.calls[1][0]).toEqual({
      email: view.user.email,
      password,
      challenge_pass: id + '.' + 'P'.repeat(43),
    })
    expect(api.verifyChallenge.mock.calls[0][0].proof.angle).toBe(270)
    c.leave()
    await c.restore()
    api.login.mockRejectedValueOnce(problem('CHALLENGE_REQUIRED'))
    await c.login(view.user.email, password)
    await c.createChallenge()
    await c.verifyChallenge(10)
    c.inputChanged()
    expect(c.state.passReady).toBe(false)
    expect(c.state.challenge).toBeNull()
  })
  it('does not expose identity or send another Cookie request before the real login tail returns', async () => {
    const { api, controller: c } = fixture()
    await c.restore()
    const held = deferred<Awaited<ReturnType<AccountAPI['login']>>>()
    api.login.mockImplementationOnce(() => held.promise)
    const first = c.login(view.user.email, password)
    await Promise.resolve()
    c.leave()
    await c.restore()
    expect(api.getSession).toHaveBeenCalledTimes(1)
    expect(c.state.busy).toBe(true)
    held.resolve({ user: view.user, session: view.session, next_path: '/' })
    await first
    expect(c.state.user).toBeNull()
    expect(c.state.phase).toBe('checking')
    expect(c.state.busy).toBe(false)
    api.getSession.mockResolvedValueOnce(view)
    await c.restore()
    expect(c.state.phase).toBe('authenticated')
  })
  it('bounds UI waiting at 30s while an abort-ignoring fetch still owns the Cookie lane', async () => {
    vi.useFakeTimers()
    const { api, controller: c } = fixture()
    await c.restore()
    const held = deferred<Awaited<ReturnType<AccountAPI['login']>>>()
    api.login.mockImplementationOnce(() => held.promise)
    const first = c.login(view.user.email, password)
    await vi.advanceTimersByTimeAsync(30_000)
    await first
    expect(c.state.phase).toBe('uncertain')
    expect(c.state.busy).toBe(true)
    expect(api.login.mock.calls[0][3].aborted).toBe(true)
    await c.restore()
    await c.retryOriginal()
    await c.logout()
    expect(api.getSession).toHaveBeenCalledTimes(1)
    expect(api.login).toHaveBeenCalledTimes(1)
    held.resolve({ user: view.user, session: view.session, next_path: '/' })
    await vi.runAllTimersAsync()
    expect(c.state.busy).toBe(false)
    expect(c.state.user).toBeNull()
    expect(c.state.phase).toBe('uncertain')
  })
  it('a late challenge/verify/finally cannot overwrite a newer question or release its busy state', async () => {
    const { api, controller: c } = fixture()
    await c.restore()
    api.login.mockRejectedValueOnce(problem('CHALLENGE_REQUIRED'))
    await c.login(view.user.email, password)
    const older = deferred<Awaited<ReturnType<AccountAPI['createChallenge']>>>()
    const newer = deferred<Awaited<ReturnType<AccountAPI['createChallenge']>>>()
    api.createChallenge
      .mockImplementationOnce(() => older.promise)
      .mockImplementationOnce(() => newer.promise)
    const first = c.createChallenge()
    await Promise.resolve()
    expect(api.createChallenge).toHaveBeenCalledTimes(1)
    const second = c.createChallenge()
    await Promise.resolve()
    expect(api.createChallenge).toHaveBeenCalledTimes(2)
    older.resolve({ id, mode: 'rotate', master: 'old', thumb: 'old', expires_at: time })
    await first
    expect(c.state.challengeBusy).toBe(true)
    expect(c.state.challenge).toBeNull()
    newer.resolve({ id, mode: 'rotate', master: 'new', thumb: 'new', expires_at: time })
    await second
    expect(c.state.challenge?.master).toBe('new')
    const verify = deferred<{ pass: string }>()
    api.verifyChallenge.mockImplementationOnce(() => verify.promise)
    const pendingVerify = c.verifyChallenge(1)
    await Promise.resolve()
    expect(api.verifyChallenge).toHaveBeenCalledTimes(1)
    c.inputChanged()
    verify.resolve({ pass: id + '.' + 'P'.repeat(43) })
    await pendingVerify
    expect(c.state.passReady).toBe(false)
    expect(c.state.challenge).toBeNull()
  })
  it('logout stays unconfirmed until the real 204 and preserves the original key on explicit replay', async () => {
    const { api, controller: c } = fixture()
    api.getSession.mockResolvedValue(view)
    await c.restore()
    api.logout.mockRejectedValueOnce(problem('COMMIT_UNKNOWN', 503, 'unknown'))
    await c.logout()
    expect(c.state.phase).toBe('uncertain')
    await c.restore()
    expect(c.state.phase).toBe('uncertain')
    await c.retryOriginal()
    expect(api.logout.mock.calls[1].slice(0, 2)).toEqual(api.logout.mock.calls[0].slice(0, 2))
    expect(c.state.phase).toBe('anonymous')
  })
})

// The additional Owner facade shares the original Cookie owner and does not
// weaken any existing personal/System gate.
describe('Project facade compatibility', () => {
  it.each(['Owner', 'Model'] as const)(
    'keeps all old mutators behind an active Project %s actual tail',
    async (domain) => {
      const { api } = fixture()
      api.getSession.mockResolvedValue(view)
      const tail = deferred<import('../api/project-owner').Project>()
      const value: import('../api/project-owner').Project = {
        id,
        owner_user_id: id,
        name: 'demo',
        normalized_name: 'demo',
        description: '',
        lifecycle: 'active',
        version: '1',
        current_sprint_id: null,
        created_at: time,
        updated_at: time,
        archived_at: null,
      }
      const projects: import('../api/project-owner').ProjectOwnerAPI = {
        list: vi.fn(),
        resolve: vi.fn(),
        get: vi.fn(async () => tail.promise),
        update: vi.fn(),
        lookup: vi.fn(),
      }
      const metadataTail =
        deferred<import('../api/project-model-credentials').ProjectCredentialMetadata>()
      const projectModels = {
        ...createProjectModelSettingsAPI(),
        getCredentialMetadata: vi.fn(async () => metadataTail.promise),
      }
      const auth = createSessionController(
        api,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        projects,
        undefined,
        projectModels,
      )
      await auth.restore()
      const reading =
        domain === 'Owner'
          ? auth.projects.get(id)
          : auth.projectModelSettings.getCredentialMetadata(id, id)
      try {
        await Promise.resolve()
        await Promise.resolve()
        const mutations = [
          () => auth.personal.updateProfile({ version: '1', display_name: 'x' }),
          () => auth.personal.setPreferences({ version: '1', theme: 'dark' }),
          () =>
            auth.personal.putAvatar({
              version: '1',
              file: new File([], 'x.png'),
              mediaType: 'image/png',
            }),
          () => auth.personal.deleteAvatar({ version: '1' }),
          () => auth.personal.changePassword({} as never),
          () => auth.system.createInvitation({ email: 'invite@example.test' }),
          () => auth.system.providers.start({} as never),
          () => auth.system.models.start({} as never),
          () => auth.system.selection.start({} as never),
          () => auth.system.selection.meetingSummary.start({} as never),
          () => auth.system.accountSecurity.start({} as never),
          () => auth.system.smtp.startUpdate({} as never),
          () => auth.system.smtpDelivery.startTest({} as never),
          () => auth.system.outboundPolicy.startUpdate({} as never),
          () => auth.projects.startUpdate(id, { expected_version: '1', description: 'value' }),
          () => auth.projectModelSettings.createProvider(id, {} as never),
          () => auth.projectModelSettings.updateProvider(id, id, '1', {} as never),
          () => auth.projectModelSettings.deleteProvider(id, id, '1'),
          () => auth.projectModelSettings.createModel(id, {} as never, {} as never),
          () => auth.projectModelSettings.updateModel(id, {} as never, '1', {} as never),
          () => auth.projectModelSettings.deleteModel(id, id, '1', null),
          () => auth.projectModelSettings.createCredential(id, 'private'),
          () => auth.projectModelSettings.updateCredential(id, id, '1', 'private'),
          () => auth.projectModelSettings.deleteCredential(id, id, '1'),
          () => auth.projectModelSettings.lookupConfiguration(),
          () => auth.projectModelSettings.lookupCredential(),
        ]
        for (const mutation of mutations)
          await expect(mutation()).rejects.toMatchObject({ kind: 'busy' })
        expect(api.updateProfile).not.toHaveBeenCalled()
        expect(api.logout).not.toHaveBeenCalled()
        expect(auth.state.busy).toBe(true)
      } finally {
        tail.resolve(value)
        metadataTail.resolve({ credential_id: id, purpose: 'model', version: '1' })
        await reading
        auth.leave()
      }
    },
  )
  it('Project forbidden preserves independent admin authority and System denial preserves Owner reads', async () => {
    const { api } = fixture()
    api.getSession.mockResolvedValue({
      ...view,
      user: { ...view.user, username: 'admin', role: 'admin' },
    })
    const users = { listUsers: vi.fn(async () => ({ items: [] })) }
    const projects: import('../api/project-owner').ProjectOwnerAPI = {
      list: vi.fn(async () => ({ items: [], next_cursor: null })),
      resolve: vi.fn(),
      get: vi.fn(async () => {
        throw problem('FORBIDDEN', 403)
      }),
      update: vi.fn(),
      lookup: vi.fn(),
    }
    const auth = createSessionController(
      api,
      users,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      projects,
    )
    try {
      await auth.restore()
      await expect(auth.projects.get(id)).rejects.toMatchObject({ kind: 'problem' })
      expect(auth.system.denied).toBe(false)
      await expect(auth.system.listUsers({})).resolves.toEqual({ items: [] })
      users.listUsers.mockRejectedValueOnce(problem('FORBIDDEN', 403))
      await expect(auth.system.listUsers({})).rejects.toMatchObject({ kind: 'problem' })
      expect(auth.system.denied).toBe(true)
      await expect(auth.projects.list({ limit: 25 })).resolves.toEqual({
        items: [],
        next_cursor: null,
      })
      expect(auth.system.denied).toBe(true)
    } finally {
      auth.leave()
    }
  })
})
