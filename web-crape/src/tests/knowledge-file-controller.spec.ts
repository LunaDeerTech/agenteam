import { webcrypto } from 'node:crypto'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createAccountAPI } from '../api/account'
import { createProjectOwnerAPI } from '../api/project-owner'
import { createKnowledgeOwnerAPI, type KnowledgeDocument } from '../api/knowledge-owner'
import { createKnowledgeFileAPI } from '../api/knowledge-file'
import type { Fetch } from '../api/client'
import { createSessionController } from '../composables/useSession'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { useKnowledgeOwner } from '../composables/useKnowledgeOwner'
import { useKnowledgeFile } from '../composables/useKnowledgeFile'
import { id, time, session, project, json, problem } from './knowledge-delete.fixture'

const initial = (): KnowledgeDocument => ({
  id: id(20),
  project_id: id(10),
  parent_document_id: null,
  title: '手册.pdf',
  content_version: '1',
  source_kind: 'file',
  media_type: 'application/pdf',
  status: 'active',
  indexing_status: 'ready',
  created_by: { kind: 'human', user_id: id(1) },
  created_at: time,
  updated_at: time,
})
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
function file(name: string, raw = new Uint8Array([0, 255, 13, 10])) {
  const value = new File([raw], name)
  Object.defineProperty(value, 'arrayBuffer', { value: vi.fn(async () => raw.slice().buffer) })
  return value
}
const cleanups: (() => void)[] = []
afterEach(async () => {
  for (const cleanup of cleanups.splice(0)) cleanup()
  await flushPromises()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
async function fixture() {
  vi.stubGlobal('crypto', webcrypto)
  let current = initial(),
    currentProject = { ...project },
    intercept: Fetch | undefined
  const normal: Fetch = async (path, options) => {
    const url = new URL(path, 'https://owned.invalid')
    if (path === '/api/v1/session') return json(session)
    if (path.startsWith('/api/v1/projects/resolve?') || path === `/api/v1/projects/${id(10)}`)
      return json(currentProject)
    if (url.pathname.endsWith('/children')) return json({ items: [current] })
    if (url.pathname.endsWith('/ancestors')) return json({ items: [] })
    if (url.pathname.endsWith('/content'))
      return json({
        document: current,
        text: { text: '当前解析正文', next_byte_offset: '18', truncated: false },
      })
    if (url.pathname.endsWith('/create-file') || url.pathname.endsWith('/replace-file')) {
      const body = JSON.parse(options.body as string)
      current = {
        ...current,
        id: url.pathname.split('/').at(-2)!,
        title: body.title ?? current.title,
        parent_document_id: body.parent_document_id ?? current.parent_document_id,
        media_type: body.media_type,
        content_version: body.expected_version ? String(BigInt(body.expected_version) + 1n) : '1',
        indexing_status: 'pending',
      }
      return json({ document: current })
    }
    if (url.pathname.endsWith('/source'))
      return json({ document: current, bytes_base64: 'AP8NCg==' })
    if (url.pathname.endsWith('/file-commands/lookup'))
      return json({ state: 'not_observed', receipt: null })
    return json({ active: current })
  }
  const fetch = vi.fn<Fetch>((path, options) =>
    intercept ? intercept(path, options) : normal(path, options),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[15] = createKnowledgeOwnerAPI(fetch)
  args[19] = { knowledgeFile: createKnowledgeFileAPI(fetch) }
  const auth = createSessionController(...args)
  await auth.restore()
  const workspace = createProjectWorkspace(auth)
  workspace.afterNavigation('/owner/delete/knowledge')
  await flushPromises()
  const owner = useKnowledgeOwner(
    auth,
    workspace,
    shallowRef({ projectPath: '/owner/delete', documentID: id(20) }),
  )
  await flushPromises()
  expect(owner.state.content.phase).toBe('ready')
  const editor = useKnowledgeFile(owner, workspace, auth, auth.knowledgeFiles)
  cleanups.push(() => {
    editor.dispose()
    owner.dispose()
    workspace.dispose()
    auth.leave()
  })
  return {
    auth,
    workspace,
    owner,
    editor,
    fetch,
    normal,
    intercept(value: Fetch | undefined) {
      intercept = value
    },
    replace() {
      current = { ...current, content_version: String(BigInt(current.content_version) + 1n) }
      owner.retryDocument()
    },
    async archive() {
      currentProject = { ...currentProject, lifecycle: 'archived', archived_at: time }
      await workspace.readCurrent()
      await flushPromises()
    },
  }
}

describe('Knowledge file editor with the original Session request owner', () => {
  it('uploads original bytes and rereads the confirmed current document', async () => {
    const f = await fixture()
    await f.editor.open()
    await f.editor.choose(file('Guide.docx'))
    expect(f.editor.canSave.value).toBe(true)
    f.editor.state.title = '📚'.repeat(512)
    expect(f.editor.canSave.value).toBe(true)
    f.editor.state.title += '📚'
    expect(f.editor.canSave.value).toBe(false)
    f.editor.state.title = '部署手册'
    await f.editor.save()
    await flushPromises()
    const write = f.fetch.mock.calls.find(([path]) => path.endsWith('/create-file'))!
    expect(JSON.parse(write[1].body as string)).toEqual({
      parent_document_id: null,
      title: '部署手册',
      media_type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      bytes_base64: 'AP8NCg==',
    })
    expect(f.editor.state.bytes).toBe('')
    expect(f.editor.state.open).toBe(false)
    expect(f.owner.state.document?.title).toBe('部署手册')
  })
  it('rejects oversized files before reading their bytes', async () => {
    const f = await fixture()
    await f.editor.open(null, true)
    expect(f.editor.state.open).toBe(false)
    await f.editor.open()
    const large = file('large.pdf', new Uint8Array(1048577))
    await f.editor.choose(large)
    expect(large.arrayBuffer).not.toHaveBeenCalled()
    expect(f.editor.state.message).toContain('1 MiB')
    expect(f.editor.canSave.value).toBe(false)
    expect(f.fetch.mock.calls.some(([path]) => path.endsWith('/create-file'))).toBe(false)
  })
  it('keeps Unknown bytes immutable and disallows replay after archival', async () => {
    const f = await fixture()
    await f.editor.open(f.owner.state.document, true)
    await f.editor.choose(file('new.pdf'))
    f.intercept((path, options) =>
      path.endsWith('/replace-file')
        ? Promise.reject(new TypeError('lost'))
        : f.normal(path, options),
    )
    await f.editor.save()
    expect(f.editor.pending.value).toBe(true)
    const original = f.editor.state.bytes
    await f.editor.choose(file('other.pdf', new Uint8Array([3])))
    expect(f.editor.state.bytes).toBe(original)
    await f.editor.recover()
    expect(f.editor.progress.value?.canRetryOriginal).toBe(true)
    await f.archive()
    await f.editor.recover(true)
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/replace-file'))).toHaveLength(1)
    expect(
      f.fetch.mock.calls.filter(([path]) => path.endsWith('/file-commands/lookup')),
    ).toHaveLength(1)
  })
  it('revokes current PDF preview on version change and joins late source after leaving', async () => {
    const f = await fixture(),
      create = vi.fn((_value: Blob) => 'blob:owned-pdf'),
      revoke = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: create })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revoke })
    await f.editor.source(true)
    expect(f.editor.preview.value).toBe('blob:owned-pdf')
    expect(create.mock.calls[0]?.[0].type).toBe('application/pdf')
    expect(create.mock.calls[0]?.[0].size).toBe(4)
    f.replace()
    await flushPromises()
    expect(revoke).toHaveBeenCalledWith('blob:owned-pdf')
    expect(f.editor.preview.value).toBe('')
    const hold = deferred<void>(),
      original = { ...f.owner.state.document! }
    f.intercept(async (path, options) => {
      if (path.endsWith('/source')) {
        await hold.promise
        return json({ document: original, bytes_base64: 'AP8NCg==' })
      }
      return f.normal(path, options)
    })
    const reading = f.editor.source(true)
    await flushPromises()
    expect(f.auth.state.busy).toBe(true)
    f.auth.leave()
    expect(f.auth.state.busy).toBe(true)
    hold.resolve()
    await reading
    await flushPromises()
    expect(create).toHaveBeenCalledTimes(1)
    expect(f.editor.preview.value).toBe('')
    expect(f.auth.state.busy).toBe(false)
  })
  it('clears current content after a current source denial', async () => {
    const f = await fixture()
    f.intercept(async (path, options) =>
      path.endsWith('/source') ? problem(403, 'FORBIDDEN') : f.normal(path, options),
    )
    await f.editor.source(true)
    expect(f.owner.state.document).toBeNull()
    expect(f.editor.preview.value).toBe('')
    expect(f.editor.state.bytes).toBe('')
  })
})
