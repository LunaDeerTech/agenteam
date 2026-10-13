import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { useProjectArtifacts } from '../composables/useProjectArtifacts'
import {
  artifactBase,
  artifactContent,
  artifactDeferred,
  artifactID,
  artifactJSON,
  artifactPage,
  artifactPath,
  artifactProblem,
  artifactRecord,
  artifactSessionFixture,
} from './project-artifacts.fixture'
const cleanups: (() => void)[] = []
afterEach(async () => {
  for (const close of cleanups.splice(0)) close()
  await flushPromises()
})
async function setup() {
  const f = artifactSessionFixture()
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth),
    location = shallowRef(artifactPath)
  workspace.afterNavigation(location.value)
  await flushPromises()
  const owner = useProjectArtifacts(f.auth, workspace, location)
  cleanups.push(() => {
    owner.dispose()
    workspace.dispose()
    f.auth.leave()
  })
  await flushPromises()
  expect(owner.page.phase).toBe('ready')
  return { ...f, owner, workspace, location }
}
const queries = (f: Awaited<ReturnType<typeof setup>>) =>
  f.fetch.mock.calls
    .filter(([path]) => path.startsWith(artifactBase + '?'))
    .map(([path]) => new URL(path, 'https://owned.invalid').searchParams)
describe('Artifact page generations and independent read boundaries', () => {
  it('appends an ordered second page with the applied filter, keeps draft changes local, and clears all forward cursors on apply/refresh', async () => {
    const f = await setup(),
      first = Array.from({ length: 50 }, (_, index) => artifactRecord(200 - index))
    f.intercept((path, init) =>
      !path.startsWith(artifactBase + '?')
        ? f.normal(path, init)
        : Promise.resolve(
            artifactJSON(
              artifactPage(
                path.includes('cursor=') ? [artifactRecord(150)] : first,
                path.includes('cursor=') ? undefined : 'page-two',
              ),
            ),
          ),
    )
    f.owner.draft.name = 'report-'
    f.owner.apply()
    await flushPromises()
    f.owner.draft.name = 'unsubmitted'
    f.owner.more()
    await flushPromises()
    expect(f.owner.page.items).toHaveLength(51)
    expect(queries(f).at(-1)!.get('name_query')).toBe('report-')
    expect(queries(f).at(-1)!.get('cursor')).toBe('page-two')
    f.owner.refresh()
    await flushPromises()
    expect(f.owner.page.items).toHaveLength(50)
    expect(queries(f).at(-1)!.has('cursor')).toBe(false)
    f.owner.clearFilter()
    await flushPromises()
    expect(queries(f).at(-1)!.has('name_query')).toBe(false)
    expect(f.owner.draft.name).toBe('')
  })
  it('rejects repeated items, reversed ordering and cyclic cursors without retaining a partial list', async () => {
    const f = await setup()
    const first = Array.from({ length: 50 }, (_, index) => artifactRecord(200 - index))
    for (const second of [
      artifactPage([artifactRecord(200)]),
      artifactPage([artifactRecord(201)]),
      artifactPage(
        Array.from({ length: 50 }, (_, index) => artifactRecord(150 - index)),
        'two',
      ),
    ]) {
      f.intercept((path, init) =>
        !path.startsWith(artifactBase + '?')
          ? f.normal(path, init)
          : Promise.resolve(
              artifactJSON(path.includes('cursor=') ? second : artifactPage(first, 'two')),
            ),
      )
      f.owner.refresh()
      await flushPromises()
      f.owner.more()
      await flushPromises()
      expect(f.owner.page.phase).toBe('error')
      expect(f.owner.page.items).toEqual([])
      expect(f.owner.page.nextCursor).toBeNull()
    }
  })
  it('uses returned UTF-8 offsets for explicit next/previous and halts on valid zero progress', async () => {
    const f = await setup(),
      row = { ...artifactRecord(), object: { ...artifactRecord().object, byte_size: '6' } }
    let zero = false
    f.intercept((path, init) => {
      if (!path.includes('/content?')) return f.normal(path, init)
      const offset = new URL(path, 'https://owned.invalid').searchParams.get('offset')!
      return Promise.resolve(
        artifactJSON(artifactContent(row, zero ? '' : '中', offset, zero || offset === '0')),
      )
    })
    f.owner.select(artifactID(100))
    await flushPromises()
    expect(f.owner.detail.content?.next_offset).toBe('3')
    f.owner.next()
    await flushPromises()
    expect(f.owner.detail.content?.offset).toBe('3')
    expect(f.owner.canNext.value).toBe(false)
    f.owner.previous()
    await flushPromises()
    expect(f.owner.detail.content?.offset).toBe('0')
    zero = true
    f.owner.reread()
    await flushPromises()
    expect(f.owner.detail.phase).toBe('ready')
    expect(f.owner.detail.message).toContain('未能推进')
    expect(f.owner.canNext.value).toBe(false)
    const count = f.fetch.mock.calls.length
    f.owner.next()
    await flushPromises()
    expect(f.fetch.mock.calls).toHaveLength(count)
    zero = false
    f.owner.reread()
    await flushPromises()
    expect(f.owner.canNext.value).toBe(true)
  })
  it('drops failed or changed segments while retaining an explicit restart of the selected reference', async () => {
    const f = await setup()
    for (const status of [416, 503]) {
      f.intercept((path, init) =>
        path.includes('/content?')
          ? Promise.resolve(
              artifactProblem(
                status,
                status === 416 ? 'RANGE_NOT_SATISFIABLE' : 'DEPENDENCY_UNAVAILABLE',
              ),
            )
          : f.normal(path, init),
      )
      f.owner.select(artifactID(100))
      await flushPromises()
      expect(f.owner.detail.phase).toBe('error')
      expect(f.owner.detail.content).toBeNull()
      expect(f.owner.selected.value?.artifact_ref.artifact_id).toBe(artifactID(100))
      f.intercept()
      f.owner.reread()
      await flushPromises()
      expect(f.owner.detail.phase).toBe('ready')
      f.owner.closeDetail()
    }
    const row = { ...artifactRecord(), object: { ...artifactRecord().object, byte_size: '6' } }
    f.intercept((path, init) => {
      if (!path.includes('/content?')) return f.normal(path, init)
      const offset = new URL(path, 'https://owned.invalid').searchParams.get('offset')!
      return Promise.resolve(
        artifactJSON(
          artifactContent(
            offset === '0' ? row : { ...row, version: '9007199254740994' },
            '中',
            offset,
            offset === '0',
          ),
        ),
      )
    })
    f.owner.select(artifactID(100))
    await flushPromises()
    f.owner.next()
    await flushPromises()
    expect(f.owner.detail.phase).toBe('error')
    expect(f.owner.detail.content).toBeNull()
    expect(f.owner.canNext.value).toBe(false)
    f.owner.reread()
    await flushPromises()
    expect(f.owner.detail.content?.offset).toBe('0')
  })
  it.each([403, 404])(
    'clears both old metadata and text on a current %s and permits only explicit first-page recovery',
    async (status) => {
      const f = await setup()
      f.owner.select(artifactID(100))
      await flushPromises()
      expect(f.owner.detail.content).not.toBeNull()
      f.intercept((path, init) =>
        path.includes('/content?')
          ? Promise.resolve(artifactProblem(status, status === 404 ? 'NOT_FOUND' : 'FORBIDDEN'))
          : f.normal(path, init),
      )
      f.owner.reread()
      await flushPromises()
      expect(f.owner.page.phase).toBe('unavailable')
      expect(f.owner.page.items).toEqual([])
      expect(f.owner.selected.value).toBeNull()
      expect(f.owner.detail.content).toBeNull()
      f.intercept()
      f.owner.refresh()
      await flushPromises()
      expect(f.owner.page.phase).toBe('ready')
      expect(f.owner.selected.value).toBeNull()
    },
  )
  it('discards a late detail after close, a late list after route change, and all observations under a new project identity', async () => {
    const f = await setup(),
      held = artifactDeferred<Response>()
    f.intercept((path, init) => (path.includes('/content?') ? held.promise : f.normal(path, init)))
    f.owner.select(artifactID(100))
    await flushPromises()
    f.owner.closeDetail()
    expect(f.owner.selected.value).toBeNull()
    held.resolve(artifactJSON(artifactContent()))
    await flushPromises()
    expect(f.owner.selected.value).toBeNull()
    expect(f.owner.detail.content).toBeNull()
    const list = artifactDeferred<Response>()
    f.intercept((path, init) =>
      path.startsWith(artifactBase + '?') ? list.promise : f.normal(path, init),
    )
    f.owner.refresh()
    await flushPromises()
    f.location.value = '/elsewhere'
    list.resolve(artifactJSON(artifactPage()))
    await flushPromises()
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.page.items).toEqual([])
    f.intercept()
    f.project({
      ...f.projectValue(),
      id: artifactID(80),
      name: 'Second',
      normalized_name: 'second',
    })
    f.location.value = '/owner/second/artifacts'
    f.workspace.afterNavigation(f.location.value)
    await flushPromises()
    expect(f.owner.page.phase).toBe('ready')
    expect(f.owner.page.items[0]!.artifact_ref.project_id).toBe(artifactID(80))
    expect(f.owner.selected.value).toBeNull()
    f.auth.leave()
    await flushPromises()
    expect(f.owner.page.items).toEqual([])
    expect(f.owner.visible.value).toBe(false)
  })
})
