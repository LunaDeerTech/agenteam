import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createSystemAccountAPI } from '../api/system-account'
import { createKnowledgeOwnerAPI } from '../api/knowledge-owner'
import { type Fetch } from '../api/client'
import { createSessionController, type SessionController } from '../composables/useSession'
import { createProjectWorkspace, type ProjectWorkspace } from '../composables/useProjectWorkspace'
import { useKnowledgeOwner, type KnowledgeOwnerController } from '../composables/useKnowledgeOwner'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-10T12:00:00.123456Z',
  base = `/api/v1/projects/${id(10)}/knowledge/documents`
const session = (role: 'user' | 'admin' = 'user'): SessionView => ({
  user: {
    id: id(1),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role,
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const project = (lifecycle: Project['lifecycle'] = 'active'): Project => ({
  id: id(10),
  owner_user_id: id(1),
  name: 'Demo',
  normalized_name: 'demo',
  description: '',
  lifecycle,
  version: '1',
  current_sprint_id: null,
  created_at: time,
  updated_at: time,
  archived_at: lifecycle === 'archived' ? time : null,
})
const document = (n = 20, parent: string | null = null, version = '1') => ({
  id: id(n),
  project_id: id(10),
  parent_document_id: parent,
  title: n === 20 ? '父文档' : '子文档',
  content_version: version,
  source_kind: 'text',
  media_type: 'text/plain',
  status: 'active',
  indexing_status: 'pending',
  created_by: { kind: 'human', user_id: id(1) },
  created_at: time,
  updated_at: time,
})
const content = (n = 20, value = '父文档正文', version = '1', offset = '0', truncated = false) => ({
  document: document(n, n === 21 ? id(20) : null, version),
  text: {
    text: value,
    next_byte_offset: (
      BigInt(offset) + BigInt(new TextEncoder().encode(value).byteLength)
    ).toString(),
    truncated,
  },
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
const problem = (status: number, code: string) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      detail: '',
      status,
      code,
      instance: base,
      request_id: id(99),
      commit_state: 'not_started',
    },
    status,
  )
function barrier<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
const pages: KnowledgeOwnerController[] = [],
  workspaces: ProjectWorkspace[] = [],
  owners: SessionController[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  for (const page of pages.splice(0)) page.dispose()
  for (const release of releases.splice(0)) release()
  await flushPromises()
  for (const workspace of workspaces.splice(0)) workspace.dispose()
  for (const owner of owners.splice(0)) owner.leave()
  await flushPromises()
  vi.useRealTimers()
})
async function fixture(
  options: {
    role?: 'user' | 'admin'
    lifecycle?: Project['lifecycle']
    bind?: boolean
    documentID?: string
  } = {},
) {
  let current = session(options.role),
    currentProject = project(options.lifecycle)
  let intercept: Fetch | undefined
  const normal: Fetch = async (path) => {
    const url = new URL(path, 'https://owned.invalid')
    if (path === '/api/v1/session') return json(current)
    if (path === '/api/v1/auth/bootstrap')
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path === '/api/v1/system/users?limit=25') return json({ items: [] })
    if (
      path.startsWith('/api/v1/projects/resolve?') ||
      path === `/api/v1/projects/${currentProject.id}`
    )
      return json(currentProject)
    if (url.pathname === `${base}/children`)
      return json({
        items:
          url.searchParams.get('parent_document_id') === 'null'
            ? [document()]
            : [document(21, id(20))],
      })
    if (url.pathname.endsWith('/ancestors'))
      return json({ items: url.pathname.includes(id(21)) ? [document()] : [] })
    const number = url.pathname.includes(id(21)) ? 21 : 20
    if (url.pathname.endsWith('/content'))
      return json(content(number, number === 21 ? '子文档正文' : '父文档正文'))
    return json({ active: document(number, number === 21 ? id(20) : null) })
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[1] = createSystemAccountAPI(fetch)
  args[12] = createProjectOwnerAPI(fetch)
  args[15] = createKnowledgeOwnerAPI(fetch)
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  const workspace = createProjectWorkspace(auth)
  workspaces.push(workspace)
  if (options.bind !== false) {
    workspace.afterNavigation('/owner/demo/knowledge')
    await flushPromises()
    expect(workspace.detail.phase).toBe('current')
  }
  const location = shallowRef({
    projectPath: '/owner/demo',
    documentID: options.documentID ?? null,
  })
  const page = useKnowledgeOwner(auth, workspace, location)
  pages.push(page)
  await flushPromises()
  return {
    auth,
    workspace,
    page,
    location,
    fetch,
    normal,
    intercept(value: Fetch) {
      intercept = value
    },
    session(value: SessionView) {
      current = value
    },
    project(value: Project) {
      currentProject = value
    },
  }
}

describe('Knowledge Human Session and actual Cookie owner', () => {
  it.each(['active', 'archived'] as const)(
    'allows an ordinary Owner in %s and publishes after original completion',
    async (lifecycle) => {
      const f = await fixture({ lifecycle })
      expect(f.auth.state.user?.role).toBe('user')
      expect(f.page.root.value.items).toHaveLength(1)
      expect(f.page.state.selected).toBeNull()
      f.page.select(id(20))
      await flushPromises()
      expect(f.page.state.content.phase).toBe('ready')
      expect(f.page.state.content.value).toEqual(content())
      expect(f.page.state.ancestors.items).toEqual([])
      expect(f.auth.state.busy).toBe(false)
      expect(
        f.fetch.mock.calls
          .filter(([path]) => path.startsWith(base))
          .map(([path]) => path.split('?')[0]),
      ).toEqual([
        `${base}/children`,
        `${base}/${id(20)}`,
        `${base}/${id(20)}/ancestors`,
        `${base}/${id(20)}/content`,
      ])
    },
  )
  it('never reads a child before current Project Get has granted the workspace context', async () => {
    const f = await fixture({ bind: false })
    expect(f.fetch.mock.calls.some(([path]) => path.startsWith(base))).toBe(false)
    f.workspace.afterNavigation('/owner/demo/knowledge')
    await flushPromises()
    expect(f.page.root.value.items).toHaveLength(1)
  })
  it('clears the current identity on current 401 and hides all protected observations', async () => {
    const f = await fixture()
    f.intercept(async (path, init) =>
      path.startsWith(base) ? problem(401, 'SESSION_REVOKED') : f.normal(path, init),
    )
    f.page.select(id(20))
    await flushPromises()
    expect(f.auth.state.phase).toBe('unavailable')
    expect(f.auth.personalContext.identity).toBeNull()
    expect(f.page.visible.value).toBe(false)
    expect(f.page.state.document).toBeNull()
    expect(f.page.root.value.items).toEqual([])
  })
  it('keeps a local 403 out of System denied state', async () => {
    const f = await fixture({ role: 'admin' })
    f.intercept(async (path, init) =>
      path.startsWith(base) ? problem(403, 'FORBIDDEN') : f.normal(path, init),
    )
    await expect(f.auth.knowledge.get(id(10), id(20))).rejects.toMatchObject({
      problem: { status: 403 },
    })
    expect(f.auth.state.phase).toBe('authenticated')
    await expect(f.auth.system.listUsers({})).resolves.toMatchObject({ items: [] })
  })
  it('retires a delayed 401 and retains the actual lane until it returns, then accepts a new Session', async () => {
    const f = await fixture(),
      gate = barrier<Response>()
    releases.push(() => gate.resolve(problem(401, 'SESSION_REVOKED')))
    f.intercept((path, init) =>
      path === `${base}/${id(20)}` ? gate.promise : f.normal(path, init),
    )
    const read = f.auth.knowledge.get(id(10), id(20)).catch((error: unknown) => error)
    await flushPromises()
    f.auth.knowledge.abandon()
    await read
    expect(f.auth.state.busy).toBe(true)
    const calls = f.fetch.mock.calls.length
    const restore = f.auth.restore()
    await flushPromises()
    expect(f.fetch.mock.calls).toHaveLength(calls)
    gate.resolve(problem(401, 'SESSION_REVOKED'))
    await restore
    await flushPromises()
    expect(f.auth.state.phase).not.toBe('unavailable')
    const original = session(),
      next = { ...original, session: { ...original.session, id: id(3) } }
    f.session(next)
    await f.auth.restore()
    expect(f.auth.state.session?.id).toBe(id(3))
  })
  it.each(['reader', 'outer'] as const)(
    'holds %s completion while a new selection waits and never publishes the retired body',
    async (held) => {
      const f = await fixture(),
        gate = barrier(),
        entered = barrier()
      const payload = JSON.stringify(content())
      let control!: ReadableStreamDefaultController<Uint8Array>
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          control = c
          c.enqueue(new TextEncoder().encode(payload))
          if (held === 'outer') c.close()
        },
        cancel() {
          entered.resolve()
          return gate.promise
        },
      })
      if (held === 'outer') {
        const cancel = stream.cancel.bind(stream)
        stream.cancel = async (reason) => {
          await cancel(reason)
          entered.resolve()
          await gate.promise
        }
      }
      releases.push(() => gate.resolve())
      f.intercept((path, init) =>
        path.startsWith(`${base}/${id(20)}/content`)
          ? Promise.resolve(
              new Response(stream, { headers: { 'Content-Type': 'application/json' } }),
            )
          : f.normal(path, init),
      )
      f.page.select(id(20))
      await flushPromises()
      if (held === 'outer') await entered.promise
      expect(f.page.state.content.phase).toBe('loading')
      f.page.select(id(21))
      await flushPromises()
      if (held === 'reader') await entered.promise
      expect(f.auth.state.busy).toBe(true)
      expect(f.page.state.content.value).toBeNull()
      expect(f.fetch.mock.calls.some(([path]) => path === `${base}/${id(21)}`)).toBe(false)
      gate.resolve()
      await flushPromises()
      expect(f.auth.state.busy).toBe(false)
      expect(f.page.state.document?.id).toBe(id(21))
      expect(f.page.state.content.value).toEqual(content(21, '子文档正文'))
      expect(control).toBeDefined()
    },
  )
})

describe('Knowledge workspace observations and bounded text navigation', () => {
  it('continues only the requested level with its cursor and deduplicates concurrent repeated IDs', async () => {
    const f = await fixture()
    const row = (n: number) => ({ ...document(n), title: String(n).padStart(3, '0') })
    f.intercept(async (path, init) =>
      path.includes('/children?')
        ? json(
            new URL(path, 'https://owned.invalid').searchParams.has('cursor')
              ? { items: [row(69), row(70)] }
              : {
                  items: Array.from({ length: 50 }, (_, i) => row(i + 20)),
                  next_cursor: 'next.page',
                },
          )
        : f.normal(path, init),
    )
    f.page.refreshTree()
    await flushPromises()
    expect(f.page.root.value.items).toHaveLength(50)
    f.page.readLevel(null, true)
    await flushPromises()
    expect(f.page.root.value.items).toHaveLength(51)
    expect(f.page.root.value.nextCursor).toBeUndefined()
    expect(f.fetch.mock.calls.at(-1)?.[0]).toBe(
      `${base}/children?parent_document_id=null&limit=50&cursor=next.page`,
    )
  })
  it.each([
    [null, 403, 'FORBIDDEN'],
    [id(20), 503, 'DEPENDENCY_UNAVAILABLE'],
  ] as const)(
    'discards only the failed continuation level %s and its cursor on %s',
    async (parent, status, code) => {
      const f = await fixture()
      f.page.setExpanded([id(20)])
      await flushPromises()
      const otherParent = parent === null ? id(20) : null
      const other = f.page.level(otherParent)
      f.intercept(async (path, init) => {
        const url = new URL(path, 'https://owned.invalid')
        if (
          url.pathname !== `${base}/children` ||
          url.searchParams.get('parent_document_id') !== (parent ?? 'null')
        )
          return f.normal(path, init)
        return url.searchParams.has('cursor')
          ? problem(status, code)
          : json({
              items: Array.from({ length: 50 }, (_, i) => ({
                ...document(i + 100, parent),
                title: String(i + 100),
              })),
              next_cursor: 'failed.page',
            })
      })
      f.page.readLevel(parent)
      await flushPromises()
      expect(f.page.level(parent).items).toHaveLength(50)
      expect(f.page.level(parent).nextCursor).toBe('failed.page')
      f.page.readLevel(parent, true)
      await flushPromises()
      expect(f.page.level(parent)).toMatchObject({
        phase: status === 403 ? 'unavailable' : 'error',
        items: [],
      })
      expect(f.page.level(parent).nextCursor).toBeUndefined()
      expect(f.page.level(otherParent)).toBe(other)
      const calls = f.fetch.mock.calls.length
      f.page.readLevel(parent, true)
      await flushPromises()
      expect(f.fetch.mock.calls).toHaveLength(calls)
      expect(f.auth.state.phase).toBe('authenticated')
      expect(f.auth.state.busy).toBe(false)
    },
  )
  it('discards a cancelled continuation and retains the actual Cookie owner until its tail', async () => {
    const f = await fixture(),
      gate = barrier<Response>()
    releases.push(() => gate.resolve(json({ items: [] })))
    f.page.setExpanded([id(20)])
    await flushPromises()
    const child = f.page.level(id(20))
    f.intercept((path, init) => {
      const url = new URL(path, 'https://owned.invalid')
      if (url.pathname !== `${base}/children`) return f.normal(path, init)
      return url.searchParams.has('cursor')
        ? gate.promise
        : Promise.resolve(
            json({
              items: Array.from({ length: 50 }, (_, i) => ({
                ...document(i + 100),
                title: String(i + 100),
              })),
              next_cursor: 'held.page',
            }),
          )
    })
    f.page.readLevel(null)
    await flushPromises()
    expect(f.page.root.value.items).toHaveLength(50)
    f.page.readLevel(null, true)
    await flushPromises()
    expect(f.page.root.value.phase).toBe('loading')
    f.page.cancel()
    await flushPromises()
    expect(f.page.root.value).toMatchObject({ phase: 'error', items: [] })
    expect(f.page.root.value.nextCursor).toBeUndefined()
    expect(f.page.level(id(20))).toBe(child)
    expect(f.auth.state.busy).toBe(true)
    gate.resolve(json({ items: [] }))
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.page.root.value).toMatchObject({ phase: 'error', items: [] })
    expect(f.page.root.value.nextCursor).toBeUndefined()
  })
  it.each([
    [403, 'FORBIDDEN'],
    [410, 'RESOURCE_DELETED'],
  ] as const)(
    'stops the selected document chain when ancestors report %s',
    async (status, code) => {
      const f = await fixture()
      f.intercept(async (path, init) =>
        path.endsWith('/ancestors') ? problem(status, code) : f.normal(path, init),
      )
      f.page.select(id(20))
      await flushPromises()
      expect(f.page.state.document).toBeNull()
      expect(f.page.state.metadataPhase).toBe(status === 410 ? 'deleted' : 'unavailable')
      expect(f.page.state.content.value).toBeNull()
      expect(f.fetch.mock.calls.some(([path]) => path.includes('/content?'))).toBe(false)
      expect(f.auth.state.phase).toBe('authenticated')
    },
  )
  it('clears the old Project observation and waits for its actual tail before reading another Project', async () => {
    const f = await fixture(),
      gate = barrier<Response>()
    releases.push(() => gate.resolve(json({ active: document() })))
    const nextBase = `/api/v1/projects/${id(11)}/knowledge/documents`
    f.intercept((path, init) =>
      path === `${base}/${id(20)}`
        ? gate.promise
        : path.startsWith(nextBase)
          ? Promise.resolve(json({ items: [] }))
          : f.normal(path, init),
    )
    f.page.select(id(20))
    await flushPromises()
    f.project({ ...project(), id: id(11), name: 'Other', normalized_name: 'other' })
    f.location.value = { projectPath: '/owner/other', documentID: null }
    f.workspace.afterNavigation('/owner/other/knowledge')
    await flushPromises()
    expect(f.page.visible.value).toBe(false)
    expect(f.page.state.document).toBeNull()
    expect(f.page.root.value.items).toEqual([])
    expect(f.auth.state.busy).toBe(true)
    expect(f.fetch.mock.calls.some(([path]) => path.startsWith(nextBase))).toBe(false)
    gate.resolve(json({ active: document() }))
    await flushPromises()
    expect(f.workspace.currentReadContext.value?.projectID).toBe(id(11))
    expect(f.page.visible.value).toBe(true)
    expect(f.page.root.value.phase).toBe('empty')
    expect(f.page.state.document).toBeNull()
    expect(f.fetch.mock.calls.some(([path]) => path.includes('/content?'))).toBe(false)
  })
  it('lazily reads a child branch independently from selecting a parent', async () => {
    const f = await fixture()
    f.page.setExpanded([id(20)])
    await flushPromises()
    expect(f.page.level(id(20)).items[0]?.id).toBe(id(21))
    expect(f.page.state.selected).toBeNull()
    expect(f.fetch.mock.calls.some(([path]) => path.includes('/content'))).toBe(false)
    f.page.select(id(20))
    await flushPromises()
    expect(f.page.state.content.value).toEqual(content())
  })
  it('uses deep-link ancestors only as a path and never fills unqueried child pages', async () => {
    const f = await fixture({ documentID: id(21) })
    expect(f.page.state.document?.id).toBe(id(21))
    expect(f.page.state.ancestors.items.map((item) => item.id)).toEqual([id(20)])
    expect(f.page.level(id(20)).items).toEqual([])
    expect(f.page.state.expanded).toContain(id(20))
  })
  it('moves by returned UTF-8 offsets without accumulating text or mixing versions', async () => {
    const f = await fixture()
    let changed = false
    f.intercept(async (path, init) => {
      if (!path.includes('/content')) return f.normal(path, init)
      const offset = new URL(path, 'https://owned.invalid').searchParams.get('byte_offset')!
      return json(
        content(
          20,
          offset === '0' ? 'a'.repeat(65535) : '中🙂',
          changed ? '2' : '1',
          offset,
          offset === '0',
        ),
      )
    })
    f.page.select(id(20))
    await flushPromises()
    expect(f.page.canNext.value).toBe(true)
    f.page.next()
    await flushPromises()
    expect(f.page.state.content.value).toMatchObject({
      text: { text: '中🙂', next_byte_offset: '65542' },
    })
    expect(f.page.canNext.value).toBe(false)
    expect(f.page.canPrevious.value).toBe(true)
    changed = true
    f.page.previous()
    await flushPromises()
    expect(f.page.state.content.phase).toBe('changed')
    expect(f.page.state.content.value).toBeNull()
    expect(f.page.canPrevious.value).toBe(false)
  })
  it('treats tombstone, empty body and file-unavailable as distinct successful observations', async () => {
    const f = await fixture()
    f.intercept(async (path, init) =>
      path === `${base}/${id(20)}`
        ? json({
            deleted: { id: id(20), project_id: id(10), content_version: '2', deleted_at: time },
          })
        : f.normal(path, init),
    )
    f.page.select(id(20))
    await flushPromises()
    expect(f.page.state.metadataPhase).toBe('deleted')
    expect(f.fetch.mock.calls.some(([path]) => path.includes('/content'))).toBe(false)
    f.intercept(async (path, init) =>
      path.includes('/content') ? json(content(20, '')) : f.normal(path, init),
    )
    f.page.retryDocument()
    await flushPromises()
    expect(f.page.state.content.phase).toBe('ready')
    expect(f.page.state.content.value).toMatchObject({ text: { text: '', truncated: false } })
    const file = { ...document(), source_kind: 'file', media_type: 'application/pdf' }
    f.intercept(async (path, init) =>
      path === `${base}/${id(20)}`
        ? json({ active: file })
        : path.includes('/content')
          ? json({ document: file, unavailable: 'dependency_unbound' })
          : f.normal(path, init),
    )
    f.page.retryDocument()
    await flushPromises()
    expect(f.page.state.content.value).toEqual({
      document: file,
      unavailable: 'dependency_unbound',
    })
  })
})
