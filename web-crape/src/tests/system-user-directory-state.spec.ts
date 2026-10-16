import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { createSystemAccountAPI } from '../api/system-account'
import { type Fetch } from '../api/client'
import { createSessionController, type SessionController } from '../composables/useSession'
import { useSystemUserDirectory } from '../composables/useSystemUserDirectory'
import { createPersonalSettings } from '../composables/usePersonalSettings'
import { useTheme } from '../composables/useTheme'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const time = '2026-10-06T12:34:56.123456Z'
const view = (
  userID = id(1),
  sessionID = id(2),
  role: 'admin' | 'user' = 'admin',
): SessionView => ({
  user: {
    id: userID,
    email: 'admin@example.com',
    username: 'admin',
    display_name: 'Current admin',
    role,
    theme: 'dark',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: sessionID, issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const rows = (start = 100, count = 25) =>
  Array.from({ length: count }, (_, n) => ({
    ...view(id(start - n)).user,
    email: `directory${start - n}@example.com`,
    created_at: time,
  }))
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(3),
    },
  })
const problem = (code: string, status = 503) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Error',
      status,
      detail: '<b>private raw detail</b>',
      instance: '/api/v1/system/users',
      code,
      request_id: id(3),
      commit_state: 'not_started',
    },
    status,
  )
function barrier<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((next) => {
    resolve = next
  })
  return { promise, resolve }
}
const owners: SessionController[] = []
const directories: ReturnType<typeof useSystemUserDirectory>[] = []
async function fixture() {
  let current: SessionView | null = view()
  let session: Fetch = async () => (current ? json(current) : problem('UNAUTHENTICATED', 401))
  let list: Fetch = async () => json({ items: rows(), next_cursor: 'next-a' })
  let mutation: Fetch = async (path) => {
    if (path.endsWith('/change-password')) {
      current = { ...current!, session: { ...current!.session, id: id(4) } }
      return json({ completed: true, next_path: '/' })
    }
    if (path.endsWith('/login'))
      return json({ user: current!.user, session: current!.session, next_path: '/' })
    if (path.endsWith('/logout') || path.endsWith('/complete'))
      return new Response(null, { status: 204 })
    return json({ accepted: true, delivery_channel: 'backend_log' }, 202)
  }
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return session(path, init)
    if (path.startsWith('/api/v1/system/users?')) return list(path, init)
    if (path.endsWith('/bootstrap'))
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path === '/api/v1/me') return json({ user: current!.user, avatar: null })
    return mutation(path, init)
  })
  const auth = createSessionController(createAccountAPI(fetch), createSystemAccountAPI(fetch))
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetch,
    directory() {
      const directory = useSystemUserDirectory(auth)
      directories.push(directory)
      return directory
    },
    setList(next: Fetch) {
      list = next
    },
    setSession(next: SessionView | null) {
      current = next
    },
    setSessionRequest(next: Fetch) {
      session = next
    },
    setMutation(next: Fetch) {
      mutation = next
    },
    cursors() {
      return fetch.mock.calls
        .filter(([p]) => p.startsWith('/api/v1/system/users?'))
        .map(([p]) => new URL(p, 'http://localhost').searchParams.get('cursor'))
    },
  }
}
afterEach(() => {
  for (const directory of directories.splice(0)) directory.dispose()
  for (const owner of owners.splice(0)) owner.leave()
  vi.useRealTimers()
  vi.restoreAllMocks()
  useTheme().setTheme('system')
})

describe('system reads share the real Cookie owner', () => {
  it.each(['fetch', 'body-cancel'] as const)(
    'retains the %s tail past abort and 30 seconds',
    async (tail) => {
      vi.useFakeTimers()
      const f = await fixture()
      const fetchTail = barrier<Response>(),
        bodyTail = barrier<void>()
      let cancelling = false
      f.setList(
        tail === 'fetch'
          ? () => fetchTail.promise
          : async () =>
              new Response(
                new ReadableStream({
                  cancel() {
                    cancelling = true
                    return bodyTail.promise
                  },
                }),
                { headers: { 'Content-Type': 'text/html' } },
              ),
      )
      const keys = vi.spyOn(crypto, 'randomUUID')
      const request = f.auth.system.listUsers({}).catch((error: unknown) => error)
      await flushPromises()
      if (tail === 'body-cancel') expect(cancelling).toBe(true)
      await vi.advanceTimersByTimeAsync(30_000)
      expect(await request).toMatchObject({ kind: 'cancelled' })
      expect(f.auth.state.busy).toBe(true)
      expect(f.auth.state.phase).toBe('authenticated')
      expect(f.auth.state.canRetryOriginal).toBe(false)
      const before = f.fetch.mock.calls.length
      await f.auth.login('admin@example.com', 'Original password 123')
      await f.auth.logout()
      await f.auth.restore()
      await expect(f.auth.system.listUsers({})).rejects.toMatchObject({ kind: 'busy' })
      await expect(f.auth.personal.getProfile()).rejects.toMatchObject({ kind: 'busy' })
      await expect(
        f.auth.personal.changePassword({
          version: '1',
          current_password: 'Original password 123',
          new_password: 'Changed password 12345',
          confirmation: 'Changed password 12345',
        }),
      ).rejects.toMatchObject({ kind: 'busy' })
      await expect(
        f.auth.entry.requestPasswordReset({ email: 'other@example.com' }),
      ).rejects.toMatchObject({ kind: 'busy' })
      await expect(f.auth.entry.prepare()).rejects.toMatchObject({ kind: 'busy' })
      expect(keys).not.toHaveBeenCalled()
      expect(f.fetch.mock.calls.length).toBe(before)
      f.auth.system.abandon()
      if (tail === 'fetch') fetchTail.resolve(json({ items: rows(10, 1) }))
      else bodyTail.resolve()
      await flushPromises()
      expect(f.auth.state.busy).toBe(false)
      await f.auth.personal.getProfile()
      await f.auth.logout()
      expect(f.fetch.mock.calls.some(([path]) => path.endsWith('/logout'))).toBe(true)
    },
  )
  it('holds a successful JSON reader cancellation until its real completion', async () => {
    const f = await fixture(),
      tail = barrier<void>()
    const reader = {
      read: vi
        .fn()
        .mockResolvedValueOnce({
          done: false,
          value: new TextEncoder().encode(JSON.stringify({ items: [] })),
        })
        .mockResolvedValueOnce({ done: true }),
      cancel: vi.fn(() => tail.promise),
      releaseLock: vi.fn(),
    }
    const response = json({ items: [] })
    Object.defineProperty(response, 'body', {
      value: { getReader: () => reader, cancel: async () => undefined },
    })
    f.setList(async () => response)
    const pending = f.auth.system.listUsers({})
    await flushPromises()
    expect(reader.cancel).toHaveBeenCalledTimes(1)
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.personal.getProfile()).rejects.toMatchObject({ kind: 'busy' })
    tail.resolve()
    expect(await pending).toEqual({ items: [] })
    expect(reader.releaseLock).toHaveBeenCalledTimes(1)
    expect(f.auth.state.busy).toBe(false)
  })
  it.each(['restore', 'password', 'entry'] as const)(
    'system disposal does not cancel the %s owner',
    async (kind) => {
      const f = await fixture(),
        held = barrier<Response>()
      let signal: AbortSignal | undefined
      if (kind === 'entry') await f.auth.entry.prepare()
      if (kind === 'restore')
        f.setSessionRequest((_p, init) => {
          signal = init.signal!
          return held.promise
        })
      else
        f.setMutation((_p, init) => {
          signal = init.signal!
          return held.promise
        })
      const pending =
        kind === 'restore'
          ? f.auth.restore()
          : kind === 'password'
            ? f.auth.personal.changePassword({
                version: '1',
                current_password: 'Original password 123',
                new_password: 'Changed password 12345',
                confirmation: 'Changed password 12345',
              })
            : f.auth.entry.completePasswordReset({
                token: id(3) + '.' + 'T'.repeat(43),
                new_password: 'Changed password 12345',
                confirmation: 'Changed password 12345',
              })
      await flushPromises()
      f.auth.system.abandon()
      expect(signal?.aborted).toBe(false)
      expect(f.auth.state.busy).toBe(true)
      if (kind === 'password') f.setSession(view(id(1), id(4)))
      held.resolve(
        kind === 'restore'
          ? json(view())
          : kind === 'password'
            ? json({ completed: true, next_path: '/' })
            : new Response(null, { status: 204 }),
      )
      await pending
      expect(f.auth.state.phase).toBe('authenticated')
      expect(f.auth.state.busy).toBe(false)
    },
  )
})

describe('directory identity, permission and page ownership', () => {
  it('rejects non-admin calls before transport and binds a 403 until a successful Session check', async () => {
    const f = await fixture(),
      d = f.directory()
    await flushPromises()
    f.setList(async () => problem('FORBIDDEN', 403))
    await d.next()
    expect(f.auth.system.denied).toBe(true)
    expect(f.auth.state.user?.role).toBe('admin')
    expect(f.auth.state.phase).toBe('authenticated')
    expect(d.state.rows).toHaveLength(0)
    expect(d.state.phase).toBe('forbidden')
    expect(d.state.hasNext).toBe(false)
    const before = f.cursors().length
    await expect(f.auth.system.listUsers({})).rejects.toMatchObject({ kind: 'invalid-input' })
    await d.refresh()
    expect(f.cursors()).toHaveLength(before)
    f.setList(async () => json({ items: rows(10, 1) }))
    await f.auth.restore()
    await flushPromises()
    expect(f.auth.system.denied).toBe(false)
    expect(f.cursors().at(-1)).toBeNull()
    expect(d.state.rows).toHaveLength(1)
    f.setSession(view(id(1), id(2), 'user'))
    await f.auth.restore()
    await flushPromises()
    expect(d.state.rows).toHaveLength(0)
    await expect(f.auth.system.listUsers({})).rejects.toMatchObject({ kind: 'invalid-input' })
  })
  it.each(['UNAUTHENTICATED', 'SESSION_REVOKED'])(
    'invalidates Session and every page material on %s',
    async (code) => {
      const f = await fixture(),
        d = f.directory()
      await flushPromises()
      f.setList(async () => problem(code, 401))
      await d.next()
      expect(f.auth.state.phase).toBe('unavailable')
      expect(f.auth.state.user).toBeNull()
      expect(f.auth.personalContext.identity).toBeNull()
      expect(d.state.rows).toHaveLength(0)
      expect(d.state.hasPrevious || d.state.hasNext).toBe(false)
    },
  )
  it('clears the page during same-Session checking and reloads its first page once', async () => {
    const f = await fixture(),
      d = f.directory()
    await flushPromises()
    f.setList(async () => json({ items: rows(50), next_cursor: 'next-b' }))
    await d.next()
    const held = barrier<Response>()
    let signal: AbortSignal | undefined
    f.setSessionRequest((_p, init) => {
      signal = init.signal!
      return held.promise
    })
    const checking = f.auth.restore()
    await flushPromises()
    expect(d.state.rows).toHaveLength(0)
    expect(signal?.aborted).toBe(false)
    held.resolve(json(view()))
    await checking
    await flushPromises()
    expect(f.cursors()).toEqual([null, 'next-a', null])
    expect(d.state.hasPrevious).toBe(false)
  })
  it.each(['account', 'session'] as const)(
    'retires all paging history after a new %s identity',
    async (kind) => {
      const f = await fixture(),
        d = f.directory()
      await flushPromises()
      f.setList(async () => json({ items: rows(50), next_cursor: 'next-b' }))
      await d.next()
      const old = f.auth.personalContext.identity
      f.setSession(view(kind === 'account' ? id(8) : id(1), id(9)))
      await f.auth.restore()
      await flushPromises()
      expect(f.auth.personalContext.identity?.epoch).not.toBe(old?.epoch)
      expect(f.cursors().at(-1)).toBeNull()
      expect(d.state.hasPrevious).toBe(false)
    },
  )
  it.each(['success', 'forbidden', 'revoked'] as const)(
    'never publishes a late old-page %s into a later identity',
    async (kind) => {
      const f = await fixture(),
        held = barrier<Response>()
      f.setList(() => held.promise)
      const d = f.directory()
      await flushPromises()
      d.dispose()
      f.auth.leave()
      f.setSession(view(id(8), id(9)))
      held.resolve(
        kind === 'success'
          ? json({ items: rows() })
          : problem(
              kind === 'forbidden' ? 'FORBIDDEN' : 'SESSION_REVOKED',
              kind === 'forbidden' ? 403 : 401,
            ),
      )
      await flushPromises()
      await f.auth.restore()
      expect(f.auth.state.user?.id).toBe(id(8))
      expect(f.auth.system.denied).toBe(false)
      expect(d.state.rows).toHaveLength(0)
      expect(d.state.phase).toBe('inactive')
    },
  )
  it('waits once for an initial owner, but never automatically retries a failed read', async () => {
    const f = await fixture(),
      held = barrier<Response>()
    f.setMutation(() => held.promise)
    const password = f.auth.personal.changePassword({
      version: '1',
      current_password: 'Original password 123',
      new_password: 'Changed password 12345',
      confirmation: 'Changed password 12345',
    })
    const d = f.directory()
    await flushPromises()
    expect(f.cursors()).toEqual([])
    f.setSession(view(id(1), id(4)))
    f.setList(async () => problem('DEPENDENCY_UNAVAILABLE'))
    held.resolve(json({ completed: true, next_path: '/' }))
    await password
    await flushPromises()
    expect(f.cursors()).toEqual([null])
    expect(d.state.phase).toBe('error')
    expect(d.state.message).not.toContain('private')
    await flushPromises()
    expect(f.cursors()).toEqual([null])
    await d.retry()
    expect(f.cursors()).toEqual([null, null])
  })
  it('never adopts a directory self-row as User, theme, or personal draft', async () => {
    const f = await fixture(),
      settings = createPersonalSettings(f.auth)
    await settings.load('profile')
    settings.profileDraft.display_name = 'Unsubmitted draft'
    const personal = f.auth.state.user
    f.setList(async () =>
      json({
        items: [
          {
            ...view().user,
            display_name: 'Directory only',
            version: '99',
            theme: 'light',
            created_at: time,
          },
        ],
      }),
    )
    const d = f.directory()
    await flushPromises()
    expect(d.state.rows[0]?.display_name).toBe('Directory only')
    expect(f.auth.state.user).toBe(personal)
    expect(useTheme().mode.value).toBe('dark')
    expect(settings.profileDraft.display_name).toBe('Unsubmitted draft')
    d.dispose()
    settings.dispose()
  })
})

describe('cursor history represents fresh successful observations', () => {
  it('rereads previous pages, drops the old forward branch and refreshes from the first page', async () => {
    const f = await fixture(),
      d = f.directory()
    await flushPromises()
    f.setList(async () => json({ items: rows(50), next_cursor: 'next-b' }))
    await d.next()
    expect(d.state.hasPrevious).toBe(true)
    f.setList(async () => json({ items: rows(120), next_cursor: 'fresh-next' }))
    await d.previous()
    expect(d.state.rows[0]?.id).toBe(id(120))
    f.setList(async () => json({ items: rows(20, 1) }))
    await d.next()
    expect(f.cursors()).toEqual([null, 'next-a', null, 'fresh-next'])
    expect(d.state.hasNext).toBe(false)
    await d.refresh()
    expect(f.cursors().at(-1)).toBeNull()
    expect(d.state.hasPrevious).toBe(false)
  })
  it('hides old rows, rejects repeat operations, and retains a failed target without advancing history', async () => {
    const f = await fixture(),
      d = f.directory()
    await flushPromises()
    const held = barrier<Response>()
    f.setList(() => held.promise)
    const pending = d.next()
    await flushPromises()
    expect(d.state.rows).toHaveLength(0)
    expect(d.state.phase).toBe('loading')
    await d.next()
    await d.refresh()
    await d.previous()
    expect(f.cursors()).toEqual([null, 'next-a'])
    held.resolve(problem('DEPENDENCY_UNAVAILABLE'))
    await pending
    expect(d.state.phase).toBe('error')
    expect(d.state.hasPrevious).toBe(false)
    f.setList(async () => json({ items: rows(20, 1) }))
    await d.retry()
    expect(f.cursors()).toEqual([null, 'next-a', 'next-a'])
    expect(d.state.hasPrevious).toBe(true)
    await d.previous()
    expect(f.cursors().at(-1)).toBeNull()
  })
  it('requires an explicit first-page reload after CURSOR_INVALID and keeps empty separate from failure', async () => {
    const f = await fixture(),
      d = f.directory()
    await flushPromises()
    f.setList(async () => problem('CURSOR_INVALID', 400))
    await d.next()
    expect(d.state.cursorInvalid).toBe(true)
    expect(d.state.phase).toBe('error')
    await d.retry()
    expect(f.cursors()).toEqual([null, 'next-a'])
    f.setList(async () => json({ items: [] }))
    await d.refresh()
    expect(f.cursors()).toEqual([null, 'next-a', null])
    expect(d.state.phase).toBe('empty')
    expect(d.state.rows).toHaveLength(0)
    expect(d.state.hasNext || d.state.hasPrevious).toBe(false)
  })
})
