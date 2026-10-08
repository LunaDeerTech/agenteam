import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { AccountFailure, type Fetch, type Problem } from '../api/client'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createSystemAccountAPI } from '../api/system-account'
import { createSessionController, type SessionController } from '../composables/useSession'
import { createProjectWorkspace, type ProjectWorkspace } from '../composables/useProjectWorkspace'

const id = (n: number) => `01900000-0000-7000-8000-${String(n).padStart(12, '0')}`
const instant = '2026-10-08T10:00:00.000000Z'
const project = (projectID = id(10)): Project => ({
  id: projectID,
  owner_user_id: id(1),
  name: 'Demo',
  normalized_name: 'demo',
  description: 'original',
  lifecycle: 'active',
  version: '1',
  current_sprint_id: null,
  created_at: instant,
  updated_at: instant,
  archived_at: null,
})
const view = (): SessionView => ({
  user: {
    id: id(1),
    email: 'owner@example.test',
    username: 'owner',
    display_name: '',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: {
    id: id(2),
    issued_at: instant,
    absolute_expires_at: instant,
    idle_expires_at: instant,
  },
  csrf_token: 'S'.repeat(43),
})
const response = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
const problem = (
  code: string,
  status: number,
  commit_state: Problem['commit_state'] = 'not_started',
  fields?: Problem['field_errors'],
) =>
  response(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      detail: '',
      instance: '/api/v1/projects',
      status,
      code,
      commit_state,
      request_id: id(99),
      ...(fields ? { field_errors: fields } : {}),
    },
    status,
  )
const owners: SessionController[] = [],
  workspaces: ProjectWorkspace[] = [],
  releases: (() => void)[] = []
function deferred<T>(fallback: T) {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  releases.push(() => resolve(fallback))
  return { promise, resolve }
}
async function fixture() {
  let session = view(),
    current = project(),
    lookup: 'committed' | 'not_observed' | 'in_progress' = 'committed'
  let intercept: Fetch | undefined
  const fetcher = vi.fn<Fetch>(async (path, init) => {
    if (intercept) return intercept(path, init)
    return normal(path, init)
  })
  const normal: Fetch = async (path, init) => {
    if (path === '/api/v1/session') return response(session)
    if (path === '/api/v1/sessions/logout') return new Response(null, { status: 204 })
    if (path === '/api/v1/auth/bootstrap')
      return response({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path.startsWith('/api/v1/system/users')) return response({ items: [] })
    if (path.startsWith('/api/v1/projects?'))
      return response({
        items: [
          {
            id: current.id,
            name: current.name,
            description: current.description,
            lifecycle: current.lifecycle,
            version: current.version,
          },
        ],
        next_cursor: null,
      })
    if (path.startsWith('/api/v1/projects/resolve?')) return response(current)
    if (path.endsWith('/commands/lookup'))
      return response(
        lookup === 'committed'
          ? { state: lookup, result: { command: 'update', project: current } }
          : { state: lookup },
      )
    if (init.method === 'PATCH') {
      const input = JSON.parse(init.body as string) as {
        expected_version: string
        name?: string
        description?: string
      }
      current = {
        ...current,
        ...(input.name !== undefined
          ? { name: input.name, normalized_name: input.name.toLowerCase() }
          : {}),
        ...(input.description !== undefined ? { description: input.description } : {}),
        version: String(BigInt(current.version) + 1n),
      }
      return response(current)
    }
    if (path.startsWith('/api/v1/projects/')) return response(current)
    throw new Error('Unexpected endpoint')
  }
  const auth = createSessionController(
    createAccountAPI(fetcher),
    createSystemAccountAPI(fetcher),
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
    createProjectOwnerAPI(fetcher),
  )
  owners.push(auth)
  await auth.restore()
  const replacements: string[] = []
  const workspace = createProjectWorkspace(auth, async (path) => {
    replacements.push(path)
    workspace.afterNavigation(path)
  })
  workspaces.push(workspace)
  return {
    auth,
    workspace,
    fetcher,
    normal,
    replacements,
    intercept(value?: Fetch) {
      intercept = value
    },
    session(value: SessionView) {
      session = value
    },
    current(value: Project) {
      current = value
    },
    lookup(value: typeof lookup) {
      lookup = value
    },
  }
}
async function open(f: Awaited<ReturnType<typeof fixture>>, path = '/owner/demo/settings/general') {
  f.workspace.afterNavigation(path)
  await flushPromises()
}
afterEach(async () => {
  releases.splice(0).forEach((release) => release())
  await flushPromises()
  workspaces.splice(0).forEach((workspace) => workspace.dispose())
  owners.splice(0).forEach((auth) => auth.leave())
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('Project owner state and original requests', () => {
  it('allows ordinary Owners through Resolve then stable Get while System stays gated', async () => {
    const f = await fixture()
    await open(f)
    expect(f.workspace.detail.project?.id).toBe(id(10))
    expect(f.fetcher.mock.calls.slice(1).map(([path]) => path)).toEqual([
      '/api/v1/projects/resolve?username=owner&project_name=demo',
      `/api/v1/projects/${id(10)}`,
    ])
    await expect(f.auth.system.listUsers({})).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(f.fetcher.mock.calls.some(([path]) => path.startsWith('/api/v1/system'))).toBe(false)
  })
  it('never publishes a Resolve projection when stable Get denies and never sends child reads', async () => {
    const f = await fixture()
    f.intercept(async (path, init) =>
      path === `/api/v1/projects/${id(10)}` ? problem('FORBIDDEN', 403) : f.normal(path, init),
    )
    await open(f)
    expect(f.workspace.detail.phase).toBe('unavailable')
    expect(f.workspace.detail.project).toBeNull()
    expect(f.workspace.editor.ready).toBe(false)
    expect(f.auth.system.denied).toBe(false)
  })
  it('does not make a deleting Resolve projection editable or publish its contents', async () => {
    const f = await fixture()
    f.intercept(async (path, init) =>
      path.includes('/resolve?')
        ? response({ ...project(), lifecycle: 'deleting' })
        : f.normal(path, init),
    )
    await open(f)
    expect(f.workspace.detail.project).toBeNull()
    expect(f.workspace.detail.phase).toBe('unavailable')
    expect(f.fetcher.mock.calls).toHaveLength(2)
  })
  it.each([
    '/api/v1/projects',
    '/system/unknown',
    '/owner/demo?token=x',
    '/owner/%64emo',
    '/owner/demo/extra',
  ])('raw path rejects before Project API: %s', async (path) => {
    const f = await fixture()
    await open(f, path)
    expect(f.fetcher.mock.calls).toHaveLength(1)
  })
  it('keeps a dirty draft through checking of the same identity, clears it for changed CSRF', async () => {
    const f = await fixture()
    await open(f)
    f.workspace.draft.description = 'dirty'
    const gate = deferred(response(view()))
    f.intercept(async (path, init) =>
      path === '/api/v1/session' ? gate.promise : f.normal(path, init),
    )
    const restoring = f.auth.restore()
    await flushPromises()
    expect(f.workspace.visible.value).toBe(false)
    expect(f.workspace.detail.phase).toBe('checking')
    expect(f.workspace.draft.description).toBe('dirty')
    gate.resolve(response(view()))
    await restoring
    expect(f.workspace.visible.value).toBe(true)
    expect(f.workspace.draft.description).toBe('dirty')
    f.intercept(undefined)
    f.session({ ...view(), csrf_token: 'T'.repeat(43) })
    await f.auth.restore()
    await flushPromises()
    expect(f.workspace.draft.description).toBe('original')
    expect(f.workspace.dirty.value).toBe(false)
  })
  it('cancels navigation without losing draft and asks only once', async () => {
    const f = await fixture()
    await open(f)
    f.workspace.draft.description = 'dirty'
    const first = f.workspace.confirmLeave('/projects')
    expect(await f.workspace.confirmLeave('/')).toBe(false)
    f.workspace.finishConfirmation(false)
    expect(await first).toBe(false)
    expect(f.workspace.draft.description).toBe('dirty')
    const accepted = f.workspace.confirmLeave('/projects')
    f.workspace.finishConfirmation(true)
    expect(await accepted).toBe(true)
    f.workspace.afterNavigation('/projects')
    await flushPromises()
    expect(f.workspace.editor.ready).toBe(false)
  })
  it('saves only changed fields, confirms first, and re-reads by original ID after a rename', async () => {
    const f = await fixture()
    await open(f)
    f.workspace.draft.name = 'Renamed'
    const saving = f.workspace.save()
    expect(f.workspace.confirmation.open).toBe(true)
    expect(f.fetcher.mock.calls.some(([, init]) => init.method === 'PATCH')).toBe(false)
    f.workspace.finishConfirmation(true)
    await saving
    expect(f.workspace.progress.value?.phase).toBe('confirmed')
    expect(f.workspace.detail.project?.normalized_name).toBe('renamed')
    expect(f.replacements).toEqual(['/owner/renamed/settings/general'])
    const writes = f.fetcher.mock.calls.filter(([, init]) => init.method === 'PATCH')
    expect(writes).toHaveLength(1)
    expect(JSON.parse(writes[0]![1].body as string)).toEqual({
      expected_version: '1',
      name: 'Renamed',
    })
    expect(f.workspace.dirty.value).toBe(false)
  })
  it('preserves confirmation when current Get fails and explicitly retries the stable ID only', async () => {
    const f = await fixture()
    await open(f)
    let committed = false,
      failRead = true
    f.intercept(async (path, init) => {
      if (init.method === 'PATCH') {
        committed = true
        return f.normal(path, init)
      }
      if (committed && init.method === 'GET' && failRead)
        return problem('DEPENDENCY_UNAVAILABLE', 503)
      return f.normal(path, init)
    })
    f.workspace.draft.description = 'saved'
    await f.workspace.save()
    expect(f.workspace.progress.value?.phase).toBe('confirmed')
    expect(f.workspace.writeMessage.value).toContain('当前信息读取失败')
    expect(f.workspace.canSave.value).toBe(false)
    failRead = false
    await f.workspace.readCurrent()
    expect(f.workspace.detail.project?.description).toBe('saved')
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'PATCH')).toHaveLength(1)
  })
  it.each([
    ['INVALID_ARGUMENT', 400, undefined],
    ['VERSION_CONFLICT', 409, undefined],
    ['INVALID_STATE', 409, undefined],
    ['PROJECT_NOT_ACTIVE', 409, undefined],
    ['RESOURCE_BUSY', 409, [{ path: '/name', code: 'NAME_TAKEN' }]],
  ] as const)('classifies the first precise rejection %s', async (code, status, fields) => {
    const f = await fixture()
    await open(f)
    f.intercept(async (path, init) =>
      init.method === 'PATCH'
        ? problem(code, status, 'not_committed', fields ? [...fields] : undefined)
        : f.normal(path, init),
    )
    f.workspace.draft.description = 'kept'
    await f.workspace.save()
    expect(f.workspace.progress.value?.phase).toBe('rejected')
    expect(f.workspace.draft.description).toBe('kept')
  })
  it('keeps uncertainty sticky and never polls, changes the key/body or rebases the version', async () => {
    const f = await fixture()
    await open(f)
    let attempt = 0
    f.intercept(async (path, init) => {
      if (init.method === 'PATCH') {
        if (++attempt === 1) throw new Error('lost response')
        return problem('VERSION_CONFLICT', 409, 'not_committed')
      }
      return f.normal(path, init)
    })
    f.workspace.draft.description = 'original intent'
    await f.workspace.save()
    expect(f.workspace.progress.value?.phase).toBe('uncertain')
    expect(f.fetcher.mock.calls.some(([path]) => path.endsWith('/lookup'))).toBe(false)
    f.workspace.draft.description = 'later input'
    await f.workspace.replayOriginal()
    expect(f.workspace.progress.value?.phase).toBe('uncertain')
    const writes = f.fetcher.mock.calls.filter(([, init]) => init.method === 'PATCH')
    expect(
      writes.length === 2 &&
        writes[0]![1].body === writes[1]![1].body &&
        JSON.stringify(writes[0]![1].headers) === JSON.stringify(writes[1]![1].headers),
    ).toBe(true)
    expect(f.workspace.draft.description).toBe('later input')
  })
  it('lookup three states remain explicit and in_progress forbids original replay', async () => {
    const f = await fixture()
    await open(f)
    f.intercept(async (path, init) => {
      if (init.method === 'PATCH') {
        await f.normal(path, init)
        throw new Error('lost response')
      }
      return f.normal(path, init)
    })
    f.workspace.draft.description = 'committed'
    await f.workspace.save()
    f.lookup('not_observed')
    await f.workspace.checkOriginal()
    expect(f.workspace.progress.value?.observation).toBe('not_observed')
    expect(f.workspace.canReplay.value).toBe(true)
    f.lookup('in_progress')
    await f.workspace.checkOriginal()
    expect(f.workspace.progress.value?.observation).toBe('in_progress')
    expect(f.workspace.canReplay.value).toBe(false)
    f.lookup('committed')
    await f.workspace.checkOriginal()
    expect(f.workspace.progress.value?.phase).toBe('confirmed')
    expect(f.workspace.detail.project?.description).toBe('committed')
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'PATCH')).toHaveLength(1)
  })
  it('a key conflict cannot enable replay, including after not_observed', async () => {
    const f = await fixture()
    await open(f)
    f.intercept(async (path, init) =>
      init.method === 'PATCH'
        ? problem('IDEMPOTENCY_KEY_REUSED', 409, 'not_committed')
        : f.normal(path, init),
    )
    f.workspace.draft.description = 'kept'
    await f.workspace.save()
    f.lookup('not_observed')
    await f.workspace.checkOriginal()
    expect(f.workspace.canReplay.value).toBe(false)
    expect(f.workspace.progress.value?.phase).toBe('uncertain')
  })
  it('a visible timeout retains the unique Cookie owner through the actual tail', async () => {
    const f = await fixture()
    await open(f)
    vi.useFakeTimers()
    const gate = deferred(response({ ...project(), description: 'saved', version: '2' }))
    f.intercept(async (path, init) =>
      init.method === 'PATCH' ? gate.promise : f.normal(path, init),
    )
    f.workspace.draft.description = 'saved'
    const saving = f.workspace.save()
    await Promise.resolve()
    await Promise.resolve()
    await vi.advanceTimersByTimeAsync(30_000)
    await saving
    expect(f.workspace.progress.value?.phase).toBe('uncertain')
    expect(f.auth.state.busy).toBe(true)
    const calls = f.fetcher.mock.calls.length
    await f.auth.restore()
    await f.auth.logout()
    expect(f.fetcher.mock.calls.length).toBe(calls)
    gate.resolve(response({ ...project(), description: 'saved', version: '2' }))
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.workspace.progress.value?.phase).toBe('uncertain')
  })
  it('invalidates old late read publication and retains no draft across a reused name ID', async () => {
    const f = await fixture()
    await open(f)
    f.workspace.draft.description = 'old draft'
    f.workspace.afterNavigation('/projects')
    await flushPromises()
    f.current(project(id(11)))
    await open(f)
    expect(f.workspace.detail.project?.id).toBe(id(11))
    expect(f.workspace.draft.description).toBe('original')
    const gate = deferred(response(project(id(11))))
    f.intercept(async (path, init) =>
      path === `/api/v1/projects/${id(11)}` ? gate.promise : f.normal(path, init),
    )
    const read = f.workspace.readCurrent()
    await flushPromises()
    f.workspace.afterNavigation('/')
    gate.resolve(response(project(id(11))))
    await read
    await flushPromises()
    expect(f.workspace.detail.project).toBeNull()
    expect(f.auth.state.busy).toBe(false)
  })
  it('keeps read-only archived state while retaining uncertain original recovery', async () => {
    const f = await fixture()
    await open(f)
    f.intercept(async (path, init) =>
      init.method === 'PATCH' ? Promise.reject(new Error('lost')) : f.normal(path, init),
    )
    f.workspace.draft.description = 'pending'
    await f.workspace.save()
    f.current({ ...project(), lifecycle: 'archived', archived_at: instant, version: '2' })
    await f.workspace.readCurrent()
    expect(f.workspace.readOnly.value).toBe(true)
    expect(f.workspace.canLookup.value).toBe(true)
    expect(f.workspace.canReplay.value).toBe(true)
  })
  it('keeps current observations separate from dirty inputs on conflict', async () => {
    const f = await fixture()
    await open(f)
    f.workspace.draft.description = 'dirty'
    f.current({ ...project(), description: 'concurrent', version: '2' })
    await f.workspace.readCurrent()
    expect(f.workspace.draft.description).toBe('dirty')
    expect(f.workspace.editor.version).toBe('1')
    expect(f.workspace.editor.conflict).toBe(true)
    expect(f.workspace.canSave.value).toBe(false)
    const adopting = f.workspace.adoptCurrent()
    f.workspace.finishConfirmation(true)
    await adopting
    expect(f.workspace.draft.description).toBe('concurrent')
    expect(f.workspace.editor.version).toBe('2')
  })
})

describe('Project conflict observation freshness', () => {
  it('requires a successful current Get after conflict before explicit adoption', async () => {
    const f = await fixture()
    await open(f)
    let failRead = true
    f.intercept(async (path, init) =>
      init.method === 'PATCH'
        ? problem('VERSION_CONFLICT', 409, 'not_committed')
        : init.method === 'GET' && path === `/api/v1/projects/${id(10)}` && failRead
          ? problem('DEPENDENCY_UNAVAILABLE', 503)
          : f.normal(path, init),
    )
    f.workspace.draft.description = 'dirty'
    await f.workspace.save()
    expect(f.workspace.canAdoptCurrent.value).toBe(false)
    await f.workspace.adoptCurrent()
    expect(f.workspace.draft.description).toBe('dirty')
    await f.workspace.readCurrent()
    expect(f.workspace.canAdoptCurrent.value).toBe(false)
    failRead = false
    f.current({ ...project(), version: '2', description: 'current' })
    await f.workspace.readCurrent()
    expect(f.workspace.canAdoptCurrent.value).toBe(true)
    expect(f.workspace.draft.description).toBe('dirty')
    const adopting = f.workspace.adoptCurrent()
    expect(f.workspace.confirmation.open).toBe(true)
    f.workspace.finishConfirmation(false)
    await adopting
    expect(f.workspace.editor.version).toBe('1')
    const accepted = f.workspace.adoptCurrent()
    f.workspace.finishConfirmation(true)
    await accepted
    expect(f.workspace.draft.description).toBe('current')
    expect(f.workspace.editor.version).toBe('2')
    expect(f.workspace.progress.value).toBeNull()
  })
  it.each([
    ['DEPENDENCY_UNAVAILABLE', 503, 'not_started'],
    ['RESOURCE_BUSY', 409, 'not_started'],
    ['VERSION_CONFLICT', 409, 'unknown'],
  ] as const)(
    'does not downgrade dispatched %s/%s/%s to rejection',
    async (code, status, state) => {
      const f = await fixture()
      await open(f)
      f.intercept(async (path, init) =>
        init.method === 'PATCH' ? problem(code, status, state) : f.normal(path, init),
      )
      f.workspace.draft.description = 'kept'
      await f.workspace.save()
      expect(f.workspace.progress.value?.phase).toBe('uncertain')
    },
  )
  it('ordinary read Unknown never creates an intent or calls lookup', async () => {
    const f = await fixture()
    f.intercept(async (path, init) =>
      path.includes('/projects/')
        ? problem('COMMIT_UNKNOWN', 503, 'unknown')
        : f.normal(path, init),
    )
    await open(f)
    expect(f.workspace.detail.phase).toBe('read-error')
    expect(f.workspace.progress.value).toBeNull()
    expect(f.fetcher.mock.calls.some(([path]) => path.endsWith('/lookup'))).toBe(false)
  })
})

describe('Project locator and paginated observations', () => {
  it('rechecks a changed current username by stable ID and preserves a dirty draft', async () => {
    const f = await fixture()
    await open(f)
    f.workspace.draft.description = 'dirty'
    f.session({ ...view(), user: { ...view().user, username: 'renamed-owner', version: '2' } })
    const before = f.fetcher.mock.calls.length
    await f.auth.restore()
    await flushPromises()
    expect(f.fetcher.mock.calls.slice(before).map(([path]) => path)).toEqual([
      '/api/v1/session',
      `/api/v1/projects/${id(10)}`,
    ])
    expect(f.replacements).toEqual(['/renamed-owner/demo/settings/general'])
    expect(f.workspace.draft.description).toBe('dirty')
  })
  it('keeps whole pages and opaque cursor context through retry, back and filters', async () => {
    const f = await fixture()
    let fail = true
    const rows = Array.from({ length: 25 }, (_, i) => ({
      id: id(100 + i),
      name: `p-${i}`,
      description: '',
      lifecycle: 'active',
      version: '1',
    }))
    f.intercept(async (path, init) => {
      if (!path.startsWith('/api/v1/projects?')) return f.normal(path, init)
      const query = new URL(path, 'http://localhost').searchParams
      if (query.get('cursor') === 'opaque+/') {
        if (fail) throw new Error('read failed')
        return response({ items: [], next_cursor: null })
      }
      return response({ items: rows, next_cursor: 'opaque+/' })
    })
    await open(f, '/projects')
    expect(f.workspace.list.items).toHaveLength(25)
    await f.workspace.next()
    expect(f.workspace.list.stale).toBe(true)
    expect(f.workspace.list.page).toBe(1)
    fail = false
    await f.workspace.retryList()
    expect(f.workspace.list.page).toBe(2)
    expect(f.workspace.hasPrevious.value).toBe(true)
    await f.workspace.previous()
    expect(f.workspace.list.items).toHaveLength(25)
    await f.workspace.setFilter('active')
    expect(f.workspace.hasPrevious.value).toBe(false)
    expect(f.fetcher.mock.calls.at(-1)?.[0]).toBe('/api/v1/projects?limit=25&lifecycle=active')
  })
})

describe('Project checking preserves observation quality', () => {
  it('does not upgrade a failed current read to current when the same Session returns', async () => {
    const f = await fixture()
    await open(f)
    f.workspace.draft.description = 'dirty'
    f.intercept(async (path, init) =>
      path === `/api/v1/projects/${id(10)}`
        ? problem('DEPENDENCY_UNAVAILABLE', 503)
        : f.normal(path, init),
    )
    await f.workspace.readCurrent()
    expect(f.workspace.detail.phase).toBe('read-error')
    await f.auth.restore()
    await flushPromises()
    expect(f.workspace.detail.phase).toBe('read-error')
    expect(f.workspace.canSave.value).toBe(false)
    expect(f.workspace.draft.description).toBe('dirty')
  })
})
