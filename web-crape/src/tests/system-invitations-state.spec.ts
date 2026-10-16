import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { createSystemAccountAPI } from '../api/system-account'
import { createSystemInvitationAPI, type Invitation } from '../api/system-invitations'
import type { Fetch, CommitState } from '../api/client'
import { createSessionController, type SessionController } from '../composables/useSession'
import {
  createSystemInvitations,
  invitationRetryReason,
  type SystemInvitationsController,
} from '../composables/useSystemInvitations'
import { useTheme } from '../composables/useTheme'
const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const time = '2026-10-06T12:34:56.123456Z'
const view = (uid = id(1), sid = id(2), role: 'admin' | 'user' = 'admin'): SessionView => ({
  user: {
    id: uid,
    email: 'admin@example.com',
    username: 'admin',
    display_name: 'Admin',
    role,
    theme: 'dark',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: sid, issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const rows = (start = 100, count = 25): Invitation[] =>
  Array.from({ length: count }, (_, n) => ({
    id: id(start - n),
    email: `invitation${start - n}@example.com`,
    version: '2',
    created_at: time,
    expires_at: time,
    latest_delivery: {
      job_id: id(300 + start - n),
      accepted_at: time,
      phase: 'sent',
      attempts: '1',
      version: '7',
      channel: 'backend_log',
      attempt_result: 'sent',
    },
  }))
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
      detail: '<private detail>',
      instance: '/api/v1/system/invitations',
      request_id: id(9),
      commit_state,
    },
    status,
  )
function barrier<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
const owners: SessionController[] = [],
  pages: SystemInvitationsController[] = []
async function fixture() {
  let current = view(),
    session: Fetch = async () => json(current),
    list: Fetch = async () => json({ items: rows(), next_cursor: 'page-b' })
  let mutation: Fetch = async (path) =>
    path.endsWith('/revoke')
      ? new Response(null, { status: 204 })
      : path.endsWith('/retry')
        ? json({ job_id: id(500), version: '9' }, 202)
        : json({ id: id(100), job_id: id(500), version: '3' }, path.endsWith('/resend') ? 202 : 201)
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return session(path, init)
    if (path.startsWith('/api/v1/system/invitations?')) return list(path, init)
    if (path.startsWith('/api/v1/system/users?')) return json({ items: [] })
    if (path.endsWith('/bootstrap'))
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path === '/api/v1/me') return json({ user: current.user, avatar: null })
    if (path.endsWith('/logout')) return new Response(null, { status: 204 })
    return mutation(path, init)
  })
  const auth = createSessionController(
    createAccountAPI(fetch),
    createSystemAccountAPI(fetch),
    createSystemInvitationAPI(fetch),
  )
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetch,
    setList(value: Fetch) {
      list = value
    },
    setMutation(value: Fetch) {
      mutation = value
    },
    setSession(value: SessionView) {
      current = value
    },
    setSessionRequest(value: Fetch) {
      session = value
    },
    page() {
      const page = createSystemInvitations(auth)
      pages.push(page)
      page.attach()
      return page
    },
    posts() {
      return fetch.mock.calls.filter(
        ([p, init]) => p.startsWith('/api/v1/system/') && init.method === 'POST',
      )
    },
    cursors() {
      return fetch.mock.calls
        .filter(([p]) => p.startsWith('/api/v1/system/invitations?'))
        .map(([p]) => new URL(p, 'http://localhost').searchParams.get('cursor'))
    },
  }
}
afterEach(() => {
  for (const p of pages.splice(0)) p.dispose()
  for (const o of owners.splice(0)) o.leave()
  vi.useRealTimers()
  vi.restoreAllMocks()
  useTheme().setTheme('system')
})

describe('one Cookie owner for invitation reads and commands', () => {
  it.each(['read-fetch', 'write-fetch', 'read-body', 'write-body', 'write-cancel'] as const)(
    'retains the actual %s tail beyond visible 30 seconds and every other Cookie operation',
    async (kind) => {
      vi.useFakeTimers()
      const f = await fixture(),
        fetchTail = barrier<Response>(),
        readTail = barrier<{ done: boolean; value?: Uint8Array }>(),
        cancelTail = barrier<void>()
      const response = json({ items: [] }, kind.startsWith('write') ? 201 : 200)
      const reader = {
        read: vi.fn(() => readTail.promise),
        cancel: vi.fn(() => (kind === 'write-cancel' ? cancelTail.promise : Promise.resolve())),
        releaseLock: vi.fn(),
      }
      if (!kind.endsWith('fetch'))
        Object.defineProperty(response, 'body', {
          value: { getReader: () => reader, cancel: async () => undefined },
        })
      if (kind === 'write-cancel') readTail.resolve({ done: true })
      const fn = kind.endsWith('fetch') ? () => fetchTail.promise : async () => response
      if (kind.startsWith('read')) f.setList(fn)
      else f.setMutation(fn)
      const request = (
        kind.startsWith('read')
          ? f.auth.system.listInvitations({})
          : f.auth.system.createInvitation({ email: 'a@b' })
      ).catch((e: unknown) => e)
      await flushPromises()
      await vi.advanceTimersByTimeAsync(30_000)
      expect(await request).toMatchObject({ kind: 'cancelled' })
      expect(f.auth.state.busy).toBe(true)
      const before = f.fetch.mock.calls.length
      const keys = vi.spyOn(crypto, 'randomUUID')
      await f.auth.login('admin@example.com', 'Password 123456')
      await f.auth.logout()
      await f.auth.restore()
      await expect(f.auth.system.listUsers({})).rejects.toMatchObject({ kind: 'busy' })
      await expect(f.auth.system.listInvitations({})).rejects.toMatchObject({ kind: 'busy' })
      await expect(f.auth.system.createInvitation({ email: 'other@b' })).rejects.toMatchObject({
        kind: 'busy',
      })
      await expect(
        f.auth.personal.changePassword({
          version: '1',
          current_password: 'Password 123456',
          new_password: 'Changed password 123456',
          confirmation: 'Changed password 123456',
        }),
      ).rejects.toMatchObject({ kind: 'busy' })
      await expect(f.auth.entry.requestPasswordReset({ email: 'other@b' })).rejects.toMatchObject({
        kind: 'busy',
      })
      expect(keys).not.toHaveBeenCalled()
      expect(f.fetch.mock.calls.length).toBe(before)
      f.auth.system.abandonInvitations()
      expect(f.auth.state.busy).toBe(true)
      fetchTail.resolve(json({ items: [] }))
      readTail.resolve({ done: true })
      cancelTail.resolve()
      await flushPromises()
      expect(f.auth.state.busy).toBe(false)
      await f.auth.system.listUsers({})
    },
  )
  it.each(['restore', 'personal', 'entry'] as const)(
    'invitation cleanup cannot abandon a held %s owner',
    async (kind) => {
      const f = await fixture(),
        held = barrier<Response>()
      let signal: AbortSignal | undefined
      const hold: Fetch = (_p, init) => {
        signal = init.signal!
        return held.promise
      }
      if (kind === 'restore') f.setSessionRequest(hold)
      else f.setMutation(hold)
      if (kind === 'entry') await f.auth.entry.prepare()
      const pending =
        kind === 'restore'
          ? f.auth.restore()
          : kind === 'personal'
            ? f.auth.personal.setPreferences({ theme: 'light', version: '1' })
            : f.auth.entry.requestPasswordReset({ email: 'other@b' })
      const result = Promise.resolve(pending).catch((e: unknown) => e)
      await flushPromises()
      f.auth.system.abandonInvitationRead()
      f.auth.system.abandonInvitations()
      expect(signal?.aborted).toBe(false)
      expect(f.auth.state.busy).toBe(true)
      await expect(f.auth.system.listInvitations({})).rejects.toMatchObject({ kind: 'busy' })
      held.resolve(
        kind === 'restore'
          ? json(view())
          : kind === 'personal'
            ? json({ theme: 'light', version: '2' })
            : json({ accepted: true, delivery_channel: 'backend_log' }, 202),
      )
      await result
      expect(f.auth.state.busy).toBe(false)
    },
  )
  it('users and invitation read abandonment do not cancel an invitation command or its private original', async () => {
    const f = await fixture(),
      held = barrier<Response>()
    let signal: AbortSignal | undefined
    f.setMutation((_p, init) => {
      signal = init.signal!
      return held.promise
    })
    const pending = f.auth.system.createInvitation({ email: 'Original@Example.com' })
    await flushPromises()
    f.auth.system.abandon()
    f.auth.system.abandonInvitationRead()
    expect(signal?.aborted).toBe(false)
    expect(f.auth.system.invitationProgress?.phase).toBe('submitting')
    held.resolve(json({ id: id(100), job_id: id(500), version: '1' }, 201))
    expect(await pending).toMatchObject({ kind: 'create' })
  })
  it('validates before allocating a key and freezes the input before dispatch', async () => {
    const f = await fixture(),
      keys = vi.spyOn(crypto, 'randomUUID')
    await expect(f.auth.system.createInvitation({ email: '' })).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(keys).not.toHaveBeenCalled()
    const input = { email: 'Original@Example.com' }
    const pending = f.auth.system.createInvitation(input)
    input.email = 'replaced@example.com'
    await pending
    expect(f.posts()[0]![1].body).toBe('{"email":"Original@Example.com"}')
    expect(Object.keys(f.auth.system.invitationProgress!).sort()).toEqual([
      'canRetryOriginal',
      'contextValid',
      'kind',
      'phase',
    ])
    for (const value of [
      'Original@Example.com',
      'S'.repeat(43),
      String((f.posts()[0]![1].headers as Record<string, string>)['Idempotency-Key']),
    ])
      expect(JSON.stringify(f.auth.system.invitationProgress)).not.toContain(value)
  })
  it('keeps retry post-read 5xx/not_started uncertain and replays identical original material after Session checking', async () => {
    const f = await fixture()
    f.setMutation(async () => problem('DEPENDENCY_UNAVAILABLE', 503, 'not_started'))
    await expect(
      f.auth.system.retryDelivery({ job_id: id(400), version: '7' }),
    ).rejects.toMatchObject({ kind: 'problem' })
    expect(f.auth.system.invitationProgress).toMatchObject({
      kind: 'retry',
      phase: 'uncertain',
      canRetryOriginal: false,
    })
    await expect(f.auth.system.createInvitation({ email: 'new@b' })).rejects.toMatchObject({
      kind: 'busy',
    })
    await f.auth.restore()
    await f.auth.system.listInvitations({})
    expect(f.auth.system.invitationProgress).toMatchObject({
      phase: 'uncertain',
      canRetryOriginal: true,
    })
    f.setMutation(async () => json({ job_id: id(501), version: '9' }, 202))
    expect(await f.auth.system.retryInvitationOriginal()).toMatchObject({
      kind: 'retry',
      value: { job_id: id(501), version: '9' },
    })
    const [first, second] = f.posts()
    expect(second![0]).toBe(first![0])
    expect(second![1].body).toBe(first![1].body)
    expect(second![1].headers).toEqual(first![1].headers)
  })
  it.each([
    ['VERSION_CONFLICT', 409],
    ['RESOURCE_DELETED', 410],
    ['RATE_LIMITED', 429],
    ['INVALID_ARGUMENT', 400],
  ] as const)(
    'distinguishes known %s from a refusal after prior uncertainty',
    async (code, status) => {
      const f = await fixture()
      f.setMutation(async () => problem(code, status, 'not_committed'))
      await expect(f.auth.system.createInvitation({ email: 'a@b' })).rejects.toMatchObject({
        kind: 'problem',
      })
      expect(f.auth.system.invitationProgress).toBeNull()
      f.setMutation(async () => {
        throw new Error('private response lost')
      })
      await expect(f.auth.system.createInvitation({ email: 'a@b' })).rejects.toMatchObject({
        kind: 'transport',
      })
      await f.auth.restore()
      f.setMutation(async () => problem(code, status, 'not_started'))
      await expect(f.auth.system.retryInvitationOriginal()).rejects.toMatchObject({
        kind: 'problem',
      })
      expect(f.auth.system.invitationProgress?.phase).toBe('uncertain')
      expect(f.posts()[2]![1].headers).toEqual(f.posts()[1]![1].headers)
    },
  )
  it.each([
    ['SESSION_REVOKED', 401],
    ['CSRF_FAILED', 403],
    ['FORBIDDEN', 403],
  ] as const)(
    'clears current invitation materials on %s without inventing a new role',
    async (code, status) => {
      const f = await fixture(),
        page = f.page()
      await flushPromises()
      page.draft.email = 'draft@b'
      f.setMutation(async () => problem(code, status))
      await page.create()
      await flushPromises()
      expect(page.draft.email).toBe('')
      expect(page.state.rows).toHaveLength(0)
      expect(f.auth.system.invitationProgress).toBeNull()
      if (code === 'FORBIDDEN') {
        expect(f.auth.system.denied).toBe(true)
        expect(f.auth.state.user?.role).toBe('admin')
        await f.auth.restore()
        await flushPromises()
        expect(f.auth.system.denied).toBe(false)
      } else expect(f.auth.state.user).toBeNull()
    },
  )
  it('honors a same-identity late 401 after visible expiration but ignores a stale 403 after abandonment', async () => {
    vi.useFakeTimers()
    for (const code of ['SESSION_REVOKED', 'FORBIDDEN']) {
      const f = await fixture(),
        tail = barrier<void>()
      const response = problem(code, code === 'SESSION_REVOKED' ? 401 : 403)
      const reader = response.body!.getReader()
      const outerCancel = vi.fn(() => tail.promise)
      Object.defineProperty(response, 'body', {
        value: { getReader: () => reader, cancel: outerCancel },
      })
      f.setList(async () => response)
      const pending = f.auth.system.listInvitations({}).catch((e: unknown) => e)
      await flushPromises()
      expect(outerCancel).toHaveBeenCalledOnce()
      if (code === 'FORBIDDEN') f.auth.system.abandonInvitationRead()
      await vi.advanceTimersByTimeAsync(30_000)
      await pending
      expect(f.auth.state.busy).toBe(true)
      tail.resolve()
      await flushPromises()
      expect(f.auth.state.busy).toBe(false)
      if (code === 'SESSION_REVOKED') expect(f.auth.state.user).toBeNull()
      else {
        expect(f.auth.system.denied).toBe(false)
        expect(f.auth.state.user?.role).toBe('admin')
      }
    }
  })
})

describe('App-owned invitation page state', () => {
  it('preserves draft across same Session checking detach and destroys it on new Session, account, CSRF or downgrade', async () => {
    for (const next of [
      view(id(1), id(20)),
      view(id(30), id(31)),
      { ...view(), csrf_token: 'T'.repeat(43) },
      view(id(1), id(2), 'user'),
    ]) {
      const f = await fixture(),
        page = f.page()
      await flushPromises()
      page.openCreate()
      page.draft.email = 'unsaved@b'
      const held = barrier<Response>()
      f.setSessionRequest(() => held.promise)
      const pending = f.auth.restore()
      await flushPromises()
      page.detach()
      expect(page.draft.email).toBe('unsaved@b')
      held.resolve(json(view()))
      await pending
      page.attach()
      await flushPromises()
      expect(page.draft.email).toBe('unsaved@b')
      expect(page.dialogs.create).toBe(true)
      f.setSessionRequest(async () => json(next))
      await f.auth.restore()
      await flushPromises()
      expect(page.draft.email).toBe('')
      expect(page.dialogs.create).toBe(false)
      page.dispose()
    }
  })
  it('captures invitation and job versions separately, retains confirmed success when the next list fails', async () => {
    const f = await fixture(),
      page = f.page()
    await flushPromises()
    const row = page.state.rows[0]!
    page.openRetry(row)
    f.setList(async () => problem('DEPENDENCY_UNAVAILABLE'))
    await page.retryDelivery()
    expect(f.posts()[0]![0]).toBe(`/api/v1/system/mail-jobs/${row.latest_delivery.job_id}/retry`)
    expect(f.posts()[0]![1].body).toBe('{"version":"7"}')
    expect(page.state.writeMessage).toBe('重试投递请求已接受')
    expect(page.state.phase).toBe('error')
    expect(page.progress.value?.phase).toBe('confirmed')
    expect(page.dirty.value).toBe(false)
    f.setList(async () => json({ items: [row] }))
    await page.retryRead()
    expect(f.posts()).toHaveLength(1)
    await page.resend(row)
    expect(f.posts()[1]![1].body).toBe('{"version":"2"}')
  })
  it('observes at most one page without treating a missing row as proof and retries the original absent target', async () => {
    const f = await fixture(),
      page = f.page()
    await flushPromises()
    f.setMutation(async () => {
      throw new Error('lost')
    })
    await page.resend(page.state.rows[0]!)
    f.setList(async () => json({ items: [] }))
    const before = f.cursors().length
    await page.checkCurrent()
    expect(f.cursors().length).toBe(before + 1)
    expect(page.progress.value?.phase).toBe('uncertain')
    expect(page.progress.value?.canRetryOriginal).toBe(true)
    f.setMutation(async () => json({ id: id(100), job_id: id(500), version: '8' }, 202))
    await page.retryOriginal()
    expect(page.progress.value?.phase).toBe('confirmed')
    expect(f.posts()[1]![1].headers).toEqual(f.posts()[0]![1].headers)
  })
  it('requires explicit dirty confirmation before clearing and a successful reload after abandoning an uncertain write', async () => {
    const f = await fixture(),
      page = f.page()
    await flushPromises()
    page.openCreate()
    page.draft.email = 'dirty@b'
    const first = page.closeDialog()
    expect(page.confirmation.open).toBe(true)
    page.finishConfirmation(false)
    await first
    expect(page.draft.email).toBe('dirty@b')
    f.setMutation(async () => {
      throw new Error('lost')
    })
    await page.create()
    const abandon = page.abandonOperation()
    expect(page.confirmation.message).toContain('此前提交仍可能生效')
    page.finishConfirmation(true)
    await abandon
    expect(page.draft.email).toBe('')
    expect(page.state.requiresReload).toBe(true)
    page.draft.email = 'new@b'
    await page.create()
    expect(f.posts()).toHaveLength(1)
    await page.refresh()
    expect(page.state.requiresReload).toBe(false)
  })
  it('keeps cursor history page-local and advances it only after successful fresh reads', async () => {
    const f = await fixture(),
      page = f.page()
    await flushPromises()
    f.setList(async () => problem('DEPENDENCY_UNAVAILABLE'))
    await page.next()
    expect(page.state.rows).toHaveLength(0)
    f.setList(async () => json({ items: rows(70), next_cursor: 'page-c' }))
    await page.retryRead()
    await page.previous()
    expect(f.cursors()).toEqual([null, 'page-b', 'page-b', null])
    f.setList(async () => problem('CURSOR_INVALID', 400))
    await page.next()
    const before = f.cursors().length
    await page.retryRead()
    expect(f.cursors()).toHaveLength(before)
    f.setList(async () => json({ items: [] }))
    await page.refresh()
    expect(f.cursors().at(-1)).toBeNull()
    expect(page.state.phase).toBe('empty')
    page.afterNavigation('/system/users', '/system/invitations')
    expect(page.state.rows).toHaveLength(0)
  })
  it('waits once for an initial occupied owner but never loops after a list error', async () => {
    const f = await fixture(),
      held = barrier<Response>()
    f.setSessionRequest(() => held.promise)
    const pending = f.auth.restore(),
      page = f.page()
    expect(f.cursors()).toHaveLength(0)
    f.setList(async () => problem('DEPENDENCY_UNAVAILABLE'))
    held.resolve(json(view()))
    await pending
    await flushPromises()
    expect(f.cursors()).toHaveLength(1)
    await f.auth.system.listUsers({})
    await flushPromises()
    expect(f.cursors()).toHaveLength(1)
  })
  it('allows legal cancelled/null while explaining inactive and unknown/nonterminal retries', () => {
    const d = rows(1, 1)[0]!.latest_delivery
    expect(
      invitationRetryReason({
        ...d,
        phase: 'cancelled',
        channel: null,
        attempt_result: null,
        attempts: '0',
      }),
    ).toBe('')
    expect(
      invitationRetryReason({ ...d, phase: 'unknown', channel: null, attempt_result: null }),
    ).toContain('尚无')
    expect(invitationRetryReason({ ...d, phase: 'unknown', attempt_result: 'unknown' })).toBe('')
    expect(invitationRetryReason({ ...d, phase: 'sending', attempt_result: null })).toContain(
      '尚未结束',
    )
  })
})
