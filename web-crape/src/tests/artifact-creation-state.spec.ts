import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createArtifactCreation } from '../composables/useArtifactCreation'
import { useProjectArtifacts } from '../composables/useProjectArtifacts'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import {
  artifactBase,
  artifactContent,
  artifactDeferred,
  artifactID,
  artifactJSON,
  artifactPage,
  artifactPath,
  artifactProblem,
  artifactProject,
  creationInput,
  createdRecord,
  creationSessionFixture,
} from './artifact-creation.fixture'
const cleanup: (() => void)[] = []
afterEach(async () => {
  for (const close of cleanup.splice(0)) close()
  await flushPromises()
  vi.unstubAllGlobals()
})
async function fixture() {
  const f = creationSessionFixture()
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth),
    location = shallowRef(artifactPath)
  workspace.afterNavigation(location.value)
  await flushPromises()
  const reader = useProjectArtifacts(f.auth, workspace, location),
    editor = createArtifactCreation(f.auth, workspace)
  editor.afterNavigation(location.value)
  const detach = editor.attachReader(reader)
  cleanup.push(() => {
    detach()
    editor.dispose()
    reader.dispose()
    workspace.dispose()
    f.auth.leave()
  })
  await flushPromises()
  editor.openCreate()
  Object.assign(editor.state, {
    name: creationInput.name,
    description: creationInput.description,
    media: creationInput.media_type,
    content: creationInput.content,
  })
  return { ...f, workspace, reader, editor, location }
}
const writes = (f: Awaited<ReturnType<typeof fixture>>) =>
  f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')
describe('Artifact creation visible workflow and read recovery', () => {
  it('confirms the historical receipt separately, reads the actual list and exact P/A/F, and retries only GET after a read failure', async () => {
    const f = await fixture(),
      row = createdRecord()
    let fail = true
    f.intercept((path, init) =>
      path.endsWith('/create-content')
        ? Promise.resolve(artifactJSON({ artifact: row }))
        : path.startsWith(artifactBase + '?')
          ? Promise.resolve(artifactJSON(artifactPage([])))
          : path.includes('/content?')
            ? Promise.resolve(
                fail
                  ? artifactProblem(503)
                  : artifactJSON(artifactContent(row, creationInput.content)),
              )
            : f.normal(path, init),
    )
    await f.editor.create()
    await flushPromises()
    expect(f.editor.state).toMatchObject({ confirmed: true, needsRead: true })
    expect(f.reader.page.items).toHaveLength(0)
    expect(f.reader.creationRead.phase).toBe('error')
    expect(f.editor.state.message).toContain('不要再次创建')
    expect(
      f.fetch.mock.calls.some(
        ([path]) =>
          path ===
          `${artifactBase}/${row.artifact_ref.artifact_id}/files/${row.artifact_ref.file_id}/content?offset=0&limit=8192`,
      ),
    ).toBe(true)
    fail = false
    f.editor.rereadCreated()
    await flushPromises()
    expect(f.editor.state).toMatchObject({ confirmed: true, needsRead: false })
    expect(f.reader.detail.content?.text).toBe(creationInput.content)
    expect(writes(f)).toHaveLength(1)
    expect(f.reader.page.items).toHaveLength(0)
  })
  it('keeps draft validation local and preserves exact bytes without silently trimming', async () => {
    const f = await fixture()
    f.editor.state.name = '界'.repeat(86)
    await f.editor.create()
    expect(f.editor.state.field).not.toBe('')
    expect(f.editor.canSave.value).toBe(true)
    expect(writes(f)).toHaveLength(0)
    expect(f.auth.artifactCreation.progress).toBeNull()
    f.editor.state.name = creationInput.name
    f.intercept(async () => artifactProblem(503))
    await f.editor.create()
    expect(JSON.parse(writes(f)[0]![1].body as string)).toEqual(creationInput)
    expect(f.editor.canSave.value).toBe(false)
  })
  it('preserves a same-Session check and its original lookup, but clears real identity changes', async () => {
    const f = await fixture()
    f.intercept(async () => artifactProblem(503))
    await f.editor.create()
    const held = artifactDeferred<Response>()
    cleanup.push(() => held.resolve(artifactJSON(f.sessionValue())))
    f.intercept((path, init) => (path === '/api/v1/session' ? held.promise : f.normal(path, init)))
    const restore = f.auth.restore()
    await flushPromises()
    expect(f.editor.state.content).toBe(creationInput.content)
    expect(f.editor.visible.value).toBe(false)
    held.resolve(artifactJSON(f.sessionValue()))
    await restore
    await flushPromises()
    expect(f.editor.state.content).toBe(creationInput.content)
    expect(f.editor.canLookup.value).toBe(true)
    f.session({ ...f.sessionValue(), session: { ...f.sessionValue().session, id: artifactID(8) } })
    f.intercept(f.normal)
    await f.auth.restore()
    await flushPromises()
    expect(f.editor.state.content).toBe('')
    expect(f.editor.state.open).toBe(false)
    expect(f.auth.artifactCreation.progress).toBeNull()
  })
  it('requires explicit local discard and never turns leaving into server cancellation', async () => {
    const f = await fixture()
    f.intercept(async () => artifactProblem(503))
    await f.editor.create()
    const leave = f.editor.confirmLeave()
    expect(f.editor.state.confirming).toBe(true)
    f.editor.confirmDiscard(false)
    await expect(leave).resolves.toBe(false)
    expect(f.editor.state.content).toBe(creationInput.content)
    const accepted = f.editor.confirmLeave()
    f.editor.confirmDiscard(true)
    await expect(accepted).resolves.toBe(true)
    expect(f.editor.state.content).toBe('')
    expect(f.auth.artifactCreation.progress).toBeNull()
    expect(writes(f)).toHaveLength(1)
  })
  it('prevents original replay on archived projects while retaining allowed lookup and cancellation', async () => {
    const f = await fixture()
    f.intercept(async () => artifactProblem(503))
    await f.editor.create()
    f.intercept((path, init) =>
      path.endsWith('/lookup')
        ? Promise.resolve(artifactJSON({ state: 'not_observed', artifact: null }))
        : f.normal(path, init),
    )
    await f.editor.lookup()
    expect(f.editor.canRetry.value).toBe(true)
    f.project({
      ...f.projectValue(),
      lifecycle: 'archived',
      archived_at: f.projectValue().updated_at,
    })
    await f.workspace.readCurrent()
    await flushPromises()
    expect(f.editor.canRetry.value).toBe(false)
    expect(f.editor.canLookup.value).toBe(true)
    await f.editor.retry()
    expect(writes(f)).toHaveLength(2)
    f.intercept((path, init) =>
      path.endsWith('/lookup')
        ? Promise.resolve(artifactJSON({ state: 'pending', artifact: null }))
        : f.normal(path, init),
    )
    await f.editor.lookup()
    expect(f.editor.canCancel.value).toBe(true)
  })
  it('rejects a late successful command after route disposal and clears private context', async () => {
    const f = await fixture(),
      held = artifactDeferred<Response>(),
      started = artifactDeferred<void>()
    cleanup.push(() => held.resolve(artifactJSON({ artifact: createdRecord() })))
    f.intercept(async () => {
      started.resolve()
      return held.promise
    })
    const writing = f.editor.create()
    await started.promise
    f.editor.afterNavigation('/elsewhere')
    held.resolve(artifactJSON({ artifact: createdRecord() }))
    await writing
    await flushPromises()
    expect(f.editor.state).toMatchObject({ open: false, confirmed: false, name: '', content: '' })
    expect(f.auth.artifactCreation.progress).toBeNull()
    expect(
      f.fetch.mock.calls
        .filter(([path]) => path.startsWith(artifactBase))
        .every(([, init]) => init.method === 'POST' || init.body === undefined),
    ).toBe(true)
  })
})
