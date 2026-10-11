import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createSystemAccountAPI } from '../api/system-account'
import { createKnowledgeCommandsAPI } from '../api/knowledge-commands'
import { createKnowledgeText, type KnowledgeTextEditor } from '../composables/useKnowledgeText'
import { createKnowledgeTextAPI } from '../api/knowledge-text'
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
const editors: KnowledgeTextEditor[] = []
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
  args[19] = {
    knowledgeCommands: createKnowledgeCommandsAPI(fetch),
    knowledgeText: createKnowledgeTextAPI(fetch),
  }
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
  const editor = createKnowledgeText(auth, workspace)
  editor.afterNavigation('/owner/demo/knowledge')
  editor.attachReader(page)
  editors.push(editor)
  return {
    editor,
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

function observed(path: string, doc: ReturnType<typeof document>, text = '当前正文') {
  const url = new URL(path, 'https://owned.invalid')
  if (url.pathname.endsWith('/children'))
    return json({
      items:
        url.searchParams.get('parent_document_id') === (doc.parent_document_id ?? 'null')
          ? [doc]
          : [],
    })
  if (url.pathname.endsWith('/ancestors')) return json({ items: [] })
  if (url.pathname.endsWith('/content'))
    return json({
      document: doc,
      text: {
        text,
        next_byte_offset: String(new TextEncoder().encode(text).length),
        truncated: false,
      },
    })
  return json({ active: doc })
}
describe('Knowledge text application lifetime owner', () => {
  it('creates from the root using a fixed target, then reads actual metadata, content and tree', async () => {
    const f = await fixture()
    await f.editor.openCreate()
    f.editor.state.title = '  我的文本🙂  '
    f.editor.state.text = 'first\nsecond'
    f.editor.state.media = 'text/markdown'
    let created: ReturnType<typeof document> | undefined
    f.intercept(async (path, init) => {
      if (path.endsWith('/create-text')) {
        const body = JSON.parse(init.body as string)
        created = {
          ...document(),
          id: path.split('/').at(-2)!,
          title: body.title,
          media_type: body.media_type,
        }
        return json({ document: created })
      }
      return observed(path, created!, f.editor.state.text)
    })
    expect(f.editor.canSave.value).toBe(true)
    await f.editor.save()
    await flushPromises()
    expect(f.editor.state.needsRead).toBe(false)
    expect(f.editor.state.message).toBe('保存已确认；已读取当前文档、正文与目录。')
    expect(f.page.state.document).toEqual(created)
    expect(f.page.root.value.items).toEqual([created])
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/create-text'))).toHaveLength(1)
    expect(f.fetch.mock.calls.some(([p]) => p.endsWith('/text-commands/lookup'))).toBe(false)
  })
  it('preserves conflicting input until explicitly adopting a freshly read version', async () => {
    const f = await fixture({ documentID: id(20) })
    await f.editor.openEdit(f.page.state.document!)
    expect(f.editor.state.text).toBe('父文档正文')
    f.editor.state.text = '我的修改'
    f.editor.state.media = 'text/markdown'
    const newer = { ...document(), title: '并发改名', content_version: '4' }
    f.intercept(async (path) =>
      path.endsWith('/replace-text')
        ? problem(409, 'VERSION_CONFLICT')
        : observed(path, newer, '别人修改'),
    )
    await f.editor.save()
    expect(f.editor.state.conflict).toBe(true)
    expect(f.editor.state.version).toBe('1')
    expect(f.editor.state.text).toBe('我的修改')
    await f.editor.rereadConflict()
    expect(f.editor.current.value?.text.text).toBe('别人修改')
    expect(f.editor.state.version).toBe('1')
    f.editor.adoptVersion()
    expect(f.editor.state.version).toBe('4')
    expect(f.editor.state.text).toBe('我的修改')
    expect(f.editor.state.media).toBe('text/markdown')
    expect(f.editor.canSave.value).toBe(true)
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/replace-text'))).toHaveLength(1)
  })
  it('retains Unknown original key, body and draft through same-Session checking and view replacement', async () => {
    const f = await fixture({ documentID: id(20) })
    await f.editor.openEdit(f.page.state.document!)
    f.editor.state.text = '原意图\r\n🙂'
    const calls: { path: string; body: string; key: string | null }[] = []
    f.intercept(async (path, init) => {
      if (path.endsWith('/replace-text')) {
        calls.push({
          path,
          body: init.body as string,
          key: new Headers(init.headers).get('Idempotency-Key'),
        })
        throw new TypeError('disconnect')
      }
      return f.normal(path, init)
    })
    await f.editor.save()
    expect(f.editor.progress.value?.phase).toBe('uncertain')
    const gate = barrier<Response>()
    releases.push(() => gate.resolve(json(session())))
    f.intercept((path, init) => (path === '/api/v1/session' ? gate.promise : f.normal(path, init)))
    const restore = f.auth.restore()
    await flushPromises()
    expect(f.editor.visible.value).toBe(false)
    expect(f.editor.state.text).toBe('原意图\r\n🙂')
    gate.resolve(json(session()))
    await restore
    await flushPromises()
    expect(f.editor.progress.value?.phase).toBe('uncertain')
    expect(f.editor.canLookup.value).toBe(true)
    f.intercept(async (path, init) => {
      if (path.endsWith('/text-commands/lookup')) {
        calls.push({
          path,
          body: init.body as string,
          key: new Headers(init.headers).get('Idempotency-Key'),
        })
        return json({ state: 'not_observed', receipt: null })
      }
      if (path.endsWith('/replace-text')) {
        calls.push({
          path,
          body: init.body as string,
          key: new Headers(init.headers).get('Idempotency-Key'),
        })
        throw new TypeError('disconnect')
      }
      return f.normal(path, init)
    })
    await f.editor.lookup()
    expect(f.editor.canReplay.value).toBe(true)
    expect(calls).toHaveLength(2)
    await f.editor.replay()
    expect(calls).toHaveLength(3)
    expect(calls[2]).toEqual(calls[0])
    expect(JSON.parse(calls[1]!.body).request).toEqual(JSON.parse(calls[0]!.body))
    expect(calls[1]!.key).toBe(calls[0]!.key)
  })
  it('requires fresh complete content for editing and never saves a paged prefix', async () => {
    const f = await fixture({ documentID: id(20) })
    f.intercept(async (path, init) =>
      path.includes('/content')
        ? json(content(20, 'prefix', '1', '0', true))
        : f.normal(path, init),
    )
    await f.editor.openEdit(f.page.state.document!)
    expect(f.editor.state.version).toBe('')
    expect(f.editor.state.text).toBe('')
    expect(f.editor.canSave.value).toBe(false)
    await f.editor.save()
    expect(f.fetch.mock.calls.some(([p]) => p.endsWith('/replace-text'))).toBe(false)
  })
  it('keeps confirmed saves non-repeatable while actual current reads fail', async () => {
    const f = await fixture({ documentID: id(20) })
    await f.editor.openEdit(f.page.state.document!)
    f.editor.state.text = '修改'
    f.intercept(async (path) =>
      path.endsWith('/replace-text')
        ? json({ document: document(20, null, '2') })
        : problem(503, 'UNAVAILABLE'),
    )
    await f.editor.save()
    await flushPromises()
    expect(f.editor.state.saved).toBe(true)
    expect(f.editor.state.needsRead).toBe(true)
    expect(f.editor.canSave.value).toBe(false)
    await f.editor.save()
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/replace-text'))).toHaveLength(1)
  })
  it('asks before discarding a draft and clears it when the real identity changes', async () => {
    const f = await fixture()
    await f.editor.openCreate()
    f.editor.state.title = 'private draft'
    const leave = f.editor.permitNavigation()
    await flushPromises()
    expect(f.editor.state.confirming).toBe(true)
    f.editor.confirmDiscard(false)
    expect(await leave).toBe(false)
    expect(f.editor.state.title).toBe('private draft')
    f.session({ ...session(), session: { ...session().session, id: id(50) } })
    await f.auth.restore()
    await flushPromises()
    expect(f.editor.state.title).toBe('')
    expect(f.editor.state.open).toBe(false)
    expect(f.auth.knowledgeText.progress).toBe(null)
  })
  it('clears private input and original recovery when a real current read denies access', async () => {
    const f = await fixture({ documentID: id(20) })
    await f.editor.openEdit(f.page.state.document!)
    f.editor.state.text = 'private changed'
    f.intercept(async (path) =>
      path.endsWith('/replace-text')
        ? json({ document: document(20, null, '2') })
        : problem(403, 'FORBIDDEN'),
    )
    await f.editor.save()
    await flushPromises()
    expect(f.editor.state.open).toBe(false)
    expect(f.editor.state.text).toBe('')
    expect(f.auth.knowledgeText.progress).toBe(null)
  })
  it('does not expose a previous Session Unknown intent after a different Session is restored', async () => {
    const f = await fixture({ documentID: id(20) })
    await f.editor.openEdit(f.page.state.document!)
    f.editor.state.text = 'private uncertain'
    f.intercept(async (path, init) => {
      if (path.endsWith('/replace-text')) throw new TypeError('disconnect')
      return f.normal(path, init)
    })
    await f.editor.save()
    expect(f.auth.knowledgeText.progress?.phase).toBe('uncertain')
    f.session({ ...session(), session: { ...session().session, id: id(50) } })
    await f.auth.restore()
    await flushPromises()
    expect(f.editor.state.text).toBe('')
    expect(f.auth.knowledgeText.progress).toBe(null)
    await expect(f.auth.knowledgeText.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
  })
  it.each(['ancestors', 'content'])(
    'clears the confirmed editor on current %s denial while preserving the fact of saving',
    async (part) => {
      const f = await fixture({ documentID: id(20) })
      await f.editor.openEdit(f.page.state.document!)
      f.editor.state.text = 'private committed body'
      f.intercept(async (path) =>
        path.endsWith('/replace-text')
          ? json({ document: document(20, null, '2') })
          : path.includes('/' + part)
            ? problem(
                part === 'ancestors' ? 403 : 404,
                part === 'ancestors' ? 'FORBIDDEN' : 'NOT_FOUND',
              )
            : observed(path, document(20, null, '2')),
      )
      await f.editor.save()
      await flushPromises()
      expect(f.editor.state.open).toBe(false)
      expect(f.editor.state.title).toBe('')
      expect(f.editor.state.text).toBe('')
      expect(f.editor.current.value).toBe(null)
      expect(f.editor.state.saved).toBe(true)
      expect(f.editor.state.message).toContain('保存已确认；')
      expect(f.auth.knowledgeText.progress).toBe(null)
    },
  )
})
