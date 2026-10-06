import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { watch } from 'vue'
import { createAccountAPI, type SessionView } from '../api/account'
import { type CommitState, type Fetch } from '../api/client'
import { createSystemAccountAPI } from '../api/system-account'
import { createSystemInvitationAPI } from '../api/system-invitations'
import { createSystemProviderAPI } from '../api/system-providers'
import { createSystemModelAPI } from '../api/system-models'
import { createSystemModelSelectionAPI } from '../api/system-model-selection'
import {
  createSystemAccountSecurityAPI,
  type AccountSecurityUpdateInput,
} from '../api/system-account-security'
import { createSessionController, type SessionController } from '../composables/useSession'
import { useTheme } from '../composables/useTheme'
import {
  createSystemAccountSecurity,
  type SystemAccountSecurityController,
} from '../composables/useSystemAccountSecurity'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const time = '2026-10-06T12:34:56.123456Z'
const view = (sessionID = id(2), role: 'admin' | 'user' = 'admin'): SessionView => ({
  user: {
    id: id(1),
    email: 'admin@example.com',
    username: 'admin',
    display_name: 'Admin',
    role,
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: sessionID, issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const values = () => ({
  session_idle_seconds: '604800',
  session_absolute_seconds: '2592000',
  password_reset_seconds: '1800',
  challenge_after_failures: '5',
})
const input = (): AccountSecurityUpdateInput => ({ version: '1', ...values() })
const settings = (version = '1') => ({
  id: id(10),
  version,
  ...values(),
  lifetime_changes_apply_to: 'newly_issued_sessions_and_tokens',
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(9),
    },
  })
const problem = (code: string, status = 503, commit_state: CommitState = 'not_started') =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Failure',
      status,
      code,
      detail: 'private detail',
      instance: '/api/v1/system/account-settings',
      request_id: id(9),
      commit_state,
      retry_hint: 'lookup',
    },
    status,
  )
function barrier<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((done) => (resolve = done))
  return { promise, resolve }
}
const owners: SessionController[] = []
const pages: SystemAccountSecurityController[] = []
async function fixture() {
  let current = view()
  let session: Fetch = async () => json(current)
  let security: Fetch = async (_, init) => json(settings(init.method === 'PUT' ? '2' : '1'))
  let other: Fetch = async (path) => {
    if (path === '/api/v1/me') return json({ user: current.user, avatar: null })
    if (path === '/api/v1/system/model-selection')
      return json({ id: id(20), version: '1', configured: null })
    return json({ items: [], next_cursor: null })
  }
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return session(path, init)
    if (path === '/api/v1/auth/bootstrap')
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path.endsWith('/logout')) return new Response(null, { status: 204 })
    return path === '/api/v1/system/account-settings' ? security(path, init) : other(path, init)
  })
  const auth = createSessionController(
    createAccountAPI(fetch),
    createSystemAccountAPI(fetch),
    createSystemInvitationAPI(fetch),
    createSystemProviderAPI(fetch),
    createSystemModelAPI(fetch),
    createSystemModelSelectionAPI(fetch),
    createSystemAccountSecurityAPI(fetch),
  )
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetch,
    setSecurity(value: Fetch) {
      security = value
    },
    setOther(value: Fetch) {
      other = value
    },
    setSession(value: SessionView) {
      current = value
    },
    setSessionRequest(value: Fetch) {
      session = value
    },
    writes() {
      return fetch.mock.calls.filter(
        ([path, init]) => path === '/api/v1/system/account-settings' && init.method === 'PUT',
      )
    },
  }
}
afterEach(() => {
  for (const page of pages.splice(0)) page.dispose()
  for (const auth of owners.splice(0)) auth.leave()
  vi.useRealTimers()
  vi.restoreAllMocks()
  useTheme().setTheme('system')
})

async function controller() {
  const f = await fixture(),
    page = createSystemAccountSecurity(f.auth)
  pages.push(page)
  page.attach()
  await flushPromises()
  return { ...f, page }
}

describe('Account security current observation, draft and historical result', () => {
  it('starts from the successful GET, keeps raw strings and performs no unchanged or invalid save', async () => {
    const f = await controller(),
      p = f.page
    expect(p.draft).toEqual(values())
    expect(p.canSave.value).toBe(false)
    await p.save()
    p.updateField('session_idle_seconds', ' 900')
    expect(p.draft.session_idle_seconds).toBe(' 900')
    expect(p.errors.value.session_idle_seconds).toBeTruthy()
    await p.save()
    p.updateField('session_idle_seconds', '2592000')
    p.updateField('session_absolute_seconds', '3600')
    expect(p.errors.value.session_idle_seconds).toBeTruthy()
    await p.save()
    expect(f.writes()).toHaveLength(0)
    p.updateField('session_idle_seconds', '900')
    p.updateField('session_absolute_seconds', '2592000')
    expect(p.canSave.value).toBe(true)
    let current = settings()
    f.setSecurity(async (_, init) => {
      if (init.method === 'PUT')
        current = { ...settings('2'), ...JSON.parse(init.body as string), version: '2' }
      return json(current)
    })
    await p.save()
    expect(f.writes()).toHaveLength(1)
    expect(p.state.confirmed?.version).toBe('2')
    expect(p.editor.version).toBe('2')
    expect(p.feedback.value).toBe('success')
    expect(p.dirty.value).toBe(false)
    p.updateField('password_reset_seconds', '3600')
    expect(p.feedback.value).toBe('idle')
    expect(f.writes()).toHaveLength(1)
    expect(p.canSave.value).toBe(true)
  })

  it('keeps conflict draft and version until explicit adoption, with no rebase from a current GET', async () => {
    const f = await controller(),
      p = f.page
    p.updateField('session_idle_seconds', '900')
    f.setSecurity(async (_, init) =>
      init.method === 'PUT'
        ? problem('VERSION_CONFLICT', 409)
        : json({ ...settings('2'), session_idle_seconds: '1800' }),
    )
    await p.save()
    expect(p.editor.conflict).toBe(true)
    await p.readLatest()
    expect(p.observation.value?.version).toBe('2')
    expect(p.editor.version).toBe('1')
    expect(p.draft.session_idle_seconds).toBe('900')
    await p.save()
    expect(f.writes()).toHaveLength(1)
    const continued = p.adoptLatest()
    expect(p.confirmation.open).toBe(true)
    p.finishConfirmation(false)
    await continued
    expect(p.editor.version).toBe('1')
    const adopted = p.adoptLatest()
    p.finishConfirmation(true)
    await adopted
    expect(p.editor.version).toBe('2')
    expect(p.draft.session_idle_seconds).toBe('1800')
    expect(p.dirty.value).toBe(false)
    expect(f.writes()).toHaveLength(1)
    p.updateField('session_idle_seconds', '900')
    expect(p.canSave.value).toBe(true)
  })

  it('preserves a higher current observation and original draft until a historical PUT confirms independently', async () => {
    const f = await controller(),
      p = f.page
    p.updateField('password_reset_seconds', '3600')
    f.setSecurity(async () => {
      throw new Error('lost')
    })
    await p.save()
    const first = f.writes()[0]![1]
    expect(p.progress.value?.phase).toBe('uncertain')
    f.setSecurity(async (_, init) =>
      init.method === 'GET'
        ? json({ ...settings('3'), password_reset_seconds: '7200' })
        : json({ ...settings('2'), password_reset_seconds: '3600' }),
    )
    await p.checkOriginal()
    expect(p.state.confirmed).toBeNull()
    expect(p.observation.value?.version).toBe('3')
    expect(p.editor.version).toBe('1')
    expect(p.draft.password_reset_seconds).toBe('3600')
    expect(p.progress.value?.canRetryOriginal).toBe(true)
    await p.retryOriginal()
    expect(p.state.confirmed?.version).toBe('2')
    expect(p.observation.value?.version).toBe('3')
    expect(p.editor.version).toBe('3')
    expect(p.draft.password_reset_seconds).toBe('7200')
    expect(f.writes()[1]![1].body).toBe(first.body)
    expect(f.writes()[1]![1].headers).toEqual(first.headers)
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/lookup'))).toHaveLength(0)
  })

  it('never reuses a confirmed historical result as a new baseline when the one post-save GET fails', async () => {
    const f = await controller(),
      p = f.page
    p.updateField('challenge_after_failures', '7')
    f.setSecurity(async (_, init) =>
      init.method === 'GET'
        ? problem('UNAVAILABLE')
        : json({ ...settings('2'), challenge_after_failures: '7' }),
    )
    const readsBefore = f.fetch.mock.calls.length
    await p.save()
    expect(f.fetch.mock.calls.slice(readsBefore).map(([, init]) => init.method)).toEqual([
      'PUT',
      'GET',
    ])
    expect(p.state.confirmed?.version).toBe('2')
    expect(p.observation.phase).toBe('error')
    expect(p.editor.ready).toBe(false)
    expect(p.state.writeMessage).toContain('保存已确认，当前配置读取失败')
    await p.save()
    await p.retryOriginal()
    expect(f.writes()).toHaveLength(1)
    f.setSecurity(async () => json({ ...settings('3'), challenge_after_failures: '9' }))
    await p.refresh()
    expect(p.editor.version).toBe('3')
    expect(p.draft.challenge_after_failures).toBe('9')
    expect(p.state.confirmed?.version).toBe('2')
    expect(f.writes()).toHaveLength(1)
  })

  it('retains same-identity drafts and pending confirmation across checking, but invalidates a new Session', async () => {
    const f = await controller(),
      p = f.page
    p.updateField('password_reset_seconds', '3600')
    const confirmation = p.confirmLeave(),
      session = barrier<Response>()
    f.setSessionRequest(() => session.promise)
    const restore = f.auth.restore()
    await flushPromises()
    p.detach()
    expect(p.confirmation.open).toBe(true)
    expect(p.draft.password_reset_seconds).toBe('3600')
    session.resolve(json(view()))
    await restore
    p.attach()
    await flushPromises()
    expect(p.confirmation.open).toBe(true)
    p.finishConfirmation(false)
    expect(await confirmation).toBe(false)
    expect(p.draft.password_reset_seconds).toBe('3600')
    const pending = p.confirmLeave()
    f.setSessionRequest(async () => json(view(id(3))))
    await f.auth.restore()
    await flushPromises()
    expect(await pending).toBe(false)
    expect(p.confirmation.open).toBe(false)
    expect(p.dirty.value).toBe(false)
    expect(f.writes()).toHaveLength(0)
  })

  it('abandonment waits for the actual tail and requires a new explicit successful GET before editing', async () => {
    const f = await controller(),
      p = f.page,
      held = barrier<Response>()
    p.updateField('password_reset_seconds', '3600')
    f.setSecurity(() => held.promise)
    const saving = p.save()
    await flushPromises()
    const abandoning = p.abandonOperation()
    p.finishConfirmation(true)
    await abandoning
    await saving
    expect(p.state.requiresRead).toBe(true)
    expect(p.editor.ready).toBe(false)
    expect(f.auth.state.busy).toBe(true)
    const count = f.fetch.mock.calls.length
    await p.refresh()
    p.detach()
    p.attach()
    held.resolve(json({ ...settings('2'), password_reset_seconds: '3600' }))
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.fetch.mock.calls).toHaveLength(count)
    f.setSecurity(async () => json(settings('2')))
    await p.refresh()
    expect(p.editor.version).toBe('2')
    expect(p.dirty.value).toBe(false)
    expect(f.writes()).toHaveLength(1)
  })

  it('never creates defaults from failed/null observations and treats maximum version as read-only', async () => {
    const f = await fixture(),
      p = createSystemAccountSecurity(f.auth)
    pages.push(p)
    f.setSecurity(async () => json(null))
    p.attach()
    await flushPromises()
    expect(p.editor.ready).toBe(false)
    expect(p.draft).toEqual(emptyValues())
    await p.save()
    expect(f.writes()).toHaveLength(0)
    f.setSecurity(async () => json(settings('9223372036854775807')))
    await p.refresh()
    expect(p.maximumVersion.value).toBe(true)
    p.updateField('password_reset_seconds', '3600')
    expect(p.canSave.value).toBe(false)
    await p.save()
    expect(f.writes()).toHaveLength(0)
  })
})

function emptyValues() {
  return {
    session_idle_seconds: '',
    session_absolute_seconds: '',
    password_reset_seconds: '',
    challenge_after_failures: '',
  }
}

describe('Account security on the seventh independent Cookie domain', () => {
  it('requires a real singleton observation and captures validated immutable inputs before key allocation', async () => {
    const f = await fixture(),
      api = f.auth.system.accountSecurity,
      key = vi.spyOn(crypto, 'randomUUID')
    await expect(api.start(input())).rejects.toMatchObject({ kind: 'invalid-input' })
    await api.get()
    for (const invalid of [
      { ...input(), extra: null },
      { ...input(), version: '9223372036854775807' },
      { ...input(), password_reset_seconds: ' 300' },
    ])
      await expect(api.start(invalid as AccountSecurityUpdateInput)).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    expect(key).not.toHaveBeenCalled()
    const held = barrier<Response>(),
      original = input()
    f.setSecurity(() => held.promise)
    const task = api.start(original)
    await flushPromises()
    ;(original as { password_reset_seconds: string }).password_reset_seconds = '7200'
    await expect(api.start(input())).rejects.toMatchObject({ kind: 'busy' })
    expect(key).toHaveBeenCalledTimes(1)
    expect(f.writes()).toHaveLength(1)
    expect(f.writes()[0]![1].body).toBe(JSON.stringify(input()))
    const publicState = JSON.stringify([f.auth.state, api.progress])
    expect(publicState).not.toContain('S'.repeat(43))
    expect(publicState).not.toContain(key.mock.results[0]!.value)
    expect(publicState).not.toContain('"body"')
    held.resolve(json(settings('2')))
    expect(await task).toEqual(settings('2'))
    expect(api.progress).toMatchObject({
      phase: 'confirmed',
      settings: settings('2'),
      canRetryOriginal: false,
    })
  })

  it('pins the singleton across same-identity reads and abandonment and rejects a foreign successful write', async () => {
    const f = await fixture(),
      api = f.auth.system.accountSecurity
    await api.get()
    api.abandon()
    f.setSecurity(async (_, init) =>
      json({ ...settings(init.method === 'PUT' ? '2' : '3'), id: id(99) }),
    )
    await expect(api.get()).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(api.start(input())).rejects.toMatchObject({ kind: 'invalid-response' })
    expect(api.progress?.phase).toBe('uncertain')
    f.setSession(view(id(3)))
    await f.auth.restore()
    expect(api.progress).toBeNull()
    expect((await api.get()).id).toBe(id(99))
  })

  it('checks Session plus current GET without lookup, then replays identical bytes/key/CSRF after a higher observation', async () => {
    const f = await fixture(),
      api = f.auth.system.accountSecurity
    await api.get()
    f.setSecurity(async () => {
      throw new Error('response lost')
    })
    await expect(api.start(input())).rejects.toMatchObject({ kind: 'transport' })
    const first = f.writes()[0]![1]
    expect(api.progress?.canRetryOriginal).toBe(false)
    const before = f.fetch.mock.calls.length
    f.setSecurity(async (_, init) =>
      init.method === 'GET' ? json(settings('3')) : json(settings('2')),
    )
    expect(await api.checkOriginal()).toEqual(settings('3'))
    expect(f.fetch.mock.calls.slice(before).map(([path, init]) => [path, init.method])).toEqual([
      ['/api/v1/session', 'GET'],
      ['/api/v1/system/account-settings', 'GET'],
    ])
    expect(api.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'current',
      settings: null,
      canRetryOriginal: true,
    })
    expect(await api.retryOriginal()).toEqual(settings('2'))
    const second = f.writes()[1]![1]
    expect([second.body, second.headers]).toEqual([first.body, first.headers])
    expect(api.progress).toMatchObject({
      phase: 'confirmed',
      settings: settings('2'),
      canRetryOriginal: false,
    })
    expect(f.fetch.mock.calls.some(([path]) => path.endsWith('/lookup'))).toBe(false)
    await expect(api.retryOriginal()).rejects.toMatchObject({ kind: 'invalid-input' })
  })

  it.each([
    ['VERSION_CONFLICT', 409, 'not_started', 'rejected'],
    ['INVALID_ARGUMENT', 400, 'not_committed', 'rejected'],
    ['UNAVAILABLE', 503, 'not_started', 'uncertain'],
    ['RESOURCE_BUSY', 409, 'not_started', 'uncertain'],
    ['COMMIT_UNKNOWN', 503, 'unknown', 'uncertain'],
    ['VERSION_CONFLICT', 409, 'committed', 'uncertain'],
    ['IDEMPOTENCY_KEY_REUSED', 409, 'not_started', 'uncertain'],
  ] as const)(
    'classifies %s/%i/%s without turning a later rejection into proof of no commit',
    async (code, status, commit, phase) => {
      const f = await fixture(),
        api = f.auth.system.accountSecurity
      await api.get()
      f.setSecurity(async () => problem(code, status, commit))
      await api.start(input()).catch(() => undefined)
      expect(api.progress?.phase).toBe(phase)
      if (phase === 'rejected') {
        api.abandon()
        f.setSecurity(async () => {
          throw new Error('lost')
        })
        await api.start(input()).catch(() => undefined)
        f.setSecurity(async (_, init) =>
          init.method === 'GET' ? json(settings('3')) : problem(code, status, commit),
        )
        await api.checkOriginal()
        await api.retryOriginal().catch(() => undefined)
        expect(api.progress?.phase).toBe('uncertain')
        expect(f.writes()[1]![1].body).toBe(f.writes()[2]![1].body)
        expect(f.writes()[1]![1].headers).toEqual(f.writes()[2]![1].headers)
      } else if (code === 'IDEMPOTENCY_KEY_REUSED') {
        f.setSecurity(async () => json(settings('3')))
        await api.checkOriginal()
        expect(api.progress?.canRetryOriginal).toBe(false)
      }
    },
  )

  it('preserves the original through failed Session/current reads and enables replay only after current Session', async () => {
    const f = await fixture(),
      api = f.auth.system.accountSecurity
    await api.get()
    f.setSecurity(async () => problem('UNAVAILABLE'))
    await api.start(input()).catch(() => undefined)
    f.setSessionRequest(async () => problem('UNAVAILABLE'))
    const before = f.fetch.mock.calls.length
    await expect(api.checkOriginal()).rejects.toMatchObject({ kind: 'cancelled' })
    expect(f.fetch.mock.calls.slice(before).map(([path]) => path)).toEqual(['/api/v1/session'])
    expect(api.progress).toMatchObject({ phase: 'uncertain', canRetryOriginal: false })
    f.setSessionRequest(async () => json(view()))
    await expect(api.checkOriginal()).rejects.toMatchObject({ kind: 'problem' })
    expect(api.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'failed',
      canRetryOriginal: true,
    })
    f.setSecurity(async () => json(settings('2')))
    await api.retryOriginal()
    expect(f.writes()[0]![1].body).toBe(f.writes()[1]![1].body)
    expect(f.writes()[0]![1].headers).toEqual(f.writes()[1]![1].headers)
  })

  it.each(['read', 'write'] as const)(
    'holds the native %s EOF and cancel tail across the 30-second visible deadline',
    async (kind) => {
      const f = await fixture(),
        api = f.auth.system.accountSecurity
      await api.get()
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      const tail = barrier(),
        cancel = vi.fn(() => tail.promise)
      let source!: ReadableStreamDefaultController<Uint8Array>
      f.setSecurity(
        async () =>
          new Response(
            new ReadableStream<Uint8Array>({
              start(value) {
                source = value
              },
              cancel,
            }),
            { headers: { 'Content-Type': 'application/json' } },
          ),
      )
      const task = (kind === 'read' ? api.get() : api.start(input())).catch((error) => error)
      await flushPromises()
      source.enqueue(
        new TextEncoder().encode(JSON.stringify(settings(kind === 'write' ? '2' : '1'))),
      )
      await flushPromises()
      expect(f.auth.state.busy).toBe(true)
      await vi.advanceTimersByTimeAsync(30000)
      expect(await task).toMatchObject({ kind: 'cancelled' })
      expect(cancel).toHaveBeenCalledTimes(1)
      const count = f.fetch.mock.calls.length
      await f.auth.restore()
      await f.auth.logout()
      await expect(f.auth.personal.getProfile()).rejects.toMatchObject({ kind: 'busy' })
      await expect(f.auth.system.selection.get()).rejects.toMatchObject({ kind: 'busy' })
      expect(f.fetch.mock.calls).toHaveLength(count)
      api.abandon()
      expect(f.auth.state.busy).toBe(true)
      tail.resolve()
      await flushPromises()
      expect(f.auth.state.busy).toBe(false)
      expect(api.progress).toBeNull()
    },
  )

  it('retains an abort-ignoring fetch owner after visible abandonment and leaves the old identity unable to publish', async () => {
    const f = await fixture(),
      api = f.auth.system.accountSecurity,
      fetch = barrier<Response>()
    f.setSecurity(() => fetch.promise)
    const task = api.get().catch((error) => error)
    await flushPromises()
    f.auth.leave()
    expect(await task).toMatchObject({ kind: 'cancelled' })
    const count = f.fetch.mock.calls.length
    f.setSession(view(id(3)))
    await f.auth.restore()
    expect(f.fetch.mock.calls).toHaveLength(count)
    fetch.resolve(json(settings()))
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.personalContext.identity).toBeNull()
    await f.auth.restore()
    await expect(api.start(input())).rejects.toMatchObject({ kind: 'invalid-input' })
  })

  it.each(['personal', 'users', 'invitations', 'providers', 'models', 'selection'] as const)(
    'isolates %s abandonment in both directions without freeing actual ownership',
    async (domain) => {
      const f = await fixture(),
        api = f.auth.system.accountSecurity
      const other = () =>
        ({
          personal: () => f.auth.personal.getProfile(),
          users: () => f.auth.system.listUsers({}),
          invitations: () => f.auth.system.listInvitations({}),
          providers: () => f.auth.system.providers.list({}),
          models: () => f.auth.system.models.listProviders({}),
          selection: () => f.auth.system.selection.get(),
        })[domain]()
      const abandon = () =>
        ({
          personal: () => f.auth.personal.abandon(),
          users: () => f.auth.system.abandon(),
          invitations: () => f.auth.system.abandonInvitations(),
          providers: () => f.auth.system.providers.abandon(),
          models: () => f.auth.system.models.abandon(),
          selection: () => f.auth.system.selection.abandon(),
        })[domain]()
      const held = barrier<Response>()
      f.setSecurity(() => held.promise)
      const securityRead = api.get()
      await flushPromises()
      abandon()
      expect(f.fetch.mock.calls.at(-1)![1].signal?.aborted).toBe(false)
      await expect(other()).rejects.toMatchObject({ kind: 'busy' })
      held.resolve(json(settings()))
      await securityRead
      const otherTail = barrier<Response>()
      f.setOther(() => otherTail.promise)
      const otherRead = other().catch((error) => error)
      await flushPromises()
      api.abandon()
      expect(f.fetch.mock.calls.at(-1)![1].signal?.aborted).toBe(false)
      await expect(api.get()).rejects.toMatchObject({ kind: 'busy' })
      otherTail.resolve(
        domain === 'personal'
          ? json({ user: view().user, avatar: null })
          : domain === 'selection'
            ? json({ id: id(20), version: '1', configured: null })
            : json({ items: [], next_cursor: null }),
      )
      await otherRead
      expect(f.auth.state.busy).toBe(false)
    },
  )

  it.each(['CSRF_FAILED', 'SESSION_REVOKED', 'FORBIDDEN'] as const)(
    'handles current %s without changing the public role',
    async (code) => {
      const f = await fixture(),
        api = f.auth.system.accountSecurity
      await api.get()
      f.setSecurity(async () => problem(code, code === 'SESSION_REVOKED' ? 401 : 403))
      await api.start(input()).catch(() => undefined)
      expect(api.progress).toBeNull()
      if (code === 'FORBIDDEN') {
        expect(f.auth.system.denied).toBe(true)
        expect(f.auth.state.user?.role).toBe('admin')
        const count = f.fetch.mock.calls.length
        await expect(f.auth.system.selection.get()).rejects.toMatchObject({ kind: 'invalid-input' })
        expect(f.fetch.mock.calls).toHaveLength(count)
        await f.auth.restore()
        expect(f.auth.system.denied).toBe(false)
      } else expect(f.auth.personalContext.identity).toBeNull()
    },
  )

  it.each([401, 403] as const)(
    'handles a parsed late %i after actual finalizer without stale authority pollution',
    async (status) => {
      const f = await fixture(),
        api = f.auth.system.accountSecurity,
        tail = barrier(),
        entered = barrier()
      const response = problem(status === 401 ? 'SESSION_REVOKED' : 'FORBIDDEN', status)
      const cancel = response.body!.cancel.bind(response.body)
      response.body!.cancel = async () => {
        entered.resolve()
        await tail.promise
        return cancel()
      }
      f.setSecurity(async () => response)
      const task = api.get().catch((error) => error)
      await entered.promise
      api.abandonRead()
      expect(await task).toMatchObject({ kind: 'cancelled' })
      expect(f.auth.state.busy).toBe(true)
      tail.resolve()
      await flushPromises()
      expect(f.auth.system.denied).toBe(false)
      expect(f.auth.personalContext.identity === null).toBe(status === 401)
    },
  )

  it('never hands an original to the same user in a new Session, a new CSRF or an ordinary role', async () => {
    for (const next of [
      view(id(3)),
      { ...view(), csrf_token: 'T'.repeat(43) },
      view(id(2), 'user'),
    ]) {
      const f = await fixture(),
        api = f.auth.system.accountSecurity
      await api.get()
      f.setSecurity(async () => problem('UNAVAILABLE'))
      await api.start(input()).catch(() => undefined)
      f.setSession(next)
      await expect(api.checkOriginal()).rejects.toMatchObject({ kind: 'cancelled' })
      expect(api.progress).toBeNull()
      expect(f.writes()).toHaveLength(1)
      await expect(api.retryOriginal()).rejects.toMatchObject({ kind: 'invalid-input' })
      if (next.user.role === 'user') {
        const count = f.fetch.mock.calls.length
        await expect(api.get()).rejects.toMatchObject({ kind: 'invalid-input' })
        await expect(api.start(input())).rejects.toMatchObject({ kind: 'invalid-input' })
        expect(f.fetch.mock.calls).toHaveLength(count)
      }
    }
  })

  it('does not clear a synchronous confirmed consumer’s new intent', async () => {
    const f = await fixture(),
      api = f.auth.system.accountSecurity,
      next = barrier<Response>()
    await api.get()
    let successor: Promise<unknown> | undefined
    const stop = watch(
      () => api.progress?.phase,
      (phase) => {
        if (phase !== 'confirmed') return
        api.abandon()
        f.setSecurity(() => next.promise)
        successor = api.start({ ...input(), version: '2' }).catch((error) => error)
      },
      { flush: 'sync' },
    )
    await api.start(input()).catch(() => undefined)
    await flushPromises()
    expect(f.writes()).toHaveLength(2)
    expect(api.progress?.phase).toBe('submitting')
    stop()
    next.resolve(json(settings('3')))
    await successor
    expect(api.progress?.settings?.version).toBe('3')
  })
})
