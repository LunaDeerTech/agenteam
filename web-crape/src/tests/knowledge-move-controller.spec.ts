import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { moveFixture, moveBase as base, moveProblem, id, json } from './knowledge-move.fixture'
import { titleDeferred as deferred } from './knowledge-title-search.fixture'
const fixtures: Awaited<ReturnType<typeof moveFixture>>[] = []
async function setup() {
  const f = await moveFixture()
  fixtures.push(f)
  f.editor.open()
  await flushPromises()
  return f
}
afterEach(async () => {
  for (const f of fixtures.splice(0)) await f.dispose()
})
describe('Knowledge Move current parent and destination controller', () => {
  it('retires private picker and current content after a current directory read denial', async () => {
    const f = await setup()
    await f.editor.choose(null)
    expect(f.editor.canSave.value).toBe(true)
    f.intercept(async (path, init) =>
      path.includes('/children?') ? moveProblem(403, 'FORBIDDEN') : f.normal(path, init),
    )
    await f.editor.browse(null)
    expect(f.editor.state.items).toEqual([])
    expect(f.editor.state.title).toBe('')
    expect(f.editor.canSave.value).toBe(false)
    expect(f.owner.state.document).toBeNull()
  })
  it('retires private dialog fields when conflict reread finds the source unavailable', async () => {
    const f = await setup()
    await f.editor.choose(null)
    f.intercept(async (path, init) =>
      path.endsWith('/move') ? moveProblem(409, 'VERSION_CONFLICT') : f.normal(path, init),
    )
    await f.editor.save()
    f.intercept(async (path, init) =>
      path === `${base}/${id(11)}` ? moveProblem(404, 'NOT_FOUND') : f.normal(path, init),
    )
    f.editor.reread()
    await flushPromises()
    expect(f.editor.state.items).toEqual([])
    expect(f.editor.state.title).toBe('')
    expect(f.editor.state.targetLabel).toBe('')
    expect(f.editor.canSave.value).toBe(false)
  })

  it('chooses without changing selection, then rereads actual position and source/target/current parents', async () => {
    const f = await setup()
    await f.editor.choose(id(12))
    expect(f.owner.state.selected).toBe(id(11))
    expect(f.editor.canSave.value).toBe(true)
    f.fetch.mockClear()
    f.intercept(async (path, init) => {
      if (path.endsWith('/move')) {
        expect(JSON.parse(init.body as string)).toEqual({
          expected_parent_id: id(10),
          target_parent_id: id(12),
        })
        const receipt = {
          ...f.documents.get(id(11))!,
          parent_document_id: id(12),
          title: 'B 并发正文改名',
          content_version: '9007199254740994',
        }
        f.documents.set(id(11), { ...receipt, parent_document_id: id(14) })
        return json({ document: receipt, changed: true })
      }
      return f.normal(path, init)
    })
    await f.editor.save()
    await flushPromises()
    expect(f.owner.state.document?.parent_document_id).toBe(id(14))
    expect(f.owner.state.ancestors.items.map((d) => d.id)).toEqual([id(14)])
    expect(f.owner.state.content.phase).toBe('ready')
    expect(f.editor.state.message).toBe('移动已确认；已读取当前文档与位置。')
    expect(f.editor.dirty.value).toBe(false)
    const parents = f.fetch.mock.calls
      .filter(([p]) => p.includes('/children?'))
      .map(([p]) => new URL(p, 'https://owned.invalid').searchParams.get('parent_document_id'))
    expect(parents).toEqual(expect.arrayContaining(['null', id(10), id(12), id(14)]))
    expect(f.owner.state.levels[id(10)]!.items).toEqual([])
    expect(f.owner.state.levels[id(12)]!.items).toEqual([])
  })
  it('rejects self/descendant candidates, permits explicit root and never sends a rejected selection', async () => {
    const f = await setup()
    await f.editor.choose(id(11))
    expect(f.editor.canSave.value).toBe(false)
    await f.editor.choose(id(13))
    expect(f.editor.canSave.value).toBe(false)
    expect(f.editor.state.pickerMessage).toContain('后代')
    await f.editor.save()
    expect(f.fetch.mock.calls.some(([p]) => p.endsWith('/move'))).toBe(false)
    await f.editor.choose(null)
    expect(f.editor.state.targetParent).toBeNull()
    expect(f.editor.canSave.value).toBe(true)
  })
  it('keeps chosen parent on conflict and changes expected parent only after explicit current adoption', async () => {
    const f = await setup()
    await f.editor.choose(id(12))
    f.intercept(async (path, init) =>
      path.endsWith('/move') ? moveProblem(409, 'VERSION_CONFLICT') : f.normal(path, init),
    )
    await f.editor.save()
    expect(f.editor.state.conflict).toBe(true)
    expect(f.editor.canAdopt.value).toBe(false)
    f.documents.set(id(11), { ...f.documents.get(id(11))!, parent_document_id: id(14) })
    f.editor.reread()
    await flushPromises()
    expect(f.editor.state.expectedParent).toBe(id(10))
    expect(f.editor.state.targetParent).toBe(id(12))
    expect(f.editor.canSave.value).toBe(false)
    f.editor.adoptCurrent()
    await flushPromises()
    expect(f.editor.state.expectedParent).toBe(id(14))
    expect(f.editor.state.targetParent).toBe(id(12))
    expect(f.editor.canSave.value).toBe(true)
  })
  it('keeps original Unknown after project archival: Lookup allowed, replay/new Move rejected', async () => {
    const f = await setup()
    await f.editor.choose(null)
    f.intercept(async (path, init) =>
      path.endsWith('/move')
        ? Promise.reject(new TypeError('lost'))
        : path.endsWith('/commands/lookup')
          ? json({ state: 'not_observed', receipt: null })
          : f.normal(path, init),
    )
    await f.editor.save()
    expect(f.editor.progress.value?.phase).toBe('uncertain')
    f.archive()
    await f.workspace.readCurrent()
    await flushPromises()
    expect(f.editor.canLookup.value).toBe(true)
    await f.editor.lookup()
    expect(f.editor.progress.value?.canRetryOriginal).toBe(true)
    expect(f.editor.canReplay.value).toBe(false)
    await f.editor.replay()
    await f.editor.save()
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/move'))).toHaveLength(1)
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/commands/lookup'))).toHaveLength(1)
  })
  it.each([403, 404])(
    'clears sensitive candidate/current state after current write denial %s',
    async (status) => {
      const f = await setup()
      await f.editor.choose(null)
      f.intercept(async (path, init) =>
        path.endsWith('/move')
          ? moveProblem(status, status === 403 ? 'FORBIDDEN' : 'NOT_FOUND')
          : f.normal(path, init),
      )
      await f.editor.save()
      await flushPromises()
      expect(f.editor.state.denied).toBe(true)
      expect(f.editor.state.items).toEqual([])
      expect(f.editor.state.title).toBe('')
      expect(f.owner.state.document).toBeNull()
      expect(f.auth.state.busy).toBe(false)
    },
  )
  it('ignores a late candidate after close and joins the original owner before another read', async () => {
    const f = await setup(),
      held = deferred<void>()
    f.intercept(async (path, init) => {
      if (path === `${base}/${id(12)}`) await held.promise
      return f.normal(path, init)
    })
    const selecting = f.editor.choose(id(12))
    await flushPromises()
    await f.editor.close()
    expect(f.editor.state.open).toBe(false)
    expect(f.auth.state.busy).toBe(true)
    held.resolve()
    await selecting
    await flushPromises()
    expect(f.editor.state.targetLabel).toBe('')
    expect(f.editor.state.items).toEqual([])
  })
  it('requires an explicit discard for a changed destination and does not silently abandon Unknown', async () => {
    const f = await setup()
    await f.editor.choose(null)
    const leaving = f.editor.permitNavigation()
    await flushPromises()
    expect(f.editor.state.confirming).toBe(true)
    f.editor.confirmDiscard(false)
    expect(await leaving).toBe(false)
    expect(f.editor.state.targetParent).toBeNull()
    expect(f.editor.state.open).toBe(true)
    const second = f.editor.permitNavigation()
    f.editor.confirmDiscard(true)
    expect(await second).toBe(true)
    expect(f.editor.state.open).toBe(false)
  })
})
