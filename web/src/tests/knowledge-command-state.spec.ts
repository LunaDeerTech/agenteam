import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createSystemAccountAPI } from '../api/system-account'
import { createKnowledgeCommandsAPI } from '../api/knowledge-commands'
import { useKnowledgeRename, type KnowledgeRename } from '../composables/useKnowledgeRename'
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
const editors: KnowledgeRename[] = []
const pages: KnowledgeOwnerController[] = [],
  workspaces: ProjectWorkspace[] = [],
  owners: SessionController[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  for (const editor of editors.splice(0)) editor.dispose()
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
  args[16] = { knowledgeCommands: createKnowledgeCommandsAPI(fetch) }
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

async function editFixture() {
  const f = await fixture({ documentID: id(20) })
  const editor = useKnowledgeRename(f.page, f.workspace, f.auth)
  editors.push(editor)
  editor.open()
  expect(editor.state.open).toBe(true)
  return { ...f, editor }
}
function currentResponse(path: string, value: ReturnType<typeof document>) {
  const url = new URL(path, 'https://owned.invalid')
  if (url.pathname.endsWith('/children'))
    return json({ items: value.parent_document_id === null ? [value] : [] })
  if (url.pathname.endsWith('/ancestors'))
    return json({
      items: value.parent_document_id
        ? [document(Number.parseInt(value.parent_document_id.slice(-12), 16))]
        : [],
    })
  if (url.pathname.endsWith('/content'))
    return json({
      document: value,
      text: { text: '原正文', next_byte_offset: '9', truncated: false },
    })
  return json({ active: value })
}

describe('Knowledge rename original Session owner and current observations', () => {
  it('saves as ordinary Owner then re-reads current data without reusing the receipt as baseline', async () => {
    const f = await editFixture(),
      updated = { ...document(), title: '新标题', content_version: '2' }
    f.intercept(async (path, init) =>
      path.endsWith('/rename') ? json({ document: updated }) : currentResponse(path, updated),
    )
    f.editor.state.title = '新标题'
    await f.editor.save()
    await flushPromises()
    expect(f.auth.knowledgeCommands.progress?.phase).toBe('confirmed')
    expect(f.page.state.document).toMatchObject(updated)
    expect(f.page.root.value.items[0]?.title).toBe('新标题')
    expect(f.editor.canSave.value).toBe(false)
    f.editor.adoptCurrent()
    expect(f.editor.state.version).toBe('2')
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/rename'))).toHaveLength(1)
  })
  it('preserves a conflict draft and only explicitly adopts a new current version', async () => {
    const f = await editFixture()
    const newer = { ...document(), title: '并发改名', content_version: '3' }
    f.intercept(async (path, init) =>
      path.endsWith('/rename') ? problem(409, 'VERSION_CONFLICT') : currentResponse(path, newer),
    )
    f.editor.state.title = '我的草稿'
    await f.editor.save()
    expect(f.editor.state.conflict).toBe(true)
    expect(f.editor.state.title).toBe('我的草稿')
    expect(f.editor.canAdopt.value).toBe(false)
    f.editor.adoptCurrent()
    await f.editor.save()
    expect(f.editor.state.version).toBe('1')
    expect(f.editor.progress.value?.phase).toBe('rejected')
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/rename'))).toHaveLength(1)
    f.editor.reread()
    await flushPromises()
    expect(f.editor.state.version).toBe('1')
    f.editor.adoptCurrent()
    expect(f.editor.state.version).toBe('3')
    expect(f.editor.state.title).toBe('我的草稿')
    expect(f.editor.canSave.value).toBe(true)
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/rename'))).toHaveLength(1)
  })
  it('requires lookup before manual original replay and retains exact key/body', async () => {
    const f = await editFixture()
    let attempts = 0
    const updated = { ...document(), title: '原意图', content_version: '2' }
    f.intercept(async (path, init) => {
      if (path.endsWith('/rename')) {
        if (++attempts === 1) throw new Error('controlled transport')
        return json({ document: updated })
      }
      if (path.endsWith('/commands/lookup')) return json({ state: 'not_observed', receipt: null })
      return currentResponse(path, updated)
    })
    f.editor.state.title = '原意图'
    await f.editor.save()
    expect(f.editor.progress.value?.phase).toBe('uncertain')
    expect(f.editor.canReplay.value).toBe(false)
    await f.editor.lookup()
    expect(f.editor.canReplay.value).toBe(true)
    await f.editor.replay()
    await flushPromises()
    const writes = f.fetch.mock.calls.filter(([path]) => path.endsWith('/rename'))
    expect(writes).toHaveLength(2)
    expect(writes[1]![1].body).toBe(writes[0]![1].body)
    expect(new Headers(writes[1]![1].headers).get('Idempotency-Key')).toBe(
      new Headers(writes[0]![1].headers).get('Idempotency-Key'),
    )
    expect(f.editor.progress.value?.phase).toBe('confirmed')
  })
  it('keeps unknown original material through same-session checking and rejects it after identity change', async () => {
    const f = await editFixture()
    f.intercept(async (path, init) => {
      if (path.endsWith('/rename')) throw new Error('controlled transport')
      if (path.endsWith('/commands/lookup')) return json({ state: 'not_observed', receipt: null })
      return f.normal(path, init)
    })
    f.editor.state.title = '未定改名'
    await f.editor.save()
    const first = f.fetch.mock.calls.find(([path]) => path.endsWith('/rename'))!
    await f.auth.restore()
    await flushPromises()
    expect(f.auth.knowledgeCommands.progress?.phase).toBe('uncertain')
    await f.auth.knowledgeCommands.checkOriginal()
    const lookup = f.fetch.mock.calls.find(([path]) => path.endsWith('/commands/lookup'))!
    expect(new Headers(lookup[1].headers).get('Idempotency-Key')).toBe(
      new Headers(first[1].headers).get('Idempotency-Key'),
    )
    const other = session()
    other.session = { ...other.session, id: id(9) }
    f.session(other)
    await f.auth.restore()
    await flushPromises()
    expect(f.auth.knowledgeCommands.progress).toBeNull()
    await expect(f.auth.knowledgeCommands.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
  })
  it('retains actual Cookie ownership after abandoning a response whose outer cancel is held', async () => {
    const f = await editFixture(),
      gate = barrier()
    releases.push(() => gate.resolve())
    const response = json({ document: { ...document(), title: '迟到', content_version: '2' } })
    const cancel = vi.spyOn(response.body!, 'cancel').mockImplementation(() => gate.promise)
    f.intercept(async (path, init) => (path.endsWith('/rename') ? response : f.normal(path, init)))
    f.editor.state.title = '迟到'
    const pending = f.editor.save()
    await vi.waitFor(() => expect(cancel).toHaveBeenCalledOnce())
    const leaving = f.editor.permitNavigation()
    f.editor.confirmDiscard(true)
    expect(await leaving).toBe(true)
    await pending
    expect(f.auth.state.busy).toBe(true)
    const count = f.fetch.mock.calls.length
    await f.auth.restore()
    expect(f.fetch.mock.calls).toHaveLength(count)
    gate.resolve()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.knowledgeCommands.progress).toBeNull()
    expect(f.page.state.document?.title).toBe('父文档')
  })
  it.each([
    ['rename', 'reader'],
    ['rename', 'outer'],
    ['lookup', 'reader'],
    ['lookup', 'outer'],
  ] as const)('keeps %s uncertain when actual %s cancellation rejects', async (action, tail) => {
    const f = await editFixture()
    f.editor.state.title = '待查证'
    if (action === 'lookup') {
      f.intercept(async () => {
        throw new Error('controlled transport')
      })
      await f.editor.save()
      expect(f.editor.progress.value?.phase).toBe('uncertain')
    }
    const receipt = { ...document(), title: '待查证', content_version: '2' }
    const response = json(
      action === 'rename'
        ? { document: receipt }
        : {
            state: 'committed',
            receipt: { command: 'update', document: receipt, changed: true },
          },
    )
    const reader = response.body!.getReader()
    vi.spyOn(response.body!, 'getReader').mockReturnValue(reader as never)
    const release = vi.spyOn(reader, 'releaseLock')
    const outer = vi.spyOn(response.body!, 'cancel')
    const inner = vi.spyOn(reader, 'cancel')
    ;(tail === 'reader' ? inner : outer).mockRejectedValue(new Error('private controlled failure'))
    f.intercept(async (path, init) =>
      path.endsWith(action === 'rename' ? '/rename' : '/commands/lookup')
        ? response
        : f.normal(path, init),
    )
    if (action === 'rename') await f.editor.save()
    else await f.editor.lookup()
    await flushPromises()
    expect(inner).toHaveBeenCalledOnce()
    expect(release).toHaveBeenCalledOnce()
    expect(outer).toHaveBeenCalledOnce()
    expect(f.editor.progress.value?.phase).toBe('uncertain')
    expect(f.editor.progress.value?.receipt).toBeNull()
    expect(f.auth.state.busy).toBe(false)
    expect(f.page.state.document?.title).toBe('父文档')
  })
  it('clears current protected observations on local refusal without denying System', async () => {
    const f = await editFixture()
    f.intercept(async () => problem(403, 'FORBIDDEN'))
    f.editor.state.title = '拒绝'
    await f.editor.save()
    await flushPromises()
    expect(f.editor.state.denied).toBe(true)
    expect(f.editor.state.title).toBe('')
    expect(f.page.state.document).toBeNull()
    expect(f.page.root.value.items).toHaveLength(0)
    expect(f.auth.state.phase).toBe('authenticated')
  })
  it('rejects a current version below the confirmed receipt without resending', async () => {
    const f = await editFixture()
    f.intercept(async (path) =>
      path.endsWith('/rename')
        ? json({ document: { ...document(), title: '保存', content_version: '2' } })
        : currentResponse(path, document()),
    )
    f.editor.state.title = '保存'
    await f.editor.save()
    await flushPromises()
    expect(f.editor.progress.value?.phase).toBe('confirmed')
    expect(f.page.state.metadataPhase).toBe('error')
    expect(f.page.state.document).toBeNull()
    expect(f.editor.canAdopt.value).toBe(false)
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/rename'))).toHaveLength(1)
  })
  it('refreshes only root and the three known loaded parents after concurrent moves', async () => {
    const f = await editFixture()
    let moved = false
    const a = id(30),
      b = id(31),
      c = id(32)
    const before = { ...document(20, a), title: '旧标题' }
    const receipt = { ...before, parent_document_id: b, title: '改名', content_version: '2' }
    const current = { ...receipt, parent_document_id: c, content_version: '3' }
    f.intercept(async (path, init) => {
      const url = new URL(path, 'https://owned.invalid')
      if (path.endsWith('/rename')) {
        moved = true
        return json({ document: receipt })
      }
      const value = moved ? current : before
      if (url.pathname.endsWith('/children')) return json({ items: [] })
      return currentResponse(path, value)
    })
    f.page.retryDocument()
    await flushPromises()
    for (const parent of [a, b, c]) {
      f.page.readLevel(parent)
      await flushPromises()
    }
    f.editor.open()
    f.editor.state.title = '改名'
    const start = f.fetch.mock.calls.length
    await f.editor.save()
    await flushPromises()
    expect(f.editor.progress.value?.phase).toBe('confirmed')
    expect(f.page.state.document).toMatchObject({ parent_document_id: c, content_version: '3' })
    const lists = f.fetch.mock.calls
      .slice(start)
      .filter(([path]) => path.includes('/children?'))
      .map(([path]) => new URL(path, 'https://owned.invalid'))
    expect(lists.map((url) => url.searchParams.get('parent_document_id'))).toEqual([
      'null',
      a,
      b,
      c,
    ])
    expect(lists.every((url) => !url.searchParams.has('cursor'))).toBe(true)
    expect(
      f.fetch.mock.calls.slice(start).filter(([path]) => path.endsWith('/rename')),
    ).toHaveLength(1)
  })
  it('keeps archived projects read-only', async () => {
    const f = await fixture({ lifecycle: 'archived', documentID: id(20) })
    const editor = useKnowledgeRename(f.page, f.workspace, f.auth)
    editors.push(editor)
    expect(editor.canOpen.value).toBe(false)
    editor.open()
    expect(editor.state.open).toBe(false)
  })
})
